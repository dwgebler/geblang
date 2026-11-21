package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityStringCaseHelpers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
io.println("parseHTTPResponse2xx version2Go caf\u{e9}Bar  --x".words());
io.println("".words());
io.println("parseHTTPResponse".snakeCase());
io.println("Hello world-foo_bar".camelCase());
io.println("HTTPServer".kebabCase());
io.println("Caf\u{e9} & Cr\u{e8}me Br\u{fb}l\u{e9}e! \u{65e5}\u{672c}".slugify());
io.println("--Hello, World--".slugify());
io.println("[" + "ab".center(5, "*") + "][" + "abc".center(2) + "][" + "x".center(4, "\u{e9}-") + "]");
io.println("the quick brown fox jumps over\n\nhttps://a-very-long-url.example".wrap(10));
io.println("".wrap(5));
try { "x".wrap(0); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { "x".center(3, ""); } catch (Error e) { io.println(e.message); }
`)
	want := `["parse", "HTTP", "Response2xx", "version2", "Go", "café", "Bar", "x"]
[]
parse_http_response
helloWorldFooBar
http-server
cafe-creme-brulee
hello-world
[*ab**][abc][éxé-]
["the quick", "brown fox", "jumps over", "", "https://a-very-long-url.example"]
[]
ValueError: string.wrap: width must be at least 1
string.center: pad must be a non-empty string
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityNumericHelpers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import math;
io.println("${5.isBetween(1, 10)} ${5.isBetween(5.0, 5)} ${11.isBetween(1, 10)} ${(2.5f).isBetween(1, 3)} ${(2.5).isBetween(3, 4)} ${math.nan().isBetween(0, 1)}");
io.println("${12.gcd(-18)} ${0.gcd(0)} ${4.lcm(6)} ${0.lcm(5)} ${(-4).lcm(6)}");
io.println("${(-120).digits()} ${0.digits()} ${64.isPowerOfTwo()} ${0.isPowerOfTwo()} ${(-8).isPowerOfTwo()} ${(-5).bitCount()} ${255.bitCount()}");
let big = 2 ** 100;
io.println("${big.isPowerOfTwo()} ${big.bitCount()} ${big.gcd(2 ** 60)} ${(big + 1).digits().length()}");
io.println("${(0.1f + 0.2f).isCloseTo(0.3f)} ${(1.0f).isCloseTo(1.1f)} ${(1.0f).isCloseTo(1.1f, 0.2)} ${(0.0f).isCloseTo(1e-12f)} ${(0.0f).isCloseTo(1e-12f, 0, 1e-9)} ${math.inf().isCloseTo(math.inf())}");
try { 5.isBetween(10, 1); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { (1.0f).isBetween(0, math.nan()); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { (1.0f).isCloseTo(1.0f, -1); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
`)
	want := `true true false true false false
6 0 12 0 12
[1, 2, 0] [0] true false false 2 8
true 1 1152921504606846976 31
true false true false true true
ValueError: int.isBetween expects lo <= hi
ValueError: float.isBetween bounds must not be NaN
ValueError: float.isCloseTo tolerances must not be negative
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityBytesSearchHelpers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import bytes;
let b = bytes.fromHex("89504e47");
io.println("${b.startsWith(bytes.fromHex("8950"))} ${b.startsWith(0x89)} ${b.startsWith(0x50)} ${b.endsWith(0x47)} ${b.endsWith(bytes.fromHex(""))}");
io.println("${b.indexOf(0x4e)} ${b.indexOf(bytes.fromHex("4e47"))} ${b.indexOf(bytes.fromHex("00"))} ${b.indexOf(999)} ${b.startsWith(-1)}");
try { b.indexOf("x"); } catch (Error e) { io.println(e.message); }
`)
	want := `true true false true true
2 2 -1 -1 false
bytes.indexOf expects bytes or int byte
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParitySetHelpers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
let s = {3, 1, 2};
set<int> none = {};
io.println("${{1, 2}.isSubsetOf(s)} ${s.isSubsetOf({1})} ${s.isSupersetOf({4})} ${s.isSupersetOf(none)} ${s.isDisjointFrom({4, 5})} ${s.isDisjointFrom({2})}");
io.println(s.symmetricDifference({3, 4}));
io.println(s.filter((int x) => x > 1));
io.println(s.map((int x) => x % 2));
let seen = [];
io.println(s.any((int x) => { seen.push(x); return x == 2; }));
io.println(seen);
io.println("${s.all((int x) => x > 0)} ${none.any((int x) => true)} ${none.all((int x) => false)}");
try { s.filter((int x) => x); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { s.isSubsetOf([1]); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
`)
	want := `true false false true true false
set{1, 2, 4}
set{2, 3}
set{0, 1}
true
[1, 2]
true false true
TypeError: set.filter predicate must return bool, got int
TypeError: set.isSubsetOf expects set, got list
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityDictHelpers(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
let d = {"a": 1, "b": 2, "c": 3};
io.println(d.filter((string k, int v) => v > 1 && k != "c"));
io.println(d.mapValues((int v) => v * 10));
io.println("${d.any((string k, int v) => k == "c")} ${d.all((string k, int v) => v > 1)} ${{}.any((string k, int v) => true)} ${{}.all((string k, int v) => false)}");
io.println("${d.pick(["c", "a", "z"])} ${d.omit(["a", "z"])} ${d.pick([])}");
io.println({1: "x", "k": [1, 2]}.invert());
io.println(d);
try { {"a": 1, "b": 1}.invert(); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { d.any((string k, int v) => v); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
try { d.pick("a"); } catch (Error e) { io.println("${e.class}: ${e.message}"); }
`)
	want := `{"b": 2}
{"a": 10, "b": 20, "c": 30}
true false false true
{"a": 1, "c": 3} {"b": 2, "c": 3} {}
{"x": 1, [1, 2]: "k"}
{"a": 1, "b": 2, "c": 3}
ValueError: dict.invert: duplicate value 1
TypeError: dict.any predicate must return bool, got int
TypeError: dict.pick expects a list of keys, got string
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityPrimitiveHelpersDispatchContexts(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Tags {
    set<string> tags;
    func Tags(set<string> tags) { this.tags = tags; }
    func covers(set<string> want): bool { return want.isSubsetOf(this.tags); }
    func slug(string title): string { return title.kebabCase(); }
}
class AdminTags extends Tags {
    func AdminTags() { parent({"admin", "staff"}); }
}
func bounded(int n): bool { return n.isBetween(1, 9); }
func failing(string k, int v): bool { throw ValueError("boom ${k}"); }
any dynS = "fooBar";
any dynD = {"a": 1};
any dynSet = {1, 2};
any dynB = 255;
io.println("${bounded(5)} ${bounded(0)} ${dynS.snakeCase()} ${dynD.pick(["a"])} ${dynSet.isSubsetOf({1, 2, 3})} ${dynB.bitCount()}");
io.println("${AdminTags().covers({"admin"})} ${AdminTags().slug("myTitle")} ${Tags({"a"}).covers({"b"})}");
io.println([{1}, {1, 2}].filter((set<int> x) => x.isSupersetOf({2})));
let counts = {"a": 2};
counts = counts.mapValues((int v) => v + 1);
io.println(counts);
try {
    {"x": 1}.filter(failing);
} catch (ValueError e) {
    io.println(e.message);
}
`)
	want := `true false foo_bar {"a": 1} true 8
true my-title false
[set{1, 2}]
{"a": 3}
boom x
`
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityPrimitiveHelpersCrossModule(t *testing.T) {
	dir := t.TempDir()
	donor := `module helpers;

export func overlap(set<int> a, set<int> b): bool { return !a.isDisjointFrom(b); }

export class Config {
    dict<string, int> values;
    func Config(dict<string, int> values) { this.values = values; }
    func positive(): dict<string, int> { return this.values.filter((string k, int v) => v > 0); }
}
`
	if err := os.WriteFile(filepath.Join(dir, "helpers.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import helpers;
class Strict extends helpers.Config {
    func Strict() { parent({"a": 1, "b": -1}); }
}
io.println("${helpers.overlap({1, 2}, {2, 3})} ${helpers.overlap({1}, {3})}");
io.println(Strict().positive());
`)
	want := "true false\n{\"a\": 1}\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
