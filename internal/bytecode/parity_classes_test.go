package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityBasicClass(t *testing.T) {
	runParity(t, `import io;
class Point {
    int x;
    int y;
    func Point(int x, int y) {
        this.x = x;
        this.y = y;
    }
    func sum(): int {
        return this.x + this.y;
    }
}
Point p = Point(3, 4);
io.println(p.sum());
io.println(p.x);
`, "7\n3\n")
}

func TestParityClassInheritance(t *testing.T) {
	runParity(t, `import io;
class Animal {
    string name;
    func Animal(string name) {
        this.name = name;
    }
    func speak(): string {
        return this.name + " says something";
    }
}
class Dog extends Animal {
    func Dog(string name) {
        parent(name);
    }
    func speak(): string {
        return this.name + " says woof";
    }
}
Dog d = Dog("Rex");
io.println(d.speak());
io.println(d.name);
`, "Rex says woof\nRex\n")
}

func TestParityClassEqMagicMethod(t *testing.T) {
	runParity(t, `import io;
class Vec {
    int x;
    func Vec(int x) { this.x = x; }
    func __eq(any other): bool {
        if (other instanceof Vec) {
            return this.x == other.x;
        }
        return false;
    }
}
Vec a = Vec(5);
Vec b = Vec(5);
Vec c = Vec(9);
io.println(a == b);
io.println(a == c);
`, "true\nfalse\n")
}

func TestParityClassArithmeticMagicMethods(t *testing.T) {
	runParity(t, `import io;
class Num {
    int v;
    func Num(int v) { this.v = v; }
    func __add(any other): Num { return Num(this.v + other.v); }
    func __mul(any other): Num { return Num(this.v * other.v); }
    func inspect(): int { return this.v; }
}
Num a = Num(3);
Num b = Num(4);
io.println((a + b).inspect());
io.println((a * b).inspect());
`, "7\n12\n")
}

func TestParityClassComparisonMagicMethods(t *testing.T) {
	runParity(t, `import io;
class Box {
    int v;
    func Box(int v) { this.v = v; }
    func __lt(any other): bool { return this.v < other.v; }
    func __gt(any other): bool { return this.v > other.v; }
}
Box a = Box(3);
Box b = Box(5);
io.println(a < b);
io.println(b < a);
io.println(b > a);
`, "true\nfalse\ntrue\n")
}

func TestParityClassLteGteMagicMethods(t *testing.T) {
	runParity(t, `import io;
class Weight {
    int g;
    func Weight(int g) { this.g = g; }
    func __lte(any other): bool { return this.g <= other.g; }
    func __gte(any other): bool { return this.g >= other.g; }
}
Weight a = Weight(3);
Weight b = Weight(5);
Weight c = Weight(3);
io.println(a <= b);
io.println(b <= a);
io.println(a <= c);
io.println(b >= a);
io.println(a >= b);
io.println(a >= c);
`, "true\nfalse\ntrue\ntrue\nfalse\ntrue\n")
}

func TestParityClassBitwiseMagicMethods(t *testing.T) {
	runParity(t, `import io;
class Flags {
    int v;
    func Flags(int v) { this.v = v; }
    func __bitand(any other): Flags { return Flags(this.v & other.v); }
    func __bitor(any other): Flags { return Flags(this.v | other.v); }
    func __bitxor(any other): Flags { return Flags(this.v ^ other.v); }
    func __lshift(any other): Flags { return Flags(this.v << other.v); }
    func __rshift(any other): Flags { return Flags(this.v >> other.v); }
    func __bitnot(): Flags { return Flags(~this.v); }
    func value(): int { return this.v; }
}
Flags a = Flags(12);
Flags b = Flags(10);
io.println((a & b).value());
io.println((a | b).value());
io.println((a ^ b).value());
io.println((a << Flags(1)).value());
io.println((a >> Flags(1)).value());
Flags c = Flags(0);
io.println((~c).value());
`, "8\n14\n6\n24\n6\n-1\n")
}

func TestParityClassGetSetMethods(t *testing.T) {
	runParity(t, `import io;
class Config {
    dict data;
    func Config() { this.data = {}; }
    func __get(string key): any { return this.data[key]; }
    func __set(string key, any value): void { this.data[key] = value; }
}
Config c = Config();
c.foo = "bar";
c.num = 42;
io.println(c.foo);
io.println(c.num);
`, "bar\n42\n")
}

func TestParityClassInvokeMagicMethod(t *testing.T) {
	runParity(t, `import io;
class Multiplier {
    int factor;
    func Multiplier(int factor) { this.factor = factor; }
    func __invoke(int x): int { return this.factor * x; }
}
Multiplier m = Multiplier(7);
io.println(m(6));
`, "42\n")
}

