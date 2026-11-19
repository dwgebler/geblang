package modules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundledStdlibIgnoresStdlibsAroundTheProcess(t *testing.T) {
	work := t.TempDir()
	local := filepath.Join(work, "stdlib")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "geblang.yaml"), []byte("name: stdlib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundled := filepath.Join(t.TempDir(), "stdlib")
	if err := os.MkdirAll(bundled, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	t.Setenv("GEBLANG_STDLIB", local)
	t.Cleanup(func() { bundledStdlib.Store(nil) })

	UseBundledStdlib(bundled)
	got := DefaultStdlibPaths()
	if len(got) != 1 || filepath.Clean(got[0]) != filepath.Clean(bundled) {
		t.Fatalf("stdlib paths = %v, want only %s", got, bundled)
	}
}

func TestBundledStdlibWithoutStdlibDirHasNoStdlibPaths(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { bundledStdlib.Store(nil) })
	UseBundledStdlib(filepath.Join(t.TempDir(), "missing"))
	if got := DefaultStdlibPaths(); len(got) != 0 {
		t.Fatalf("stdlib paths = %v, want none", got)
	}
}
