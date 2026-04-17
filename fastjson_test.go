package fastjson

import (
	encjson "encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testUser struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Age       int      `json:"age"`
	Active    bool     `json:"active"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}

func TestUnmarshalStruct(t *testing.T) {
	data := []byte(`{"id":42,"name":"Alice","email":"a@b","age":30,"active":true,"score":98.5,"tags":["x","y"],"created_at":"t"}`)
	var got testUser
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := testUser{42, "Alice", "a@b", 30, true, 98.5, []string{"x", "y"}, "t"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUnmarshalInterface(t *testing.T) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var ours, stdlib interface{}
			if err := Unmarshal(data, &ours); err != nil {
				t.Fatalf("fastjson err: %v", err)
			}
			if err := encjson.Unmarshal(data, &stdlib); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ours, stdlib) {
				t.Fatalf("%s: decode mismatch vs stdlib", name)
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var v interface{}
			if err := Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			out, err := Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			// re-parse and compare
			var rt interface{}
			if err := encjson.Unmarshal(out, &rt); err != nil {
				t.Fatalf("stdlib cannot parse our output: %v", err)
			}
			if !reflect.DeepEqual(v, rt) {
				t.Fatalf("roundtrip mismatch")
			}
		})
	}
}
