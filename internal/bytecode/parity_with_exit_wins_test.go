package bytecode_test

import "testing"

// __exit raising replaces any body outcome (throw, fault, return), and a clean __exit lets the body propagate, identically on both backends.
func TestParityWithExitWins(t *testing.T) {
	src := `import io;
class ExitThrows { func __enter(): ExitThrows { return this; } func __exit(): void { throw ValueError("EXIT"); } }
class ExitClean  { func __enter(): ExitClean  { return this; } func __exit(): void {} }
func c1(): void { try { with (r = ExitThrows()) { throw ValueError("BODY"); } } catch (Error e) { io.println("1=[${e}]"); } }
func c2(): void { try { with (r = ExitClean())  { throw ValueError("BODY"); } } catch (Error e) { io.println("2=[${e}]"); } }
func c3(): void { try { with (r = ExitThrows()) { int z = 0; let q = 1 // z; } } catch (Error e) { io.println("3=[${e}]"); } }
func retH(): int { with (r = ExitThrows()) { return 42; } return 0; }
func c4(): void { try { let v = retH(); io.println("4 v=${v}"); } catch (Error e) { io.println("4=[${e}]"); } }
c1(); c2(); c3(); c4();
`
	want := "1=[ValueError: EXIT]\n2=[ValueError: BODY]\n3=[ValueError: EXIT]\n4=[ValueError: EXIT]\n"
	runParity(t, src, want)
}
