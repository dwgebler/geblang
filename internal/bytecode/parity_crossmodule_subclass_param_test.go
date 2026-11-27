package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrossModuleSubclassArgumentParity(t *testing.T) {
	dir := t.TempDir()
	module := `module parts;

export class Part {
    string name = "";
    ?Part next = null;
}
`
	if err := os.WriteFile(filepath.Join(dir, "parts.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import parts;

class Bolt extends parts.Part {}
class Hex extends Bolt {}
class Other {}

func describe(parts.Part p): string { return "part"; }
func maybe(?parts.Part p): string { return p == null ? "none" : "part"; }
func other(Other o): string { return "other"; }

let h = Hex();
h.next = Bolt();
io.println([describe(Bolt()), describe(Hex()), maybe(Hex()), maybe(null)]);
try {
    other(Bolt() as any);
} catch (TypeError e) {
    io.println("rejected");
}
`)

	want := "[\"part\", \"part\", \"part\", \"none\"]\nrejected\n"
	if ev != want || vm != want {
		t.Fatalf("cross-module subclass argument mismatch\nevaluator: %q\nvm:        %q\nwant:      %q", ev, vm, want)
	}
}
