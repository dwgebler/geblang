package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDictionaryViewsAcrossCachedAndBuiltExecution(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	project := t.TempDir()
	for _, name := range []string{"dict_view_donor.gb", "dict_view_probe.gb"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "tests", "crossmodule", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, name), source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, source := range map[string]string{
		"geblang.yaml": "name: dictviews\nversion: 0.0.0\n",
		"runner.gb":    "import dict_view_probe;\ndict_view_probe.main();\n",
	} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(path string, args ...string) {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = project
		output, err := cmd.CombinedOutput()
		want := "views-ok\nint\nany\n{}\n[1, 1]\n7\n1\n"
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
	executable := filepath.Join(project, "dictviews")
	build := exec.Command(bin, "build", "--entry", "dict_view_probe", "--out", executable, ".")
	build.Dir = project
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run(executable)
}
