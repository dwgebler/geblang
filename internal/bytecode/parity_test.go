package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// TestParityContainerInspectIsJSONLike pins the Inspect output for
// dicts, lists, and sets across both backends. Strings inside a
// container are JSON-quoted; top-level strings stay unquoted to
// match the existing io.println contract. Dict entries appear in
// TestParityVecmathNormalizeSearch pins vecmath.normalize + semanticSearch output on both backends.
func TestParityVecmathNormalizeSearch(t *testing.T) {
	runParity(t, `import vecmath;
import io;
io.println("${vecmath.normalize([3.0, 4.0])}");
let r = vecmath.semanticSearch([[1.0, 0.0], [0.0, 1.0]], [[1.0, 0.0], [0.0, 1.0], [0.7, 0.7]], 1);
io.println("${r[0][0]["index"]} ${r[1][0]["index"]}");
`, `[0.6, 0.8]
0 1
`)
}

// TestParityTransformersTokenize pins transformers.tokenize WordPiece output on both backends.
func TestParityTransformersTokenize(t *testing.T) {
	runParity(t, `import transformers;
import io;
let tjson = "{\"model\":{\"unk_token\":\"[UNK]\",\"continuing_subword_prefix\":\"##\",\"vocab\":{\"[PAD]\":0,\"[UNK]\":1,\"[CLS]\":2,\"[SEP]\":3,\"hi\":4}}}";
let r = transformers.tokenize(tjson, ["hi", "hi hi"]);
io.println("${(r["input_ids"] as list<any>)[0]}");
io.println("${(r["input_ids"] as list<any>)[1]}");
`, `[2, 4, 3, 0]
[2, 4, 4, 3]
`)
}

// TestParityTransformersMaxLength pins that an int option value (a VM SmallInt) reaches the native option reader so truncation applies on both backends.
func TestParityTransformersMaxLength(t *testing.T) {
	runParity(t, `import transformers;
import io;
let tjson = "{\"model\":{\"unk_token\":\"[UNK]\",\"continuing_subword_prefix\":\"##\",\"vocab\":{\"[PAD]\":0,\"[UNK]\":1,\"[CLS]\":2,\"[SEP]\":3,\"hello\":4,\"world\":5,\"play\":6}}}";
let r = transformers.tokenize(tjson, ["hello world play"], {"maxLength": 4});
io.println("${(r["input_ids"] as list<any>)[0]}");
`, "[2, 4, 5, 3]\n")
}

// TestParityNativeModuleQualifiedCast pins that a native value (unqualified TypeName) casts to a module-qualified target on both backends.
func TestParityNativeModuleQualifiedCast(t *testing.T) {
	runParityWithStdlib(t, `import streams;
import io;
let m = streams.memory("hi");
let s = m as streams.IOStream;
io.println(typeof(s));
`, "IOStream\n")
}

// TestParityBrowserGate pins the --allow-browser gate (PermissionError when absent) on both backends.
func TestParityBrowserGate(t *testing.T) {
	runParityStateful(t, `import browser;
import io;
try { browser.launch(); io.println("opened"); }
catch (PermissionError e) { io.println("denied"); }
`, "denied\n")
}

// TestParityNetworkErrorClasses pins TimeoutError/TlsError catching + the IOError subclass hierarchy on both backends.
func TestParityNetworkErrorClasses(t *testing.T) {
	runParity(t, `import errors;
import io;
func exact(string cls): string {
    try { throw errors.new(cls, "x"); }
    catch (TimeoutError e) { return "Timeout"; }
    catch (TlsError e) { return "Tls"; }
    catch (IOError e) { return "IO"; }
    catch (Error e) { return "Error"; }
}
func asIO(string cls): string {
    try { throw errors.new(cls, "x"); }
    catch (IOError e) { return "IO"; }
    catch (Error e) { return "Error"; }
}
io.println(exact("TimeoutError") + " " + exact("TlsError") + " " + exact("IOError"));
io.println(asIO("TimeoutError") + " " + asIO("TlsError"));
`, "Timeout Tls IO\nIO IO\n")
}

// TestParityTransformersPool pins transformers.pool mask-weighted mean pooling on both backends.
func TestParityTransformersPool(t *testing.T) {
	runParity(t, `import transformers;
import ndarray;
import io;
let hidden = ndarray.array([[[1.0, 2.0], [3.0, 4.0]]]);
let mask = ndarray.array([[1, 1]]);
io.println("${transformers.pool(hidden, mask, {"normalize": false}).toList()}");
`, "[[2, 3]]\n")
}

// TestParitySearch pins search/searchPattern across list/dict/string and value/predicate/regex on both backends.
func TestParitySearch(t *testing.T) {
	runParity(t, `import io;
let xs = ["a", "b", "a", "c"];
io.println("${xs.search("a")}");
let pr = xs.search(func(string s): bool { return s == "c"; });
io.println("${pr}");
io.println("${["foo", "bar", "baz"].searchPattern("^ba")}");
let d = {"x": 1, "y": 2, "z": 1};
io.println("${d.search(1)}");
let words = {"a": "cat", "b": "dog", "c": "cow"};
io.println("${words.searchPattern("^c")}");
io.println("${"hello".search("l")}");
io.println("${"a1b2c3".searchPattern("[0-9]")}");
`, `[0, 2]
[3]
[1, 2]
["x", "z"]
["a", "c"]
[2, 3]
[1, 3, 5]
`)
}

func TestParityPrimitiveMethodNamesAreCaseSensitive(t *testing.T) {
	runParity(t, `import io;

func wrongLength(): string {
    any value = "abc";
    try {
        value.Length();
        return "accepted";
    } catch (Error e) {
        return "rejected";
    }
}

func wrongIsEmpty(): string {
    any value = [];
    try {
        value.isempty();
        return "accepted";
    } catch (Error e) {
        return "rejected";
    }
}

func wrongConversion(): string {
    any value = "42";
    try {
        value.TOINT();
        return "accepted";
    } catch (Error e) {
        return "rejected";
    }
}

io.println(wrongLength());
io.println(wrongIsEmpty());
io.println(wrongConversion());
io.println(("42" as any).toInt());
`, "rejected\nrejected\nrejected\n42\n")
}

// TestParityListFill pins list.fill (append count copies, return receiver) on both backends.
func TestParityListFill(t *testing.T) {
	runParity(t, `import io;
let a = [];
io.println("${a.fill('x', 5)}");
let b = ['a', 'b', 'c'];
io.println("${b.fill('x', 2)}");
io.println("${[1, 2].fill(9, 3).length()}");
io.println("${[1].fill(0, 0)}");
`, `["x", "x", "x", "x", "x"]
["a", "b", "c", "x", "x"]
5
[1]
`)
}

// TestParitySqliteTuningOptions pins identical WAL/foreign-keys/optimize output on both backends.
func TestParitySqliteTuningOptions(t *testing.T) {
	runParityStatefulWithFile(t, `import db;
import io;
let c = db.Connection({"driver": "sqlite", "dsn": "TMPFILE", "wal": true, "foreignKeys": true});
io.println(c.query("PRAGMA journal_mode;").all()[0]["journal_mode"]);
io.println(c.query("PRAGMA foreign_keys;").all()[0]["foreign_keys"]);
c.optimize();
io.println("optimized");
c.close();
`, "", `wal
1
optimized
`)
}

// TestParityCastThenCompare pins that `as int` binds tighter than `<` (not a generic) on both backends.
func TestParityCastThenCompare(t *testing.T) {
	runParity(t, `import io;
let y = 8;
io.println(7.9 as int < y);
io.println(10 as int < y);
io.println(([1, 2] as list<int>).length());
`, `true
false
2
`)
}

// TestParityTrueDivisionTyping pins identical `/` (decimal) vs `//` (int) output on both backends.
func TestParityTrueDivisionTyping(t *testing.T) {
	runParity(t, `import io;
let a = 7;
let b = 2;
decimal q = a / b;
int n = a // b;
io.println(q as string);
io.println(n as string);
io.println((50000 / 1000) as string);
io.println((50000 // 1000) as string);
io.println((6 / 2.0f) as string);
`, `3.5000000000
3
50.0000000000
50
3
`)
}

// insertion order (since 1.5.1).
func TestParityContainerInspectIsJSONLike(t *testing.T) {
	runParity(t, `import io;
io.println({"name": "Ada", "age": 36});
io.println(["a", "b", 1, true, null]);
io.println({"nested": {"a": [1, 2], "b": "x"}});
io.println([{"zeta": 1, "alpha": 2}]);
io.println({"outer": {"zeta": 1, "alpha": 2}});
io.println("plain");
io.println(42);
io.println(["with \"quote\""]);
`, `{"name": "Ada", "age": 36}
["a", "b", 1, true, null]
{"nested": {"a": [1, 2], "b": "x"}}
[{"zeta": 1, "alpha": 2}]
{"outer": {"zeta": 1, "alpha": 2}}
plain
42
["with \"quote\""]
`)
}

func TestParityDirBuiltin(t *testing.T) {
	// dir(value) returns the sorted method-name list for a value; both
	// backends must produce identical output AND it must match the
	// authoritative registry (expected is derived from it, so a wrong
	// list can't be silently re-encoded - the original dir-phantom bug).
	cases := []struct{ literal, typeName string }{
		{`[1, 2, 3]`, "list"},
		{`{"a": 1}`, "dict"},
		{`[1, 2] as set`, "set"},
		{`42`, "int"},
		{`3.5`, "decimal"},
		{`"x"`, "string"},
	}
	src := "import io;\n"
	want := ""
	for _, c := range cases {
		src += `io.println("${dir(` + c.literal + `)}");` + "\n"
		want += formatPrimitiveMethodList(c.typeName) + "\n"
	}
	runParity(t, src, want)
}

func TestParityDumpBuiltin(t *testing.T) {
	// dump(value) renders a type-annotated debug string; identical on
	// both backends (was evaluator-only before R1).
	runParity(t, `import io;
io.println(dump(42));
io.println(dump("hi"));
io.println(dump([1, 2]));
io.println(dump({"a": 1}));
io.println(dump(true));
`, "int(42)\nstring(\"hi\")\nlist[int(1), int(2)]\ndict{string(\"a\"): int(1)}\nbool(true)\n")
}

func TestParityProfilerModule(t *testing.T) {
	// profiler is a dual-name module (source stdlib + native fallback).
	// Values are non-deterministic, so assert result shape only.
	runParityWithStdlib(t, `import io;
import profiler;
io.println(typeof(profiler.snapshot()));
io.println(typeof(profiler.memory()));
io.println(typeof(profiler.cpu()));
io.println(typeof(profiler.peak()));
io.println(typeof(profiler.delta(profiler.snapshot())));
`, "dict\ndict\ndict\ndict\ndict\n")
}

func TestParityProfilerTimerContextManager(t *testing.T) {
	runParityWithStdlib(t, `import profiler;
import io;
let t = profiler.timer();
with (t) {
    let x = 0;
    for (i in range(0, 300000)) { x = x + i; }
}
io.println(t.elapsedNs() > 0);
io.println(t.elapsedMs() > 0.0f);
io.println(typeof(t.elapsedNs()) == int);
`, "true\ntrue\ntrue\n")
}

func TestParityProfilerProfileContextManager(t *testing.T) {
	runParityWithStdlib(t, `import profiler;
import io;
let p = profiler.profile();
with (p) {
    let xs = [];
    for (i in range(0, 30000)) { xs = xs.push(i); }
}
io.println(p.report().hasKey("elapsed_ms"));
io.println(p.report().hasKey("cpu_ms"));
io.println(p.report().hasKey("heap_alloc"));
io.println(p.elapsedMs() > 0.0f);
`, "true\ntrue\ntrue\ntrue\n")
}

// TestParityMetricsProfileDurationMs pins metrics.duration / profile.elapsed to a float millisecond scale on both backends (9.38). A wide bound avoids timing flakiness while still catching a nanosecond regression.
func TestParityMetricsProfileDurationMs(t *testing.T) {
	runParityStateful(t, `import metrics;
import profile;
import time;
import io;
let m0 = metrics.now();
time.sleep(30);
let md = metrics.duration(m0);
io.println(typeof(md) == float);
io.println(md >= 10.0f && md <= 5000.0f);
let p0 = profile.now();
time.sleep(30);
let pe = profile.elapsed(p0);
io.println(typeof(pe) == float);
io.println(pe >= 10.0f && pe <= 5000.0f);
`, "true\ntrue\ntrue\ntrue\n")
}

func TestParityFunctions(t *testing.T) {
	runParity(t, `import io;
func add(int a, int b): int {
    return a + b;
}
func greet(string name, string prefix = "Hello"): string {
    return prefix + ", " + name + "!";
}
io.println(add(3, 4));
io.println(greet("World"));
io.println(greet("Alice", "Hi"));
`, "7\nHello, World!\nHi, Alice!\n")
}

func TestParityRecursion(t *testing.T) {
	runParity(t, `import io;
func fact(int n): int {
    if (n <= 1) { return 1; }
    return n * fact(n - 1);
}
io.println(fact(6));
`, "720\n")
}

func TestParityFunctionOverloads(t *testing.T) {
	runParity(t, `import io;
func describe(int x): string {
    return "int:" + (x as string);
}
func describe(string x): string {
    return "str:" + x;
}
io.println(describe(42));
io.println(describe("hello"));
`, "int:42\nstr:hello\n")
}

func TestParityOverloadNestedExpectedType(t *testing.T) {
	runParity(t, `import io;
func wrap(string s): string {
    return "[" + s + "]";
}
func describe(int x): string {
    return "int:" + (x as string);
}
func describe(string x): string {
    return "str:" + x;
}
func printWrapped(string s): void {
    io.println(s);
}
string r = describe(42);
io.println(r);
printWrapped(describe("hello"));
io.println(wrap(describe(99)));
`, "int:42\nstr:hello\n[int:99]\n")
}

func TestParityClosureNoCapture(t *testing.T) {
	runParity(t, `import io;
let double = func(int x): int { return x * 2; };
io.println(double(5));
io.println(double(21));
`, "10\n42\n")
}

func TestParityClosureCapture(t *testing.T) {
	runParity(t, `import io;
func makeAdder(int n): any {
    return func(int x): int { return x + n; };
}
let add10 = makeAdder(10);
io.println(add10(5));
io.println(add10(32));
`, "15\n42\n")
}

func TestParityClosureMultipleCaptures(t *testing.T) {
	runParity(t, `import io;
func makeLinear(int a, int b): any {
    return func(int x): int { return a * x + b; };
}
let f = makeLinear(3, 1);
io.println(f(0));
io.println(f(4));
`, "1\n13\n")
}

func TestParityClosureMutableCapture(t *testing.T) {
	runParity(t, `import io;
func makeCounter(): any {
    int n = 0;
    return func(): int {
        n++;
        return n;
    };
}
let counter = makeCounter();
io.println(counter());
io.println(counter());
		`, "1\n2\n")
}

func TestParityCallFunctionLiteralDirectly(t *testing.T) {
	runParity(t, `import io;
io.println((func(int value): int {
    return value * 2;
})(21));
	`, "42\n")
}

func TestParityCallReturnedCallableDirectly(t *testing.T) {
	runParity(t, `import io;
func makeAdder(int amount): callable {
    return func(int value): int {
        return value + amount;
    };
}

io.println(makeAdder(5)(7));
io.println(makeAdder(10)(value: 3));
	`, "12\n13\n")
}

func TestParityIfElse(t *testing.T) {
	runParity(t, `import io;
int x = 7;
if (x > 10) {
    io.println("big");
} elseif (x > 3) {
    io.println("medium");
} else {
    io.println("small");
}
`, "medium\n")
}

func TestParityWhileLoop(t *testing.T) {
	runParity(t, `import io;
int i = 0;
int sum = 0;
while (i < 5) {
    sum = sum + i;
    i = i + 1;
}
io.println(sum);
`, "10\n")
}

func TestParityForLoop(t *testing.T) {
	runParity(t, `import io;
int total = 0;
for (int i = 1; i <= 5; i++) {
    total = total + i;
}
io.println(total);
`, "15\n")
}

func TestParityExceptionHandling(t *testing.T) {
	runParity(t, `import io;
try {
    throw Error("oops");
} catch (Error e) {
    io.println(e);
}
io.println("after");
`, "Error: oops\nafter\n")
}

func TestParityBreakContinue(t *testing.T) {
	runParity(t, `import io;
int sum = 0;
for (int i = 0; i < 10; i++) {
    if (i == 3) { continue; }
    if (i == 7) { break; }
    sum = sum + i;
}
io.println(sum);
`, "18\n")
}