func TestParityInterface(t *testing.T) {
	runParity(t, `import io;
interface Greeter {
    func greet(): string;
}
class English implements Greeter {
    func greet(): string { return "Hello"; }
}
class Spanish implements Greeter {
    func greet(): string { return "Hola"; }
}
English e = English();
Spanish s = Spanish();
io.println(e.greet());
io.println(s.greet());
io.println(e instanceof Greeter);
`, "Hello\nHola\ntrue\n")
}

func TestParityStaticMembers(t *testing.T) {
	runParity(t, `import io;
class Named {
    static const prefix = "N";
    static func label(string name): string {
        return Named.prefix + ":" + name;
    }
}
io.println(Named.prefix);
io.println(Named.label("Ada"));
`, "N\nN:Ada\n")
}

func TestParityCompoundAssignField(t *testing.T) {
	runParity(t, `import io;
class Counter {
    int value;
    func Counter(int v) { this.value = v; }
}
let c = Counter(10);
c.value += 5;
io.println(c.value as string);
c.value *= 2;
io.println(c.value as string);
`, "15\n30\n")
}

func TestParityRangeFieldAccess(t *testing.T) {
	runParity(t, `import io;
let r = 3..15 by 4;
io.println(r.start as string);
io.println(r.end as string);
io.println(r.step as string);
`, "3\n15\n4\n")
}

// TestParityParentInheritedConstructor covers the evaluator parent() fix
// where multi-level inherited constructors recursed infinitely.
func TestParityParentInheritedConstructor(t *testing.T) {
	runParity(t, `import io;
class A extends Error { func A(string msg) { parent(msg); } }
class B extends A { func B(string msg) { parent(msg); } }
let e = B("kaboom");
io.println(e.message);
let caught = false;
try { throw B("kaboom"); } catch (A x) { caught = true; }
io.println(caught);
`, "kaboom\ntrue\n")
}

// TestParityParentMethodChain covers a multi-level override chain where each
// level calls parent.method(). The evaluator must resolve parent against the
// lexically-enclosing class, not the runtime class of `this`; resolving via
// this.Class.Parent re-enters the same method and infinite-loops.
func TestParityParentMethodChain(t *testing.T) {
	runParity(t, `import io;
class Base { int n; func Base(int n) { this.n = n; } func value(): int { return this.n + 1; } }
class Sub extends Base { func Sub(int n) { parent(n); } func value(): int { return parent.value() + 10; } }
class Leaf extends Sub { func Leaf(int n) { parent(n); } func value(): int { return parent.value() - 3; } }
io.println(Sub(5).value());
io.println(Leaf(5).value());
`, "16\n13\n")
}

// TestParityParentMethodChainKeepsDynamicDispatch proves that while parent.X
// resolves lexically, the parent method still invokes this.Y() polymorphically.
func TestParityParentMethodChainKeepsDynamicDispatch(t *testing.T) {
	runParity(t, `import io;
class Base {
  func name(): string { return "base"; }
  func greet(): string { return "hello " + this.name(); }
}
class Sub extends Base { func name(): string { return "sub"; } }
class Leaf extends Sub {
  func name(): string { return "leaf"; }
  func describe(): string { return parent.greet() + " / " + this.name(); }
}
io.println(Base().greet());
io.println(Sub().greet());
io.println(Leaf().greet());
io.println(Leaf().describe());
`, "hello base\nhello sub\nhello leaf\nhello leaf / leaf\n")
}

// TestParityStaticLetMutation verifies that `static let` class members
// hold mutable state and survive across calls to the class's static
// methods.
func TestParityStaticLetMutation(t *testing.T) {
	runParity(t, `import io;
class Counter {
    static let count = 0;
    static func bump(): int {
        Counter.count = Counter.count + 1;
        return Counter.count;
    }
}
io.println(Counter.bump());
io.println(Counter.bump());
io.println(Counter.bump());
`, "1\n2\n3\n")
}

// TestParityFuncAsFieldType verifies `func` is accepted as a class
// field type (parser disambiguates `func NAME ;` and `func NAME = ...`
// as a typed declaration vs `func NAME (` as a method definition).
func TestParityFuncAsFieldType(t *testing.T) {
	runParity(t, `import io;
class Holder {
    func cb;
    func Holder(func cb) { this.cb = cb; }
    func invoke(): int {
        let fn = this.cb;
        return fn();
    }
}
let h = Holder(func(): int { return 7; });
io.println(h.invoke());
`, "7\n")
}

// TestParityCrossModuleClassExtension exercises `class B extends mod.A`
// patterns: B in the main script extends a class A declared in another
// .gb module. The fixture covers both `parent(args)` (constructor) and
// `parent.method(args)` dispatch into A, plus a field-write inside A's
// constructor that mutates the (subclass) instance.
func TestParityCrossModuleClassExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Greeter {
    string name;
    func Greeter(string name) {
        this.name = name;
    }
    func greet(): string {
        return "hello from " + this.name;
    }
}
`), 0o644); err != nil {
		t.Fatalf("write base module: %v", err)
	}

	source := `import io;
import base;

