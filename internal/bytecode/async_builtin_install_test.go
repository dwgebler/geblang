package bytecode_test

import "testing"

func TestAsyncTasksShareLazyBuiltinInstall(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{}, `import io;
import async;
async func job(int n): int {
    await async.sleep(1);
    return n * 2;
}
let tasks = [];
for (i in 0..7) {
    tasks.push(job(i));
}
let results = await async.all(tasks);
io.println(results);
`)
	want := "[0, 2, 4, 6, 8, 10, 12, 14]\n"
	if ev != want || vm != want {
		t.Fatalf("async tasks:\nevaluator: %q\nvm: %q\nwant: %q", ev, vm, want)
	}
}
