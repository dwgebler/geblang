package bytecode_test

import "testing"

func TestNativeCallFormsParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import random;
import collections;
import errors;

let a = random.Generator(7);
let b = random.Generator(7);
io.println(random.next(a) == random.next(b));
io.println(random.choice(a, [1, 2, 3]) == random.choice(b, [1, 2, 3]));
io.println(random.shuffle(a, [1, 2, 3]) == random.shuffle(b, [1, 2, 3]));
io.println(typeof(random.float(a)));
io.println(typeof(random.bool(a)));
io.println([x for x in collections.range(3)]);
io.println(errors.new("ValueError").class);
`)
	want := "true\ntrue\ntrue\nfloat\nbool\n[0, 1, 2]\nValueError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