class Loud extends base.Greeter {
    func Loud(string name) {
        parent(name);
    }
    func shout(): string {
        return parent.greet().upper();
    }
}

let l = Loud("ada");
io.println(l.greet());
io.println(l.shout());
io.println(l.name);
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "hello from ada\nHELLO FROM ADA\nada\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

func TestParityCrossModuleInterfaceDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iface.gb"), []byte(`module iface;
export interface Greetable {
    string name;
    int age;
    func greet(): string {
        return "hello, " + this.name + " (" + (this.age as string) + ")";
    }
}
`), 0o644); err != nil {
		t.Fatalf("write iface: %v", err)
	}

	source := `import io;
import iface;

class User implements iface.Greetable {
    func User(string n, int a) { this.name = n; this.age = a; }
}

class Loud implements iface.Greetable {
    func Loud(string n, int a) { this.name = n; this.age = a; }
    func greet(): string { return "HELLO, " + this.name; }
}

io.println(User("ada", 36).greet());
io.println(Loud("bo", 4).greet());
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "hello, ada (36)\nHELLO, bo\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

func TestParityCrossModuleInterfaceDefaultSubclass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "contracts.gb"), []byte(`module contracts;
export interface Stampable {
    func stamp(): string;
    func label(): string { return "L:" + this.stamp(); }
}
`), 0o644); err != nil {
		t.Fatalf("write contracts: %v", err)
	}

	source := `import io;
import contracts;
from contracts import Stampable;

class Coin implements contracts.Stampable {
    func stamp(): string { return "m"; }
}
class BigCoin extends Coin {}

class Token implements Stampable {
    func stamp(): string { return "c"; }
}
class BigToken extends Token {}

io.println(Coin().label());
io.println(BigCoin().label());
io.println(Token().label());
io.println(BigToken().label());
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "L:m\nL:m\nL:c\nL:c\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

func TestParityInterfaceDefaults(t *testing.T) {
	runParity(t, `import io;

interface Greetable {
    string name;
    func greet(): string { return "hello, " + this.name; }
    func upper(): string;
}

class User implements Greetable {
    func User(string n) { this.name = n; }
    func upper(): string { return this.name.upper(); }
}

class Loud implements Greetable {
    func Loud(string n) { this.name = n; }
    func greet(): string { return "HELLO, " + this.name; }
    func upper(): string { return this.name.upper(); }
}

let u = User("ada");
io.println(u.greet());
io.println(u.upper());

let l = Loud("ada");
io.println(l.greet());
`, "hello, ada\nADA\nHELLO, ada\n")
}

func TestParityInterfaceDefaultSubclass(t *testing.T) {
	runParity(t, `import io;

interface Stampable {
    func stamp(): string;
    func label(): string { return "L:" + this.stamp(); }
}

class Marker implements Stampable {
    func mark(): string { return this.stamp(); }
    func stamp(): string { return "s"; }
}

class SubMarker extends Marker {}

class OverMarker extends Marker {
    func stamp(): string { return "o"; }
}

io.println(Marker().label());
io.println(SubMarker().label());
io.println(OverMarker().label());
`, "L:s\nL:s\nL:o\n")
}

func TestParityInterfaceDiamondConflict(t *testing.T) {
	source := `import io;
interface A { func foo(): string { return "A"; } }
interface B { func foo(): string { return "B"; } }
class C implements A, B {}
io.println(C().foo());
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.New(&evOut)
	if _, err := ev.Eval(program); err == nil {
		t.Fatalf("evaluator: expected ambiguous-default error, got nil")
	} else if !strings.Contains(err.Error(), "multiple defaults") {
		t.Fatalf("evaluator: error should mention multiple defaults: %v", err)
	}

	_, err := bytecode.Compile(program, []byte(source), "parity")
	if err == nil {
		t.Fatalf("bytecode: expected ambiguous-default error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple defaults") {
		t.Fatalf("bytecode: error should mention multiple defaults: %v", err)
	}
}

func TestParityInterfaceDiamondOverride(t *testing.T) {
	runParity(t, `import io;
interface A { func foo(): string { return "A"; } }
interface B { func foo(): string { return "B"; } }
class C implements A, B {
    func foo(): string { return "C"; }
}
io.println(C().foo());
`, "C\n")
}

// TestParityCrossModuleParentMethodChain covers a multi-level parent.method()
// override chain whose top class lives in another module. The lexical-class
// resolution must hold across the module boundary on both backends.
func TestParityCrossModuleParentMethodChain(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "basemod.gb"), []byte(`module basemod;
export class Base {
    int n;
    func Base(int n) { this.n = n; }
    func value(): int { return this.n + 1; }
}
`), 0o644); err != nil {
		t.Fatalf("write basemod: %v", err)
	}

	source := `import io;
import basemod;

class Sub extends basemod.Base {
    func Sub(int n) { parent(n); }
    func value(): int { return parent.value() + 10; }
}
class Leaf extends Sub {
    func Leaf(int n) { parent(n); }
    func value(): int { return parent.value() - 3; }
}
io.println(Sub(5).value());
io.println(Leaf(5).value());
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "16\n13\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// TestParityLengthProperty verifies the .length property (no parens)
// returns the same result as the .length() method on every supported
// collection type, on both backends.
func TestParityLengthProperty(t *testing.T) {
	runParity(t, `import io;