func TestParityNullHandling(t *testing.T) {
	runParity(t, `import io;
?string s = null;
io.println(s == null);
s = "hello";
io.println(s == null);
io.println(s);
`, "true\nfalse\nhello\n")
}

func TestParityBooleanOperators(t *testing.T) {
	runParity(t, `import io;
io.println(true && false);
io.println(true || false);
io.println(true xor true);
io.println(true xor false);
io.println(!true);
io.println(!false);
`, "false\ntrue\nfalse\ntrue\nfalse\ntrue\n")
}

func TestParityTOMLStdlib(t *testing.T) {
	runParity(t, `import io;
import toml;
dict parsed = toml.parse("name = \"geb\"\nversion = 1\n");
io.println(parsed["name"]);
io.println(parsed["version"]);
`, "geb\n1\n")
}

func TestParityFromImportNative(t *testing.T) {
	runParity(t, `import io;
from crypt import passwordHash, passwordVerify as verify;
let h = passwordHash("hunter2", {"algorithm": "bcrypt", "cost": 4});
io.println(h.startsWith("$2y$04$"));
io.println(verify("hunter2", h));
io.println(verify("wrong", h));
`, "true\ntrue\nfalse\n")
}

func TestParityPasswordHashRoundTrip(t *testing.T) {
	runParity(t, `import io;
import crypt;
let h = crypt.passwordHash("hunter2", {"algorithm": "bcrypt", "cost": 4});
io.println(h.startsWith("$2y$04$"));
io.println(crypt.passwordVerify("hunter2", h));
io.println(crypt.passwordVerify("wrong", h));
io.println(crypt.passwordVerify("hunter2", "$2y$04$cAkpTnTMpIpo0m80Unnoc.XTtHYSLKMe1xOUiV8i7MRi.q6noRl3y"));
`, "true\ntrue\nfalse\ntrue\n")
}

func TestParityBinaryPack(t *testing.T) {
	runParity(t, `import io;
import binary;
import bytes;
let buf = binary.pack(">IH", 3735928559, 1024);
io.println(bytes.toHex(buf));
let parts = binary.unpack(">IH", buf);
io.println(parts);
io.println(binary.size(">IH"));
`, "deadbeef0400\n[3735928559, 1024]\n6\n")
}

func TestParityExceptionTypes(t *testing.T) {
	runParity(t, `import io;
try {
    io.println(10);
    throw ValueError("negative");
} catch (ValueError e) {
    io.println(e);
} catch (Error e) {
    io.println(e);
}
`, "10\nValueError: negative\n")
}

func TestParityExceptionFromCalledFunction(t *testing.T) {
	runParity(t, `import io;
func risky(int n): int {
    if (n < 0) {
        throw ValueError("negative input");
    }
    return n * 2;
}
try {
    let r = risky(-1);
    io.println("unreachable");
} catch (ValueError e) {
    io.println(e);
}
io.println("done");
`, "ValueError: negative input\ndone\n")
}

func TestParityNestedFunctions(t *testing.T) {
	runParity(t, `import io;
func outer(int x): int {
    func inner(int y): int {
        return x + y;
    }
    return inner(10);
}
io.println(outer(5));
io.println(outer(20));
`, "15\n30\n")
}

func TestParityNamedArguments(t *testing.T) {
	runParity(t, `import io;
func connect(string host, int port = 80, bool tls = false): string {
    string result = host + ":" + (port as string);
    if (tls) { result = result + "s"; }
    return result;
}
io.println(connect("example.com"));
io.println(connect("example.com", port: 443, tls: true));
io.println(connect("api.com", tls: true, port: 8443));
`, "example.com:80\nexample.com:443s\napi.com:8443s\n")
}

func TestParityTopLevelReturn(t *testing.T) {
	runParity(t, `import io;
io.println("before");
return;
io.println("after");
`, "before\n")
}

func TestParityConditionalExpression(t *testing.T) {
	runParity(t, `import io;
int x = 5;
string size = "small";
if (x > 3) { size = "big"; }
io.println(size);
string label = "other";
if (x == 5) { label = "five"; }
io.println(label);
`, "big\nfive\n")
}

func TestParityOptionalChaining(t *testing.T) {
	runParity(t, `import io;
class User {
    string name;
    func User(string name) { this.name = name; }
    func greet() { return "hello " + this.name; }
}
let u = User("Alice");
let n = null;
io.println(u?.name);
io.println(u?.greet());
let result = n?.name;
if (result == null) { io.println("null"); } else { io.println(result); }
`, "Alice\nhello Alice\nnull\n")
}

func TestParityTemplateModule(t *testing.T) {
	runParity(t, `import io;
import template;
io.println(template.renderString("<p>{{.name}}</p>", {"name": "<Ada>"}));
let tmpl = template.Template("Hello {{.name}}", "greeting");
io.println(tmpl.name());
io.println(tmpl.render({"name": "Grace"}));
let engine = template.Engine("templates");
io.println(engine.dir());
`, "<p>&lt;Ada&gt;</p>\ngreeting\nHello Grace\ntemplates\n")
}

func TestParityBcrypt(t *testing.T) {
	runParity(t, `import io;
import crypt;
let hash = crypt.bcryptHash("secret");
io.println(crypt.bcryptVerify("secret", hash) as string);
io.println(crypt.bcryptVerify("wrong", hash) as string);
let argon = crypt.argon2idHash("secret", {"memory": 64, "time": 1, "parallelism": 1, "keyLength": 16, "saltLength": 8});
io.println(argon.startsWith("$argon2id$") as string);
io.println(crypt.argon2idVerify("secret", argon) as string);
io.println(crypt.argon2idVerify("wrong", argon) as string);
`, "true\nfalse\ntrue\ntrue\nfalse\n")
}

func TestParityCompressModule(t *testing.T) {
	runParity(t, `import io;
import compress;
import bytes;
let original = bytes.fromString("hello world");
let compressed = compress.gzip(original);
let decompressed = compress.gunzip(compressed);
io.println(bytes.toString(decompressed));
io.println(compressed.length() > 0 as string);
`, "hello world\ntrue\n")
}

func TestParityVariadic(t *testing.T) {
	runParity(t, `import io;
func sum(int ...values): int {
    let total = 0;
    for (int v in values) {
        total = total + v;
    }
    return total;
}
io.println(sum() as string);
io.println(sum(1) as string);
io.println(sum(1, 2, 3) as string);
io.println(sum(10, 20, 30, 40) as string);
`, "0\n1\n6\n100\n")
}

func TestParityVariadicWithRequired(t *testing.T) {
	runParity(t, `import io;
func greet(string prefix, string ...names): string {
    let result = prefix;
    for (string n in names) {
        result = result + " " + n;
    }
    return result;
}
io.println(greet("Hello"));
io.println(greet("Hi", "Alice"));
io.println(greet("Hey", "Bob", "Carol", "Dave"));
`, "Hello\nHi Alice\nHey Bob Carol Dave\n")
}

// crypt.jwk/jwks produce RFC 7517 documents and crypt.jwtVerify
// accepts them, selecting by the token's kid and pinning the alg to
// the matched key.
func TestParityJWKSRoundTrip(t *testing.T) {
	runParity(t, `import io;
import crypt;
let privA = crypt.generateRsaKey(2048);
let privB = crypt.generateEcKey("P-256");
let jwkA = crypt.jwk(crypt.publicKey(privA), {"kid": "a"});
io.println(jwkA["kty"]);
io.println(jwkA["alg"]);
io.println(jwkA["use"]);
io.println((jwkA["kid"] as string) == "a");
let auto = crypt.jwk(crypt.publicKey(privA));
io.println(((auto["kid"] as string).length() > 20) as string);
let set = crypt.jwks([
    {"pem": crypt.publicKey(privA), "kid": "a"},
    {"pem": crypt.publicKey(privB), "kid": "b"},
]);
io.println((set["keys"] as list<any>).length());
let tokenA = crypt.jwtSign({"sub": "alice"}, privA, {"alg": "RS256", "kid": "a"});
let tokenB = crypt.jwtSign({"sub": "bob"}, privB, {"alg": "ES256", "kid": "b"});
let claimsA = crypt.jwtVerify(tokenA, set);
let claimsB = crypt.jwtVerify(tokenB, set);
io.println(claimsA["sub"]);
io.println(claimsB["sub"]);
let wrongKid = crypt.jwtSign({"sub": "eve"}, privA, {"alg": "RS256", "kid": "nope"});
io.println((crypt.jwtVerify(wrongKid, set) == null) as string);
let hsForged = crypt.jwtSign({"sub": "eve"}, "shh", {"alg": "HS256", "kid": "a", "allowedAlgs": ["HS256"]});
io.println((crypt.jwtVerify(hsForged, set) == null) as string);
`, "RSA\nRS256\nsig\ntrue\ntrue\n2\nalice\nbob\ntrue\ntrue\n")
}

func TestParityRSAKeyAndJWT(t *testing.T) {
	runParity(t, `import io;
import crypt;
let privPem = crypt.generateRsaKey(2048);
let pubPem = crypt.publicKey(privPem);
io.println(privPem.contains("PRIVATE KEY") as string);
io.println(pubPem.contains("PUBLIC KEY") as string);
let payload = {"sub": "alice", "iss": "test"};
let token = crypt.jwtSignRS256(payload, privPem);
io.println(token.contains(".") as string);
let verified = crypt.jwtVerifyRS256(token, pubPem);
io.println(verified["sub"]);
let bad = crypt.jwtVerifyRS256(token, crypt.publicKey(crypt.generateRsaKey(2048)));
io.println((bad == null) as string);
let decoded = crypt.jwtDecode(token);
io.println(decoded["header"]["alg"]);
`, "true\ntrue\ntrue\nalice\ntrue\nRS256\n")
}

func TestParityECKeyAndJWT(t *testing.T) {
	runParity(t, `import io;
import crypt;
let privPem = crypt.generateEcKey("P-256");
let pubPem = crypt.publicKey(privPem);
io.println(privPem.contains("PRIVATE KEY") as string);
io.println(pubPem.contains("PUBLIC KEY") as string);
let payload = {"sub": "bob", "role": "user"};
let token = crypt.jwtSignES256(payload, privPem);
io.println(token.contains(".") as string);
let verified = crypt.jwtVerifyES256(token, pubPem);
io.println(verified["sub"]);
let bad = crypt.jwtVerifyES256(token, crypt.publicKey(crypt.generateEcKey("P-256")));
io.println((bad == null) as string);
let decoded = crypt.jwtDecode(token);
io.println(decoded["header"]["alg"]);
`, "true\ntrue\ntrue\nbob\ntrue\nES256\n")
}

func TestParityEd25519Key(t *testing.T) {
	runParity(t, `import io;
import crypt;
let privPem = crypt.generateEd25519Key();
let pubPem = crypt.publicKey(privPem);
io.println(privPem.contains("PRIVATE KEY") as string);
io.println(pubPem.contains("PUBLIC KEY") as string);
`, "true\ntrue\n")
}

func TestParitySelfSignedCert(t *testing.T) {
	runParity(t, `import io;
import crypt;
let result = crypt.generateSelfSignedCert({
    "subject": {"commonName": "test.local", "organization": "TestOrg"},
    "dnsNames": ["test.local", "localhost"],
    "validDays": 30,
    "keyType": "EC-P256"
});
io.println(result["cert"].contains("CERTIFICATE") as string);
io.println(result["key"].contains("PRIVATE KEY") as string);
let parsed = crypt.parseCert(result["cert"]);
io.println(parsed["subject"]["commonName"]);
io.println(parsed["subject"]["organization"]);
io.println(parsed["keyType"]);
io.println(parsed["isCA"] as string);
`, "true\ntrue\ntest.local\nTestOrg\nEC\ntrue\n")
}

func TestParityGenerateCsr(t *testing.T) {
	runParity(t, `import io;
import crypt;
let key = crypt.generateEcKey("P-384");
let csr = crypt.generateCsr({
    "key": key,
    "subject": {"commonName": "example.com"},
    "dnsNames": ["example.com", "www.example.com"]
});
io.println(csr.contains("CERTIFICATE REQUEST") as string);
`, "true\n")
}

func TestParityFusedAccumulatorChecksGuards(t *testing.T) {
	runParity(t, `import io;
import freeze;
let xs = freeze.shallow([1]);
try { xs = xs.push(2); } catch (ImmutableError e) { io.println("caught ${e.message}"); }
io.println("${xs}");
list<int> nums = [1];
any bad2 = "bad";
try { nums = nums.push(bad2); } catch (TypeError e) { io.println("typed ok"); }
io.println("${nums}");
`, `caught cannot modify frozen list
[1]
typed ok
[1]
`)
}

// Guards the call fast path's skip-leading-param zeroing: an
// uninitialized local must read null even when its frame reuses
// locals-stack slots dirtied by earlier calls.
func TestParityUninitializedLocalIsNullAfterFrameReuse(t *testing.T) {
	runParity(t, `import io;
func probe(int n): ?int {
    ?int local;
    if (n > 100) { local = n; }
    return local;
}
func fill(int n): int {
    int a = n;
    int b = n * 2;
    int c = n * 3;
    if (n > 0) { return fill(n - 1) + a + b + c; }
    return 0;
}
io.println(fill(5));
io.println(probe(1) == null);
io.println("${probe(2)}");
`, "90\ntrue\nnull\n")
}

// withBodyFile streams a file from disk as the request body (1.16.0):
// the body never loads into a Geblang string and Content-Length comes
// from the file size. Round-trips over a real socket on both backends.
func TestParityRequestBuilderBodyFileStreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload.bin")
	source := `
import io;
import http;
let path = ` + strconv.Quote(path) + `;
io.writeText(path, "stream-me-".repeat(200));
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    let body = request["body"] as string;
    let lenHeader = (request["headers"] as dict<string, any>)["Content-Length"] as string;
    return {"status": 200, "body": "len:" + (body.length() as string) + " cl:" + lenHeader + " head:" + body.slice(0, 9)};
}, {});
let resp = http.request("http://" + http.serverAddr(server) + "/up")
    .withMethod("PUT")
    .withBodyFile(path)
    .send();
io.println(resp.text());
http.close(server);
`
	runParityWithStdlib(t, source, "len:2000 cl:2000 head:stream-me\n")
}

// http.wait blocks until the handle's server stops; a graceful
// shutdown from another callable releases the waiter.
func TestParityHTTPWaitReleasedByShutdown(t *testing.T) {
	runParityWithStdlib(t, `
import io;
import http;
import async;
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return {"status": 200, "body": "ok"};
}, {});
let stopper = async.run(func(): void {
    async.await(async.sleep(100));
    http.shutdown(server, 2000);
});
http.wait(server);
io.println("released");
async.await(stopper);
http.wait(server);
io.println("idempotent");
`, "released\nidempotent\n")
}

func TestParitySysInfo(t *testing.T) {
	runParity(t, `import io;
import sys;
let h = sys.hostname();
let p = sys.pid();
let pl = sys.platform();
let ar = sys.arch();
let td = sys.tmpdir();
io.println(h.length() > 0);
io.println(p > 0);
io.println(pl.length() > 0);
io.println(ar.length() > 0);
io.println(td.length() > 0);
`, "true\ntrue\ntrue\ntrue\ntrue\n")
}

func TestParitySysOSVersion(t *testing.T) {
	runParity(t, `import io;
import sys;
let v = sys.osVersion();
io.println(v.length() > 0);
`, "true\n")
}

func TestParityProcessIdentity(t *testing.T) {
	runParityStateful(t, `import io;
import process;
io.println(process.pid() > 0);
io.println(process.ppid() > 0);
io.println(typeof(process.uid()) == "int");
io.println(typeof(process.gid()) == "int");
io.println(process.euid() >= 0);
io.println(process.egid() >= 0);
io.println(typeof(process.groups()) == "list");
`, "true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\n")
}

func TestParityProcessExistsAndInfo(t *testing.T) {
	runParityStateful(t, `import io;
import process;
io.println(process.exists(process.pid()));
let info = process.info(process.pid());
io.println(info["pid"] == process.pid());
io.println(typeof(info["name"]) == "string");
io.println(info == null);
`, "true\ntrue\ntrue\nfalse\n")
}

func TestParityProcessList(t *testing.T) {
	runParityStateful(t, `import io;
import process;
let all = process.list();
let self = process.pid();
let found = false;
for (entry in all) {
    if (entry["pid"] == self) { found = true; }
}
io.println(all.length() > 0);
io.println(found);
`, "true\ntrue\n")
}

