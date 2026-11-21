package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityNamedArgsOnPrimitiveMethods(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
string s = "x";
list<int> xs = [3, 1, 2];
dict<string, int> d = {"a": 1, "b": 2};
io.println(s.padStart(pad: "ab", width: 4));
io.println(s.padEnd(width: 3, pad: "-"));
io.println("a-b-c".replace("-", "+", count: 1));
io.println("ab".repeat(count: 2));
io.println("Hello".containsIgnoreCase(needle: "ELL"));
io.println(xs.reduce(initial: 10, combine: (int a, int b) => a + b));
io.println(xs.map(transform: (int x) => x * 2));
io.println(xs.sortBy(key: (int x) => x, descending: true));
io.println([[1, "b"], [2, "a"]].binarySearchBy(key: (list<any> p) => p[0], target: 2));
io.println(d.mapValues(transform: (int v) => v + 1));
io.println({1, 2}.map(transform: (int x) => x % 2));
io.println(255.toString(base: 16));
io.println((2.567).round(places: 2));
io.println([1, 2, 3, 4].windowed(size: 2, step: 2));
io.println("${(1.0f).isCloseTo(1.1f, absTol: 0.2)} ${(1.0f).isCloseTo(1.1f, relTol: 0.2)} ${(1.0f).isCloseTo(other: 1.0f)}");
io.println(5.isBetween(hi: 9, lo: 1));
io.println("abc".slice(1, end: 2));
`)
	want := `abax
x--
a+b-c
abab
true
16
[6, 2, 4]
[3, 2, 1]
1
{"a": 2, "b": 3}
set{0, 1}
ff
2.57
[[1, 2], [3, 4]]
true true true
true
b
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedArgsOnPrimitiveMethodsDynamicReceivers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
any s = "x";
any xs = [3, 1, 2];
any f = 1.0f;
any n = 255;
io.println(s.padStart(pad: "ab", width: 4));
io.println(xs.reduce(initial: 10, combine: (int a, int b) => a + b));
io.println(f.isCloseTo(1.1f, absTol: 0.2));
io.println(n.toString(base: 2));
let items = ["a", 7];
for (item in items) {
    io.println(item.toString());
}
try { s.padStart(wdth: 3); } catch (Error e) { io.println(e.message); }
try { s.padStart(pad: "-"); } catch (Error e) { io.println(e.message); }
try { s.padStart(3, width: 4); } catch (Error e) { io.println(e.message); }
try { "abc".slice(end: 2); } catch (Error e) { io.println(e.message); }
try { n.toString(base: 2, base: 3); } catch (Error e) { io.println(e.message); }
`)
	want := `abax
16
true
11111111
a
7
string.padStart has no parameter wdth
string.padStart missing argument width
string.padStart parameter width passed more than once
string.slice missing argument start
int.toString parameter base passed more than once
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedPrimitiveArgsKeepSourceOrder(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
let trace = [];
func note(string tag, any value): any { trace.push(tag); return value; }
string s = "x";
io.println(s.padStart(pad: note("pad", "*"), width: note("width", 3)));
io.println([1, 2].reduce(initial: note("initial", 0), combine: note("combine", (int a, int b) => a + b)));
io.println((1.0f).isCloseTo(absTol: note("abs", 0.2), other: note("other", 1.1f)));
io.println(trace);
`)
	want := "**x\n3\ntrue\n[\"pad\", \"width\", \"initial\", \"combine\", \"abs\", \"other\"]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedPrimitiveArgsDispatchContexts(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Label {
    string text;
    func Label(string text) { this.text = text.padEnd(pad: ".", width: 5); }
    func wide(int width): string { return this.text.padStart(width: width, pad: "_"); }
}
class Tag extends Label {
    func Tag() { parent("t"); }
    func shout(): string { return this.text.replace(to: "!", from: ".", count: 2); }
}
func pad(string s): string { return s.padStart(pad: "0", width: 3); }
let shorten = (string s) => s.padEnd(width: 2, pad: "?");
io.println(pad("7"));
io.println(shorten("a"));
io.println(Label("ab").wide(7));
io.println(Tag().shout());
io.println(Tag().wide(width: 6));
string acc = "a";
acc += acc.padStart(width: 3, pad: "-");
io.println(acc);
?string maybe = null;
io.println(maybe?.padStart(width: 3, pad: "-"));
list<string> words = ["x", "yy"];
io.println(words.map(transform: (string w) => w.padStart(width: 3, pad: ".")));
io.println(words[1].padStart(pad: "#", width: 4));
let xs = [1, 2];
func later() { defer xs.insert(value: 9, index: 0); }
later();
io.println(xs);
`)
	want := `007
a?
__ab...
t!!..
_t....
a--a
null
["..x", ".yy"]
##yy
[9, 1, 2]
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedPrimitiveArgsCrossModule(t *testing.T) {
	dir := t.TempDir()
	donor := `module textkit;

export func fence(string s, int width = 6): string { return s.padStart(pad: "=", width: width); }

export class Box {
    string body;
    func Box(string body) { this.body = body; }
    func render(): string { return this.body.padEnd(width: 5, pad: "|"); }
}
`
	if err := os.WriteFile(filepath.Join(dir, "textkit.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import textkit;
class Banner extends textkit.Box {
    func Banner() { parent("hi"); }
    func both(): string { return this.render().padStart(width: 7, pad: "<"); }
}
io.println(textkit.fence("ab"));
io.println(textkit.fence(width: 4, s: "c"));
io.println(Banner().render());
io.println(Banner().both());
`)
	want := "====ab\n===c\nhi|||\n<<hi|||\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedArgsFillSkippedModuleDefaults(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import datetime;
io.println(datetime.make(2024, 1, 2, second: 5) == datetime.make(2024, 1, 2, 0, 0, 5));
io.println(datetime.make(2024, 1, 2, minute: 30) == datetime.make(2024, 1, 2, 0, 30));
`)
	want := "true\ntrue\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNamedArgsOnNativeClassMethods(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import db;
let c = db.connect("sqlite", ":memory:");
c.exec(sql: "create table t (n int)");
let tx = c.begin(opts: {"isolation": "default"});
tx.exec("insert into t values (1)");
tx.commit();
c.Begin(timeoutMs: 5000).rollback();
func later() { defer c.exec(sql: "insert into t values (2)"); }
later();
io.println(db.query(c.handle, "select count(*) as n from t")[0]["n"]);
try { c.begin(bogus: 1); } catch (Error e) { io.println(e.message); }
`)
	want := "2\nConnection.begin has no parameter bogus\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
