//go:build amd64

package fastjson

//go:noescape
func scanStringAVX512(p *byte, n int) int

//go:noescape
func skipWSAVX512(p *byte, n int) int
