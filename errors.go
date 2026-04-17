package fastjson

import (
	"fmt"
	"reflect"
)

type SyntaxError struct {
	Msg    string
	Offset int64
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("fastjson: %s at offset %d", e.Msg, e.Offset)
}

type UnmarshalTypeError struct {
	Value  string
	Type   reflect.Type
	Offset int64
}

func (e *UnmarshalTypeError) Error() string {
	return fmt.Sprintf("fastjson: cannot unmarshal %s into Go value of type %s", e.Value, e.Type)
}

type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "fastjson: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Ptr {
		return fmt.Sprintf("fastjson: Unmarshal(non-pointer %s)", e.Type)
	}
	return fmt.Sprintf("fastjson: Unmarshal(nil %s)", e.Type)
}

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return fmt.Sprintf("fastjson: unsupported type: %s", e.Type)
}

func syntaxErr(msg string, off int) error {
	return &SyntaxError{Msg: msg, Offset: int64(off)}
}
