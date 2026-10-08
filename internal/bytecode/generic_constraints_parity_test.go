package bytecode_test

import "testing"

// Union/intersection generic constraints with primitive, interface, and
// class leaves; bare and `implements` spellings; function and class
// boundaries; canonical failure message.

func TestParityConstraintPrimitiveUnion(t *testing.T) {
	runParity(t, `import io;
func pick<T implements string|int>(T v): string { return "${typeof(v)}"; }
func bare<T string|int>(T v): string { return "${typeof(v)}"; }
io.println(pick(42));
io.println(pick("hi"));
io.println(bare(42));
try {
    io.println(bare(true));
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "int\nstring\nint\ncaught: type bool does not satisfy constraint string|int for type parameter T\n")
}

func TestParityConstraintInterfaceAndClassLeaves(t *testing.T) {
	runParity(t, `import io;
interface Scored { func score(): int; }
class Win implements Scored { func Win() {} func score(): int { return 1; } }
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
func scored<T implements Scored|Animal>(T v): string { return "${typeof(v)}"; }
io.println(scored(Win()));
io.println(scored(Dog()));
try {
    io.println(scored(42));
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "Win\nDog\ncaught: type int does not satisfy constraint Scored|Animal for type parameter T\n")
}

func TestParityConstraintClassBoundary(t *testing.T) {
	runParity(t, `import io;
class Holder<T string|int> {
    T value;
    func Holder(T value) { this.value = value; }
}
let h = Holder(7);
io.println(h.value);
let s = Holder<string>("ok");
io.println(s.value);
try {
    let bad = Holder(true);
    io.println("constructed");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "7\nok\ncaught: type bool does not satisfy constraint string|int for type parameter T\n")
}

func TestParityConstraintIntersection(t *testing.T) {
	runParity(t, `import io;
interface Named { func name(): string; }
interface Aged { func age(): int; }
class Person implements Named, Aged {
    func Person() {}
    func name(): string { return "p"; }
    func age(): int { return 1; }
}
class Tag implements Named {
    func Tag() {}
    func name(): string { return "t"; }
}
func both<T implements Named & Aged>(T v): string { return v.name(); }
io.println(both(Person()));
try {
    io.println(both(Tag()));
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "p\ncaught: type Tag does not satisfy constraint Named&Aged for type parameter T\n")
}
