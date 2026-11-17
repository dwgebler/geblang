package bytecode_test

import "testing"

// Expressions inside string interpolation were lexed as if they started on line 1, so their faults reported line 1.
func TestParityInterpolationFaultLines(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
func indexFault(): string {
    list<int> xs = [];
    return "value ${xs[3]}";
}
let closureFault = func(): string {
    any m = {"a": 1};
    return """
first line
second ${m.nope()}
""";
};
func report(string label, callable f): void {
    try {
        f();
    } catch (Error e) {
        io.println("${label}: ${typeof(e)} at line ${e.stackTrace().first().line()}");
    }
}
report("function", indexFault);
report("closure", closureFault);
`)
	want := "function: ValueError at line 4\nclosure: RuntimeError at line 10\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
