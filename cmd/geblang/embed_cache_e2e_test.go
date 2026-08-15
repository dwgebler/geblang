package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEmbedCacheRevalidation confirms a .gbc cache hit is rejected once an embedded file changes on disk.
func TestEmbedCacheRevalidation(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("data/msg.txt", "FIRST")
	write("main.gb", "import io;\nio.println(embed(\"data/msg.txt\"));\n")

	run := func() string {
		cmd := exec.Command(bin, "main.gb")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		return string(out)
	}
	if got := run(); got != "FIRST\n" {
		t.Fatalf("first run: %q", got)
	}
	if got := run(); got != "FIRST\n" { // cache-hit lane
		t.Fatalf("second run (cached): %q", got)
	}
	write("data/msg.txt", "SECOND")
	if got := run(); got != "SECOND\n" { // revalidation must force recompile
		t.Fatalf("run after embed change: %q", got)
	}
}
