package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommand_ExplainsAmbiguityWithoutChangingSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app", "init"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.gb", "app/init.gb"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("module app;"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, strict := range []bool{false, true} {
		var out, diagnostic bytes.Buffer
		args := []string{"--from", root, "--explain"}
		wantCode := 0
		if strict {
			args = append(args, "--strict")
			wantCode = 1
		}
		args = append(args, "app")

		code := resolveCommand(args, &out, &diagnostic)

		if code != wantCode || !strings.Contains(out.String(), "selected: "+filepath.Join(root, "app.gb")) || !strings.Contains(out.String(), "shadowed: "+filepath.Join(root, "app/init.gb")) || !strings.Contains(diagnostic.String(), "multiple providers") {
			t.Fatalf("code %d, output %s, diagnostics %s", code, &out, &diagnostic)
		}
	}
}

func TestResolveCommand_NativeAndMissingModules(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		module string
		code   int
		output string
	}{
		{"geblang.io", 0, "builtin: geblang.io"},
		{"missing_workspace_fixture.module", 1, "missing:"},
	} {
		var out, diagnostic bytes.Buffer

		code := resolveCommand([]string{"--from", root, "--explain", tc.module}, &out, &diagnostic)

		if code != tc.code || !strings.Contains(out.String(), tc.output) {
			t.Fatalf("%s: code %d, output %s, diagnostics %s", tc.module, code, &out, &diagnostic)
		}
	}
}
