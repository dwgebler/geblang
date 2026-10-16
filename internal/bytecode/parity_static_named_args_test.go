package bytecode_test

import "testing"

func TestParityNamedArgumentsToStaticMethodsAcrossModules(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"calc.gb": "module calc;\nexport class Calc {\n    static func add(int a, int b = 10, int c = 100): int { return a + b + c; }\n}\n",
	}, `import io;
import calc;
class Local extends calc.Calc {}
class Here { static func add(int a, int b = 10, int c = 100): int { return a + b + c; } }
io.println(calc.Calc.add(a: 1));
io.println(calc.Calc.add(1, c: 2));
io.println(Local.add(c: 7, a: 1));
io.println(Here.add(a: 1, c: 0));
let k = Here;
io.println(k.add(1, c: 1));
`)
	want := "111\n13\n18\n11\n12\n"
	if ev != want || vm != want {
		t.Errorf("named static args:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
