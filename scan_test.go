package fastjson

import (
	"strings"
	"testing"
	"unsafe"
)

func TestScanString(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"hello world, this is a clean string without problems yeah ok", 60},
		{"hello\"world", 5},
		{"hello\\world", 5},
		{"hello\nworld", 5},
		{strings.Repeat("a", 100), 100},
		{strings.Repeat("a", 100) + "\"tail", 100},
		{strings.Repeat("a", 63) + "\"tail", 63},
		{strings.Repeat("a", 64) + "\"tail", 64},
		{strings.Repeat("a", 65) + "\"tail", 65},
		{strings.Repeat("a", 128) + "\"tail", 128},
	}
	for i, c := range cases {
		n := len(c.s)
		p := unsafe.Pointer(unsafe.StringData(c.s))
		if got := scanString(p, n); got != c.want {
			t.Errorf("case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
		// also test SWAR fallback path
		if got := scanStringSWAR(p, n); got != c.want {
			t.Errorf("SWAR case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
	}
}

func BenchmarkScanStringAVX512(b *testing.B) {
	s := strings.Repeat("the quick brown fox ", 50) // 1000 bytes clean
	p := unsafe.Pointer(unsafe.StringData(s))
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scanStringAVX512((*byte)(p), n)
	}
}

func BenchmarkScanStringSWAR(b *testing.B) {
	s := strings.Repeat("the quick brown fox ", 50)
	p := unsafe.Pointer(unsafe.StringData(s))
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scanStringSWAR(p, n)
	}
}
