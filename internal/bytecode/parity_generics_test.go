package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
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

func TestParityReflectTypeBindings(t *testing.T) {
	runParity(t, `import io;
import reflect;

class Box<T> {
    T value;
    func Box(T v) { this.value = v; }
}

Box<string> b = Box("hello");
Box<int> n = Box(42);
io.println(reflect.typeBindings(b)["T"]);
io.println(reflect.typeBindings(n)["T"]);
`, "string\nint\n")
}

// An explicit constructor type arg naming an enclosing generic function's type parameter resolves through to the concrete binding.
func TestParityExplicitCtorTypeArgResolvesThroughT(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Box<T> {
    T item;
    func Box(T item) { this.item = item; }
}
func wrap<T>(T v): Box<T> {
    return Box<T>(v);
}
io.println(reflect.typeBindings(wrap("x"))["T"]);
io.println(reflect.typeBindings(wrap(42))["T"]);
`, "string\nint\n")
}

// An explicit type arg on a dynamically-dispatched generic method (`this.m<T>()`) binds T so `v instanceof T` resolves on both backends.
func TestParityGenericMethodExplicitTypeArgInstanceof(t *testing.T) {
	runParity(t, `import io;
class Config {}
class Checker {
    func isType<T>(any v): bool { return v instanceof T; }
    func check(): bool { return this.isType<Config>(Config()); }
}
io.println(Checker().check());
`, "true\n")
}

func TestParityGenericGeneratorKeepsTypeBindings(t *testing.T) {
	runParity(t, `import io;
func checks<T>(T value): generator<bool> {
    yield value instanceof T;
}

for (ok in checks("hello")) {
    io.println(ok);
}
for (ok in checks(42)) {
    io.println(ok);
}
`, "true\ntrue\n")
}

func TestParityMatchTypeBinding(t *testing.T) {
	runParity(t, `import io;
func describe(any v): string {
    return match(v) {
        case int n => "int:" + (n as string);
        case string s => "str:" + s;
        default => "other";
    };
}
io.println(describe(42));
io.println(describe("hi"));
io.println(describe(true));
`, "int:42\nstr:hi\nother\n")
}

// Generic FUNCTION call-site inference must project the inferred type
// argument through to instances the body constructs (roadmap post-1.0
// known issue; the VM already did this, the evaluator left bare "T").
func TestParityGenericFunctionCallSiteInference(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Pair<A, B> {
    A first;
    B second;
    func Pair(A first, B second) {
        this.first = first;
        this.second = second;
    }
}
func make<T>(T v): Pair<T, T> {
    return Pair(v, v);
}
io.println("${reflect.typeBindings(make("hello"))}");
io.println("${reflect.typeBindings(make(42))}");
io.println("${reflect.typeBindings(make<float>(1.5 as float))}");
`, "{\"A\": \"string\", \"B\": \"string\"}\n{\"A\": \"int\", \"B\": \"int\"}\n{\"A\": \"float\", \"B\": \"float\"}\n")
}

func TestParityGenericAsyncFunctionKeepsTypeBindings(t *testing.T) {
	runParity(t, `import io;
async func check<T>(T value): bool {
    return value instanceof T;
}

io.println(await check("hello"));
io.println(await check(42));
`, "true\ntrue\n")
}

func TestParityDecoratedGenericAsyncFunction(t *testing.T) {
	runParity(t, `import io;
import reflect;

func logged(any next): any {
    return func(any value): any {
        io.println("calling");
        return next(value);
    };
}

@logged
async func identify<T>(T value): bool {
    return value instanceof T;
}

io.println(await identify("hello"));
io.println(await identify(42));
io.println(reflect.decorators(identify)[0]["name"]);
`, "calling\ntrue\ncalling\ntrue\nlogged\n")
}

func TestParityDecoratedGenericAsyncMethod(t *testing.T) {
	runParity(t, `import io;

func traced(any f): any {
    return func(): any {
        io.println("trace");
        return f();
    };
}

class Box<T> {
    T val;
    func Box(T v) { this.val = v; }

    @traced
    async func isT(): bool {
        return this.val instanceof T;
    }
}

Box<string> b = Box("hi");
io.println(await b.isT());
Box<int> bi = Box(7);
io.println(await bi.isT());
`, "trace\ntrue\ntrace\ntrue\n")
}

