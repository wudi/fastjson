package jsonx

import (
	"strconv"
	"testing"
)

var floatSamples = []string{
	"-79.55324172975964",
	"0",
	"1.2345678901234567",
	"-123456",
	"1e-5",
	"2.718281828459045",
}

func BenchmarkScanNumberOurs(b *testing.B) {
	datas := make([][]byte, len(floatSamples))
	for i, s := range floatSamples {
		datas[i] = append([]byte(s), ' ')
	}
	b.ReportAllocs()
	b.ResetTimer()
	d := &decoder{}
	for i := 0; i < b.N; i++ {
		for _, data := range datas {
			d.reset(data)
			if _, err := d.scanNumber(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkScanNumberStdlib(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range floatSamples {
			if _, err := strconv.ParseFloat(s, 64); err != nil {
				b.Fatal(err)
			}
		}
	}
}