func TestParityProcessControlGated(t *testing.T) {
	runParityStateful(t, `import io;
import process;
func gated(string label, func body): void {
    try {
        body();
        io.println("${label}: NO ERROR");
    } catch (PermissionError e) {
        io.println("${label}: gated");
    } catch (Error e) {
        io.println("${label}: ${e.class}");
    }
}
gated("kill", func(): void { process.kill(1); });
gated("signal", func(): void { process.signal(1, "TERM"); });
gated("setuid", func(): void { process.setuid(0); });
gated("setgid", func(): void { process.setgid(0); });
`, "kill: gated\nsignal: gated\nsetuid: gated\nsetgid: gated\n")
}

func TestParityDefaultArgInReturnPosition(t *testing.T) {
	runParity(t, `import io;
func helper(string greeting = "hi"): string {
    return greeting;
}
func caller(): string {
    return helper();
}
func callerWithArg(): string {
    return helper("howdy");
}
io.println(caller());
io.println(callerWithArg());
`, "hi\nhowdy\n")
}

func TestParityDefaultArgInExpressionContexts(t *testing.T) {
	runParity(t, `import io;
func two(int a, int b = 10): int {
    return a + b;
}
io.println(two(5));
io.println(two(5, 20));
let xs = [two(1), two(1, 1)];
io.println(xs);
`, "15\n25\n[11, 2]\n")
}

func TestParityXML(t *testing.T) {
	runParity(t, `import io;
import xml;
let doc = xml.parse("<root><item>hello</item><item>world</item></root>");
io.println(doc["name"]);
io.println(doc["children"][0]["text"]);
io.println(doc["children"][1]["text"]);
`, "root\nhello\nworld\n")
}

func TestParityYAML(t *testing.T) {
	runParity(t, `import io;
import yaml;
let text = "name: alice\nage: 30\n";
let parsed = yaml.parse(text);
io.println(parsed["name"]);
io.println(parsed["age"]);
let out = yaml.stringify({"x": 1});
io.println(out.contains("x"));
`, "alice\n30\ntrue\n")
}

func TestParitySecrets(t *testing.T) {
	runParity(t, `import io;
import secrets;
let b = secrets.randomBytes(8);
io.println(b.length());
let h = secrets.randomHex(4);
io.println(h.length());
let n = secrets.randomInt(1, 100);
io.println(n >= 1);
io.println(n <= 100);
let a = secrets.randomBase64(6);
io.println(a.length() > 0);
`, "8\n8\ntrue\ntrue\ntrue\n")
}

func TestParityArgsParse(t *testing.T) {
	runParity(t, `import io;
import args;
let schema = {
    "verbose": {"type": "bool", "short": "v", "default": false, "help": "Verbose mode"},
    "output":  {"type": "string", "short": "o", "default": "out.txt", "help": "Output file"},
    "count":   {"type": "int", "default": 1, "help": "Count"},
};
let r = args.parse(["--verbose", "--output", "foo.txt", "--count", "3", "pos1", "pos2"], schema);
io.println(r["verbose"]);
io.println(r["output"]);
io.println(r["count"]);
io.println(r["_"][0]);
io.println(r["_"][1]);
io.println(r["error"] == null);
`, "true\nfoo.txt\n3\npos1\npos2\ntrue\n")
}

func TestParityArgsShortFlags(t *testing.T) {
	runParity(t, `import io;
import args;
let schema = {
    "verbose": {"type": "bool", "short": "v"},
    "output":  {"type": "string", "short": "o", "default": ""},
};
let r = args.parse(["-v", "-o", "bar.txt"], schema);
io.println(r["verbose"]);
io.println(r["output"]);
io.println(r["_"].length());
`, "true\nbar.txt\n0\n")
}

func TestParityArgsInlineValue(t *testing.T) {
	runParity(t, `import io;
import args;
let schema = {
    "name": {"type": "string", "default": ""},
    "count": {"type": "int", "default": 0},
};
let r = args.parse(["--name=alice", "--count=5"], schema);
io.println(r["name"]);
io.println(r["count"]);
`, "alice\n5\n")
}

func TestParityArgsDefaults(t *testing.T) {
	runParity(t, `import io;
import args;
let schema = {
    "flag":  {"type": "bool", "default": false},
    "value": {"type": "string", "default": "hello"},
    "num":   {"type": "int", "default": 42},
};
let r = args.parse([], schema);
io.println(r["flag"]);
io.println(r["value"]);
io.println(r["num"]);
io.println(r["_"].length());
`, "false\nhello\n42\n0\n")
}

func TestParityArgsHelp(t *testing.T) {
	runParity(t, `import io;
import args;
let schema = {
    "verbose": {"type": "bool", "short": "v", "help": "Enable verbose mode"},
};
let h = args.help("mytool", schema);
io.println(h.contains("mytool"));
io.println(h.contains("verbose"));
io.println(h.contains("-v"));
`, "true\ntrue\ntrue\n")
}

func TestParityTernaryBasic(t *testing.T) {
	runParity(t, `import io;
io.println(true ? "yes" : "no");
io.println(false ? "yes" : "no");
`, "yes\nno\n")
}

func TestParityTernaryWithExpressions(t *testing.T) {
	runParity(t, `import io;
let x = 5;
let y = x > 3 ? "big" : "small";
io.println(y);
let z = x < 3 ? "big" : "small";
io.println(z);
`, "big\nsmall\n")
}

func TestParityTernaryNested(t *testing.T) {
	runParity(t, `import io;
io.println(true ? (false ? "a" : "b") : "c");
io.println(false ? "a" : (true ? "b" : "c"));
`, "b\nb\n")
}

func TestParityTernaryInExpression(t *testing.T) {
	runParity(t, `import io;
io.println("val: " + (true ? "one" : "two"));
let x = 10;
io.println((x > 5 ? "pos" : "neg") + "!");
`, "val: one\npos!\n")
}

func TestParityForByStep(t *testing.T) {
	runParity(t, `import io;
for (i in 0..10 by 2) {
    io.print(i as string + " ");
}
io.println("");
`, "0 2 4 6 8 10 \n")
}

func TestParityForByStepExclusive(t *testing.T) {
	runParity(t, `import io;
for (i in 0..<10 by 3) {
    io.print(i as string + " ");
}
io.println("");
`, "0 3 6 9 \n")
}

func TestParityForByStepExpr(t *testing.T) {
	runParity(t, `import io;
let step = 4;
for (i in 0..12 by step) {
    io.print(i as string + " ");
}
io.println("");
`, "0 4 8 12 \n")
}

// TestParityImageTransforms verifies the image module (native imagenative +
// stdlib Image class) produces identical output on both backends. The dual-name
// shadow bug returned a raw handle on the evaluator and a wrapped Image on the
// VM; this locks that divergence shut.
func TestParityImageTransforms(t *testing.T) {
	runParityWithStdlib(t, `
import image;
import io;
let img = image.blank(20, 10);
let r = img.resize(5, 8);
io.println("${r.width()}x${r.height()}");
let c = img.crop(1, 1, 4, 3);
io.println("${c.width()}x${c.height()}");
let rot = img.rotate(90);
io.println("${rot.width()}x${rot.height()}");
let png = img.encode("png");
io.println("${png.length() > 0}");
io.println("${image.loadBytes(png).width()}");
`, "5x8\n4x3\n10x20\ntrue\n20\n")
}

// TestParityWebRouterRealServe verifies a web.router app served over a real
// socket resolves through the callback child evaluator: the HTTP handler runs in
// a child and must find the web app registered on the parent at setup. Before
// the parent-walk fix this failed with "unknown web app handle". Both backends.
func TestParityWebRouterRealServe(t *testing.T) {
	runParityWithStdlib(t, `
import web.router as router;
import http;
import io;
let app = router.newRouter();
router.get(app, "/hi", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "ok:" + (req["path"] as string)};
});
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return router.handle(app, request);
}, {});
io.println(http.get("http://" + http.serverAddr(server) + "/hi").text());
http.close(server);
`, "ok:/hi\n")
}

// TestParityWebRouterRequestResponse verifies native web passes a rich Request
// (and Response to after-middleware) when a handler/middleware opts in by
// declaring those param types, while plain func(dict) handlers still work and a
// returned Response is serialized rather than mangled. Runs on both backends.
func TestParityWebRouterRequestResponse(t *testing.T) {
	runParityWithStdlib(t, `
import web.router as router;
import http;
import json;
import io;
let app = router.newRouter();
router.before(app, func(Request req): ?dict<string, any> {
    if (req.path == "/blocked") { return {"status": 403, "body": "no"}; }
    return null;
});
router.get(app, "/hi", func(Request req): Response {
    return http.jsonResponse({"path": req.path, "get": req.isMethod("GET")});
});
router.get(app, "/dict", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "dictok"};
});
router.after(app, func(Request req, Response resp): Response {
    return resp.withHeader("X-Path", req.path);
});
let r1 = router.handle(app, {"method": "GET", "path": "/hi", "headers": {}, "body": ""});
let p1 = json.parse(r1["body"] as string);
io.println(r1["status"]);
io.println((r1["headers"] as dict<string, any>)["X-Path"]);
io.println(p1["path"]);
io.println(p1["get"]);
let r2 = router.handle(app, {"method": "GET", "path": "/dict", "headers": {}, "body": ""});
io.println(r2["status"]);
io.println(r2["body"]);
io.println((r2["headers"] as dict<string, any>)["X-Path"]);
let r3 = router.handle(app, {"method": "GET", "path": "/blocked", "headers": {}, "body": ""});
io.println(r3["status"]);
io.println(r3["body"]);
`, "200\n/hi\n/hi\ntrue\n200\ndictok\n/dict\n403\nno\n")
}

// TestParityRequestRouteParam verifies the rich Request route-param accessors
// (routeParam / routeParams) and that toDict includes route params, identically
// on both backends.
func TestParityRequestRouteParam(t *testing.T) {
	runParityWithStdlib(t, `
import web.router as router;
import http;
import io;
let app = router.newRouter();
router.get(app, "/users/:id/posts/:slug", func(Request req): Response {
    io.println(req.routeParam("id"));
    io.println(req.routeParam("slug"));
    io.println(req.routeParam("missing") == null);
    io.println(req.routeParams());
    let d = req.toDict();
    let key = "params";
    io.println(d.contains(key));
    return http.jsonResponse({"ok": true});
});
let r = router.handle(app, {"method": "GET", "path": "/users/42/posts/hello", "headers": {}, "body": ""});
io.println(r["status"]);
`, "42\nhello\ntrue\n{\"id\": \"42\", \"slug\": \"hello\"}\ntrue\n200\n")
}

func TestParityDeclarationElementTypeEnforcement(t *testing.T) {
	// Valid typed declarations must compile and run without error.
	runParity(t, `import io;
list<int> nums = [1, 2, 3];
list<string> strs = ["a", "b"];
io.println(nums.length());
io.println(strs.length());
`, "3\n2\n")

	// Wrong element type in list declaration - error names the offending element.
	runErrorParity(t, `
list<int> bad = ["a", "b", "c"];
`, "cannot assign", "list<int>", "element at index 0 is string")

	// Wrong element type in dict declaration.
	runErrorParity(t, `
dict<string, int> scores = {"alice": "not-an-int"};
`, "cannot assign")

	// Wrong element type in set declaration.
	runErrorParity(t, `
set<int> s = {"a", "b"};
`, "cannot assign")

	// Wrong collection kind (dict to list).
	runErrorParity(t, `
list<int> bad = {"a": 1};
`, "cannot assign")

	// T[] alias syntax: valid.
	runParity(t, `import io;
int[] nums = [1, 2, 3];
io.println(nums.length());
`, "3\n")

	// T[] alias syntax: wrong element type.
	runErrorParity(t, `
int[] bad = ["a", "b"];
`, "cannot assign")

	// Heterogeneous list: error message identifies the offending element.
	runErrorParity(t, `
list<int> bad = [1, 2, "oops"];
`, "element at index 2 is string")
}

