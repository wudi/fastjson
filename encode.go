package fastjson

import (
	"math"
	"reflect"
	"strconv"
	"sync"
	"unicode/utf8"
	"unsafe"
)

type encoder struct {
	buf []byte
}

var encoderPool = sync.Pool{New: func() interface{} { return &encoder{buf: make([]byte, 0, 1024)} }}

func (e *encoder) reset() { e.buf = e.buf[:0] }

func (e *encoder) encode(v interface{}) error {
	if v == nil {
		e.buf = append(e.buf, 'n', 'u', 'l', 'l')
		return nil
	}
	// Fast path for common scalar interface values.
	switch x := v.(type) {
	case string:
		e.writeString(x)
		return nil
	case bool:
		if x {
			e.buf = append(e.buf, 't', 'r', 'u', 'e')
		} else {
			e.buf = append(e.buf, 'f', 'a', 'l', 's', 'e')
		}
		return nil
	case float64:
		return e.writeFloat(x, 64)
	case int:
		e.buf = strconv.AppendInt(e.buf, int64(x), 10)
		return nil
	case int64:
		e.buf = strconv.AppendInt(e.buf, x, 10)
		return nil
	case map[string]interface{}:
		return e.writeMapInterface(x)
	case []interface{}:
		return e.writeSliceInterface(x)
	}
	rv := reflect.ValueOf(v)
	enc := cachedEncoder(rv.Type())
	return enc(e, unsafePointerOf(rv))
}

func unsafePointerOf(rv reflect.Value) unsafe.Pointer {
	// For most kinds the reflect.Value holds the value itself (not a pointer).
	// We need a pointer to that value. Use Addr() if addressable; otherwise
	// copy to a heap slot.
	if rv.CanAddr() {
		return unsafe.Pointer(rv.UnsafeAddr())
	}
	// Make addressable via a new pointer of the same type.
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	return unsafe.Pointer(p.Pointer())
}

// -------- string writing --------

var htmlEscape = [256]bool{'<': true, '>': true, '&': true}

func (e *encoder) writeString(s string) {
	e.buf = append(e.buf, '"')
	n := len(s)
	if n == 0 {
		e.buf = append(e.buf, '"')
		return
	}
	var i int
	if hasAVX512 && n >= 64 {
		i = scanStringAVX512(unsafe.StringData(s), n)
	} else {
		// Inline 8-byte SWAR scan for short strings.
		sp := unsafe.StringData(s)
		for i+8 <= n {
			w := *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(sp)) + uintptr(i)))
			if hasQuoteOrBackslashOrCtl(w) {
				break
			}
			i += 8
		}
		for i < n {
			c := s[i]
			if c == '"' || c == '\\' || c < 0x20 {
				break
			}
			i++
		}
	}
	if i == n {
		e.buf = append(e.buf, s...)
		e.buf = append(e.buf, '"')
		return
	}
	e.writeStringSlow(s, i)
}

func (e *encoder) writeStringSlow(s string, start int) {
	// copy the already-scanned prefix
	e.buf = append(e.buf, s[:start]...)
	for i := start; i < len(s); {
		c := s[i]
		if c < 0x20 {
			switch c {
			case '\n':
				e.buf = append(e.buf, '\\', 'n')
			case '\r':
				e.buf = append(e.buf, '\\', 'r')
			case '\t':
				e.buf = append(e.buf, '\\', 't')
			case '\b':
				e.buf = append(e.buf, '\\', 'b')
			case '\f':
				e.buf = append(e.buf, '\\', 'f')
			default:
				e.buf = append(e.buf, '\\', 'u', '0', '0', hexChar[c>>4], hexChar[c&0xf])
			}
			i++
			continue
		}
		if c == '"' {
			e.buf = append(e.buf, '\\', '"')
			i++
			continue
		}
		if c == '\\' {
			e.buf = append(e.buf, '\\', '\\')
			i++
			continue
		}
		// handle valid UTF-8 fast
		if c < utf8.RuneSelf {
			e.buf = append(e.buf, c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			e.buf = append(e.buf, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			continue
		}
		e.buf = append(e.buf, s[i:i+size]...)
		i += size
	}
	e.buf = append(e.buf, '"')
}

var hexChar = "0123456789abcdef"

// -------- number writing --------

func (e *encoder) writeFloat(f float64, bits int) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return &UnsupportedTypeError{Type: reflect.TypeOf(f)}
	}
	abs := math.Abs(f)
	fmt := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) {
			fmt = 'e'
		} else if bits == 32 && (abs < 1e-6 || abs >= 1e21) {
			fmt = 'e'
		}
	}
	e.buf = strconv.AppendFloat(e.buf, f, fmt, -1, bits)
	return nil
}

// -------- map[string]interface{} / []interface{} --------

func (e *encoder) writeMapInterface(m map[string]interface{}) error {
	e.buf = append(e.buf, '{')
	first := true
	for k, v := range m {
		if !first {
			e.buf = append(e.buf, ',')
		}
		first = false
		e.writeString(k)
		e.buf = append(e.buf, ':')
		if err := e.encodeAny(v); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) writeSliceInterface(a []interface{}) error {
	e.buf = append(e.buf, '[')
	for i, v := range a {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		if err := e.encodeAny(v); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ']')
	return nil
}

// encodeAny is an inlined type switch for interface{} values. It duplicates
// the top-level encode() switch but skips the entry-point nil check and the
// reflect fallback for unknown types, which lets the compiler inline the
// common cases (string/float64/map/slice/bool — the shape of a decoded
// JSON tree) into the hot map/slice iterator.
func (e *encoder) encodeAny(v interface{}) error {
	switch x := v.(type) {
	case nil:
		e.buf = append(e.buf, 'n', 'u', 'l', 'l')
		return nil
	case string:
		e.writeString(x)
		return nil
	case float64:
		return e.writeFloat(x, 64)
	case bool:
		if x {
			e.buf = append(e.buf, 't', 'r', 'u', 'e')
		} else {
			e.buf = append(e.buf, 'f', 'a', 'l', 's', 'e')
		}
		return nil
	case map[string]interface{}:
		return e.writeMapInterface(x)
	case []interface{}:
		return e.writeSliceInterface(x)
	case int:
		e.buf = strconv.AppendInt(e.buf, int64(x), 10)
		return nil
	case int64:
		e.buf = strconv.AppendInt(e.buf, x, 10)
		return nil
	}
	return e.encode(v)
}
