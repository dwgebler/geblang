package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledStandardFlags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if buildOut, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, buildOut)
	}

	build := func(name, source string) string {
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
		write("geblang.yaml", "name: "+name+"\nversion: 1.2.3\n")
		write("app.gb", source)
		out := filepath.Join(dir, "out", name)
		if buildOut, err := exec.Command(bin, "build", "--entry", "app", "--out", out, dir).CombinedOutput(); err != nil {
			t.Fatalf("geblang build: %v\n%s", err, buildOut)
		}
		return out
	}
	runner := func(out string) func(args ...string) string {
		return func(args ...string) string {
			outBytes, err := exec.Command(out, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("run %v: %v\n%s", args, err, outBytes)
			}
			return string(outBytes)
		}
	}

	withArgs := runner(build("flagapp", `module app;
import io;
import sys;

export func main(list<string> args): int {
    io.println("args:${args}");
    let info = sys.appInfo();
    io.println("info:${info["name"]} ${info["version"]} ${info["runtime"] != ""}");
    return 0;
}
`))
	if got := withArgs("--geblang-version"); !strings.HasPrefix(got, "flagapp 1.2.3\n  geblang ") {
		t.Fatalf("--geblang-version output: %q", got)
	}
	help := withArgs("--geblang-help")
	for _, want := range []string{"flagapp 1.2.3", "--geblang-notices", "--geblang-version", "including --help and --version, go to the application"} {
		if !strings.Contains(help, want) {
			t.Fatalf("--geblang-help missing %q in %q", want, help)
		}
	}
	if got := withArgs("--geblang-notices"); !strings.Contains(got, "Third-Party Notices") {
		t.Fatalf("--geblang-notices output: %q", got)
	}
	for _, flag := range []string{"--help", "-h", "--version", "--notices"} {
		if got := withArgs(flag); !strings.Contains(got, `args:["`+flag+`"]`) {
			t.Fatalf("%s should reach the app: %q", flag, got)
		}
	}
	if got := withArgs("--", "--help"); !strings.Contains(got, `args:["--", "--help"]`) {
		t.Fatalf("-- should reach the app untouched: %q", got)
	}
	if got := withArgs("x", "--geblang-version"); !strings.Contains(got, `args:["x", "--geblang-version"]`) {
		t.Fatalf("runtime flags apply only as the first argument: %q", got)
	}
	if got := withArgs(); !strings.Contains(got, "info:flagapp 1.2.3 true") {
		t.Fatalf("sys.appInfo in a built binary: %q", got)
	}

	noArgs := runner(build("plainapp", `module app;
import io;

export func main(): int {
    io.println("ran");
    return 0;
}
`))
	for _, flag := range []string{"--version", "--geblang-version"} {
		if got := noArgs(flag); !strings.HasPrefix(got, "plainapp 1.2.3\n  geblang ") {
			t.Fatalf("%s output: %q", flag, got)
		}
	}
	for _, flag := range []string{"--help", "-h", "--geblang-help"} {
		if got := noArgs(flag); !strings.Contains(got, "accepted as short forms") {
			t.Fatalf("%s output: %q", flag, got)
		}
	}
	if got := noArgs("--notices"); !strings.Contains(got, "Third-Party Notices") {
		t.Fatalf("--notices output: %q", got)
	}
	if got := noArgs("x"); !strings.Contains(got, "ran") {
		t.Fatalf("plain args output: %q", got)
	}
}
