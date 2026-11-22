package modules

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStdlibDirNamesRescansWhenDirectoryChanges(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.gb"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if names := stdlibDirNames(dir); !names["alpha"] || names["beta"] {
		t.Fatalf("first scan: %v", names)
	}
	if err := os.Mkdir(filepath.Join(dir, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dir, later, later); err != nil {
		t.Fatal(err)
	}
	if names := stdlibDirNames(dir); !names["alpha"] || !names["beta"] {
		t.Fatalf("rescan after change: %v", names)
	}
}
