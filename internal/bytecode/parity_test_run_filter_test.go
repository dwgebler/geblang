package bytecode_test

import "testing"

// The VM test runner ignored options.methods, so a method filter ran every test.
func TestParityTestRunMethodFilter(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import test;
class BaseCase extends test.Test {
    @test
    @tag("fast")
    func inherited(): void { this.assertTrue(true); }
}
class Filtered extends BaseCase {
    @test
    @tag("fast")
    func chosen(): void { this.assertTrue(true); }
    @test
    func other(): void { this.fail("should be filtered out"); }
}
func names(dict<string, any> r): string {
    list<string> out = [];
    for (c in r["tests"]) { out.push((c as dict<string, any>)["name"] as string); }
    return "${r["total"]} ${r["failed"]} ${out.sorted()}";
}
io.println(names(test.run(Filtered, {"methods": ["chosen"]})));
io.println(names(test.run(Filtered, {"methods": ["chosen", "inherited"]})));
io.println(names(test.run(Filtered, {"methods": ["other", "inherited"], "tags": ["fast"]})));
io.println(names(test.run(Filtered, {"methods": ["missing"]})));
`)
	want := "1 0 [\"chosen\"]\n2 0 [\"chosen\", \"inherited\"]\n1 0 [\"inherited\"]\n0 0 []\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
