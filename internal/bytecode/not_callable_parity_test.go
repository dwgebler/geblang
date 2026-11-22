package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestNotCallableMessageParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Plain { int n = 0; }
class Doubler { func __invoke(int x): int { return x * 2; } }
enum Color { Red }

func report(callable f): void {
    try { f(); } catch (Error e) { io.println("${typeof(e)}: ${e.message}"); }
}

let x = 5;
let s = "a";
any v = null;
let t = int;
let p = Plain();
let items = [1, 2];
let c = Color.Red;
report(func(): void { x(); });
report(func(): void { s(1, 2); });
report(func(): void { v(); });
report(func(): void { t(); });
report(func(): void { p(); });
report(func(): void { items[0](); });
report(func(): void { c(); });
report(func(): void { x(a: 1); });
report(func(): void { x(...items); });
io.println(Doubler()(4));
func deferred(): void {
    let n = 1;
    defer n();
}
try { deferred(); } catch (Error e) { io.println("${typeof(e)}: ${e.message}"); }
`)
	want := "RuntimeError: x is not callable\nRuntimeError: s is not callable\nRuntimeError: v is not callable\n" +
		"RuntimeError: t is not callable\nRuntimeError: p is not callable\nRuntimeError: items[0] is not callable\n" +
		"RuntimeError: c is not callable\nRuntimeError: x is not callable\nRuntimeError: x is not callable\n8\n" +
		"RuntimeError: n is not callable\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}

func TestNotCallableMessageCrossModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module nclib;

export int count = 3;
export class Plain { int n = 0; }
export class Doubler { func __invoke(int x): int { return x * 2; } }
`
	if err := os.WriteFile(filepath.Join(dir, "nclib.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import nclib;

class Tripler extends nclib.Doubler {}

let p = nclib.Plain();
try { p(); } catch (Error e) { io.println(e.message); }
try { nclib.count(); } catch (Error e) { io.println(e.message); }
io.println(Tripler()(5));
io.println(nclib.Doubler()(6));
`)
	want := "p is not callable\nnclib.count is not callable\n10\n12\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}

func TestCalleeNamesSurviveChunkRoundTrip(t *testing.T) {
	src := "let x = 1;\nfunc f() {\n    defer x();\n}\nx();\n"
	p := parser.New(lexer.New(src))
	chunk, err := bytecode.Compile(p.ParseProgram(), []byte(src), "roundtrip.gb")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.CalleeNames) != 2 {
		t.Fatalf("compiled CalleeNames = %v, want the value call and the deferred call", chunk.CalleeNames)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.CalleeNames) != len(chunk.CalleeNames) {
		t.Fatalf("decoded CalleeNames = %v, want %v", decoded.CalleeNames, chunk.CalleeNames)
	}
	for key, text := range chunk.CalleeNames {
		if decoded.CalleeNames[key] != text {
			t.Fatalf("decoded CalleeNames[%d] = %q, want %q", key, decoded.CalleeNames[key], text)
		}
	}
}
