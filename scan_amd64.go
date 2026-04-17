//go:build amd64

package fastjson

import "github.com/klauspost/cpuid/v2"

// hasFastScan is set once at init based on AVX-512F + AVX-512BW
// availability. Goroutine-safe to read.
var hasFastScan = cpuid.CPU.Supports(cpuid.AVX512F, cpuid.AVX512BW)

//go:noescape
func scanStringSIMD(p *byte, n int) int

//go:noescape
func skipWSSIMD(p *byte, n int) int
