package bytecode_test

import "testing"

// The element-tag write barrier: hierarchy-aware (subclass and
// implementer writes pass), covering every mutation surface including
// index assignment, dict key/value tags, and set.add - identically on
// both backends.

func TestParityElementTagSubtypeWritesPass(t *testing.T) {
	runParity(t, `import io;
interface Scored { func score(): int; }
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Win implements Scored { func Win() {} func score(): int { return 1; } }
list<Animal> zoo = [];
zoo.push(Dog());
zoo[0] = Dog();
io.println("${zoo.length()} ${typeof(zoo[0])}");
list<Scored> scores = [];
scores.push(Win());
io.println(typeof(scores[0]));
dict<string, Animal> da = {"seed": Dog()};
da["d"] = Dog();
io.println(typeof(da["d"]));
set<Animal> sa = {Dog()};
sa.add(Dog());
io.println(sa.length());
`, "1 Dog\nWin\nDog\n2\n")
}

func TestParityElementTagBarrierAllWriteSurfaces(t *testing.T) {
	runParity(t, `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Cat extends Animal { func Cat() { parent(); } }

func probe(string label, func body): void {
    try {
        body();
        io.println(label + ": UNCHECKED");
    } catch (TypeError e) {
        io.println(label + ": " + e.message);
    }
}

list<Dog> dogs = [Dog()];
dict<string, Dog> dd = {"a": Dog()};
set<Dog> ds = {Dog()};

probe("push", func(): void { dogs.push(Cat()); });
probe("index", func(): void { dogs[0] = Cat(); });
probe("listset", func(): void { dogs.set(0, Cat()); });
probe("insert", func(): void { dogs.insert(0, Cat()); });
probe("dictindex", func(): void { dd["c"] = Cat(); });
probe("dictset", func(): void { dd.set("c", Cat()); });
probe("dictkey", func(): void {
    any anyd = dd;
    anyd[42] = Dog();
});
probe("setadd", func(): void { ds.add(Cat()); });
`, "push: cannot push Cat to list<Dog>\nindex: cannot assign Cat to list<Dog>\nlistset: cannot assign Cat to list<Dog>\ninsert: cannot insert Cat to list<Dog>\ndictindex: cannot assign Cat to dict<string, Dog>\ndictset: cannot assign Cat to dict<string, Dog>\ndictkey: cannot use int key in dict<string, Dog>\nsetadd: cannot add Cat to set<Dog>\n")
}

func TestParityElementTagCovariantViewStillBarriered(t *testing.T) {
	// The variance hole: a list<Dog> passed covariantly as list<Animal>
	// still carries its Dog tag, so sibling writes through the wider
	// view throw.
	runParity(t, `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Cat extends Animal { func Cat() { parent(); } }
func sneakPush(list<Animal> xs): string {
    try { xs.push(Cat()); return "pushed"; } catch (TypeError e) { return e.message; }
}
func sneakIndex(list<Animal> xs): string {
    try { xs[0] = Cat(); return "assigned"; } catch (TypeError e) { return e.message; }
}
list<Dog> dogs = [Dog()];
io.println(sneakPush(dogs));
io.println(sneakIndex(dogs));
io.println(typeof(dogs[0]));
`, "cannot push Cat to list<Dog>\ncannot assign Cat to list<Dog>\nDog\n")
}

func TestParityNullableElementTagAcceptsNull(t *testing.T) {
	// A ?T element tag accepts null on every write surface, while a
	// non-nullable tag still rejects it; instanceof and reflect read
	// the base type unchanged.
	runParity(t, `import io;
import reflect;
list<?int> xs = [1, null];
xs.push(5);
xs.push(null);
xs[0] = null;
io.println(xs.length());
dict<string, ?int> d = {"a": 1};
d["b"] = null;
io.println(d.length());
set<?int> s = {1};
s.add(null);
io.println(s.length());
io.println("${xs instanceof list<int>}");
io.println("${reflect.typeBindings(xs)}");
any n = null;
list<int> strict = [1];
try {
    strict.push(n);
    io.println("accepted");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "4\n2\n2\ntrue\n{\"T\": \"int\"}\ncaught: cannot push null to list<int>\n")
}

func TestParityNestedElementTagShallow(t *testing.T) {
	// Element tags enforce only the outer kind at write boundaries: a
	// list<list<int>> accepts an inner list of the wrong element type
	// (declaration-time checking is deeper; writes are shallow).
	runParity(t, `import io;
list<list<int>> nested = [[1, 2]];
any wrong = ["x", "y"];
nested.push(wrong);
io.println(nested.length());
`, "2\n")
}

func TestParityUnaryMinusNonNumericMessage(t *testing.T) {
	runParity(t, `import io;
any v = "x";
try {
    let y = -v;
    io.println("no throw");
} catch (TypeError e) {
    io.println(e.message);
}
`, "- expects numeric value, got string\n")
}
