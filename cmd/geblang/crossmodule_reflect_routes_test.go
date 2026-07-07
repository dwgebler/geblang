package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// reflect.methods (and the decorated route walk) on a class from a cross-module instance must match across cold VM, cached .gbc, evaluator, and a built binary (empty on every VM path in 1.32.0).
func TestCrossModuleReflectRoutesAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: rroute\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("base.gb", `module rroute.base;
export class BaseCtl {
    func BaseCtl() {}
    func shared(): string { return "s"; }
}
`)
	write("routes.gb", `module rroute.routes;
export func Get(string path): dict<string, any> { return {"__route": true, "path": path}; }
`)
	write("ctl.gb", `module rroute.ctl;
import rroute.base as base;
import rroute.routes as routes;
export class Home extends base.BaseCtl {
    func Home() { parent(); }
    @routes.Get("/")
    func index(): string { return "i"; }
    @routes.Get("/about")
    func about(): string { return "a"; }
    static func mk(): Home { return Home(); }
}
`)
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module rroute.main;
import rroute.ctl as ctl;
import reflect;
import io;
export func main(list<string> args): void {
    let c = ctl.Home();
    let cls = reflect.class(c);
    io.println("methods=${reflect.methods(cls).sorted()}");
    int routes = 0;
    for (name in reflect.methods(cls)) {
        let h = reflect.method(c, name as string);
        for (d in reflect.decorators(h)) {
            let dm = d as dict<string, any>;
            if ((dm["name"] as string).endsWith("Get")) { routes = routes + 1; }
        }
    }
    io.println("routes=${routes}");
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

	const want = "methods=[\"about\", \"index\"]\nroutes=2\n"
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

	outBin := filepath.Join(pkgDir, "rroute")
	if buildOut, err := exec.Command(bin, "build", "--entry", "rroute.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	builtOut, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	if string(builtOut) != want {
		t.Fatalf("built binary: got %q, want %q", builtOut, want)
	}
}
