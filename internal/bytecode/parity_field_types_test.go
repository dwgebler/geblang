package bytecode_test

import "testing"

func TestParityFieldTypeEnforcement(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import json;
import reflect;
class Base {
    int count;
    ?string note;
    func bump(any v): void { this.count = v; }
}
class Sub extends Base {
    list<int> ids = [];
    decimal total;
    float rate;
}
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func put(any v): void { this.value = v; }
}
class Plain { string name; int age; }
let s = Sub();
any text = "x";
any nothing = null;
any mixed = [1, "a"];
any whole = 2;
let attempts = [
    func(): void { s.count = text; },
    func(): void { s.bump(text); },
    func(): void { s.note = nothing; },
    func(): void { s.count = nothing; },
    func(): void { s.ids = mixed; },
    func(): void { s.rate = whole; },
    func(): void { s.count = "lit"; },
    func(): void { Box<int>(1).put("one"); },
    func(): void { reflect.setField(s, "count", "r"); },
    func(): void { json.parseAs("{\"name\": \"n\", \"age\": \"old\"}", Plain); },
];
for (a in attempts) {
    try {
        a();
        io.println("ok");
    } catch (TypeError e) {
        io.println(e.message);
    }
}
s.total = 0;
s.rate = 2;
s.count = whole;
io.println("${typeof(s.total)} ${typeof(s.rate)} ${s.count}");
`)
	want := "cannot assign string to int field Sub.count\n" +
		"cannot assign string to int field Sub.count\n" +
		"ok\n" +
		"cannot assign null to int field Sub.count\n" +
		"cannot assign list to list<int> field Sub.ids (element at index 1 is string)\n" +
		"cannot assign int to float field Sub.rate\n" +
		"cannot assign string to int field Sub.count\n" +
		"cannot assign string to T field Box.value\n" +
		"cannot assign string to int field Sub.count\n" +
		"cannot assign string to int field Plain.age\n" +
		"decimal float 2\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityIntLiteralArgumentTyping(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func half(float x): float { return x / 2.0f; }
func price(decimal d): decimal { return d; }
func pick(int x): string { return "int"; }
func pick(float x): string { return "float"; }
class M {
    float r;
    func M(float r) { this.r = r; }
    func scale(float f): float { return this.r * f; }
    static func zero(float o): M { return M(o); }
}
let m = M(2);
io.println("${typeof(half(3))} ${typeof(price(4))} ${pick(2)} ${typeof(m.r)} ${m.scale(3)} ${typeof(M.zero(1).r)}");
`)
	want := "float decimal int float 6 float\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityAssignmentTargetEvaluatesBeforeValue(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box { int v; }
let trail = [];
let box = Box();
let items = [0, 0];
func target(): Box { trail.push("object"); return box; }
func list(): list<int> { trail.push("list"); return items; }
func index(): int { trail.push("index"); return 1; }
func value(): int { trail.push("value"); return 5; }
target().v = value();
list()[index()] = value();
io.println(trail);
`)
	want := "[\"object\", \"value\", \"list\", \"index\", \"value\"]\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityFieldTypeReviewCases(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import json;
type Cash = decimal;
type Label = int | string;
class Wallet { Cash balance; Label tag; }
class Defaults { float ratio = 1; decimal offset = -2; }
class Counters { static int count = 0; static float rate = 1; }
class Base<T> {
    T item;
    func setItem(any v): void { this.item = v; }
}
class Shadow<T> extends Base<int> {}
class Pass<U> extends Base<U> {}
class Priced { decimal amount; float rate; int qty; list<float> weights = []; }
let w = Wallet();
w.balance = 2.5;
any whole = 1;
w.tag = whole;
let d = Defaults();
Counters.rate = 3;
io.println("${typeof(w.balance)} ${w.tag} ${typeof(d.ratio)} ${d.offset} ${typeof(Counters.rate)}");
let attempts = [
    func(): void { any f = 1.5; w.tag = f; },
    func(): void { any s = "x"; Counters.count = s; },
    func(): void { Shadow<string>().setItem("s"); },
    func(): void { Pass<float>().setItem(1); },
    func(): void { json.parseAs("{\"qty\": 2.5}", Priced); },
];
for (a in attempts) {
    try {
        a();
        io.println("ok");
    } catch (TypeError e) {
        io.println(e.message);
    }
}
Shadow<string>().setItem(5);
let p = json.parseAs("{\"amount\": 10, \"rate\": 1, \"qty\": 3.0, \"weights\": [1]}", Priced);
io.println("${typeof(p.amount)} ${typeof(p.rate)} ${typeof(p.qty)} ${typeof(p.weights[0])}");
`)
	want := "decimal 1 float -2.0000000000 float\n" +
		"cannot assign decimal to int | string field Wallet.tag\n" +
		"cannot assign string to int field Counters.count\n" +
		"cannot assign string to T field Shadow.item\n" +
		"cannot assign int to T field Pass.item\n" +
		"cannot assign decimal to int field Priced.qty\n" +
		"decimal float int float\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityIntLiteralReviewCases(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func half(float x): float { return x / 2.0f; }
