package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"testing"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityTypeOf(t *testing.T) {
	runParity(t, `import io;
import reflect;
io.println(typeof(42));
io.println(typeof("hello"));
io.println(typeof(true));
io.println(typeof(null));
io.println(reflect.typeOf([1, 2]));
io.println(reflect.typeOf({"a": 1}));
`, "int\nstring\nbool\nnull\nlist\ndict\n")
}

func TestParityInstanceOf(t *testing.T) {
	runParity(t, `import io;
class Foo {}
class Bar extends Foo {}
Bar b = Bar();
io.println(b instanceof Bar);
io.println(b instanceof Foo);
`, "true\ntrue\n")
}

func TestParityScalarCasts(t *testing.T) {
	runParity(t, `import io;
io.println(("42" as int));
io.println((99 as string));
io.println((true as string));
io.println((false as string));
`, "42\n99\ntrue\nfalse\n")
}

func TestParityStringInterpolationViaCast(t *testing.T) {
	runParity(t, `import io;
int n = 42;
bool b = true;
io.println("n=" + (n as string) + " b=" + (b as string));
`, "n=42 b=true\n")
}

func TestParityNullCoalesce(t *testing.T) {
	runParity(t, `import io;
let a = null;
let b = "hello";
io.println(a ?? "default");
io.println(b ?? "default");
`, "default\nhello\n")
}

func TestParityTypeAliases(t *testing.T) {
	runParity(t, `import io;
type UserId = string;
type Money = decimal;
type Numbers = int[];
UserId id = "u-1";
Money price = 12.5;
Numbers nums = [1, 2, 3];
func label(UserId value): UserId { return "id:" + value; }
io.println(label(id));
io.println(price.format(2));
io.println(nums.length() as string);
io.println(("42" as UserId).toInt() + 1);
`, "id:u-1\n12.50\n3\n43\n")
}

func TestParityInstanceofListAnyAndUnion(t *testing.T) {
	runParity(t, `import io;
let mixed = [1, "f", true];
let strs = ["a", "b", "c"];
io.println(mixed instanceof list<any>);
io.println(strs instanceof list<any>);
io.println(mixed instanceof list<string|bool|int>);
io.println(mixed instanceof list<string>);
io.println(strs instanceof list<string>);
io.println([] instanceof list<any>);
io.println({"a": 1, "b": 2} instanceof dict<string, any>);
io.println({"a": 1, "b": "x"} instanceof dict<string, int|string>);
`, "true\ntrue\ntrue\nfalse\ntrue\ntrue\ntrue\ntrue\n")
}

func TestParityInstanceofTaggedSatisfiesAnyAndUnion(t *testing.T) {
	runParity(t, `import io;
list<int> xs = [1, 2, 3];
io.println(xs instanceof list<int>);
io.println(xs instanceof list<any>);
io.println(xs instanceof list<int|string>);
io.println(xs instanceof list<string>);
`, "true\ntrue\ntrue\nfalse\n")
}

func TestParityNullableChainMultiHop(t *testing.T) {
	runParity(t, `import io;
class Inner {
    string value;
    func Inner(string v) { this.value = v; }
    func upper() { return this.value.upper(); }
}
class Outer {
    Inner inner;
    func Outer(Inner i) { this.inner = i; }
    func getInner() { return this.inner; }
}
let o = Outer(Inner("hello"));
io.println(o?.inner?.value);
io.println(o?.getInner()?.upper());
`, "hello\nHELLO\n")
}

func TestParityNullableChainShortCircuit(t *testing.T) {
	runParity(t, `import io;
class Node {
    string name;
    func Node(string n) { this.name = n; }
    func upper() { return this.name.upper(); }
}
let n = null;
let result = n?.upper();
if (result == null) { io.println("null"); } else { io.println(result); }
`, "null\n")
}

func TestParityNullableChainMidNull(t *testing.T) {
	runParity(t, `import io;
class Wrapper {
    func Wrapper() {}
    func getNull() { return null; }
}
let w = Wrapper();
let result = w?.getNull()?.upper();
if (result == null) { io.println("null"); } else { io.println(result); }
`, "null\n")
}

