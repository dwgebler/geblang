package bytecode_test

import "testing"

func TestParityNamedLiteralArgsAnyOrder(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class M {
    float v;
    func M(float v, string tag = "t") { this.v = v; }
    func scale(float f, int k = 1): float { return this.v * f; }
    func pick(float f, string s = "a"): string { return "f"; }
    func pick(int f, int s): string { return "i"; }
}
let m = M(v: 2);
io.println("${m.scale(f: 2)} ${m.scale(k: 1, f: 3)} ${m.scale(K: 1, F: 4)} ${m.pick(s: 2, f: 1)}");
try { m.pick(f: 1); } catch (Error e) { io.println("pick"); }
`)
	want := "4 6 8 i\npick\n"
	if ev != want || vm != want {
		t.Fatalf("named literal args:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityParentCallLiteralThroughExtends(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box<T> {
    T item;
    func Box(T v) { this.item = v; }
    func put(T v): void { this.item = v; }
}
class Mid<A> extends Box<A> { func Mid(A v) { parent(v); } }
class Leaf extends Box<float> {
    func Leaf() { parent(1); }
    func again(): string { parent.put(2); return typeof(this.item) as string; }
}
class Deep extends Mid<decimal> { func Deep() { parent(3); } }
class Open<U> extends Box<U> {
    func Open(U v) { parent(v); }
    func go(): string { parent.put(4); return typeof(this.item) as string; }
}
io.println("${typeof(Leaf().item)} ${Leaf().again()} ${typeof(Deep().item)} ${Open<decimal>(1.5).go()}");
`)
	want := "float float decimal decimal\n"
	if ev != want || vm != want {
		t.Fatalf("parent literal:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityInheritedGenericBindingThroughExtends(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box<T> {
    T item;
    func put(T v): void { this.item = v; }
}
class Leaf<U> extends Box<U> { func Leaf() {} }
class Pair<A> extends Box<list<A>> {}
class Sub<V> extends Leaf<V> {}
let l = Leaf<decimal>();
l.put(2.5);
let s = Sub<string>();
s.put("x");
let p = Pair<int>();
p.put([1, 2]);
io.println("${typeof(l.item)} ${s.item} ${p.item}");
try { s.put(5); } catch (TypeError e) { io.println(e.message); }
try { p.put(["no"]); } catch (TypeError e) { io.println("pair"); }
`)
	want := "decimal x [1, 2]\nBox.put expects T for parameter 'v', got int\npair\n"
	if ev != want || vm != want {
		t.Fatalf("inherited binding:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityMethodTypeParamShadowsClass(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box<T> {
    T val;
    func Box(T v) { this.val = v; }
    func id<T>(T x): string { T y = x; return typeof(y) as string; }
    func keep(T x): string { return typeof(x) as string; }
}
let b = Box<float>(1.0f);
let n = 5;
io.println("${b.id(n)} ${b.id(5)} ${b.id<int>(5)} ${b.id<int>(n)} ${b.id("s")} ${b.keep(2)}");
try { b.keep(n); } catch (TypeError e) { io.println(e.message); }
`)
	want := "int int int int string float\nBox.keep expects T for parameter 'x', got int\n"
	if ev != want || vm != want {
		t.Fatalf("method type param shadowing:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityLocalCollectionAliasParams(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import json;
type Nums = list<float>;
class P {
    Nums xs;
    func P(Nums xs) { this.xs = xs; }
}
func takes(Nums n): string { return "${n}"; }
any bad = ["a"];
io.println(typeof(json.parseAs("{\"xs\": [1, 2]}", P).xs[0]));
try { takes(bad); } catch (TypeError e) { io.println(e.message); }
`)
	want := "float\ntakes expects list<float> for parameter 'n', got list (element at index 0 is string)\n"
	if ev != want || vm != want {
		t.Fatalf("alias params:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityCrossModuleOverrideOfNonExportedBase(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"lib.gb": `module lib;
class Base {
    func m(float x): string { return "base"; }
    func viaThis(): string { return this.m(3.0f); }
}
export class Kid extends Base {}
export func run(Kid k): string { Base b = k; float f = 2.0f; return b.m(f); }
`}, `import io;
import lib;
class X extends lib.Kid { func m(float x): string { return "x"; } }
io.println("${lib.run(lib.Kid())} ${lib.run(X())} ${X().viaThis()}");
`)
	want := "base x x\n"
	if ev != want || vm != want {
		t.Fatalf("cross-module override:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityCrossModuleInheritedGenericBinding(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"lib.gb": `module lib;
export class Box<T> {
    T item;
    func put(T v): void { this.item = v; }
    func id<T>(T x): string { return typeof(x) as string; }
}
export class Pair<A> extends Box<A> {}
`}, `import io;
import lib;
class Leaf<U> extends lib.Box<U> { func Leaf() {} }
class Sub<V> extends lib.Pair<V> {}
let l = Leaf<decimal>();
l.put(2.5);
let s = Sub<string>();
s.put("x");
io.println("${typeof(l.item)} ${s.item} ${l.id(5)}");
try { s.put(5); } catch (TypeError e) { io.println("rejected"); }
`)
	want := "decimal x int\nrejected\n"
	if ev != want || vm != want {
		t.Fatalf("cross-module inherited binding:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}
