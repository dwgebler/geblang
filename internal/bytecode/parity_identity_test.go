package bytecode_test

import "testing"

func TestParityIdentityOperator(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import math;
enum Shape { Circle(float), Dot }
class P {
    func m(): int { return 1; }
    static func s(): int { return 2; }
}
class Q extends P {}
func top(): int { return 3; }
func maker(): callable { return func(): int { return 4; }; }
func capturing(int n): callable { return func(): int { return n; }; }
let p = P();
let sub = Q();
P asBase = sub;
let m1 = p.m;
let m2 = m1;
let c1 = maker();
let c2 = c1;
let k = capturing(1);
let fs = [];
for (i in [1, 2]) { fs.push(func(): int { return 5; }); }
let l = [1];
let l2 = l;
let d = {"k": 1};
let d2 = d;
let s = {1};
let s2 = s;
io.println("${p is p} ${p is P()} ${sub is asBase} ${P is P} ${P is Q}");
io.println("${m1 is m2} ${p.m is p.m} ${top is top} ${P.s is P.s} ${math.sqrt is math.sqrt} ${math.sqrt is math.abs}");
io.println("${c1 is c2} ${maker() is maker()} ${k is k} ${capturing(1) is capturing(1)} ${fs[0] is fs[1]}");
io.println("${Shape.Dot is Shape.Dot} ${Shape.Circle(1.5f) is Shape.Circle(1.5f)} ${Shape.Circle(1.5f) is Shape.Circle(2.5f)}");
io.println("${1 is 1} ${1 is 1.0f} ${"a" is "a"} ${null is null} ${1 is not 2}");
l2.push(2);
d2["z"] = 2;
io.println("${l is l2} ${l is [1, 2]} ${d is d2} ${d is {"k": 1, "z": 2}} ${s is s2} ${s is {1}} ${{} is {}}");
`)
	want := "true false true true false\n" +
		"true false true true true false\n" +
		"true false true false false\n" +
		"true true false\n" +
		"true false true true true\n" +
		"true false true false true false false\n"
	if ev != want || vm != want {
		t.Fatalf("identity:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityIdentityAcrossModules(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"lib.gb": `module lib;
export func helper(): int { return 1; }
export func over(int x): int { return x; }
export func over(string x): string { return x; }
export class Box {
    func get(): int { return 2; }
    static func make(): Box { return Box(); }
}
export let shared = [1, 2];
export func sharedRef(): list<int> { return shared; }
export func same(any a, any b): bool { return a is b; }
`}, `import io;
import lib;
import reflect;
func deco(callable f): callable { return f; }
@deco
func wrapped(): int { return 3; }
class D {
    @deco
    static func st(): int { return 4; }
    func m(): int { return 5; }
}
let b = lib.Box();
let bm = b.get;
let items = [1];
let rm = reflect.method(D(), "m");
let dd = D();
io.println("${lib.helper is lib.helper} ${lib.over is lib.over} ${lib.Box.make is lib.Box.make} ${bm is bm} ${b.get is b.get}");
io.println("${lib.shared is lib.sharedRef()} ${b is lib.Box()} ${lib.same(items, items)} ${lib.same(items, [1])}");
io.println("${wrapped is wrapped} ${D.st is D.st} ${rm is rm} ${reflect.method(dd, "m") is reflect.method(dd, "m")}");
`)
	want := "true true true true false\ntrue false true false\ntrue true true false\n"
	if ev != want || vm != want {
		t.Fatalf("cross-module identity:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}
