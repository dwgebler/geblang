package bytecode_test

import "testing"

func TestParityNullEqualityNeverCallsTypedEq(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Money {
    int p;
    func Money(int p) { this.p = p; }
    func __eq(Money o): bool { return this.p == o.p; }
}
?Money missing = null;
?Money present = Money(1);
io.println("${missing == null} ${missing != null} ${present == null} ${present != null} ${null == present} ${present == Money(1)}");
io.println("${[present] == [null]} ${[null] == [null]} ${{"a": present} == {"a": null}}");
`)
	want := "true false false true false true\nfalse true false\n"
	if ev != want || vm != want {
		t.Errorf("null equality:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
