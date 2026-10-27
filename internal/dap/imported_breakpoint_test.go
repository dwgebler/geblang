package dap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportedFunctionsAndInheritedMethodsHitTheirOwnBreakpoints(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.gb")
	donor := filepath.Join(dir, "donor.gb")
	if err := os.WriteFile(main, []byte(`import io;
import donor;
func host(): void { io.println(donor.work()); }
host();
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(donor, []byte(`module donor;
export class Base {
    func inherited(): int {
        int value = 7;
        return value;
    }
}
export class Child extends Base {
    func own(): int {
        return this.inherited();
    }
}
export func work(): int {
    return Child().own();
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	session := newTestSession(t, main)
	defer session.close()
	session.setBreakpoints(donor, 4, 10, 14)

	session.configurationDone()
	for _, line := range []int{14, 10, 4} {
		stopped := session.waitStopped()
		frames := session.stackTrace(stopped.ThreadID)
		first := frames[0]
		if int(first["line"].(float64)) != line || first["source"].(map[string]any)["path"] != donor {
			t.Fatalf("breakpoint at donor:%d stopped at %#v", line, first)
		}
		last := frames[len(frames)-1]
		if last["source"].(map[string]any)["path"] != main {
			t.Fatalf("caller frame lost its source: %#v", last)
		}
		session.continueAll()
	}
	session.waitTerminated()

	if session.collectedOutput() != "7\n" {
		t.Fatalf("output = %q", session.collectedOutput())
	}
}
