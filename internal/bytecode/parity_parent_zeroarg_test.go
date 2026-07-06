package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

// 9.35: passing arguments to a parent constructor that does not exist is a loud error on both backends; zero-arg parent() to a no-constructor base stays a no-op.

func TestParityParentZeroArgNoOp(t *testing.T) {
	runParity(t, `import io;
class Base { }
class Sub extends Base {
    func Sub() { parent(); }
}
let s = Sub();
io.println("ok");
`, "ok\n")
}

func TestParityParentImplicitDefaultBase(t *testing.T) {
	runParity(t, `import io;
class Base { int count = 5; }
class Sub extends Base {
    func Sub() { parent(); }
}
io.println("${Sub().count}");
`, "5\n")
}

func TestParityParentMatchingCtor(t *testing.T) {
	runParity(t, `import io;
class Base {
    string name;
    func Base(string n) { this.name = n; }
}
class Sub extends Base {
    func Sub() { parent("hi"); }
}
io.println(Sub().name);
`, "hi\n")
}

func TestParityParentBuiltinErrorMessageCapture(t *testing.T) {
	runParity(t, `import io;
class MyErr extends Error {
    func MyErr() { parent("boom"); }
}
try {
    throw MyErr();
} catch (MyErr e) {
    io.println(e.message);
}
`, "boom\n")
}

func TestParityParentArgsToNoCtorBaseUncaught(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackends(t, `class Base { }
class Sub extends Base {
    func Sub() { parent(1, 2, 3); }
}
let s = Sub();
`)
	want := "uncaught RuntimeError: Base constructor expects no arguments\n  at Sub.Sub (line 3)\n  at <top level> (line 5)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch:\n got %q\nwant %q", vmGot, want)
	}
}

// The grandparent is the no-constructor base: the error must restamp the frame that supplies the args.
func TestParityParentArgsMultiLevelUncaught(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackends(t, `class A { }
class B extends A {
    func B() { parent(1, 2); }
}
class C extends B {
    func C() { parent(); }
}
let c = C();
`)
	want := "uncaught RuntimeError: A constructor expects no arguments\n  at B.B (line 3)\n  at C.C (line 6)\n  at <top level> (line 8)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch:\n got %q\nwant %q", vmGot, want)
	}
}

// A user class in an error hierarchy with no constructor is still a caller error (matches direct construction).
func TestParityParentArgsUserErrorBaseUncaught(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackends(t, `class MyErr extends Error { }
class Sub extends MyErr {
    func Sub() { parent("msg"); }
}
let s = Sub();
`)
	want := "uncaught RuntimeError: MyErr constructor expects no arguments\n  at Sub.Sub (line 3)\n  at <top level> (line 5)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch:\n got %q\nwant %q", vmGot, want)
	}
}

func TestParityParentArgsToNoCtorBaseCaught(t *testing.T) {
	runParity(t, `import io;
class Base { }
class Sub extends Base {
    func Sub() { parent(1, 2, 3); }
}
try {
    let s = Sub();
    io.println("no error");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
`, "caught: Base constructor expects no arguments\n")
}

// --- Cross-module ---

const parentZeroArgDonorGb = `module shapes;
export class Shape { }
export class Point {
    int x;
    func Point(int x) { this.x = x; }
}
`

func writeParentZeroArgDonor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(parentZeroArgDonorGb), 0o644); err != nil {
		t.Fatalf("write shapes: %v", err)
	}
	return dir
}

func TestParityCrossModuleParentZeroArgNoOp(t *testing.T) {
	dir := writeParentZeroArgDonor(t)
	runParityModulesDir(t, dir, `import io;
import shapes;
class Circle extends shapes.Shape {
    func Circle() { parent(); }
}
let c = Circle();
io.println("ok");
`, "ok\n")
}

func TestParityCrossModuleParentMatchingCtor(t *testing.T) {
	dir := writeParentZeroArgDonor(t)
	runParityModulesDir(t, dir, `import io;
import shapes;
class Dot extends shapes.Point {
    func Dot() { parent(7); }
}
io.println("${Dot().x}");
`, "7\n")
}

func TestParityCrossModuleParentArgsToNoCtorBaseCaught(t *testing.T) {
	dir := writeParentZeroArgDonor(t)
	runParityModulesDir(t, dir, `import io;
import shapes;
class Circle extends shapes.Shape {
    func Circle() { parent(1, 2, 3); }
}
try {
    let c = Circle();
    io.println("no error");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
`, "caught: Shape constructor expects no arguments\n")
}

func TestUncaughtCrossModuleParentArgsToNoCtorBaseTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, map[string]string{"shapes": parentZeroArgDonorGb}, `import shapes;

func makeCircle(): any {
    return makeCircleInner();
}

func makeCircleInner(): any {
    return Circle();
}

class Circle extends shapes.Shape {
    func Circle() { parent(1, 2, 3); }
}

makeCircle();
`)
	want := "uncaught RuntimeError: Shape constructor expects no arguments\n  at Circle.Circle (line 12)\n  at makeCircleInner (line 8)\n  at makeCircle (line 4)\n  at <top level> (line 15)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}
