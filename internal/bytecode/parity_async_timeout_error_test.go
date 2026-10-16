package bytecode_test

import "testing"

func TestParityAsyncTimeoutRaisesTimeoutError(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import async;
async func slow(): int { await async.sleep(300); return 1; }
try { await async.timeout(slow(), 20); } catch (TimeoutError e) { io.println("${e.class}: ${e.message}"); }
try { await async.timeout(slow(), 20); } catch (IOError e) { io.println("as IOError: ${e.class}"); }
io.println(await async.timeout(slow(), 2000));
`)
	want := "TimeoutError: async.timeout: task did not complete within 20ms\nas IOError: TimeoutError\n1\n"
	if ev != want || vm != want {
		t.Errorf("async.timeout error class:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
