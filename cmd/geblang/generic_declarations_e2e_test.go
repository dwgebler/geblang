package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const genericDeclApp = `module app;
import io;
import generic_decl_donor as donor;

func raw(any v): any { return v; }
func nested<T>(T x): int {
    func inner(any v): list<T> { list<T> r = raw(v); return r; }
    return inner([x]).length();
}

export func main(list<string> args): int {
    io.println(donor.wrap<int>(1));
    io.println(donor.Holder().closureRet(["s"]));
    io.println(nested(1));
    try { donor.Holder<int>().closureRet(["s"]); } catch (TypeError e) { io.println(e.message); }
    try { int[] w = raw(["a"]); } catch (TypeError e) { io.println(e.message); }
    return 0;
}
`

func TestGenericDeclarationsAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/types/generic_decl_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":          "name: genericdecl\nversion: 0.0.1\n",
		"generic_decl_donor.gb": string(donor),
		"app.gb":                genericDeclApp,
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
		`[1]`,
		`["s"]`,
		`1`,
		`<closure> expects list<T> return, got list<string>`,
		`type error: cannot assign list to int[] (element at index 0 is string)`,
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
	out := filepath.Join(dir, "genericdecl")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
