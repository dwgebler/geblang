package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const deferSpreadDonor = `module dsdonor;
import io;
export func lf(int a, int b = 4): void { io.println("lf ${a} ${b}"); }
export class LK {
    func go(int a, int b = 6): void { io.println("lk ${a} ${b}"); }
}
`

func writeDeferSpreadDonor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dsdonor.gb"), []byte(deferSpreadDonor), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A dict spread in a defer is read when the call fires, drops unknown keys and keeps defaults, for every callee kind.
func TestParityDeferDictSpread(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, writeDeferSpreadDonor(t), `import io;
import dsdonor;
class K { func go(int a, int b = 5): void { io.println("go ${a} ${b}"); } }
class SubK extends K {}
class Sub extends dsdonor.LK {}
func f(int a, int b = 7): void { io.println("f ${a} ${b}"); }
func run(): void {
    dict<string, any> d = {"a": 1, "extra": 99};
    let c = func(int a, int b = 3): void { io.println("c ${a} ${b}"); };
    let fv = dsdonor.lf;
    defer f(...d);
    defer SubK().go(...d);
    defer c(...d);
    defer f(0, ...{"b": 2});
    defer dsdonor.lf(...d);
    defer dsdonor.LK().go(...d);
    defer Sub().go(...d);
    defer fv(...d);
    defer Sub().go(b: 8, a: 9);
    d["a"] = 2;
}
run();
`)
	want := "lk 9 8\nlf 2 4\nlk 2 6\nlk 2 6\nlf 2 4\nf 0 2\nc 2 3\ngo 2 5\nf 2 7\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}

// Direct dict-spread and named calls reach cross-module function values and methods inherited from another module.
func TestParityCrossModuleNamedAndSpreadCalls(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, writeDeferSpreadDonor(t), `import io;
import dsdonor;
class Sub extends dsdonor.LK {}
dict<string, any> d = {"a": 1, "zzz": 0};
let fv = dsdonor.lf;
fv(...d);
Sub().go(...d);
Sub().go(b: 3, a: 4);
let m = Sub().go;
m(b: 5, a: 6);
m(...d);
`)
	want := "lf 1 4\nlk 1 6\nlk 4 3\nlk 6 5\nlk 1 6\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}

// A spread into io.println / io.print / io.stdoutWrite / sys.exit gets the same argument checks as any native call.
func TestParitySpreadIntoOpcodeBuiltins(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import sys;
func attempt(callable f): void {
    try { f(); } catch (Error e) { io.println("${typeof(e)}: ${e.message}"); }
}
io.println(...["x"]);
io.print(...["y"]);
io.stdoutWrite(...["z"]);
io.println("");
attempt(func(): void { io.println(...5); });
attempt(func(): void { io.println(..."ab"); });
attempt(func(): void { io.println(...["a", "b"]); });
attempt(func(): void { io.println(...{"value": 1}); });
attempt(func(): void { sys.exit(...["a", "b"]); });
`)
	want := "x\nyz\nRuntimeError: spread argument must be a list\nRuntimeError: spread argument must be a list\nRuntimeError: io.println expects exactly one argument\nRuntimeError: spread argument must be a list\nRuntimeError: sys.exit expects exactly one argument\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}

// A failing deferred native call keeps its error class, carries no extra prefix, and is reported at the defer line.
func TestParityDeferredNativeFailures(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
func reader(): void {
    defer io.readText("/nonexistent/geblang-defer-probe.txt");
    io.println("body");
}
try { reader(); } catch (IOError e) { io.println("IOError caught"); }
class K { func go(int a): void {} }
func f(int a): void {}
func t1(): void { defer f(...5); }
func t2(): void { defer K().go(...5); }
func t3(): void { let c = func(int a): void {}; defer c(...5); }
func t4(): void { defer io.println(...{"a": 1}); }
for (fn in [t1, t2, t3, t4]) {
    try { fn(); } catch (Error e) { let top = e.stackTrace().first(); io.println("${typeof(e)}: ${e.message} @${top.function()}:${top.line()}"); }
}
`)
	want := "body\nIOError caught\nRuntimeError: spread argument must be a list or dict @t1:9\nRuntimeError: spread argument must be a list or dict @t2:10\nRuntimeError: spread argument must be a list or dict @t3:11\nRuntimeError: spread argument must be a list @t4:12\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}
