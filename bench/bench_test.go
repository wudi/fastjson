package bench

import (
	encjson "encoding/json"
	"os"
	"path/filepath"
	"testing"

	sonic "github.com/bytedance/sonic"
	gojson "github.com/goccy/go-json"
	fastjson "github.com/wudi/fastjson"
)

// Corpus files live under ../testdata. We load all once at init time.
var corpus = map[string][]byte{}

func init() {
	files := []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"}
	root, _ := os.Getwd()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, "..", "testdata", f))
		if err != nil {
			panic(err)
		}
		corpus[f] = b
	}
}

func runDecode(b *testing.B, data []byte, dec func(data []byte) error) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := dec(data); err != nil {
			b.Fatal(err)
		}
	}
}

// ---- Interface{} decoding (generic map path) ----
func BenchmarkDecodeInterface_Stdlib(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v interface{}
				return encjson.Unmarshal(d, &v)
			})
		})
	}
}

func BenchmarkDecodeInterface_Goccy(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v interface{}
				return gojson.Unmarshal(d, &v)
			})
		})
	}
}

func BenchmarkDecodeInterface_Sonic(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v interface{}
				return sonic.Unmarshal(d, &v)
			})
		})
	}
}

func BenchmarkDecodeInterface_Fastjson(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v interface{}
				return fastjson.Unmarshal(d, &v)
			})
		})
	}
}

// ---- Struct decoding (small.json → SmallUser) ----
func BenchmarkDecodeStruct_Stdlib(b *testing.B) {
	data := corpus["small.json"]
	runDecode(b, data, func(d []byte) error {
		var v SmallUser
		return encjson.Unmarshal(d, &v)
	})
}
func BenchmarkDecodeStruct_Goccy(b *testing.B) {
	data := corpus["small.json"]
	runDecode(b, data, func(d []byte) error {
		var v SmallUser
		return gojson.Unmarshal(d, &v)
	})
}
func BenchmarkDecodeStruct_Sonic(b *testing.B) {
	data := corpus["small.json"]
	runDecode(b, data, func(d []byte) error {
		var v SmallUser
		return sonic.Unmarshal(d, &v)
	})
}
func BenchmarkDecodeStruct_Fastjson(b *testing.B) {
	data := corpus["small.json"]
	runDecode(b, data, func(d []byte) error {
		var v SmallUser
		return fastjson.Unmarshal(d, &v)
	})
}

// ---- Encoding ----
func runEncode(b *testing.B, v interface{}, enc func(v interface{}) ([]byte, error)) {
	// probe size once
	data, err := enc(v)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc(v); err != nil {
			b.Fatal(err)
		}
	}
}

func loadInterface(name string) interface{} {
	var v interface{}
	if err := encjson.Unmarshal(corpus[name], &v); err != nil {
		panic(err)
	}
	return v
}

func BenchmarkEncodeInterface_Stdlib(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		v := loadInterface(name)
		b.Run(name, func(b *testing.B) {
			runEncode(b, v, func(v interface{}) ([]byte, error) { return encjson.Marshal(v) })
		})
	}
}
func BenchmarkEncodeInterface_Goccy(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		v := loadInterface(name)
		b.Run(name, func(b *testing.B) {
			runEncode(b, v, func(v interface{}) ([]byte, error) { return gojson.Marshal(v) })
		})
	}
}
func BenchmarkEncodeInterface_Sonic(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		v := loadInterface(name)
		b.Run(name, func(b *testing.B) {
			runEncode(b, v, func(v interface{}) ([]byte, error) { return sonic.Marshal(v) })
		})
	}
}
func BenchmarkEncodeInterface_Fastjson(b *testing.B) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		v := loadInterface(name)
		b.Run(name, func(b *testing.B) {
			runEncode(b, v, func(v interface{}) ([]byte, error) { return fastjson.Marshal(v) })
		})
	}
}
