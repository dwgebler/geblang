package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEntryDecoratorReferencesAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	for _, name := range []string{"entry_decorator_inspector.gb", "entry_decorator_probe.gb"} {
		source, err := os.ReadFile(filepath.Join("../../tests/reflect", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte("name: entry_decorators\nversion: 0.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(executable, expected string, args ...string) {
		t.Helper()
		cmd := exec.Command(executable, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != expected {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, output)
		}
	}

	for _, args := range [][]string{{"run", "entry_decorator_probe.gb"}, {"run", "entry_decorator_probe.gb"}, {"--disable-vm", "entry_decorator_probe.gb"}} {
		run(bin, "entry-decorators-ok\n", args...)
	}
	executable := filepath.Join(dir, "entry-decorators")
	cmd := exec.Command(bin, "build", "--entry", "entry_decorator_probe", "--out", executable, ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, name := range []string{"entry_decorator_inspector.gb", "entry_decorator_probe.gb"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	run(executable, "entry-decorators-ok\n")
}
