package fastjson

import (
	"unsafe"
)

// hasFastScan reports whether the host has a SIMD string-scan kernel
// available. Defined per-arch:
//   - amd64: runtime cpuid check for AVX-512 BW (scan_amd64.go)
//   - arm64: unconditionally true — NEON is mandatory in ARMv8-A
//     (scan_arm64.go)
//   - other arches: false (scan_other.go)
//
// The matching SIMD kernels are scanStringSIMD and skipWSSIMD, whose
// implementations live in the per-arch .s files (AVX-512BW on amd64,
// NEON on arm64). On arches without a kernel, the stubs in
// scan_other.go return 0 and are never reached since hasFastScan is
// false.

// scanString returns the offset of the first byte in p[0:n] that is '"',
// '\\', or < 0x20. Returns n if none found.
//
// Dispatches to the SIMD kernel when hasFastScan is true and n >= 64
// (threshold amortises the broadcast/zeroupper setup cost). Falls back
// to an 8-byte SWAR scan otherwise.
func scanString(p unsafe.Pointer, n int) int {
	if hasFastScan && n >= 64 {
		return scanStringSIMD((*byte)(p), n)
	}
	return scanStringSWAR(p, n)
}

// scanStringSWAR is the pure-Go fallback. 8 bytes at a time via the
// hasQuoteOrBackslashOrCtl formula from decode.go.
func scanStringSWAR(p unsafe.Pointer, n int) int {
	i := 0
	for i+8 <= n {
		w := *(*uint64)(unsafe.Pointer(uintptr(p) + uintptr(i)))
		if hasQuoteOrBackslashOrCtl(w) {
			// precise byte position within this 8-byte window
			for j := 0; j < 8; j++ {
				c := *(*byte)(unsafe.Pointer(uintptr(p) + uintptr(i+j)))
				if c == '"' || c == '\\' || c < 0x20 {
					return i + j
				}
			}
		}
		i += 8
	}
	for i < n {
		c := *(*byte)(unsafe.Pointer(uintptr(p) + uintptr(i)))
		if c == '"' || c == '\\' || c < 0x20 {
			return i
		}
		i++
	}
	return n
}
