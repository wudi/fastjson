package fastjson

import (
	"math"
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
}

var decoderPool = sync.Pool{New: func() interface{} { return &decoder{} }}

func (d *decoder) reset(data []byte) {
	d.data = data
	d.p = 0
	d.scratch = d.scratch[:0]
	// reset slabs: drop references so GC can reclaim if no longer held.
	d.fslab.buf = nil
	d.sslab.buf = nil
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

func (d *decoder) skipWS() {
	b := d.data
	p := d.p
	for p < len(b) {
		c := b[p]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		p++
	}
	d.p = p
}

// -------- Generic decodeAny (returns interface{}) --------

func (d *decoder) decodeAny() (interface{}, error) {
	// skip whitespace inline
	b := d.data
	p := d.p
	for p < len(b) {
		c := b[p]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		p++
	}
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
		return d.decodeArray()
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
	p := d.p
	for p < len(b) {
		c := b[p]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		p++
	}
	if p < len(b) && b[p] == '}' {
		d.p = p + 1
		return map[string]interface{}{}, nil
	}
	// Cheap size hint: scan up to 256 bytes, count commas and close-brace.
	// Over-counts (commas inside nested strings / nested objects) but that
	// only enlarges the initial hint, which still beats the map's growth
	// doubling + rehash cost observed at 47 % CPU on twitter.json decode.
	hint := peekObjectHint(b, p)
	m := make(map[string]interface{}, hint)
	d.p = p
	for {
		b = d.data
		p = d.p
		for p < len(b) {
			c := b[p]
			if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
				break
			}
			p++
		}
		if p >= len(b) || b[p] != '"' {
			return nil, syntaxErr("expected string key", p)
		}
		d.p = p
		key, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		b = d.data
		p = d.p
		for p < len(b) {
			c := b[p]
			if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
				break
			}
			p++
		}
		if p >= len(b) || b[p] != ':' {
			return nil, syntaxErr("expected ':'", p)
		}
		d.p = p + 1
		val, err := d.decodeAny()
		if err != nil {
			return nil, err
		}
		m[key] = val
		b = d.data
		p = d.p
		for p < len(b) {
			c := b[p]
			if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
				break
			}
			p++
		}
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
	// skip WS and check for empty array
	p := d.p
	for p < len(b) {
		c := b[p]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		p++
	}
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
		// inline skipWS and delimiter check
		b = d.data
		p = d.p
		for p < len(b) {
			c := b[p]
			if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
				break
			}
			p++
		}
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
	remain := len(b) - p
	// Threshold chosen so the AVX-512 kernel pays off (VPBROADCASTB setup
	// + function call overhead). Below 64, use inline 8-byte SWAR.
	if hasAVX512 && remain >= 64 {
		off := scanStringAVX512(&b[p], remain)
		p += off
	} else {
		for p+8 <= len(b) {
			w := *(*uint64)(unsafe.Pointer(&b[p]))
			if hasQuoteOrBackslashOrCtl(w) {
				break
			}
			p += 8
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
	const lo = 0x0101010101010101
	const hi = 0x8080808080808080
	// equals 0x22 / 0x5c via zero-byte on XOR
	q := w ^ (lo * 0x22)
	b := w ^ (lo * 0x5c)
	hasQuote := (q - lo) & ^q & hi
	hasBslash := (b - lo) & ^b & hi
	// less-than 0x20
	hasCtl := (w - lo*0x20) & ^w & hi
	return (hasQuote | hasBslash | hasCtl) != 0
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
	if p < len(b) && b[p] == '.' {
		p++
		for p < len(b) && b[p] >= '0' && b[p] <= '9' {
			p++
		}
	}
	if p < len(b) && (b[p] == 'e' || b[p] == 'E') {
		p++
		if p < len(b) && (b[p] == '+' || b[p] == '-') {
			p++
		}
		for p < len(b) && b[p] >= '0' && b[p] <= '9' {
			p++
		}
	}
	d.p = p
	return b[start:p], nil
}

// peekObjectHint returns a starting size hint for `make(map, hint)` by
// counting ',' bytes until '}' or end of the cheap scan budget. Nested
// commas (inside strings / sub-objects) over-count, which only enlarges
// the hint — still better than default-8 + repeated rehash. On twitter
// this was ~47 % CPU.
//
// Size-gated: when the remaining buffer is small (≤ 160 B) we skip the
// scan entirely and fall back to hint=8. Small.json is a 154-B file
// where the scan overhead dominated, and the object was tiny anyway.
func peekObjectHint(b []byte, p int) int {
	remain := len(b) - p
	if remain <= 160 {
		return 8
	}
	// 256-byte cap keeps worst-case scan at ~4 cache lines; past that the
	// object is big enough that a slightly-low hint of 8 is hurting anyway.
	end := p + 256
	if end > len(b) {
		end = len(b)
	}
	count := 1
	for i := p; i < end; i++ {
		c := b[i]
		if c == ',' {
			count++
		} else if c == '}' {
			return count
		}
	}
	// Hit the cap without finding '}' — assume a large object. Sonic uses
	// similar behaviour via its JIT.
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
