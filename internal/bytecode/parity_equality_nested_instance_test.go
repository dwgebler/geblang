package bytecode_test

import "testing"

// A nested instance compares exactly as it does at top level: structurally, not by identity (finding 9.6). The evaluator previously routed container-nested instances through an identity compare while the VM recursed structurally.
func TestParityNestedInstanceEquality(t *testing.T) {
	runParity(t, `import io;
class Point {
    int x;
    func Point(int x) { this.x = x; }
}
let p1 = Point(1);
let p2 = Point(1);
let p3 = Point(2);
io.println(p1 == p2);
io.println([p1] == [p2]);
io.println([p1] == [p3]);
io.println([[p1]] == [[p2]]);
let d1 = {"k": p1};
let d2 = {"k": p2};
io.println(d1 == d2);
let d3 = {"k": p3};
io.println(d1 == d3);
`, "true\ntrue\nfalse\ntrue\ntrue\nfalse\n")
}

// A nested instance honors a user __eq at every depth, exactly as at top level.
func TestParityNestedInstanceEqualityDunder(t *testing.T) {
	runParity(t, `import io;
class Money {
    int cents; string currency;
    func Money(int c, string cur) { this.cents = c; this.currency = cur; }
    func __eq(any other): bool {
        if (!(other instanceof Money)) { return false; }
        return this.cents == other.cents;
    }
}
let a = Money(100, "USD");
let b = Money(100, "EUR");
let c = Money(200, "USD");
io.println([a] == [b]);
io.println([a] == [c]);
`, "true\nfalse\n")
}

// A frozen instance keys structurally, so sets of equal frozen instances compare equal on both backends.
func TestParityFrozenInstanceSetEquality(t *testing.T) {
	runParity(t, `import io;
@immutable
class FP { int x; func FP(int x) { this.x = x; } }
io.println({FP(1)} == {FP(1)});
io.println({FP(1)} == {FP(2)});
`, "true\nfalse\n")
}
