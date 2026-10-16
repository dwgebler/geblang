package bytecode_test

import "testing"

const barrierDonorGb = `module staff;
func upper(string v): string { return v.upper(); }
func positive(int v): int {
    if (v <= 0) { throw ValueError("must be positive"); }
    return v;
}
export class Person {
    @upper
    string name;
    @positive
    int age;
    func Person(string name, int age) { this.name = name; this.age = age; }
}
`

func TestParityFieldDecoratorsRunForAssignmentsFromOtherModules(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"staff.gb": barrierDonorGb}, `import io;
import staff;
class Employee extends staff.Person {
    func Employee(string n) { parent(n, 30); }
    func relabel(string n): void { this.name = n; }
}
let p = staff.Person("ada", 36);
p.name = "grace";
io.println(p.name);
try { p.age = -1; } catch (ValueError e) { io.println("rejected ${e.message}"); }
p.age += 1;
io.println(p.age);
let e = Employee("kay");
e.relabel("lin");
io.println(e.name);
`)
	want := "GRACE\nrejected must be positive\n37\nLIN\n"
	if ev != want || vm != want {
		t.Errorf("cross-module field barrier:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityFieldDecoratorResolvesPastShadowingAndThroughInheritance(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func sku(string v): string { return v.trim().upper(); }
func tag(string v): string { return "#" + v; }
class Product {
    @sku
    string sku;
    @tag
    string label;
    func Product(string sku, string label) { this.sku = sku; this.label = label; }
}
class Special extends Product {
    func Special(string s) { parent(s, "x"); }
    func relabel(string l): void { this.label = l; }
}
let p = Product(" ab-1 ", "a");
io.println("${p.sku} ${p.label}");
let s = Special("cd");
s.relabel("y");
io.println("${s.sku} ${s.label}");
`)
	want := "AB-1 #a\nCD #y\n"
	if ev != want || vm != want {
		t.Errorf("field barrier shadowing/inheritance:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
