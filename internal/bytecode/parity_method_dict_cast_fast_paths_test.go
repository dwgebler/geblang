package bytecode_test

import (
	"strings"
	"testing"
)

const constKeyDictProgram = `import io;
import json;

string log = "";

func note(string tag, int v): int {
    log = log + tag;
    return v;
}

func build(int i): dict<string, int> {
    return {"a": i, "b": i + 1, "c": i + 2};
}

class Holder {
    dict<string, any> meta = {"kind": "holder", "n": 1};
    func describe(): dict<string, any> { return {"self": this.meta["kind"], "n": this.meta["n"]}; }
}

let dup = {"b": 1, "a": 2, "b": 3};
io.println(dup.keys());
io.println(dup["b"]);
let ordered = {"z": note("1", 1), "y": note("2", 2), "x": note("3", 3)};
io.println(log);
io.println(ordered.keys());
string k = "dyn";
let mixed = {"a": 1, k: 2, "${k}x": 3};
io.println(mixed.keys());
io.println({"one": 1});
io.println({"one": 1, "two": 2});
io.println({"a": 1, "b": 2, "c": 3, "d": 4});
io.println({"a": 1, "b": 2, "c": 3, "d": 4, "e": 5});
let eight = {"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8};
eight["i"] = 9;
eight["j"] = 10;
eight.remove("a");
io.println(eight.keys());
let nine = {"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8, "i": 9};
io.println(nine.length());
let small = {"p": 1};
small["q"] = 2;
small["r"] = 3;
io.println(small);
io.println({"1": "s"}.keys());
io.println([{"a": 1}, {"a": 2}]);
io.println({"outer": {"inner": {"deep": true}}});
let maker = func(int v): dict<string, int> { return {"v": v}; };
io.println(maker(4));
io.println(build(10));
io.println(Holder().describe());
io.println(json.stringify({"b": 1, "a": [1, 2]}));
dict<string, int> typed = {"a": 1};
try {
    let bad = typed as any;
    typed["b"] = (bad as dict<string, any>)["a"] as int;
    io.println(typed);
} catch (Error e) {
    io.println("error " + e.message);
}
`

func TestParityConstKeyDictLiterals(t *testing.T) {
	ev, vm := parityOutputs(t, constKeyDictProgram)
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	want := `["b", "a"]
3
123
["z", "y", "x"]
["a", "dyn", "dynx"]
{"one": 1}
`
	if len(ev) < len(want) || ev[:len(want)] != want {
		t.Fatalf("unexpected output start:\n%s", ev)
	}
}

const castFastPathProgram = `import io;

type Flag = bool;

func asIntParam<int>(any v): any { return v as int; }

io.println(5 as int);
io.println(typeof(5 as int));
io.println("s" as string);
io.println(1.5 as float);
io.println(true as bool);
io.println(5 as string);
io.println(5 as float);
io.println(5 as Flag);
io.println(asIntParam<bool>(5));
io.println(typeof(asIntParam<string>(5)));
?int maybe = null;
io.println(maybe as ?int);
try {
    io.println("abc" as int);
} catch (Error e) {
    io.println("${e.class}: ${e.message}");
}
`

const castAliasIntProgram = `import io;

type int = bool;

io.println(5 as int);
io.println(typeof(5 as int));
`

func TestParityCastFastPathRespectsRebinding(t *testing.T) {
	for name, program := range map[string]string{"cast": castFastPathProgram, "aliasInt": castAliasIntProgram} {
		ev, vm := parityOutputs(t, program)
		if vm != ev {
			t.Fatalf("%s: vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", name, vm, ev)
		}
	}
	ev, _ := parityOutputs(t, castAliasIntProgram)
	if ev != "true\nbool\n" {
		t.Fatalf("alias int output = %q", ev)
	}
}

const zeroArgPrimitiveProgram = `import io;
import re;
import bytes;

class Ranked {
    int r;
    func Ranked(int r) { this.r = r; }
    func __lt(Ranked other): bool { return this.r < other.r; }
    func __string(): string { return "R${this.r}"; }
}

let s = "héllo";
let xs = [3, 1, 2];
let d = {"a": 1, "b": 2};
set<int> st = {1, 2, 3};
let b = bytes.fromString("ab");
io.println(s.length());
io.println(s.upper());
io.println(xs.length());
io.println(xs.reversed());
io.println(d.length());
io.println(d.keys());
io.println(st.length());
io.println(b.length());
let ranked = [Ranked(3), Ranked(1), Ranked(2)];
ranked.sort();
io.println(ranked);
io.println(xs.pop());
io.println(xs);
let p = re.compile("[a-z]+");
io.println(p.test("abc"));
io.println(d.get("a") as int);
try {
    io.println("x".nope());
} catch (Error e) {
    io.println("${e.class}: ${e.message}");
}
try {
    io.println(xs.length(1));
} catch (Error e) {
    io.println("${e.class}: ${e.message}");
}
func counts(): int {
    int total = 0;
    for (int i = 0; i < 50; i++) {
        total = total + s.length() + xs.length() + (d.get("a") as int);
    }
    return total;
}
io.println(counts());
`

func TestParityZeroArgPrimitiveMethods(t *testing.T) {
	ev, vm := parityOutputs(t, zeroArgPrimitiveProgram)
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
}

func TestParityConstKeyDictAcrossModules(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "dictdonor", `module dictdonor;
let shared = {"x": 1, "y": 2};
export func make(int n): dict<string, int> { return {"n": n, "twice": n * 2}; }
export func sharedKeys(): list<string> { return shared.keys(); }
export func lengthOf(string s): int { return s.length(); }
`)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import dictdonor;

