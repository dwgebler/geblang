package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityReflectInterfaceNameAndLocation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(`module shapes;

/** A named shape. */
export interface Shape {
    func area(): int;
}

export interface Solid extends Shape {
    func volume(): int;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import shapes;
from shapes import Solid;

interface Local {
    func name(): string;
}

func describe(any target): string {
    let loc = reflect.location(target);
    return "${reflect.className(target)} ${loc["module"]}:${loc["line"]}:${loc["column"]}";
}

io.println(describe(Local));
io.println(describe(shapes.Shape));
io.println(describe(Solid));
let alias = shapes.Shape;
io.println(describe(alias));
`)
	if ev != vm {
		t.Fatalf("output mismatch:\n  evaluator: %q\n  vm:        %q", ev, vm)
	}
	want := "Local :6:1\nShape shapes:4:8\nSolid shapes:8:8\nShape shapes:4:8\n"
	if ev != want {
		t.Fatalf("got %q, want %q", ev, want)
	}
}
