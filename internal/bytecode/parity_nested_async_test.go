package bytecode_test

import "testing"

func TestParityNestedAsyncCallsReturnTasks(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import async;
async func inner(int n): int { await async.sleep(1); return n; }
async func outer(): string {
    let t = inner(1);
    return "${typeof(t)} ${await t}";
}
class K {
    async func m(int n): int { return n * 2; }
    async func n(): string { let t = this.m(3); return "${typeof(t)} ${await t}"; }
}
class Child extends K {
    async func m(int n): int { let t = parent.m(n); return (await t) + 1; }
}
func forward(callable next): callable { return func(any ...args): any { return next(...args); }; }
@forward
async func decorated(int n): int { await async.sleep(1); return n * 10; }
io.println(await outer());
io.println(await K().n());
io.println(await Child().m(4));
let fromCallback = [5, 6].map(func(int n): any { return inner(n); });
io.println([typeof(x) for x in fromCallback]);
let d = decorated(7);
io.println("${typeof(d)} ${await d}");
`)
	want := "Task 1\nTask 6\n9\n[Task, Task]\nTask 70\n"
	if ev != want || vm != want {
		t.Errorf("nested async calls:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
