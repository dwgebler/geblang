package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const zeroParamTypeArgsProgram = `import io;

class User { int id = 0; }

func zero<T>(): bool { return User() instanceof T; }
async func azero<T>(): bool { return User() instanceof T; }
func gzero<T>(): generator<bool> { yield User() instanceof T; }
func tail<T>(): bool { return zero<T>(); }
func logged(callable f): callable { return f; }
@logged
func dzero<T>(): bool { return User() instanceof T; }
func rec<T>(int n): bool {
    if (n == 0) { return zero<T>(); }
    return rec<T>(n - 1);
}
class Svc {
    func zero<T>(): bool { return User() instanceof T; }
    static func szero<T>(): bool { return User() instanceof T; }
}
func noop<T>(): bool { return true; }
func probe<T>(any v): bool { return v instanceof T; }

io.println("${zero<User>()} ${zero<string>()}");
let task = azero<User>();
let ar = await task;
io.println(ar);
for (v in gzero<User>()) { io.println(v); }
io.println("${tail<User>()} ${tail<string>()}");
io.println(dzero<User>());
io.println(rec<User>(3));
io.println("${Svc().zero<User>()} ${Svc.szero<User>()}");
let u = User();
noop<User>();
io.println(probe(u));
gzero<User>();
io.println(probe(u));
`

func TestZeroParamExplicitTypeArgsParity(t *testing.T) {
	ev, vm := parityOutputs(t, zeroParamTypeArgsProgram)
	want := "true false\ntrue\ntrue\ntrue false\ntrue\ntrue\ntrue true\nfalse\nfalse\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}

func TestZeroParamExplicitTypeArgsCrossModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module zerolib;

export class Item { int n = 0; }
export func isA<T>(): bool { return Item() instanceof T; }
export func noop<T>(): bool { return true; }
export func probe<T>(any v): bool { return v instanceof T; }
`
	if err := os.WriteFile(filepath.Join(dir, "zerolib.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import zerolib;

io.println("${zerolib.isA<zerolib.Item>()} ${zerolib.isA<string>()}");
zerolib.noop<zerolib.Item>();
io.println(zerolib.probe(zerolib.Item()));
`)
	want := "true false\nfalse\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}