import bytes;
io.println([1,2,3].length);
io.println("hello".length);
io.println({"a": 1, "b": 2}.length);
io.println(bytes.fromString("hi").length);
io.println((1..10).length);
`, "3\n5\n2\n2\n10\n")
}

// TestParityStaticTypedField verifies the `static <type> name = value`
// declaration syntax produces a mutable static class member.
func TestParityStaticTypedField(t *testing.T) {
	runParity(t, `import io;
class Stats {
    static int hits = 0;
    static string label = "items";
    static func record(): void {
        Stats.hits = Stats.hits + 1;
    }
}
Stats.record();
Stats.record();
io.println(Stats.hits);
io.println(Stats.label);
`, "2\nitems\n")
}

// TestParityDestructorAtProgramExit verifies destructors fire at
// the end-of-program sweep (after the user code finishes
// executing) and in reverse-creation order on both backends.
func TestParityDestructorAtProgramExit(t *testing.T) {
	runParity(t, `import io;
class R {
    string name;
    func R(string n) { this.name = n; io.println("acq " + n); }
    func ~R() { io.println("rel " + this.name); }
}
let a = R("first");
let b = R("second");
io.println("end of body");
`, "acq first\nacq second\nend of body\nrel second\nrel first\n")
}

// TestParityDestructorRunsOnce verifies the destructor does not
// fire twice when the same instance is reached via both a `del`
// and the program-exit sweep.
func TestParityDestructorRunsOnce(t *testing.T) {
	runParity(t, `import io;
class R {
    func R() {}
    func ~R() { io.println("once"); }
}
let r = R();
del r;
io.println("after del");
`, "once\nafter del\n")
}

// TestParityDelFiresDestructor verifies `del x` calls the
// destructor inline (not at program-exit time).
func TestParityDelFiresDestructor(t *testing.T) {
	runParity(t, `import io;
class R {
    string name;
    func R(string n) { this.name = n; }
    func ~R() { io.println("rel " + this.name); }
}
let r = R("a");
io.println("before del");
del r;
io.println("after del");
`, "before del\nrel a\nafter del\n")
}

func TestParityDelDestructorStaticWritePersists(t *testing.T) {
	runParity(t, `import io;
class Counter {
    static int destroyed = 0;
    func ~Counter() { Counter.destroyed = Counter.destroyed + 1; }
}
Counter.destroyed = 0;
let c = Counter();
io.println(Counter.destroyed);
del c;
io.println(Counter.destroyed);
`, "0\n1\n")
}

// TestParityWithDoesNotInvokeDestructor verifies that a class
// destructor does NOT fire at with-block exit (only __exit__
// does). The destructor fires later via the program-exit sweep,
// after "after with" has already printed.
func TestParityWithDoesNotInvokeDestructor(t *testing.T) {
	runParity(t, `import io;
class R {
    func R() { io.println("acq"); }
    func __exit__(): void { io.println("exit"); }
    func ~R() { io.println("dtor"); }
}
with (R()) {
    io.println("body");
}
io.println("after with");
`, "acq\nbody\nexit\nafter with\ndtor\n")
}

// TestParityExplicitTypeArgOverridesInference verifies that an explicit
// `<TypeArgs>` clause on a generic function call binds T to the explicit
// type and the function body sees that binding, even when the arg's
// actual runtime type differs. This is the reified-type-test pattern -
// the function takes `any` so the parameter accepts any value, and `T`
// is used purely for the runtime check inside the body.
func TestParityExplicitTypeArgOverridesInference(t *testing.T) {
	runParity(t, `import io;

func assertIs<T>(any value): bool {
    return value instanceof T;
}

io.println(assertIs<string>("hello"));
io.println(assertIs<int>("hello"));
io.println(assertIs<int>(42));
io.println(assertIs<string>(42));
`, "true\nfalse\ntrue\nfalse\n")
}

// TestParityUserClassIterator verifies the 1.0.6 user-class iterator
// protocol: `for (x in obj)` calls obj.__iter() to get an iterator,
// then drives __done()/__next() per step. Both backends must produce
// the same sequence. Also covers a class that implements __next()
// directly (no __iter() method), in which case the instance is its
// own iterator.
func TestParityUserClassIterator(t *testing.T) {
	runParity(t, `import io;

class Range {
    int from;
    int to;
    int cur;

    func Range(int from, int to) {
        this.from = from;
        this.to = to;
        this.cur = from;
    }

    func __iter(): Range {
        this.cur = this.from;
        return this;
    }

    func __done(): bool {
        return this.cur >= this.to;
    }

