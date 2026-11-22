package bytecode_test

import "testing"

func TestTestPatchParity(t *testing.T) {
	ev, vm := parityModuleOutputs(t, nil, `import io;
import test;

func shout(any next): any {
    return func(string s): string { return (next(s) as string).upper(); };
}

class Greeter {
    func greet(string who): string { return "hi " + who; }
    func twice(string who): string { return this.greet(who) + "," + this.greet(who); }
    @shout
    func loud(string s): string { return s; }
    func __string(): string { return "Greeter"; }
}

class Polite extends Greeter {
    func greet(string who): string { return "dear " + parent.greet(who); }
}

let g = Greeter();
let p = Polite();
test.patch(Greeter, "greet", (Greeter self, string who) => "yo " + who);
io.println(g.greet("a"));
io.println(g.twice("b"));
io.println(p.greet("c"));
let bound = g.greet;
io.println(bound("d"));
io.println(["e", "f"].map((w) => g.greet(w)));
test.restore(Greeter, "greet");
test.patch(p, "greet", (Polite self, string who) => "only " + who);
io.println(p.greet("g"));
io.println(Polite().greet("h"));
test.patch(Greeter, "loud", (Greeter self, string s) => "quiet");
io.println(g.loud("x"));
test.patch(Greeter, "__string", (Greeter self) => "Fake");
io.println("${g}");
try { test.patch(Greeter, "nope", (Greeter self) => 1); } catch (ValueError e) { io.println(e.message); }
try { test.patch(Greeter, "greet", (Greeter self) => 1); } catch (TypeError e) { io.println(e.message); }
test.restoreAll();
io.println(g.greet("i") + " " + p.greet("j") + " " + g.loud("k") + " ${g}");
`)
	want := "yo a\nyo b,yo b\ndear yo c\nyo d\n[\"yo e\", \"yo f\"]\n" +
		"only g\ndear hi h\nquiet\nFake\n" +
		"test.patch: Greeter has no method nope\n" +
		"test.patch: the replacement for Greeter.greet must accept the receiver followed by 1 argument(s)\n" +
		"hi i dear hi j K Greeter\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}

func TestTestPatchCrossModuleParity(t *testing.T) {
	module := `module shapes;

export class Shape {
    func area(): int { return 1; }
    func report(): string { return "area=${this.area()}"; }
}
export func describe(Shape s): string { return s.report(); }
`
	ev, vm := parityModuleOutputs(t, map[string]string{"shapes.gb": module}, `import io;
import test;
import shapes;

class Square extends shapes.Shape {
    func report(): string { return "square " + parent.report(); }
}

test.patch(shapes.Shape, "area", (shapes.Shape self) => 9);
io.println(shapes.describe(shapes.Shape()));
io.println(Square().report());
io.println(shapes.describe(Square()));
test.restoreAll();
let s = shapes.Shape();
test.patch(s, "report", (shapes.Shape self) => "patched");
io.println(shapes.describe(s));
io.println(shapes.describe(shapes.Shape()));
test.restoreAll();
`)
	want := "area=9\nsquare area=9\nsquare area=9\npatched\narea=1\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
