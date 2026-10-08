package bytecode_test

import "testing"

// Explicit constructor type arguments constrain T-typed parameters at
// runtime on both backends (Box<string>(42) throws). Inference and
// covariant passing stay accepted.

func TestParityGenericCtorExplicitTypeArgMismatch(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
try {
    let b = Box<string>(42);
    io.println("constructed");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "caught: Box expects T for parameter 'value', got int\n")
}

func TestParityGenericCtorExplicitTypeArgValid(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
let b = Box<string>("hello");
io.println(typeof(b.value));
io.println("${reflect.typeBindings(b)}");
`, "string\n{\"T\": \"string\"}\n")
}

func TestParityGenericCtorInferenceUnconstrained(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
let b = Box(42);
io.println(typeof(b.value));
`, "int\n")
}

func TestParityGenericCtorExplicitCovariant(t *testing.T) {
	runParity(t, `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Pen<T> {
    T occupant;
    func Pen(T occupant) { this.occupant = occupant; }
}
let p = Pen<Animal>(Dog());
io.println(typeof(p.occupant));
`, "Dog\n")
}

func TestParityGenericCtorSubclassExplicitTypeArgMismatch(t *testing.T) {
	runParity(t, `import io;
class Base<T> {
    T value;
    func Base(T value) { this.value = value; }
}
class Sub<T> extends Base<T> {
    func Sub(T value) { parent(value); }
}
try {
    let s = Sub<string>(42);
    io.println("constructed");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "caught: Sub expects T for parameter 'value', got int\n")
}

func TestParityGenericCtorExplicitSecondParam(t *testing.T) {
	runParity(t, `import io;
class Pair<K, V> {
    K key;
    V value;
    func Pair(K key, V value) {
        this.key = key;
        this.value = value;
    }
}
try {
    let p = Pair<string, int>("k", "not-int");
    io.println("constructed");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
let ok = Pair<string, int>("k", 7);
io.println(typeof(ok.value));
`, "caught: Pair expects V for parameter 'value', got string\nint\n")
}

func TestParityGenericMethodParamEnforced(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func put(T value): void { this.value = value; }
}
let b = Box<string>("hello");
try {
    b.put(42);
    io.println("accepted");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
b.put("fine");
io.println(b.value);
`, "caught: Box.put expects T for parameter 'value', got int\nfine\n")
}

func TestParityGenericMethodParamCovariantAndOpen(t *testing.T) {
	runParity(t, `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Pen<T> {
    T occupant;
    func Pen(T occupant) { this.occupant = occupant; }
    func admit(T next): void { this.occupant = next; }
    func tag<U>(U label): string { return typeof(label) as string; }
}
let p = Pen<Animal>(Dog());
p.admit(Dog());
io.println(typeof(p.occupant));
io.println(p.tag(42));
io.println(p.tag("x"));
`, "Dog\nint\nstring\n")
}

func TestParityGenericMethodExtendsClauseBindings(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func put(T value): void { this.value = value; }
}
class IntBox extends Box<int> {
    func IntBox(int value) { parent(value); }
}
let ib = IntBox(7);
try {
    ib.put("nope");
    io.println("accepted");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
ib.put(9);
io.println(ib.value);
`, "caught: Box.put expects T for parameter 'value', got string\n9\n")
}

func TestParityGenericMethodInferredInstanceUnconstrained(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func put(T value): void { this.value = value; }
}
let b = Box(42);
b.put(7);
io.println(b.value);
`, "7\n")
}

