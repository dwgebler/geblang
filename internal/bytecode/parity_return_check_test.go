package bytecode_test

import "testing"

func TestParityConcreteReturnChecks(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
type Ints = list<int>;
func g1(): list<int> { list x = ["s"]; return x; }
func g2(list x): list<int> { return x; }
func g3(): dict<string, int> { dict x = {"a": "s"}; return x; }
func g4(): int[] { list x = ["s"]; return x; }
func g5(): ?int[] { return null; }
func g6(): list<int> | string { list x = [1.5]; return x; }
func g7(): Ints { list x = ["s"]; return x; }
func g8(): ?int { return null; }
func g9(): float { return 2; }
func g10(): decimal { return 2; }
func g11(): list<int> { list x = [1, 2]; return x; }
class K { func m(): int { any v = "s"; return v as string; } }
class L extends K {}
for (any f in [g1, func(): any { return g2(["s"]); }, g3, g4, g5, g6, g7, g8, g9, g10, g11, func(): any { return K().m(); }, func(): any { return L().m(); }]) {
    try { io.println(f()); } catch (TypeError e) { io.println(e.message); }
}
io.println(typeof(g9()));
io.println(typeof(g10()));
`)
	want := "g1 expects list<int> return, got list<string>\n" +
		"g2 expects list<int> return, got list<string>\n" +
		"g3 expects dict<string, int> return, got dict<string,string>\n" +
		"g4 expects int[] return, got list<string>\n" +
		"null\n" +
		"g6 expects list<int> | string return, got list<decimal>\n" +
		"g7 expects list<int> return, got list<string>\n" +
		"null\n2\n2.0000000000\n[1, 2]\n" +
		"K.m expects int return, got string\n" +
		"K.m expects int return, got string\n" +
		"float\ndecimal\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityIntLiteralCoercesToExpectedNumericType(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
float f = 3;
decimal d = 3;
?float nf = 4;
?decimal nd = 4;
func half(): float { return 1; }
io.println("${f} ${typeof(f)} ${d} ${typeof(d)}");
io.println("${nf} ${typeof(nf)} ${nd} ${typeof(nd)}");
io.println(half() / 2);
`)
	want := "3 float 3.0000000000 decimal\n4 float 4.0000000000 decimal\n0.5\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityNonBoolConditionsRaiseTypeError(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
any n = 5;
any t = true;
try { if (n) { io.println("x"); } } catch (TypeError e) { io.println(e.message); }
try { while (n) { break; } } catch (TypeError e) { io.println(e.message); }
try { any r = n ? 1 : 2; } catch (TypeError e) { io.println(e.message); }
try { any r = !n; } catch (TypeError e) { io.println(e.message); }
try { any r = t && n; } catch (TypeError e) { io.println(e.message); }
try { any r = n || t; } catch (TypeError e) { io.println(e.message); }
`)
	want := "condition must be bool, got int\n" +
		"condition must be bool, got int\n" +
		"condition must be bool, got int\n" +
		"! expects bool, got int\n" +
		"condition must be bool, got int\n" +
		"condition must be bool, got int\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityCrossModuleConcreteReturnChecks(t *testing.T) {
	dir := moduleDirFor(t, map[string]string{"donor": `module donor;
export class Base {
    func Base() {}
    func ids(): list<int> { list x = ["s"]; return x; }
}
export class Sub extends Base {
    func Sub() { parent(); }
    func ratio(): float { return 2; }
}
export func scores(): dict<string, int> { dict x = {"a": "b"}; return x; }
`})
	runParityModulesDir(t, dir, `import donor;
