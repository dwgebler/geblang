package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const overloadSpreadDonor = `module osdonor;
export func over(int a): string { return "o1 ${a}"; }
export func over(int a, int b): string { return "o2 ${a} ${b}"; }
export class Cls {
    string tag = "";
    func Cls(int a) { this.tag = "c1 ${a}"; }
    func Cls(int a, int b) { this.tag = "c2 ${a} ${b}"; }
    static func st(int a): string { return "st1 ${a}"; }
    static func st(int a, int b): string { return "st2 ${a} ${b}"; }
}
`

func overloadSpreadDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "osdonor.gb"), []byte(overloadSpreadDonor), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Spreads into overloads select at call time; a dict spread prefers the overload dropping the fewest keys, and a tie is ambiguous.
func TestParityOverloadSpread(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, overloadSpreadDir(t), `import io;
import osdonor;
func pick(int a): string { return "one ${a}"; }
func pick(int a, int b): string { return "two ${a} ${b}"; }
func opt(int a, int b = 0): string { return "opt ${a} ${b}"; }
func opt(string s): string { return "str ${s}"; }
func tie(int a, int x = 0): string { return "tie1"; }
func tie(int b, string y = ""): string { return "tie2"; }
async func apick(int a): string { return "a1 ${a}"; }
async func apick(int a, int b): string { return "a2 ${a} ${b}"; }
class P {
    string tag = "";
    func P(int a) { this.tag = "P1 ${a}"; }
    func P(int a, int b) { this.tag = "P2 ${a} ${b}"; }
    static func s(int a): string { return "s1 ${a}"; }
    static func s(int a, int b): string { return "s2 ${a} ${b}"; }
}
class Q extends P { func Q() { parent(0); } }
func probe(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (Error e) { io.println("${label}: ${typeof(e)}: ${e.message}"); }
}
list<int> xs = [1, 2];
probe("fn list", func(): string { return pick(...xs); });
probe("fn dict", func(): string { return pick(...{"a": 3, "b": 4}); });
probe("fn dict drops keys", func(): string { return opt(...{"a": 5, "zz": 1}); });
probe("fn dict by name", func(): string { return opt(...{"s": "q"}); });
probe("fn mixed", func(): string { return pick(7, ...[8]); });
probe("fn no match", func(): string { return pick(...[1, 2, 3]); });
probe("tie", func(): string { return tie(...{"a": 1, "b": 2}); });
probe("fewest dropped", func(): string { return tie(...{"a": 1, "x": 2, "zz": 0}); });
probe("ctor list", func(): string { return P(...xs).tag; });
probe("ctor dict", func(): string { return P(...{"a": 9}).tag; });
probe("static list", func(): string { return P.s(...xs); });
probe("static dict", func(): string { return P.s(...{"a": 1, "b": 2}); });
probe("inherited static", func(): string { return Q.s(...xs); });
probe("xmod fn list", func(): string { return osdonor.over(...xs); });
probe("xmod fn dict", func(): string { return osdonor.over(...{"a": 6}); });
probe("xmod ctor", func(): string { return osdonor.Cls(...xs).tag; });
probe("xmod ctor dict", func(): string { return osdonor.Cls(...{"a": 7}).tag; });
probe("xmod static", func(): string { return osdonor.Cls.st(...{"a": 1, "b": 2}); });
probe("async", func(): string { return await apick(...xs); });
probe("async dict", func(): string { return await apick(...{"a": 2}); });
let value = pick;
probe("value", func(): string { return value(...{"a": 4}); });
string typed = pick(...xs);
io.println("typed ${typed}");
`)
	want := "fn list: two 1 2\nfn dict: two 3 4\nfn dict drops keys: opt 5 0\nfn dict by name: str q\nfn mixed: two 7 8\n" +
		"fn no match: RuntimeError: no matching overload for pick\ntie: RuntimeError: ambiguous overload for tie\nfewest dropped: tie1\n" +
		"ctor list: P2 1 2\nctor dict: P1 9\nstatic list: s2 1 2\nstatic dict: s2 1 2\ninherited static: s2 1 2\n" +
		"xmod fn list: o2 1 2\nxmod fn dict: o1 6\nxmod ctor: c2 1 2\nxmod ctor dict: c1 7\nxmod static: st2 1 2\n" +
		"async: a2 1 2\nasync dict: a1 2\nvalue: one 4\ntyped two 1 2\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}

// Deferred spreads into overloads and deferred constructions select and build when the defer fires.
func TestParityDeferredOverloadsAndConstructions(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, overloadSpreadDir(t), `import io;
import osdonor;
func pick(int a): void { io.println("pick1 ${a}"); }
func pick(int a, int b): void { io.println("pick2 ${a} ${b}"); }
class K { func K(int a, int b = 9) { io.println("K ${a} ${b}"); } }
class P {
    func P(int a) { io.println("P1 ${a}"); }
    func P(int a, int b) { io.println("P2 ${a} ${b}"); }
    static func s(int a): void { io.println("s1 ${a}"); }
    static func s(int a, int b): void { io.println("s2 ${a} ${b}"); }
}
func run(): void {
    dict<string, any> d = {"a": 1, "zz": 0};
    list<int> xs = [3, 4];
    defer pick(...xs);
    defer pick(...d);
    defer P.s(...d);
    defer K(1);
    defer K(b: 2, a: 3);
    defer K(...xs);
    defer K(...d);
    defer P(...xs);
    defer P(...d);
    defer io.println(osdonor.Cls(...xs).tag);
    io.println("body");
}
run();
func bad(): void { defer P(1, 2, 3); }
try { bad(); } catch (Error e) { io.println("${typeof(e)}: ${e.message}"); }
`)
	want := "body\nc2 3 4\nP1 1\nP2 3 4\nK 1 9\nK 3 4\nK 3 2\nK 1 9\ns1 1\npick1 1\npick2 3 4\nRuntimeError: no matching overload for P\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}
