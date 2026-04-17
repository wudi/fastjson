//go:build arm64

package jsonx

import (
	"strings"
	"testing"
	"unsafe"
)

// TestScanStringNEON validates the NEON kernel against the pure-Go
// SWAR implementation on a spread of inputs: clean long strings,
// strings with a match at every possible offset, and boundary
// lengths (63 / 64 / 65 / 128).
func TestScanStringNEON(t *testing.T) {
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
		{strings.Repeat("a", 15) + "\"tail", 15},
		{strings.Repeat("a", 16) + "\"tail", 16},
		{strings.Repeat("a", 17) + "\"tail", 17},
		{strings.Repeat("a", 31) + "\"tail", 31},
		{strings.Repeat("a", 32) + "\"tail", 32},
		{strings.Repeat("a", 63) + "\"tail", 63},
		{strings.Repeat("a", 64) + "\"tail", 64},
		{strings.Repeat("a", 65) + "\"tail", 65},
		{strings.Repeat("a", 128) + "\"tail", 128},
	}
	for i, c := range cases {
		n := len(c.s)
		p := unsafe.Pointer(unsafe.StringData(c.s))
		if got := scanStringSIMD((*byte)(p), n); got != c.want {
			t.Errorf("NEON case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
		if got := scanStringSWAR(p, n); got != c.want {
			t.Errorf("SWAR case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
	}
}

// TestSkipWSNEON validates the NEON whitespace skipper.
func TestSkipWSNEON(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"   \t\n\rhello", 6},
		{"", 0},
		{"x", 0},
		{"   ", 3},
		{strings.Repeat(" ", 100) + "x", 100},
		{strings.Repeat(" ", 15) + "hello", 15},
		{strings.Repeat(" ", 16) + "hello", 16},
		{strings.Repeat(" ", 17) + "hello", 17},
		{strings.Repeat(" ", 64) + "hello", 64},
		{strings.Repeat(" ", 63) + "hello", 63},
		{strings.Repeat(" ", 65) + "hello", 65},
		{strings.Repeat("\t\n\r ", 32) + "x", 128},
	}
	for i, c := range cases {
		n := len(c.s)
		var p *byte
		if n > 0 {
			p = unsafe.StringData(c.s)
		}
		if got := skipWSSIMD(p, n); got != c.want {
			t.Errorf("case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
	}
}