    func __next(): int {
        int v = this.cur;
        this.cur = this.cur + 1;
        return v;
    }
}

class Steps {
    int n;
    int seen;

    func Steps(int n) {
        this.n = n;
        this.seen = 0;
    }

    func __done(): bool {
        return this.seen >= this.n;
    }

    func __next(): int {
        this.seen = this.seen + 1;
        return this.seen * 10;
    }
}

for (n in Range(2, 5)) {
    io.println(n);
}

for (n in Steps(3)) {
    io.println(n);
}
`, "2\n3\n4\n10\n20\n30\n")
}

// TestParityFunctionCallDoesNotBindToCaseFoldedClass guards a
// bytecode-compiler regression where `view(args)` in a module that
// also exports a `View` class was dispatching to the class
// constructor because identifier lookup was case-insensitive. The
// evaluator was always case-sensitive; the VM is now too.
func TestParityFunctionCallDoesNotBindToCaseFoldedClass(t *testing.T) {
	runParity(t, `import io;

class View {
    string label;
    func View(string label, list<int> nums) {
        this.label = label + ":" + (nums.length() as string);
    }
}

func view(string a, string b, dict<string, any> opts = {}): string {
    return "fn:" + a + ":" + b + ":" + (opts.keys().length() as string);
}

func wrap(string a, string b): string {
    return view(a, b, {"k": 1});
}

io.println(wrap("x", "y"));
io.println(View("hello", [1, 2, 3]).label);
`, "fn:x:y:1\nhello:3\n")
}

// TestParityCrossChunkInstanceFields guards a regression where
// `reflect.fields(instance)` on a value handed to a sub-module
// returned an empty list because the originating chunk's
// ClassInfo wasn't reachable from the sub-VM's classIndex. The
// instance's runtime.Class.Fields is now populated at
// construction time and carries the field decorators through any
// module boundary, so framework code (`@Groups` filtering in the
// gebweb framework, similar reflection-driven helpers) sees the
// originating class's annotations.
func TestParityCrossChunkInstanceFields(t *testing.T) {
	runParity(t, `import io;
import reflect;

class Box {
    @Groups("read", "admin") string id;
    string label;
}

func surfaceFieldsFromOtherFunction(any value): int {
    let fields = reflect.fields(value);
    int withGroups = 0;
    for (entry in fields) {
        let f = entry as dict<string, any>;
        if (!f.contains("decorators")) {
            continue;
        }
        for (d in f["decorators"] as list<any>) {
            let dec = d as dict<string, any>;
            if ((dec["name"] as string) == "Groups") {
                withGroups = withGroups + 1;
            }
        }
    }
    return withGroups;
}

let b = Box();
b.id = "b1";
b.label = "demo";
io.println(reflect.fields(b).length());
io.println(surfaceFieldsFromOtherFunction(b));
`, "2\n1\n")
}

// TestParityClassRefRuntimeConstruction guards a VM-only regression
// where `classRef()` via a variable (e.g. a class value carried
// through a function parameter or obtained from `reflect.class`)
// dispatched as a static-method call on `__invoke` rather than as
// construction. Required by gebweb's DI container which holds class
// refs in a dict and constructs them at resolve time.
func TestParityClassRefRuntimeConstruction(t *testing.T) {
	runParity(t, `import io;

class Box {
    int n;
    func Box(int n) { this.n = n; }
}

func makeOne(any cls): any {
    return cls(7);
}

let b = makeOne(Box) as Box;
io.println(b.n);
`, "7\n")
}

// TestParityOpAddStringStaticTyping guards the compile-time
// `OpAddString` specialisation: when both operands of `+` are
// statically typed `string`, the compiler emits a type-specialised
// opcode that skips the runtime type switch + magic-method dispatch.
// Verifies the static-string detection works through identifiers,
// literals, and nested concatenations.
func TestParityOpAddStringStaticTyping(t *testing.T) {
	runParity(t, `import io;

string a = "hello";
string b = " ";
string c = "world";
io.println(a + b + c);
io.println("foo" + "bar");

func greet(string name): string {
    return "hi " + name;
}
io.println(greet("ada"));

/* Untyped local with a string value still flows through the
 * generic OpAdd; the specialiser only fires for STATICALLY typed
 * operands. Both backends produce the same output regardless. */
let s = "x";
io.println(s + "y");
`, "hello world\nfoobar\nhi ada\nxy\n")
}

// TestParityStaticFunctionLifted guards the lifted compiler parity
// gap: `static func` declarations now compile directly to bytecode
// instead of falling back to the evaluator. Static methods can be
// called via `ClassName.method(...)`; static-const class members
// (a 1.0.2-era feature) work alongside them.
func TestParityStaticFunctionLifted(t *testing.T) {
	runParity(t, `import io;

class Counter {
    static const VERSION = "1.0";

    static func make(int start): Counter {
        let c = Counter();
        c.value = start;
        return c;
    }

    int value;
    func Counter() { this.value = 0; }

