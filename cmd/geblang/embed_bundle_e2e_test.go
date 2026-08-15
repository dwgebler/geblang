package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildEmbedsCompileTimeFiles confirms a built binary carries embed(...) file content with no source tree present.
func TestBuildEmbedsCompileTimeFiles(t *testing.T) {
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
	write("geblang.yaml", "name: app\nversion: 0.0.0\n")
	write("data/msg.txt", "EMBED-CONST-OK")
	write("data/payload.bin", "\xde\xad\xbe\xef\x00")
	write("app.gb", `module app;
import io;
export func main(): int {
    io.println(embed("data/msg.txt"));
    let b = embed("data/payload.bin", {binary: true});
    io.println(b.toHex());
    return 0;
}
`)
	out := filepath.Join(dir, "out", "app")
	cmd := exec.Command(bin, "build", "--entry", "app", "--out", out, ".")
	cmd.Dir = dir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, combined)
	}
	run := exec.Command(out)
	run.Dir = t.TempDir() // no source tree: only the bundle can satisfy the embed
	combined, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run built binary: %v\n%s", err, combined)
	}
	if !strings.Contains(string(combined), "EMBED-CONST-OK") {
		t.Errorf("built binary lost the embedded constant; output: %q", combined)
	}
	if !strings.Contains(string(combined), "deadbeef00") {
		t.Errorf("built binary lost the embedded binary constant; output: %q", combined)
	}
}
