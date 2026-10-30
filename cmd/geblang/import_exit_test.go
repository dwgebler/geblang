package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestImportTimeExitCodeMatchesAcrossBackends(t *testing.T) {
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	if err := os.WriteFile(filepath.Join(dir, "exiter.gb"), []byte("module exiter;\nimport sys;\ninit {\n    sys.exit(3);\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte("import io;\nimport exiter;\nio.println(\"after\");\n")
	for _, mode := range []executionMode{executionEvaluatorOnly, executionVMStrict} {
		program, err := parseAndAnalyze(sourcePath, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		code, err := runScript(sourcePath, nil, source, program, mode, nil, &out, nil)
		if err != nil || code != 3 || out.String() != "" {
			t.Fatalf("mode %d: code %d, err %v, out %q", mode, code, err, out.String())
		}
	}
}