    func double(): int { this.value = this.value * 2; return this.value; }
}

let c = Counter.make(7);
io.println(c.double());
io.println(Counter.VERSION);

class Registry {
    static func register(string name): string { return "registered:" + name; }
}

io.println(Registry.register("widget"));
`, "14\n1.0\nregistered:widget\n")
}

func TestParityFieldLookupCacheAcrossClasses(t *testing.T) {
	runParity(t, `import io;

class A {
    int x;
    func A(int x) { this.x = x; }
}

class B {
    int x;
    func B(int x) { this.x = x; }
}

let a = A(1);
let b = B(2);
for (int i = 0; i < 50; i++) {
    a.x = a.x + 1;
    b.x = b.x + 10;
}
io.println(a.x);
io.println(b.x);
`, "51\n502\n")
}

func TestParityFieldLookupCacheWithGetMagic(t *testing.T) {
	runParity(t, `import io;

class WithGet {
    int n;
    func WithGet(int n) { this.n = n; }
    func __get(string name): int {
        return this.n * 100;
    }
}

let w = WithGet(3);
io.println(w.n);
io.println(w.dynamic);
io.println(w.other);
`, "3\n300\n300\n")
}

func TestParityFieldLookupCacheWithSetMagic(t *testing.T) {
	runParity(t, `import io;

class WithSet {
    dict<string, any> extras;
    func WithSet() { this.extras = {}; }
    func __set(string name, any value): void {
        this.extras[name] = value;
    }
}

let w = WithSet();
w.foo = 1;
w.bar = 2;
io.println(w.extras["foo"]);
io.println(w.extras["bar"]);
`, "1\n2\n")
}

// __string drives println and interpolation (not just `as string`); no __string falls back to Inspect.
func TestParityImplicitStringDunder(t *testing.T) {
	runParity(t, `import io;
class Tag {
    string name;
    func Tag(string n) { this.name = n; }
    func __string(): string { return "#" + this.name; }
}
class Plain { int x; func Plain(int x) { this.x = x; } }
let t = Tag("alpha");
io.println(t);
io.println("tag is ${t}");
io.print(t);
io.println("");
let p = Plain(1);
io.println("${p}".contains("Plain"));
`, "#alpha\ntag is #alpha\n#alpha\ntrue\n")
}

func TestParityStreamsCopyViaDunders(t *testing.T) {
	runParityWithStdlib(t, `import io;
import streams;

let src = streams.memory("the quick brown fox");
let dst = streams.memory();
let n = streams.copy(src, dst);
io.println(n);
io.println(dst.toString());
`, "19\nthe quick brown fox\n")
}

func TestParityStreamsUserDunderProtocol(t *testing.T) {
	runParityWithStdlib(t, `import io;
import streams;

class Chunked {
    int step;
    func Chunked() { this.step = 0; }
    func __read(int n): string {
        if (this.step == 0) { this.step = 1; return "hello "; }
        if (this.step == 1) { this.step = 2; return "world"; }
        return "";
    }
}

let dst = streams.memory();
streams.copy(Chunked(), dst);
io.println(dst.toString());
io.println(streams.readAll(Chunked()));
`, "hello world\nhello world\n")
}

// TestParityUserClassOpOverloadInStreamReduce exercises user-class
// operator overloads (__add, __eq) inside a streams.reduce / .anyMatch
// pipeline where the reducer closure is created in the main chunk and
// fired from inside the streams sub-VM. The 1.0.6 cross-chunk closure
// dispatch fix routes the closure back to its declaring chunk so the
// __add / __eq dispatches happen in the main-chunk VM, where the user
// class lives.
//
// Companion to TestParityStreamsUserDunderProtocol, which covers the
// methodCall cross-chunk path (`vm.go:~10465`). The prefix-op, __eq,
// and binary-op guards at vm.go:~2520 / ~2647 / ~3307 are also
// relaxed to `instance.Class.Module != vm.moduleName` for symmetry,
// but no current stdlib code path evaluates a magic op directly on a
// user-class instance from inside a sub-VM. The relaxation is
// defensive; this test locks in the closure-dispatch route that real
// stream pipelines exercise.
func TestParityUserClassOpOverloadInStreamReduce(t *testing.T) {
	runParityWithStdlib(t, `import io;
import streams;

class Money {
    int cents;
    func Money(int c) { this.cents = c; }
    func __add(any other): Money {
        return Money(this.cents + (other as Money).cents);
    }
    func __eq(any other): bool {
        if (!(other instanceof Money)) { return false; }
        return this.cents == (other as Money).cents;
    }
    func __string(): string {
        return "$" + (this.cents as string);
    }
}

let total = streams.of([Money(100), Money(250), Money(75)])
    .reduce(Money(0), func(any acc, any x): any {
        return (acc as Money) + (x as Money);
    });
io.println(total as string);

let hasExact = streams.of([Money(10), Money(20), Money(30)])
    .anyMatch(func(any x): bool { return (x as Money) == Money(20); });
