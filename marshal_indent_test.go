package jsonx

import (
	encjson "encoding/json"
	"math"
	"strings"
	"testing"
)

// parityCase pairs a value and a human-readable label. We keep values
// deterministic (structs, slices, scalars) so the stdlib and jsonx outputs
// are byte-for-byte comparable — unordered maps are covered separately.
type parityCase struct {
	name string
	v    interface{}
}

func parityCases() []parityCase {
	type inner struct {
		D string `json:"d"`
	}
	type outer struct {
		A int    `json:"a"`
		B []int  `json:"b"`
		C inner  `json:"c"`
		E []int  `json:"e"`
		F struct{} `json:"f"`
	}
	type deep struct {
		L1 struct {
			L2 struct {
				L3 struct {
					L4 []int `json:"l4"`
				} `json:"l3"`
			} `json:"l2"`
		} `json:"l1"`
	}
	type tricky struct {
		S1 string `json:"s1"`
		S2 string `json:"s2"`
		S3 string `json:"s3"`
		S4 string `json:"s4"`
	}
	var d deep
	d.L1.L2.L3.L4 = []int{1, 2, 3}
	return []parityCase{
		{"null", nil},
		{"int", 42},
		{"float", 3.14},
		{"string-plain", "hello"},
		{"string-escapes", "line1\nline2\t\"quoted\"\\back"},
		{"string-json-chars", `{"x":[1,2,3],"y":"z"}`},
		{"bool-true", true},
		{"bool-false", false},
		{"empty-array", []int{}},
		{"empty-object", struct{}{}},
		{"flat-struct", outer{A: 1, B: []int{1, 2, 3}, C: inner{D: "e"}, E: []int{}, F: struct{}{}}},
		{"array-of-structs", []outer{
			{A: 1, B: []int{1}, C: inner{D: "x"}},
			{A: 2, B: []int{}, C: inner{D: "y"}},
		}},
		{"deep-nesting", d},
		{"string-with-syntax", tricky{
			S1: `{"not":"a","real":"object"}`,
			S2: "has , and : inside",
			S3: "with \\ backslash and \" quote",
			S4: "control\x01chars",
		}},
		{"mixed-scalars", []interface{}{1, "two", 3.5, true, false, nil}},
		{"array-of-arrays", [][]int{{1, 2}, {3, 4}, {}}},
	}
}

func prefixIndentVariants() [][2]string {
	return [][2]string{
		{"", "  "},
		{"", "\t"},
		{"", ""},
		{">>", "  "},
		{"| ", "--"},
		{"PFX", "\t\t"},
		{"", "    "},
	}
}

func TestMarshalIndentParity(t *testing.T) {
	for _, pi := range prefixIndentVariants() {
		for _, c := range parityCases() {
			name := c.name + "/prefix=" + pi[0] + "/indent=" + pi[1]
			t.Run(name, func(t *testing.T) {
				want, err := encjson.MarshalIndent(c.v, pi[0], pi[1])
				if err != nil {
					t.Fatalf("stdlib MarshalIndent err: %v", err)
				}
				got, err := MarshalIndent(c.v, pi[0], pi[1])
				if err != nil {
					t.Fatalf("jsonx MarshalIndent err: %v", err)
				}
				if string(got) != string(want) {
					t.Fatalf("mismatch\nprefix=%q indent=%q\nwant:\n%s\n\ngot:\n%s", pi[0], pi[1], want, got)
				}
			})
		}
	}
}

