package bytecode_test

import "testing"

// The VM test runner formats a failure line as a clean class-prefixed message plus the full frame chain, matching the evaluator, not the "uncaught ..." blob with a truncated chain (finding 9.5).
func TestParityTestRunnerFailureFormat(t *testing.T) {
	runParityWithStdlib(t, `import io;
import test;
class AssertFail extends test.Test {
    @test
    func fails(): void { this.assertEquals(1, 2); }
}
class ThrowFail extends test.Test {
    @test
    func fails(): void { throw ValueError("boom value"); }
}
class DivFail extends test.Test {
    @test
    func fails(): void { let z = 0; let q = 10 // z; }
}
for (f in test.run(AssertFail)["failures"]) { io.println(f); }
for (f in test.run(ThrowFail)["failures"]) { io.println(f); }
for (f in test.run(DivFail)["failures"]) { io.println(f); }
`, "fails: RuntimeError: expected 1, got 2\n  at AssertFail.fails (line 5)\n  at <top level> (line 15)\nfails: ValueError: boom value\n  at ThrowFail.fails (line 9)\n  at <top level> (line 16)\nfails: RuntimeError: integer division by zero\n  at DivFail.fails (line 13)\n  at <top level> (line 17)\n")
}
