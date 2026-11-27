package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericParameterDiagnosticsAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	cases, err := os.ReadFile("../../tests/generics/parameter_diagnostic_cases.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml": "name: genericdiagnostics\nversion: 0.0.1\n",
		"cases.gb":     string(cases),
		"app.gb": `module app;
import io;
import cases;
export func main(list<string> args): int {
    for (text in cases.diagnostics()) { io.println(text); }
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
	want := strings.Join([]string{
		"TypeError: constrained expects int for parameter 'value', got string",
		"TypeError: constrained expects string for parameter 'value', got int",
		"TypeError: DiagnosticBox expects int for parameter 'value', got string",
		"TypeError: DiagnosticBox.accept expects int for parameter 'value', got string",
		"TypeError: DiagnosticChild.child expects int for parameter 'value', got string",
		"TypeError: DiagnosticBox.own expects string for parameter 'value', got int",
		"TypeError: <closure> expects int for parameter 'value', got string",
		"TypeError: variadic expects int for parameter 'values', got string",
		"TypeError: DiagnosticBox.own expects string for parameter 'value', got int",
		"TypeError: DiagnosticBox.tail expects int for parameter 'values', got string",
		"TypeError: DiagnosticBox.tail expects int for parameter 'values', got string",
		"TypeError: nested expects list<int> for parameter 'values', got list (element at index 1 is string)",
	}, "\n") + "\n"
	for _, args := range [][]string{{"-m", "app"}, {"-m", "app"}, {"--disable-vm", "-m", "app"}} {
		if got := run(bin, args...); got != want {
			t.Fatalf("%v: got %q, want %q", args, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatalf("bytecode cache: %v", err)
	}
	out := filepath.Join(dir, "genericdiagnostics")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for name := range files {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatal(err)
	}
	if got := run(out); got != want {
		t.Fatalf("source-independent binary: got %q, want %q", got, want)
	}
}
