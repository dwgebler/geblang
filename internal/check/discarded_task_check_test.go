package check

import (
	"path/filepath"
	"testing"
)

func TestCheckWarnsOnDiscardedWaitOnlyTasks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `import async;
import async as a2;
from async import sleep as nap;

func work(): int {
    return 1;
}

async func later(): int {
    return 2;
}

async.sleep(10);
a2.sleep(10);
nap(10);
async.all([async.run(work)]);
async.race([async.run(work)]);
async.timeout(async.run(work), 100);
await async.sleep(10);
async.run(work);
later();
let t = async.sleep(10);
await t;
func shadowed(): void {
    let nap = (int ms) => ms;
    nap(5);
}
`
	_, diags := Source(file, src, classCheckOpts(dir))
	var lines []int
	for _, d := range diags {
		if d.Rule == "discarded-task" {
			if d.Severity != SeverityWarning {
				t.Errorf("discarded-task must be a warning: %+v", d)
			}
			lines = append(lines, d.Line)
		}
	}
	want := []int{13, 14, 15, 16, 17, 18}
	if len(lines) != len(want) {
		t.Fatalf("discarded-task lines %v, want %v; diagnostics: %v", lines, want, diags)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("discarded-task lines %v, want %v", lines, want)
		}
	}
}
