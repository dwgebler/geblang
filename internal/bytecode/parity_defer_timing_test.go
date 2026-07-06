package bytecode_test

import "testing"

// 9.10: a deferred call's argument is frozen at registration time; a later reassignment does not change what runs.
func TestParityDeferArgFrozenAtRegistration(t *testing.T) {
	runParity(t, `import io;
func show(int x): void { io.println(x); }
func work(): void {
    int x = 1;
    defer show(x);
    x = 99;
}
work();
`, "1\n")
}

// 9.10: loop defers freeze the loop variable per iteration and run last-in-first-out.
func TestParityDeferLoopPerIteration(t *testing.T) {
	runParity(t, `import io;
func work(): void {
    for (int i = 0; i < 3; i = i + 1) {
        defer io.println(i);
    }
}
work();
`, "2\n1\n0\n")
}

// 9.10: a deferred method's receiver is frozen at registration (a reassignment of the variable does not change the receiver).
func TestParityDeferReceiverFrozen(t *testing.T) {
	runParity(t, `import io;
class G { string n; func G(string n) { this.n = n; } func hi(): void { io.println(this.n); } }
func work(): void {
    G obj = G("A");
    defer obj.hi();
    obj = G("B");
}
work();
`, "A\n")
}

// 9.10: the frozen receiver is captured by reference, so an in-place field mutation is visible when the defer runs.
func TestParityDeferReceiverInPlaceMutationVisible(t *testing.T) {
	runParity(t, `import io;
class C { int n; func C() { this.n = 0; } func show(): void { io.println(this.n); } }
func work(): void {
    C c = C();
    defer c.show();
    c.n = 42;
}
work();
`, "42\n")
}

// 9.10: a deferred variable-held callable is frozen at registration; reassigning the variable does not change what runs.
func TestParityDeferCallableVarFrozen(t *testing.T) {
	runParity(t, `import io;
func showA(): void { io.println("A"); }
func showB(): void { io.println("B"); }
func work(): void {
    let f = showA;
    defer f();
    f = showB;
}
work();
`, "A\n")
}

// 9.10: a deferred call to a declared function uses the function bound at registration, even when a later local shadows the name.
func TestParityDeferFunctionNameShadow(t *testing.T) {
	runParity(t, `import io;
func helper(): void { io.println("declared"); }
func work(): void {
    defer helper();
    let helper = func(): void { io.println("shadow"); };
    io.println("body");
}
work();
`, "body\ndeclared\n")
}

// 9.10: a deferred argument expression that throws does so at registration time (before the rest of the body runs), matching the docs.
func TestParityDeferArgThrowsAtRegistration(t *testing.T) {
	runErrorParity(t, `import io;
func risky(): int { throw RuntimeError("boom"); }
func show(int x): void { io.println("show"); }
func work(): void {
    io.println("before");
    defer show(risky());
    io.println("after");
}
work();
`, "boom")
}

// 9.10: a module-qualified deferred call freezes its argument at registration time (previously the VM thunk captured it at execution time).
func TestParityDeferModuleQualifiedArgFrozen(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": "module dm;\nimport io;\nexport func show(int x): void { io.println(x); }\n"},
		"import io;\nimport dm;\nfunc work(): void {\n    int x = 1;\n    defer dm.show(x);\n    x = 99;\n}\nwork();\n",
		"1\n")
}

// 9.10: a nested-selector static deferred call (module.Class.staticMethod) freezes its argument at registration time.
func TestParityDeferNestedStaticArgFrozen(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": "module dm;\nimport io;\nexport class Box { static func stamp(int x): void { io.println(x); } }\n"},
		"import io;\nimport dm;\nfunc work(): void {\n    int x = 5;\n    defer dm.Box.stamp(x);\n    x = 500;\n}\nwork();\n",
		"5\n")
}

// 9.10: a numeric literal argument to an overloaded deferred target keeps its concrete type (hint fidelity), selecting the same overload the direct call would.
func TestParityDeferOverloadHintFidelity(t *testing.T) {
	runParity(t, `import io;
func pick(int x): void { io.println("int"); }
func pick(decimal x): void { io.println("decimal"); }
func work(): void {
    defer pick(5);
    defer pick(5.0);
}
work();
`, "decimal\nint\n")
}

