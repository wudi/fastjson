package fastjson

import (
	"strconv"
	"testing"
)

var canadaSamples = []float64{
	-65.613616999999977, 43.420273000000009, -65.619720000000029,
	43.418052999999986, -65.625, 43.421379000000059,
	-65.636123999999882, 43.449714999999969, -65.633056999999951,
	43.474709000000132, -65.642776999999915, 43.481938,
}

func BenchmarkFloatShortestF(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range canadaSamples {
			buf = strconv.AppendFloat(buf[:0], v, 'f', -1, 64)
		}
	}
	_ = buf
}

func BenchmarkFloatGPrec17(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range canadaSamples {
			buf = strconv.AppendFloat(buf[:0], v, 'g', 17, 64)
		}
	}
	_ = buf
}

func BenchmarkFloatEPrec17(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range canadaSamples {
			buf = strconv.AppendFloat(buf[:0], v, 'e', 17, 64)
		}
	}
	_ = buf
}