import io;
let sub = donor.Sub();
try { donor.Base().ids(); } catch (TypeError e) { io.println(e.message); }
try { sub.ids(); } catch (TypeError e) { io.println(e.message); }
io.println(typeof(sub.ratio()));
try { donor.scores(); } catch (TypeError e) { io.println(e.message); }
`, "Base.ids expects list<int> return, got list<string>\n"+
		"Base.ids expects list<int> return, got list<string>\n"+
		"float\n"+
		"scores expects dict<string, int> return, got dict<string,string>\n")
}

func TestParityClassInterfaceGenericReturnChecks(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
interface Shape { func area(): int; }
class Sq implements Shape { func Sq() {} func area(): int { return 4; } }
class Big extends Sq { func Big() { parent(); } }
class Dog { func Dog() {} func self(): Dog { return this; } }
class Cat { func Cat() {} }
enum Color { Red, Green }
class Box<T> {
    T v;
    func Box(T v) { this.v = v; }
    func get(): T { list raw = [5]; return raw[0]; }
    func maker(): func { return func(): T { list raw = [1]; return raw[0]; }; }
}
func asShape(list xs): Shape { return xs[0]; }
func asDog(list xs): Dog { return xs[0]; }
func asMaybe(list xs): ?Dog { return xs[0]; }
func asEither(list xs): Dog | Cat { return xs[0]; }
func asColor(list xs): Color { return xs[0]; }
func asFn(list xs): func { return xs[0]; }
func pick<T>(list xs): T { return xs[0]; }
func fall(bool b): Dog { if (b) { return Dog(); } }
func gen(): iterable { yield 1; }
func run(string label, func f): void {
    try { f(); io.println(label + " ok"); } catch (TypeError e) { io.println(label + " " + e.message); }
}
run("sq", func(): any { return asShape([Sq()]); });
run("big", func(): any { return asShape([Big()]); });
run("dog-shape", func(): any { return asShape([Dog()]); });
run("cat-dog", func(): any { return asDog([Cat()]); });
run("null-dog", func(): any { return asDog([null]); });
run("null-maybe", func(): any { return asMaybe([null]); });
run("cat-either", func(): any { return asEither([Cat()]); });
run("sq-either", func(): any { return asEither([Sq()]); });
run("enum", func(): any { return asColor([Color.Red]); });
run("int-enum", func(): any { return asColor([1]); });
run("fn", func(): any { return asFn([func(): int { return 1; }]); });
run("int-fn", func(): any { return asFn([1]); });
run("pick-free", func(): any { return pick([1]); });
run("pick-bound", func(): any { return pick<string>([1]); });
run("fall", func(): any { return fall(false); });
run("self", func(): any { return Dog().self(); });
Box<string> bs = Box("x");
any raw = Box(1);
run("box-bound", func(): any { return bs.get(); });
run("box-free", func(): any { return raw.get(); });
run("closure", func(): any { func f = bs.maker(); return f(); });
run("gen", func(): any { return gen(); });
`)
	want := "sq ok\nbig ok\n" +
		"dog-shape asShape expects Shape return, got Dog\n" +
		"cat-dog asDog expects Dog return, got Cat\n" +
		"null-dog asDog expects Dog return, got null\n" +
		"null-maybe ok\ncat-either ok\n" +
		"sq-either asEither expects Dog | Cat return, got Sq\n" +
		"enum ok\n" +
		"int-enum asColor expects Color return, got int\n" +
		"fn ok\n" +
		"int-fn asFn expects func return, got int\n" +
		"pick-free ok\n" +
		"pick-bound pick expects T return, got int\n" +
		"fall fall expects Dog return, got null\n" +
		"self ok\n" +
		"box-bound Box.get expects T return, got int\n" +
		"box-free ok\n" +
		"closure <closure> expects T return, got int\n" +
		"gen ok\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityClosureTypeErrorsNameTheClosure(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
func f = func(int x): int { return x; };
func g = func(): int { any v = "a"; return v; };
any s = "a";
try { f(s); } catch (TypeError e) { io.println(e.message); }
try { g(); } catch (TypeError e) { io.println(e.message); }
`)
	want := "<closure> expects int for parameter 'x', got string\n<closure> expects int return, got string\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityCrossModuleClassInterfaceReturnChecks(t *testing.T) {
	dir := moduleDirFor(t, map[string]string{"shapes": `module shapes;
export interface Shape { func area(): int; }
export class Sq implements Shape { func Sq() {} func area(): int { return 4; } }
export class Dog { func Dog() {} }
export class Box<T> { T v; func Box(T v) { this.v = v; } func get(): T { list raw = [5]; return raw[0]; } }
export func make(list xs): Shape { return xs[0]; }
export class Factory { func Factory() {} func build(list xs): Sq { return xs[0]; } }
`})
	runParityModulesDir(t, dir, `import shapes;
import io;
class Local implements shapes.Shape { func Local() {} func area(): int { return 1; } }
class Sub extends shapes.Factory { func Sub() { parent(); } }
func mine(list xs): shapes.Shape { return xs[0]; }
func run(string label, func f): void {
    try { f(); io.println(label + " ok"); } catch (TypeError e) { io.println(label + " " + e.message); }
}
run("make-local", func(): any { return shapes.make([Local()]); });
run("make-dog", func(): any { return shapes.make([shapes.Dog()]); });
run("mine-sq", func(): any { return mine([shapes.Sq()]); });
run("mine-dog", func(): any { return mine([shapes.Dog()]); });
run("sub-ok", func(): any { return Sub().build([shapes.Sq()]); });
run("sub-dog", func(): any { return Sub().build([shapes.Dog()]); });
shapes.Box<string> b = shapes.Box("x");
run("box", func(): any { return b.get(); });
`, "make-local ok\n"+
		"make-dog make expects Shape return, got Dog\n"+
		"mine-sq ok\n"+
		"mine-dog mine expects shapes.Shape return, got Dog\n"+
		"sub-ok ok\n"+
		"sub-dog Factory.build expects Sq return, got Dog\n"+
		"box Box.get expects T return, got int\n")
}
