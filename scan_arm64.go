//go:build arm64

package jsonx

// hasFastScan is always true on arm64: NEON (Advanced SIMD) is
// mandatory in the ARMv8-A base architecture that Go targets, so the
// runtime never needs to fall through to the SWAR path purely on
// capability grounds.
const hasFastScan = true

// NEON kernels — see scan_arm64.s and skipws_arm64.s. Signatures
// match the amd64 AVX-512 siblings so the call sites are identical.

//go:noescape
func scanStringSIMD(p *byte, n int) int

//go:noescape
func skipWSSIMD(p *byte, n int) int
