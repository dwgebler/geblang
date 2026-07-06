package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A cross-module destructor fires at del/exit identically across the evaluator, cold VM, cached .gbc VM, and a built binary; the built path also proves DestructorIndex survives chunk serialization.
func TestCrossModuleDestructorAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: destr\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "donor.gb"), []byte(`module destr.donor;
import io;
export class Resource {
    string name;
    func Resource(string n) { this.name = n; io.println("construct ${n}"); }
    func tag(): string { return this.name; }
    func ~Resource() { io.println("destruct ${this.name}"); }
}
export func make(string n): Resource { return Resource(n); }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "main.gb"), []byte(`module destr.main;
import io;
import destr.donor as d;
export func main(list<string> args): void {
    d.Resource a = d.Resource("A");
    d.Resource b = d.make("B");
    io.println("before del");
    del a;
    io.println(b.tag());
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(srcDir, "main.gb")

	run := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	const want = "construct A\nconstruct B\nbefore del\ndestruct A\nB\ndestruct B\n"
	if cold := run("cold VM", entry); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entry); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entry); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "destr")
	run("build", "build", "--entry", "destr.main", "--out", outBin, pkgDir)
	builtCmd := exec.Command(outBin)
	builtCmd.Dir = pkgDir
	builtOut, err := builtCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	if built := string(builtOut); built != want {
		t.Fatalf("built binary: got %q, want %q", built, want)
	}
}
