package bytecode_test

import "testing"

func TestParityListShiftAndTake(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

list<int> xs = [1, 2, 3, 4, 5];
io.println(xs.shift());
io.println(xs.shift() == xs);
list<int> empty = [];
io.println(empty.shift());

list<int> ys = [10, 20, 30, 40, 50];
int first = ys.takeFirst();
int last = ys.takeLast();
int mid = ys.takeAt(1);
int neg = ys.takeAt(-1);
io.println("${first} ${last} ${mid} ${neg} ${ys}");

let alias = ys;
alias.takeFirst();
io.println(ys.length());

list<string> words = ["a", "b"];
string w = words.takeLast();
io.println(w + words.join(","));
`)
	want := "[2, 3, 4, 5]\ntrue\n[]\n10 50 30 40 [20]\n0\nba\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityListTakeErrors(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import freeze;

func report(func f): void {
    try {
        f();
    } catch (Error e) {
        io.println("${typeof(e)}: ${e.message}");
    }
}

list<int> empty = [];
report(func(): void { empty.takeFirst(); });
report(func(): void { empty.takeLast(); });
report(func(): void { empty.takeAt(0); });
list<int> xs = [1, 2];
report(func(): void { xs.takeAt(2); });
report(func(): void { xs.takeAt(-3); });
report(func(): void { xs.takeAt("a"); });
let frozen = freeze.shallow([1, 2]);
report(func(): void { frozen.shift(); });
report(func(): void { frozen.takeFirst(); });
report(func(): void { frozen.takeLast(); });
report(func(): void { frozen.takeAt(0); });
io.println(xs);
`)
	want := "ValueError: list.takeFirst on empty list\n" +
		"ValueError: list.takeLast on empty list\n" +
		"ValueError: list.takeAt: index out of range\n" +
		"ValueError: list.takeAt: index out of range\n" +
		"ValueError: list.takeAt: index out of range\n" +
		"TypeError: list.takeAt: index must be int, got string\n" +
		"ImmutableError: cannot modify frozen list\n" +
		"ImmutableError: cannot modify frozen list\n" +
		"ImmutableError: cannot modify frozen list\n" +
		"ImmutableError: cannot modify frozen list\n" +
		"[1, 2]\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
