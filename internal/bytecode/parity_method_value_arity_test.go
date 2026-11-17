package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const methodValueArityDonor = `module mvadonor;
export class Remote {
    func go(int a, int b): int { return a + b; }
    static func st(int a): int { return a; }
}
`

// Method values report misuse as Class.member with the caller's argument count (no receiver), like the type-mismatch message.
func TestParityMethodValueArityMessages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mvadonor.gb"), []byte(methodValueArityDonor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import mvadonor;
class R {
    func R(int seed = 0) {}
    func inst(int a, int b = 1): int { return a; }
    func vari(int a, int ...rest): int { return a; }
    static func reg(int a): int { return a; }
}
class Sub extends R {}
class LocalRemote extends mvadonor.Remote {}
class P { func P(int a) {} }
func probe(string label, callable f): void {
    try { f(); io.println("${label}: ok"); } catch (Error e) { io.println("${label}: ${typeof(e)}: ${e.message}"); }
}
let m = R().inst;
let s = Sub().inst;
let v = R().vari;
let st = R.reg;
let rem = mvadonor.Remote().go;
let lrem = LocalRemote().go;
let rst = mvadonor.Remote.st;
let pc = P;
let c = func(int a): int { return a; };
probe("instance arity", func(): void { m(1, 2, 3); });
probe("instance missing", func(): void { m(); });
probe("instance type", func(): void { m("x"); });
probe("named missing", func(): void { m(b: 2); });
probe("named unknown", func(): void { m(a: 1, zz: 2); });
probe("named dup", func(): void { m(1, a: 2); });
probe("subclass arity", func(): void { s(1, 2, 3); });
probe("variadic missing", func(): void { v(); });
probe("static arity", func(): void { st(1, 2); });
probe("cross-module arity", func(): void { rem(1, 2, 3); });
probe("cross-module missing", func(): void { rem(1); });
probe("inherited cross-module arity", func(): void { lrem(1, 2, 3); });
probe("cross-module static", func(): void { rst(1, 2); });
probe("constructor value", func(): void { pc(); });
probe("closure arity", func(): void { c(1, 2); });
`)
	want := "instance arity: RuntimeError: R.inst expects at most 2 arguments, got 3\n" +
		"instance missing: RuntimeError: R.inst missing argument a\n" +
		"instance type: TypeError: R.inst expects int for parameter 'a', got string\n" +
		"named missing: RuntimeError: R.inst missing argument a\n" +
		"named unknown: RuntimeError: R.inst has no parameter zz\n" +
		"named dup: RuntimeError: R.inst parameter a passed more than once\n" +
		"subclass arity: RuntimeError: R.inst expects at most 2 arguments, got 3\n" +
		"variadic missing: RuntimeError: R.vari missing argument a\n" +
		"static arity: RuntimeError: R.reg expects at most 1 arguments, got 2\n" +
		"cross-module arity: RuntimeError: Remote.go expects at most 2 arguments, got 3\n" +
		"cross-module missing: RuntimeError: Remote.go missing argument b\n" +
		"inherited cross-module arity: RuntimeError: Remote.go expects at most 2 arguments, got 3\n" +
		"cross-module static: RuntimeError: Remote.st expects at most 1 arguments, got 2\n" +
		"constructor value: RuntimeError: no matching overload for P\n" +
		"closure arity: RuntimeError: <closure> expects at most 1 arguments, got 2\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}
