package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityTestingAssertions(t *testing.T) {
	runParity(t, `import io;
import test;

class AssertionTest extends test.Test {
    @test
    func assertions(): void {
        this.equal(2 + 2, 4);
        this.assertEquals(4, 2 + 2);
        this.assertNotEquals(5, 2 + 2);
        this.assertTrue(4 > 3);
        this.assertFalse(3 > 4);
        this.assertNull(null);
        this.assertNotNull("ok");
        this.assertContains("hello Geblang", "Geb");
        this.assertNotContains("hello Geblang", "PHP");
        this.assertContains([1, 2, 3], 2);
        this.assertContains({"name": "Ada"}, "name");
        this.assertEmpty([]);
        this.assertNotEmpty(["ok"]);
        this.assertGreaterThan(3, 4);
        this.assertGreaterThanOrEqual(4, 4);
        this.assertLessThan(5, 4);
        this.assertLessThanOrEqual(4, 4);
    }
}

let instance = AssertionTest();
instance.assertions();
io.println("ok");
`, "ok\n")
}

// Both backends reject a scalar-mismatch overload call with the identical
// detailed message (compiler now mirrors the evaluator's runtime wording).
func TestParityOverloadMismatchErrorText(t *testing.T) {
	source := `func g(int x): int { return x; }
g("s");
`
	want := "g expects int for parameter 'x', got string"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil || !strings.Contains(evErr.Error(), want) {
		t.Fatalf("evaluator: want %q, got %v", want, evErr)
	}
	_, vmErr := bytecode.Compile(program, []byte(source), "parity")
	if vmErr == nil || !strings.Contains(vmErr.Error(), want) {
		t.Fatalf("vm: want %q, got %v", want, vmErr)
	}
}

func TestParityFinallyBlock(t *testing.T) {
	runParity(t, `import io;
func withFinally(): int {
    try {
        return 7;
    } finally {
        io.println("finally");
    }
}
io.println(withFinally());
`, "finally\n7\n")
}

func TestParityFinallyRunsDuringExceptionUnwind(t *testing.T) {
	runParity(t, `import io;
func boom(): int { throw RuntimeError("kaboom"); }
int order = 0;
try {
    try {
        boom();
    } finally {
        order = 1;
    }
} catch (RuntimeError e) {
    order = order + 10;
}
io.println(order);
`, "11\n")
}

func TestParityTryBodyMutationPersistsInCatch(t *testing.T) {
	runParity(t, `import io;
func boom(): int { throw RuntimeError("e"); }
int x = 0;
try {
    x = 5;
    boom();
} catch (RuntimeError e) {
    io.println(x);
}
`, "5\n")
}

// A closure's mutation of a captured outer variable persists after the exception it throws is caught (the drain-on-body-error shape).
func TestParityClosureMutationPersistsAcrossCaughtThrow(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    any captured = null;
    func body(): void {
        captured = "set";
        throw RuntimeError("boom");
    }
    try { body(); } catch (RuntimeError e) {}
    io.println("${captured}");
}
run();
`, "set\n")
}

func TestParityDefer(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    defer io.println("deferred");
    io.println("body");
}
run();
`, "body\ndeferred\n")
}

func TestParityDeferMultiple(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    defer io.println("first");
    defer io.println("second");
    defer io.println("third");
    io.println("body");
}
run();
`, "body\nthird\nsecond\nfirst\n")
}

func TestParityDeferOnReturn(t *testing.T) {
	runParity(t, `import io;
func run(bool early): void {
    defer io.println("deferred");
    if (early) {
        io.println("early");
        return;
    }
    io.println("normal");
}
run(true);
run(false);
`, "early\ndeferred\nnormal\ndeferred\n")
}

// A deferred call to a nested function that shares its parent's frame (closes over an outer local) resolves that local when it runs during the parent's return.
func TestParityDeferSharedFrameClosure(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    list<string> order = [];
    func record(string label): void { order = order.push(label); }
    func wrapper(): void {
        defer record("a");
        defer record("b");
        record("body");
    }
    wrapper();
    io.println("${order}");
}
run();
`, "[\"body\", \"b\", \"a\"]\n")
}

