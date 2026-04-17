package jsonx

import "encoding/base64"

func base64Decode(s string) ([]byte, error) {
	// encoding/json uses std base64 with padding.
	return base64.StdEncoding.DecodeString(s)
}

func base64Encode(dst []byte, src []byte) []byte {
	n := base64.StdEncoding.EncodedLen(len(src))
	start := len(dst)
	dst = append(dst, make([]byte, n)...)
	base64.StdEncoding.Encode(dst[start:], src)
	return dst
}
