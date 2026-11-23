package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCatchFinallyAcrossCachedAndBuiltExecution(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	for name, source := range map[string]string{
		"geblang.yaml": "name: catch_finally\nversion: 0.0.0\n",
		"cleanup.gb": `module cleanup;
import io;
export class Base {
    func run(): void {
        try { throw ValueError("original"); }
        catch (Error error) { throw error; }
        finally { io.println("cleanup"); }
    }
}
`,
		"app.gb": `module app;
import cleanup;
import io;
class Child extends cleanup.Base { func own(): void { this.run(); } }
export func main(): void {
    let child = Child();
    for (run in [cleanup.Base().run, child.run, child.own]) {
        try { run(); } catch (ValueError error) { io.println(error.message); }
    }
}
`,
		"runner.gb": "import app; app.main();\n",
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
		if err != nil || string(output) != "cleanup\noriginal\ncleanup\noriginal\ncleanup\noriginal\n" {
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
	executable := filepath.Join(dir, "catch-finally")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = dir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, name := range []string{"cleanup.gb", "app.gb", "runner.gb"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	run(executable)
}
