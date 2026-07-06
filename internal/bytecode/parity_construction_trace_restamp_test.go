package bytecode_test

import "testing"

// 9.32: a boundary error raised before any callee frame exists (Instruction{}, line 0) must restamp the caller's frame line.
const traceRestampDonorGb = `module helpers;
export class Point {
    int x;
    int y;
    func Point(int x, int y = 10) { this.x = x; this.y = y; }
}
export class Utils {
    static func sMethod(int a, int b): int { return a + b; }
    func iMethod(int a, int b): int { return a + b; }
}
export class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}
export class Zero {
}
export func addTwo(int a, int b): int { return a + b; }
export func namedOnly(int a, int b): int { return a + b; }
`

func traceRestampModules() map[string]string {
	return map[string]string{"helpers": traceRestampDonorGb}
}

func TestUncaughtCrossModuleConstructPositionalMissTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func makeIt(): any {
    return helpers.Point(1, 2, 3);
}

makeIt();
`)
	want := "uncaught RuntimeError: no matching overload for Point\n  at makeIt (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}

func TestUncaughtCrossModuleConstructNamedMissTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func makeItNamed(): any {
    return helpers.Point(z: 9);
}

makeItNamed();
`)
	want := "uncaught RuntimeError: no matching overload for Point\n  at makeItNamed (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}

// Two intervening user frames: the restamp must reach through both, not just the innermost.
func TestUncaughtCrossModuleConstructTwoFrameTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func wrapperB(): any {
    return helpers.Point(1, 2, 3);
}

func wrapperA(): any {
    return wrapperB();
}

wrapperA();
`)
	want := "uncaught RuntimeError: no matching overload for Point\n  at wrapperB (line 4)\n  at wrapperA (line 8)\n  at <top level> (line 11)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}

// Generic ctor type mismatch raises via throwTyped (the vmThrownError branch of propagateModuleError, not vmRuntimeError); both need the restamp.
func TestUncaughtCrossModuleGenericConstructorTypeMismatchTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func makeBox(): any {
    return helpers.Box<int>("nope");
}

makeBox();
`)
	want := "uncaught RuntimeError: Box expects T for parameter 'value', got string\n  at makeBox (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}

// Sweep: static-method miss, same line-drop shape; the bare-vs-qualified wording gap is the separate tracked ISSUE-029.
func TestUncaughtCrossModuleStaticMethodMissTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func staticMiss(): any {
    return helpers.Utils.sMethod(1, 2, 3);
}

staticMiss();
`)
	wantEval := "uncaught RuntimeError: no matching overload for Utils.sMethod\n  at staticMiss (line 4)\n  at <top level> (line 7)"
	wantVM := "uncaught RuntimeError: no matching overload for sMethod\n  at staticMiss (line 4)\n  at <top level> (line 7)"
	if evGot != wantEval {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, wantEval)
	}
	if vmGot != wantVM {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, wantVM)
	}
}

// Sweep: same shape as the static-method case, on a cross-module instance method.
func TestUncaughtCrossModuleInstanceMethodMissTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func instanceMiss(): any {
    let u = helpers.Utils();
    return u.iMethod(1, 2, 3);
}

instanceMiss();
`)
	wantEval := "uncaught RuntimeError: no matching overload for Utils.iMethod\n  at instanceMiss (line 5)\n  at <top level> (line 8)"
	wantVM := "uncaught RuntimeError: no matching overload for imethod\n  at instanceMiss (line 5)\n  at <top level> (line 8)"
	if evGot != wantEval {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, wantEval)
	}
	if vmGot != wantVM {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, wantVM)
	}
}

// Sweep: a non-overloaded free-function arity miss hits the same Instruction{} boundary via CallModuleFunction -> executeFunctionDirect.
func TestUncaughtCrossModuleFunctionArityMissTrace(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func callFn(): any {
    return helpers.addTwo(1, 2, 3);
}

callFn();
`)
	want := "uncaught RuntimeError: addTwo expects at most 2 arguments, got 3\n  at callFn (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (line restamp regression):\n got %q\nwant %q", vmGot, want)
	}
}

// Regression pin: the 9.27 named-function-miss path raises directly on the caller VM and already had the correct line; guard it stays correct.
func TestUncaughtCrossModuleNamedFunctionMissTraceRegression(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func callFnNamed(): any {
    return helpers.namedOnly(zzz: 9);
}

callFnNamed();
`)
	want := "uncaught RuntimeError: namedOnly has no parameter zzz\n  at callFnNamed (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch:\n got %q\nwant %q", vmGot, want)
	}
}

// 9.33: the VM's positional zero-constructor wording must match the evaluator's.
func TestUncaughtZeroCtorPositionalWordingSameModule(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackends(t, `class Zero {
}

func makeZero(): any {
    return Zero(1, 2, 3);
}

makeZero();
`)
	want := "uncaught RuntimeError: Zero constructor expects no arguments\n  at makeZero (line 5)\n  at <top level> (line 8)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (wording regression):\n got %q\nwant %q", vmGot, want)
	}
}

func TestUncaughtZeroCtorPositionalWordingCrossModule(t *testing.T) {
	evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, traceRestampModules(), `import helpers;

func makeZero(): any {
    return helpers.Zero(1, 2, 3);
}

makeZero();
`)
	want := "uncaught RuntimeError: Zero constructor expects no arguments\n  at makeZero (line 4)\n  at <top level> (line 7)"
	if evGot != want {
		t.Fatalf("evaluator mismatch:\n got %q\nwant %q", evGot, want)
	}
	if vmGot != want {
		t.Fatalf("vm mismatch (wording regression):\n got %q\nwant %q", vmGot, want)
	}
}
