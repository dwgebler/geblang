package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCastTargetsAcrossCachedAndBuiltExecution(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	project := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: castapp\nversion: 0.0.0\n",
		"donor.gb": `module donor;
export type Flag = bool;
export func convert<int>(any value): any { return value as int; }
export class Base {
    any value;
    func Base(any value) { this.value = value as int; }
    func converted(): any { return this.value as int; }
}
`,
		"app.gb": `module app;
import donor;
import io;
from donor import Flag as int;
class Child extends donor.Base {
    func Child(any value) { parent(value); }
    func own(): any { return this.value as string; }
}
export func main(): void {
    io.println(5 as int);
    io.println(donor.convert<bool>(5));
    io.println(donor.convert<string>(5));
    io.println(donor.convert<bool>(0));
    io.println(donor.Base(7).converted());
    let child = Child(8);
    io.println(child.converted());
    io.println(child.own());
    let bound = child.converted;
    io.println(bound());
    io.println([1, 2].map(func(any n): any { return n as string; }));
    try { io.println("bad" as donor.Flag); } catch (Error e) { io.println(e.class); }
}
`,
		"runner.gb": "import app;\napp.main();\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := "true\ntrue\n5\nfalse\n7\n8\n8\n8\n[\"1\", \"2\"]\nValueError\n"
	run := func(path string, args ...string) {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = project
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != want {
			t.Fatalf("%v: %v\noutput %q, want %q", cmd.Args, err, output, want)
		}
	}

	for _, args := range [][]string{{"run", "runner.gb"}, {"run", "runner.gb"}, {"--disable-vm", "runner.gb"}} {
		run(bin, args...)
	}
	cache, err := filepath.Glob(filepath.Join(project, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("expected cached bytecode: %v, files %v", err, cache)
	}
	executable := filepath.Join(project, "cast-app")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run(executable)
}
