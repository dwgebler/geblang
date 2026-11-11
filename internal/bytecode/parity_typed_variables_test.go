package bytecode_test

import (
	"testing"
)

const typedVariablesDonor = `module typed_vars_donor;

export func ident(any x): any { return x; }

export class Base {
    func Base() {}
    func pick(any x): string { string s = ident(x); return s; }
    func swap(any x): int { int n = 0; n = ident(x); return n; }
}
`

const typedVariablesMain = `import io;
import reflect;
import typed_vars_donor as donor;

func raw(any x): any { return x; }
interface Named { func name(): string; }
class Dog implements Named { func Dog() {} func name(): string { return "d"; } }
class Puppy extends Dog { func Puppy() { parent(); } }
class Cat { func Cat() {} }
class Child extends donor.Base { func Child() { parent(); } }
enum Color { Red, Green }
string shared = "g";

func run(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (TypeError e) { io.println("${label}: rejected: ${e.message}"); }
}
func typedParam(int x): int { x = raw("s"); return x; }
func unionParam(list<int> | string x): any { x = raw([1]); return reflect.typeBindings(x); }
func nested(): int {
    int c = 1;
    func inner(): int { c = raw("s"); return c; }
    return inner();
}

run("crossDecl", func(): any { string t = donor.ident(5); return t; });
run("localDecl", func(): any { string u = raw(5); return u; });
run("okDecl", func(): any { string u = raw("s"); return u; });
run("floatDecl", func(): any { float f = raw(5); return f; });
run("reassign", func(): any { string v = "a"; v = raw(5); return v; });
run("global", func(): any { shared = raw(1); return shared; });
run("reassignLit", func(): any { float f = 1.0f; f = 5; return typeof(f); });
run("decimalLit", func(): any { decimal d = 1.0; d = 2; return typeof(d); });
run("ternaryLit", func(): any { decimal d = 1.0; d = true ? 2 : 3; return typeof(d); });
run("compound", func(): any { float g = 1.0f; g += 1; return typeof(g); });
run("intCompound", func(): any { int i = 0; i += raw(2); return i; });
run("intCompoundBad", func(): any { int i = 0; i += raw(2.5f); return i; });
run("nullableOk", func(): any { ?string s = raw(null); return s; });
run("nullBad", func(): any { string s = raw(null); return s; });
run("iface", func(): any { Named n = raw(Cat()); return n; });
run("ifaceOk", func(): any { Named n = raw(Puppy()); return n.name(); });
run("subclass", func(): any { Dog d = raw(Puppy()); return d.name(); });
run("enumBad", func(): any { Color c = raw(1); return c; });
run("enumOk", func(): any { Color c = raw(Color.Red); return c; });
run("funcTyped", func(): any { func f = raw(3); return f; });
run("loop", func(): any { for (string n in raw([1])) { return n; } return null; });
run("loopOk", func(): any { int t = 0; for (int n in raw([1, 2])) { t += n; } return t; });
run("loopRange", func(): any { int t = 0; for (int n in 0..3) { t += n; } return t; });
run("loopBodyReassign", func(): any { for (int n in [1]) { n = raw("s"); } return null; });
run("param", func(): any { return typedParam(1); });
run("lambdaParam", func(): any { let f = func(int x): int { x = raw("s"); return x; }; return f(1); });
run("variadic", func(): any { let f = func(int ...xs): int { xs = [9]; return xs.length(); }; return f(1, 2); });
run("captured", func(): any { int c = 1; let f = func(): int { c = raw("s"); return 0; }; return f(); });
run("nestedCapture", func(): any { return nested(); });
run("listReassign", func(): any { list<int> xs = [1]; xs = raw(["s"]); return xs; });
run("untyped", func(): any { let w = 1; w = raw(2); return w; });
run("crossMethodDecl", func(): any { return donor.Base().pick(1); });
run("crossMethodReassign", func(): any { return donor.Base().swap("s"); });
run("unionParamUntagged", func(): any { return unionParam("s"); });
run("unionParamBad", func(): any { return unionParam(raw(true)); });
run("inheritedMethod", func(): any { return Child().pick(1); });
`

const typedVariablesWant = `crossDecl: rejected: type error: cannot assign int to string
localDecl: rejected: type error: cannot assign int to string
okDecl: s
floatDecl: rejected: type error: cannot assign int to float
reassign: rejected: type error: cannot assign int to string
global: rejected: type error: cannot assign int to string
reassignLit: float
decimalLit: decimal
ternaryLit: decimal
compound: float
intCompound: 2
intCompoundBad: rejected: type error: cannot assign float to int
nullableOk: null
nullBad: rejected: type error: cannot assign null to string
iface: rejected: type error: cannot assign Cat to Named
ifaceOk: d
subclass: d
enumBad: rejected: type error: cannot assign int to Color
enumOk: Color.Red
funcTyped: rejected: type error: cannot assign int to func
loop: rejected: type error: cannot assign int to string
loopOk: 3
loopRange: 6
loopBodyReassign: rejected: type error: cannot assign string to int
param: rejected: type error: cannot assign string to int
lambdaParam: rejected: type error: cannot assign string to int
variadic: 1
captured: rejected: type error: cannot assign string to int
nestedCapture: rejected: type error: cannot assign string to int
listReassign: rejected: type error: cannot assign list to list<int> (element at index 0 is string)
untyped: 2
crossMethodDecl: rejected: type error: cannot assign int to string
crossMethodReassign: rejected: type error: cannot assign string to int
unionParamUntagged: {}
unionParamBad: rejected: unionParam expects list<int> | string for parameter 'x', got bool
inheritedMethod: rejected: type error: cannot assign int to string
`

func TestParityTypedVariableRuntimeChecks(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"typed_vars_donor.gb": typedVariablesDonor}, typedVariablesMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != typedVariablesWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
