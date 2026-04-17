//go:build !amd64

package fastjson

func scanStringAVX512(p *byte, n int) int {
	// Should never be called outside amd64 (guarded by hasAVX512).
	return 0
}
