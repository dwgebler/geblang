package bytecode_test

import "testing"

func TestSetOperationsMatchTypeAndValueParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

any same = (any k) => k;
io.println(["1", "x"].difference([1]));
io.println([1].intersection(["1"]));
io.println(["true", "null", true].difference([true, null]));
io.println(["1.5", 1.5].differenceBy([1.5], same));
io.println([1.5].intersectionBy(["1.5"], same));
io.println(["1", 1, 1].frequencies());
io.println(["1", "1", 1].mode());
io.println([1, 2, 2, 3].difference([2]));
`)
	want := "[\"1\", \"x\"]\n[]\n[\"true\", \"null\"]\n[\"1.5\"]\n[]\n{1: 2, \"1\": 1}\n1\n[1, 3]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestDecimalDefaultDisplayParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import json;

class Order {
    decimal total;
}

decimal third = 1.0 / 3.0;
io.println(12.5);
io.println(7 / 2);
io.println(0.1 + 0.2);
io.println(7.0);
io.println(-2.50);
io.println(third);
io.println("${3.25}|${12.5 as string}|${(12.5).toString()}|${(12.5).toString(3)}|${(12.5).format(2)}");
io.println([1.5, 2.0]);
io.println(json.stringify(json.parseAs("{\"total\": 120.50}", Order)));
io.println(dump(4.10));
`)
	want := "12.5\n3.5\n0.3\n7.0\n-2.5\n0.3333333333\n3.25|12.5|12.5|12.500|12.50\n[1.5, 2.0]\n{\"total\":120.5}\ndecimal(4.1)\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
