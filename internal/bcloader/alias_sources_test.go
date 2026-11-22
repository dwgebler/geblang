package bcloader

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bytecode"
)

func TestAliasSourcesFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "funds.gb")
	source := []byte("module funds;\nexport type Cash = int;\n")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}
	chunk := bytecode.Chunk{AliasSources: []bytecode.AliasSource{{Module: "funds", Path: path, Hash: sha256.Sum256(source)}, {Module: "later", Path: ""}}}
	if !AliasSourcesFresh(chunk, []string{dir}) {
		t.Fatal("unchanged sources reported stale")
	}
	if err := os.WriteFile(path, []byte("module funds;\nexport type Cash = decimal;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if AliasSourcesFresh(chunk, []string{dir}) {
		t.Fatal("edited source reported fresh")
	}
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "later.gb"), []byte("module later;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if AliasSourcesFresh(chunk, []string{dir}) {
		t.Fatal("a module that now resolves was reported fresh")
	}
}
