package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const exitDefersLib = `module exlib;
import io;
import sys;
export func quit(int code): void {
    defer io.println("lib defer");
    sys.exit(code);
}
export class Q {
    func stop(): void {
        defer io.println("Q.stop defer");
        defer sys.exit(9);
    }
}
`

// sys.exit unwinds like a return: pending defers in every active function run, innermost first, on both runtimes.
func TestExitRunsPendingDefers(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "exlib.gb"), []byte(exitDefersLib), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, src, out string
		code           int
	}{
		{"direct", `import io;
import sys;
func inner(): void { defer io.println("inner defer"); sys.exit(5); }
func outer(): void { defer io.println("outer defer"); inner(); }
outer();
io.println("not reached");
`, "inner defer\nouter defer\n", 5},
		{"deferred exit runs the remaining defers", `import io;
import sys;
class Leaver {
    func go(): void {
        defer io.println("first registered");
        defer sys.exit(4);
        defer io.println("last registered");
        io.println("in method");
    }
}
func wrapper(): void { defer io.println("wrapper defer"); Leaver().go(); }
wrapper();
io.println("not reached");
`, "in method\nlast registered\nfirst registered\nwrapper defer\n", 4},
		{"later exit wins", `import io;
import sys;
func inner(): void { defer sys.exit(7); sys.exit(5); }
defer io.println("top defer");
inner();
`, "top defer\n", 7},
		{"throwing defer replaces the exit", `import io;
import sys;
func boom(): void { throw ValueError("cleanup failed"); }
func inner(): void { defer boom(); sys.exit(5); }
func outer(): void { defer io.println("outer defer"); inner(); }
outer();
`, "outer defer\nuncaught ValueError: cleanup failed\n  at boom (line 3)\n  at inner (line 4)\n  at outer (line 5)\n  at <top level> (line 6)\n", 1},
		{"throwing defer can be caught", `import io;
import sys;
func boom(): void { throw ValueError("cleanup failed"); }
func inner(): void { defer boom(); sys.exit(5); }
try { inner(); } catch (ValueError e) { io.println("caught ${e.message}"); }
io.println("continues");
`, "caught cleanup failed\ncontinues\n", 0},
		{"exit inside an imported module", `import io;
import exlib;
func viaLib(): void { defer io.println("main frame defer"); exlib.quit(6); }
defer io.println("top defer");
viaLib();
`, "lib defer\nmain frame defer\ntop defer\n", 6},
		{"exit from a method value", `import io;
import exlib;
func viaValue(): void {
    defer io.println("value caller defer");
    let s = exlib.Q().stop;
    s();
}
viaValue();
io.println("not reached");
`, "Q.stop defer\nvalue caller defer\n", 9},
	}
	for i, tc := range cases {
		path := filepath.Join(dir, "exit_case_"+string(rune('a'+i))+".gb")
		if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, mode := range [][]string{{"--disable-vm"}, {}, {}} {
			cmd := exec.Command(bin, append(append([]string{"run"}, mode...), path)...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("%s %v: %v", tc.name, mode, err)
			}
			if string(out) != tc.out || code != tc.code {
				t.Fatalf("%s %v: got exit %d output %q, want exit %d output %q", tc.name, mode, code, out, tc.code, tc.out)
			}
		}
	}
}
