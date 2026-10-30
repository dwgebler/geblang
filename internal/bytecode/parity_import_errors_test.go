package bytecode_test

import (
	"testing"
)

func TestUncaughtImportTimeErrorTraces(t *testing.T) {
	throwing := `module throwing;
func boom(): int {
    throw ValueError("module boom");
}
let value = boom();
`
	cases := []struct {
		name    string
		modules map[string]string
		main    string
		want    string
	}{
		{"FunctionThrow", map[string]string{"throwing": throwing}, "import io;\n\nimport throwing;\n",
			"uncaught ValueError: module boom\n  at boom (line 3)\n  at <module throwing> (line 5)\n  at <top level> (line 3)"},
		{"InitFault", map[string]string{"faulting": `module faulting;
func div(int z): int {
    return 1 // z;
}
init {
    div(0);
}
`}, "import faulting;\n",
			"uncaught RuntimeError: integer division by zero\n  at div (line 3)\n  at <module faulting> (line 6)\n  at <top level> (line 1)"},
		{"NestedImport", map[string]string{"throwing": throwing, "outer": "module outer;\nimport throwing;\n"}, "import io;\nimport outer;\n",
			"uncaught ValueError: module boom\n  at boom (line 3)\n  at <module throwing> (line 5)\n  at <module outer> (line 2)\n  at <top level> (line 2)"},
		{"ImportInsideFunction", map[string]string{"throwing": throwing}, "func load(): void {\n    import throwing;\n}\nload();\n",
			"uncaught ValueError: module boom\n  at boom (line 3)\n  at <module throwing> (line 5)\n  at load (line 2)\n  at <top level> (line 4)"},
		{"StaticRejection", map[string]string{"direct": "module direct;\nthrow ValueError(\"x\");\n"}, "import direct;\n",
			"uncaught RuntimeError: module direct: free-standing top-level statement is not allowed in a module file; wrap imperative setup in an init { ... } block\n  at <top level> (line 1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, vm := uncaughtOnBothBackendsModulesDir(t, tc.modules, tc.main)
			if ev != vm || vm != tc.want {
				t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, tc.want)
			}
		})
	}
}

func TestParityImportTimeErrorIsCatchable(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "throwing", `module throwing;
func boom(): int { throw ValueError("module boom"); }
let value = boom();
`)
	runParityModulesDir(t, dir, `import io;
try {
    import throwing;
} catch (ValueError e) {
    io.println("caught ${e.message}");
}
`, "caught module boom\n")
}

func TestParityInheritedConstructors(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Z { int z = 4; func Z() { this.z = 12; } }
class D extends Z {}
class DD extends D {}
class EmptyD extends Z { func EmptyD() {} }
class X extends D { func X() {} }
class Explicit extends D { func Explicit() { parent(); } }
class Req { int r = 1; func Req(int v) { this.r = v; } }
class R extends Req {}
class Def { int d = 0; func Def(int v = 9) { this.d = v; } }
class DefKid extends Def {}
class AppError extends Error { func AppError(string m) { parent(m); } }
class SubApp extends AppError {}
io.println("${D().z} ${DD().z} ${EmptyD().z} ${X().z} ${Explicit().z} ${R().r} ${DefKid().d} ${SubApp("s").message}");
try { D(1); } catch (Error e) { io.println(e.message); }
`)
	want := "12 12 12 12 12 1 9 s\nD constructor expects no arguments\n"
	if ev != vm || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestParityInheritedConstructorsAcrossModules(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "bases", `module bases;
export class Z { int z = 4; func Z() { this.z = 12; } }
export class D extends Z {}
export class Req { int r = 1; func Req(int v) { this.r = v; } }
`)
	runParityModulesDir(t, dir, `import io;
import bases;
class E extends bases.Z { func E() {} }
class C extends bases.Z {}
class CD extends bases.D {}
class X extends bases.D { func X() {} }
class R extends bases.Req {}
io.println("${E().z} ${C().z} ${CD().z} ${X().z} ${bases.D().z} ${R().r}");
`, "12 12 12 12 12 1\n")
}

func TestUncaughtInheritedConstructorTraces(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"ConstructorlessChild", `class Z {
    func Z() {
        throw ValueError("ctor boom");
    }
}
class D extends Z {}
func build(): any { return D(); }

build();
`, "uncaught ValueError: ctor boom\n  at Z.Z (line 3)\n  at build (line 7)\n  at <top level> (line 9)"},
		{"EmptyConstructorChain", `class Z {
    func Z() {
        throw ValueError("deep");
    }
}
class Y extends Z { func Y() {} }
class X extends Y { func X() {} }
X();
`, "uncaught ValueError: deep\n  at Z.Z (line 3)\n  at Y.Y (line 6)\n  at X.X (line 7)\n  at <top level> (line 8)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, vm := uncaughtOnBothBackends(t, tc.source)
			if ev != vm || vm != tc.want {
				t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, tc.want)
			}
		})
	}
	ev, vm := uncaughtOnBothBackendsModulesDir(t, map[string]string{"zboom": `module zboom;
export class Z {
    func Z() {
        throw ValueError("remote boom");
    }
}
`}, `import zboom;
class E extends zboom.Z {
    func E() {}
}
E();
`)
	if want := "uncaught ValueError: remote boom\n  at Z.Z (line 4)\n  at E.E (line 3)\n  at <top level> (line 5)"; ev != vm || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}