func TestParityDeferUserFunc(t *testing.T) {
	runParity(t, `import io;
func cleanup(): void {
    io.println("cleanup");
}
func run(): void {
    defer cleanup();
    io.println("body");
}
run();
`, "body\ncleanup\n")
}

func TestParityDeferUserFuncWithArgs(t *testing.T) {
	runParity(t, `import io;
func log(string msg): void {
    io.println(msg);
}
func run(): void {
    defer log("done");
    io.println("running");
}
run();
`, "running\ndone\n")
}

func TestParityDeferUserFuncLIFO(t *testing.T) {
	runParity(t, `import io;
func log(string msg): void {
    io.println(msg);
}
func run(): void {
    defer log("first");
    defer log("second");
    defer log("third");
    io.println("body");
}
run();
`, "body\nthird\nsecond\nfirst\n")
}

func TestParityDeferUserFuncOnReturn(t *testing.T) {
	runParity(t, `import io;
func cleanup(): void {
    io.println("cleanup");
}
func run(bool early): void {
    defer cleanup();
    if (early) {
        io.println("early");
        return;
    }
    io.println("normal");
}
run(true);
run(false);
`, "early\ncleanup\nnormal\ncleanup\n")
}

// A deferred method call on a primitive receiver (list/dict/set), not just a class instance.
func TestParityDeferPrimitiveMethodCall(t *testing.T) {
	runParity(t, `import io;
func h(): void {
    list<string> out = [];
    defer out.push("deferred");
    out.push("done");
    io.println("${out}");
}
h();
`, "[\"done\"]\n")
}

func TestParityDeferMethodCall(t *testing.T) {
	runParity(t, `import io;
class Resource {
    string name;
    func Resource(string name) {
        this.name = name;
    }
    func close(): void {
        io.println("closing " + this.name);
    }
}
func run(): void {
    Resource r = Resource("db");
    defer r.close();
    io.println("working");
}
run();
`, "working\nclosing db\n")
}

func TestParityDeferMethodCallWithArgs(t *testing.T) {
	runParity(t, `import io;
class Logger {
    func Logger() {}
    func log(string msg): void {
        io.println(msg);
    }
}
func run(): void {
    Logger l = Logger();
    defer l.log("done");
    io.println("start");
}
run();
`, "start\ndone\n")
}

func TestParityDeferMethodCallOnReturn(t *testing.T) {
	runParity(t, `import io;
class Resource {
    string name;
    func Resource(string name) {
        this.name = name;
    }
    func close(): void {
        io.println("closed " + this.name);
    }
}
func run(bool early): void {
    Resource r = Resource("conn");
    defer r.close();
    if (early) {
        io.println("early exit");
        return;
    }
    io.println("normal exit");
}
run(true);
run(false);
`, "early exit\nclosed conn\nnormal exit\nclosed conn\n")
}

func TestParityDeferCallableVar(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    let cleanup = func(): void { io.println("cleaned up"); };
    defer cleanup();
    io.println("running");
}
run();
`, "running\ncleaned up\n")
}

func TestParityDeferCallableVarWithArgs(t *testing.T) {
	runParity(t, `import io;
func run(): void {
    let log = func(string msg): void { io.println(msg); };
    defer log("done");
    io.println("working");
}
run();
`, "working\ndone\n")
}

func TestParityLiteralDivModByZeroThrowsCatchably(t *testing.T) {
	runParity(t, `import io;
