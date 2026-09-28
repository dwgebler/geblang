package bytecode_test

import "testing"

func TestParityMathSumAndMean(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import math;

io.println(math.sum([1, 2, 3]));
io.println(typeof(math.sum([1, 2, 3])));
io.println(math.sum([1, 2.5]));
io.println(typeof(math.sum([1, 2.5])));
io.println(math.sum([1, 2.5f]));
io.println(typeof(math.sum([1, 2.5f])));
io.println(math.sum([]));
io.println(math.sum([9223372036854775807, 1]));
io.println(math.sum({1, 2, 3}));
io.println(math.sum(range(1, 5)));
io.println(math.sum({"a": 2, "b": 3}.values()));
io.println(math.mean([1, 2, 3, 4]));
io.println(typeof(math.mean([1, 2])));
io.println(math.mean([1.5, 2.5f]));
io.println(math.mean({2, 4}));
io.println(math.mean([7]));
`)
	want := "6\nint\n3.5000000000\ndecimal\n3.5\nfloat\n0\n9223372036854775808\n6\n15\n5\n2.5\nfloat\n2\n3\n7\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}

func TestParityMathSumAndMeanErrors(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import math;

func report(func f): void {
    try {
        f();
    } catch (Error e) {
        io.println("${typeof(e)}: ${e.message}");
    }
}

list<any> mixed = [1.5, 2.5f];
list<any> strs = [1, "a"];
report(func(): void { math.sum(mixed); });
report(func(): void { math.sum(strs); });
report(func(): void { math.sum(5); });
report(func(): void { math.mean([]); });
report(func(): void { math.mean(strs); });
report(func(): void { math.mean({1, "x"}); });
report(func(): void { math.mean("abc"); });
`)
	want := "RuntimeError: math.sum: cannot mix decimal and float (got decimal and float): cast one side - 'as float' drops decimal exactness, 'as decimal' adopts the float's imprecision\n" +
		"RuntimeError: math.sum: list element 1: expected numeric value, got string\n" +
		"RuntimeError: math.sum: argument must be a list or set\n" +
		"RuntimeError: math.mean: list must not be empty\n" +
		"RuntimeError: math.mean: list element 1: expected numeric value, got string\n" +
		"RuntimeError: math.mean: set element: expected numeric value, got string\n" +
		"RuntimeError: math.mean: argument must be a list or set\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
