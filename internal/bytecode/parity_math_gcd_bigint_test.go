package bytecode_test

import "testing"

func TestParityMathGcdLcmBigInt(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import math;

let big = 2 ** 100;
io.println(math.gcd(big, 6 ** 50));
io.println(math.gcd(-(2 ** 70), 2 ** 65));
io.println(math.gcd(big, 12));
io.println(math.gcd(12, big));
io.println(typeof(math.gcd(big, 12)));
io.println(math.gcd(big, 0));
io.println(math.gcd(0, 0));
io.println(math.gcd(0, -5));
io.println(math.gcd(math.perm(365, 23), 365 ** 23));
io.println(math.lcm(2 ** 70, 3 ** 50));
io.println(math.lcm(-(2 ** 70), 2 ** 65));
io.println(math.lcm(big, 0));
io.println(math.lcm(-4, 6));
io.println(math.lcm(9223372036854775807, 2));
let pair = [big, 2 ** 90];
io.println(math.gcd(...pair));
io.println(math.gcd(b: 18, a: big * 9));
try {
    math.gcd(1.5, 2);
} catch (RuntimeError e) {
    io.println(e.message);
}
try {
    math.lcm(2, "x");
} catch (RuntimeError e) {
    io.println(e.message);
}
`)
	want := "1125899906842624\n36893488147419103232\n4\n4\nint\n1267650600228229401496703205376\n0\n5\n" +
		"1140625\n847544348798892439652940749688313000363032576\n1180591620717411303424\n0\n12\n18446744073709551614\n" +
		"1237940039285380274899124224\n18\nmath.gcd a expects an integer\nmath.lcm b expects an integer\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
