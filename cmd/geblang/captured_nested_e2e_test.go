package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedNestedGenericsAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/crossmodule/captured_nested_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("../../tests/crossmodule/captured_nested_probe.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":             "name: capturednested\nversion: 0.0.1\n",
		"captured_nested_donor.gb": string(donor),
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
	want := strings.Join([]string{
		`["s"]`,
		`{"k": 1}`,
		`u`,
		`[5]`,
		`[7]`,
		`<closure> expects list<T> return, got list<string>`,
		`<closure> expects list<T> for parameter 'xs', got list (element at index 0 is string)`,
		`<closure> expects list<int> for parameter 'xs', got list (element at index 0 is string)`,
		`takeScores expects dict<string, int> for parameter 's', got dict (value for key a is string)`,
		`Base.take expects int[] for parameter 'xs', got list (element at index 0 is string)`,
		`dict<string, int>`,
	}, "\n") + "\n"
	for _, check := range []struct {
		name string
		args []string
	}{
		{name: "cold VM", args: []string{"app.gb"}},
		{name: "cached VM", args: []string{"app.gb"}},
		{name: "evaluator", args: []string{"--disable-vm", "app.gb"}},
	} {
		if got := run(bin, check.args...); got != want {
			t.Fatalf("%s: got %q, want %q", check.name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatalf("bytecode cache: %v", err)
	}
	out := filepath.Join(dir, "capturednested")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
