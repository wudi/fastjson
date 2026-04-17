//go:build arm64

package jsonx

import "unsafe"

// writeDigitsAsm is the arm64 assembly kernel for the Schubfach
// digit-emission inner loop (see writedigits_arm64.s). Contract matches
// the amd64 sibling:
//
//   - `sig` is the decimal significand, 1 ≤ sig < 10^17 (enforced by caller).
//   - `buf` points to a fixed-size scratch with at least 24 writable bytes.
//     `cnt` is the exact decimal digit count, in [1, 17].
//   - If `trim != 0` and the trailing 8 digits of `sig` are all zero, those
//     8 bytes are NOT written; the return value is `cnt-8`. Otherwise the
//     return value is `cnt`.
//
// Scalar arm64 port — uses UDIV + MSUB for the 1e8 and 1e4 divides, and
// the same (v * 10486) >> 20 magic div-by-100 trick used on amd64. No
// SVE or NEON dependency, so runs on every 64-bit ARM CPU Go supports.
//
//go:noescape
func writeDigitsAsm(sig uint64, buf *byte, cnt uint64, trim uint64, tab *uint16) uint64

// writeDigitsFast dispatches to the arm64 assembly kernel when the
// input is big enough to amortise the call frame, otherwise falls
// through to the pure-Go writeDigitsStack. Threshold matches amd64.
//
//go:nosplit
func writeDigitsFast(sig uint64, buf *[24]byte, cnt int, trim bool) int {
	if cnt >= 8 {
		t := uint64(0)
		if trim {
			t = 1
		}
		bp := (*byte)(unsafe.Pointer(&buf[0]))
		return int(writeDigitsAsm(sig, bp, uint64(cnt), t, &digits100[0]))
	}
	return writeDigitsStack(sig, buf, cnt, trim)
}
