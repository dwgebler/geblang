package bytecode_test

import "testing"

func TestGenericCallAsOperatorOperandParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

async func later<T>(int v): int { return v; }
func isA<T>(any v): bool { return v instanceof T; }
func neg<T>(int v): int { return -v; }

class Calc {
    func twice<T>(int v): int { return v * 2; }
}

let task = later<int>(4);
io.println(await task);
io.println(await later<int>(5));
io.println("${await later<int>(6)}");
io.println(!isA<int>(1));
io.println(-neg<int>(2));
io.println(~neg<int>(2));
io.println(1 + neg<int>(2));
io.println(2 * neg<int>(3));
io.println(10 - Calc().twice<int>(3));
io.println(1 < neg<int>(2));
io.println(neg<int>(2) + 1);
`)
	want := "4\n5\n6\nfalse\n2\n1\n-1\n-6\n4\nfalse\n-1\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}
