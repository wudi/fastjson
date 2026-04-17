//go:build amd64 || arm64

package jsonx

import (
	"math/rand"
	"testing"
)

// TestWriteDigitsAsmParity fuzzes the amd64 asm kernel against the
// pure-Go reference for every cnt in [1,17] and a mix of significand
// values. Mismatches here are silent bugs in the JSON numeric output,
// so this gate is the correctness source-of-truth.
func TestWriteDigitsAsmParity(t *testing.T) {
	const N = 50_000
	r := rand.New(rand.NewSource(7))
	for cnt := 1; cnt <= 17; cnt++ {
		// Build the sig range: values in [10^(cnt-1), 10^cnt) so cnt is
		// actually the decimal-digit count.
		var lo uint64 = 1
		for i := 1; i < cnt; i++ {
			lo *= 10
		}
		hi := lo * 10
		// Always include the boundary values.
		trials := []uint64{lo, hi - 1}
		for i := 0; i < N; i++ {
			trials = append(trials, lo+r.Uint64()%(hi-lo))
		}
		for _, sig := range trials {
			for _, trim := range []bool{false, true} {
				var bufA, bufB [24]byte
				nA := writeDigitsStack(sig, &bufA, cnt, trim)
				nB := writeDigitsAsm(sig, &bufB[0], uint64(cnt), boolU64(trim), &digits100[0])
				if int(nB) != nA {
					t.Fatalf("cnt=%d sig=%d trim=%v: n mismatch asm=%d go=%d",
						cnt, sig, trim, nB, nA)
				}
				// Compare only the first n bytes; bytes beyond n are
				// allowed to differ because the Go impl may have
				// written them on a prior iteration (both bufs are
				// fresh here, but the contract is first-n-only).
				for i := 0; i < int(nB); i++ {
					if bufA[i] != bufB[i] {
						t.Fatalf("cnt=%d sig=%d trim=%v: byte[%d] asm=%q go=%q\nasm=%q\ngo =%q",
							cnt, sig, trim, i, bufB[i], bufA[i],
							string(bufB[:nB]), string(bufA[:nA]))
					}
				}
			}
		}
	}
}

func boolU64(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
