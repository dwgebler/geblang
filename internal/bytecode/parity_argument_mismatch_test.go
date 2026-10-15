package bytecode_test

import "testing"

func TestParitySingleSignatureArgumentMismatch(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"lib.gb": `module lib;
export class Acct {
    int n;
    func Acct(int n) { this.n = n; }
    func add(int k): void { this.n = this.n + k; }
    static func of(int n): Acct { return Acct(n); }
}
`}, `import io;
import lib;
func opaque(any v): any { return v; }
func greet(string name): string { return name; }
class User {
    string name;
    func User(string name) { this.name = name; }
    func rename(string name): void { this.name = name; }
    static func make(string name): User { return User(name); }
}
class Kid extends User {
    func Kid(any name) { parent(name); }
}
class Two {
    func Two(int a) {}
    func Two(string a, int b) {}
}
any missing = null;
any num = 5;
let u = User("a");
let attempts = [
    func(): void { greet(num); },
    func(): void { greet(missing); },
    func(): void { User(num); },
    func(): void { u.rename(num); },
    func(): void { u.rename(name: num); },
    func(): void { User.make(num); },
    func(): void { Kid(num); },
    func(): void { lib.Acct(opaque("s")); },
    func(): void { lib.Acct(1).add(opaque("s")); },
    func(): void { lib.Acct.of(opaque("s")); },
    func(): void { Two(opaque(true)); },
];
for (a in attempts) {
    try { a(); io.println("ok"); } catch (Error e) { io.println(e.class + ": " + e.message); }
}
`)
	want := "TypeError: greet expects string for parameter 'name', got int\n" +
		"TypeError: greet expects string for parameter 'name', got null\n" +
		"TypeError: User expects string for parameter 'name', got int\n" +
		"TypeError: User.rename expects string for parameter 'name', got int\n" +
		"TypeError: User.rename expects string for parameter 'name', got int\n" +
		"TypeError: User.make expects string for parameter 'name', got int\n" +
		"TypeError: User expects string for parameter 'name', got int\n" +
		"TypeError: Acct expects int for parameter 'n', got string\n" +
		"TypeError: Acct.add expects int for parameter 'k', got string\n" +
		"TypeError: Acct.of expects int for parameter 'n', got string\n" +
		"RuntimeError: no matching overload for Two\n"
	if ev != want || vm != want {
		t.Fatalf("argument mismatch:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityOverloadsChosenFromAnyArguments(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func f(int x): string { return "f-int"; }
func f(string x): string { return "f-str"; }
class C {
    string kind;
    func C(int x) { this.kind = "C-int"; }
    func C(string x) { this.kind = "C-str"; }
}
class Box<T> {
    string which;
    func Box(T value) { this.which = "generic"; }
    func Box(int value) { this.which = "int"; }
}
any a = 1;
any b = "z";
any raw = true;
io.println("${f(a)} ${f(b)} ${C(a).kind} ${C(b).kind} ${Box<string>(b).which}");
try { Box<string>(raw); } catch (TypeError e) { io.println(e.message); }
`)
	want := "f-int f-str C-int C-str generic\nBox expects T for parameter 'value', got bool\n"
	if ev != want || vm != want {
		t.Fatalf("overloads from any arguments:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}

func TestParityOverloadedMethodsWithAnyArguments(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Formatter {
    func show(int x): string { return "int"; }
    func show(string x): string { return "string"; }
    static func parse(int x): string { return "int"; }
    static func parse(string x): string { return "string"; }
}
any count = 3;
any label = "circle";
any flag = true;
let f = Formatter();
io.println("${f.show(count)} ${f.show(label)} ${Formatter.parse(count)} ${Formatter.parse(label)}");
try { f.show(flag); } catch (Error e) { io.println(e.class + ": " + e.message); }
try { Formatter.parse(flag); } catch (Error e) { io.println(e.class + ": " + e.message); }
`)
	want := "int string int string\n" +
		"RuntimeError: no matching overload for Formatter.show\n" +
		"RuntimeError: no matching overload for Formatter.parse\n"
	if ev != want || vm != want {
		t.Fatalf("overloaded methods with any:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}