func TestParityGenericDeclarationAnnotationEnforced(t *testing.T) {
	// A declaration annotation over a direct constructor call is
	// explicit type arguments by another spelling: a contradicting
	// argument throws at the construct site (routed through `any` so
	// the runtime path is pinned), a matching one constructs with the
	// annotation's bindings, and method calls stay constrained by them.
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func put(T value): void { this.value = value; }
}
any raw = "not an int";
try {
    Box<int> wrong = Box(raw);
    io.println("accepted: ${typeof(wrong.value)}");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
Box<int> ok = Box(5);
io.println(ok instanceof Box<int>);
try {
    ok.put("still wrong");
    io.println("accepted");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
ok.put(9);
io.println(ok.value);
`, "caught: Box expects T for parameter 'value', got string\ntrue\ncaught: Box.put expects T for parameter 'value', got string\n9\n")
}

func TestParityGenericCtorOverloadResolvedByExplicitTypeArgs(t *testing.T) {
	// Explicit type args break overload-selection ties: Box<string>(42)
	// resolves to Box(int value) (T=string excludes the int), a string
	// resolves to the generic constructor, and an argument matching
	// neither reports the precise per-parameter mismatch against the
	// surviving generic candidate.
	runParity(t, `import io;
class Box<T> {
    T value;
    string which;
    func Box(T value) {
        this.value = value;
        this.which = "generic";
    }
    func Box(int value) {
        this.value = value;
        this.which = "int";
    }
}
let a = Box<string>(42);
io.println("a: ${a.which}");
let b = Box<string>("hi");
io.println("b: ${b.which}");
any raw = true;
try {
    let c = Box<string>(raw);
    io.println("c: ${c.which}");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "a: int\nb: generic\ncaught: Box expects T for parameter 'value', got bool\n")
}

func TestParityGenericInstanceofBindings(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
class IntBox extends Box<int> {
    func IntBox(int value) { parent(value); }
}
let s = Box<string>("hi");
io.println("${s instanceof Box<string>} ${s instanceof Box<int>} ${s instanceof Box}");
let inf = Box(42);
io.println("${inf instanceof Box<int>} ${inf instanceof Box<string>}");
let ib = IntBox(7);
io.println("${ib instanceof Box<int>} ${ib instanceof IntBox} ${ib instanceof Box<string>}");
`, "true false true\ntrue false\ntrue true false\n")
}

func TestParityGenericInstanceofFrameBoundArg(t *testing.T) {
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
func check<T>(any v): bool {
    return v instanceof Box<T>;
}
let s = Box<string>("hi");
io.println("${check<string>(s)} ${check<int>(s)}");
`, "true false\n")
}

func TestParityGenericInstanceofMultiParam(t *testing.T) {
	runParity(t, `import io;
class Pair<K, V> {
    K key;
    V value;
    func Pair(K key, V value) {
        this.key = key;
        this.value = value;
    }
}
let p = Pair<string, int>("k", 7);
io.println("${p instanceof Pair<string, int>} ${p instanceof Pair<string, string>} ${p instanceof Pair<string>}");
`, "true false true\n")
}

func TestParityAnyReturnIntoTypedContext(t *testing.T) {
	// An any-returning call is statically opaque: it must compile into
	// any typed context (param, generic ctor arg, declaration) and be
	// validated at runtime - the VM compiler previously rejected it
	// via the expected-return overload filter while the evaluator ran it.
	runParity(t, `import io;
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
func opaque(any v): any { return v; }
func wantInt(int n): int { return n; }

io.println(wantInt(opaque(42)));
try {
    io.println(wantInt(opaque("nope")));
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
let b = Box<string>(opaque("hi"));
io.println(b.value);
int n = opaque(7);
io.println(n);
`, "42\ncaught: wantInt expects int for parameter 'n', got string\nhi\n7\n")
}

// Explicit type args on a generic function call enforce the binding at runtime on both backends (a dynamic 'any' arg reaches the runtime check).
func TestParityGenericFuncExplicitTypeArgRuntimeMismatch(t *testing.T) {
	runParity(t, `import io;
func identity<T>(T value): T { return value; }
any x = 42;
try {
    let r = identity<string>(x);
    io.println("no throw");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "caught: identity expects T for parameter 'value', got int\n")
}

func TestParityGenericFuncExplicitTypeArgValid(t *testing.T) {
	runParity(t, `import io;
func identity<T>(T value): T { return value; }
io.println(identity<string>("ok"));
`, "ok\n")
}

// Explicit type args on a statically-resolvable generic method call now enforce at runtime on both backends (the VM previously ignored them).
func TestParityGenericResolvedMethodExplicitTypeArgRuntimeMismatch(t *testing.T) {
	runParity(t, `import io;
class Repo { func pick<T>(T value): T { return value; } }
any x = 42;
try {
    let r = Repo().pick<string>(x);
    io.println("no throw");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "caught: Repo.pick expects T for parameter 'value', got int\n")
}

func TestParityGenericResolvedMethodExplicitTypeArgValid(t *testing.T) {
	runParity(t, `import io;
class Repo { func pick<T>(T value): T { return value; } }
io.println(Repo().pick<string>("ok"));
`, "ok\n")
}
