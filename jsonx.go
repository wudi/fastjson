// Package jsonx is a high-performance JSON library with a drop-in
// encoding/json API. It targets beating bytedance/sonic on AMD64 with AVX-512
// by using type-specialized decoders cached by reflect.Type, unsafe field
// writes, SWAR structural scanning, and branchless number parsing.
package jsonx

import (
	"io"
)

// Unmarshal parses the JSON-encoded data and stores the result in the value
// pointed to by v. It is API-compatible with encoding/json.
func Unmarshal(data []byte, v interface{}) error {
	d := decoderPool.Get().(*decoder)
	d.reset(data)
	err := d.decodeInto(v)
	decoderPool.Put(d)
	return err
}

// Marshal returns the JSON encoding of v. It is API-compatible with
// encoding/json.
func Marshal(v interface{}) ([]byte, error) {
	e := encoderPool.Get().(*encoder)
	e.reset()
	err := e.encode(v)
	if err != nil {
		encoderPool.Put(e)
		return nil, err
	}
	// hand back a copy so the pooled buffer can be reused
	out := make([]byte, len(e.buf))
	copy(out, e.buf)
	encoderPool.Put(e)
	return out, nil
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	d := decoderPool.Get().(*decoder)
	d.reset(data)
	ok := d.validate()
	decoderPool.Put(d)
	return ok
}

// Decoder mirrors encoding/json.Decoder surface minimally.
type Decoder struct {
	r   io.Reader
	buf []byte
}

func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: r} }

func (d *Decoder) Decode(v interface{}) error {
	if d.buf == nil {
		b, err := io.ReadAll(d.r)
		if err != nil {
			return err
		}
		d.buf = b
	}
	return Unmarshal(d.buf, v)
}

// Encoder mirrors encoding/json.Encoder minimally.
type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: w} }

func (e *Encoder) Encode(v interface{}) error {
	data, err := Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = e.w.Write(data)
	return err
}
