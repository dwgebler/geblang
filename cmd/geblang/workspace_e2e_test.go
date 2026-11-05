package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCLI_ExecutesChecksCachesAndBuildsFromRootAndMember(t *testing.T) {
	bin := buildCMBinary(t)
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "examples", "project-workspace")
	err := filepath.WalkDir(fixture, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".geblang-cache" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, source, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		return string(output)
	}

	for _, dir := range []string{root, filepath.Join(root, "apps/report")} {
		run(dir, "install", "--workspace")
		for _, mode := range []string{"--vm-strict", "--vm-strict", "--disable-vm"} {
			if got := run(dir, mode, "-m", "report_app.main"); got != string(want) {
				t.Fatalf("%s %s: got %q, want %q", dir, mode, got, want)
			}
		}
		explanation := run(dir, "resolve", "--explain", "project_tasks.model")
		if !strings.Contains(explanation, "packages/tasks/geblang.yaml") || !strings.Contains(explanation, "selected:") {
			t.Fatalf("missing member provenance: %s", explanation)
		}
	}
	run(root, "check", "--strict", "apps/report/src")
	run(root, "check", "--strict", "packages/tasks/src")
	run(root, "check", "--strict", "external/labels/lib")
	app := filepath.Join(t.TempDir(), "workspace-app")
	run(root, "build", "--entry", "report_app.main", "--out", app)
	cmd := exec.Command(app)
	cmd.Dir = t.TempDir()
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != string(want) {
		t.Fatalf("standalone build: %v, got %q, want %q", err, output, want)
	}
}
