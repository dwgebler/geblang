package bytecode_test

import "testing"

// TestParityCrossModuleIntLiteralArgs: an int literal converts to another module's float/decimal parameter (function, overload, constructor) on both backends; an int variable does not.
func TestParityCrossModuleIntLiteralArgs(t *testing.T) {
	dir := moduleDirFor(t, map[string]string{
		"lits": `module lits;
export func asText(decimal d): string { return d.format(2); }
export func scaled(float f): float { return f * 2.0f; }
export func pick(decimal d): string { return "decimal"; }
export func pick(string s): string { return "string"; }
export class Amount {
    decimal value;
    func Amount(decimal value) { this.value = value; }
}
`,
	})
	runParityModulesDir(t, dir, `import io;
import lits;
io.println(lits.asText(4));
io.println(lits.scaled(3));
io.println(lits.pick(5));
io.println(lits.Amount(7).value.format(1));
int n = 2;
try { lits.asText(n); } catch (Error e) { io.println(e.message); }
`, "4.00\n6\ndecimal\n7.0\nasText expects decimal for parameter 'd', got int\n")
}