io.println(hasExact);

let missing = streams.of([Money(10), Money(20), Money(30)])
    .anyMatch(func(any x): bool { return (x as Money) == Money(999); });
io.println(missing);
`, "$425\ntrue\nfalse\n")
}

// A class implementing the cross-module maps.DictInterface gets the dict
// method surface (and `in`) via interface defaults that call sibling defaults
// across the module boundary - identical on both backends.
func TestParityDictInterface(t *testing.T) {
	runParityWithStdlib(t, `import io;
import maps;
class M implements maps.DictInterface {
    dict<string, any> store;
    func M() { this.store = {}; }
    func __index(any key): any { return this.store.get(key as string); }
    func __setIndex(any key, any value): void { this.store.set(key as string, value); }
    func keys(): list<any> { return this.store.keys(); }
}
let m = M();
m["a"] = 1;
m["b"] = 2;
io.println(m["a"]);
io.println(m.contains("a"));
io.println(m.get("z", "fallback"));
io.println("a" in m);
io.println("z" in m);
io.println(m.length());
io.println(m.isEmpty());
`, "1\ntrue\nfallback\ntrue\nfalse\n2\nfalse\n")
}

// Subscript dunders: a class with __index/__setIndex is index-able with
// [], identically on both backends.
func TestParitySubscriptDunders(t *testing.T) {
	runParity(t, `import io;
class Bag {
    dict<string, any> data;
    func Bag() { this.data = {}; }
    func __index(string k): any { return this.data.get(k); }
    func __setIndex(string k, any v): void { this.data.set(k, v); }
}
let b = Bag();
b["x"] = 10;
b["y"] = 20;
io.println(b["x"]);
io.println(b["y"]);
io.println(b["z"]);
`, "10\n20\nnull\n")
}

// Builtin type static methods (bytes.fromString, string.fromCodePoint, ...)
// resolve without an import on both backends.
func TestParityTypeStaticsWithoutImport(t *testing.T) {
	runParity(t, `import io;
io.println(bytes.fromString("a") as string);
io.println(bytes.fromList([97, 98, 99]) as string);
io.println(string.fromCodePoint(65));
io.println(string.fromCodePoints([72, 105]));
`, "a\nabc\nA\nHi\n")
}

func TestParityStoreClass(t *testing.T) {
	runParityWithStdlib(t, `
import store;
import io;
let s = store.Store();
s.set("cfg", {"n": 1});
let got = s.get("cfg") as dict<string, any>;
got["n"] = 99;
io.println((s.get("cfg") as dict<string, any>)["n"]);
io.println(s.incr("hits"));
io.println(s.incr("hits"));
let r = s.update("total", func(any old): any { return (old == null ? 0 : old as int) + 10; });
io.println(r);
`, "1\n1\n2\n10\n")
}

// TestParityModuleVsClassCaseSensitivity guards the compiler fix that a
// module-qualified call (lowercase `store`) must not bind to a same-spelled
// class (`Store`) in scope on the VM (the evaluator was always case-sensitive).
func TestParityModuleVsClassCaseSensitivity(t *testing.T) {
	runParityWithStdlib(t, `
import store;
import io;
let s = store.Store();
s.set("k", 7);
io.println(s.get("k"));
io.println(store.has(store.new(), "k"));
`, "7\nfalse\n")
}

// Field-level @immutable: set-once fields are writable during construction and
// locked afterwards; mutable fields stay writable; a default on an @immutable
// field is rejected. Eval/VM parity.
func TestParityImmutableFields(t *testing.T) {
	// Set in constructor, read back; mutable sibling still writable.
	runParity(t, `import io;
class U {
    @immutable string id;
    string name;
    func U(string id, string n) { this.id = id; this.name = n; }
}
let u = U("a1", "Ada");
u.name = "Ada L.";
io.println(u.id + " " + u.name);
`, "a1 Ada L.\n")

	// Rewrite within the constructor is allowed (free during construction).
	runParity(t, `import io;
class U {
    @immutable string id;
    func U() { this.id = "first"; this.id = "second"; }
}
io.println(U().id);
`, "second\n")

	// Write after construction throws.
	runParity(t, `import io;
class U {
    @immutable string id;
    func U(string id) { this.id = id; }
}
let u = U("a1");
try { u.id = "x"; io.println("MUTATED"); } catch (Error e) { io.println("blocked"); }
`, "blocked\n")

	// An immutable field inherited from a parent is locked too.
	runParity(t, `import io;
class Base { @immutable string id; func Base(string id) { this.id = id; } }
class Sub extends Base { string extra; func Sub(string id) { parent(id); this.extra = "x"; } }
let s = Sub("p1");
try { s.id = "z"; io.println("MUTATED"); } catch (Error e) { io.println("blocked"); }
`, "blocked\n")

	// A subclass cannot rewrite a parent's @immutable field after parent(): the
	// parent constructor locks it the moment it completes.
	runParity(t, `import io;
