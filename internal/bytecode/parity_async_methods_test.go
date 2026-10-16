package bytecode_test

import "testing"

func TestParityAsyncMethodsReturnTasks(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import async;
class Base {
    async func work(int n, int extra = 0): int { await async.sleep(1); return n + extra; }
    func viaThis(int n): any { return this.work(n); }
    async func fail(): int { throw ValueError("boom"); }
}
class Child extends Base {}
func kind(any v): string { return "${typeof(v)}"; }
let b = Base();
let t1 = b.work(1);
io.println("${kind(t1)} ${await t1}");
let t2 = b.viaThis(2);
io.println("${kind(t2)} ${await t2}");
let t3 = Child().work(3);
io.println("${kind(t3)} ${await t3}");
let t4 = b.work(4, extra: 10);
io.println("${kind(t4)} ${await t4}");
let t5 = b.work(...[5]);
io.println("${kind(t5)} ${await t5}");
let bound = b.work;
let t6 = bound(6);
io.println("${kind(t6)} ${await t6}");
io.println(await async.all([b.work(7), b.work(8)]));
let f = b.fail();
io.println(kind(f));
try { await f; } catch (ValueError e) { io.println("caught ${e.message}"); }
`)
	want := "Task 1\nTask 2\nTask 3\nTask 14\nTask 5\nTask 6\n[7, 8]\nTask\ncaught boom\n"
	if ev != want || vm != want {
		t.Errorf("async method calls:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

const asyncMethodDonorGb = `module asyncsvc;
import async;
export class Svc {
    async func fetch(int n): int { await async.sleep(1); return n * 2; }
}
`

func TestParityCrossModuleAsyncMethodsReturnTasks(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"asyncsvc.gb": asyncMethodDonorGb}, `import io;
import asyncsvc;
class Local extends asyncsvc.Svc {}
let t1 = asyncsvc.Svc().fetch(4);
io.println("${typeof(t1)} ${await t1}");
let t2 = Local().fetch(5);
io.println("${typeof(t2)} ${await t2}");
`)
	want := "Task 8\nTask 10\n"
	if ev != want || vm != want {
		t.Errorf("cross-module async methods:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
