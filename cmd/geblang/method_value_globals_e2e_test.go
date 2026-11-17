package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const mvgDispatchGb = `module mvg.disp;
import reflect;
export func viaCallable(callable f): any {
    return f();
}
export func viaReflect(any obj, string name): any {
    let h = reflect.method(obj, name);
    return h();
}
`

const mvgMainGb = `module mvg.main;
import io;
import async;
import mvg.disp as disp;
int calls = 0;
class C {
    func hit(): int { calls = calls + 1; return calls; }
    func gen(): any { calls = calls + 1; yield calls; }
}
async func later(): int { calls = calls + 1; return calls; }
export func main(list<string> args): void {
    let c = C();
    let m = c.hit;
    m();
    io.println("same-module ${calls}");
    io.println("cross-module ${disp.viaCallable(c.hit)} global=${calls}");
    io.println("reflect ${disp.viaReflect(c, "hit")} global=${calls}");
    async.await(async.run(func(): int { return c.hit(); }));
    await later();
    let g = c.gen;
    for (v in g()) { }
    io.println("async ${calls}");
}
`

const mvgExpected = "same-module 1\ncross-module 2 global=2\nreflect 3 global=3\nasync 6\n"

// Method values, tasks and generators must keep their module-global writes on every runtime path, including a built binary whose entry module runs on a worker.
func TestMethodValueGlobalWritesAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(pkgDir, "geblang.yaml"): "name: mvg\nversion: 0.1.0\nsource: src\n",
		filepath.Join(srcDir, "disp.gb"):      mvgDispatchGb,
		filepath.Join(srcDir, "main.gb"):      mvgMainGb,
	}
	for path, src := range files {
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(srcDir, "main.gb")
	run := func(label string, args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		if string(out) != mvgExpected {
			t.Fatalf("%s: got %q, want %q", label, out, mvgExpected)
		}
	}
	run("cold VM run", entry)
	run("cached VM run", entry)
	run("evaluator run", "--disable-vm", entry)

	outBin := filepath.Join(pkgDir, "mvg")
	if out, err := exec.Command(bin, "build", "--entry", "mvg.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, out)
	}
	if string(out) != mvgExpected {
		t.Fatalf("built binary: got %q, want %q", out, mvgExpected)
	}
}