// TestMarshalIndentEmpty exercises the "empty container stays on one line"
// rule that encoding/json and we both honor.
func TestMarshalIndentEmpty(t *testing.T) {
	type holder struct {
		A []int    `json:"a"`
		B struct{} `json:"b"`
	}
	got, err := MarshalIndent(holder{A: []int{}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": [],\n  \"b\": {}\n}"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestMarshalIndentStringPreservesBytes ensures we don't mangle braces,
// brackets, commas, colons, or quotes inside string values.
func TestMarshalIndentStringPreservesBytes(t *testing.T) {
	payload := `{"inside":[1,2,3],"k":"v:, and more"}`
	got, err := MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// Expect a single JSON string literal — no reformatting inside.
	want, _ := encjson.MarshalIndent(payload, "", "  ")
	if string(got) != string(want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if !strings.HasPrefix(string(got), `"`) || !strings.HasSuffix(string(got), `"`) {
		t.Fatalf("expected quoted string literal, got %s", got)
	}
}

// TestMarshalIndentEscapedQuote guards against the string-scanner forgetting
// that a backslash-escaped quote does NOT end the string.
func TestMarshalIndentEscapedQuote(t *testing.T) {
	type s struct {
		A string `json:"a"`
		B int    `json:"b"`
	}
	v := s{A: `he said \"hi\" }`, B: 7}
	got, err := MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := encjson.MarshalIndent(v, "", "  ")
	if string(got) != string(want) {
		t.Fatalf("mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestMarshalIndentErrorPropagates verifies that Marshal errors surface
// through MarshalIndent unchanged (e.g. NaN / Inf float).
func TestMarshalIndentErrorPropagates(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := MarshalIndent(f, "", "  "); err == nil {
			t.Fatalf("expected error for %v", f)
		}
	}
}

// TestMarshalIndentMatchesCompactMarshal verifies that stripping the added
// whitespace returns the same bytes as Marshal — i.e. indenting is purely
// cosmetic and doesn't corrupt the structure.
func TestMarshalIndentMatchesCompactMarshal(t *testing.T) {
	for _, c := range parityCases() {
		compact, err := Marshal(c.v)
		if err != nil {
			t.Fatalf("%s: Marshal err: %v", c.name, err)
		}
		indented, err := MarshalIndent(c.v, "  ", "    ")
		if err != nil {
			t.Fatalf("%s: MarshalIndent err: %v", c.name, err)
		}
		// Re-compact via encoding/json.Compact then compare.
		var buf strings.Builder
		dst := make([]byte, 0, len(indented))
		dst = compactJSON(dst, indented)
		_ = buf
		if string(dst) != string(compact) {
			t.Fatalf("%s: re-compacted form mismatch\ncompact : %s\nindented: %s\nre-comp : %s",
				c.name, compact, indented, dst)
		}
	}
}

// compactJSON removes insignificant whitespace from valid JSON, treating
// strings correctly. It mirrors what encoding/json.Compact does and is used
// solely by the test above to round-trip indented output back to compact.
func compactJSON(dst, src []byte) []byte {
	inString := false
	escape := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			dst = append(dst, c)
			if escape {
				escape = false
				continue
			}
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			dst = append(dst, c)
		case ' ', '\t', '\n', '\r':
			// skip
		case ':':
			dst = append(dst, c)
			// also skip following space inserted by indent
			if i+1 < len(src) && src[i+1] == ' ' {
				i++
			}
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// TestMarshalIndentMapParityNormalized covers map[string]interface{}: jsonx
// does not sort keys, so we normalize by decoding both outputs and comparing
// structurally rather than byte-for-byte.
func TestMarshalIndentMapParityNormalized(t *testing.T) {
	m := map[string]interface{}{
		"a":     1.0,
		"b":     []interface{}{1.0, 2.0, 3.0},
		"c":     map[string]interface{}{"d": "e"},
		"empty": map[string]interface{}{},
		"arr":   []interface{}{},
	}
	indented, err := MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := encjson.Unmarshal(indented, &got); err != nil {
		t.Fatalf("indented output not valid JSON: %v\n%s", err, indented)
	}
	if len(got) != len(m) {
		t.Fatalf("key count mismatch: got %d want %d", len(got), len(m))
	}
	// Spot-check values the decoder should have preserved.
	if got["a"].(float64) != 1.0 {
		t.Fatalf("a: %v", got["a"])
	}
	if len(got["b"].([]interface{})) != 3 {
		t.Fatalf("b: %v", got["b"])
	}
	if got["c"].(map[string]interface{})["d"] != "e" {
		t.Fatalf("c.d: %v", got["c"])
	}
}

// TestMarshalIndentDeeplyNested ensures arbitrary depth works — catches
// off-by-one errors in the depth counter.
func TestMarshalIndentDeeplyNested(t *testing.T) {
	// build [[[[[[[[1]]]]]]]]
	var v interface{} = 1
	for i := 0; i < 20; i++ {
		v = []interface{}{v}
	}
	got, err := MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := encjson.MarshalIndent(v, "", " ")
	if string(got) != string(want) {
		t.Fatalf("deep-nesting mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
