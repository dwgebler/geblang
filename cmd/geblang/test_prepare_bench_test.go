package main

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPrepareTestProgram(b *testing.B) {
	path := filepath.Join(b.TempDir(), "calc_test.gb")
	if err := os.WriteFile(path, []byte(passingTestFile), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		prepared, err := prepareTestProgram(path, nil, "", nil, "summary")
		if err != nil || prepared == nil {
			b.Fatalf("prepare: %v, %v", prepared, err)
		}
	}
}
