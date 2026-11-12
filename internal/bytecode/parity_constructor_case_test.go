package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityConstructorVersusSameNamedMethod(t *testing.T) {
	dir := t.TempDir()
	donor := `module shapes;

export class Star {
    int points;
    func Star(int points = 5) { this.points = points; }
    func star(): string { return "star:${this.points}"; }
    static func STAR(): string { return "static"; }
}
`
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import shapes;

class Box {
    int n;
    func Box() { this.n = 1; }
    func Box(int n) { this.n = n; }
    func box(): string { return "box:${this.n}"; }
    func box(string label): string { return label + ":${this.n}"; }
}
class Crate extends Box {
    func Crate() { parent(7); }
    func crate(): string { return "crate/" + this.box(); }
}
class Plain {
    func plain(): string { return "no ctor"; }
}
class Nova extends shapes.Star {
    func Nova() { parent(9); }
    func nova(): string { return "nova/" + this.star(); }
}
class DefaultNova extends shapes.Star {
    func describe(): string { return parent.star() + "!"; }
}

io.println(Box().box());
io.println(Box(3).box());
io.println(Box(n: 5).box("b"));
io.println(Crate().crate());
io.println(Crate().box("c"));
io.println(Plain().plain());
io.println(shapes.Star().star());
io.println(shapes.Star(points: 8).star());
io.println(shapes.Star.STAR());
io.println(Nova().nova());
io.println(DefaultNova().describe());
io.println(shapes.Star(...[6]).star());
let bound = Box(4).box;
io.println(bound());
io.println(reflect.methods(Box));
io.println(reflect.constructors(Box).length());
io.println(reflect.methods(shapes.Star));
try { Plain(1); } catch (Error e) { io.println("rejected"); }
`)
	want := "box:1\nbox:3\nb:5\ncrate/box:7\nc:7\nno ctor\nstar:5\nstar:8\nstatic\nnova/star:9\nstar:5!\nstar:6\nbox:4\n[\"box\"]\n2\n[\"star\"]\nrejected\n"
	if ev != want || vm != want {
		t.Fatalf("constructor/method case split:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