func TestParityCompoundAssignNullCoalesce(t *testing.T) {
	runParity(t, `import io;
let n = null;
n ??= "default";
io.println(n);
let m = "existing";
m ??= "other";
io.println(m);
`, "default\nexisting\n")
}

// TestParityTypeofStringComparison verifies typeof(x) compares equal to a
// type-name string on both backends, while still comparing to a type value.
func TestParityTypeofStringComparison(t *testing.T) {
	runParity(t, `
import io;
class Foo { int x; func Foo() { this.x = 1; } }
io.println(typeof(5) == "int");
io.println(typeof("hi") == "string");
io.println(typeof([1]) == "list");
io.println(typeof(true) == "bool");
io.println(typeof(Foo()) == "Foo");
io.println("int" == typeof(5));
io.println(typeof(5) != "string");
io.println(typeof(5) == int);
`, "true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\n")
}

// TestParityCastBool covers the J7 numeric -> bool cast.
func TestParityCastBool(t *testing.T) {
	runParity(t, `import io;
io.println(1 as bool);
io.println(0 as bool);
io.println(-7 as bool);
io.println(3.14 as bool);
io.println(0.0 as bool);
io.println("true" as bool);
io.println("false" as bool);
io.println(null as bool);
`, "true\nfalse\ntrue\ntrue\nfalse\ntrue\nfalse\nfalse\n")
}

// TestParityCastTruncatesDecimalAndFloat guards the cast policy
// agreed for 1.0.2: `decimal as int` and `float as int` truncate
// toward zero instead of erroring on non-integer values. Matches
// the C/Java/Go integer-cast convention.
func TestParityCastTruncatesDecimalAndFloat(t *testing.T) {
	runParity(t, `import io;

io.println(2.7 as int);
io.println(-2.7 as int);
io.println(2.0 as int);
io.println(true as int);
io.println(false as int);
io.println((3.99 as decimal) as int);
io.println(((-3.99) as decimal) as int);
`, "2\n-2\n2\n1\n0\n3\n-3\n")
}

// TestParitySmallIntCastsToDecimalAndFloat guards a regression where
// the evaluator's `int as decimal` and `int as float` paths only
// handled runtime.Int and rejected runtime.SmallInt with
// "cannot cast int to decimal". Method results that produce SmallInt
// (list.length, string.length, range iterators, ...) are the common
// trigger.
func TestParitySmallIntCastsToDecimalAndFloat(t *testing.T) {
	runParity(t, `import io;
let xs = ["a", "b", "c"];
io.println(xs.length() as decimal);
io.println(xs.length() as float);
any v = xs.length();
io.println(v as decimal);
io.println(v as float);
`, "3.0000000000\n3\n3.0000000000\n3\n")
}

// TestParityCrossTypeCastsForBytesAndCollections guards the 1.0.2
// cast extensions: `string <-> bytes` round-trip UTF-8 (errors on
// invalid byte sequences); `list as set<T>` de-duplicates; and
// `set as list<T>` materializes. Pre-1.0.2 each raised "cannot
// cast X to Y" on both backends; the runtime parity was already
// good (both errored identically), the change is the behaviour.
func TestParityCrossTypeCastsForBytesAndCollections(t *testing.T) {
	runParity(t, `import io;

let b = "hello" as bytes;
io.println(b.length);
io.println(b as string);

let u = "résumé" as bytes;
io.println(u.length);
io.println(u as string);

let dedup = [1, 1, 2, 3, 3] as set<int>;
io.println(dedup.length);

let materialized = {1, 2, 3} as list<int>;
io.println(materialized.length);
`, "5\nhello\n8\nrésumé\n3\n3\n")
}

