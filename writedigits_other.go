//go:build !amd64

package fastjson

// writeDigitsFast on non-amd64 is just the pure-Go path; the asm kernel
// does not exist outside amd64.
//
//go:nosplit
func writeDigitsFast(sig uint64, buf *[24]byte, cnt int, trim bool) int {
	return writeDigitsStack(sig, buf, cnt, trim)
}
