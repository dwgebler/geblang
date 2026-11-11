package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const primFusionApp = `module app;
import io;
import prim_fusion_donor as donor;

let text = "héllo";
let table = {"a": 1};

class Sized {
    func Sized() {}
    func length(): int { return 42; }
}

export func main(list<string> args): int {
    let local = [1, 2];
    let custom = Sized();
    io.println(text.length());
    io.println(table.get("a"));
    io.println(local.length());
    io.println(custom.length());
    io.println(donor.sizes());
    return 0;
}
`

func TestPrimitiveMethodSlotsAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/core/prim_fusion_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":         "name: primfusion\nversion: 0.0.1\n",
		"prim_fusion_donor.gb": string(donor),
		"app.gb":               primFusionApp,
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
	want := strings.Join([]string{"5", "1", "2", "42", "3"}, "\n") + "\n"
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
	out := filepath.Join(dir, "primfusion")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}