// Dual-name modules: stdlib `async.sync` wraps native `async.sync`.
// External callers see classes and native free functions on one alias.
func TestParityDualNameModule(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async.sync as sync;
let m = sync.Mutex();
m.lock();
io.println("class works");
m.unlock();
let h = sync.mutexNew();
sync.mutexLock(h);
io.println("native works");
sync.mutexUnlock(h);
`, "class works\nnative works\n")
}

func TestParityCron(t *testing.T) {
	// Parse + field values, then validate + special.
	runParity(t, `import io;
import cron;
let p = cron.parse("0 9 * * 1-5");
io.println(p["minute"]);
io.println(p["hour"]);
io.println(p["dayOfWeek"]);
io.println(p["special"]);
io.println(cron.isValid("0 9 * * 1-5"));
io.println(cron.isValid("nope"));
io.println(cron.isValid("@daily"));
let d = cron.parse("@daily");
io.println(d["special"]);
io.println(d["minute"]);
io.println(d["hour"]);
`, "[0]\n[9]\n[1, 2, 3, 4, 5]\nnull\ntrue\nfalse\ntrue\n@daily\n[0]\n[0]\n")

	// nextAfter: 2025-02-01T00:00:00Z (Sunday) -> 2025-02-03T09:00:00Z.
	runParity(t, `import io;
import cron;
io.println(cron.nextAfter("0 9 * * 1-5", 1738368000));
`, "1738573200\n")

	// nextN returns the next N occurrences.
	runParity(t, `import io;
import cron;
let firings = cron.nextN("0 * * * *", 0, 3);
io.println(firings);
`, "[3600, 7200, 10800]\n")

	// Named months and days work case-insensitively.
	runParity(t, `import io;
import cron;
let p = cron.parse("0 0 * jan,jul mon");
io.println(p["month"]);
io.println(p["dayOfWeek"]);
`, "[1, 7]\n[1]\n")

	// Step expressions.
	runParity(t, `import io;
import cron;
let p = cron.parse("*/15 0 * * *");
io.println(p["minute"]);
`, "[0, 15, 30, 45]\n")
}

func TestParityUnicode(t *testing.T) {
	// Round-trip NFC <-> NFD on a single accented character.
	// NFC e-acute = U+00E9 (1 code point); NFD = U+0065 + U+0301 (2).
	runParity(t, `import io;
import unicode;
let nfc = "é";
let nfd = "é";
io.println(unicode.normalize(nfd, "NFC") == nfc);
io.println(unicode.normalize(nfc, "NFD") == nfd);
io.println(nfc.length());
io.println(nfd.length());
`, "true\ntrue\n1\n2\n")

	// isNormalized reports both directions.
	runParity(t, `import io;
import unicode;
io.println(unicode.isNormalized("é", "NFC"));
io.println(unicode.isNormalized("é", "NFC"));
io.println(unicode.isNormalized("é", "NFD"));
io.println(unicode.isNormalized("é", "NFD"));
`, "true\nfalse\nfalse\ntrue\n")

	// Compatibility decomposition: ligature fi (U+FB01) -> "fi" under NFKC / NFKD.
	runParity(t, `import io;
import unicode;
let lig = "ﬁ";
io.println(unicode.normalize(lig, "NFKC"));
io.println(unicode.normalize(lig, "NFKD"));
io.println(unicode.normalize(lig, "NFC") == lig);
`, "fi\nfi\ntrue\n")

	// Unknown form name throws.
	runParity(t, `import io;
import unicode;
try {
    unicode.normalize("x", "BAD");
} catch (Error e) {
    io.println("caught");
}
`, "caught\n")
}

func TestParityMsgpack(t *testing.T) {
	// Primitives encode to the spec-fixed byte sequences.
	runParity(t, `import io;
import msgpack;
import bytes;
io.println(bytes.toHex(msgpack.encode(null)));
io.println(bytes.toHex(msgpack.encode(true)));
io.println(bytes.toHex(msgpack.encode(false)));
io.println(bytes.toHex(msgpack.encode(0)));
io.println(bytes.toHex(msgpack.encode(127)));
io.println(bytes.toHex(msgpack.encode(-1)));
io.println(bytes.toHex(msgpack.encode(-32)));
io.println(bytes.toHex(msgpack.encode("hello")));
io.println(bytes.toHex(msgpack.encode([])));
io.println(bytes.toHex(msgpack.encode([1, 2, 3])));
io.println(bytes.toHex(msgpack.encode({})));
`, "c0\nc3\nc2\n00\n7f\nff\ne0\na568656c6c6f\n90\n93010203\n80\n")

	// Round trip for nested structures.
	runParity(t, `import io;
import msgpack;
let v = {"items": [1, 2, 3], "meta": {"k": "v"}};
let b = msgpack.encode(v);
let back = msgpack.decode(b);
io.println(back["items"]);
io.println(back["meta"]["k"]);
`, "[1, 2, 3]\nv\n")

	// Float is preserved as float; decimal as a lossless string.
	runParity(t, `import io;
import msgpack;
let f = (1.5 as float);
io.println(msgpack.decode(msgpack.encode(f)));
let d = 1.5;
io.println(msgpack.decode(msgpack.encode(d)));
`, "1.5\n1.5000000000\n")

	// validate + tryDecode behaviours.
	runParity(t, `import io;
import msgpack;
import bytes;
io.println(msgpack.validate(msgpack.encode([1, 2, 3])));
io.println(msgpack.validate(bytes.fromHex("ff80")));
io.println(msgpack.tryDecode(bytes.fromHex("ff80")));
io.println(msgpack.tryDecode(bytes.fromHex("a3616263")));
`, "true\nfalse\nnull\nabc\n")

	// int boundaries spill into the larger integer tags.
	runParity(t, `import io;
import msgpack;
import bytes;
io.println(bytes.toHex(msgpack.encode(128)));
io.println(bytes.toHex(msgpack.encode(1000)));
io.println(bytes.toHex(msgpack.encode(-1000)));
io.println(msgpack.decode(msgpack.encode(128)));
io.println(msgpack.decode(msgpack.encode(1000)));
io.println(msgpack.decode(msgpack.encode(-1000)));
`, "d10080\nd103e8\nd1fc18\n128\n1000\n-1000\n")

	// Bytes round-trip.
	runParity(t, `import io;
import msgpack;
import bytes;
let b = bytes.fromHex("deadbeef");
let enc = msgpack.encode(b);
io.println(bytes.toHex(enc));
io.println(bytes.toHex(msgpack.decode(enc)));
`, "c404deadbeef\ndeadbeef\n")
}

func TestParityLruCache(t *testing.T) {
	// Basic put / get with eviction order.
	runParityWithStdlib(t, `import io;
import lrucache;
let c = lrucache.LruCache<string, int>(3);
c.put("a", 1); c.put("b", 2); c.put("c", 3);
io.println(c.length());
io.println(c.get("a"));
c.put("d", 4);
io.println(c.get("a"));
io.println(c.get("b"));
io.println(c.get("c"));
io.println(c.get("d"));
`, "3\n1\n1\nnull\n3\n4\n")

	// has() does not bump LRU order; delete() removes.
	runParityWithStdlib(t, `import io;
import lrucache;
let c = lrucache.LruCache<string, int>(2);
c.put("x", 1);
c.put("y", 2);
io.println(c.has("x"));
c.put("z", 3);
io.println(c.has("x"));
io.println(c.delete("y"));
io.println(c.delete("missing"));
io.println(c.length());
`, "true\nfalse\ntrue\nfalse\n1\n")

	// Stats counters - field access avoids dict display-order
	// divergence between backends.
	runParityWithStdlib(t, `import io;
import lrucache;
let c = lrucache.LruCache<string, int>(2);
c.put("a", 1);
c.put("b", 2);
c.get("a");
c.get("a");
c.get("missing");
c.put("c", 3);
let s = c.stats();
io.println(s["hits"]);
io.println(s["misses"]);
io.println(s["evictions"]);
io.println(s["expirations"]);
`, "2\n1\n1\n0\n")

	// Capacity must be at least 1.
	runParityWithStdlib(t, `import io;
import lrucache;
try {
    let c = lrucache.LruCache<string, int>(0);
} catch (ValueError e) {
    io.println("caught");
}
`, "caught\n")
}

// Regression: the VM's user-iterator dispatch looked the iterator's
// class up via the running chunk's classInfo, which fails for an
// instance whose class is defined in another module. Iteration of
// any stdlib class that implements __iter / __done / __next reported
// "is not an iterator" even though the trampoline table populated at
// import time exposed the methods. Fix routes the presence check
// through iter.Class.Methods for foreign classes and lets thrown
// errors flow back through propagateModuleError so try / catch
// around the loop still fires.
func TestParityIterAcrossStdlibBoundary(t *testing.T) {
	runParityWithStdlib(t, `import io;
import deque;
let d = deque.Deque<int>();
d.pushBack(1); d.pushBack(2); d.pushBack(3);
for (var x in d) {
    io.println(x);
}
`, "1\n2\n3\n")
}

func TestParityPriorityQueue(t *testing.T) {
	// Natural-order min-heap drains in ascending order.
	runParityWithStdlib(t, `import io;
import priorityq;
let q = priorityq.PriorityQueue<int>();
q.push(3); q.push(1); q.push(4); q.push(1); q.push(5); q.push(9); q.push(2); q.push(6);
io.println(q.length());
io.println(q.peek());
while (!q.isEmpty()) {
    io.println(q.pop());
}
`, "8\n1\n1\n1\n2\n3\n4\n5\n6\n9\n")

	// Custom comparator reverses the order.
	runParityWithStdlib(t, `import io;
import priorityq;
let q = priorityq.PriorityQueue<int>(func(int a, int b): int { return b - a; });
q.push(2); q.push(7); q.push(1); q.push(5);
io.println(q.pop());
io.println(q.pop());
io.println(q.pop());
io.println(q.pop());
`, "7\n5\n2\n1\n")

	// pushPop, drain, clear, empty errors.
	runParityWithStdlib(t, `import io;
import priorityq;
let q = priorityq.PriorityQueue<int>();
q.push(5); q.push(10); q.push(1);
io.println(q.pushPop(0));
io.println(q.pushPop(7));
io.println(q.drain());
io.println(q.isEmpty());

q.push(3); q.push(2);
q.clear();
io.println(q.length());

try { q.pop(); } catch (ValueError e) { io.println("empty"); }
`, "0\n1\n[5, 7, 10]\ntrue\n0\nempty\n")
}

func TestParityJumpIfModZero(t *testing.T) {
	runParity(t, `import io;
int total = 0;
for (int i = 0; i < 12; i++) {
    if (i % 3 == 0) { total = total + i; }
    else            { total = total - 1; }
}
io.println(total);
`, "10\n")

	runParity(t, `import io;
int n = 0;
for (int i = 0; i < 10; i++) {
    if (i % 2 != 0) { n = n + 1; }
}
io.println(n);
`, "5\n")

	// 0 == i % d reversed form takes the same fused opcode.
	runParity(t, `import io;
int n = 0;
for (int i = 0; i < 9; i++) {
    if (0 == i % 4) { n = n + 1; }
}
io.println(n);
`, "3\n")

	// Negative dividend exercises the modulo-correction branch.
	runParity(t, `import io;
int n = 0;
for (int i = -6; i <= 0; i++) {
    if (i % 3 == 0) { n = n + 1; }
}
io.println(n);
`, "3\n")
}

func TestParityFreeze(t *testing.T) {
	// freeze.shallow prevents list index mutation.
	runErrorParity(t, `import freeze; let x = freeze.shallow([1,2,3]); x[0] = 99;`, "ImmutableError")

	// freeze.shallow prevents dict mutation.
	runErrorParity(t, `import freeze; let x = freeze.shallow({"a": 1}); x["a"] = 2;`, "ImmutableError")

	// freeze.isFrozen returns false for unfrozen collection.
	runParity(t, `import freeze; import io; io.println(freeze.isFrozen([1,2]));`, "false\n")

	// freeze.isFrozen returns true after freeze.shallow.
	runParity(t, `import freeze; import io; let x = freeze.shallow([1,2]); io.println(freeze.isFrozen(x));`, "true\n")

	// primitives are always considered frozen.
	runParity(t, `import freeze; import io; io.println(freeze.isFrozen(42));`, "true\n")

	// const shallow-freezes a list.
	runErrorParity(t, `const x = [1,2,3]; x[0] = 99;`, "ImmutableError")

	// const shallow-freezes a dict.
	runErrorParity(t, `const x = {"a": 1}; x["a"] = 2;`, "ImmutableError")

	// .copy() returns a mutable copy of a frozen list.
	runParity(t, `import freeze; import io; let x = freeze.shallow([1,2,3]); let y = x.copy(); y[0] = 99; io.println(y[0]);`, "99\n")

	// .copy() returns a mutable copy of a frozen dict.
	runParity(t, `import freeze; import io; let x = freeze.shallow({"a": 1}); let y = x.copy(); y["a"] = 2; io.println(y["a"]);`, "2\n")

	// ImmutableError is catchable.
	runParity(t, `import freeze; import io;
let x = freeze.shallow([1]);
try { x[0] = 99; } catch (ImmutableError e) { io.println("caught"); }
`, "caught\n")

	// freeze.deep freezes nested collections.
	runErrorParity(t, `import freeze;
let x = freeze.deep([[1,2],[3,4]]); x[0][0] = 99;`, "ImmutableError")

	// @immutable class instance cannot have fields mutated after construction.
	runErrorParity(t, `
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}
let p = Point(1, 2); p.x = 99;
`, "ImmutableError")

	// @immutable instance fields are readable.
	runParity(t, `import io;
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}
let p = Point(3, 4); io.println(p.x);
`, "3\n")
}

// TestParityMultiAssign covers the J2 `a, b = b, a` multi-target form.
func TestParityMultiAssign(t *testing.T) {
	runParity(t, `import io;
let a = 1;
let b = 2;
a, b = b, a;
io.println(a);
io.println(b);
let x = 0;
let y = 0;
let z = 0;
x, y, z = [10, 20, 30];
io.println(x);
io.println(y);
io.println(z);
`, "2\n1\n10\n20\n30\n")
}

// TestParityVariadicClosure covers the fix where the bytecode compiler did
// not set FunctionInfo.Variadic for closure literals (only for top-level
// function statements). Variadic packing in startFunctionWithValidation
// silently passed the first variadic arg as a non-list, breaking any
// closure with `any ...args` semantics.
func TestParityVariadicClosure(t *testing.T) {
	runParity(t, `import io;
let f = func(any ...args): int { return args.length(); };
io.println(f());
io.println(f(1));
io.println(f(1, 2, 3));
`, "0\n1\n3\n")
}

// TestParityAesRoundTrip verifies AES-256-GCM encrypts then decrypts
// to the original plaintext on both backends.
func TestParityAesRoundTrip(t *testing.T) {
	runParity(t, `import crypt;
import io;
import bytes;
let key = bytes.fromString("0123456789abcdef0123456789abcdef");
let enc = crypt.aesEncrypt(key, "hello aes");
let dec = crypt.aesDecrypt(key, enc["nonce"], enc["ciphertext"]);
io.println(dec.toString());
`, "hello aes\n")
}

// TestParityChaCha20RoundTrip verifies XChaCha20-Poly1305 encrypts
// then decrypts to the original plaintext on both backends.
func TestParityChaCha20RoundTrip(t *testing.T) {
	runParity(t, `import crypt;
import io;
import bytes;
let key = bytes.fromString("0123456789abcdef0123456789abcdef");
let enc = crypt.chacha20Encrypt(key, "hello chacha");
let dec = crypt.chacha20Decrypt(key, enc["nonce"], enc["ciphertext"]);
io.println(dec.toString());
`, "hello chacha\n")
}

// TestParityAesWrongKeyRejected verifies AES-GCM authentication rejects
// a ciphertext when the decryption key differs from the encryption key.
func TestParityAesWrongKeyRejected(t *testing.T) {
	runErrorParity(t, `import crypt;
import bytes;
let key1 = bytes.fromString("0123456789abcdef0123456789abcdef");
let key2 = bytes.fromString("ABCDEF0123456789ABCDEF0123456789");
let enc = crypt.aesEncrypt(key1, "hello aes");
crypt.aesDecrypt(key2, enc["nonce"], enc["ciphertext"]);
`, "authentication failed")
}

// TestParityAesBadKeySize verifies the 32-byte key requirement is
// enforced on both backends.
func TestParityAesBadKeySize(t *testing.T) {
	runErrorParity(t, `import crypt;
import bytes;
crypt.aesEncrypt(bytes.fromString("short"), "x");
`, "32-byte AES-256 key")
}

// TestParityInitBlockRunsInOrder verifies that an init block executes
// at module-load time, in source order with the surrounding top-level
// code, on both backends.
func TestParityInitBlockRunsInOrder(t *testing.T) {
	runParity(t, `import io;
io.println("before");
init {
    io.println("init");
}
io.println("after");
`, "before\ninit\nafter\n")
}

// TestParityInitBlockSeesAndMutatesTopLevelState verifies init can
// read and write top-level declarations declared above it.
func TestParityInitBlockSeesAndMutatesTopLevelState(t *testing.T) {
	runParity(t, `import io;
int count = 0;
init {
    count = count + 5;
}
io.println(count);
`, "5\n")
}

// TestParityModuleTopLevelRuleRejectsFreeStandingStatement verifies
// the module-top-level discipline at import time: both the evaluator
// and the bytecode VM run semantic.Analyze on imported source
// modules, so a violating module fails to load on either backend with
// the same diagnostic.
func TestParityModuleTopLevelRuleRejectsFreeStandingStatement(t *testing.T) {
	// Write the violating module to a fresh dir we add to the
	// resolver's module path. The module is `loud.gb` containing
	// `module loud; import io; io.println("..."); ` - a free-standing
	// expression statement that the rule should reject.
	dir := t.TempDir()
	badModule := filepath.Join(dir, "loud.gb")
	if err := os.WriteFile(badModule, []byte(`module loud;
import io;
io.println("ran at import time");
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	source := `import loud;
`

	// Evaluator path - manually drive it so we can inject the
	// modulePath, mirroring what evaluator.NewWithArgsAndModulePaths does.
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	_, evErr := ev.Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected error importing violating module, got nil")
	}
	if !strings.Contains(evErr.Error(), "free-standing top-level") {
		t.Fatalf("evaluator error should describe the rule: %q", evErr.Error())
	}

	// VM path - use the stdlib loader, but seed it with our temp dir
	// as the search path.
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
	vmErr := vm.Run()
	if vmErr == nil {
		t.Fatalf("vm: expected error importing violating module, got nil")
	}
	if !strings.Contains(vmErr.Error(), "free-standing top-level") {
		t.Fatalf("vm error should describe the rule: %q", vmErr.Error())
	}
}

