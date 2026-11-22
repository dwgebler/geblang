package typealias

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportedFromFileTracksContentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "money.gb")
	write := func(src string) {
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("module money;\nexport type Cash = decimal;\n")
	aliases, _, clean, err := ExportedFromFile(path, "money")
	if err != nil || !clean || aliases["Cash"] != "decimal" {
		t.Fatalf("first read: %v %v %v", aliases, clean, err)
	}
	again, _, _, _ := ExportedFromFile(path, "money")
	if again["Cash"] != "decimal" {
		t.Fatalf("cached read: %v", again)
	}
	write("module money;\nexport type Cash = int;\n")
	changed, _, _, _ := ExportedFromFile(path, "money")
	if changed["Cash"] != "int" {
		t.Fatalf("edited file still served stale aliases: %v", changed)
	}
	write("module money;\nexport type Cash = ;\n")
	if _, _, clean, err := ExportedFromFile(path, "money"); err != nil || clean {
		t.Fatalf("parse error not reported: clean=%v err=%v", clean, err)
	}
	if _, _, _, err := ExportedFromFile(filepath.Join(t.TempDir(), "missing.gb"), "missing"); err == nil {
		t.Fatal("missing file should fail")
	}
}

func TestFileLookupHonoursCleanRequirement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.gb")
	if err := os.WriteFile(path, []byte("module broken;\nexport type Id = int;\nfunc (\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolve := func(string) (string, bool) { return path, true }
	if target, ok := NewFileLookup(resolve, true, nil).Target("broken.Id"); ok {
		t.Fatalf("strict lookup used a file with parse errors: %q", target)
	}
	if target, ok := NewFileLookup(resolve, false, nil).Target("broken.Id"); !ok || target != "int" {
		t.Fatalf("lenient lookup: %q %v", target, ok)
	}
}
