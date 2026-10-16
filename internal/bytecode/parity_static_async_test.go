package bytecode_test

import "testing"

func TestParityStaticAsyncMethodsReturnTasks(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import async;
class Rates {
    static async func fetch(string code): int { await async.sleep(1); return code.length(); }
    static async func both(): list<int> { return await async.all([Rates.fetch("ab"), Rates.fetch("abc")]); }
}
let t = Rates.fetch("gbp");
io.println("${typeof(t)} ${await t}");
io.println(await Rates.both());
let f = Rates.fetch;
io.println("${typeof(f("x"))}");
`)
	want := "Task 3\n[2, 3]\nTask\n"
	if ev != want || vm != want {
		t.Errorf("static async methods:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityCrossModuleStaticAsyncMethodsReturnTasks(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"ratesvc.gb": "module ratesvc;\nimport async;\nexport class Svc {\n    static async func make(int n): int { await async.sleep(1); return n + 100; }\n}\n",
	}, `import io;
import ratesvc;
class Local extends ratesvc.Svc {}
let a = ratesvc.Svc.make(1);
io.println("${typeof(a)} ${await a}");
let b = Local.make(2);
io.println("${typeof(b)} ${await b}");
let c = ratesvc.Svc.make(...[4]);
io.println("${typeof(c)} ${await c}");
`)
	want := "Task 101\nTask 102\nTask 104\n"
	if ev != want || vm != want {
		t.Errorf("cross-module static async:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
