package bytecode_test

import "testing"

// Module-exact instanceof (1.32.0): a class/interface RHS matches by declaring-module identity, not name.

func instanceofDonor() string {
	return "export class Config { func Config() {} }\nexport func make(): any { return Config(); }\n"
}

// Same-named class in two modules: qualified RHS is module-exact on both backends.
func TestParityInstanceofSameNameQualified(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"moda": instanceofDonor(), "modb": instanceofDonor()},
		`import io;
import moda;
import modb;
let x = modb.make();
io.println(x instanceof modb.Config);
io.println(x instanceof moda.Config);
let y = moda.make();
io.println(y instanceof moda.Config);
io.println(y instanceof modb.Config);
`, "true\nfalse\ntrue\nfalse\n")
}

// A bare from-imported class RHS binds to the declaring module exactly.
func TestParityInstanceofFromImportBare(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"moda": instanceofDonor(), "modb": instanceofDonor()},
		`import io;
import modb;
from moda import Config, make;
let a = make();
let b = modb.make();
io.println(a instanceof Config);
io.println(b instanceof Config);
`, "true\nfalse\n")
}

// An aliased import RHS resolves to the aliased module exactly.
func TestParityInstanceofAliasedImport(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"moda": instanceofDonor(), "modb": instanceofDonor()},
		`import io;
import moda as A;
import modb as B;
let x = B.make();
io.println(x instanceof B.Config);
io.println(x instanceof A.Config);
`, "true\nfalse\n")
}

// A subclass matches its resolved parent class exactly; a same-named third-module class does not.
func TestParityInstanceofInheritance(t *testing.T) {
	modules := map[string]string{
		"animals": "export class Animal { func Animal() {} }\n",
		"other":   "export class Animal { func Animal() {} }\n",
		"shapes":  "export class Base { func Base() {} }\nexport class Sub extends Base {}\nexport func makeSub(): any { return Sub(); }\n",
	}
	runMultiModuleParity(t, modules,
		`import io;
import animals;
import other;
import shapes;
class Dog extends animals.Animal {}
let d = Dog();
io.println(d instanceof Dog);
io.println(d instanceof animals.Animal);
io.println(d instanceof other.Animal);
let s = shapes.makeSub();
io.println(s instanceof shapes.Sub);
io.println(s instanceof shapes.Base);
io.println(s instanceof Dog);
`, "true\ntrue\nfalse\ntrue\ntrue\nfalse\n")
}

// A class implementing a cross-module interface matches it exactly; a same-named foreign interface does not.
func TestParityInstanceofInterfaceModuleExact(t *testing.T) {
	modules := map[string]string{
		"contracts": "export interface Greeter { func greet(): string; }\n",
		"other":     "export interface Greeter { func greet(): string; }\n",
	}
	runMultiModuleParity(t, modules,
		`import io;
import contracts;
import other;
class Dog implements contracts.Greeter { func greet(): string { return "woof"; } }
let d = Dog();
io.println(d instanceof contracts.Greeter);
io.println(d instanceof other.Greeter);
`, "true\nfalse\n")
}

// Same-module sub-interface inheritance still matches under module-exact.
func TestParityInstanceofSubInterfaceSameModule(t *testing.T) {
	runParity(t, `import io;
interface Base { func a(): int; }
interface Sub extends Base { func b(): int; }
class C implements Sub { func a(): int { return 1; } func b(): int { return 2; } }
let c = C();
io.println(c instanceof Sub);
io.println(c instanceof Base);
`, "true\ntrue\n")
}

// instanceof in expression position and in if/while conditions.
func TestParityInstanceofPositions(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"moda": instanceofDonor(), "modb": instanceofDonor()},
		`import io;
import moda;
import modb;
let x = modb.make();
let inExpr = x instanceof modb.Config;
io.println(inExpr);
if (x instanceof moda.Config) { io.println("if-moda"); } else { io.println("if-not-moda"); }
let n = 0;
while (n < 1 && x instanceof modb.Config) { io.println("while-modb"); n = n + 1; }
`, "true\nif-not-moda\nwhile-modb\n")
}

// `instanceof T` with T bound to a class stays name-based (unchanged mechanism), parity-consistent.
func TestParityInstanceofGenericTypeParam(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"moda": instanceofDonor(), "modb": instanceofDonor()},
		`import io;
import moda;
import modb;
func isType<T>(any v): bool { return v instanceof T; }
let x = modb.make();
io.println(isType<modb.Config>(x));
`, "true\n")
}

// A cross-module thrown error is still caught by its class name: catch-clause matching is unchanged by module-exact instanceof.
func TestParityInstanceofCatchUnchanged(t *testing.T) {
	modules := map[string]string{
		"errs": "export class AppError extends Error { func AppError(string m) { parent(m); } }\n" +
			"export func boom(): void { throw AppError(\"kaboom\"); }\n",
	}
	runMultiModuleParity(t, modules,
		`import io;
import errs;
func run(): void {
    try {
        errs.boom();
    } catch (errs.AppError e) {
        io.println("caught");
    }
}
run();
`, "caught\n")
}
