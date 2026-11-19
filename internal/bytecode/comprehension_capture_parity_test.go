package bytecode_test

import "testing"

func TestComprehensionClosuresCaptureEachItemParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import async;

let plain = [() => n for n in [1, 2, 3]];
io.println(plain.map((callable f) => f()));
let pairs = [() => "${k}=${v}" for k, v in {"a": 1, "b": 2}.items()];
io.println(pairs.map((callable f) => f()));
let nested = [() => x * 10 + y for x in [1, 2] for y in [3, 4] if x != y];
io.println(nested.map((callable f) => f()));
let typed = [() => n + 1 for int n in [5, 6]];
io.println(typed.map((callable f) => f()));
let keyed = {k: () => k for k in ["m", "n"]};
io.println((keyed["n"] as callable)());
let tasks = [async.run(() => n * n) for n in [2, 3, 4]];
io.println(await async.all(tasks));
let shared = [];
for (i in [1, 2, 3]) {
    shared.push(() => i);
}
io.println(shared.map((callable f) => f()));
`)
	want := "[1, 2, 3]\n[\"a=1\", \"b=2\"]\n[13, 14, 23, 24]\n[6, 7]\nn\n[4, 9, 16]\n[3, 3, 3]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
