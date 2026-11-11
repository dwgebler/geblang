package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedGenericReturnsAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/crossmodule/captured_return_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("../../tests/crossmodule/captured_return_probe.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":             "name: capturedreturns\nversion: 0.0.1\n",
		"captured_return_donor.gb": string(donor),
		"app.gb":                   "module app;\n" + strings.Replace(string(probe), "func main(): void", "func verify(): void", 1) + "\nexport func main(list<string> args): int { verify(); return 0; }",
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
	want := "7\ntext\nnull\n[1, 2]\n{\"n\": 3}\ntrue\n7\nnested\nbase\n9\nchild\n[7, 7]\n9\nrejected\nrejected\n"
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
	out := filepath.Join(dir, "capturedreturns")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
