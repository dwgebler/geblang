package bytecode_test

import "testing"

// TestParityNamedArgsToNatives: named arguments bind to a native's declared parameter names in any order, identically on both backends; unknown names are compile errors (native_named_args_compile_test.go).
func TestParityNamedArgsToNatives(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.pow(base: 2.0, exponent: 3.0));
io.println(math.pow(exponent: 3.0, base: 2.0));
io.println(math.max(right: 5, left: 3));
`, "8\n8\n5\n")
}
