package bytecode_test

import (
	"testing"

	"geblang/internal/runtime"
)

func TestNamedArgsToPrimitiveMethodsShareOneMessage(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
let xs = [1, 2, 3];
try {
    xs.reduce(fn: func(int a, int b): int { return a + b; }, initial: 0);
} catch (Error e) {
    io.println(e.message);
}
try {
    "a,b".split(separator: ",");
} catch (Error e) {
    io.println(e.message);
}
try {
    {"k": 1}.get(key: "k");
} catch (Error e) {
    io.println(e.message);
}
`)
	line := runtime.NamedArgsUnsupported + "\n"
	want := line + line + line
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
