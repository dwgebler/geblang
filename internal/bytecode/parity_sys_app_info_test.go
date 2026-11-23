package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParitySysAppInfoFromManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte("name: infoapp\nversion: 2.3.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import sys;
let info = sys.appInfo();
io.println("${info["name"]}|${info["version"]}|${(info["runtime"] as string).isEmpty()}|${info["packages"]}");
`)
	if ev != vm {
		t.Fatalf("output mismatch:\n  evaluator: %q\n  vm:        %q", ev, vm)
	}
	if want := "infoapp|2.3.4|false|[]\n"; ev != want {
		t.Fatalf("got %q, want %q", ev, want)
	}
}

func TestParitySysAppInfoWithoutManifest(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, t.TempDir(), `import io;
import sys;
let info = sys.appInfo();
io.println("${info["name"]}|${info["version"]}|${info.keys().sorted()}");
`)
	if ev != vm {
		t.Fatalf("output mismatch:\n  evaluator: %q\n  vm:        %q", ev, vm)
	}
	if want := `||["name", "packages", "runtime", "version"]` + "\n"; ev != want {
		t.Fatalf("got %q, want %q", ev, want)
	}
}
