package bytecode_test

import (
	"testing"
)

const builtinErrorDonor = `module builtin_error_donor;
export class AppError extends RuntimeError { func AppError(string m) { parent(m); } }
`

const builtinErrorMain = `import io;
import builtin_error_donor as errs;
class LocalError extends ValueError { func LocalError(string m) { parent(m); } }
class Plain extends Error { func Plain(string m) { parent(m); } }
func raw(any v): any { return v; }
func asError(Error e): string { return "Error ok"; }
func asIO(IOError e): string { return "IOError ok"; }
func asRuntime(RuntimeError e): string { return "RuntimeError ok"; }
func capture(callable f): any { try { f(); } catch (Error e) { return e; } return null; }
func run(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (TypeError e) { io.println("${label}: rejected: ${e.message}"); }
}
let rt = capture(func(): void { throw RuntimeError("r"); });
let tm = capture(func(): void { throw TimeoutError("t"); });
let le = capture(func(): void { throw LocalError("l"); });
let pe = capture(func(): void { throw Plain("p"); });
let ae = capture(func(): void { throw errs.AppError("a"); });
run("runtimeAsError", func(): any { return asError(raw(rt)); });
run("timeoutAsIO", func(): any { return asIO(raw(tm)); });
run("timeoutAsError", func(): any { return asError(raw(tm)); });
run("localAsError", func(): any { return asError(raw(le)); });
run("localAsRuntime", func(): any { return asRuntime(raw(le)); });
run("plainAsRuntime", func(): any { return asRuntime(raw(pe)); });
run("crossAsError", func(): any { return asError(raw(ae)); });
run("crossAsRuntime", func(): any { return asRuntime(raw(ae)); });
run("varDecl", func(): any { ?Error last = null; last = raw(ae); return "assigned"; });
run("instanceofError", func(): any { return rt instanceof Error; });
run("instanceofIO", func(): any { return tm instanceof IOError; });
run("instanceofWrong", func(): any { return rt instanceof IOError; });
`

const builtinErrorWant = `runtimeAsError: Error ok
timeoutAsIO: IOError ok
timeoutAsError: Error ok
localAsError: Error ok
localAsRuntime: rejected: asRuntime expects RuntimeError for parameter 'e', got LocalError
plainAsRuntime: rejected: asRuntime expects RuntimeError for parameter 'e', got Plain
crossAsError: Error ok
crossAsRuntime: RuntimeError ok
varDecl: assigned
instanceofError: true
instanceofIO: true
instanceofWrong: false
`

func TestParityBuiltinErrorTypeMatching(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"builtin_error_donor.gb": builtinErrorDonor}, builtinErrorMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != builtinErrorWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
