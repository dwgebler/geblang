package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownFieldAssignmentThrowsParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Svc {
    callable hook;
    int n = 0;
    func Svc() { this.hook = () => "real hook"; }
    func greet(): string { return "real greet"; }
    func poke(): void { this.extra = 1; }
}
class Sub extends Svc {
    func Sub() { parent(); }
}
class Open {
    dict<string, any> seen = {};
    func __set(string name, any value): void { this.seen[name] = value; }
}
func attempt(string label, callable body) {
    try { body(); io.println(label + ": ok"); } catch (Error e) { io.println(label + ": " + e.message); }
}
let s = Svc();
s.hook = () => "fake hook";
io.println((s.hook)());
let d = s as any;
attempt("method", () => { d.greet = () => "fake"; });
io.println(s.greet());
attempt("unknown", () => { d.nope = 1; });
attempt("inside", () => { s.poke(); });
attempt("compound", () => { s.n += 2; });
io.println(s.n);
let sub = Sub() as any;
attempt("inherited method", () => { sub.greet = 1; });
attempt("subclass unknown", () => { sub.nope = 1; });
sub.n = 5;
io.println(sub.n);
let o = Open();
o.anything = 3;
io.println(o.seen);
`)
	want := "fake hook\n" +
		"method: unknown field Svc.greet\nreal greet\n" +
		"unknown: unknown field Svc.nope\n" +
		"inside: unknown field Svc.extra\n" +
		"compound: ok\n2\n" +
		"inherited method: unknown field Sub.greet\n" +
		"subclass unknown: unknown field Sub.nope\n5\n" +
		"{\"anything\": 3}\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}

func TestUnknownFieldAssignmentCrossModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module donors;

export class Base {
    int n = 0;
    func Base() {}
    func greet(): string { return "base"; }
}
export class Magic {
    dict<string, any> seen = {};
    func Magic() {}
    func __set(string name, any value): void { this.seen[name] = value; }
}
export func setOn(any target, string which): string {
    try {
        if (which == "n") { target.n = 7; } else { target.nope = 1; }
        return "ok";
    } catch (Error e) {
        return e.message;
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "donors.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import donors;

class Local extends donors.Base {
    func Local() { parent(); }
}
class LocalMagic extends donors.Magic {
    func LocalMagic() { parent(); }
}
func attempt(string label, callable body) {
    try { body(); io.println(label + ": ok"); } catch (Error e) { io.println(label + ": " + e.message); }
}
let b = donors.Base() as any;
attempt("foreign unknown", () => { b.nope = 1; });
attempt("foreign method", () => { b.greet = 1; });
let l = Local() as any;
attempt("local subclass unknown", () => { l.nope = 1; });
attempt("local subclass method", () => { l.greet = 1; });
l.n = 4;
io.println(l.n);
io.println(donors.setOn(Local(), "n"));
io.println(donors.setOn(Local(), "nope"));
let m = LocalMagic() as any;
m.anything = 2;
io.println(m.seen);
`)
	want := "foreign unknown: unknown field Base.nope\n" +
		"foreign method: unknown field Base.greet\n" +
		"local subclass unknown: unknown field Local.nope\n" +
		"local subclass method: unknown field Local.greet\n4\n" +
		"ok\nunknown field Local.nope\n" +
		"{\"anything\": 2}\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
