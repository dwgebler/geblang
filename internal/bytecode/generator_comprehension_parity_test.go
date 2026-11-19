package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratorComprehensionParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

func src(string tag, list xs): list {
    io.println("eval " + tag);
    return xs;
}
class Bag {
    func Bag() {}
    func __iter(): any { return [7, 8]; }
}
class Scaler {
    int factor;
    func Scaler(int f) { this.factor = f; }
    func apply(list<int> xs): generator<int> { return (x * this.factor for x in xs); }
}
func evens(int limit): generator<int> {
    for (v in (n for n in 0..limit if n % 2 == 0)) {
        yield v;
    }
}

let g = (a + b for a in src("outer", [1, 2]) for b in src("inner", [10]));
io.println("created");
io.println(typeof(g));
io.println([v for v in g]);

let items = [1, 2];
let h = (x for x in items);
items = [9];
io.println([v for v in h]);

int k = 1;
let scaled = (x * k for x in [1, 2]);
k = 10;
io.println([v for v in scaled]);

let fs = (() => x for x in [1, 2, 3]);
io.println([f() for f in fs]);

io.println([v for v in ("${key}=${val}" for key, val in {"a": 1, "b": 2})]);
io.println([v for v in (n + 1 for int n in [1, 2])]);
io.println([v for v in (x for x in Bag())]);
io.println([v for v in (c for c in "ab")]);
io.println([v for v in (y * 2 for y in (x + 1 for x in [1, 2]))]);
io.println([v for v in evens(6)]);
io.println([v for v in Scaler(3).apply([1, 2])]);

let s = (n for n in [1, 2, 3]);
io.println(s.next());
for (v in s) { io.println(v); break; }
io.println(s.done());

func show(string label, func body) {
    try { body(); io.println(label + ": ok"); } catch (Error e) { io.println(label + ": [" + e.class + "] " + e.message); }
}
show("nullSource", () => { let bad = (x for x in null); });
show("intSource", () => { let bad = (x for x in 5); });
show("innerSource", () => { let lazy = (x for x in [1] for y in null); });
show("innerSourceIterated", () => { for (v in (x for x in [1] for y in null)) {} });
show("body", () => { for (v in (10 / x for x in [0])) {} });
show("binder", () => { for (v in (n for int n in ["a"])) {} });
`)
	want := "eval outer\ncreated\ngenerator\neval inner\neval inner\n[11, 12]\n" +
		"[1, 2]\n[10, 20]\n[1, 2, 3]\n[\"a=1\", \"b=2\"]\n[2, 3]\n[7, 8]\n[\"a\", \"b\"]\n[4, 6]\n[0, 2, 4, 6]\n[3, 6]\n" +
		"1\n2\ntrue\n" +
		"nullSource: [TypeError] null is not iterable\n" +
		"intSource: [TypeError] int is not iterable\n" +
		"innerSource: ok\n" +
		"innerSourceIterated: [RuntimeError] null is not iterable\n" +
		"body: [RuntimeError] decimal division by zero\n" +
		"binder: [TypeError] type error: cannot assign string to int\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestGeneratorComprehensionAcrossModulesParity(t *testing.T) {
	dir := t.TempDir()
	module := `module gencomp;

export func squares(list<int> xs): generator<int> {
    return (x * x for x in xs);
}
export class Source {
    list<int> items;
    func Source(list<int> items) { this.items = items; }
    func doubled(): generator<int> { return (x * 2 for x in this.items); }
}
`
	if err := os.WriteFile(filepath.Join(dir, "gencomp.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import gencomp;

class Local extends gencomp.Source {
    func Local(list<int> items) { parent(items); }
}
io.println([v for v in gencomp.squares([1, 2, 3])]);
io.println([v for v in Local([4]).doubled()]);
io.println([v for v in (x + 1 for x in gencomp.squares([2]))]);
`)
	want := "[1, 4, 9]\n[8]\n[5]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
