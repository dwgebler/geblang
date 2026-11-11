package bytecode_test

import (
	"testing"
)

const genericDeclDonor = `module generic_decl_donor;

func raw(any v): any { return v; }

export func wrap<T>(T x): list<T> { list<T> y = [x]; return y; }
export func count<T>(T x): int { T[] y = [x, x]; return y.length(); }
export class Bag<T> {
    T v;
    func Bag(T v) { this.v = v; }
    func items(): list<T> { list<T> r = [this.v]; return r; }
    func size(): int { dict<string, T> d = {"k": this.v}; return d.length(); }
    func viaClosure(): list<T> { let f = func(): list<T> { list<T> r = [this.v]; return r; }; return f(); }
    func closureRet(any x): any { let f = func(): list<T> { return raw(x); }; return f(); }
}
`

const genericDeclMain = `import io;
import reflect;
import generic_decl_donor as donor;

func raw(any v): any { return v; }
func run(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (TypeError e) { io.println("${label}: rejected: ${e.message}"); }
}

func wrap<T>(T x): list<T> { list<T> y = [x]; return y; }
func tags<T>(T x): dict { list<T> y = [x]; return reflect.typeBindings(y); }
func nestedStmt<T>(T x): int {
    func inner(any v): list<T> { list<T> r = raw(v); return r; }
    return inner([x]).length();
}
func closureDecl<T>(any x): int { let f = func(): int { list<T> r = raw(x); return r.length(); }; return f(); }
func closureRet<T>(any x): any { let f = func(): list<T> { return raw(x); }; return f(); }
class Box<T> {
    T v;
    func Box(T v) { this.v = v; }
    func all(): list<T> { list<T> r = [this.v]; return r; }
    func pair(): int { set<T> s = {this.v}; return s.length(); }
}
class IntBag extends donor.Bag<int> { func IntBag(int v) { parent(v); } }
class StrBox extends Box<string> { func StrBox() { parent("s"); } }

run("wrapExplicit", func(): any { return wrap<int>(5); });
run("wrapInferred", func(): any { return wrap("a"); });
run("tagsUnbound", func(): any { return tags<int>(1); });
run("nestedStmt", func(): any { return nestedStmt(1); });
run("closureDecl", func(): any { return closureDecl([1, 2]); });
run("closureRetUnbound", func(): any { return closureRet([1]); });
run("closureRetBound", func(): any { return closureRet<string>([1]); });
run("boxExplicit", func(): any { return Box<int>(3).all(); });
run("boxInferred", func(): any { return Box(3).all(); });
run("boxSet", func(): any { return Box<int>(3).pair(); });
run("subclassLocal", func(): any { return StrBox().all(); });
run("donorWrap", func(): any { return donor.wrap<int>(1); });
run("donorCount", func(): any { return donor.count("x"); });
run("donorBag", func(): any { return donor.Bag<string>("s").items(); });
run("donorBagDict", func(): any { return donor.Bag<string>("s").size(); });
run("donorClosure", func(): any { return donor.Bag<int>(2).viaClosure(); });
run("donorClosureRetInferred", func(): any { return donor.Bag(2).closureRet(["s"]); });
run("donorClosureRetBound", func(): any { return donor.Bag<int>(2).closureRet(["s"]); });
run("subclassInherited", func(): any { return IntBag(7).items(); });
run("concreteMismatch", func(): any { int[] w = raw(["a"]); return w; });
run("concreteGenericMismatch", func(): any { list<int> w = raw(["a"]); return w; });
run("concreteTag", func(): any { int[] w = raw([1]); return reflect.typeBindings(w); });
`

const genericDeclWant = `wrapExplicit: [5]
wrapInferred: ["a"]
tagsUnbound: {}
nestedStmt: 1
closureDecl: 2
closureRetUnbound: [1]
closureRetBound: rejected: <closure> expects list<T> return, got list<int>
boxExplicit: [3]
boxInferred: [3]
boxSet: 1
subclassLocal: ["s"]
donorWrap: [1]
donorCount: 2
donorBag: ["s"]
donorBagDict: 1
donorClosure: [2]
donorClosureRetInferred: rejected: <closure> expects list<T> return, got list<string>
donorClosureRetBound: rejected: <closure> expects list<T> return, got list<string>
subclassInherited: [7]
concreteMismatch: rejected: type error: cannot assign list to int[] (element at index 0 is string)
concreteGenericMismatch: rejected: type error: cannot assign list to list<int> (element at index 0 is string)
concreteTag: {"T": "int"}
`

func TestParityGenericTypedDeclarations(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"generic_decl_donor.gb": genericDeclDonor}, genericDeclMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != genericDeclWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
