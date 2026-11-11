package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericValuesAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/crossmodule/generic_values_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("../../tests/crossmodule/generic_values_probe.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":            "name: genericvalues\nversion: 0.0.1\n",
		"generic_values_donor.gb": string(donor),
		"app.gb":                  "module app;\n" + strings.Replace(string(probe), "func main(): void", "func verify(): void", 1) + "\nexport func main(list<string> args): int { verify(); return 0; }",
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
		`5`,
		`nestedExplicit expects list<T> return, got list<string>`,
		`Box.nested expects list<T> return, got list<string>`,
		`[<func top>, <func>, <func m>, <func s>, <func inner>, <class Thing>]`,
		`[<interface Local>, true, true, <interface Hidden>]`,
		`area`,
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
	out := filepath.Join(dir, "genericvalues")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