io.println(dictdonor.make(3));
io.println(dictdonor.make(4)["twice"]);
io.println(dictdonor.sharedKeys());
io.println(dictdonor.lengthOf("abcd"));
`)
	want := "{\"n\": 3, \"twice\": 6}\n8\n[\"x\", \"y\"]\n4\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
}

const primitiveArityMessagesProgram = `import io;
import bytes;

func check(callable f): void {
    try {
        f();
        io.println("no error");
    } catch (Error e) {
        io.println("${e.class}: ${e.message}");
    }
}

check(func(): void { "ab".length(1); });
check(func(): void { [1].isEmpty(1); });
check(func(): void { {"a": 1}.isEmpty(1); });
check(func(): void { "ab".contains(); });
check(func(): void { [1].contains(); });
check(func(): void { {1}.contains(); });
check(func(): void { bytes.fromString("a").contains(); });
check(func(): void { "ab".startsWith(); });
check(func(): void { "ab".endsWith(); });
check(func(): void { "ab".indexOf(); });
check(func(): void { [1].indexOf(); });
check(func(): void { "ab".get(); });
check(func(): void { {"a": 1}.get(); });
check(func(): void { bytes.fromString("a").get(); });
check(func(): void { [1].set(1); });
check(func(): void { {"a": 1}.set(1); });
check(func(): void { [1].insert(1); });
check(func(): void { {"a": 1}.remove(); });
check(func(): void { {1}.remove(); });
check(func(): void { [1].toList(1); });
check(func(): void { {1}.toList(1); });
check(func(): void { [1].first(1); });
check(func(): void { [1].last(1); });
check(func(): void { {"a": 1}.clear(1); });
`

func TestParityPrimitiveArityMessages(t *testing.T) {
	ev, vm := parityOutputs(t, primitiveArityMessagesProgram)
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	for _, want := range []string{"string.length expects no arguments", "list.isEmpty expects no arguments", "bytes.contains expects one argument", "list.insert expects two arguments (index, item)", "dict.clear expects no arguments"} {
		if !strings.Contains(ev, want) {
			t.Fatalf("missing %q in:\n%s", want, ev)
		}
	}
}

const primMethodFallbackProgram = `import io;
import bytes;

class Bag {
    list<int> items = [1, 2];
    func length(): int { return 100 + this.items.length(); }
    func isEmpty(): bool { return false; }
    func get(int i): int { return this.items[i] * 10; }
    func contains(any v): string { return "bag-${v}"; }
}

func show(string label, callable f): void {
    try {
        io.println(label + " " + "${f()}");
    } catch (Error e) {
        io.println(label + " ERR ${e.class}: ${e.message}");
    }
}

let b = Bag();
show("bag.length", func(): any { return b.length(); });
show("bag.isEmpty", func(): any { return b.isEmpty(); });
show("bag.get", func(): any { return b.get(1); });
show("bag.contains", func(): any { return b.contains(3); });
let d = {"a": 1, 2: "two"};
show("dict.get.hit", func(): any { return d.get("a"); });
show("dict.get.miss", func(): any { return d.get("zz"); });
show("dict.get.intkey", func(): any { return d.get(2); });
show("dict.get.default", func(): any { return d.get("zz", 9); });
show("dict.contains", func(): any { return [d.contains("a"), d.contains("zz"), d.contains(2)]; });
let xs = [10, 20, 30];
show("list.get", func(): any { return [xs.get(0), xs.get(2), xs.get(-1), xs.get(5), xs.get(-9)]; });
show("list.get.str", func(): any { return xs.get("x"); });
show("list.contains", func(): any { return [xs.contains(20), xs.contains(99)]; });
show("str.get", func(): any { return ["héllo".get(1), "abc".get(-1), "abc".get(7)]; });
show("str.contains", func(): any { return ["abc".contains("b"), "abc".contains("z")]; });
show("str.contains.int", func(): any { return "abc".contains(1); });
set<int> st = {1, 2};
show("set", func(): any { return [st.length(), st.isEmpty(), st.contains(2), st.contains(5)]; });
let bs = bytes.fromString("ab");
show("bytes", func(): any { return [bs.length(), bs.isEmpty(), bs.get(0), bs.contains(97)]; });
show("range", func(): any { return [range(1, 4).length(), range(1, 4).isEmpty(), range(1, 4).contains(2)]; });
show("empty", func(): any { return ["".isEmpty(), [].isEmpty(), {}.isEmpty()]; });
?string maybe = null;
show("optional", func(): any { return maybe?.length(); });
string some = "abc";
show("optional.some", func(): any { return some?.length(); });
show("null.length", func(): any { any z = null; return z.length(); });
show("int.length", func(): any { any z = 5; return z.length(); });
show("get.wrongtype", func(): any { any z = 5; return z.get(1); });
`

func TestParityPrimMethodFallbacks(t *testing.T) {
	ev, vm := parityOutputs(t, primMethodFallbackProgram)
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	for _, want := range []string{"bag.length 102", "bag.get 20", "bag.contains bag-3", "dict.get.miss null", "dict.get.intkey two", "dict.get.default ERR RuntimeError: dict.get expects one argument", "list.get [10, 30, 30, null, null]"} {
		if !strings.Contains(ev, want) {
			t.Fatalf("missing %q in:\n%s", want, ev)
		}
	}
}
