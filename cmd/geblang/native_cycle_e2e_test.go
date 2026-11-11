package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeContainerCyclesAcrossCachedAndBuiltExecution(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	project := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "tests", "crossmodule", "native_cycle_donor.gb"))
	if err != nil {
		t.Fatal(err)
	}
	callbacks, err := os.ReadFile(filepath.Join("..", "..", "tests", "crossmodule", "native_callback_donor.gb"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml":             "name: nativecycles\nversion: 0.0.0\n",
		"native_cycle_donor.gb":    string(source),
		"native_callback_donor.gb": string(callbacks),
		"app.gb": `module app;
import io;
import native_cycle_donor as donor;
import native_callback_donor as callbacks;
export func main(): void {
    io.println(donor.dictionaryCycle());
    io.println(donor.listCycle());
    io.println(donor.mixedCycleAndCallbacks());
    io.println(callbacks.verify());
}
`,
		"runner.gb": "import app;\napp.main();\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(path string, args ...string) {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = project
		output, err := cmd.CombinedOutput()
		want := "dict-ok\nlist-ok\nmixed-ok\ncallbacks-ok\n"
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
	executable := filepath.Join(project, "nativecycles")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run(executable)
}
