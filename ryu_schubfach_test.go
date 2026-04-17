package jsonx

import (
	"math"
	"math/rand"
	"strconv"
	"testing"
)

// TestSchubfachSeedCorpus exercises the pathological-case corpus spelled
// out in the plan's risk register. Each value is round-tripped: format
// with Schubfach, parse with strconv, assert the resulting float64 has
// the same bit pattern as the original. This is weaker than bit-identity
// with stdlib but it's what matters for JSON (as long as parsing our
// output gives back the same float, we're correct).
func TestSchubfachSeedCorpus(t *testing.T) {
	seeds := []float64{
		0,
		-0.0,
		1,
		-1,
		0.5,
		-0.5,
		0.1,
		0.2,
		0.3,
		3.14,
		3.141592653589793,
		2.718281828459045,
		math.SmallestNonzeroFloat64,
		math.MaxFloat64,
		math.Nextafter(1.0, 2.0),  // 1 + 2^-52
		math.Nextafter(1.0, 0.0),  // 1 - 2^-53
		1e-300,
		1e300,
		1e-10,
		1e10,
		// explicit halfway-round cases:
		8.589934593e9,
		2.5,
		3.5,
		0.5,
		// canada.json extremes
		-141.002991,
		83.11387600000012,
		-65.613616999999977,
	}
	// Powers of ten
	for k := -20; k <= 20; k++ {
		seeds = append(seeds, math.Pow10(k))
	}
	// 0.5^k (binary fractions — Ryu shortest-search corner)
	for k := 1; k <= 53; k++ {
		seeds = append(seeds, math.Ldexp(1.0, -k))
	}

	for _, v := range seeds {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out := schubfachAppendFloat64(nil, v)
		got, err := strconv.ParseFloat(string(out), 64)
		if err != nil {
			t.Errorf("value=%v: our output %q did not parse: %v", v, out, err)
			continue
		}
		if math.Float64bits(got) != math.Float64bits(v) {
			t.Errorf("roundtrip mismatch for %v: our=%q parsed=%v (bits %#x vs %#x)",
				v, out, got, math.Float64bits(got), math.Float64bits(v))
		}
	}
}

// TestSchubfachCanadaRoundtrip: every canada.json float must round-trip
// bit-exactly. This is the most important correctness check for our
// actual workload.
func TestSchubfachCanadaRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	// Reuse the same extraction as the benchmark file.
	b, err := readTestdata("canada.json")
	if err != nil {
		t.Fatal(err)
	}
	var v interface{}
	if err := Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	var floats []float64
	var walk func(x interface{})
	walk = func(x interface{}) {
		switch t := x.(type) {
		case float64:
			floats = append(floats, t)
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
	t.Logf("testing %d canada floats", len(floats))

	var mismatches int
	for _, f := range floats {
		out := schubfachAppendFloat64(nil, f)
		got, err := strconv.ParseFloat(string(out), 64)
		if err != nil {
			t.Errorf("unparseable output for %v: %q", f, out)
			mismatches++
			if mismatches > 10 {
				t.Fatal("too many errors")
			}
			continue
		}
		if math.Float64bits(got) != math.Float64bits(f) {
			t.Errorf("roundtrip mismatch: %v → %q → %v", f, out, got)
			mismatches++
			if mismatches > 10 {
				t.Fatal("too many errors")
			}
		}
	}
}

// TestSchubfachRandomFloat64 performs differential fuzz against
// strconv.ParseFloat roundtrip over a random sample.
func TestSchubfachRandomFloat64(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := rand.New(rand.NewSource(42))
	const n = 1_000_000
	var mismatches int
	for i := 0; i < n; i++ {
		bits := r.Uint64()
		f := math.Float64frombits(bits)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		out := schubfachAppendFloat64(nil, f)
		got, err := strconv.ParseFloat(string(out), 64)
		if err != nil {
			t.Errorf("[%d] unparseable output for bits %#x (%v): %q", i, bits, f, out)
			mismatches++
			if mismatches > 20 {
				t.Fatal("too many errors")
			}
			continue
		}
		if math.Float64bits(got) != math.Float64bits(f) {
			t.Errorf("[%d] bits %#x (%v): our=%q parsed=%v (bits %#x)",
				i, bits, f, out, got, math.Float64bits(got))
			mismatches++
			if mismatches > 20 {
				t.Fatal("too many errors")
			}
		}
	}
}

// readTestdata is a small indirection so the test doesn't need filepath
// dependencies every time it's invoked.
func readTestdata(name string) ([]byte, error) {
	return readTestdataImpl(name)
}
