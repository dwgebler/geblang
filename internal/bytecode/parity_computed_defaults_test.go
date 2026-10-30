package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestParityComputedParameterDefaults(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
int base = 10;
int b = 99;
int calls = 0;
func tick(): int { calls = calls + 1; return calls; }
func mk(): list<int> { return [7]; }
func g(): any { return "x"; }
func earlier(int a, int b = a + base): int { return b; }
func later(int a = b, int b = 1): int { return a; }
func mixed(list<int> xs = [1, 2], int n = 2 * 3, list<int> m = mk()): string { return "${xs} ${n} ${m}"; }
func perCall(int t = tick()): int { return t; }
func typed(int a = g()): any { return a; }
func generic<T>(T a = g()): any { return a; }
func frozen(const list<int> xs = [1]): any { try { xs.push(2); } catch (Error e) { return "frozen"; } return xs; }
func closure(): callable { int local = 5; return func(int x = local * 2): int { return x; }; }
func nested(): int { int local = 6; func inner(int x = local + 1): int { return x; } return inner(); }
async func asyncDefault(int a = base + 1): int { return a; }
func gen(int start = base * 2): any { yield start; yield start + 1; }
func twice(callable fn): callable { return func(any ...args): any { return fn(...args) * 2; }; }
@twice
func decorated(int a = base + 3): int { return a; }
func over(int a, int b = a + base): string { return "int ${b}"; }
func over(string s, string t = s + "!"): string { return "str ${t}"; }
func variadic(int a = base + 4, int ...rest): string { return "${a} ${rest}"; }
class K {
    int z = 4;
    func K(int w = this.z * 3) { this.z = w; }
    func m(int q = this.z + 1): int { return q; }
    static func s(string v = "s" + "t"): string { return v; }
}
io.println(earlier(1)); io.println(earlier(1, 2)); io.println(later()); io.println(mixed());
io.println(perCall()); io.println(perCall()); io.println(perCall(99));
try { typed(); } catch (TypeError e) { io.println(e.message); }
io.println(generic()); io.println(frozen());
io.println(closure()()); io.println(nested());
io.println(await asyncDefault());
for (v in gen()) { io.println(v); }
io.println(decorated()); io.println(over(1)); io.println(over("x"));
io.println(variadic()); io.println(variadic(1, 2, 3));
let k = K(); io.println(k.z); io.println(k.m()); io.println(K.s()); io.println(K(2).m(1));
io.println(mixed(n: 1));
let ref = earlier; io.println(ref(3));
let args = [4]; io.println(earlier(...args));
io.println([1, 2].map(func(int v, int w = v * 10): int { return w; }));
`)
	want := "11\n2\n99\n[1, 2] 6 [7]\n1\n2\n99\ntyped expects int for parameter 'a', got string\nx\nfrozen\n10\n7\n11\n20\n21\n26\nint 11\nstr x!\n14 []\n1 [2, 3]\n12\n13\nst\n1\n[1, 2] 1 [7]\n13\n14\n[10, 20]\n"
	if ev != vm || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestParityComputedFieldAndStaticDefaults(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
int seed = 5;
int calls = 0;
func tick(): int { calls = calls + 1; return calls; }
func mk(): list<int> { return [seed]; }
func g(): any { return "x"; }
class F {
    int n = seed * 3;
    list<int> called = mk();
    list<int> lit = [1, 2];
    dict<string, list<int>> d = {"k": [1]};
    float f = 1.5f + 1.0f;
    int t = tick();
    string s = "a" + "b";
}
class G extends F { int extra = seed + 1; }
class Bad { int n = g(); }
class Thrower { int n = boom(); }
func boom(): int { throw ValueError("boom"); }
class S {
    static let computed = seed * 2;
    static int typed = seed + 1;
    static const LIST = mk();
    static func bump(): int { S.computed = S.computed + 1; return S.computed; }
}
class Sub extends S { static let own = S.computed + 100; }
let first = G(); let second = G();
first.lit.push(9); first.called.push(9); first.d["k"].push(9);
io.println("${second.n} ${second.called} ${second.lit} ${second.d} ${second.f} ${second.s} ${second.extra}");
io.println(first.t < second.t);
try { Bad(); } catch (TypeError e) { io.println(e.message); }
try { Thrower(); } catch (ValueError e) { io.println("caught ${e.message}"); }
io.println("${S.computed} ${S.typed} ${S.LIST} ${S.bump()} ${Sub.own}");
`)
	want := "15 [5] [1, 2] {\"k\": [1]} 2.5 ab 6\ntrue\ncannot assign string to int field Bad.n\ncaught boom\n10 6 [5] 11 110\n"
	if ev != vm || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestParityComputedDefaultsAcrossModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "computed.gb"), []byte(`module computed;
int secret = 40;
func make(): list<int> { return [secret]; }
func boom(): int { throw ValueError("module boom"); }
export class Item { int v = secret + 2; }
export class Base {
    int computed = secret + 1;
    list<int> called = make();
    Item obj = Item();
    func scaled(int factor = this.computed * 2): int { return factor; }
}
export class Mid extends Base { string tag = "m" + "id"; }
export class Exploding { int bad = boom(); }
export class Config { static let limit = secret * 10; }
export func helper(int a, int b = a + secret, list<int> c = make()): string { return "${b} ${c}"; }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	runParityModulesDir(t, dir, `import computed;
import io;
class Child extends computed.Mid { int own = 5 * 5; }
class Grand extends Child { list<string> names = ["g" + "1"]; }
class LocalExploding extends computed.Exploding {}
for (c in [computed.Base(), computed.Mid(), Child(), Grand()]) {
    io.println("${c.computed} ${c.called} ${c.obj.v} ${c.scaled()} ${c.scaled(1)}");
}
let a = Grand(); let b = Grand();
a.called.push(9); a.obj.v = 0; a.names.push("x");
io.println("${b.called} ${b.obj.v} ${b.names} ${b.tag} ${b.own}");
io.println(computed.helper(2)); io.println(computed.helper(2, 3));
let h = computed.helper; io.println(h(1));
io.println(computed.Config.limit);
try { LocalExploding(); } catch (ValueError e) { io.println("caught ${e.message}"); }
`, strings.Repeat("41 [40] 42 82 1\n", 4)+"[40] 42 [\"g1\"] mid 25\n42 [40]\n3 [40]\n41 [40]\n400\ncaught module boom\n")
}

func TestUncaughtComputedDefaultTraces(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"ParameterDefaultThrows", `func boom(): int {
    throw ValueError("boom");
}
func f(int a = boom()): int { return a; }


f();
`, "uncaught ValueError: boom\n  at boom (line 2)\n  at f (line 4)\n  at <top level> (line 7)"},
		{"ParameterDefaultTypeMismatch", `func g(): any {
    return "x";
}
func f(int a = g()): int { return a; }


f();
`, "uncaught TypeError: f expects int for parameter 'a', got string\n  at f (line 4)\n  at <top level> (line 7)"},
		{"FieldDefaultThrows", `func boom(): int {
    throw ValueError("boom");
}
class C { int a = boom(); }


C();
`, "uncaught ValueError: boom\n  at boom (line 2)\n  at C (line 4)\n  at <top level> (line 7)"},
		{"FieldDefaultTypeMismatch", `func mk(): list<int> {
    return [7];
}
func g(): any { return "x"; }
class Bad {
    list<int> c = mk();
    int n = g();
}
func build(): any { return Bad(); }
build();
`, "uncaught TypeError: cannot assign string to int field Bad.n\n  at Bad (line 7)\n  at build (line 9)\n  at <top level> (line 10)"},
		{"StaticInitializerTypeMismatch", `func mk(): list<int> {
    return [7];
}
class S {
    static const LIST = mk();
    static float f = 1 + 1;
}
`, "uncaught TypeError: cannot assign int to float field S.f\n  at <top level> (line 6)"},
		{"CallerLineAfterReturn", `func g(): any {
    return "x";
}
class C { int n; func C() { this.n = g(); } }
C();
`, "uncaught TypeError: cannot assign string to int field C.n\n  at C.C (line 4)\n  at <top level> (line 5)"},
		{"GlobalReadBeforeDeclaration", `func f(): any { return later; }

f();
int later = 3;
`, "uncaught RuntimeError: \"later\" is not declared\n  at f (line 1)\n  at <top level> (line 3)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, vm := uncaughtOnBothBackends(t, tc.source)
			if ev != vm || vm != tc.want {
				t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, tc.want)
			}
		})
	}
}

func TestUncaughtCrossModuleFieldDefaultTrace(t *testing.T) {
	ev, vm := uncaughtOnBothBackendsModulesDir(t, map[string]string{"exploding": `module exploding;
func boom(): int {
    throw ValueError("module boom");
}
export class Exploding { int bad = boom(); }
`}, `import exploding;
class Local extends exploding.Exploding {}

Local();
`)
	want := "uncaught ValueError: module boom\n  at boom (line 3)\n  at Exploding (line 5)\n  at <top level> (line 4)"
	if ev != vm || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestComputedDefaultsSurviveChunkRoundTrip(t *testing.T) {
	source := `import io;
int seed = 3;
func mk(): list<int> { return [seed]; }
func f(int a, int b = a + seed): int { return b; }
class C { list<int> xs = mk(); static let s = seed * 2; }
io.println(f(1)); io.println(C().xs); io.println(C.s);
try { io.println(later); } catch (Error e) { io.println(e.message); }
int later = 1;
`
	program := parser.New(lexer.New(source)).ParseProgram()
	chunk, err := bytecode.Compile(program, []byte(source), "roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatal(err)
	}
	if want := "4\n[3]\n6\n\"later\" is not declared\n"; out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}
