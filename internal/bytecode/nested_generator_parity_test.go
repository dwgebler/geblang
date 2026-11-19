package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const nestedGeneratorProgram = `import io;

func named(): generator<int> { yield 1; yield 2; }
class Source {
    func Source() {}
    func each(): generator<int> { yield 3; }
    static func stat(): generator<int> { yield 4; }
}
class Sub extends Source { func Sub() { parent(); } }
interface Walk { func walk(): generator<int> { yield 5; } }
class W implements Walk { func W() {} }
let closure = func(): generator<int> { yield 6; };

func helper(): list { return [v for v in named()]; }
func deep(int depth): generator<int> {
    if (depth == 0) { yield 1; } else {
        for (v in deep(depth - 1)) { yield v + 1; }
    }
}
func interleave(): generator<int> {
    let a = named();
    let b = named();
    while (!a.done()) { yield a.next(); yield b.next() * 100; }
}
func viaNamed(): generator<int> { for (v in named()) { yield v * 10; } }
func viaMethod(): generator<int> { for (v in Sub().each()) { yield v * 10; } }
func viaStatic(): generator<int> { for (v in Source.stat()) { yield v * 10; } }
func viaDefault(): generator<int> { for (v in W().walk()) { yield v * 10; } }
func viaClosure(): generator<int> { for (v in closure()) { yield v * 10; } }
func viaLocalClosure(): generator<int> {
    let inner = func(): generator<int> { yield 7; };
    for (v in inner()) { yield v * 10; }
}
func viaHelper(): generator<int> { for (v in helper()) { yield v * 10; } }

io.println([v for v in viaNamed()]);
io.println([v for v in viaMethod()]);
io.println([v for v in viaStatic()]);
io.println([v for v in viaDefault()]);
io.println([v for v in viaClosure()]);
io.println([v for v in viaLocalClosure()]);
io.println([v for v in viaHelper()]);
io.println([v for v in deep(4)]);
io.println([v for v in interleave()]);
`

const nestedGeneratorWant = "[10, 20]\n[30]\n[40]\n[50]\n[60]\n[70]\n[10, 20]\n[5]\n[1, 100, 2, 200]\n"

func TestNestedGeneratorsStayLazyParity(t *testing.T) {
	ev, vm := parityOutputs(t, nestedGeneratorProgram)
	if ev != nestedGeneratorWant || vm != nestedGeneratorWant {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, nestedGeneratorWant)
	}
}

func TestNestedGeneratorsAcrossModulesParity(t *testing.T) {
	dir := t.TempDir()
	module := `module gens;

export func counted(int n): generator<int> {
    for (let int i = 1; i <= n; i++) { yield i; }
}
export class Source {
    func Source() {}
    func each(): generator<int> { yield 1; yield 2; }
}
export func relay(): generator<int> {
    for (v in counted(2)) { yield v * 1000; }
}
`
	if err := os.WriteFile(filepath.Join(dir, "gens.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import gens;

class Sub extends gens.Source { func Sub() { parent(); } }
func viaImported(): generator<int> { for (v in gens.counted(3)) { yield v * 10; } }
func viaInherited(): generator<int> { for (v in Sub().each()) { yield v * 10; } }

io.println([v for v in viaImported()]);
io.println([v for v in viaInherited()]);
io.println([v for v in gens.relay()]);
`)
	want := "[10, 20, 30]\n[10, 20]\n[1000, 2000]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
