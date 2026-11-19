package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuiltBinaryIgnoresWorkingDirectoryAndKeepsPackageModuleNames builds an app whose dependency has a module
// declared with a stdlib's short name, then runs the binary from a directory that contains a stdlib.
func TestBuiltBinaryIgnoresWorkingDirectoryAndKeepsPackageModuleNames(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}
	dir := t.TempDir()
	write := func(root, rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "geblang.yaml", "name: app\nversion: 0.0.0\nsource: src\ndependencies:\n  webkit:\n    path: ./webkit\n")
	write(dir, "webkit/geblang.yaml", "name: webkit\nversion: 2.1.0\nsource: src\n")
	write(dir, "webkit/src/i18n.gb", "module i18n;\n\nexport func greeting(): string {\n    return \"hello\";\n}\n")
	write(dir, "webkit/src/webkit.gb", "module webkit;\n\nimport webkit.i18n as words;\n\nexport func greet(): string {\n    return words.greeting();\n}\n")
	write(dir, "src/app.gb", `module app;
import io;
import i18n;
import webkit;

export func main(): int {
    let c = i18n.catalog({"en": {"bye": "goodbye"}}, "en");
    io.println(webkit.greet() + ", " + c.text("bye"));
    return 0;
}
`)
	out := filepath.Join(dir, "out", "app")
	cmd := exec.Command(bin, "build", "--entry", "app", "--out", out, ".")
	cmd.Dir = dir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, combined)
	}

	versionOut, err := exec.Command(out, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, versionOut)
	}
	wantVersion := "app 0.0.0\n  geblang " + version + "\n  webkit 2.1.0\n"
	if string(versionOut) != wantVersion {
		t.Errorf("--version printed %q, want %q", versionOut, wantVersion)
	}

	elsewhere := t.TempDir()
	write(elsewhere, "stdlib/geblang.yaml", "name: stdlib\n")
	write(elsewhere, "stdlib/webkit.gb", "module webkit;\n")
	for _, runDir := range []string{t.TempDir(), elsewhere} {
		run := exec.Command(out)
		run.Dir = runDir
		combined, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("run built binary in %s: %v\n%s", runDir, err, combined)
		}
		if got := strings.TrimSpace(string(combined)); got != "hello, goodbye" {
			t.Errorf("built binary in %s printed %q, want %q", runDir, got, "hello, goodbye")
		}
	}
}
