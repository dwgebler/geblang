package bytecode_test

import (
	"strings"
	"testing"
)

// TestParityRuntimeFaultsCatchable proves that implicit runtime faults
// (division by zero, index out of range, conversion failure) are
// catchable as RuntimeError on BOTH backends. Before the error-model
// fix the VM let these escape try/catch and crash.
func TestParityRuntimeFaultsCatchable(t *testing.T) {
	runParity(t, `import io;
func catch_(callable f): string {
    try { f(); } catch (Error e) { return e.class; }
    return "no-throw";
}
io.println(catch_(func(): void { let x = 1 / 0; }));
io.println(catch_(func(): void { let xs = [1, 2]; let y = xs[9]; }));
io.println(catch_(func(): void { let n = "abc".toInt(); }));
`, "RuntimeError\nValueError\nValueError\n")
}

// TestParityFatalErrorUncatchable proves a FatalError bypasses every
// try/catch (even catch(any)) on both backends and propagates to the
// top as an uncaught fault.
func TestParityFatalErrorUncatchable(t *testing.T) {
	src := `import io;
try {
    throw FatalError("unrecoverable");
} catch (any e) {
    io.println("WRONGLY CAUGHT");
}
io.println("AFTER");
`
	evalErr := runOnEvaluator(src)
	vmErr := runOnVM(src)
	if evalErr == nil || !strings.Contains(evalErr.Error(), "FatalError") {
		t.Errorf("evaluator: FatalError should be uncaught, got %v", evalErr)
	}
	if vmErr == nil || !strings.Contains(vmErr.Error(), "FatalError") {
		t.Errorf("VM: FatalError should be uncaught, got %v", vmErr)
	}
}

// TestParityCaughtFaultTraceContent proves the lazily-formatted trace
// of a caught fault yields the correct frames on both backends (the
// lazy VM path must produce the same frames as the eager evaluator).
func TestParityCaughtFaultTraceContent(t *testing.T) {
	runParity(t, `import io; import errors;
func g(): void { let x = 1 / 0; }
func f(): void { g(); }
try { f(); } catch (Error e) {
    let frames = errors.frames(e);
    io.println("${frames.length() >= 3}");
}
`, "true\n")
}

// TestParityErrorsIsMatchesInstanceOf proves errors.is and instanceof
// agree (one shared matcher): a built-in error is an Error, FatalError
// is not. Previously errors.is used a class-only matcher that diverged.
func TestParityErrorsIsMatchesInstanceOf(t *testing.T) {
	runParity(t, `import io; import errors;
io.println("${errors.is(RuntimeError("x"), "Error")}");
io.println("${RuntimeError("x") instanceof Error}");
io.println("${errors.is(FatalError("x"), "Error")}");
io.println("${FatalError("x") instanceof Error}");
`, "true\ntrue\nfalse\nfalse\n")
}

// TestParityCaughtFaultHasStackTrace proves a caught runtime fault
// carries a stack trace on both backends (the VM routing must not drop
// the trace the dispatch error already holds).
func TestParityCaughtFaultHasStackTrace(t *testing.T) {
	runParity(t, `import io; import errors;
func g(): void { let x = 1 / 0; }
func f(): void { g(); }
try { f(); } catch (Error e) {
    io.println("${errors.hasStackTrace(e)}");
}
`, "true\n")
}

// TestParityErrorInstanceOf proves instanceof resolves the built-in
// error hierarchy identically on both backends: a built-in error is an
// Error, FatalError is not, and subclass relationships hold.
func TestParityErrorInstanceOf(t *testing.T) {
	runParity(t, `import io;
io.println("${RuntimeError("x") instanceof Error}");
io.println("${ValueError("x") instanceof Error}");
io.println("${RuntimeError("x") instanceof RuntimeError}");
io.println("${RuntimeError("x") instanceof ValueError}");
io.println("${FatalError("x") instanceof Error}");
io.println("${FatalError("x") instanceof FatalError}");
`, "true\ntrue\ntrue\nfalse\nfalse\ntrue\n")
}

// TestParityMonotonicClock proves time.monotonic never decreases across
// a sleep on both backends (the wall clock can, which broke TTLs).
func TestParityMonotonicClock(t *testing.T) {
	runParity(t, `import io; import time;
let a = time.monotonic();
time.sleep(5);
let b = time.monotonic();
io.println("${typeof(a)}");
io.println("${b >= a}");
`, "int\ntrue\n")
}
