package bytecode_test

import (
	"testing"
)

const capturedNestedGenericsDonor = `module captured_nested_donor;

export class Node { int v = 1; }

export class Base<T> {
    T value;
    func Base(T value) { this.value = value; }
    func listReader(): callable { return func(): list<T> { return [this.value]; }; }
    func dictReader(): callable { return func(): dict<string, T[]> { return {"k": [this.value]}; }; }
}

export func listOf<T>(T c): callable { return func(): list<T> { return [c]; }; }
export func wrongList<T>(T c): callable { return func(): list<T> { return ["bad"]; }; }
export func acceptList<T>(T c): callable { return func(list<T> xs): int { return xs.length(); }; }
export func plain(): callable {
    func inner<U>(U own): U { return own; }
    return inner;
}
export func mixed<T>(T c): callable {
    func inner<U>(U own): dict<U, T> { return {own: c}; }
    return inner;
}
`

const capturedNestedGenericsMain = `import io;
import async;
import captured_nested_donor as donor;

func run(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (TypeError e) { io.println("${label}: rejected: ${e.message}"); }
}

func listOf<T>(T c): callable { return func(): list<T> { return [c]; }; }
func unionOf<T>(T c): callable { return func(): list<T> | string { return [c]; }; }
func nested<T>(T c): callable { return func(): dict<string, list<T>> { return {"k": [c]}; }; }
func setOf<T>(T c): callable { return func(): set<T> { return {c}; }; }
func arrOf<T>(T c): callable { return func(): T[] { return [c]; }; }
func nullableArr<T>(T c): callable { return func(): ?T[] { return null; }; }
func named<T>(T c): callable {
    func read(int n): dict<string, T> { return {"k": c}; }
    return read;
}
func wrongArr<T>(T c): callable { return func(): T[] { return ["bad"]; }; }
func wrongNested<T>(T c): callable { return func(): dict<string, list<T>> { return {"k": ["bad"]}; }; }
func ownArr<T>(T c): T[] { return [c]; }
func ownArrParam<T>(T[] xs): int { return xs.length(); }
func paramList<T>(T c): callable { return func(list<T> xs): int { return xs.length(); }; }
func paramWrong<T>(T c): callable { return func(list<T> xs): int { return xs.length(); }; }

class Child extends donor.Base<int> {
    func Child() { parent(5); }
    func ownReader(): callable { return func(): list<int> { return [this.value]; }; }
}

run("list", func(): any { return listOf<int>(1)(); });
run("union", func(): any { return unionOf<int>(1)(); });
run("nested", func(): any { return nested<int>(1)(); });
run("set", func(): any { return setOf<int>(1)(); });
run("arr", func(): any { return arrOf<int>(1)(); });
run("nullableArr", func(): any { return nullableArr<int>(1)(); });
run("named", func(): any { return named<int>(1)(0); });
run("class", func(): any { return listOf<donor.Node>(donor.Node())().length(); });
run("wrongArr", func(): any { return wrongArr<int>(1)(); });
run("wrongNested", func(): any { return wrongNested<int>(1)(); });
run("ownArr", func(): any { return ownArr<int>(1); });
run("ownArrParam", func(): any { return ownArrParam<int>([1, 2]); });
run("paramList", func(): any { return paramList<int>(1)([1, 2]); });
run("paramWrong", func(): any { return paramWrong<int>(1)(["x"]); });
run("donorList", func(): any { return donor.listOf<string>("s")(); });
run("donorWrong", func(): any { return donor.wrongList<int>(1)(); });
run("donorParam", func(): any { return donor.acceptList<int>(1)([3]); });
run("donorMixed", func(): any { return donor.mixed<int>(1)("k"); });
run("baseMethod", func(): any { return donor.Base<string>("b").listReader()(); });
run("baseDict", func(): any { return donor.Base<int>(2).dictReader()(); });
run("childInherited", func(): any { return Child().listReader()(); });
run("childOwn", func(): any { return Child().ownReader()(); });
run("callback", func(): any { return [1, 2].map(func(int n): any { return listOf<int>(n)(); }); });
run("task", func(): any { return async.await(async.run(listOf<int>(7))); });
any badList = ["x"];
let lambdaList = func(list<int> xs): int { return xs.length(); };
let lambdaDict = func(dict<string, int> d): int { return d.length(); };
run("lambdaList", func(): any { return lambdaList(badList); });
run("lambdaDict", func(): any { return lambdaDict({"a": "x"}); });
run("lambdaOk", func(): any { return lambdaList([1, 2]); });
run("lambdaCallback", func(): any { return [badList].map(lambdaList); });
run("donorPlain", func(): any { return donor.plain()("u"); });
`

const capturedNestedGenericsWant = `list: [1]
union: [1]
nested: {"k": [1]}
set: set{1}
arr: [1]
nullableArr: null
named: {"k": 1}
class: 1
wrongArr: rejected: <closure> expects T[] return, got list<string>
wrongNested: rejected: <closure> expects dict<string, list<T>> return, got dict<string,list>
ownArr: [1]
ownArrParam: 2
paramList: 2
paramWrong: rejected: <closure> expects list<T> for parameter 'xs', got list (element at index 0 is string)
donorList: ["s"]
donorWrong: rejected: <closure> expects list<T> return, got list<string>
donorParam: 1
donorMixed: {"k": 1}
baseMethod: ["b"]
baseDict: {"k": [2]}
childInherited: [5]
childOwn: [5]
callback: [[1], [2]]
task: [7]
lambdaList: rejected: <closure> expects list<int> for parameter 'xs', got list (element at index 0 is string)
lambdaDict: rejected: <closure> expects dict<string, int> for parameter 'd', got dict (value for key a is string)
lambdaOk: 2
lambdaCallback: rejected: <closure> expects list<int> for parameter 'xs', got list (element at index 0 is string)
donorPlain: u
`

func TestParityCapturedGenericsInNestedTypes(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"captured_nested_donor.gb": capturedNestedGenericsDonor}, capturedNestedGenericsMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != capturedNestedGenericsWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