class Base { @immutable string id; func Base(string id) { this.id = id; } }
class Sub extends Base { func Sub() { parent("p"); try { this.id = "q"; io.println("MUTATED"); } catch (Error e) { io.println("blocked"); } } }
Sub();
`, "blocked\n")

	// Auto parent constructor (no explicit parent() call) still locks the
	// parent's @immutable field once the subclass instance is built.
	runParity(t, `import io;
class Base { @immutable string id; func Base() { this.id = "auto"; } }
class Sub extends Base { string extra; func Sub() { this.extra = "x"; } }
let s = Sub();
io.println(s.id);
try { s.id = "z"; io.println("MUTATED"); } catch (Error e) { io.println("blocked"); }
`, "auto\nblocked\n")
}

// A cross-module instance dispatches its cast/__string dunders on both backends
// (println, interpolation, and `as` cast).
func TestParityCrossModuleStringDunder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "money.gb"), []byte(`module money;
export class Money {
    int cents;
    func Money(int cents) { this.cents = cents; }
    func __string(): string { return "$" + (this.cents as string); }
    func __int(): int { return this.cents; }
}
`), 0o644); err != nil {
		t.Fatalf("write money: %v", err)
	}
	source := `import io;
import money;
let m = money.Money(7);
io.println(m);
io.println("paid ${m}");
io.println(m as string);
io.println(m as int);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	const want = "$7\npaid $7\n$7\n7\n"
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: %q want %q", evOut.String(), want)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: %q want %q", vmOut.String(), want)
	}
}

// A cross-module inherited @immutable field locks after the parent ctor runs, on both backends.
func TestParityCrossModuleImmutableField(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Account {
    @immutable string id;
    func Account(string id) { this.id = id; }
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	source := `import io;
import base as basemod;
class Savings extends basemod.Account {
    string label;
    func Savings(string id) { parent(id); this.label = "s"; }
}
let s = Savings("acct-1");
io.println(s.id);
try { s.id = "tampered"; io.println("MUTATED"); } catch (Error e) { io.println("blocked"); }
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	const want = "acct-1\nblocked\n"
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: %q want %q", evOut.String(), want)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: %q want %q", vmOut.String(), want)
	}
}

func TestParitySpreadIntoConstructorsMethodsStatics(t *testing.T) {
	runParity(t, `import io;
class P {
    int a; int b;
    func P(int a, int b = 9) { this.a = a; this.b = b; }
    func m(int a, int b = 9): string { return "${a}|${b}"; }
    static func sm(int a, int b = 9): string { return "${a}|${b}"; }
}
let p = P(...[3, 4]);
io.println("${p.a}/${p.b}");
let q = P(...{"a": 5, "junk": 1});
io.println("${q.a}/${q.b}");
io.println(P(1, ...{"b": 7}).b);
let r = P(0);
io.println(r.m(...[1, 2]));
io.println(r.m(...[1]));
io.println(r.m(...{"a": 5}));
io.println(r.m(...{"a": 5, "b": 6, "junk": 2}));
io.println(r.m(1, ...{"b": 7}));
io.println(P.sm(...[1, 2]));
io.println(P.sm(...{"a": 8}));
io.println(P.sm(2, ...{"b": 3}));
`, "3/4\n5/9\n7\n1|2\n1|9\n5|9\n5|6\n1|7\n1|2\n8|9\n2|3\n")
}

func TestParityRangeFirstLastFields(t *testing.T) {
	runParity(t, `import io;
let r = 1..5;
io.println(r.first);
io.println(r.last);
io.println((2..<8).last);
io.println((5..1).first ?? "empty");
io.println((5..1).last ?? "empty");
io.println(r.first());
io.println(r.last());
io.println(r.length);
`, "1\n5\n7\nempty\nempty\n1\n5\n5\n")
}

func TestParityDerivedComparisonDunders(t *testing.T) {
	runParity(t, `import io;
class LT { int x; func LT(int x) { this.x = x; } func __lt(LT o): bool { return this.x < o.x; } }
class GT { int x; func GT(int x) { this.x = x; } func __gt(GT o): bool { return this.x > o.x; } }
io.println(LT(1) < LT(2));
io.println(LT(2) > LT(1));
io.println(LT(1) > LT(2));
io.println(LT(1) <= LT(2));
io.println(LT(2) <= LT(1));
io.println(LT(2) >= LT(1));
io.println(LT(1) >= LT(2));
io.println(GT(1) < GT(2));
io.println(GT(2) > GT(1));
io.println(GT(1) <= GT(2));
io.println(GT(2) >= GT(1));
class Both {
    int x;
    func Both(int x) { this.x = x; }
    func __lt(Both o): bool { return this.x < o.x; }
    func __gt(Both o): bool { return false; }
}
io.println(Both(2) > Both(1));
`, "true\ntrue\nfalse\ntrue\nfalse\ntrue\nfalse\ntrue\ntrue\ntrue\ntrue\nfalse\n")
}
