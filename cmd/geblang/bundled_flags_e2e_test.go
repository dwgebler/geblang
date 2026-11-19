package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledStandardFlags builds a tiny app and exercises the
// first-argument standard flags every built binary carries: --help,
// --version, --notices, and the `--` passthrough escape.
func TestBundledStandardFlags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if buildOut, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, buildOut)
	}

	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: flagapp\nversion: 1.2.3\n")
	write("app.gb", `module app;
import io;

export func main(list<string> args): int {
    io.println("args:${args.length()}");
    return 0;
}
`)

	out := filepath.Join(dir, "out", "flagapp")
	if buildOut, err := exec.Command(bin, "build", "--entry", "app", "--out", out, dir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build: %v\n%s", err, buildOut)
	}

	run := func(args ...string) string {
		cmd := exec.Command(out, args...)
		outBytes, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, outBytes)
		}
		return string(outBytes)
	}

	if got := run("--version"); !strings.HasPrefix(got, "flagapp 1.2.3\n  geblang ") {
		t.Fatalf("--version output: %q", got)
	}
	help := run("--help")
	for _, want := range []string{"flagapp 1.2.3", "--notices", "--version", "pass everything after it"} {
		if !strings.Contains(help, want) {
			t.Fatalf("--help missing %q in %q", want, help)
		}
	}
	if got := run("--notices"); !strings.Contains(got, "Third-Party Notices") {
		t.Fatalf("--notices output: %q", got)
	}
	// `--` disables interception: --help reaches the app as an argument.
	if got := run("--", "--help"); !strings.Contains(got, "args:1") {
		t.Fatalf("passthrough output: %q", got)
	}
	// Non-flag args flow through untouched.
	if got := run("x", "y"); !strings.Contains(got, "args:2") {
		t.Fatalf("plain args output: %q", got)
	}
}