try { io.println(1 % 0); } catch (Error e) { io.println("mod: ${e.message}"); }
try { io.println(1 // 0); } catch (Error e) { io.println("idiv: ${e.message}"); }
int z = 0;
try { io.println(7 % z); } catch (Error e) { io.println("modv: ${e.message}"); }
int w = 9;
try { w %= z; } catch (Error e) { io.println("modc: ${e.message}"); }
`, "mod: modulo by zero\nidiv: integer division by zero\nmodv: modulo by zero\nmodc: modulo by zero\n")
}

func TestParityRuntimeErrorMessages(t *testing.T) {
	runParity(t, `import io;
func show(string label, func body): void {
    try { body(); } catch (Error e) { io.println("${label}: ${e.message}"); }
}
show("m-set", func(): void { {1, 2}.bogus(); });
show("m-list", func(): void { [1].bogus(); });
show("m-dict", func(): void { {"a": 1}.bogus(); });
show("m-str", func(): void { "x".bogus(); });
show("m-int", func(): void { (1).bogus(); });
show("op-strmod", func(): void { let r = "x" % [1]; io.println("${r}"); });
show("op-strminus", func(): void { let r = "x" - 1; io.println("${r}"); });
show("op-listplus", func(): void { let r = [1] + 2; io.println("${r}"); });
`, `m-set: unknown method set.bogus
m-list: unknown method list.bogus
m-dict: unknown method dict.bogus
m-str: unknown method string.bogus
m-int: unknown method int.bogus
op-strmod: unsupported operands for %: string and list
op-strminus: unsupported operands for -: string and int
op-listplus: unsupported operands for +: list and int
`)
}

func TestParityMatchErrorIncludesValue(t *testing.T) {
	runParity(t, `import io;
try {
    match (42) {
        case 1: io.println("one");
        case 2: io.println("two");
    };
} catch (MatchError e) {
    io.println(e.message.contains("42"));
    io.println(e.message.contains("int"));
    io.println(e.message.contains("default"));
}
`, "true\ntrue\ntrue\n")
}

func TestParityMatchErrorWithDefault(t *testing.T) {
	runParity(t, `import io;
let x = match (7) {
    case 1 => "one";
    case 7 => "seven";
    default => "other";
};
io.println(x);
`, "seven\n")
}

func TestParityMatchGuardedCaseError(t *testing.T) {
	runParity(t, `import io;
try {
    match ("hello") {
        case string s if (s.length() > 10): io.println("long");
    };
} catch (MatchError e) {
    io.println(e.message.contains("hello"));
    io.println(e.message.contains("string"));
    io.println(e.message.contains("default"));
}
`, "true\ntrue\ntrue\n")
}

func TestParityStackTraceUncaughtError(t *testing.T) {
	runErrorParity(t, `
func inner() {
    throw RuntimeError("boom");
}
func outer() {
    inner();
}
outer();
`,
		"RuntimeError: boom",
		"at inner",
		"at outer",
		"at <top level>",
	)
}

func TestParityStackTraceCaughtErrorHasNoTrace(t *testing.T) {
	runParity(t, `import io;
func inner() {
    throw RuntimeError("oops");
}
try {
    inner();
} catch (RuntimeError e) {
    io.println(e.message);
}
`, "oops\n")
}

func TestParityStructuredErrorStackTrace(t *testing.T) {
	runParity(t, `import errors;
import io;

func inner() {
    throw RuntimeError("boom");
}

func outer() {
    inner();
}

try {
    outer();
} catch (RuntimeError e) {
    let trace = e.stackTrace();
    let first = trace.first();
    let frames = errors.frames(e);
    io.println(typeof(trace));
    io.println(trace.length() > 0);
    io.println(first.function());
    io.println(first.line() > 0);
    io.println(frames[0]["function"]);
    io.println(errors.hasStackTrace(e));
}
`, "errors.StackTrace\ntrue\ninner\ntrue\ninner\ntrue\n")
}

func TestParityCollectionsTopologicalSortCycleError(t *testing.T) {
	runErrorParity(t, `import collections;
let g = {"a": ["b"], "b": ["c"], "c": ["a"]};
collections.topologicalSort(g);
`, "cycle detected")
}

func TestParityUserErrorBasic(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
try {
    throw AppError("something failed");
} catch (AppError e) {
    io.println(e.message);
}
`, "something failed\n")
}

func TestParityUserErrorCatchParent(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
try {
    throw AppError("caught by parent");
} catch (Error e) {
    io.println(e.message);
}
`, "caught by parent\n")
}

func TestParityUserErrorCatchAll(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
try {
    throw AppError("caught by catch-all");
} catch (Error e) {
    io.println(e.message);
}
`, "caught by catch-all\n")
}

