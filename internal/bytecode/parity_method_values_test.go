package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

// 9.11: a local static method used as a first-class value (store, call, pass, return).
func TestParityStaticMethodValueLocal(t *testing.T) {
	runParity(t, `import io;
class Box {
    static const KIND = "box";
    static func stamp(int x): int { return x + 100; }
}
func apply1(any f, int v): int { return f(v); }
func getf(): any { return Box.stamp; }
let s = Box.stamp;
io.println(s(1));
io.println(apply1(Box.stamp, 2));
let g = getf();
io.println(g(3));
io.println(Box.KIND);
`, "101\n102\n103\nbox\n")
}

// 9.11: an inherited static method resolves as a value through a local subclass.
func TestParityStaticMethodValueInherited(t *testing.T) {
	runParity(t, `import io;
class Base {
    static func tag(string s): string { return "base:" + s; }
}
class Sub extends Base {}
let f = Sub.tag;
io.println(f("x"));
`, "base:x\n")
}

// 9.11: an overloaded static value pins to the first overload, matching the evaluator (unlike a free-function value it does not reselect).
func TestParityStaticMethodValueOverloadPinsFirst(t *testing.T) {
	runParity(t, `import io;
class R {
    static func reg(int x): string { return "int:" + (x as string); }
    static func reg(string x): string { return "str:" + x; }
}
let f = R.reg;
io.println(f(5));
try {
    io.println(f("z"));
} catch (RuntimeError e) {
    io.println("pinned-first");
}
`, "int:5\npinned-first\n")
}

// 9.11 Gap B: a dynamically-obtained class value resolves static const + method (getField on a BytecodeClass), via reflect.class on a local instance.
func TestParityStaticMemberViaClassValueLocal(t *testing.T) {
	runParityWithStdlib(t, `import io;
import reflect;
class Box {
    static const KIND = "box";
    static func stamp(int x): int { return x * 2; }
    int id;
    func Box(int id) { this.id = id; }
}
let cls = reflect.class(Box(1));
io.println(cls.KIND);
let f = cls.stamp;
io.println(f(21));
`, "box\n42\n")
}

// 9.11 cross-module: a static method + static const on a class in another module resolve as values (nested selector -> getField BytecodeClass, foreign path).
func TestParityStaticMethodValueCrossModule(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "boxmod", `module boxmod;
export class Box {
    static const KIND = "box";
    static func stamp(int x): int { return x * 3; }
}
`)
	runParityModulesDir(t, dir, `import io;
import boxmod;
let f = boxmod.Box.stamp;
io.println(f(10));
io.println(boxmod.Box.KIND);
func apply1(any g, int v): int { return g(v); }
io.println(apply1(boxmod.Box.stamp, 4));
`, "30\nbox\n12\n")
}

// 9.11 cross-module: a static declared on a cross-module ancestor resolves as a value through a local subclass value.
func TestParityStaticMethodValueCrossModuleInherited(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "shapes", `module shapes;
export class Shape {
    static const KIND = "shape";
    static func make(string n): string { return "made:" + n; }
}
`)
	runParityModulesDir(t, dir, `import io;
import shapes;
class Square extends shapes.Shape {}
let f = Square.make;
io.println(f("sq"));
io.println(Square.KIND);
`, "made:sq\nshape\n")
}

// 9.16: a bound instance method used as a value (store, call, pass, HOF callback, mutation-after-binding sees the receiver by reference).
func TestParityBoundMethodValueLocal(t *testing.T) {
	runParity(t, `import io;
class Greeter {
    string name;
    func Greeter(string name) { this.name = name; }
    func greet(): string { return "hi " + this.name; }
    func add(int x): int { return (this.name as string).length() + x; }
}
class Helper { func key(int x): int { return x; } }
func call0(any f): string { return f(); }
let g = Greeter("ann");
let m = g.greet;
io.println(m());
io.println(call0(g.greet));
g.name = "bob";
io.println(m());
let h = Helper();
io.println("${[3, 1, 2].sortBy(h.key)}");
`, "hi ann\nhi ann\nhi bob\n[1, 2, 3]\n")
}

// 9.16: a bound method invoked from inside another method's scope uses its own captured receiver, not the caller's this (the evaluator previously forwarded the caller's this).
func TestParityBoundMethodValueInsideMethodScope(t *testing.T) {
	runParity(t, `import io;
class Box {
    int base;
    func Box(int base) { this.base = base; }
    func add(int x): int { return this.base + x; }
}
class Runner {
    int base;
    func Runner() { this.base = 999; }
    func run(): int {
        let b = Box(10);
        let m = b.add;
        return m(5);
    }
}
io.println(Runner().run());
`, "15\n")
}

// 9.16: an inherited instance method binds through a subclass instance.
func TestParityBoundMethodValueInherited(t *testing.T) {
	runParity(t, `import io;
class Base { func hello(): string { return "base-hello"; } }
class Sub extends Base { int v; func Sub(int v) { this.v = v; } }
let s = Sub(9);
let m = s.hello;
io.println(m());
`, "base-hello\n")
}

// 9.16: an overloaded instance method value pins to the first overload, matching the evaluator.
func TestParityBoundMethodValueOverloadPinsFirst(t *testing.T) {
	runParity(t, `import io;
class Calc {
    func go(int x): string { return "int:" + (x as string); }
    func go(string x): string { return "str:" + x; }
}
let c = Calc();
let m = c.go;
io.println(m(5));
try {
    io.println(m("z"));
} catch (RuntimeError e) {
    io.println("pinned-first");
}
`, "int:5\npinned-first\n")
}

// 9.16 cross-module: a bound instance method whose class lives in another module works as a value, including mutation-after-binding.
func TestParityBoundMethodValueCrossModule(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "boxmod", `module boxmod;
export class Box {
    int id;
    func Box(int id) { this.id = id; }
    func label(): string { return "box-" + (this.id as string); }
}
`)
	runParityModulesDir(t, dir, `import io;
import boxmod;
func call0(any f): string { return f(); }
let b = boxmod.Box(42);
let m = b.label;
io.println(m());
io.println(call0(b.label));
b.id = 7;
io.println(m());
`, "box-42\nbox-42\nbox-7\n")
}

func writeModule(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".gb"), []byte(source), 0o644); err != nil {
		t.Fatalf("write module %s: %v", name, err)
	}
}