func mixed(int x, float y): string { return "if"; }
func mixed(float x, float y): string { return "ff"; }
func named(string label, float x = 0.5f): string { return typeof(x) as string; }
func again(): float { return half(-3); }
class Meter {
    float r;
    func Meter(float r) { this.r = r; }
    func scale(float f): float { return this.r * f; }
}
class Scaled extends Meter {
    func Scaled() { parent(4); }
    func scale(float f): float { return parent.scale(-1); }
}
float neg = -1;
let m = Meter(2.0f);
io.println("${typeof(neg)} ${half(-3)} ${mixed(1, 2)} ${named("a", x: 1)} ${again()} ${typeof(Scaled().r)} ${Scaled().scale(9.0f)} ${m.scale(3)}");
`)
	want := "float -1.5 if float -1.5 float -4 6\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParitySecondReviewCases(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import yaml;
type Label = int | string;
class Nested<T> {
    T item;
    func Nested(T item) { this.item = item; }
}
class ListOf<U> extends Nested<list<U>> {
    func ListOf(list<U> items) { parent(items); }
}
class Maybe extends Nested<?string> {
    func Maybe() { parent(null); }
}
class Tagged {
    Label label;
    func Tagged() { this.label = 1; }
}
class Exact { decimal d; }
class Ratio { float r; }
class Holder<T> {
    T item;
    func Holder(T item) { this.item = item; }
    func put(T item): void { this.item = item; }
}
class Picker {
    func m(float x): string { return "mf"; }
    func m(string x): string { return "ms"; }
}
export class Circle {
    float r = 1.0f;
    func scale(float k): float { return this.r * k; }
    func doubled(): float { return this.scale(2); }
}
class Animal { func feed(float a): string { return "A"; } }
class Dog extends Animal { func feed(int a): string { return "D"; } }
bool on = true;
let ratio = Ratio();
ratio.r = on ? 1 : 2;
float f = on ? -1 : 2;
let box = Holder<float>(1);
box.put(3);
Holder<decimal> exact = Holder(2);
Animal dog = Dog();
io.println("${ListOf<int>([1, 2]).item} ${Maybe().item} ${Tagged().label} ${yaml.parseAs("d: 0.1\n", Exact).d == 0.1}");
io.println("${typeof(ratio.r)} ${typeof(f)} ${typeof(box.item)} ${typeof(exact.item)} ${Picker().m(2)} ${Circle().doubled()} ${dog.feed(1)}");
`)
	want := "[1, 2] null 1 true\nfloat float float decimal mf 2 D\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityConditionalAndCoalesceLiteralBranches(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box {
    decimal amount;
    static decimal total = 0.0;
    func Box(decimal a) { this.amount = a; }
    func setThis(bool c, decimal d): void { this.amount = c ? d : 0; }
}
class Sub extends Box {
    func Sub() { parent(1.0); }
}
func ret(bool c, decimal d): decimal { return c ? d : 0; }
func take(decimal x): string { return typeof(x) as string; }
bool f = false;
decimal d1 = 2.5;
let b = Box(1.0);
b.setThis(f, d1);
io.println(typeof(b.amount) as string);
b.amount = f ? d1 : 3;
io.println("${typeof(b.amount)} ${b.amount}");
let s = Sub();
s.amount = f ? d1 : (f ? 1 : 2);
io.println("${typeof(s.amount)} ${s.amount}");
?decimal n = null;
s.amount = n ?? 4;
io.println("${typeof(s.amount)} ${s.amount}");
s.amount = d1 ?? 5;
io.println("${typeof(s.amount)} ${s.amount}");
Box.total = f ? d1 : 6;
io.println("${typeof(Box.total)} ${Box.total}");
decimal viaCoalesce = n ?? 7;
io.println("${typeof(viaCoalesce)} ${typeof(ret(f, d1))} ${take(f ? d1 : 8)}");
any count = 9;
try { b.amount = f ? d1 : count; } catch (TypeError e) { io.println(e.message); }
`)
	want := "decimal\ndecimal 3.0000000000\ndecimal 2.0000000000\ndecimal 4.0000000000\ndecimal 2.5000000000\ndecimal 6.0000000000\ndecimal decimal decimal\ncannot assign int to decimal field Box.amount\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityForwardReferenceWithDefaults(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class A {
    func m(): string { return j({"a": 1}) as string; }
}
func viaFunction(): string { return k(3); }
io.println(A().m());
io.println(viaFunction());
func j(any v, string key = "dflt"): any { return "${v} ${key}"; }
func k(int x, int y = 10, string z = "z"): string { return "${x + y}${z}"; }
`)
	want := "{\"a\": 1} dflt\n13z\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}