func TestParityGenericClassWithTypeErasure(t *testing.T) {
	runParity(t, `import io;

class Box<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func get(): T {
        return this.value;
    }

    func set(T v) {
        this.value = v;
    }
}

Box<string> b = Box("hello");
io.println(b.get());
b.set("world");
io.println(b.get());

Box<int> n = Box(42);
io.println(n.get());
`, "hello\nworld\n42\n")
}

func TestParityGenericClassMultipleTypeParams(t *testing.T) {
	runParity(t, `import io;

class Pair<A, B> {
    A first;
    B second;

    func Pair(A a, B b) {
        this.first = a;
        this.second = b;
    }

    func getFirst(): A {
        return this.first;
    }

    func getSecond(): B {
        return this.second;
    }

    func mapFirst<C>(func f): C {
        return f(this.first);
    }
}

Pair<string, int> p = Pair("abc", 3);
io.println(p.getFirst());
io.println(p.getSecond());
io.println(p.mapFirst(func(string s): string { return s + "!"; }));
`, "abc\n3\nabc!\n")
}

func TestParityGenericFunctionInstanceofTypeParam(t *testing.T) {
	runParity(t, `import io;

func check<T>(T value): bool {
    return value instanceof T;
}

io.println(check("hello"));
io.println(check(42));
io.println(check(true));
`, "true\ntrue\ntrue\n")
}

func TestParityGenericClassMethodInstanceofTypeParam(t *testing.T) {
	runParity(t, `import io;

class Container<T> {
    T item;

    func Container(T v) {
        this.item = v;
    }

    func isT(): bool {
        return this.item instanceof T;
    }
}

Container<string> cs = Container("hello");
io.println(cs.isT());

Container<int> ci = Container(42);
io.println(ci.isT());
`, "true\ntrue\n")
}

func TestParityGenericDecoratedMethodInheritsTypeBindings(t *testing.T) {
	runParity(t, `import io;

func id(any f): any {
    return func(): any { return f(); };
}

class Container<T> {
    T item;

    func Container(T v) {
        this.item = v;
    }

    @id
    func isT(): bool {
        return this.item instanceof T;
    }
}

Container<string> cs = Container("hello");
io.println(cs.isT());

Container<int> ci = Container(42);
io.println(ci.isT());
`, "true\ntrue\n")
}

func TestParityGenericMultiParamClassInstanceofTypeParams(t *testing.T) {
	runParity(t, `import io;

class Pair<A, B> {
    A first;
    B second;

    func Pair(A a, B b) {
        this.first = a;
        this.second = b;
    }

    func firstIsA(): bool {
        return this.first instanceof A;
    }

    func secondIsB(): bool {
        return this.second instanceof B;
    }
}

Pair<string, int> p = Pair("abc", 3);
io.println(p.firstIsA());
io.println(p.secondIsB());
`, "true\ntrue\n")
}

func TestParityGenericDeclarationAnnotationWinsOverInference(t *testing.T) {
	// The annotation pins the bindings (not inference), and since 1.20.0
	// it also validates the constructor arguments like explicit
	// call-site type args do; the mismatch is routed through `any` so
	// the runtime path is what's pinned here.
	runParity(t, `import io;

class Box<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func isT(): bool {
        return this.value instanceof T;
    }
}

Box<int> n = Box(42);
io.println(n.isT());

Box<string> s = Box("hello");
io.println(s.isT());

any raw = "not an int";
try {
    Box<int> wrong = Box(raw);
    io.println(wrong.isT());
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}
`, "true\ntrue\ncaught: Box expects T for parameter 'v', got string\n")
}

func TestParityGenericListParamInfersElementType(t *testing.T) {
	runParity(t, `import io;

func firstIs<T>(list<T> xs): bool {
    return xs[0] instanceof T;
}

io.println(firstIs(["hello", "world"]));
io.println(firstIs([1, 2, 3]));
io.println(firstIs([true, false]));
`, "true\ntrue\ntrue\n")
}

func TestParityGenericConstraintAcceptsImplementor(t *testing.T) {
	runParity(t, `import io;

interface Printable {
    func print(): string;
}

class Dog implements Printable {
    func print(): string {
        return "woof";
    }
}

func show<T implements Printable>(T item): string {
    return item.print();
}

Dog d = Dog();
io.println(show(d));
`, "woof\n")
}

