package bytecode_test

import (
	"strings"
	"testing"
)

const genericsValuesDonor = `module values_donor;

export interface Shape { func area(): int; }
interface Hidden { func x(): int; }
export interface Child extends Shape { func more(): int; }

export class Thing { func m(): int { return 1; } static func s(): int { return 2; } }

export class Box<T> {
    T v;
    func Box(T v) { this.v = v; }
    func nested(any x): list<T> { return [x]; }
    func take(list<T> xs): int { return xs.length(); }
}

export func f(int x): int { return x; }
export func ident<T>(T x): T { return x; }
export func wrap<T>(any x): list<T> { return [x]; }
export func maker(): callable { return func(int x): int { return x; }; }
export func named(): callable { func inner(): int { return 1; } return inner; }
export func own(): any { return Shape; }
export func hidden(): any { return Hidden; }
export func viaClosure(): callable { return func(): any { return Child; }; }
`

const genericsValuesMain = `import io;
import async;
import reflect;
import values_donor as donor;
from values_donor import Thing, ident;

interface Local { func area(): int; }
class Sq implements Local { func area(): int { return 4; } }

func run(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (TypeError e) { io.println("${label}: rejected: ${e.message}"); }
}

func id<T>(T x): T { return x; }
func pair<K, V>(K k, V v): dict<K, V> { return {k: v}; }
func make<T>(): ?T { return null; }
int a = id(5);
dict<string, int> d = pair("k", 1);
?int e = make();
func viaReturn(): int { return id(7); }
io.println([a, d, e, viaReturn(), ident(8)]);

any s = "x";
any bad = ["x"];
any one = 1;
func nestedExplicit<T>(any x): list<T> { return [x]; }
func arrExplicit<T>(any x): T[] { return [x]; }
func dictNested<T>(any x): dict<string, T> { return {"k": x}; }
func nestedInferred<T>(T seed, any x): list<T> { return [x]; }
func paramNested<T>(T seed, list<T> xs): int { return xs.length(); }
func unbound<T>(any x): list<T> { return [x]; }
class IntBox extends donor.Box<int> { func IntBox() { parent(1); } }
class Holder<T> { T v; any raw = "x"; func Holder(T v) { this.v = v; } func get(): T { return this.raw; } }
run("nestedExplicit", func(): any { return nestedExplicit<int>(s); });
run("arrExplicit", func(): any { return arrExplicit<int>(s); });
run("dictNested", func(): any { return dictNested<int>(s); });
run("okExplicit", func(): any { return nestedExplicit<int>(one); });
run("nestedInferred", func(): any { return nestedInferred(one, s); });
run("paramNested", func(): any { return paramNested(one, bad); });
run("paramNestedExplicit", func(): any { return paramNested<int>(1, bad); });
run("unbound", func(): any { return unbound(s); });
run("donorWrap", func(): any { return donor.wrap<int>(s); });
run("boxNested", func(): any { return donor.Box<int>(1).nested(s); });
run("boxTake", func(): any { return donor.Box<int>(1).take(bad); });
run("subNested", func(): any { return IntBox().nested(s); });
run("boundNested", func(): any { let m = donor.Box<int>(1).nested; return m(s); });
run("boundGet", func(): any { let g = Holder<int>(1).get; return g(); });
run("callback", func(): any { return [s].map(func(any v): any { return nestedExplicit<int>(v); }); });
run("task", func(): any { return async.await(async.run(func(): any { return nestedExplicit<int>(s); })); });

func top(int x): int { return x; }
io.println([top, func(int x): int { return x; }, Sq().area, Thing().m, Thing.s]);
io.println([donor.f, donor.maker(), donor.named(), donor.Thing, Thing, Sq]);
func h(int x): int { return x; }
func h(string x): string { return x; }
io.println(h);

let l = Local;
io.println([l, l == Local, l is Local, Local == donor.Shape, [Local].contains(Local)]);
io.println(reflect.interfaceMethods(Local));
io.println([donor.Shape, donor.own(), donor.own() == donor.Shape, donor.hidden(), donor.viaClosure()()]);
io.println(reflect.interfaceParents(donor.viaClosure()()));
`

func TestParityGenericsValuesAndInterfaces(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"values_donor.gb": genericsValuesDonor}, genericsValuesMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	for _, want := range []string{
		`[5, {"k": 1}, null, 7, 8]`,
		"nestedExplicit: rejected: nestedExplicit expects list<T> return, got list<string>",
		"arrExplicit: rejected: arrExplicit expects T[] return, got list<string>",
		"dictNested: rejected: dictNested expects dict<string, T> return, got dict<string,string>",
		"okExplicit: [1]",
		`nestedInferred: ["x"]`,
		"paramNested: 1",
		"paramNestedExplicit: rejected: paramNested expects list<T> for parameter 'xs', got list (element at index 0 is string)",
		`unbound: ["x"]`,
		"donorWrap: rejected: wrap expects list<T> return, got list<string>",
		"boxNested: rejected: Box.nested expects list<T> return, got list<string>",
		"boxTake: rejected: Box.take expects list<T> for parameter 'xs', got list (element at index 0 is string)",
		"subNested: rejected: Box.nested expects list<T> return, got list<string>",
		"boundNested: rejected: Box.nested expects list<T> return, got list<string>",
		"boundGet: rejected: Holder.get expects T return, got string",
		"callback: rejected: nestedExplicit expects list<T> return, got list<string>",
		"task: rejected: nestedExplicit expects list<T> return, got list<string>",
		"[<func top>, <func>, <func area>, <func m>, <func s>]",
		"[<func f>, <func>, <func inner>, <class Thing>, <class Thing>, <class Sq>]",
		"<overloaded func h>",
		"[<interface Local>, true, true, false, true]",
		`[{"doc": null, "name": "area", "parameters": [], "returnType": "int"}]`,
		"[<interface Shape>, <interface Shape>, true, <interface Hidden>, <interface Child>]",
		`["Shape"]`,
	} {
		if !strings.Contains(ev, want+"\n") {
			t.Fatalf("missing %q in output:\n%s", want, ev)
		}
	}
}
