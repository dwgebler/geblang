package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A cross-module parent-chain walk and dir on the class value are byte-identical across cold VM, cached .gbc, evaluator, and a built binary.
func TestCrossModuleReflectParentWalkAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: dirwalk\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "base.gb"), []byte(`module dirwalk.base;
export interface Greeter {
    func greet(): string;
}
export class BaseCtl implements Greeter {
    static const KIND = "base";
    func BaseCtl() {}
    func greet(): string { return "hi"; }
    func shared(): string { return "s"; }
    static func mkBase(): BaseCtl { return BaseCtl(); }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "mid.gb"), []byte(`module dirwalk.mid;
import dirwalk.base as base;
export interface Named {
    func name(): string;
}
export class MyCtl extends base.BaseCtl implements Named {
    func MyCtl() { parent(); }
    func extra(): string { return "e"; }
    func name(): string { return "n"; }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module dirwalk.main;
import dirwalk.mid as mid;
import reflect;
import io;
func collectInterfaces(any cls): list<string> {
    let acc = [];
    let cur = cls;
    let done = false;
    while (!done) {
        for (i in reflect.interfaces(cur)) { acc.push(i as string); }
        let p = reflect.parent(cur);
        if (p == null) { done = true; }
        else {
            let nxt = reflect.class(p as string);
            if (nxt == null) { done = true; } else { cur = nxt; }
        }
    }
    return acc;
}
export func main(list<string> args): void {
    let cls = reflect.class(mid.MyCtl());
    io.println("parent=${reflect.parent(cls)}");
    io.println("ifaces=${collectInterfaces(cls)}");
    io.println("dir=${dir(cls)}");
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

	const want = "parent=BaseCtl\nifaces=[\"Named\", \"Greeter\"]\ndir=[\"KIND\", \"extra\", \"greet\", \"mkbase\", \"name\", \"shared\"]\n"
	if cold := run("cold VM", entryPath); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "dirwalk")
	if buildOut, err := exec.Command(bin, "build", "--entry", "dirwalk.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	if built, err := exec.Command(outBin).CombinedOutput(); err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, built)
	} else if string(built) != want {
		t.Fatalf("built binary: got %q, want %q", string(built), want)
	}
}
