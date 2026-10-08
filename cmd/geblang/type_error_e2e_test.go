package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTypeErrorClassAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: typeerrors\nversion: 0.0.1\n",
		"donor.gb": `module donor;
export func fail(): int {
    any left = 1;
    any right = "2";
    return left + right;
}
`,
		"app.gb": `module app;
import donor;
import io;
export func main(list<string> args): int {
    try { donor.fail(); }
    catch (TypeError e) { io.println(e.class + ": " + e.message); }
    return 0;
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	want := "TypeError: unsupported operands for +: int and string\n"
	for _, check := range []struct {
		name string
		args []string
	}{
		{name: "cold VM", args: []string{"app.gb"}},
		{name: "cached VM", args: []string{"app.gb"}},
		{name: "evaluator", args: []string{"--disable-vm", "app.gb"}},
	} {
		name, got := check.name, run(bin, check.args...)
		if got != want {
			t.Fatalf("%s: got %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatalf("bytecode cache: %v", err)
	}
	out := filepath.Join(dir, "typeerrors")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
