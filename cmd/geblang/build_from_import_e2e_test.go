package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildBundlesModulesReachedOnlyByFromImport confirms a module imported only with `from m import f` ships in the binary, including one supplying decorators.
func TestBuildBundlesModulesReachedOnlyByFromImport(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
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
	write("geblang.yaml", "name: app\nversion: 0.0.0\n")
	write("aspects.gb", `module aspects;
export func shout(callable next): callable {
    return func(any ...args): any { return next(...args).upper(); };
}
export func trimmed(string value): string { return value.trim(); }
`)
	write("greeter.gb", `module greeter;
from aspects import shout, trimmed;
export class Greeter {
    @trimmed
    string name;
    func Greeter(string name) { this.name = name; }
    @shout
    func greet(): string { return "hello " + this.name; }
}
`)
	write("app.gb", `module app;
import io;
import greeter;
export func main(): int {
    io.println(greeter.Greeter("  ada  ").greet());
    return 0;
}
`)
	out := filepath.Join(dir, "out", "app")
	cmd := exec.Command(bin, "build", "--entry", "app", "--out", out, ".")
	cmd.Dir = dir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, combined)
	}
	run := exec.Command(out)
	run.Dir = t.TempDir()
	combined, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run built binary: %v\n%s", err, combined)
	}
	if got := strings.TrimSpace(string(combined)); got != "HELLO ADA" {
		t.Errorf("built binary output = %q, want %q", got, "HELLO ADA")
	}
}