// TestParityModuleTopLevelRuleAcceptsInitBlock is the positive
// counterpart: the same imperative work moved into an init block is
// accepted by both backends.
func TestParityModuleTopLevelRuleAcceptsInitBlock(t *testing.T) {
	dir := t.TempDir()
	goodModule := filepath.Join(dir, "quiet.gb")
	if err := os.WriteFile(goodModule, []byte(`module quiet;
import io;
export int loaded = 0;
init {
    loaded = 1;
    io.println("loaded once");
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	source := `import io;
import quiet;
io.println(quiet.loaded);
`

	// Evaluator path.
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != "loaded once\n1\n" {
		t.Fatalf("evaluator output: got %q", evOut.String())
	}

	// VM path.
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
	if vmOut.String() != "loaded once\n1\n" {
		t.Fatalf("vm output: got %q", vmOut.String())
	}
}

func TestParityTwoHopCrossModuleMethodDispatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Root {
    func Root() {}
    func describe(): string { return "from-root"; }
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mid.gb"), []byte(`module mid;
import base;
export class Middle extends base.Root {
    func Middle() { parent(); }
}
`), 0o644); err != nil {
		t.Fatalf("write mid: %v", err)
	}

	source := `import io;
import mid;
class Leaf extends mid.Middle {
    func Leaf() { parent(); }
}
io.println(Leaf().describe());
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	want := "from-root\n"

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

// Regression: callee mutation of a cross-module dict/list/nested arg must be visible to the caller on both backends.
func TestParityCrossModuleContainerMutation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mutator.gb"), []byte(`module mutator;
export func setFlag(dict<string, any> d): void { d["flag"] = true; }
export func pushAll(list<any> xs): void { xs.push(99); }
export func mutateInner(dict<string, any> d): void { (d["items"] as list<any>).push(99); }
`), 0o644); err != nil {
		t.Fatalf("write mutator: %v", err)
	}
	source := `import io;
import mutator;
let d = {"value": 1};
mutator.setFlag(d);
io.println(d.contains("flag"));
let xs = [1, 2];
mutator.pushAll(xs);
io.println(xs.length());
let nested = {"items": [1]};
mutator.mutateInner(nested);
io.println((nested["items"] as list<any>).length());
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	want := "true\n3\n2\n"

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

// TestParityAliasedNativeImportShadowedByLocal verifies that an
// aliased-import name shadowed by a local variable still dispatches to
// the local (selector becomes a method call on the value), preserving
// the established precedence.
func TestParityAliasedNativeImportShadowedByLocal(t *testing.T) {
	runParity(t, `import io;
import path as natpath;
func demo(): void {
    let natpath = "hello";
    io.println(natpath);
}
demo();
`, "hello\n")
}

// TestParityTickerStops verifies time.scheduler.Ticker stops after
// stop() and ticks() reports the accumulated count.
func TestParityTickerStops(t *testing.T) {
	runParityWithStdlib(t, `import time.scheduler as sched;
import async;
import io;
let n = 0;
let ticker = sched.Ticker(20, func(): void { n = n + 1; });
async.await(async.sleep(75));
ticker.stop();
io.println(ticker.ticks() >= 2);
io.println(n >= 2);
`, "true\ntrue\n")
}

// TestParityBase32RoundTrip verifies base32 encode/decode round-trips
// a UTF-8 string identically on both backends.
func TestParityBase32RoundTrip(t *testing.T) {
	runParity(t, `import encoding;
import io;
let enc = encoding.base32Encode("hello world");
io.println(enc);
io.println(encoding.base32Decode(enc).toString());
`, "NBSWY3DPEB3W64TMMQ======\nhello world\n")
}

// TestParityBase58RoundTrip verifies base58 encode/decode round-trips
// a UTF-8 string identically on both backends.
func TestParityBase58RoundTrip(t *testing.T) {
	runParity(t, `import encoding;
import io;
let enc = encoding.base58Encode("hello world");
io.println(enc);
io.println(encoding.base58Decode(enc).toString());
`, "StV1DL6CwTryKyV\nhello world\n")
}

// TestParityBase58LeadingZeros verifies base58 preserves leading zero
// bytes (each becomes a leading "1" in the output, per the Bitcoin spec).
func TestParityBase58LeadingZeros(t *testing.T) {
	runParity(t, `import encoding;
import io;
import bytes;
let raw = bytes.fromHex("000000aa");
let enc = encoding.base58Encode(raw);
io.println(enc);
let dec = encoding.base58Decode(enc);
io.println(dec.toHex());
`, "1113w\n000000aa\n")
}

// TestParityWithEnterExit verifies that __enter__/__exit__ magic
// methods on an instance run at with-block entry/exit and that
// __enter__'s return value supplies the binding.
func TestParityWithEnterExit(t *testing.T) {
	runParity(t, `import io;
class Guarded {
    string label;
    func Guarded(string label) { this.label = label; }
    func __enter__(): string { io.println("enter " + this.label); return "bound-" + this.label; }
    func __exit__(): void { io.println("exit " + this.label); }
}
with (name = Guarded("ada")) {
    io.println("body sees " + name);
}
`, "enter ada\nbody sees bound-ada\nexit ada\n")
}

// TestParityWithoutBinding verifies the bare `with (expr) { ... }`
// form (no `name =`) calls __exit__ on the resource when defined.
func TestParityWithoutBinding(t *testing.T) {
	runParity(t, `import io;
class R {
    func R() { io.println("acq"); }
    func __exit__(): void { io.println("exit"); }
}
with (R()) {
    io.println("body");
}
`, "acq\nbody\nexit\n")
}

// TestParityDelClearsBinding verifies that re-binding the same
// name after `del` produces a fresh value and the static analyzer
// accepts subsequent references.
func TestParityDelClearsBinding(t *testing.T) {
	runParity(t, `import io;
class R {
    string label;
    func R(string label) { this.label = label; }
    func ~R() { io.println("rel " + this.label); }
}
let r = R("first");
del r;
let r = R("second");
io.println("middle " + r.label);
`, "rel first\nmiddle second\nrel second\n")
}

// TestParityClosureCaptureCamelCase guards a 1.0.2 regression: the
// compiler's freeVarSet was lowercasing identifier names while local
// scope entries kept their original case, causing closures that
// captured a variable with uppercase letters in its name to silently
// miss the capture. The closure body then emitted OpGetLocal at the
// wrong slot and at runtime read whichever value happened to be
// there, producing wildly wrong type errors. Both backends must now
// resolve case-sensitively.
func TestParityClosureCaptureCamelCase(t *testing.T) {
	runParity(t, `import io;

func makeAdapter(list<string> pathParamNames): callable {
    return func(int x): void {
        io.println(typeof(pathParamNames));
    };
}

makeAdapter(["a", "b"])(42);
`, "list\n")
}

// TestParityForwardFunctionReferences guards a compiler regression
// where a function calling a sibling declared later in the same
// file (`func a() { return b(); } func b() { ... }`) failed with
// "no matching overload" because the forward-declared FunctionInfo
// hadn't been populated with parameter / return-type metadata by
// the time the body of `a` was compiled. Pre-pass now records
// signatures up front; bodies fill in on the second pass.
func TestParityForwardFunctionReferences(t *testing.T) {
	runParity(t, `import io;

func a(): bool {
    return b(7);
}

func b(int x): bool {
    return x > 0;
}

io.println(a());
`, "true\n")
}

// TestParityChainedIter verifies `for (x in obj)` when obj.__iter() returns
// another object that itself needs resolving: a list, a generator, and a
// second user object whose __iter yields a generator. Both backends must
// follow the chain to the same sequence (the evaluator previously stopped at
// the first hop and reported "not iterable").
func TestParityChainedIter(t *testing.T) {
	runParity(t, `import io;

class WrapList {
    func WrapList() {}
    func __iter(): any { return [10, 20, 30]; }
}

class Inner {
    func Inner() {}
    func __iter(): any {
        return (func(): generator<int> { yield 1; yield 2; yield 3; })();
    }
}

class Outer {
    Inner inner;
    func Outer() { this.inner = Inner(); }
    func __iter(): any { return this.inner; }
}

for (x in WrapList()) { io.println(x); }
for (x in Outer()) { io.println(x); }
`, "10\n20\n30\n1\n2\n3\n")
}

// TestVMTailCallElimination exercises 1.0.6's OpTailCall path under
// the VM. The evaluator's call-depth limit caps mutual recursion at
// ~10k frames; the VM with TCE collapses the chain into a single
// frame and finishes whatever depth the loop runs to. We assert the
// VM reaches a depth (100_000) the evaluator could not.
func TestVMTailCallElimination(t *testing.T) {
	source := `import io;

func loop(int n, int acc): int {
    if (n == 0) {
        return acc;
    }
    return loop(n - 1, acc + 1);
}

io.println(loop(100000, 0));
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "tce")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if got, want := out.String(), "100000\n"; got != want {
		t.Fatalf("output mismatch: got %q want %q", got, want)
	}
}

// TestParityFuncAsCallable guards that a function value casts to
// `callable` / `func` / `function` on both backends. Pre-1.0.3 the
// evaluator's castValue rejected the cast with "cannot cast func
// to callable" while the VM accepted it (via the value's TypeName
// being "func", which matches the target). Both backends now
// route through `runtime.IsCallableValue` for the callable family.
func TestParityFuncAsCallable(t *testing.T) {
	runParity(t, `import io;

let f = func(int n): int { return n + 1; };
let c = f as callable;
io.println(c(5));

any g = func(string s): string { return s + "!"; };
let c2 = g as callable;
io.println(c2("hi"));
`, "6\nhi!\n")
}

// TestParityLocalShadowsBuiltinModule guards a regression where a
// `name.method(...)` call against a local variable whose identifier
// happens to match a built-in stdlib module would dispatch to the
// module instead of invoking the method on the local. Lexical scope
// wins: a local in scope is checked first.
func TestParityLocalShadowsBuiltinModule(t *testing.T) {
	runParity(t, `import io;
import errors;

func f(): int {
    let errors = [1, 2, 3];
    errors = errors.push(4);
    return errors.length();
}
io.println(f());
`, "4\n")
}

// TestParityBareReturnInVoidFunction guards an analyzer regression
// where a bare `return;` inside a function declared as returning
// `void` raised "cannot return null from F returning void". Early
// exits should be legal: there is no value being returned, only an
// early termination of the body. Surfaced while wiring @Assert
// validation through the Gebweb dispatch path.
func TestParityBareReturnInVoidFunction(t *testing.T) {
	runParity(t, `import io;

func early(int n): void {
    if (n < 0) {
        return;
    }
    io.println(n);
}

early(-5);
early(7);
io.println("done");
`, "7\ndone\n")
}

// TestParitySingleOverloadMethodDispatch guards an `OpAdd`-style
// fast-path on the VM's `selectRuntimeFunction` for methods that
// have exactly one declared overload. Most user classes hit this
// case on every dispatch (50000+ times on the `class_dispatch`
// benchmark); the fast path skips the matches-slice allocation +
// the post-loop "ambiguous overload" check. Behaviour is unchanged.
func TestParitySingleOverloadMethodDispatch(t *testing.T) {
	runParity(t, `import io;

class Counter {
    int value;
    func Counter(int start) { this.value = start; }
    func step(int delta): int {
        this.value = this.value + delta;
        return this.value;
    }
}

let c = Counter(0);
io.println(c.step(1));
io.println(c.step(5));
io.println(c.step(-2));

/* Single-overload methods on inheriting classes still dispatch
 * correctly; the parent's method runs when the child doesn't
 * redeclare it. */
class Base { func name(): string { return "base"; } }
class Child extends Base { }
io.println(Child().name());
`, "1\n6\n4\nbase\n")
}

// TestParityMethodLookupCache guards the single-slot method-lookup
// cache the VM uses to skip the `classInfo.Methods` map access on
// the second-and-later dispatches to the same method on the same
// class. A tight loop calling `Counter.step` repeatedly hits the
// cache after the first call. Switching to a different method on
// the same class refills the cache; calls to the parent's method
// continue to walk the parent chain on a miss.
func TestParityMethodLookupCache(t *testing.T) {
	runParity(t, `import io;

class Counter {
    int value;
    func Counter() { this.value = 0; }
    func step(int n): int { this.value = this.value + n; return this.value; }
    func double(): int { this.value = this.value * 2; return this.value; }
}

let c = Counter();
for (int i = 0; i < 5; i++) {
    c.step(1);
}
io.println(c.value);
io.println(c.double());
io.println(c.step(10));

class Base {
    func tag(): string { return "base"; }
}
class Child extends Base { }
let ch = Child();
io.println(ch.tag());
io.println(ch.tag());
io.println(ch.tag());
`, "5\n10\n20\nbase\nbase\nbase\n")
}

// TestParityEmptyContainerDefaults guards the lifted compiler parity
// gap: `dict opts = {}`, `list xs = []`, and `set s = set()`-shaped
// parameter defaults now compile directly to bytecode. Empty
// containers go into the constant pool and the VM clones at fill
// time so each call sees a fresh empty container - avoiding the
// Python-style mutable-default shared-state trap.
func TestParityEmptyContainerDefaults(t *testing.T) {
	runParity(t, `import io;

func use_opts(dict<string, any> opts = {}): int { return opts.length(); }
func use_list(list<int> xs = []): int { return xs.length(); }

io.println(use_opts());
io.println(use_opts({"a": 1, "b": 2}));
io.println(use_list());
io.println(use_list([1, 2, 3]));

/* Mutable-default isolation: each call without args sees a fresh
 * empty dict, NOT the same instance accumulating state. */
func incr(dict<string, int> d = {}): int {
    d["n"] = (d["n"] ?? 0) + 1;
    return d["n"];
}
io.println(incr());
io.println(incr());
io.println(incr());
`, "0\n2\n0\n3\n1\n1\n1\n")
}

// TestParityImportAliasDoesNotCollideAcrossFiles guards a VM-only-correct
// regression: the evaluator kept a process-wide `importNames` map that
// recorded the LAST `import X as Y` to use alias `Y`. Two files that both
// used the same alias for different canonical modules (e.g. a user file
// `import web.websocket as websocket;` while stdlib `import websocket;`
// keeps the native) collided - whichever import ran last won, and stdlib
// code that wanted the native ended up dispatching against the user
// module, surfacing as "module websocket has no export upgrade".
// The VM was already correct because each compiled chunk owns its own
// globals; the evaluator now consults the env-local Module's
// `Canonical` field first and only falls back to the shared map.
func TestParityImportAliasDoesNotCollideAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	donorModule := filepath.Join(dir, "alias_donor.gb")
	/* The donor module has its own `import websocket;` that should
	 * resolve to the NATIVE websocket regardless of what aliases
	 * the caller registers. We can't call native upgrade() from
	 * the donor scope without actually upgrading, so the donor
	 * exposes a tiny shim that calls a function we KNOW only the
	 * native module exports (`websocket.upgrade` returns a dict
	 * with a `websocket` key). */
	if err := os.WriteFile(donorModule, []byte(`module alias_donor;

import websocket;

export func makeUpgrade(callable handler): dict<string, any> {
    return websocket.upgrade(handler);
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	source := `import io;
import alias_donor as donor;
/* Alias web.websocket to the same identifier the donor's
 * import websocket uses. The donor must continue to resolve to the
 * native websocket regardless of this user-side alias. */
import web.websocket as websocket;

let r = donor.makeUpgrade(func(any conn): void { });
io.println(r.contains("websocket"));
/* Plus verify our local alias still resolves to web.websocket: the
 * wrapped version also returns a dict containing "websocket". */
let local = websocket.upgrade(func(any conn): void { });
io.println(local.contains("websocket"));
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "true\ntrue\n"

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
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// TestParityCrossModuleImplements guards a VM-only regression where the
// bytecode compiler rejected `class C implements mod.Iface { ... }` for
// any interface declared in a different module - including
// `gebweb.repository.Repository<T>`, the canonical case that motivated
// the fix. The evaluator's `resolveTypeValue` already walked imports;
// the compiler's local-only `c.interfaces` lookup did not, mirroring
// the parent-class case that was already allowed at compiler.go:891.
// Verifies `instanceof` matches against the dotted name AND against the
// trailing identifier on both backends.
func TestParityCrossModuleImplements(t *testing.T) {
	dir := t.TempDir()
	donorModule := filepath.Join(dir, "iface_donor.gb")
	if err := os.WriteFile(donorModule, []byte(`module iface_donor;

export interface Pingable {
    func ping(): string;
}

export interface Countable {
    func count(): int;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	source := `import io;
import iface_donor as donor;

class Pinger implements donor.Pingable {
    func ping(): string { return "ok"; }
}

class Tally implements donor.Pingable, donor.Countable {
    int n;
    func Tally(int start) { this.n = start; }
    func ping(): string { return "tally"; }
    func count(): int { this.n = this.n + 1; return this.n; }
}

let p = Pinger();
io.println(p.ping());
io.println(p instanceof donor.Pingable);
io.println(p instanceof Pingable);
io.println(p instanceof donor.Countable);

let t = Tally(0);
io.println(t.ping());
io.println(t.count());
io.println(t instanceof donor.Pingable);
io.println(t instanceof donor.Countable);
`

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	want := "ok\ntrue\ntrue\nfalse\ntally\n1\ntrue\ntrue\n"

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
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// TestParityCallResolvedMethod exercises the OpCallResolvedMethod
// specialised opcode the compiler emits when the receiver's class is
// statically known and the method resolves to a single non-decorated
// overload with no subclass overrides.
func TestParityCallResolvedMethod(t *testing.T) {
	runParity(t, `import io;

class Counter {
    int value;
    func Counter(int start) {
        this.value = start;
    }
    func step(int delta): int {
        this.value = this.value + delta;
        return this.value;
    }
    func double(): int {
        this.value = this.value * 2;
        return this.value;
    }
}

let c = Counter(10);
io.println(c.step(5));    /* 15 */
io.println(c.step(7));    /* 22 */
io.println(c.double());   /* 44 */
io.println(c.step(-4));   /* 40 */
`, "15\n22\n44\n40\n")
}

func TestParityCSVCustomDelimiter(t *testing.T) {
	runParity(t, `import io;
import csv;

let text = "a;b;c\n1;2;3";
let rows = csv.parse(text, {"delimiter": ";"});
io.println(rows.length);
io.println(rows[1][2]);
`, "2\n3\n")
}

func TestParityCSVStringifyDicts(t *testing.T) {
	runParity(t, `import io;
import csv;

let records = [{"a": 1, "b": 2}, {"a": 3, "b": 4}];
let text = csv.stringify(records);
io.println(text);
`, "a,b\n1,2\n3,4\n\n")
}

// @dataclass synthesizes constructor, value __eq, __string, and with(); frozen composes with immutability.
func TestParityDataclass(t *testing.T) {
	runParity(t, `import io;
@dataclass
class Point { int x; int y; }
let p = Point(1, 2);
io.println(p);
io.println(p == Point(1, 2));
io.println(p == Point(9, 2));
io.println(p.with({"y": 5}));
io.println(p == p.with({"x": 1}));
`, "Point(x=1, y=2)\ntrue\nfalse\nPoint(x=1, y=5)\ntrue\n")

	// Field default becomes an optional constructor parameter.
	runParity(t, `import io;
@dataclass
class Tag { string name; int weight = 1; }
io.println(Tag("a"));
io.println(Tag("b", 3));
`, "Tag(name=a, weight=1)\nTag(name=b, weight=3)\n")

	// frozen dataclass: a post-construction write throws on both backends.
	runParity(t, `import io;
@dataclass(frozen: true)
class Money { int cents; }
let m = Money(100);
try { m.cents = 5; io.println("MUTATED"); } catch (Error e) { io.println("frozen"); }
io.println(m == Money(100));
`, "frozen\ntrue\n")

	// A user-defined __string wins over the generated one.
	runParity(t, `import io;
@dataclass
class Box { int n; func __string(): string { return "box#" + (this.n as string); } }
io.println(Box(7));
io.println(Box(7) == Box(7));
`, "box#7\ntrue\n")

	// A frozen dataclass instance keys dicts/sets by value; a non-frozen one
	// keys by identity. Both backends must agree.
	runParity(t, `import io;
@dataclass(frozen: true)
class P { int x; int y; }
let s = {P(1, 2), P(3, 4), P(1, 2)};
io.println(s.length());
io.println(s.contains(P(1, 2)));
let d = {};
d[P(1, 2)] = "a";
d[P(1, 2)] = "b";
io.println(d.length());
io.println(d[P(1, 2)]);
@dataclass
class M { int x; }
io.println({M(1), M(1)}.length());
`, "2\ntrue\n1\nb\n2\n")
}

func TestParityStreamsMemoryReadLine(t *testing.T) {
	runParityWithStdlib(t, `import io;
import streams;

let mem = streams.memory("alpha\nbeta\ngamma\n");
io.println(mem.readLine());
io.println(mem.readLine());
io.println(mem.readLine());
io.println(mem.readLine());
`, "alpha\nbeta\ngamma\nnull\n")
}

// TestParityWatchStartStopFires exercises the F5 watch.start /
// watch.stop callback path. The fsnotify watcher fires the event
// callback on a goroutine; the eval-side child evaluator and the
// VM-side wrap-bridge both have to propagate the module-level
// `kinds` mutation back to the parent before the assertion reads it.
// watch.stop waits for the dispatch goroutine to drain so the read
// happens-after the last callback write.
func TestParityWatchStartStopFires(t *testing.T) {
	runParityWithStdlib(t, `import io;
import watch;
import sys;
import path;

let p = path.join(sys.tmpdir(), "geb_parity_watch.txt");
io.writeText(p, "v1");

list<string> kinds = [];
let h = watch.start(p, func(dict<string, any> e): void {
    kinds = kinds.push(e["type"] as string);
});

sys.sleep(50);
io.writeText(p, "v2");
sys.sleep(150);

watch.stop(h);
io.remove(p);
io.println(kinds.contains("write"));
`, "true\n")
}

// TestParityProcSpawnEcho exercises F4 subprocess streaming on
// both backends: spawn echo, read stdout to EOF, wait for exit.
// Reuses the IOStream wrapper from F3 - proc.spawn returns a
// process whose stdout/stderr/stdin are IOStream-shaped.
func TestParityProcSpawnEcho(t *testing.T) {
	runParityWithStdlib(t, `import io;
import proc;

let p = proc.spawn("echo", ["hello", "world"]);
let out = p.stdout.readAll();
let code = p.wait();
io.print(out);
io.println(code);
`, "hello world\n0\n")
}

// TestParitySocketsEchoRoundTrip exercises the F3-shaped sockets
// stdlib wrapper on both backends: a sockets.serve handler receives
// a Socket, the client dials, writes a line, and reads back the
// echo. Server.close drains the accept goroutine so the read on
// the parent goroutine happens-after the last callback write.
func TestParitySocketsEchoRoundTrip(t *testing.T) {
	runParityWithStdlib(t, `import io;
import sockets;
import sys;

list<string> received = [];
let server = sockets.serve("127.0.0.1", 0, func(sockets.Socket conn): void {
    for (line in conn) {
        received = received.push(line as string);
        conn.writeln("echo: " + (line as string));
    }
    conn.close();
});

let port = (server.localAddr().split(":")[1] as string) as int;
let client = sockets.dial("127.0.0.1", port);
client.writeln("ping");
let reply = client.readLine();
client.close();
sys.sleep(100 as int);
server.close();

io.println(reply);
io.println(received.length());
`, "echo: ping\n1\n")
}

func TestParitySSHExec(t *testing.T) {
	srv := startSSHTestServer(t)
	defer srv.stop()
	runParityWithStdlib(t, fmt.Sprintf(`import io;
import ssh;

let c = ssh.connect("alice@127.0.0.1", {
    "port": %s,
    "password": "secret",
    "insecureSkipHostKey": true,
});
let r = c.exec("echo hello");
io.print(r.stdout);
io.println(r.exitCode);
c.close();
`, srv.port()), "hello\n0\n")
}

func TestParitySSHSpawnEcho(t *testing.T) {
	srv := startSSHTestServer(t)
	defer srv.stop()
	runParityWithStdlib(t, fmt.Sprintf(`import io;
import ssh;

let c = ssh.connect("alice@127.0.0.1", {
    "port": %s,
    "password": "secret",
    "insecureSkipHostKey": true,
});
let s = c.spawn("cat");
s.stdin.write("ping\n");
s.stdin.close();
io.print(s.stdout.readAll());
io.println(s.wait());
c.close();
`, srv.port()), "ping\n0\n")
}

func TestParityHttpStreamingBody(t *testing.T) {
	runParityWithStdlib(t, `import io;
import http;
import streams;
import sys;

let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "got: " + (req["body"] as string)};
});
let port = (http.serverAddr(server).split(":")[1] as string) as int;
sys.sleep(20 as int);

let body = streams.memory("streamed-payload");
let r = http.post("http://127.0.0.1:" + (port as string) + "/u", body);
io.println(r["status"]);
io.println(r["body"]);
http.shutdown(server);
`, "200\ngot: streamed-payload\n")
}

// TestParityPCREBasic exercises the pcre module's PHP-compatible
// regex engine. Most patterns here also work with re/RE2; the
// PCRE-only features are covered by the lookahead / lookbehind /
// backref tests below.
func TestParityPCREBasic(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.test("\\d+", "abc123") as string);
io.println(pcre.test("\\d+", "abc") as string);
io.println(pcre.find("[A-Z]+", "hello WORLD") ?? "null");
let all = pcre.findAll("\\d+", "1 two 3 four 56");
for (let i = 0; i < all.length(); i = i + 1) {
    io.println(all[i] as string);
}
`, "true\nfalse\nWORLD\n1\n3\n56\n")
}

// TestParityPCRELookahead verifies PCRE-only lookahead assertions,
// which RE2 does not support.
func TestParityPCRELookahead(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.find("\\w+(?=ing\\b)", "swimming and running") ?? "null");
io.println(pcre.test("foo(?!bar)", "foobaz") as string);
io.println(pcre.test("foo(?!bar)", "foobar") as string);
`, "swimm\ntrue\nfalse\n")
}

// TestParityPCRELookbehind verifies PCRE-only lookbehind, which
// RE2 does not support.
func TestParityPCRELookbehind(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.find("(?<=\\$)\\d+", "price is $42 plus tax") ?? "null");
`, "42\n")
}

// TestParityPCREBackref verifies backreferences in the pattern,
// which RE2 does not support.
func TestParityPCREBackref(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.test("(\\w+)\\s+\\1", "hello hello") as string);
io.println(pcre.test("(\\w+)\\s+\\1", "hello world") as string);
`, "true\nfalse\n")
}

// TestParityPCREFlags verifies the imsx modifier letters.
func TestParityPCREFlags(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.test("hello", "HELLO", "i") as string);
io.println(pcre.test("hello", "HELLO", "") as string);
io.println(pcre.find("a.b", "a\nb", "s") ?? "null");
io.println(pcre.find("^bar", "foo\nbar\nbaz", "m") ?? "null");
`, "true\nfalse\na\nb\nbar\n")
}

// TestParityPCREReplaceBackref verifies $1 / $2 backref expansion
// in replacements.
func TestParityPCREReplaceBackref(t *testing.T) {
	runParity(t, `import pcre;
import io;
io.println(pcre.replace("(\\w+) (\\w+)", "$2 $1", "hello world"));
io.println(pcre.replace("(\\d+)", "[$1]", "x=42 y=99"));
`, "world hello\nx=[42] y=[99]\n")
}

// TestParityPCRESplitAndQuote verifies pcre.split and pcre.quote.
func TestParityPCRESplitAndQuote(t *testing.T) {
	runParity(t, `import pcre;
import io;
let parts = pcre.split("\\s*,\\s*", "a, b ,c,  d");
for (let i = 0; i < parts.length(); i = i + 1) {
    io.println(parts[i] as string);
}
io.println(pcre.quote("a.b+c"));
`, "a\nb\nc\nd\na\\.b\\+c\n")
}

// TestParityPCREBadFlag verifies unknown flag letters error out
// on both backends rather than getting silently dropped.
func TestParityPCREBadFlag(t *testing.T) {
	runErrorParity(t, `import pcre;
pcre.test("foo", "foobar", "q");
`, "unknown pcre flag")
}

// TestParityTestMock verifies test.mock works identically on
// both engines: a patched stdlib function dispatches to the
// user-supplied callable rather than the real native.
// Uses runParityWithStdlib because test.mock dispatches through
// the evaluator's stateful native bridge.
func TestParityTestMock(t *testing.T) {
	runParityWithStdlib(t, `import test;
import crypt;
import io;
test.mock("crypt", {"sha256": func(string s): string { return "mocked-" + s; }});
io.println(crypt.sha256("hello"));
test.restoreAll();
let real = crypt.sha256("hello");
/* Real sha256 of "hello" is well-known; we just confirm it
 * is no longer "mocked-hello" after restoreAll. */
io.println((real != "mocked-hello") as string);
`, "mocked-hello\ntrue\n")
}

// TestParityArchiveZipRoundTrip exercises the archive.zip{Read,Write}
// pair on both backends with the same source: write a two-entry
// archive, read it back, print the names and decoded text. The
// expected output is identical regardless of which engine ran it.
func TestParityArchiveZipRoundTrip(t *testing.T) {
	runParity(t, `import archive;
import bytes;
import io;
let raw = archive.zipWrite([
    {"name": "a.txt", "data": "alpha"},
    {"name": "b.txt", "data": "beta"}
]);
let entries = archive.zipRead(raw);
io.println(entries.length as string);
io.println(entries[0]["name"] as string);
io.println(bytes.toString(entries[0]["data"] as bytes));
io.println(entries[1]["name"] as string);
io.println(bytes.toString(entries[1]["data"] as bytes));
`, "2\na.txt\nalpha\nb.txt\nbeta\n")
}

// TestParityArchiveTarGzRoundTrip covers the gzip-wrapped tar
// helpers; tar writers sort by name for determinism so the entry
// order is stable across backends.
func TestParityArchiveTarGzRoundTrip(t *testing.T) {
	runParity(t, `import archive;
import bytes;
import io;
let raw = archive.tarGzWrite([
    {"name": "second", "data": "two"},
    {"name": "first", "data": "one"}
]);
let entries = archive.tarGzRead(raw);
io.println(entries[0]["name"] as string);
io.println(bytes.toString(entries[0]["data"] as bytes));
io.println(entries[1]["name"] as string);
io.println(bytes.toString(entries[1]["data"] as bytes));
`, "first\none\nsecond\ntwo\n")
}

// Regression: cross-module facade `class X extends mod.X` failed in the
// evaluator at construction because parent() routed through
// applyOverloadedFunction with a label matching this.Class.Name.
func TestParityFacadeSubclassSameName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Tenant {
    string id;
    string label;
    func Tenant(string id, string label = "") {
        this.id = id;
        this.label = label;
    }
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "facade.gb"), []byte(`module facade;
import base as basemod;
export class Tenant extends basemod.Tenant {
    func Tenant(string id, string label = "") {
        parent(id, label);
    }
}
`), 0o644); err != nil {
		t.Fatalf("write facade: %v", err)
	}
	source := `import io;
import facade;
let t = facade.Tenant("acme");
let u = facade.Tenant("beta", "Beta Co");
io.println(t.id);
io.println(u.label);
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: %v", err)
	}
	if evOut.String() != "acme\nBeta Co\n" {
		t.Fatalf("evaluator output: %q", evOut.String())
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
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != "acme\nBeta Co\n" {
		t.Fatalf("vm output: %q", vmOut.String())
	}
}

// Regression: both backends must reject `import X; func X(...)`.
func TestParityImportNameCollisionWithFunction(t *testing.T) {
	source := `import secrets;
func secrets(): string { return "x"; }
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	_, vmErr := bytecode.Compile(program, []byte(source), "parity")
	if vmErr == nil {
		t.Fatalf("vm compile: expected name-collision error")
	}
	if !strings.Contains(vmErr.Error(), "already declared") {
		t.Fatalf("vm compile error should mention already-declared: %q", vmErr.Error())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgs(&evOut, nil)
	_, evErr := ev.Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected name-collision error")
	}
	if !strings.Contains(evErr.Error(), "already declared") {
		t.Fatalf("evaluator error should mention already-declared: %q", evErr.Error())
	}
}

// Both backends must agree on top-level redeclaration: only func+func
// (overloads), idempotent re-import of one module, and re-bind after `del`
// are allowed; every other same-name declaration is rejected. Mirrors the
// evaluator's Environment.Define/DefineFunction. Reject cases are tested at
// the imported-module level (the evaluator gates a main program's value
// redeclarations through the semantic analyzer, but module loading on both
// backends goes straight to Define/the compiler - the surface gebweb hit).
func TestParityGlobalRedeclarationRule(t *testing.T) {
	reject := map[string]string{
		"var_var":               "let x = 1;\nlet x = 2;\n",
		"const_func":            "const C = 5;\nexport func C(): int { return 1; }\n",
		"enum_class":            "export enum E { A }\nexport class E {}\n",
		"enum_enum":             "export enum E { A }\nexport enum E { B }\n",
		"interface_func":        "export interface I { func a(): int; }\nexport func I(): int { return 1; }\n",
		"import_class":          "import secrets;\nexport class secrets {}\n",
		"import_var":            "import secrets;\nlet secrets = 5;\n",
		"fromimport_var":        "from math import abs;\nlet abs = 5;\n",
		"fromimport_func":       "from math import abs;\nexport func abs(): int { return 1; }\n",
		"fromimport_func_first": "export func abs(): int { return 1; }\nfrom math import abs;\n",
	}
	for name, body := range reject {
		t.Run("reject_"+name, func(t *testing.T) {
			assertImportedModuleRejected(t, body)
		})
	}
	accept := map[string]struct{ src, want string }{
		"func_overload":         {"import io;\nfunc f(int x): int { return x; }\nfunc f(string s): int { return s.length(); }\nio.println(f(3));\nio.println(f(\"ab\"));\n", "3\n2\n"},
		"idempotent_reimport":   {"import io;\nimport math;\nimport math;\nio.println(math.abs(-2));\n", "2\n"},
		"del_then_rebind":       {"import io;\nlet x = 1;\ndel x;\nlet x = 2;\nio.println(x);\n", "2\n"},
		"fromimport_used":       {"import io;\nfrom math import abs;\nio.println(abs(-3));\n", "3\n"},
		"fromimport_idempotent": {"import io;\nfrom math import abs;\nfrom math import abs;\nio.println(abs(-4));\n", "4\n"},
	}
	for name, tc := range accept {
		t.Run("accept_"+name, func(t *testing.T) {
			runParity(t, tc.src, tc.want)
		})
	}
}

// A native module function can be referenced as a first-class value (with
// the module imported) and passed as a callback, on both backends.
func TestParityNativeModuleFnAsValue(t *testing.T) {
	runParity(t, `import io;
import math;
let g = math.abs;
io.println(g(-7));
io.println([-3, 1, -2].map(math.abs));
`, "7\n[3, 1, 2]\n")
}

// Multi-value returns (`return a, b`) and unpacking (`let a, b = f()`,
// `a, b = b, a`) behave identically on both backends.
func TestParityMultiReturn(t *testing.T) {
	runParity(t, `import io;
func ends(list<int> xs): list<int> { return xs.get(0), xs.get(xs.length() - 1); }
let a, b = ends([3, 1, 4, 1, 5]);
io.println("${a} ${b}");
let x, y = 1, 2;
x, y = y, x;
io.println("${x} ${y}");
func mixed(): list<any> { return 1, "z", true; }
let p, q, r = mixed();
io.println("${p} ${q} ${r}");
`, "3 5\n2 1\n1 z true\n")
}

// A `const` parameter is frozen on entry: mutating it raises ImmutableError
// and the caller's value is untouched; reads still work. Identical on both
// backends.
func TestParityConstParam(t *testing.T) {
	runParity(t, `import io;
func mutate(const list<int> xs): void { xs.append(9); }
let a = [1, 2, 3];
let blocked = false;
try { mutate(a); } catch (ImmutableError e) { blocked = true; }
io.println(blocked);
io.println(a);
func readIt(const list<int> xs): int { return xs.length(); }
io.println(readIt([5, 6, 7]));
func mutateFree(list<int> xs): void { xs.append(9); }
let b = [1, 2, 3];
mutateFree(b);
io.println(b);
`, "true\n[1, 2, 3]\n3\n[1, 2, 3, 9]\n")
}

// clone.deep / deepCopy() produce independent deep copies, and dict.copy()
// preserves insertion order - identically on both backends.
func TestParityDeepCopy(t *testing.T) {
	runParity(t, `import io;
import clone;
let a = [[1], [2]];
let b = clone.deep(a);
b[0].append(9);
io.println(a);
io.println(b);
let c = a.deepCopy();
c[0].append(7);
io.println(a);
io.println(c);
let d = {"z": 1, "a": 2, "m": 3};
io.println(d.copy());
io.println(d.deepCopy());
`, "[[1], [2]]\n[[1, 9], [2]]\n[[1], [2]]\n[[1, 7], [2]]\n{\"z\": 1, \"a\": 2, \"m\": 3}\n{\"z\": 1, \"a\": 2, \"m\": 3}\n")
}

// clone.deep deep-copies user objects: mutating the copy leaves the original
// untouched, on both backends.
func TestParityCloneDeepObject(t *testing.T) {
	runParity(t, `import io;
import clone;
class Box { int v; list<int> xs; func Box() { this.v = 1; this.xs = [1, 2]; } }
let b = Box();
let c = clone.deep(b);
c.v = 99;
c.xs.append(3);
io.println("${b.v} ${b.xs}");
io.println("${c.v} ${c.xs}");
`, "1 [1, 2]\n99 [1, 2, 3]\n")
}

// The `in` membership operator: list/dict/set/string/range + __contains,
// identical on both backends.
func TestParityInOperator(t *testing.T) {
	runParity(t, `import io;
io.println(2 in [1, 2, 3]);
io.println(9 in [1, 2, 3]);
io.println("m" in {"m": 1, "n": 2});
io.println("x" in {"m": 1});
io.println("ell" in "hello");
io.println(3 in (1..5));
io.println(!(9 in [1, 2, 3]));
class Bag {
    dict<string, any> d;
    func Bag() { this.d = {}; }
    func __setIndex(string k, any v): void { this.d.set(k, v); }
    func __contains(string k): bool { return this.d.hasKey(k); }
}
let b = Bag();
b["a"] = 1;
io.println("a" in b);
io.println("z" in b);
`, "true\nfalse\ntrue\nfalse\ntrue\ntrue\ntrue\ntrue\nfalse\n")
}

// time.stopwatch.Stopwatch is a pure stdlib class; assert its structural
// invariants (deterministic booleans) match on both backends.
func TestParityStopwatch(t *testing.T) {
	runParityWithStdlib(t, `import io;
import time;
import time.stopwatch as sw;
let s = sw.Stopwatch();
io.println(s.elapsed() >= 0);
let lap = s.lap();
io.println(lap >= 0);
io.println(s.elapsedFloat() >= 0.0f);
s.reset();
io.println(s.elapsed() >= 0);
`, "true\ntrue\ntrue\ntrue\n")
}

// An invalid \u{...} escape is rejected at parse time (shared lexer/parser,
// so both backends fail identically) in plain and interpolated strings.
func TestParityInvalidUnicodeEscapeRejected(t *testing.T) {
	for _, src := range []string{
		"import io;\nio.println(\"\\u{110000}\");\n",
		"import io;\nio.println(\"\\u{D800}\");\n",
		"import io;\nlet x = 1;\nio.println(\"v \\u{} ${x}\");\n",
	} {
		if _, _, _, _, compileErr := fuzzRunBoth(src); compileErr == nil {
			t.Fatalf("expected invalid unicode escape to be rejected:\n%s", src)
		}
	}
}

// Type-conversion surface: string.codePoints, bytes.toList, bytes.fromList
// must produce identical results on both backends.
func TestParityTypeConversions(t *testing.T) {
	runParity(t, `import io;
import bytes;
io.println("abc".codePoints());
io.println(("abc" as bytes).toList());
io.println(bytes.fromList([97, 98, 99]) as string);
io.println(bytes.fromList(("hi" as bytes).toList()) as string);
`, "[97, 98, 99]\n[97, 98, 99]\nabc\nhi\n")
}

// del operates on variables; deleting a class/func/enum/interface
// declaration is rejected identically on both backends. (Variable and
// instance del, and del+rebind, are covered by TestParityDelClearsBinding
// and TestParityDelFiresDestructor.)
func TestParityDelRejectsDeclarations(t *testing.T) {
	cases := map[string]string{
		"class":     "export class C {}\ndel C;\n",
		"func":      "export func f(): int { return 1; }\ndel f;\n",
		"enum":      "export enum E { A }\ndel E;\n",
		"interface": "export interface I { func a(): int; }\ndel I;\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) { assertImportedModuleRejected(t, body) })
	}
}

func TestParityStoreFunctional(t *testing.T) {
	runParityWithStdlib(t, `
import store;
import io;
let s = store.new();
store.set(s, "a", 1);
store.incr(s, "a", 5);
io.println(store.get(s, "a"));
io.println(store.has(s, "a"));
io.println(store.getOrSet(s, "b", 9));
io.println(store.compareAndSet(s, "a", 6, 10));
io.println(store.compareAndSet(s, "a", 6, 10));
io.println(store.len(s));
io.println(store.keys(s));
`, "6\ntrue\n9\ntrue\nfalse\n2\n[\"a\", \"b\"]\n")
}

func TestParityResponseBody(t *testing.T) {
	runParityWithStdlib(t, `
import http;
import io;
let r = http.response("hello", 200);
io.println(r.body());
io.println(r.body() == r["body"]);
`, "hello\ntrue\n")
}

// TestParityGoroutineId asserts properties (not values, which vary per run): the
// id is positive and stable within a goroutine on both backends.
func TestParityGoroutineId(t *testing.T) {
	runParityWithStdlib(t, `
import sys;
import io;
let a = sys.goroutineId();
io.println(a > 0);
io.println(sys.goroutineId() == a);
`, "true\ntrue\n")
}

func TestParityVectorStore(t *testing.T) {
	runParityWithStdlib(t, `import io;
import vectorstore as vs;
let store = vs.MemoryVectorStore();
store.add("a", [0.1, 0.2, 0.9], {"text": "cats"});
store.add("b", [0.9, 0.1, 0.1], {"text": "cars"});
store.add("c", [0.15, 0.25, 0.85], {"text": "kittens"});
io.println(store.count());
let hits = store.search([0.1, 0.2, 0.8], 2);
io.println(hits.length());
io.println(hits[0].record.id + " " + hits[1].record.id);
io.println(store.get("b").metadata["text"]);
io.println(store.get("z") == null);
io.println(store.delete("a"));
io.println(store.delete("a"));
io.println(store.count());
let dot = vs.MemoryVectorStore("dot");
dot.add("x", [1.0, 0.0], {});
dot.add("y", [0.0, 1.0], {});
io.println(dot.search([2.0, 0.5], 1)[0].record.id);
let euc = vs.MemoryVectorStore("euclidean");
euc.add("near", [1.0, 1.0], {});
euc.add("far", [9.0, 9.0], {});
io.println(euc.search([1.1, 1.1], 1)[0].record.id);
let f = store.searchWhere([0.1, 0.2, 0.8], 5, func(any r): bool {
    return (r as vs.VectorRecord).metadata["text"] == "kittens";
});
io.println((f.length() as string) + " " + f[0].record.id);
`, "3\n2\na c\ncars\ntrue\ntrue\nfalse\n2\nx\nnear\n1 c\n")
}

// SqliteVectorStore: float32-blob persistence, upsert, search, and delete.
func TestParitySqliteVectorStore(t *testing.T) {
	runParityWithStdlib(t, `import db;
import vectorstore as vs;
import io;
let conn = db.connect("sqlite", ":memory:");
let store = vs.SqliteVectorStore(conn);
store.add("cats", [0.1, 0.2, 0.9], {"text": "cats"});
store.add("cars", [0.9, 0.1, 0.1], {"text": "cars"});
store.add("kittens", [0.15, 0.25, 0.85], {"text": "kittens"});
io.println(store.count());
let hits = store.search([0.1, 0.2, 0.8], 2);
io.println(hits[0].record.id + " " + hits[1].record.id);
io.println(store.get("cars").metadata["text"]);
io.println(store.get("z") == null);
store.add("cats", [0.0, 0.0, 1.0], {"text": "updated"});
io.println(store.count());
io.println(store.get("cats").metadata["text"]);
io.println(store.delete("cars"));
io.println(store.delete("cars"));
io.println(store.count());
let f = store.searchWhere([0.1, 0.2, 0.8], 5, func(any r): bool {
    return (r as vs.VectorRecord).metadata["text"] == "kittens";
});
io.println((f.length() as string) + " " + f[0].record.id);
store.clear();
io.println(store.count());
`, "3\ncats kittens\ncars\ntrue\n3\nupdated\ntrue\nfalse\n2\n1 kittens\n0\n")
}

// rag: chunking, index/retrieve over a stub embedder (no network), and context.
func TestParityRag(t *testing.T) {
	runParityWithStdlib(t, `import rag;
import vectorstore as vs;
import io;
class Stub implements rag.Embedder {
    func embed(string text): list<any> {
        let keys = ["cat", "dog", "car"];
        let v = [];
        for (k in keys) {
            if (text.lower().contains(k as string)) { v = v.push(1.0f); } else { v = v.push(0.0f); }
        }
        return v;
    }
}
let cs = rag.chunk("one two three four five six", {"size": 3, "overlap": 1});
io.println(cs.length());
io.println(cs[0]);
let store = vs.MemoryVectorStore();
let emb = Stub();
io.println(rag.index(store, emb, "d1", "the cat sat", {}, {}));
rag.index(store, emb, "d2", "a fast car", {}, {});
rag.index(store, emb, "d3", "the dog ran", {}, {});
io.println(store.count());
let hits = rag.retrieve(store, emb, "find the cat", 1);
io.println(hits[0].record.metadata["text"]);
io.println(rag.context(hits, {}));
io.println(rag.context(hits, {"withSources": false}));
`, "3\none two three\n1\n3\nthe cat sat\n[1] (d1): the cat sat\nthe cat sat\n")
}

// Test framework skip: this.skip(reason) + @Skip decorator, with a separate
// skipped count, agree across the evaluator testRun and the VM RunTestClass.
func TestParityTestSkip(t *testing.T) {
	runParityWithStdlib(t, `import io;
import test;
class SkipParity extends test.Test {
    @test
    func passes(): void { this.assertTrue(true); }
    @test
    func runtimeSkip(): void { this.skip("nope"); this.assertTrue(false); }
    @test
    @Skip("disabled")
    func staticSkip(): void { this.assertTrue(false); }
}
let r = test.run(SkipParity, {});
io.println(r["total"]);
io.println(r["passed"]);
io.println(r["failed"]);
io.println(r["skipped"]);
`, "3\n1\n0\n2\n")
}

// HnswVectorStore over a small, well-separated set (exact regime, so the
// approximate index is deterministic) - CRUD, ranking, and filter pushdown.
func TestParityHnswVectorStore(t *testing.T) {
	runParityWithStdlib(t, `import io;
import vectorstore as vs;
let store = vs.HnswVectorStore("cosine");
store.add("a", [1.0, 0.0, 0.0], {"g": "x"});
store.add("b", [0.0, 1.0, 0.0], {"g": "y"});
store.add("c", [0.0, 0.0, 1.0], {"g": "x"});
io.println(store.count());
io.println(store.search([0.9, 0.1, 0.0], 1)[0].record.id);
io.println(store.get("b").metadata["g"]);
io.println(store.get("z") == null);
io.println(store.searchFilter([0.1, 0.1, 0.9], 5, {"g": "x"})[0].record.id);
io.println(store.delete("b"));
io.println(store.delete("b"));
io.println(store.count());
store.clear();
io.println(store.count());
`, "3\na\ny\ntrue\nc\ntrue\nfalse\n2\n0\n")
}

// Aliased io.print/println must not underflow the VM stack.
func TestParityAliasedIoPrint(t *testing.T) {
	runParity(t, `import io as out;
out.println("via alias");
out.print("no newline");
`, "via alias\nno newline")
}

// sys.bundleDir() returns "" when not running from a bundle, on both backends.
func TestParitySysBundleDir(t *testing.T) {
	t.Setenv("GEBLANG_BUNDLE_DIR", "")
	runParityWithStdlib(t, `import io; import sys;
io.println(sys.bundleDir() == "");
`, "true\n")
}

// Closures created inside a loop: a `let` in the body is a fresh binding per
// iteration (so closures stored and called later see distinct values), while
// the loop variable itself is a single shared binding. Regression guard for a
// VM bug where a closure's `return` emitted the enclosing for-loop's iterator
// close (crash) and loop-body lets shared one cell.
func TestParityClosureCapturesInLoops(t *testing.T) {
	// Body-let captured, closures called after the loop: per-iteration values.
	runParity(t, `import io;
let hs = [];
for (item in ["a", "b", "c"]) {
    let nm = item;
    hs = hs.push(func(): string { return nm; });
}
for (h in hs) { io.println((h as callable)()); }
`, "a\nb\nc\n")

	// Loop variable captured directly: a single shared binding (last value).
	runParity(t, `import io;
let hs = [];
for (item in ["a", "b", "c"]) {
    hs = hs.push(func(): string { return item; });
}
for (h in hs) { io.println((h as callable)()); }
`, "c\nc\nc\n")

	// while-loop body-let: fresh per iteration.
	runParity(t, `import io;
let hs = [];
let i = 0;
while (i < 3) {
    let nm = "n" + (i as string);
    hs = hs.push(func(): string { return nm; });
    i = i + 1;
}
for (h in hs) { io.println((h as callable)()); }
`, "n0\nn1\nn2\n")

	// Closure created and called inside the loop body (no crash).
	runParity(t, `import io;
for (item in ["a", "b"]) {
    let h = func(): string { return item; };
    io.println(h());
}
`, "a\nb\n")

	// Assignment to a captured variable writes through the shared binding.
	runParity(t, `import io;
let x = 1;
let f = func(): int { return x; };
x = 2;
io.println("${f()}");
`, "2\n")
}

// A default value on an @immutable field is rejected by both backends: the VM
// at compile time, the evaluator at class-build (runtime). Both carry the same
// message. Not a runParity case because the rejection points differ by design.
func TestImmutableFieldDefaultRejected(t *testing.T) {
	src := `class U { @immutable int x = 5; func U() {} }
let u = U();
`
	const want = "may not declare a default value"

	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	if _, err := bytecode.Compile(program, []byte(src), "test"); err == nil {
		t.Error("VM: expected compile error for @immutable field default")
	} else if !strings.Contains(err.Error(), want) {
		t.Errorf("VM error missing %q: %v", want, err)
	}

	if _, err := evaluator.New(&bytes.Buffer{}).Eval(program); err == nil {
		t.Error("evaluator: expected error for @immutable field default")
	} else if !strings.Contains(err.Error(), want) {
		t.Errorf("evaluator error missing %q: %v", want, err)
	}
}

// TestParityNDArraySurface pins the ndarray module surface on both
// backends: constructors, views, elementwise ops, reductions, linalg.
func TestParityNDArraySurface(t *testing.T) {
	runParity(t, `import io;
import ndarray as nd;

let a = nd.array([[1.0, 2.0], [3.0, 4.0]]);
io.println(a.shape());
io.println(a.dtype());
io.println(a.sum());
io.println(a.mean());
io.println(a.t().toList());
io.println(a.add(a).toList());
io.println(a.mulScalar(10).toList());
io.println(a.matmul(nd.eye(2)).toList());
let m = a.gt(2.0);
io.println(m.toList());
io.println(a.where(m).toList());
let r = nd.arange(0, 10, 3);
io.println(r.toList());
io.println(r.sum());
io.println(nd.linspace(0.0, 1.0, 5).toList());
let s = nd.solve(nd.array([[2.0, 0.0], [0.0, 4.0]]), nd.array([2.0, 8.0]));
io.println(s.toList());
io.println(nd.det(nd.array([[1.0, 2.0], [3.0, 4.0]])));
io.println(nd.random([2, 2], {"seed": 42}).shape());
io.println(a.slice([[0, 1], [0, 2]]).toList());
io.println(a.sum({"axis": 0}).toList());
io.println(nd.array([1, 2, 3]).dtype());
io.println(nd.array([1, 2, 3]).astype("float64").toList());
`, `[2, 2]
float64
10
2.5
[[1, 3], [2, 4]]
[[2, 4], [6, 8]]
[[10, 20], [30, 40]]
[[1, 2], [3, 4]]
[[0, 0], [1, 1]]
[3, 4]
[0, 3, 6, 9]
18
[0, 0.25, 0.5, 0.75, 1]
[1, 2]
-2
[2, 2]
[[1, 2]]
[4, 6]
int64
[1, 2, 3]
`)
}

// Seeded ndarray.random must be reproducible and identical across backends.
func TestParityNDArrayRandomSeeded(t *testing.T) {
	runParity(t, `import io;
import ndarray as nd;

let a = nd.random([2, 2], {"seed": 7});
let b = nd.random([2, 2], {"seed": 7});
io.println(a.sub(b).sum());
io.println(a.eq(b).sum());
`, "0\n4\n")
}

// TestParityDataFrameSurface pins the dataframe module surface on both
// backends: construction, expressions, verbs, grouping, joins, codecs.
func TestParityDataFrameSurface(t *testing.T) {
	runParity(t, `import io;
import dataframe as df;

let users = df.fromDict({
    "name": ["Ada", "Grace", "Linus", "Ken"],
    "age": [36, 41, null, 55],
    "country": ["UK", "US", "FI", "US"],
    "active": [true, true, false, true]
});
io.println(users.shape());
io.println(users.columns());
io.println(users.dtypes());
io.println(users.col("age").mean());
io.println(users.col("age").isNull().toList());

let adults = users.filter(df.col("age").gt(38).and_(df.col("active").eq(true)));
io.println(adults.col("name").toList());

let derived = users.withColumn("ageMonths", df.col("age").mul(12));
io.println(derived.col("ageMonths").toList());

io.println(users.sort("age", {"desc": true}).col("name").toList());
io.println(users.dropNulls(["age"]).rows());
io.println(users.fillNull("age", 0).col("age").toList());

let byCountry = users.groupBy("country").agg({"age": ["mean", "max"], "name": "count"});
io.println(byCountry.sort("country", {}).toDicts());

let orders = df.fromRecords([
    {"userName": "Ada", "amount": 100},
    {"userName": "Ken", "amount": 50},
    {"userName": "Ada", "amount": 25},
    {"userName": "Zoe", "amount": 70}
]);
let joined = orders.rename({"userName": "name"}).join(users, {"on": "name", "how": "left"});
io.println(joined.shape());
io.println(joined.sort("amount", {}).col("country").toList());

let csvText = users.toCsv();
let back = df.fromCsv(csvText);
io.println(back.dtypes());
io.println(back.col("age").toList());

io.println(df.fromJson("[{\"a\": 1, \"b\": \"x\"}, {\"a\": 2}]").toDicts());
io.println(df.concat([users.head(1), users.tail(1)]).col("name").toList());
io.println(users.describe().col("age").toList());
`, `[4, 4]
["name", "age", "country", "active"]
{"name": "string", "age": "int64", "country": "string", "active": "bool"}
44
[false, false, true, false]
["Grace", "Ken"]
[432, 492, null, 660]
["Ken", "Grace", "Ada", "Linus"]
3
[36, 41, 0, 55]
[{"country": "FI", "age_mean": null, "age_max": null, "name_count": 1}, {"country": "UK", "age_mean": 36, "age_max": 36, "name_count": 1}, {"country": "US", "age_mean": 48, "age_max": 55, "name_count": 2}]
[4, 5]
["UK", "US", null, "UK"]
{"name": "string", "age": "int64", "country": "string", "active": "bool"}
[36, 41, null, 55]
[{"a": 1, "b": "x"}, {"a": 2, "b": null}]
["Ada", "Ken"]
[3, 44, 9.848857801796104, 36, 55]
`)
}

// Top-level code (init/let) can call a function declared later, on both
// backends. Guards evaluator function hoisting matching the VM.
func TestParityTopLevelFunctionHoisting(t *testing.T) {
	runParity(t, `module m;
import io;
init { io.println(greet()); }
export func greet(): string { return "hi"; }
`, "hi\n")

	// Mutually/forward referencing top-level functions called from an init.
	runParity(t, `module m;
import io;
init { io.println("${a()}"); }
func a(): int { return b() + 1; }
func b(): int { return 10; }
`, "11\n")
}

func TestParityVariadicWithDefaults(t *testing.T) {
	runParity(t, `import io;
func f(int a, int b = 10, int ...rest): string { return "${a}|${b}|${rest}"; }
io.println(f(1));
io.println(f(1, 2));
io.println(f(1, 2, 3, 4));
io.println(f(1, b: 5));
io.println(f(...[7, 8, 9]));
let lam = func(int a, int b = 10, int ...rest): string { return "${a}|${b}|${rest}"; };
io.println(lam(1));
io.println(lam(1, 2, 3));
class M {
    func go(int a, int b = 10, int ...rest): string { return "${a}|${b}|${rest}"; }
    static func sgo(int a, int b = 10, int ...rest): string { return "${a}|${b}|${rest}"; }
}
io.println(M().go(1));
io.println(M().go(1, 2, 3));
io.println(M().go(1, b: 6));
io.println(M.sgo(1));
io.println(M.sgo(1, 2, 3));
class K {
    string s;
    func K(int a, int b = 10, int ...rest) { this.s = "${a}|${b}|${rest}"; }
}
io.println(K(1).s);
io.println(K(1, 2, 3).s);
io.println(K(1, b: 7).s);
`, "1|10|[]\n1|2|[]\n1|2|[3, 4]\n1|5|[]\n7|8|[9]\n1|10|[]\n1|2|[3]\n1|10|[]\n1|2|[3]\n1|6|[]\n1|10|[]\n1|2|[3]\n1|10|[]\n1|2|[3]\n1|7|[]\n")
}

func TestParityCrossModuleVariadicWithDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vd.gb"), []byte(`module vd;
export func make(int w, int h = 3, int ...extra): int { return w * h + extra.length(); }
export class Builder {
    func tag(string base, string sep = "-", string ...parts): string {
        if (parts.length() == 0) { return base; }
        return base + sep + parts.join(sep);
    }
}
`), 0o644); err != nil {
		t.Fatalf("write vd: %v", err)
	}

	source := `import io;
import vd;
io.println(vd.make(2));
io.println(vd.make(2, 5));
io.println(vd.make(2, 5, 1, 1));
let b = vd.Builder();
io.println(b.tag("x"));
io.println(b.tag("x", "+", "y", "z"));
`
	want := "6\n10\n12\nx\nx+y+z\n"

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}

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

func TestParitySignalHandlerRunsOnRaise(t *testing.T) {
	runParityWithStdlib(t, `import io;
import sys;
import store;
import time;

let s = store.Store();
s.set("hits", 0);
sys.onSignal("SIGUSR1", func(string name): void {
    s.update("hits", func(any old): any { return (old as int) + 1; });
});
sys.raise("SIGUSR1");
let deadline = time.now() + 5000;
while ((s.get("hits") as int) < 1 && time.now() < deadline) {
    sys.sleep(10);
}
io.println("hits=${s.get("hits")}");
sys.clearSignal("SIGUSR1");
`, "hits=1\n")
}

func TestParityAsAnyWidening(t *testing.T) {
	runParity(t, `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
let d = Dog();
io.println(typeof("x" as any));
io.println(typeof(5 as any));
io.println(typeof(d as any));
io.println(typeof(("y" as any) as string));
io.println(typeof(5 as ?any));
`, `string
int
Dog
string
int
`)
}

func TestParitySumByAverageBySmallInt(t *testing.T) {
	runParity(t, `import io;
import json;
let xs = [1, 2, 3, 4];
io.println(xs.sumBy(func(int n): any { return n; }));
io.println(xs.averageBy(func(int n): any { return n * 2; }));
let rows = json.parse("[{\"v\": 10}, {\"v\": 20}]");
io.println(rows.sumBy(func(any r): any { return (r as dict<string, any>)["v"]; }));
`, `10
5
30
`)
}

func TestParityHtmlModule(t *testing.T) {
	runParity(t, `import html;
import ndarray;
import io;
let doc = html.parse("<html><body><h1 class='t'>Hello</h1><ul><li>a</li><li>b</li></ul><a href='http://x'>L</a></body></html>");
io.println(doc.tag());
io.println(doc.selectFirst("h1.t").text());
io.println(doc.select("li").length());
io.println(doc.selectFirst("a").attr("href"));
io.println(doc.selectFirst("body").children().length());
io.println(doc.selectFirst("h1").parent().tag());
io.println(doc.selectFirst("ul").html());
io.println(dir(doc.selectFirst("h1")));
io.println(dir(ndarray.array([1, 2, 3])).length());
`, "#document\nHello\n2\nhttp://x\n3\nbody\n<li>a</li><li>b</li>\n[\"attr\", \"attrs\", \"children\", \"html\", \"parent\", \"select\", \"selectFirst\", \"tag\", \"text\"]\n44\n")
}

func TestParityGenericNullArg(t *testing.T) {
	// null must bind to a generic T param on both backends (eval was rejecting it)
	runParity(t, `import io;
class Box<T> { any v; func put(T value): void { this.v = value; } func get(): any { return this.v; } }
let b = Box<any>();
b.put(null);
io.println(b.get() == null);
`, "true\n")
	runParity(t, `import io;
class Box<T> { any v; func put(T value): void { this.v = value; } func get(): any { return this.v; } }
let b = Box<any>();
b.put(42);
io.println(b.get());
`, "42\n")
}

func TestParityStringRuneCache(t *testing.T) {
	// ASCII index and substring via rune-index cache.
	runParity(t, `import io;
io.println("hello"[1]);
io.println("hello".substring(1, 4));
io.println("hello".length());
`, "e\nell\n5\n")
	// Multi-byte index and substring.
	runParity(t, `import io;
io.println("aé中z"[2]);
io.println("aé中z".substring(1, 3));
io.println("aé中z".length());
`, "中\né中\n4\n")
	// Repeated scan counting spaces.
	runParity(t, `import io;
let s = "the quick brown fox";
let int c = 0;
for (let int i = 0; i < s.length(); i++) {
    if (s[i] == " ") { c = c + 1; }
}
io.println(c);
`, "3\n")
	// Multi-byte reconstruct via per-rune index.
	runParity(t, `import io;
let s = "aé中z";
let string r = "";
for (let int i = 0; i < s.length(); i++) {
    r = r + s[i];
}
	io.println(r);
	`, "aé中z\n")
	// Exercise both sides of the short-string threshold and the cached Unicode path.
	runParity(t, `import io;
import string;
let short = "a".repeat(256);
let long = "b".repeat(257);
let pair = string.fromCodePoints([233, 20013]);
let unicode = pair.repeat(200);
io.println(short.length());
io.println(short[255]);
io.println(long.length());
io.println(long.substring(127, 130));
io.println(unicode.length());
io.println(unicode[201] == string.fromCodePoint(20013));
io.println(unicode.substring(200, 202) == pair);
`, "256\na\n257\nbbb\n400\ntrue\ntrue\n")
}

// TestParityClosureCaptureViaInterpolation: an outer local used only inside ${...} must still be captured.
func TestParityClosureCaptureViaInterpolation(t *testing.T) {
	// Referenced only via ${f}, closure has a parameter (was silently reading the param slot).
	runParity(t, `import io;
func outer() {
    let f = 10;
    let cb = func(int x): void {
        io.println("only ${f}");
        io.println("${f + x}");
    };
    cb(3);
}
outer();
`, "only 10\n13\n")

	// Referenced only via ${f}, closure has no parameters (was "local is undefined").
	runParity(t, `import io;
func outer() {
    let f = 10;
    let cb = func(): void {
        io.println("only ${f}");
    };
    cb();
}
outer();
`, "only 10\n")

	// Nested closures: each level captures the same outer local only through interpolation.
	runParity(t, `import io;
func outer() {
    let f = 10;
    let mk = func(): callable {
        return func(): void { io.println("nested ${f}"); };
    };
    let inner = mk();
    inner();
}
outer();
`, "nested 10\n")

	// FormattedInterpolation (${expr:spec}) form.
	runParity(t, `import io;
func outer() {
    let pi = 3.14159;
    let cb = func(): void { io.println("${pi:.2f}"); };
    cb();
}
outer();
`, "3.14\n")

	// Closure stored and invoked later: the capture must survive past the defining call's return.
	runParity(t, `import io;
func makeGreeter(): callable {
    let name = "World";
    return func(): void { io.println("Hello, ${name}!"); };
}
let g = makeGreeter();
g();
`, "Hello, World!\n")
}

// TestParityClosureCaptureViaOtherExprKinds: same scanExpr gap for pipe, partial, and comprehension nodes.
func TestParityClosureCaptureViaOtherExprKinds(t *testing.T) {
	// Pipe: outer local only used as the pipe's left operand.
	runParity(t, `import io;
func double(int x): int { return x * 2; }
func outer() {
    let f = 10;
    let cb = func(): int {
        return f |> double;
    };
    io.println(cb());
}
outer();
`, "20\n")

	// Partial application: outer local only used as a bound argument.
	runParity(t, `import io;
func add(int a, int b): int { return a + b; }
func outer() {
    let f = 10;
    let cb = func(): callable {
        return add(f, _);
    };
    let inc = cb();
    io.println(inc(5));
}
outer();
`, "15\n")

	// List comprehension: outer local only used in the body expression.
	runParity(t, `import io;
func outer() {
    let f = 10;
    let cb = func(): list<int> {
        return [f + x for x in range(0, 3)];
    };
    io.println(cb());
}
outer();
`, "[10, 11, 12, 13]\n")

	// Set comprehension.
	runParity(t, `import io;
func outer() {
    let f = 10;
    let cb = func(): set<int> {
        return {f + x for x in range(0, 3)};
    };
    io.println(cb());
}
outer();
`, "set{10, 11, 12, 13}\n")

	// Dict comprehension: outer local only used in the value body.
	runParity(t, `import io;
func outer() {
    let f = 10;
    let cb = func(): dict<int, int> {
        return {x: f + x for x in range(0, 3)};
    };
    io.println(cb());
}
outer();
`, "{0: 10, 1: 11, 2: 12, 3: 13}\n")
}

// TestParityClosureCaptureViaMatchCaseFields: same scanExpr gap in or-pattern Alternates and list-pattern Literal bindings.
func TestParityClosureCaptureViaMatchCaseFields(t *testing.T) {
	// Or-pattern alternate: outer local only used as a `|` alternate.
	runParity(t, `import io;
func outer() {
    let f = 99;
    let cb = func(int v): string {
        return match (v) {
            case 1 | f => "matched";
            default => "no";
        };
    };
    io.println(cb(99));
    io.println(cb(1));
    io.println(cb(2));
}
outer();
`, "matched\nmatched\nno\n")

	// List-pattern literal-equality binding: outer local only used inside a positional literal expression.
	runParity(t, `import io;
func outer() {
    let f = 7;
    let cb = func(list<int> v): string {
        return match (v) {
            case [int x, -f] => "hit ${x}";
            default => "no";
        };
    };
    io.println(cb([1, -7]));
    io.println(cb([1, 5]));
}
outer();
`, "hit 1\nno\n")

	// Same or-pattern alternate case, but as a match statement (not an expression).
	runParity(t, `import io;
func outer() {
    let f = 5;
    let cb = func(int v): void {
        match (v) {
            case 1 | f => io.println("matched");
            default => io.println("no");
        }
    };
    cb(5);
    cb(2);
}
outer();
`, "matched\nno\n")
}

// TestParityClosureCaptureViaDestructuring: scanStatement had no case for DestructuringStatement at all.
func TestParityClosureCaptureViaDestructuring(t *testing.T) {
	// `let` list-destructuring: outer local read only as the RHS value.
	runParity(t, `import io;
func outer() {
    let pair = [1, 2];
    let cb = func(): int {
        let [a, b] = pair;
        return a + b;
    };
    io.println(cb());
}
outer();
`, "3\n")

	// Bracketed destructuring assignment: outer locals written through only inside the closure.
	runParity(t, `import io;
func outer() {
    let a = 100;
    let b = 200;
    let pair = [1, 2];
    let cb = func(): void {
        [a, b] = pair;
    };
    cb();
    io.println(a);
    io.println(b);
}
outer();
`, "1\n2\n")

	// Bare multi-assignment (no brackets): outer locals written through only inside the closure.
	runParity(t, `import io;
func outer() {
    let a = 0;
    let b = 0;
    let cb = func(): void {
        a, b = 5, 6;
    };
    cb();
    io.println(a + b);
}
outer();
`, "11\n")
}
