package bytecode_test

import (
	"strings"
	"testing"

	"geblang/internal/native"
)

func TestFoldArgumentOrderMatchesCatalog(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
let xs = [1, 2, 3];
io.println(xs.reduce(func(int a, int b): int { return a + b; }, 10));
io.println(xs.scan(0, func(int a, int b): int { return a + b; }));
`)
	want := "16\n[0, 1, 3, 6]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
	list := native.CatalogPrimitiveMethods["list"]
	if p := list["reduce"].Params; len(p) != 2 || !strings.HasPrefix(p[0], "func ") || !strings.HasSuffix(p[1], " initial") {
		t.Fatalf("catalog list.reduce params = %v, want (func fn, any initial)", p)
	}
	if p := list["scan"].Params; len(p) != 2 || !strings.HasSuffix(p[0], " initial") || !strings.HasPrefix(p[1], "func ") {
		t.Fatalf("catalog list.scan params = %v, want (any initial, func combine)", p)
	}
}