func TestParityUserErrorChain(t *testing.T) {
	runParity(t, `import io;
class NetworkError extends RuntimeError {}
try {
    throw NetworkError("timeout");
} catch (Error e) {
    io.println(e.class + ": " + e.message);
}
`, "NetworkError: timeout\n")
}

func TestParityUserErrorMultiCatch(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
class NetworkError extends RuntimeError {}
try {
    throw NetworkError("timeout");
} catch (AppError e) {
    io.println("app: " + e.message);
} catch (NetworkError e) {
    io.println("network: " + e.message);
} catch (Error e) {
    io.println("generic: " + e.message);
}
`, "network: timeout\n")
}

func TestParityUserErrorFinally(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
try {
    throw AppError("oops");
} catch (AppError e) {
    io.println("caught: " + e.message);
} finally {
    io.println("finally");
}
`, "caught: oops\nfinally\n")
}

func TestParityUserErrorNoMessage(t *testing.T) {
	runParity(t, `import io;
class AppError extends Error {}
try {
    throw AppError();
} catch (AppError e) {
    io.println(e.class);
}
`, "AppError\n")
}

func TestParityErrorsIsBuiltin(t *testing.T) {
	runParity(t, `import io;
import errors;
try {
    throw ValueError("oops");
} catch (Error e) {
    io.println(errors.is(e, "ValueError") as string);
    io.println(errors.is(e, "Error") as string);
    io.println(errors.is(e, "IOError") as string);
}
`, "true\ntrue\nfalse\n")
}

func TestParityErrorsIsUserHierarchy(t *testing.T) {
	runParity(t, `import io;
import errors;
class AppError extends RuntimeError {}
class NotFoundError extends AppError {}
try {
    throw NotFoundError("not found");
} catch (Error e) {
    io.println(errors.is(e, "NotFoundError") as string);
    io.println(errors.is(e, "AppError") as string);
    io.println(errors.is(e, "RuntimeError") as string);
    io.println(errors.is(e, "Error") as string);
    io.println(errors.is(e, "IOError") as string);
}
`, "true\ntrue\ntrue\ntrue\nfalse\n")
}

func TestParityErrorCustomFields(t *testing.T) {
	runParity(t, `import io;
class HttpError extends RuntimeError {
    int code;
    func HttpError(int code, string msg) {
        parent(msg);
        this.code = code;
    }
}
try {
    throw HttpError(404, "not found");
} catch (Error e) {
    io.println(e.message);
    io.println(e.code as string);
}
`, "not found\n404\n")
}

func TestParityErrorCustomFieldsIs(t *testing.T) {
	runParity(t, `import io;
import errors;
class ApiError extends IOError {
    int status;
    func ApiError(int status, string msg) {
        parent(msg);
        this.status = status;
    }
}
try {
    throw ApiError(500, "server error");
} catch (Error e) {
    io.println(errors.is(e, "ApiError") as string);
    io.println(errors.is(e, "IOError") as string);
    io.println(errors.is(e, "Error") as string);
    io.println(e.status as string);
}
`, "true\ntrue\ntrue\n500\n")
}

// TestParityHTTPServerOnError verifies the onError server callback captures
// connection-level failures (here an mTLS handshake with no client cert) on
// both backends, and that a non-function onError is rejected. The callback
// fires on a background connection, so a buffered channel + blocking recv()
// keeps the test deterministic; runParityWithStdlib wires the module loader
// (for async.channel) and the evaluator->VM callback dispatcher.
func TestParityHTTPServerOnError(t *testing.T) {
	runParityWithStdlib(t, `
import http;
import io;
import crypt;
import async.channel as chan;
try {
    http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> { return {"status": 200, "body": ""}; }, {"onError": 5});
    io.println("accepted");
} catch (Error e) { io.println("rejected-bad-onError"); }
let caKey = crypt.generateEcKey("P-256");
let caBundle = crypt.generateSelfSignedCert({"subject": {"commonName": "GeblangCA"}, "key": caKey});
let errs = chan.Channel<string>(16);
let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"ok": true});
}, {"tls": {"selfSigned": true, "clientCa": caBundle["cert"], "clientAuth": "require"},
   "onError": func(string msg): void { errs.send("captured"); }});
let url = "https://" + http.serverAddr(server) + "/";
try { http.newClient({"tls": {"verify": false}}).get(url); } catch (Error e) {}
io.println(errs.recv());
http.close(server);
`, "rejected-bad-onError\ncaptured\n")
}

