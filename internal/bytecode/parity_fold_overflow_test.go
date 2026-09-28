package bytecode_test

import "testing"

func TestParityConstantFoldingIntOverflow(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

io.println(9223372036854775807 + 1);
io.println(9223372036854775807 + 9223372036854775807);
io.println(9223372036854775807 * 2);
io.println(4611686018427387904 * 4);
io.println(0 - 9223372036854775807);
io.println(typeof(9223372036854775807 + 1));
io.println(9223372036854775807 + 1 > 9223372036854775807);
`)
	want := "9223372036854775808\n18446744073709551614\n18446744073709551614\n18446744073709551616\n-9223372036854775807\nint\ntrue\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
