//go:build amd64

package fastjson

import (
	"math/rand"
	"testing"
)

// BenchmarkWriteDigitsAsm micro-benches the asm kernel alone on a
// canada-like distribution (17-digit significands dominate, sprinkled
// with 16/15-digit ones). Provides a direct signal for Phase 3 asm
// tuning without the surrounding encoder overhead.
func BenchmarkWriteDigitsAsm(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const N = 1024
	sigs := make([]uint64, N)
	cnts := make([]int, N)
	// Mirror canada distribution: ~70 % 16-digit, ~18 % 17-digit, ~10 % 8-digit.
	for i := 0; i < N; i++ {
		switch p := r.Intn(100); {
		case p < 70:
			cnts[i] = 16
			sigs[i] = 1_000_000_000_000_000 + r.Uint64()%9_000_000_000_000_000
		case p < 88:
			cnts[i] = 17
			sigs[i] = 10_000_000_000_000_000 + r.Uint64()%90_000_000_000_000_000
		case p < 97:
			cnts[i] = 8
			sigs[i] = 10_000_000 + r.Uint64()%90_000_000
		default:
			cnts[i] = 15
			sigs[i] = 100_000_000_000_000 + r.Uint64()%900_000_000_000_000
		}
	}
	var buf [24]byte
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i & (N - 1)
		writeDigitsAsm(sigs[idx], &buf[0], uint64(cnts[idx]), 0, &digits100[0])
	}
}

func BenchmarkWriteDigitsStack(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const N = 1024
	sigs := make([]uint64, N)
	cnts := make([]int, N)
	for i := 0; i < N; i++ {
		switch p := r.Intn(100); {
		case p < 70:
			cnts[i] = 16
			sigs[i] = 1_000_000_000_000_000 + r.Uint64()%9_000_000_000_000_000
		case p < 88:
			cnts[i] = 17
			sigs[i] = 10_000_000_000_000_000 + r.Uint64()%90_000_000_000_000_000
		case p < 97:
			cnts[i] = 8
			sigs[i] = 10_000_000 + r.Uint64()%90_000_000
		default:
			cnts[i] = 15
			sigs[i] = 100_000_000_000_000 + r.Uint64()%900_000_000_000_000
		}
	}
	var buf [24]byte
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i & (N - 1)
		writeDigitsStack(sigs[idx], &buf, cnts[idx], false)
	}
}
