package vm

import (
	"testing"
)

func BenchmarkResize(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := new(Memory)
		bytes := m.Resize(1024)
		putPool(bytes)
	}
}

func BenchmarkOldResize(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := new(Memory)
		if uint64(m.Len()) < 1024 {
			m.store = append(m.store, make([]byte, 1024-uint64(m.Len()))...)
		}
	}
}