// Regression: the VM's foreign-class method dispatch was wrapping
// the native trampoline's error via runtimeError, which stripped the
// vmThrownError chain and prevented a try/catch in the calling module
// from catching exceptions thrown inside a stdlib class method.
func TestParityCatchAcrossStdlibBoundary(t *testing.T) {
	runParityWithStdlib(t, `import io;
import option;
let o = option.Option(false, 0);
try {
    o.unwrap();
} catch (ValueError e) {
    io.println("caught: " + e.message);
}
io.println("done");
`, "caught: Option.unwrap called on None\ndone\n")
}

func TestParityAssert(t *testing.T) {
	// Truthy assert is a no-op; falsy assert throws AssertionError.
	runParity(t, `import io;
assert(1 + 1 == 2);
io.println("ok");
try {
    assert(1 == 2);
} catch (AssertionError e) {
    io.println("caught: " + e.message);
}
`, "ok\ncaught: assertion failed: (1 == 2)\n")

	// Explicit message overrides the default source-text rendering.
	runParity(t, `import io;
try {
    assert(false, "custom message");
} catch (AssertionError e) {
    io.println(e.message);
}
`, "custom message\n")

	// AssertionError is a subclass of Error.
	runParity(t, `import io;
try {
    assert(false, "boom");
} catch (Error e) {
    io.println(e.class);
}
`, "AssertionError\n")
}

func TestParityUnknownMethodThrowsCatchableRuntimeError(t *testing.T) {
	runParity(t, `import io;

class Plain {
    func Plain() {}
    func known(): string { return "ok"; }
}

let p = Plain();
io.println(p.known());

try {
    p.notThere("argument");
    io.println("FAIL: no throw");
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}

io.println("after try");
`, "ok\ncaught: unknown method Plain.notThere\nafter try\n")
}

func TestParityUnknownMethodOnForeignInstanceIsCatchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "remote.gb"), []byte(`module remote;
export class Remote {
    func Remote() {}
    func known(): string { return "remote-ok"; }
}
`), 0o644); err != nil {
		t.Fatalf("write remote: %v", err)
	}

	source := `import io;
import remote;

let r = remote.Remote();
io.println(r.known());

try {
    r.missing("x");
    io.println("FAIL: no throw");
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}

io.println("after try");
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	want := "remote-ok\ncaught: unknown method Remote.missing\nafter try\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// TestParityCrossModuleInheritedThrow covers the fix where a method
// inherited from a parent class in another module threw an error
// that was silently swallowed by the bytecode cross-module dispatch
// fallback - the loader's error was treated as "method not found"
// rather than as a real throw, so try/catch never saw it.
func TestParityCrossModuleInheritedThrow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probemod.gb"), []byte(`module probemod;
export class Base {
    func Base() {}
    func ok(): int { return 42; }
    func boom(): void { throw RuntimeError("bang"); }
}
`), 0o644); err != nil {
		t.Fatalf("write probemod: %v", err)
	}

	source := `import io;
import probemod;

class Sub extends probemod.Base {
    func go(): void {
        io.println(this.ok());
        try {
            this.boom();
            io.println("after-try");
        } catch (Error e) {
            io.println("caught: " + e.message);
        }
        io.println("end");
    }
}
Sub().go();
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "42\ncaught: bang\nend\n"

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// TestParityUserErrorParentChain verifies that user-defined error
// subclasses (BadRequestError extends HttpException extends
// RuntimeError) walk the full parent chain under instanceof and catch
// on both backends - the case that was diverging before 1.0.2.
func TestParityUserErrorParentChain(t *testing.T) {
	runParity(t, `import io;

class HttpException extends RuntimeError {
    int status;
    string detail;
    func HttpException(int s, string d) {
        parent("HTTP " + (s as string));
        this.status = s;
        this.detail = d;
    }
}

class BadRequestError extends HttpException {
    func BadRequestError(string d) {
        parent(400, d);
    }
}

let e = BadRequestError("missing");
io.println(e instanceof BadRequestError);
io.println(e instanceof HttpException);
io.println(e instanceof RuntimeError);

let caught = "";
try {
    throw BadRequestError("nope");
} catch (HttpException err) {
    caught = "http:" + err.detail;
}
io.println(caught);
`, "true\ntrue\ntrue\nhttp:nope\n")
}

