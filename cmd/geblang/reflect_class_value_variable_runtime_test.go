package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// reflect.methods/staticMethods/parent on a class value held in a variable are byte-identical across cold VM, cached .gbc, evaluator, and a built binary.
func TestReflectClassValueViaVariableAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: reflectvar\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module reflectvar.main;
import io;
import reflect;
class Base {
    func Base() {}
    func greet(): string { return "hi"; }
}
class Mid extends Base {
    func extra(): string { return "extra"; }
    static func makeMid(): Mid { return Mid(); }
}
export func main(list<string> args): void {
    let c = Mid;
    io.println("methods=${reflect.methods(c)}");
    io.println("statics=${reflect.staticMethods(c)}");
    io.println("parent=${reflect.parent(c)}");
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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

	const want = "methods=[\"extra\"]\nstatics=[\"makeMid\"]\nparent=Base\n"
	if cold := run("cold VM", entryPath); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM (.gbc hit): got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "reflectvar")
	if buildOut, err := exec.Command(bin, "build", "--entry", "reflectvar.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	if built, err := exec.Command(outBin).CombinedOutput(); err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, built)
	} else if string(built) != want {
		t.Fatalf("built binary: got %q, want %q", string(built), want)
	}
}
