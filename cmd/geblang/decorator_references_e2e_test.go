package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDecoratorReferencesAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	for _, name := range []string{"decorator_scope_donor.gb", "decorator_scope_probe.gb"} {
		source, err := os.ReadFile(filepath.Join("../../tests/reflect", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, source := range map[string]string{
		"geblang.yaml": "name: decorator_scopes\nversion: 0.0.0\n",
		"runner.gb":    "import decorator_scope_probe; decorator_scope_probe.main();\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(executable string, args ...string) {
		t.Helper()
		cmd := exec.Command(executable, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "decorator-scopes-ok\n" {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, output)
		}
	}

	for _, args := range [][]string{{"run", "runner.gb"}, {"run", "runner.gb"}, {"--disable-vm", "runner.gb"}} {
		run(bin, args...)
	}
	cache, err := filepath.Glob(filepath.Join(dir, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("cache files=%v, error=%v", cache, err)
	}
	executable := filepath.Join(dir, "decorator-scopes")
	build := exec.Command(bin, "build", "--entry", "decorator_scope_probe", "--out", executable, ".")
	build.Dir = dir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, name := range []string{"decorator_scope_donor.gb", "decorator_scope_probe.gb", "runner.gb"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	run(executable)
}