func TestParityGenericConstraintRejectsNonImplementor(t *testing.T) {
	t.Helper()
	source := `import io;

interface Printable {
    func print(): string;
}

class Cat {
    func meow(): string {
        return "meow";
    }
}

func show<T implements Printable>(T item): string {
    return item.print();
}

Cat c = Cat();
show(c);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Error("evaluator: expected error for non-implementor, got nil")
	}

	src := []byte(source)
	chunk, err := bytecode.Compile(program, src, "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Error("vm: expected error for non-implementor, got nil")
	}
}

func TestParityGenericConstraintRejectsPrimitive(t *testing.T) {
	t.Helper()
	source := `interface Printable {
    func print(): string;
}

func show<T implements Printable>(T item): void {
}

show("not printable");
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Error("evaluator: expected error for primitive, got nil")
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Error("vm: expected error for primitive, got nil")
	}
}

func TestParityClassGenericConstraintRejectsNonImplementor(t *testing.T) {
	t.Helper()
	source := `interface Printable {
    func print(): string;
}

class Dog implements Printable {
    func print(): string { return "woof"; }
}

class Cat {
}

class Box<T implements Printable> {
    T value;
    func Box(T value) {
        this.value = value;
    }
}

Dog d = Dog();
Box<Dog> ok = Box(d);
Cat c = Cat();
Box<Cat> bad = Box(c);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Error("evaluator: expected error for non-implementor, got nil")
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Error("vm: expected error for non-implementor, got nil")
	}
}

func TestParityGenericConstraintPrecedenceMatchesEvaluator(t *testing.T) {
	t.Helper()
	source := `interface A {
    func a(): string;
}
interface B {
    func b(): string;
}
interface C {
    func c(): string;
}

class OnlyC implements C {
    func c(): string { return "c"; }
}

func accept<T implements A & B | C>(T item): void {
}

OnlyC c = OnlyC();
accept(c);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Error("evaluator: expected precedence constraint error, got nil")
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Error("vm: expected precedence constraint error, got nil")
	}
}

func TestParityGenericUnionConstraintAcceptsEither(t *testing.T) {
	runParity(t, `import io;

interface Swimmer {
    func swim(): string;
}

interface Runner {
    func run(): string;
}

class Fish implements Swimmer {
    func swim(): string { return "splash"; }
}

class Horse implements Runner {
    func run(): string { return "gallop"; }
}

func move<T implements Swimmer | Runner>(T animal): void {
    io.println("ok");
}

Fish f = Fish();
Horse h = Horse();
move(f);
move(h);
`, "ok\nok\n")
}

func TestParityGenericSetParamInfersElementType(t *testing.T) {
	runParity(t, `import io;

func isSetOf<T>(set<T> s, any val): bool {
    return val instanceof T;
}

io.println(isSetOf({"hello", "world"}, "x"));
io.println(isSetOf({1, 2, 3}, 42));
io.println(isSetOf({"hello"}, 99));
`, "true\ntrue\nfalse\n")
}

func TestParityGenericDictParamInfersKeyValueTypes(t *testing.T) {
	runParity(t, `import io;

func keyIs<K, V>(dict<K, V> d, any val): bool {
    return val instanceof K;
}

func valIs<K, V>(dict<K, V> d, any val): bool {
    return val instanceof V;
}

io.println(keyIs({"a": 1, "b": 2}, "x"));
io.println(keyIs({"a": 1}, 42));
io.println(valIs({"a": 1, "b": 2}, 99));
io.println(valIs({"a": 1}, "hello"));
`, "true\nfalse\ntrue\nfalse\n")
}

func TestParityGenericCommaConstraints(t *testing.T) {
	runParity(t, `import io;

interface Printable {
    func print(): string;
}

interface Saveable {
    func save(): string;
}

class Document implements Printable, Saveable {
    func print(): string { return "printing"; }
    func save(): string { return "saving"; }
}

func process<T implements Printable, Saveable>(T item): void {
    io.println(item.print());
    io.println(item.save());
}

Document d = Document();
process(d);
`, "printing\nsaving\n")
}

func TestParityGenericInterfaceWithTypeParams(t *testing.T) {
	runParity(t, `import io;

interface Container<T> {
    func get(): T;
}

class Box<T> implements Container<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func get(): T {
        return this.value;
    }
}

Box<string> b = Box("hello");
io.println(b.get());
`, "hello\n")
}

// TestParityNullableGeneric covers the fix for `?list<int> = null` which
// previously panicked on the VM and threw on the evaluator.
func TestParityNullableGeneric(t *testing.T) {
	runParity(t, `import io;
?list<int> xs = null;
io.println(xs);
xs = [1, 2, 3];
io.println(xs);
`, "null\n[1, 2, 3]\n")
}