// TestParityNullAsNullableType guards `null as ?T` working on
// both backends. The evaluator's cast path used to drop the
// nullable bit from the target TypeRef before calling castValue,
// so the cast rejected null on the eval side while the VM
// accepted it after the 1.0.2 cast-error catchability work. The
// eval path now special-cases a nullable target ahead of the
// class-chain match.
func TestParityNullAsNullableType(t *testing.T) {
	runParity(t, `import io;

class Box {
    int x;
    func Box(int x) { this.x = x; }
}

let n = null;
let b = n as ?Box;
io.println(b == null);
let n2 = null as ?int;
io.println(n2 == null);
let n3 = null as ?string;
io.println(n3 == null);
`, "true\ntrue\ntrue\n")
}

// TestParityCastWidensToParentClass guards a VM-only regression where the
// `as` operator rejected widening an error-derived value (or instance)
// to a parent class declared in another module. Surfaced while writing
// the Gebweb hello-world example: the framework adapter does `e as
// errors.HttpException` against a thrown NotFoundError. Evaluator
// already walked the parent chain; VM did not.
func TestParityCastWidensToParentClass(t *testing.T) {
	runParity(t, `import io;

class A extends RuntimeError {
    func A(string m) { parent(m); }
}
class B extends A {
    func B(string m) { parent(m); }
}

let b = B("nope");
let a = b as A;
io.println(a instanceof A);
io.println(a instanceof B);

class P {}
class C extends P {}
let c = C();
let p = c as P;
io.println(p instanceof P);
io.println(p instanceof C);
`, "true\ntrue\ntrue\ntrue\n")
}

func TestParityCastDunders(t *testing.T) {
	runParity(t, `import io;

class Box {
    int n;
    func Box(int n) { this.n = n; }
    func __string(): string { return "Box(" + (this.n as string) + ")"; }
    func __int(): int { return this.n; }
    func __float(): float { return (this.n as float); }
    func __bool(): bool { return this.n != 0; }
    func __decimal(): decimal { return (this.n as decimal); }
    func __bytes(): bytes { return ("Box(" + (this.n as string) + ")") as bytes; }
}

let b = Box(42);
io.println(b as string);
io.println(b as int);
io.println(b as float);
io.println(b as bool);
io.println(b as decimal);
io.println((b as bytes) as string);
io.println(Box(0) as bool);
`, "Box(42)\n42\n42\ntrue\n42.0000000000\nBox(42)\nfalse\n")
}

// TestParityUnionTypeAcceptsAndRejects walks T | U | V at
// parameter and return positions on both backends. Each accept
// path returns the matching branch's value; the reject path
// goes through xs[0] (statically opaque element type) and is
// caught by a user-level try/catch - confirming the VM throws
// param-validation errors as catchable RuntimeError (matching
// the evaluator) rather than fatally aborting.
func TestParityUnionTypeAcceptsAndRejects(t *testing.T) {
	runParity(t, `import io;

func tag(int | string | bool v): string {
    if (v instanceof int)    { return "int "  + (v as string); }
    if (v instanceof string) { return "str "  + (v as string); }
    return "bool " + (v as string);
}

func pickInt(): int | string { return 1; }
func pickStr(): int | string { return "x"; }

io.println(tag(42));
io.println(tag("ada"));
io.println(tag(true));
io.println(pickInt() as string);
io.println(pickStr() as string);

let xs = [1.5];
try {
    io.println(tag(xs[0]));
} catch (RuntimeError e) {
    io.println("rejected");
}
`, "int 42\nstr ada\nbool true\n1\nx\nrejected\n")
}

// instanceof against a builtin type name that also names an imported module, in expression position.
func TestParityInstanceofBuiltinTypeCollidingWithModule(t *testing.T) {
	runParity(t, `import io;
import bytes;
import string;

func isBytes(any v): bool { return v instanceof bytes; }
func isString(any v): bool { let r = v instanceof string; return r; }
func show(bool b): void { io.println(b); }

let b = bytes.fromHex("0a0b") as any;
let s = "hi" as any;
let n = 42 as any;

io.println(isBytes(b));
io.println(isBytes(s));
io.println(isString(s));
io.println(isString(n));
show(b instanceof bytes);
show(n instanceof string);
`, "true\nfalse\ntrue\nfalse\ntrue\nfalse\n")
}
