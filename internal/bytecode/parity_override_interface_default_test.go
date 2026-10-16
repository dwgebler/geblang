package bytecode_test

import "testing"

func TestParityOverrideOfInterfaceDefaultCompilesAndRuns(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
interface Labelled {
    func label(): string { return "unlabelled"; }
    func shout(): string { return this.label().upper(); }
}
class Parcel implements Labelled {
    @override
    func label(): string { return "fragile"; }
}
io.println(Parcel().shout());
`)
	if want := "FRAGILE\n"; ev != want || vm != want {
		t.Errorf("override of interface default:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
