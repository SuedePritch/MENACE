package indexer

import (
	"os"
	"testing"
)

func BenchmarkParseLargeFile(b *testing.B) {
	data, err := os.ReadFile("../../testdata/large.ts")
	if err != nil {
		b.Fatalf("read large.ts: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseFile("../../testdata/large.ts", data)
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}