// 9.10: defers in stacked frames each run at their own frame's exit, in registration order per frame.
func TestParityDeferNestedFrames(t *testing.T) {
	runParity(t, `import io;
func inner(int v): void {
    defer io.println("inner");
    io.println(v);
}
func outer(): void {
    int y = 1;
    defer io.println(y);
    inner(9);
    y = 100;
}
outer();
`, "9\ninner\n1\n")
}

// 9.20: a spread argument in a defer expands into a variadic function's collected list; the frozen list reference is read at fire time, so an in-place mutation after registration is visible.
func TestParityDeferSpreadVariadic(t *testing.T) {
	runParity(t, `import io;
func showAll(int ...xs): void { io.println(xs); }
func work(): void {
    list<int> xs = [1, 2, 3];
    defer showAll(...xs);
    xs.push(9);
}
work();
`, "[1, 2, 3, 9]\n")
}

// 9.20: mixed positional + spread in a defer, with the spread's leading fixed parameter positional and the mutation-visible reference behavior together.
func TestParityDeferSpreadMixedPositional(t *testing.T) {
	runParity(t, `import io;
func showLead(int lead, int ...rest): void { io.println("${lead}: ${rest}"); }
func work(): void {
    list<int> xs = [2, 3];
    defer showLead(1, ...xs);
    xs.push(99);
}
work();
`, "1: [2, 3, 99]\n")
}

// 9.20: a spread argument in a defer to a fixed-arity function fails at fire time (not compile time) when the frozen list's current length no longer matches the arity, on both backends.
func TestParityDeferSpreadFixedArityMismatch(t *testing.T) {
	runErrorParity(t, `import io;
func show(int a, int b, int c): void { io.println("show: ${a} ${b} ${c}"); }
func work(): void {
    list<int> xs = [1, 2, 3];
    defer show(...xs);
    xs.push(9);
}
work();
`, "show", "4")
}

// 9.20: a spread argument in a defer to a fixed-arity function succeeds when the frozen list's element count matches the declared arity.
func TestParityDeferSpreadFixedAritySuccess(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c): void { io.println("show: ${a} ${b} ${c}"); }
func work(): void {
    list<int> xs = [1, 2, 3];
    defer show(...xs);
}
work();
`, "show: 1 2 3\n")
}

// 9.20: the deferred spread target's reference is frozen at registration; a later reassignment of the variable (not an in-place mutation) does not change what the deferred call sees.
func TestParityDeferSpreadReassignmentNotVisible(t *testing.T) {
	runParity(t, `import io;
func showAll(int ...xs): void { io.println(xs); }
func work(): void {
    list<int> xs = [1, 2, 3];
    defer showAll(...xs);
    xs = [7, 8];
}
work();
`, "[1, 2, 3]\n")
}

// 9.20: a spread argument in a deferred instance method call expands against the method's declared parameters, receiver frozen at registration.
func TestParityDeferSpreadMethodCall(t *testing.T) {
	runParity(t, `import io;
class Greeter {
    string name;
    func Greeter(string name) { this.name = name; }
    func greet(int a, int b): void { io.println("${this.name}: ${a} ${b}"); }
}
func work(): void {
    Greeter g = Greeter("G");
    list<int> xs = [1, 2];
    defer g.greet(...xs);
}
work();
`, "G: 1 2\n")
}

// 9.20: a spread argument in a module-qualified defer expands correctly and observes an in-place mutation of the frozen list, matching the local-function case.
func TestParityDeferSpreadModuleQualified(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"dm": "module dm;\nimport io;\nexport func showAll(int ...xs): void { io.println(xs); }\n",
	},
		"import io;\nimport dm;\nfunc work(): void {\n    list<int> xs = [1, 2];\n    defer dm.showAll(...xs);\n    xs.push(3);\n}\nwork();\n",
		"[1, 2, 3]\n")
}
