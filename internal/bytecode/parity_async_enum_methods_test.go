package bytecode_test

import "testing"

func TestParityAsyncEnumMethodsReturnTasks(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"signals.gb": "module signals;\nimport async;\nexport enum Signal {\n    Red, Green;\n    async func next(): string { await async.sleep(1); return this == Signal.Red ? \"Green\" : \"Red\"; }\n}\n",
	}, `import io;
import async;
import signals;
enum Light {
    Red, Green;

    async func next(): string { await async.sleep(1); return this == Light.Red ? "Green" : "Red"; }
    func label(): string { return "light"; }
}
let local = Light.Red.next();
io.println("${typeof(local)} ${await local} ${Light.Green.label()}");
let foreign = signals.Signal.Green.next();
io.println("${typeof(foreign)} ${await foreign}");
io.println(await async.all([Light.Red.next(), Light.Green.next()]));
`)
	want := "Task Green light\nTask Red\n[\"Green\", \"Red\"]\n"
	if ev != want || vm != want {
		t.Errorf("async enum methods:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
