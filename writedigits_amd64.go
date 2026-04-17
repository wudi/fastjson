//go:build amd64

package fastjson

import (
	"unsafe"

	"github.com/klauspost/cpuid/v2"
)

// hasBMI2 reports whether the runtime CPU supports BMI2 (for MULX) and
// ADX (for ADCX/ADOX). We don't use BMI2/ADX in the current
// writeDigitsAsm kernel — it uses IMUL3Q + DIV which are always
// available on amd64 — but the flag is the runtime switch point where
// future MULX-based roundOdd/f64todec rewrites can slot in.
var hasBMI2ADX = cpuid.CPU.Supports(cpuid.BMI2, cpuid.ADX)

// writeDigitsAsm is the assembly kernel for the Schubfach digit-emission
// inner loop (see writedigits_amd64.s). Contract:
//
//   - `sig` is the decimal significand, 1 ≤ sig < 10^17 (enforced by caller).
//   - `buf` points to a fixed-size scratch with at least 24 writable bytes.
//     `cnt` is the exact decimal digit count, in [1, 17].
//   - If `trim != 0` and the trailing 8 digits of `sig` are all zero, those
//     8 bytes are NOT written; the return value is `cnt-8`. Otherwise the
//     return value is `cnt`.
//
// Written as ABI0 (stack-based). The frame is tiny (5 word args + 1 word
// return) so the stack round-trip cost is small relative to the 30+
// instructions of work inside. If profiling flags the call boundary,
// a future revision can move to register ABIInternal.
//
//go:noescape
func writeDigitsAsm(sig uint64, buf *byte, cnt uint64, trim uint64, tab *uint16) uint64

// writeDigitsFast dispatches to the amd64 assembly kernel when available
// and the input is big enough to amortise the call, otherwise the pure-Go
// writeDigitsStack. The threshold (cnt >= 8) ensures the kernel's one big
// DIV-by-1e8 pays for the call-frame cost.
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