// TestParityGenericInnerLambda covers the fix where a lambda declared
// inside a generic function's body could not reference the outer
// function's type parameter T directly. The captured type bindings
// now flow from the outer call frame into the closure value and into
// the lambda's call frame so the inner `T x` parameter resolves
// against the bound concrete type.
func TestParityGenericInnerLambda(t *testing.T) {
	runParity(t, `import io;
import collections;

interface Scored { func score(): int; }

class Player implements Scored {
    int rating;
    func Player(int r) { this.rating = r; }
    func score(): int { return this.rating; }
}

func topBy<T implements Scored>(list<T> items): T {
    return collections.maxBy(items, func(T x): int { return x.score(); });
}

let players = [Player(1), Player(3), Player(2)];
io.println(topBy(players).score());
`, "3\n")
}

// TestParityGenericNamedFunctionReference covers the symmetric case:
// passing a generic function as a value (via OpMakeClosure with zero
// upvalues) should still let it pick up the call-site type bindings of
// the surrounding generic frame.
func TestParityGenericNamedFunctionReference(t *testing.T) {
	runParity(t, `import io;

func identity<T>(T x): T { return x; }

func apply<T>(T value, func fn): T {
    return fn(value);
}

io.println(apply(42, identity));
io.println(apply("hi", identity));
`, "42\nhi\n")
}

// TestParityGenericTypeMismatch is a regression guard: when the
// substituted concrete type for T cannot accept the argument, an error
// must surface on both backends. Today the mismatch is detected by
// the runtime parameter-type check; this test pins that behaviour so
// the closure type-binding plumbing doesn't accidentally accept
// anything for T.
func TestParityGenericTypeMismatch(t *testing.T) {
	runErrorParity(t, `import io;

func box<T>(T x): T { return x; }

let asInt = func(int n): int { return n; };
io.println(asInt(box("hello")));
`, "expects int")
}

// TestParityGenericInheritedBindings verifies that a subclass declared
// as `extends Base<T1, T2, ...>` propagates the concrete type
// arguments to instance TypeBindings on both backends.
func TestParityGenericInheritedBindings(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Base<T> {
    T value;
    func Base(T v) { this.value = v; }
}
class StringBase extends Base<string> {
    func StringBase(string s) { parent(s); }
}
class GrandChild extends StringBase {
    func GrandChild(string s) { parent(s); }
}
let b = StringBase("hi");
io.println(reflect.typeBindings(b)["T"]);
let g = GrandChild("hi");
io.println(reflect.typeBindings(g)["T"]);
`, "string\nstring\n")
}

// TestParityGenericInheritedMultipleParams verifies a parent with two
// type parameters propagates both via the extends clause.
func TestParityGenericInheritedMultipleParams(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Pair<A, B> {
    A first;
    B second;
    func Pair(A a, B b) { this.first = a; this.second = b; }
}
class IntStringPair extends Pair<int, string> {
    func IntStringPair(int a, string b) { parent(a, b); }
}
let p = IntStringPair(1, "hi");
let b = reflect.typeBindings(p);
io.println(b["A"]);
io.println(b["B"]);
`, "int\nstring\n")
}

// TestParityExplicitGenericCallSyntax verifies that the call form
// `Identifier<Type>(args)` is recognised by the parser, threaded through the
// compiler, and produces equivalent output on both the evaluator and the VM.
// Before this was supported the parser misread the `<` as the less-than
// operator and the call evaluated as a chained comparison.
func TestParityExplicitGenericCallSyntax(t *testing.T) {
	runParity(t, `import io;
import reflect;

class Box<T> {
    func Box() {}
}

let b = Box<string>();
io.println(typeof(b) == Box);
io.println(reflect.typeBindings(b)["T"]);
`, "true\nstring\n")
}

// TestParityExplicitGenericCallOnFunction verifies the same call form on a
// generic free-standing function. Previously this parsed as a chained
// less-than comparison and crashed at runtime. The explicit binding here
// agrees with the inferred binding; the explicit-binding-overrides-inference
// case is covered by the class-construction parity test above.
func TestParityExplicitGenericCallOnFunction(t *testing.T) {
	runParity(t, `import io;

func identity<T>(T value): T {
    return value;
}

io.println(identity<string>("hello"));
io.println(identity<int>(42));
`, "hello\n42\n")
}

