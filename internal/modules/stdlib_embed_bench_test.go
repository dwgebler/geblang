package modules

import "testing"

func BenchmarkEmbeddedStdlibResolution(b *testing.B) {
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	if path := ensureEmbeddedStdlib(); path == "" {
		b.Fatal("could not extract stdlib")
	}
	b.ReportAllocs()
	for b.Loop() {
		if path := ensureEmbeddedStdlib(); path == "" {
			b.Fatal("could not resolve stdlib")
		}
	}
}
