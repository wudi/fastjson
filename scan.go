package fastjson

import (
	"unsafe"

	"github.com/klauspost/cpuid/v2"
)

// hasAVX512 is set once at init based on runtime CPU features. Goroutine-
// safe to read.
var hasAVX512 = cpuid.CPU.Supports(cpuid.AVX512F, cpuid.AVX512BW)

// scanString returns the offset of the first byte in p[0:n] that is '"',
// '\\', or < 0x20. Returns n if none found.
//
// When AVX-512BW is available on amd64, dispatches to an assembly kernel
// that scans 64 bytes per instruction (see scan_amd64.s). Otherwise falls
// back to an 8-byte SWAR scan identical to the one embedded in
// decodeString / writeString before this kernel existed.
func scanString(p unsafe.Pointer, n int) int {
	if hasAVX512 && n >= 64 {
		return scanStringAVX512((*byte)(p), n)
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