// TestParityGenericInvarianceRejectsMismatch verifies that both backends reject
// passing a generic instance whose reified bindings disagree with the
// parameter's declared bindings. The unsoundness this guards against is the
// classic one: widening Container<Sub> to Container<Base> and then having the
// callee insert a sibling Base subtype.
func TestParityGenericInvarianceRejectsMismatch(t *testing.T) {
	source := `class Base {}
class Sub extends Base {}
class Container<T> { func Container() {} }
func consume(Container<Base> c): void {}

let sub = Container<Sub>();
consume(sub);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	// Evaluator must reject the call at runtime.
	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected runtime error, got nil")
	}
	if !strings.Contains(evErr.Error(), "Container<Base>") {
		t.Fatalf("evaluator error did not mention parameter type: %v", evErr)
	}

	// VM must also reject.
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Fatalf("vm: expected runtime error, got nil")
	}
	if !strings.Contains(vmErr.Error(), "Container<Base>") {
		t.Fatalf("vm error did not mention parameter type: %v", vmErr)
	}
}

// TestParityGenericInvarianceAcceptsExactMatch verifies the runtime check
// does NOT over-reject: when the caller's reified bindings exactly match the
// parameter's declared bindings, the call goes through normally.
func TestParityGenericInvarianceAcceptsExactMatch(t *testing.T) {
	runParity(t, `import io;
class Base {}
class Container<T> { func Container() {} }
func consume(Container<Base> c): void {
    io.println("ok");
}

let bases = Container<Base>();
consume(bases);
`, "ok\n")
}

// TestParityVarianceErrorMessageIncludesReifiedBindings verifies that when
// a generic-class invariance violation is detected at runtime, both the
// evaluator and the VM include the offending value's reified type
// arguments in the "got X" segment of the error message, not just the
// bare class name. Before this was fixed the message read "got Container",
// which was unhelpful for diagnosing variance bugs - "got Container<Sub>"
// makes the mismatch immediately obvious.
func TestParityVarianceErrorMessageIncludesReifiedBindings(t *testing.T) {
	source := `class Base {}
class Sub extends Base {}
class Container<T> { func Container() {} }
func use(Container<Base> c): void {}

let sub = Container<Sub>();
use(sub);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected runtime error, got nil")
	}
	if !strings.Contains(evErr.Error(), "got Container<Sub>") {
		t.Fatalf("evaluator error did not mention reified Container<Sub>: %v", evErr)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Fatalf("vm: expected runtime error, got nil")
	}
	if !strings.Contains(vmErr.Error(), "got Container<Sub>") {
		t.Fatalf("vm error did not mention reified Container<Sub>: %v", vmErr)
	}
}

// TestParityCollectionTypeBindings verifies reflect.typeBindings on tagged
// list/dict/set values reads the declared element types on both backends.
func TestParityCollectionTypeBindings(t *testing.T) {
	runParity(t, `import io;
import json;
import reflect;
let list<int> xs = [1, 2, 3];
io.println(json.stringify(reflect.typeBindings(xs)));
let dict<string, int> d = {"a": 1};
let b = reflect.typeBindings(d);
io.println(b["K"]);
io.println(b["V"]);
let untagged = [4, 5];
io.println(json.stringify(reflect.typeBindings(untagged)));
`, "{\"T\":\"int\"}\nstring\nint\n{}\n")
}

// TestParityInstanceofGenericCollection verifies `instanceof list<int>`
// parses and dispatches on both backends, honouring the tag when set and
// walking elements when the value is untagged.
func TestParityInstanceofGenericCollection(t *testing.T) {
	runParity(t, `import io;
let list<int> xs = [1, 2, 3];
io.println(xs instanceof list<int>);
io.println(xs instanceof list<string>);
io.println([1, 2, 3] instanceof list<int>);
io.println([1, 2, "three"] instanceof list<int>);
let dict<string, int> d = {"a": 1};
io.println(d instanceof dict<string, int>);
io.println(d instanceof dict<string, string>);
`, "true\nfalse\ntrue\nfalse\ntrue\nfalse\n")
}

// TestParityNestedGenericInference pins the 1.0.6 fix where both
// backends walk parameter type-spec trees recursively to bind type
// params nested inside `list<dict<K, V>>` (etc). Previously only the
// outermost container level was inspected, leaving inner type params
// unbound.
func TestParityNestedGenericInference(t *testing.T) {
	runParity(t, `import io;
import reflect;

class Box<K, V> {
    list<dict<K, V>> rows;
    func Box(list<dict<K, V>> rows) { this.rows = rows; }
}

let b = Box([{"a": 1}]);
let bindings = reflect.typeBindings(b);
io.println(bindings["K"]);
io.println(bindings["V"]);
`, "string\nint\n")
}
