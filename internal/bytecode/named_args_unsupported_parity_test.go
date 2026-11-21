package bytecode_test

import (
	"testing"

	"geblang/internal/runtime"
)

func TestNamedArgsToPrimitiveMethodsShareOneMessage(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func gen(): generator<int> { yield 1; }
any xs = [1, 2, 3];
any text = "a,b";
try {
    xs.reduce(fn: (int a, int b) => a + b, initial: 0);
} catch (Error e) {
    io.println(e.message);
}
try {
    text.split(sep: ",");
} catch (Error e) {
    io.println(e.message);
}
try {
    "abc".slice(end: 2);
} catch (Error e) {
    io.println(e.message);
}
any g = gen();
try {
    g.next(value: 1);
} catch (Error e) {
    io.println(e.message);
}
`)
	want := "list.reduce has no parameter fn\nstring.split has no parameter sep\nstring.slice missing argument start\n" + runtime.NamedArgsUnsupported + "\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
