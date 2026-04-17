package bench

import (
	encjson "encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	sonic "github.com/bytedance/sonic"
)

// Extract every float64 value from canada.json into a flat slice so we can
// benchmark the float formatter in isolation. Phase 0 of the Schubfach
// plan: confirm the +12.6% canada-encode gap lives in the formatter and
// not in the surrounding append / iteration code.

var canadaFloats []float64

func init() {
	root, _ := os.Getwd()
	b, err := os.ReadFile(filepath.Join(root, "..", "testdata", "canada.json"))
	if err != nil {
		panic(err)
	}
	var v interface{}
	if err := encjson.Unmarshal(b, &v); err != nil {
		panic(err)
	}
	var walk func(x interface{})
	walk = func(x interface{}) {
		switch t := x.(type) {
		case float64:
			canadaFloats = append(canadaFloats, t)
		case []interface{}:
			for _, e := range t {
				walk(e)
			}
		case map[string]interface{}:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
}

func BenchmarkCanadaFloats_StrconvAppend(b *testing.B) {
	buf := make([]byte, 0, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range canadaFloats {
			buf = strconv.AppendFloat(buf[:0], v, 'f', -1, 64)
		}
	}
	_ = buf
}

// Sonic gives us no direct per-float formatter; Marshal([]float64) is the
// closest isolated test. The JSON array overhead is ~1 byte per element
// (the comma) plus a pair of brackets, which is negligible relative to
// ~17 digits of fp text.
func BenchmarkCanadaFloats_SonicMarshal(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sonic.Marshal(canadaFloats); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanadaFloats_StdlibMarshal(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encjson.Marshal(canadaFloats); err != nil {
			b.Fatal(err)
		}
	}
}

// Distribution report — run once and print the stats so we know what
// kind of floats we're actually formatting.
func TestCanadaFloatDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	n := len(canadaFloats)
	var min, max float64 = canadaFloats[0], canadaFloats[0]
	digitHist := make(map[int]int)
	for _, v := range canadaFloats {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		s := strconv.FormatFloat(v, 'g', -1, 64)
		d := 0
		for _, c := range s {
			if c >= '0' && c <= '9' {
				d++
			}
		}
		digitHist[d]++
	}
	t.Logf("count=%d  min=%v  max=%v", n, min, max)
	for i := 1; i <= 18; i++ {
		if digitHist[i] > 0 {
			t.Logf("  %d digits: %d (%.1f%%)", i, digitHist[i], 100*float64(digitHist[i])/float64(n))
		}
	}
}
