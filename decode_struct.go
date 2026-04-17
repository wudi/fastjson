package fastjson

import (
	"reflect"
	"strings"
	"unsafe"
)

// structField holds the info needed to decode one field.
type structField struct {
	name    string // JSON name
	prefix  uint64 // first min(8, len(name)) bytes, little-endian; zero-padded
	nameLen int
	offset  uintptr
	dec     typedDecodeFn
}

type structPlan struct {
	fields []structField
}

// loadPrefix8 reads up to 8 bytes of s into a uint64 (little-endian).
// Bytes beyond len(s) are zero.
func loadPrefix8(s string) uint64 {
	var b [8]byte
	n := len(s)
	if n > 8 {
		n = 8
	}
	copy(b[:], s[:n])
	return *(*uint64)(unsafe.Pointer(&b[0]))
}

func buildStructDecoder(t reflect.Type) typedDecodeFn {
	plan := &structPlan{}
	n := t.NumField()
	for i := 0; i < n; i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		if tag != "" {
			if comma := strings.Index(tag, ","); comma >= 0 {
				if comma > 0 {
					name = tag[:comma]
				}
			} else {
				name = tag
			}
		}
		plan.fields = append(plan.fields, structField{
			name:    name,
			prefix:  loadPrefix8(name),
			nameLen: len(name),
			offset:  f.Offset,
			dec:     cachedDecoder(f.Type),
		})
	}
	return func(d *decoder, p unsafe.Pointer) error {
		return decodeStruct(d, p, plan)
	}
}

func decodeStruct(d *decoder, p unsafe.Pointer, plan *structPlan) error {
	d.skipWS()
	if d.p >= len(d.data) {
		return syntaxErr("expected object", d.p)
	}
	if d.data[d.p] == 'n' {
		return d.decodeNull()
	}
	if d.data[d.p] != '{' {
		return syntaxErr("expected object", d.p)
	}
	d.p++
	d.skipWS()
	if d.p < len(d.data) && d.data[d.p] == '}' {
		d.p++
		return nil
	}
	for {
		d.skipWS()
		if d.p >= len(d.data) || d.data[d.p] != '"' {
			return syntaxErr("expected key", d.p)
		}
		// Read key without allocation — slice alias is enough for lookup.
		key, err := d.decodeStringRaw()
		if err != nil {
			return err
		}
		d.skipWS()
		if d.p >= len(d.data) || d.data[d.p] != ':' {
			return syntaxErr("expected ':'", d.p)
		}
		d.p++
		// Field dispatch: first 8 bytes of the key loaded as uint64, then
		// linear-scan comparing (prefix, length) against each field.
		// When name length > 8, also compare the tail. This kills the
		// fnv1aBytes hot spot (was ~4 % CPU on struct decode) and
		// collapses the check to a pointer-sized compare.
		klen := len(key)
		var kb [8]byte
		if klen >= 8 {
			*(*uint64)(unsafe.Pointer(&kb[0])) = *(*uint64)(unsafe.Pointer(&key[0]))
		} else {
			copy(kb[:], key)
		}
		kprefix := *(*uint64)(unsafe.Pointer(&kb[0]))
		found := false
		for i := range plan.fields {
			f := &plan.fields[i]
			if f.prefix != kprefix || f.nameLen != klen {
				continue
			}
			// length + prefix match. If the name fits in 8 bytes,
			// that's conclusive; otherwise compare the tail.
			if klen <= 8 || f.name[8:] == b2sUnsafe(key[8:]) {
				if err := f.dec(d, unsafe.Add(p, f.offset)); err != nil {
					return err
				}
				found = true
				break
			}
		}
		if !found {
			// skip value
			if err := d.skipValue(); err != nil {
				return err
			}
		}
		d.skipWS()
		if d.p >= len(d.data) {
			return syntaxErr("unexpected end in struct", d.p)
		}
		if d.data[d.p] == ',' {
			d.p++
			continue
		}
		if d.data[d.p] == '}' {
			d.p++
			return nil
		}
		return syntaxErr("expected ',' or '}'", d.p)
	}
}

// decodeStringRaw returns the *unescaped* bytes of the string (aliased into
// input if no escapes present; otherwise a scratch slice). Only valid until
// the next scratch use.
func (d *decoder) decodeStringRaw() ([]byte, error) {
	b := d.data
	p := d.p
	if p >= len(b) || b[p] != '"' {
		return nil, syntaxErr("expected string", p)
	}
	p++
	start := p
	remain := len(b) - p
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
			return b[start:p], nil
		}
		if c == '\\' {
			// escape path: copy to scratch, then return scratch slice
			d.scratch = append(d.scratch[:0], b[start:p]...)
			for p < len(b) {
				c := b[p]
				if c == '"' {
					d.p = p + 1
					return d.scratch, nil
				}
				if c == '\\' {
					p++
					if p >= len(b) {
						return nil, syntaxErr("bad escape", p)
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
							return nil, syntaxErr("bad \\u escape", p)
						}
						r, ok := hexToRune(b[p+1 : p+5])
						if !ok {
							return nil, syntaxErr("bad \\u hex", p)
						}
						if r >= 0xd800 && r <= 0xdbff && p+11 <= len(b) && b[p+5] == '\\' && b[p+6] == 'u' {
							r2, ok2 := hexToRune(b[p+7 : p+11])
							if ok2 && r2 >= 0xdc00 && r2 <= 0xdfff {
								r = 0x10000 + (r-0xd800)*0x400 + (r2 - 0xdc00)
								p += 6
							}
						}
						d.scratch = utf8AppendRune(d.scratch, r)
						p += 4
					default:
						return nil, syntaxErr("bad escape char", p)
					}
					p++
					continue
				}
				if c < 0x20 {
					return nil, syntaxErr("invalid control char in string", p)
				}
				d.scratch = append(d.scratch, c)
				p++
			}
			return nil, syntaxErr("unterminated string", p)
		}
		if c < 0x20 {
			return nil, syntaxErr("invalid control char in string", p)
		}
		p++
	}
	return nil, syntaxErr("unterminated string", p)
}

// skipValue consumes one JSON value without producing a result.
func (d *decoder) skipValue() error {
	d.skipWS()
	if d.p >= len(d.data) {
		return syntaxErr("unexpected end", d.p)
	}
	c := d.data[d.p]
	switch {
	case c == '"':
		_, err := d.decodeStringRaw()
		return err
	case c == '{' || c == '[':
		return d.skipContainer()
	case c == 't' || c == 'f':
		_, err := d.decodeBool()
		return err
	case c == 'n':
		return d.decodeNull()
	default:
		_, err := d.decodeNumberSlice()
		return err
	}
}

func (d *decoder) skipContainer() error {
	b := d.data
	depth := 0
	p := d.p
	for p < len(b) {
		c := b[p]
		switch c {
		case '{', '[':
			depth++
			p++
		case '}', ']':
			depth--
			p++
			if depth == 0 {
				d.p = p
				return nil
			}
		case '"':
			// scan to end of string
			p++
			for p < len(b) {
				if b[p] == '\\' {
					p += 2
					continue
				}
				if b[p] == '"' {
					p++
					break
				}
				p++
			}
		default:
			p++
		}
	}
	return syntaxErr("unterminated container", p)
}

