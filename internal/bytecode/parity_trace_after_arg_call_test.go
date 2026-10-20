package bytecode_test

import "testing"

// TestParityTraceLineAfterNestedArgCall: an overload/constructor selection error is attributed to the call's line, not to the last line a function evaluated as one of its arguments ran, on both backends.
func TestParityTraceLineAfterNestedArgCall(t *testing.T) {
	dir := moduleDirFor(t, map[string]string{
		"tl": `module tl;
export func o(decimal d): string { return "d"; }
export func o(string s): string { return "s"; }
export class Money {
    decimal amount;
    func Money(decimal amount) { this.amount = amount; }
}
`,
	})
	runParityModulesDir(t, dir, `import io;
import tl;
func f(): any {
    return tl.Money(dyn(1), 2);
}
func g(): any {
    return tl.o(dyn(true));
}
try { f(); } catch (RuntimeError e) { let fr = e.stackTrace().frames(); io.println("${fr[0].function()}:${fr[0].line()}"); }
try { g(); } catch (RuntimeError e) { let fr = e.stackTrace().frames(); io.println("${fr[0].function()}:${fr[0].line()}"); }
func dyn(any v): any {
    return v;
}
`, "f:4\ng:7\n")
}