// TestParityDeferNamedArgs pins the 1.0.6 fix that lifted the
// "named arguments in defer" rejection for instance method and
// callable-variable defers. Both backends must order the deferred
// arguments by name (not source order) when running the queue.
func TestParityDeferNamedArgs(t *testing.T) {
	runParity(t, `import io;

class Logger {
    string prefix;
    func Logger(string prefix) { this.prefix = prefix; }
    func log(string head, string tail): void {
        io.println(this.prefix + ":" + head + "-" + tail);
    }
}

func run(): void {
    let logger = Logger("M");
    defer logger.log(tail: "end", head: "start");
    let cb = func(string left, string right): void {
        io.println("cb:" + left + "+" + right);
    };
    defer cb(right: "R", left: "L");
    io.println("before");
}

run();
`, "before\ncb:L+R\nM:start-end\n")
}

// TestParityErrorGetMessageAndGetClass guards the Java/PHP-style
// accessor methods on built-in error values. Pre-1.0.2 only the
// `.message` field was exposed - calls like `e.getMessage()`
// (the convention everywhere else) errored with "X has no method
// getMessage". Both names now resolve consistently on eval + VM.
func TestParityErrorGetMessageAndGetClass(t *testing.T) {
	runParity(t, `import io;

try {
    throw RuntimeError("boom");
} catch (RuntimeError e) {
    io.println(e.message);
    io.println(e.getMessage());
    io.println(e.getClass());
}
`, "boom\nboom\nRuntimeError\n")
}

// TestParityCastErrorIsCatchable guards that a failed `x as Y`
// raises a catchable RuntimeError on both backends instead of
// escaping as an uncatchable "bytecode runtime error" (VM
// divergence pre-1.0.2: the VM emitted vm.runtimeError directly,
// so a surrounding try/catch never saw the failure). Uses
// list->int which has no defined cast.
func TestParityCastErrorIsCatchable(t *testing.T) {
	runParity(t, `import io;

try {
    let n = [1, 2, 3] as int;
    io.println("unreached");
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}
io.println("after");
`, "caught: cannot cast list to int\nafter\n")
}

// TestParityCrossModuleThrowCatch guards a VM-only regression where a
// throw originating in a sub-module (or in a callback dispatched from a
// sub-module) collapsed to "uncaught RuntimeError" at the VM boundary,
// losing the original class + parent chain. The fix wraps the
// underlying runtime.Error in a vmThrownError so the calling VM can
// recover it via errors.As and re-throw it as a typed pendingThrow.
// Surfaced building the Gebweb adapter: stdlib catch (errors.HttpException)
// failed to match user-script throws.
func TestParityCrossModuleThrowCatch(t *testing.T) {
	runParity(t, `import io;

class HttpException extends RuntimeError {
    int status;
    func HttpException(int s, string m) { parent(m); this.status = s; }
}
class NotFoundError extends HttpException {
    func NotFoundError(string m) { parent(404, m); }
}

func wrap(callable fn): string {
    try {
        fn();
        return "no throw";
    } catch (HttpException e) {
        return "caught " + (e.status as string) + ": " + e.message;
    }
}

let userFn = func(): void {
    throw NotFoundError("missing widget");
};

io.println(wrap(userFn));
`, "caught 404: missing widget\n")
}

// TestParityUnknownNativeClassMethodError guards that an unresolved method on a
// native-module class instance (Response) raises a catchable "unknown method"
// error on both backends, not the VM's old "module http is not loaded".
func TestParityUnknownNativeClassMethodError(t *testing.T) {
	runParityWithStdlib(t, `
import http;
import io;
let r = http.response("x", 200);
try {
    r.nope();
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "unknown method Response.nope\n")
}
