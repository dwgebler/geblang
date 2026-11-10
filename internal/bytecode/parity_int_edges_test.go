package bytecode_test

import "testing"

const intEdgeProgram = `import io;

int mn = -9223372036854775807 - 1;
int neg1 = -1;
io.println(neg1 * mn);
io.println(mn * neg1);
func locals(): void {
    int a = -1;
    int b = -9223372036854775807 - 1;
    io.println(a * b);
    int c = -1;
    c *= b;
    io.println(c);
    int d = 3037000500;
    io.println(d * d);
    io.println(-3037000500 * 3037000500);
    io.println(b // -1);
    io.println(-b);
}
locals();
`

func TestParityIntMultiplyOverflowEdges(t *testing.T) {
	ev, vm := parityOutputs(t, intEdgeProgram)
	want := "9223372036854775808\n9223372036854775808\n9223372036854775808\n9223372036854775808\n9223372037000250000\n-9223372037000250000\n9223372036854775808\n9223372036854775808\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
}

const nativeIntOperandsProgram = `import io;
import math;

func takesInt(int x): int { return x * 2; }

int n = "abc".length();
io.println(-n);
io.println(~n);
int m = "ab".length();
m++;
m++;
io.println(m);
m--;
io.println(m);
io.println(math.abs(-n));
io.println((-n).abs());
io.println(takesInt(n));
io.println(range(0, 5).contains(n));
io.println([n & 1, n | 4, n ^ 1, n << 2, n >> 1]);
io.println(n == 3);
let d = {3: "three"};
io.println(d[n]);
int total = 0;
for (int i = 0; i < 1000; i++) {
    total = total + n + i;
}
io.println(total);
io.println(-(3 + 2));
int k = (-9223372036854775807 - 1) + ("".length());
io.println(-k);
`

func TestParityNativeIntOperands(t *testing.T) {
	ev, vm := parityOutputs(t, nativeIntOperandsProgram)
	want := "-3\n-4\n4\n3\n3\n3\n6\ntrue\n[1, 7, 2, 12, 1]\ntrue\nthree\n502500\n-5\n9223372036854775808\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
}
