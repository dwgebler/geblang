package bytecode_test

import "testing"

func TestListClearRejectsArgumentsParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

any xs = [1, 2, 3];
try {
    xs.clear(1);
} catch (Error e) {
    io.println(e.message);
}
io.println(xs);
xs.clear();
io.println(xs);
`)
	want := "list.clear expects no arguments\n[1, 2, 3]\n[]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
