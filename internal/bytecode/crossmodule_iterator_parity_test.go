package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForeignSubclassIteratedInDeclaringModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module shelves;

export class Shelf {
    list<int> items;
    func Shelf(list<int> items) { this.items = items; }
    func __iter(): any { return this.items; }
}
export class Counter {
    int n = 0;
    func Counter() {}
    func __next(): any { this.n += 1; return this.n; }
    func __done(): bool { return this.n >= 2; }
}
export func total(any xs): int {
    int sum = 0;
    for (x in xs) { sum += x; }
    return sum;
}
export func listed(any xs): list<any> { return [x for x in xs]; }
`
	if err := os.WriteFile(filepath.Join(dir, "shelves.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import shelves;

class BigShelf extends shelves.Shelf {
    func BigShelf(list<int> items) { parent(items); }
}
class BigCounter extends shelves.Counter {
    func BigCounter() { parent(); }
}
class Own {
    func Own() {}
    func __iter(): any { return [4]; }
}
io.println(shelves.total(BigShelf([2, 3])));
io.println(shelves.total(BigCounter()));
io.println(shelves.total(Own()));
io.println(shelves.listed(BigShelf([5])));
`)
	want := "5\n3\n4\n[5]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestComprehensionsOverUserIterablesParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Bag {
    func Bag() {}
    func __iter(): any { return [1, 2]; }
}
class Lazy {
    func Lazy() {}
    func __iter(): any { return (x * 10 for x in [1, 2]); }
}
class Steps {
    int n = 0;
    func Steps() {}
    func __next(): any { this.n += 1; return this.n; }
    func __done(): bool { return this.n >= 3; }
}
class Wrapper {
    func Wrapper() {}
    func __iter(): any { return Steps(); }
}
class Pairs {
    func Pairs() {}
    func __iter(): any { return [["a", 1], ["b", 2]]; }
}
io.println([x for x in Bag()]);
io.println([x for x in Lazy()]);
io.println([x for x in Steps()]);
io.println([x for x in Wrapper()]);
io.println({x for x in Bag()});
io.println({k: v for k, v in Pairs()});
io.println([x * y for x in Bag() for y in Steps() if y != 2]);
`)
	want := "[1, 2]\n[10, 20]\n[1, 2, 3]\n[1, 2, 3]\nset{1, 2}\n{\"a\": 1, \"b\": 2}\n[1, 3, 2, 6]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
