//go:build !amd64 && !arm64

package fastjson

// hasFastScan is false on arches without a SIMD string-scan kernel.
// Call sites gate on this before invoking scanStringSIMD / skipWSSIMD,
// so the zero-returning stubs below are defensive only.
const hasFastScan = false

func scanStringSIMD(p *byte, n int) int {
	return 0
}

func skipWSSIMD(p *byte, n int) int {
	return 0
}
