package jsonx

import (
	"reflect"
	"unsafe"
)

// eface is the Go runtime's internal layout of interface{}.
// Guaranteed by the language spec to be { typePtr, dataPtr }.
type eface struct {
	typ  unsafe.Pointer // *runtime._type for the boxed type
	data unsafe.Pointer // pointer to value (or small scalars by value)
}

// Capture the runtime type descriptors for primitive types. We extract them
// from a boxed zero value via the internal iface layout.
var (
	typeFloat64            unsafe.Pointer
	typeBool               unsafe.Pointer
	typeString             unsafe.Pointer
	typeMapStringInterface unsafe.Pointer
	typeSliceInterface     unsafe.Pointer
	typeInt                unsafe.Pointer
	typeInt64              unsafe.Pointer
	// cached immutable singletons
	ifaceTrue  interface{} = true
	ifaceFalse interface{} = false
	ifaceNil   interface{} = nil
)

func init() {
	var f float64 = 1.5
	var fi interface{} = f
	typeFloat64 = (*eface)(unsafe.Pointer(&fi)).typ

	var bi interface{} = true
	typeBool = (*eface)(unsafe.Pointer(&bi)).typ

	var si interface{} = "a"
	typeString = (*eface)(unsafe.Pointer(&si)).typ

	var mi interface{} = map[string]interface{}{}
	typeMapStringInterface = (*eface)(unsafe.Pointer(&mi)).typ

	var sli interface{} = []interface{}{}
	typeSliceInterface = (*eface)(unsafe.Pointer(&sli)).typ

	var ii interface{} = int(0)
	typeInt = (*eface)(unsafe.Pointer(&ii)).typ

	var i64 interface{} = int64(0)
	typeInt64 = (*eface)(unsafe.Pointer(&i64)).typ

	_ = reflect.TypeOf
}

// ifaceFromFloat64Ptr builds an interface{} that holds a float64 stored at
// the given pointer (typically a slot in a slab). Caller must keep the
// backing array alive (GC follows the data pointer).
func ifaceFromFloat64Ptr(p *float64) interface{} {
	var i interface{}
	ef := (*eface)(unsafe.Pointer(&i))
	ef.typ = typeFloat64
	ef.data = unsafe.Pointer(p)
	return i
}

// ifaceFromStringPtr mirrors the above for string-valued interfaces. A
// string header is 16 bytes, so its data pointer must point at the header
// itself (which lives in a slab).
func ifaceFromStringPtr(p *string) interface{} {
	var i interface{}
	ef := (*eface)(unsafe.Pointer(&i))
	ef.typ = typeString
	ef.data = unsafe.Pointer(p)
	return i
}

// floatSlab is a bulk allocator for float64 interface{} boxes. Instead of
// one mallocgc per float (which dominates canada.json decode), we allocate
// a chunk of float64 cells and hand out pointers into it. Starts small to
// keep tiny-input overhead low; grows geometrically up to a cap.
type floatSlab struct {
	buf []float64
}

const floatSlabMax = 512

func (s *floatSlab) alloc(v float64) *float64 {
	if len(s.buf) == cap(s.buf) {
		newCap := cap(s.buf) * 2
		if newCap == 0 {
			newCap = 4
		} else if newCap > floatSlabMax {
			newCap = floatSlabMax
		}
		s.buf = make([]float64, 0, newCap)
	}
	s.buf = append(s.buf, v)
	return &s.buf[len(s.buf)-1]
}

// stringSlab mirrors floatSlab for strings. A string header is 16 bytes; we
// want to hand out stable pointers to headers, so we allocate them in a
// slab too.
type stringSlab struct {
	buf []string
}

const stringSlabMax = 256

func (s *stringSlab) alloc(v string) *string {
	if len(s.buf) == cap(s.buf) {
		newCap := cap(s.buf) * 2
		if newCap == 0 {
			newCap = 4
		} else if newCap > stringSlabMax {
			newCap = stringSlabMax
		}
		s.buf = make([]string, 0, newCap)
	}
	s.buf = append(s.buf, v)
	return &s.buf[len(s.buf)-1]
}

// byteSlab pools the backing bytes for strings copied into user structs.
// Each struct-field string is physically owned (Unmarshal guarantees the
// decoded value outlives the input), so we must copy — but copying each
// string via `string(bs)` triggers an individual mallocgc. The slab
// collapses those into chunked allocations that amortize the runtime
// overhead. The slab keeps its chunks reachable via d.scratch-style
// append semantics: once a chunk is full, a new chunk is allocated and
// the old one remains live through the returned string headers.
type byteSlab struct {
	buf []byte
}

// allocString copies src into the slab and returns a Go string whose
// backing data lives inside the slab chunk.
func (s *byteSlab) allocString(src []byte) string {
	n := len(src)
	if n == 0 {
		return ""
	}
	if cap(s.buf)-len(s.buf) < n {
		// Grow: next chunk at least 2× previous, enough for src.
		newCap := cap(s.buf) * 2
		if newCap < 256 {
			newCap = 256
		}
		if newCap < n {
			newCap = n
		}
		s.buf = make([]byte, 0, newCap)
	}
	start := len(s.buf)
	s.buf = append(s.buf, src...)
	// String header aliases into the slab. Safe: chunk is kept alive
	// by the returned string (GC follows the data pointer).
	return unsafe.String(&s.buf[start], n)
}
