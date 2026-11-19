package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// arrowOutputs parses separately for each backend so each runs its own arrow inference.
func arrowOutputs(t *testing.T, source string) (string, string, error, error) {
	t.Helper()
	parse := func() *ast.Program {
		p := parser.New(lexer.New(source))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		return program
	}
	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(parse())
	var vmOut bytes.Buffer
	chunk, vmErr := bytecode.Compile(parse(), []byte(source), "parity")
	if vmErr == nil {
		vmErr = bytecode.NewVM(chunk, &vmOut).Run()
	}
	return evOut.String(), vmOut.String(), evErr, vmErr
}

func TestArrowFunctionFormsParity(t *testing.T) {
	ev, vm, evErr, vmErr := arrowOutputs(t, `import io;
import async;

let double = (int n) => n * 2;
io.println(double(21));
let join = (string a, string b) => {
    let joined = a + "-" + b;
    return joined;
};
io.println(join("x", "y"));
let none = () => "none";
io.println(none());
let add = (int a) => (int b) => a + b;
io.println(add(2)(3));
let task = async (int n) => n + 1;
io.println(await task(41));
let untyped = func(int n) { return n * 3; };
io.println(untyped(2));
let m = match (2) {
    case (2) => "two";
    default => "other";
};
io.println(m);
io.println((1 + 2) * 3);
`)
	want := "42\nx-y\nnone\n5\n42\n6\ntwo\n9\n"
	if evErr != nil || vmErr != nil || ev != want || vm != want {
		t.Fatalf("evaluator %q (%v), vm %q (%v), want %q", ev, evErr, vm, vmErr, want)
	}
}

func TestArrowParameterInferenceParity(t *testing.T) {
	ev, vm, evErr, vmErr := arrowOutputs(t, `import io;

let xs = [3, 1, 2];
io.println(xs.map((x) => x * 2));
io.println(xs.filter((x) => x > 1));
io.println(xs.sorted((a, b) => b - a));
io.println(xs.reduce((acc, x) => acc + x, 10));
io.println(xs.scan(0, (acc, x) => acc + x));
io.println(xs.zipWith(["a", "b", "c"], (n, s) => "${n}${s}"));
io.println(xs.map((x) => xs.filter((y) => y < x).length()));
list<string> words = ["pear", "fig"];
io.println(words.sortBy((w) => w.length()));

class Bag {
    list<int> items;
    func Bag(list<int> items) {
        this.items = items;
    }
    func evens(): list<int> {
        return this.items.filter((n) => n % 2 == 0);
    }
}
io.println(Bag([1, 2, 3, 4]).evens());

func total(list<int> values): int {
    return values.reduce((acc, v) => acc + v, 0);
}
io.println(total([5, 6]));
`)
	want := "[6, 2, 4]\n[3, 2]\n[3, 2, 1]\n16\n[0, 3, 4, 6]\n[\"3a\", \"1b\", \"2c\"]\n[2, 0, 1]\n[\"fig\", \"pear\"]\n[2, 4]\n11\n"
	if evErr != nil || vmErr != nil || ev != want || vm != want {
		t.Fatalf("evaluator %q (%v), vm %q (%v), want %q", ev, evErr, vm, vmErr, want)
	}
}

func TestInferredArrowTypeIsEnforcedParity(t *testing.T) {
	ev, vm, evErr, vmErr := arrowOutputs(t, `import io;
let xs = [1, 2];
try {
    xs.reduce((acc, x) => "s", 0);
} catch (TypeError e) {
    io.println("TypeError");
}
`)
	if evErr != nil || vmErr != nil || ev != "TypeError\n" || vm != ev {
		t.Fatalf("evaluator %q (%v), vm %q (%v)", ev, evErr, vm, vmErr)
	}
}

func TestArrowParameterWithoutContextIsRejectedParity(t *testing.T) {
	for _, src := range []string{
		"let f = (x) => x;\n",
		"func apply(func fn): int {\n    return 1;\n}\napply((x) => x);\n",
		"let d = {\"a\": 1};\nd.map((x) => x);\n",
	} {
		_, _, evErr, vmErr := arrowOutputs(t, src)
		if evErr == nil || vmErr == nil {
			t.Fatalf("%q: evaluator err %v, vm err %v; want both rejected", src, evErr, vmErr)
		}
		if evErr.Error() != vmErr.Error() || !strings.Contains(evErr.Error(), "arrow parameter x needs a type here") {
			t.Fatalf("%q: evaluator %q, vm %q", src, evErr, vmErr)
		}
	}
}

func TestArrowFunctionsInImportedModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module shapes;

export let evens = (list<int> xs) => xs.filter((x) => x % 2 == 0);

export func doubled(list<int> xs): list<int> {
    return xs.map((x) => x * 2);
}
`
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import shapes;
io.println(shapes.evens([1, 2, 3, 4]));
io.println(shapes.doubled([1, 2]));
let local = [5, 6].map((x) => x + 1);
io.println(local);
`)
	want := "[2, 4]\n[2, 4]\n[6, 7]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestExportedCallableValuesParity(t *testing.T) {
	dir := t.TempDir()
	module := `module donor;

export let evens = func(list<int> xs): list<int> { return xs.filter((x) => x % 2 == 0); };
export const twice = (int n) => n * 2;
let count = 0;
export let bump = () => {
    count = count + 1;
    return count;
};

class Doubler {
    func __invoke(int n): int {
        return n * 2;
    }
}

export let doubler = Doubler();
`
	if err := os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import donor;

func run(): void {
    defer io.println(donor.twice(5));
    io.println("body");
}

io.println(donor.evens([1, 2, 3, 4]));
io.println(donor.twice(21));
io.println(donor.twice(n: 2));
io.println(donor.evens(xs: [6, 7]));
io.println(donor.evens(...[[2, 3]]));
io.println(donor.bump());
io.println(donor.bump());
io.println(donor.doubler(4));
io.println(donor.doubler(n: 5));
run();
`)
	want := "[2, 4]\n42\n4\n[6]\n[2]\n1\n2\n8\n10\nbody\n10\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
