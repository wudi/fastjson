package jsonx

import (
	"math"
	"math/bits"
	"reflect"
	"sync"
	"unsafe"
)

// decoder is the reusable state for one Unmarshal call. Pooled.
type decoder struct {
	data []byte
	p    int // current position
	// scratch for unescaped strings
	scratch []byte
	// slab allocators for interface{}-boxed scalars — collapse N small
	// mallocgc calls into a single chunked allocation.
	fslab floatSlab
	sslab stringSlab
	// slice-header slab: pools the 24-byte []interface{} headers so
	// decodeAny can return them as interface{} without boxing per call.
	aslab sliceIfaceSlab
	// rootPeeked is set after the first object's size-hint scan. Inner
	// objects skip the peek: we'd otherwise pay the 256-B cost on every
	// nested object (32 % CPU on deeply-formatted JSON).
	rootPeeked bool
}

var decoderPool = sync.Pool{New: func() interface{} { return &decoder{} }}

func (d *decoder) reset(data []byte) {
	d.data = data
	d.p = 0
	d.scratch = d.scratch[:0]
	d.rootPeeked = false
	// reset slabs: drop references so GC can reclaim if no longer held.
	d.fslab.buf = nil
	d.sslab.buf = nil
	d.aslab.buf = nil
}

// decodeInto dispatches on the dynamic type of v.
func (d *decoder) decodeInto(v interface{}) error {
	if v == nil {
		return &InvalidUnmarshalError{}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	d.skipWS()
	// Fast path for *interface{} (the most common "generic" target).
	if ip, ok := v.(*interface{}); ok {
		val, err := d.decodeAny()
		if err != nil {
			return err
		}
		*ip = val
		return d.trailing()
	}
	// Compile or fetch a type-specialized decoder.
	elem := rv.Elem()
	dec := cachedDecoder(elem.Type())
	if err := dec(d, unsafe.Pointer(elem.UnsafeAddr())); err != nil {
		return err
	}
	return d.trailing()
}

func (d *decoder) trailing() error {
	d.skipWS()
	if d.p != len(d.data) {
		return syntaxErr("trailing data", d.p)
	}
	return nil
}

// -------- Whitespace / structural --------

// skipWS is the method form of skipWSFast. Kept tiny so the compiler
// inlines it — otherwise the per-call cost cascades through all the
// typed decoders that call d.skipWS() per field.
func (d *decoder) skipWS() {
	d.p = skipWSFast(d.data, d.p)
}

// skipWSFast is the fast-path for the common "next byte is non-WS" plus
// "single-space separator" cases (e.g. `": "<value>`, `, <value>`). Only
// when the whitespace run is >1 byte do we hand off to skipWSDeep.
func skipWSFast(b []byte, p int) int {
	if p >= len(b) {
		return p
	}
	c := b[p]
	if c > ' ' {
		return p
	}
	if c == ' ' && p+1 < len(b) && b[p+1] > ' ' {
		return p + 1
	}
	return skipWSDeep(b, p)
}

// skipWSDeep consumes whitespace bytes starting at p. For long runs
// (≥ 64 bytes remaining and AVX-512 available) we dispatch to the
// asm kernel; otherwise scalar.
func skipWSDeep(b []byte, p int) int {
	remain := len(b) - p
	if hasFastScan && remain >= 64 {
		return p + skipWSSIMD(&b[p], remain)
	}
	for p < len(b) {
		c := b[p]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		p++
	}
	return p
}

// -------- Generic decodeAny (returns interface{}) --------

func (d *decoder) decodeAny() (interface{}, error) {
	b := d.data
	p := skipWSFast(b, d.p)
	if p >= len(b) {
		d.p = p
		return nil, syntaxErr("unexpected end", p)
	}
	d.p = p
	c := b[p]
	// dispatch ordered by expected frequency (numbers and strings dominate)
	if c == '"' {
		s, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		// Slab-box the string so N tiny heap strings become one slab alloc.
		return ifaceFromStringPtr(d.sslab.alloc(s)), nil
	}
	if c == '{' {
		return d.decodeObject()
	}
	if c == '[' {
		arr, err := d.decodeArray()
		if err != nil {
			return nil, err
		}
		// Box the 24-byte slice header through the slab so the interface
		// conversion doesn't mallocgc per array. Each chunk amortizes
		// ~256 boxings into a single heap allocation.
		return ifaceFromSlicePtr(d.aslab.alloc(arr)), nil
	}
	if c == 't' || c == 'f' {
		v, err := d.decodeBool()
		if err != nil {
			return nil, err
		}
		if v {
			return ifaceTrue, nil
		}
		return ifaceFalse, nil
	}
	if c == 'n' {
		return nil, d.decodeNull()
	}
	if c == '-' || (c >= '0' && c <= '9') {
		v, err := d.decodeNumber()
		if err != nil {
			return nil, err
		}
		return ifaceFromFloat64Ptr(d.fslab.alloc(v)), nil
	}
	return nil, syntaxErr("invalid character", p)
}

func (d *decoder) decodeObject() (map[string]interface{}, error) {
	d.p++
	b := d.data
	p := skipWSFast(b, d.p)
	if p < len(b) && b[p] == '}' {
		d.p = p + 1
		return map[string]interface{}{}, nil
	}
	// Pay the 256-B size-hint peek only on the root object. Nested
	// objects in deeply-formatted JSON would otherwise pay the peek
	// thousands of times (observed 32 % CPU on 10-level corpus).
	hint := 8
	if !d.rootPeeked {
		hint = peekObjectHint(b, p)
		d.rootPeeked = true
	}
	m := make(map[string]interface{}, hint)
	d.p = p
	for {
		b = d.data
		p = skipWSFast(b, d.p)
		if p >= len(b) || b[p] != '"' {
			return nil, syntaxErr("expected string key", p)
		}
		d.p = p
		key, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		// Fast-path `:` adjacent to key (the common compact-JSON case).
		if d.p < len(d.data) && d.data[d.p] == ':' {
			d.p++
		} else {
			d.p = skipWSFast(d.data, d.p)
			if d.p >= len(d.data) || d.data[d.p] != ':' {
				return nil, syntaxErr("expected ':'", d.p)
			}
			d.p++
		}
		val, err := d.decodeAny()
		if err != nil {
			return nil, err
		}
		m[key] = val
		// Fast-path the typical `"..."<,|}>` adjacency where no
		// whitespace sits between the value and the structural char.
		if d.p < len(d.data) {
			c := d.data[d.p]
			if c == ',' {
				d.p++
				continue
			}
			if c == '}' {
				d.p++
				return m, nil
			}
		}
		b = d.data
		p = skipWSFast(b, d.p)
		if p >= len(b) {
			return nil, syntaxErr("unexpected end in object", p)
		}
		if b[p] == ',' {
			d.p = p + 1
			continue
		}
		if b[p] == '}' {
			d.p = p + 1
			return m, nil
		}
		return nil, syntaxErr("expected ',' or '}'", p)
	}
}

func (d *decoder) decodeArray() ([]interface{}, error) {
	d.p++
	b := d.data
	p := skipWSFast(b, d.p)
	if p < len(b) && b[p] == ']' {
		d.p = p + 1
		return []interface{}{}, nil
	}
	d.p = p
	arr := make([]interface{}, 0, 4)
	for {
		val, err := d.decodeAny()
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
		// Fast-path adjacent `,`/`]` to skip the skipWSFast call in the
		// compact-JSON case (no whitespace between element and separator).
		if d.p < len(d.data) {
			c := d.data[d.p]
			if c == ',' {
				d.p++
				continue
			}
			if c == ']' {
				d.p++
				return arr, nil
			}
		}
		b = d.data
		p = skipWSFast(b, d.p)
		if p >= len(b) {
			return nil, syntaxErr("unexpected end in array", p)
		}
		if b[p] == ',' {
			d.p = p + 1
			continue
		}
		if b[p] == ']' {
			d.p = p + 1
			return arr, nil
		}
		return nil, syntaxErr("expected ',' or ']'", p)
	}
}

func (d *decoder) decodeBool() (bool, error) {
	b := d.data
	p := d.p
	if p+4 <= len(b) && b[p] == 't' && b[p+1] == 'r' && b[p+2] == 'u' && b[p+3] == 'e' {
		d.p = p + 4
		return true, nil
	}
	if p+5 <= len(b) && b[p] == 'f' && b[p+1] == 'a' && b[p+2] == 'l' && b[p+3] == 's' && b[p+4] == 'e' {
		d.p = p + 5
		return false, nil
	}
	return false, syntaxErr("invalid bool", p)
}

func (d *decoder) decodeNull() error {
	b := d.data
	p := d.p
	if p+4 <= len(b) && b[p] == 'n' && b[p+1] == 'u' && b[p+2] == 'l' && b[p+3] == 'l' {
		d.p = p + 4
		return nil
	}
	return syntaxErr("invalid null", p)
}

// -------- String --------
// Returns an unescaped Go string. If no escapes present, uses an unsafe
// aliased view of the input buffer (zero-alloc).

func (d *decoder) decodeString() (string, error) {
	b := d.data
	p := d.p
	if b[p] != '"' {
		return "", syntaxErr("expected string", p)
	}
	p++
	start := p
	// SWAR scan until first `"`/`\\`/ctl match, then pin exact byte position
	// via TrailingZeros on the combined mask. Skips the per-byte scalar tail
	// that used to re-scan up to 7 bytes after the SWAR said "something's in
	// this word".
	for p+8 <= len(b) {
		w := *(*uint64)(unsafe.Pointer(&b[p]))
		mask := stringBreakMask(w)
		if mask != 0 {
			p += bits.TrailingZeros64(mask) >> 3
			c := b[p]
			if c == '"' {
				d.p = p + 1
				return b2sUnsafe(b[start:p]), nil
			}
			if c == '\\' {
				return d.decodeStringEscape(start, p)
			}
			return "", syntaxErr("invalid control char in string", p)
		}
		p += 8
		// Long-string path: dispatch AVX-512/NEON once we've scanned past
		// the 16-byte warmup window and still have at least 64 bytes left.
		if hasFastScan && p-start >= 16 && len(b)-p >= 64 {
			p += scanStringSIMD(&b[p], len(b)-p)
			break
		}
	}
	for p < len(b) {
		c := b[p]
		if c == '"' {
			d.p = p + 1
			return b2sUnsafe(b[start:p]), nil
		}
		if c == '\\' {
			return d.decodeStringEscape(start, p)
		}
		if c < 0x20 {
			return "", syntaxErr("invalid control char in string", p)
		}
		p++
	}
	return "", syntaxErr("unterminated string", p)
}

// hasQuoteOrBackslashOrCtl reports whether any byte in w is '"' (0x22),
// '\\' (0x5c), or < 0x20.
//
// Per Hacker's Delight:
//   hasZeroByte(v)   = (v - lo)     & ~v & 0x80*lo     // any byte == 0
//   hasLessThan(v,n) = (v - n*lo)   & ~v & 0x80*lo     // any byte <  n
// We conservatively accept false positives (they only cost us a slow
// byte-by-byte scan); false negatives would be wrong.
func hasQuoteOrBackslashOrCtl(w uint64) bool {
	return stringBreakMask(w) != 0
}

// stringBreakMask returns a bitmask with bit 7 of each byte set if that
// byte is '"', '\\', or a control byte (< 0x20). `bits.TrailingZeros64` on
// the mask divided by 8 is the byte offset of the first match — the
// position-pinning variant is 2–3 × faster than a scalar retry loop when
// the caller needs to know exactly where the match is.
func stringBreakMask(w uint64) uint64 {
	const lo = 0x0101010101010101
	const hi = 0x8080808080808080
	q := w ^ (lo * 0x22)
	b := w ^ (lo * 0x5c)
	hasQuote := (q - lo) & ^q & hi
	hasBslash := (b - lo) & ^b & hi
	hasCtl := (w - lo*0x20) & ^w & hi
	return hasQuote | hasBslash | hasCtl
}

func (d *decoder) decodeStringEscape(start, p int) (string, error) {
	// copy the already-scanned portion into scratch and continue handling escapes.
	b := d.data
	d.scratch = append(d.scratch[:0], b[start:p]...)
	for p < len(b) {
		c := b[p]
		if c == '"' {
			d.p = p + 1
			out := string(d.scratch)
			return out, nil
		}
		if c == '\\' {
			p++
			if p >= len(b) {
				return "", syntaxErr("bad escape", p)
			}
			esc := b[p]
			switch esc {
			case '"', '\\', '/':
				d.scratch = append(d.scratch, esc)
			case 'b':
				d.scratch = append(d.scratch, '\b')
			case 'f':
				d.scratch = append(d.scratch, '\f')
			case 'n':
				d.scratch = append(d.scratch, '\n')
			case 'r':
				d.scratch = append(d.scratch, '\r')
			case 't':
				d.scratch = append(d.scratch, '\t')
			case 'u':
				if p+5 > len(b) {
					return "", syntaxErr("bad \\u escape", p)
				}
				r, ok := hexToRune(b[p+1 : p+5])
				if !ok {
					return "", syntaxErr("bad \\u hex", p)
				}
				// surrogate pair?
				if r >= 0xd800 && r <= 0xdbff {
					if p+11 <= len(b) && b[p+5] == '\\' && b[p+6] == 'u' {
						r2, ok := hexToRune(b[p+7 : p+11])
						if ok && r2 >= 0xdc00 && r2 <= 0xdfff {
							r = 0x10000 + (r-0xd800)*0x400 + (r2 - 0xdc00)
							p += 6
						}
					}
				}
				d.scratch = utf8AppendRune(d.scratch, r)
				p += 4
			default:
				return "", syntaxErr("bad escape char", p)
			}
			p++
			continue
		}
		if c < 0x20 {
			return "", syntaxErr("invalid control char in string", p)
		}
		d.scratch = append(d.scratch, c)
		p++
	}
	return "", syntaxErr("unterminated string", p)
}

func hexToRune(b []byte) (rune, bool) {
	var r rune
	for i := 0; i < 4; i++ {
		c := b[i]
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return r, true
}

func utf8AppendRune(b []byte, r rune) []byte {
	switch {
	case r < 0x80:
		return append(b, byte(r))
	case r < 0x800:
		return append(b, byte(0xc0|r>>6), byte(0x80|r&0x3f))
	case r < 0x10000:
		return append(b, byte(0xe0|r>>12), byte(0x80|(r>>6)&0x3f), byte(0x80|r&0x3f))
	default:
		return append(b, byte(0xf0|r>>18), byte(0x80|(r>>12)&0x3f), byte(0x80|(r>>6)&0x3f), byte(0x80|r&0x3f))
	}
}

// -------- Number --------
// decodeNumber returns a float64 (matching encoding/json's behavior when
// decoding into interface{}).
func (d *decoder) decodeNumber() (float64, error) {
	v, err := d.scanNumber()
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, syntaxErr("invalid number", d.p)
	}
	return v, nil
}

// decodeNumberSlice returns the raw slice (without advancing past trailing
// whitespace), used by the typed struct decoder to parse integers directly.
func (d *decoder) decodeNumberSlice() ([]byte, error) {
	start := d.p
	b := d.data
	p := start
	if p < len(b) && b[p] == '-' {
		p++
	}
	s := p
	for p < len(b) && b[p] >= '0' && b[p] <= '9' {
		p++
	}
	if p == s {
		return nil, syntaxErr("invalid number", start)
	}
	// RFC 8259: leading zero must not be followed by more digits.
	if b[s] == '0' && p-s > 1 {
		return nil, syntaxErr("invalid number (leading zero)", start)
	}
	if p < len(b) && b[p] == '.' {
		p++
		fs := p
		for p < len(b) && b[p] >= '0' && b[p] <= '9' {
			p++
		}
		if p == fs {
			return nil, syntaxErr("invalid number (no digits after '.')", start)
		}
	}
	if p < len(b) && (b[p] == 'e' || b[p] == 'E') {
		p++
		if p < len(b) && (b[p] == '+' || b[p] == '-') {
			p++
		}
		es := p
		for p < len(b) && b[p] >= '0' && b[p] <= '9' {
			p++
		}
		if p == es {
			return nil, syntaxErr("invalid exponent", start)
		}
	}
	d.p = p
	return b[start:p], nil
}

// peekObjectHint returns a starting size hint for `make(map, hint)` at
// the ROOT object only. Callers gate on `d.depth == 0` so inner /
// nested objects use hint=8 without paying the scan. Properly depth-
// tracks commas (top-level only), skips strings with escape handling.
func peekObjectHint(b []byte, p int) int {
	remain := len(b) - p
	if remain <= 160 {
		return 8
	}
	end := p + 256
	if end > len(b) {
		end = len(b)
	}
	count := 1
	depth := 0
	for i := p; i < end; i++ {
		c := b[i]
		switch c {
		case ',':
			if depth == 0 {
				count++
			}
		case '}':
			if depth == 0 {
				return count
			}
			depth--
		case ']':
			if depth > 0 {
				depth--
			}
		case '{', '[':
			depth++
		case '"':
			i++
			for i < end {
				if b[i] == '\\' {
					i += 2
					continue
				}
				if b[i] == '"' {
					break
				}
				i++
			}
		}
	}
	return 16
}

// -------- Validate (structural-only) --------
func (d *decoder) validate() bool {
	d.skipWS()
	_, err := d.decodeAny()
	if err != nil {
		return false
	}
	d.skipWS()
	return d.p == len(d.data)
}

// -------- unsafe helpers --------

func b2sUnsafe(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
