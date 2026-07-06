package bytecode_test

import "testing"

// An overloaded function used as a value carries all overloads; calling it selects by the canonical rules on both backends.
func TestParityOverloadedFunctionAsCallback(t *testing.T) {
	runParity(t, `import io;
func pick(int x): bool { return x > 2; }
func pick(int x, int y): bool { return x > y; }
func apply(any cb, int v): bool { return cb(v); }
io.println(apply(pick, 5));
io.println(apply(pick, 1));
`, "true\nfalse\n")
}

// Same-arity overloads disambiguate by parameter type identically on both backends.
func TestParityOverloadedCallbackByType(t *testing.T) {
	runParity(t, `import io;
func f(int x): string { return "int"; }
func f(string x): string { return "str"; }
func call(any cb, any v): string { return cb(v); }
io.println(call(f, 5));
io.println(call(f, "hi"));
`, "int\nstr\n")
}

// Callback selection equals direct-call selection (the canonical reference).
func TestParityOverloadedCallbackMatchesDirect(t *testing.T) {
	runParity(t, `import io;
func f(int x): string { return "int"; }
func f(string x): string { return "str"; }
func call(any cb, any v): string { return cb(v); }
io.println(f(5) == call(f, 5));
io.println(f("hi") == call(f, "hi"));
`, "true\ntrue\n")
}

// A variadic overload is selected for the trailing-arg shape; the fixed-arity one otherwise.
func TestParityOverloadedCallbackVariadic(t *testing.T) {
	runParity(t, `import io;
func f(string s): string { return "str"; }
func f(int... xs): string { return "ints:" + (xs.length() as string); }
func one(any cb, any v): string { return cb(v); }
func three(any cb, int a, int b, int c): string { return cb(a, b, c); }
io.println(one(f, "x"));
io.println(three(f, 1, 2, 3));
`, "str\nints:3\n")
}

// No-matching-overload errors identically (caught so output is comparable).
func TestParityOverloadedCallbackNoMatch(t *testing.T) {
	runParity(t, `import io;
func f(int x): string { return "int"; }
func f(string x): string { return "str"; }
func call(any cb, any v): string {
    try { return cb(v); } catch (Error e) { return "err: " + e.message; }
}
io.println(call(f, true));
`, "err: no matching overload for f\n")
}

// An overloaded callback invoked concurrently from multiple goroutines selects the right overload on its own worker (run under -race via the engine race gate).
func TestParityOverloadedCallbackConcurrent(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async.tasks as task;
func dbl(int x): int { return x * 2; }
func dbl(int x, int y): int { return x + y; }
let xs = [1, 2, 3, 4, 5, 6, 7, 8];
let ys = task.map(xs, dbl, {"concurrency": 4});
io.println(ys);
`, "[2, 4, 6, 8, 10, 12, 14, 16]\n")
}

// Ambiguous overloads error identically (a default makes two overloads bind one arg).
func TestParityOverloadedCallbackAmbiguous(t *testing.T) {
	runParity(t, `import io;
func f(int x): string { return "one"; }
func f(int x, int y = 9): string { return "two"; }
func call(any cb): string {
    try { return cb(5); } catch (Error e) { return "err: " + e.message; }
}
io.println(call(f));
`, "err: ambiguous overload for f\n")
}

// Followup #4: an overloaded function invoked as a value matches class subtypes, interfaces, enums, and generic-collection element types identically on both backends (the VM's callback selector previously did a flat name compare and missed these).
func TestParityOverloadedCallbackAssignability(t *testing.T) {
	runParity(t, `import io;
class Animal { }
class Dog extends Animal { }
func choose(Animal a): string { return "animal"; }
func choose(string s): string { return "string"; }
let f = choose;
io.println(f(Dog()));
io.println(f("hi"));
`, "animal\nstring\n")

	runParity(t, `import io;
interface Speaker { func speak(): string; }
class Dog implements Speaker { func speak(): string { return "woof"; } }
func k(Speaker s): string { return "speaker"; }
func k(int n): string { return "int"; }
let f = k;
io.println(f(Dog()));
io.println(f(5));
`, "speaker\nint\n")

	runParity(t, `import io;
interface Named { func label(): string; }
enum Color implements Named { Red; Green; func label(): string { return "color"; } }
func k(Named n): string { return "named"; }
func k(int x): string { return "int"; }
let f = k;
io.println(f(Color.Red));
io.println(f(3));
`, "named\nint\n")

	runParity(t, `import io;
func g(list<int> xs): string { return "ints"; }
func g(list<string> xs): string { return "strs"; }
let f = g;
io.println(f([1, 2, 3]));
io.println(f(["a", "b"]));
`, "ints\nstrs\n")

	runParity(t, `import io;
func d(dict<string, int> m): string { return "si"; }
func d(dict<string, string> m): string { return "ss"; }
let f = d;
io.println(f({"a": 1}));
io.println(f({"a": "x"}));
`, "si\nss\n")
}

// Followup adversarial: an overloaded callback selects the right USER-generic overload (Box<Dog> vs Box<Animal>) on both backends - the VM previously matched user generics by base name only and reported "ambiguous overload".
func TestParityUserGenericOverloadSelection(t *testing.T) {
	runParity(t, `import io;
class Animal {}
class Dog extends Animal {}
class Box<T> { T item; func Box(T item) { this.item = item; } }
func receive(Box<Dog> b): string { return "boxDog"; }
func receive(Box<Animal> b): string { return "boxAnimal"; }
let f = receive;
io.println(f(Box<Dog>(Dog())));
io.println(f(Box<Animal>(Animal())));
`, "boxDog\nboxAnimal\n")
}

// An overloaded ASYNC function used as a value yields a Task per selected overload (like a single async value and a direct overloaded async call), not the eagerly-run result.
func TestParityOverloadedAsyncValue(t *testing.T) {
	runParity(t, `import io;
import async;
async func af(int v): string { return "int:${v}"; }
async func af(string s): string { return "str:${s}"; }
let f = af;
let a = f(5);
let b = f("hi");
io.println("${typeof(a)} ${typeof(b)}");
io.println("${async.await(a)} ${async.await(b)}");
io.println("${typeof(af(9))} ${async.await(af(9))}");
`, "Task Task\nint:5 str:hi\nTask int:9\n")
}

// A mixed overload set wraps only the selected overload: an async winner yields a Task, a sync winner yields its plain result, matching the evaluator's post-selection async check.
func TestParityOverloadedMixedAsyncValue(t *testing.T) {
	runParity(t, `import io;
import async;
async func mf(int v): string { return "async:${v}"; }
func mf(string s): string { return "sync:${s}"; }
let f = mf;
let a = f(5);
let b = f("hi");
io.println("${typeof(a)} ${typeof(b)}");
io.println("${async.await(a)} ${b}");
`, "Task string\nasync:5 sync:hi\n")
}

// An overloaded async value passed to a HOF callback yields a Task from the invocation, matching the evaluator.
func TestParityOverloadedAsyncCallback(t *testing.T) {
	runParity(t, `import io;
import async;
async func af(int v): string { return "int:${v}"; }
async func af(string s): string { return "str:${s}"; }
func apply(any cb, int x): any { return cb(x); }
let f = af;
let r = apply(f, 7);
io.println("${typeof(r)} ${async.await(r)}");
`, "Task int:7\n")
}
