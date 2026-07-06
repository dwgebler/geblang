package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// uncaughtOnBothBackends runs source on the evaluator and the VM and
// returns each backend's uncaught-error string.
func uncaughtOnBothBackends(t *testing.T, source string) (string, string) {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected an uncaught error, got none (stdout %q)", evOut.String())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "uncaught_parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Fatalf("vm: expected an uncaught error, got none (stdout %q)", vmOut.String())
	}
	return evErr.Error(), vmErr.Error()
}

// uncaughtOnBothBackendsModulesDir runs mainSource against modules (name -> source) on both backends and returns each backend's uncaught-error string.
func uncaughtOnBothBackendsModulesDir(t *testing.T, modules map[string]string, mainSource string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range modules {
		if err := os.WriteFile(filepath.Join(dir, name+".gb"), []byte(body), 0o644); err != nil {
			t.Fatalf("write module %s: %v", name, err)
		}
	}
	p := parser.New(lexer.New(mainSource))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, evErr := ev.Eval(program); evErr != nil {
		vmErr := runModulesDirVMExpectError(t, dir, program, mainSource)
		return evErr.Error(), vmErr
	}
	t.Fatalf("evaluator: expected an uncaught error, got none (stdout %q)", evOut.String())
	return "", ""
}

func runModulesDirVMExpectError(t *testing.T, dir string, program *ast.Program, mainSource string) string {
	t.Helper()
	chunk, err := bytecode.Compile(program, []byte(mainSource), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
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
	vmErr := vm.Run()
	if vmErr == nil {
		t.Fatalf("vm: expected an uncaught error, got none (stdout %q)", vmOut.String())
	}
	return vmErr.Error()
}

// TestUncaughtRenderParity is the byte-identical guard for canonical
// uncaught-error rendering: every corpus program must produce the same
// string on both backends AND match the pinned literal, so the backends
// cannot drift apart or drift together into a wrong shape.
func TestUncaughtRenderParity(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "PrimitiveDispatchThrowAfterCallArg",
			source: `import io;
class Animal { func Animal() {} }
class Dog extends Animal { func Dog() { parent(); } }
class Cat extends Animal { func Cat() { parent(); } }
func makeCat(): any { return Cat(); }
func sneak(list<Animal> xs): void {
    xs.push(makeCat());
}
list<Dog> dogs = [Dog()];
sneak(dogs);
`, // arg evaluation moves the eval's currentLine; the dispatch must re-stamp
			want: `uncaught TypeError: cannot push Cat to list<Dog>
  at sneak (line 7)
  at <top level> (line 10)`,
		},
		{
			name: "NestedThrow",
			source: `import io;
import errors;

func inner(int x): int {
    throw errors.new("ValueError", "boom");
}

func middle(int x): int {
    let r = inner(x);
    return r;
}

io.println(middle(5));
`,
			want: `uncaught ValueError: boom
  at inner (line 5)
  at middle (line 9)
  at <top level> (line 13)`,
		},
		{
			name: "ReturnPositionCall",
			source: `import io;
import errors;

func inner(int x): int {
    throw errors.new("ValueError", "boom");
}

func middle(int x): int {
    return inner(x);
}

io.println(middle(5));
`,
			want: `uncaught ValueError: boom
  at inner (line 5)
  at middle (line 9)
  at <top level> (line 12)`,
		},
		{
			name: "SelfTailRecursion",
			source: `import io;
import errors;

func down(int n): int {
    if (n == 0) {
        throw errors.new("ValueError", "bottom");
    }
    return down(n - 1);
}

io.println(down(1000));
`,
			want: `uncaught ValueError: bottom
  at down (line 6)
  at down (line 8) [x1000]
  at <top level> (line 11)`,
		},
		{
			name: "DivisionFault",
			source: `import io;

func boom(int x): int {
    return 10 / (x - x);
}

func mid(int x): int {
    return boom(x) + 1;
}

io.println(mid(3));
`,
			want: `uncaught RuntimeError: decimal division by zero
  at boom (line 4)
  at mid (line 8)
  at <top level> (line 11)`,
		},
		{
			name: "IndexFault",
			source: `import io;

func pick(list<int> xs): int {
    return xs[9];
}

io.println(pick([1, 2, 3]));
`,
			want: `uncaught RuntimeError: list index out of range
  at pick (line 4)
  at <top level> (line 7)`,
		},
		{
			name: "MethodThrow",
			source: `import io;
import errors;

class Worker {
    func work(int x): int {
        throw errors.new("ValueError", "method boom");
    }
}

let w = Worker();
io.println(w.work(5));
`,
			want: `uncaught ValueError: method boom
  at Worker.work (line 6)
  at <top level> (line 11)`,
		},
		{
			name: "InheritedMethodThrow",
			source: `import io;
import errors;

class Base {
    func boom(int x): int {
        throw errors.new("ValueError", "inherited boom");
    }
}

class Sub extends Base {
}

let s = Sub();
io.println(s.boom(2));
`,
			want: `uncaught ValueError: inherited boom
  at Base.boom (line 6)
  at <top level> (line 14)`,
		},
		{
			name: "ConstructorThrow",
			source: `import io;
import errors;

class Widget {
    func Widget(int n) {
        throw errors.new("ValueError", "ctor boom");
    }
}

let w = Widget(1);
io.println("unreachable");
`,
			want: `uncaught ValueError: ctor boom
  at Widget.Widget (line 6)
  at <top level> (line 10)`,
		},
		{
			name: "StaticMethodThrow",
			source: `import io;
import errors;

class Maths {
    static func boom(int x): int {
        throw errors.new("ValueError", "static boom");
    }
}

io.println(Maths.boom(4));
`,
			want: `uncaught ValueError: static boom
  at Maths.boom (line 6)
  at <top level> (line 10)`,
		},
		{
			name: "DeferredCallThrow",
			source: `import io;
import errors;

func explode() {
    throw errors.new("ValueError", "deferred boom");
}

func run() {
    defer explode();
    io.println("body done");
}

run();
`,
			want: `uncaught ValueError: deferred boom
  at explode (line 5)
  at run (line 9)
  at <top level> (line 13)`,
		},
		{
			name: "GeneratorThrow",
			source: `import io;
import errors;

func gen(): generator<int> {
    yield 1;
    throw errors.new("ValueError", "gen boom");
}

for (v in gen()) {
    io.println(v);
}
`,
			want: `uncaught ValueError: gen boom
  at gen (line 6)
  at <top level>`,
		},
		{
			name: "ClosureThrow",
			source: `import io;
import errors;

let f = func(int x): int {
    throw errors.new("ValueError", "anon");
};

io.println(f(1));
`,
			want: `uncaught ValueError: anon
  at <closure> (line 5)
  at <top level> (line 8)`,
		},
		{
			name: "TopLevelThrow",
			source: `import errors;

throw errors.new("ValueError", "top");
`,
			want: `uncaught ValueError: top
  at <top level> (line 3)`,
		},
		{
			name: "MultiLineArgument",
			source: `import io;
import errors;

func makeArg(int x): int {
    return x + 1;
}

func inner(int x): int {
    throw errors.new("ValueError", "boom");
}

func middle(int x): int {
    let r = inner(
        makeArg(x)
    );
    return r;
}

io.println(middle(5));
`,
			want: `uncaught ValueError: boom
  at inner (line 9)
  at middle (line 13)
  at <top level> (line 19)`,
		},
		{
			name: "MidStackTailCall",
			source: `import io;
import errors;

func g(int x): int {
    throw errors.new("ValueError", "mid boom");
}

func f(int n): int {
    if (n == 0) {
        return g(n);
    }
    return f(n - 1);
}

io.println(f(3));
`,
			want: `uncaught ValueError: mid boom
  at g (line 5)
  at f (line 10)
  at f (line 12) [x3]
  at <top level> (line 15)`,
		},
		{
			name: "MultipleTailFrames",
			source: `import io;
import errors;

func g(int n): int {
    if (n == 0) {
        throw errors.new("ValueError", "deep boom");
    }
    return g(n - 1);
}

func f(int n): int {
    if (n == 0) {
        return g(2);
    }
    return f(n - 1);
}

io.println(f(3));
`,
			want: `uncaught ValueError: deep boom
  at g (line 6)
  at g (line 8) [x2]
  at f (line 13)
  at f (line 15) [x3]
  at <top level> (line 18)`,
		},
		{
			name: "CaughtRethrow",
			source: `import io;
import errors;

func origin() {
    throw errors.new("ValueError", "original");
}

func relay() {
    try {
        origin();
    } catch (ValueError e) {
        throw e;
    }
}

relay();
`,
			want: `uncaught ValueError: original
  at origin (line 5)
  at relay (line 10)
  at <top level> (line 16)`,
		},
		{
			name: "TopLevelDeferredThrow",
			source: `import io;
import errors;

func explode() {
    throw errors.new("ValueError", "deferred top");
}

defer explode();
io.println("main done");
`,
			want: `uncaught ValueError: deferred top
  at explode (line 5)
  at <top level> (line 8)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evGot, vmGot := uncaughtOnBothBackends(t, tc.source)
			if evGot != vmGot {
				t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
			}
			if evGot != tc.want {
				t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, tc.want)
			}
		})
	}
}

// TestUncaughtHofClosureErrorNotDoubled pins byte-identity for an uncaught throw inside a list-HOF closure (formerly the VM doubled the render; the residual top-level line divergence is now closed by the HOF caller-line fix).
func TestUncaughtHofClosureErrorNotDoubled(t *testing.T) {
	src := `import io;
let xs = ["alpha", "beta"];
io.println(xs.map(func(string w): int { return (w as int) + 1; }));
`
	want := `uncaught RuntimeError: invalid integer literal "alpha"
  at <closure> (line 3)
  at <top level> (line 3)`
	evGot, vmGot := uncaughtOnBothBackends(t, src)
	if evGot != vmGot {
		t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
	}
	if evGot != want {
		t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, want)
	}
}
