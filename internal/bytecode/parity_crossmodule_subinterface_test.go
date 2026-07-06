package bytecode_test

import "testing"

// Cross-module sub-interface parent chains (finding 9.17): a foreign interface's Parents are resolved through the loader so instanceof reaches the whole chain.

func subInterfaceDonor() string {
	return "export interface Base { func base(): string; }\n" +
		"export interface Mid extends Base { func mid(): string; }\n" +
		"export interface Sub extends Mid { func sub(): string; }\n" +
		"export class Circle implements Sub {\n" +
		"    func base(): string { return \"b\"; }\n" +
		"    func mid(): string { return \"m\"; }\n" +
		"    func sub(): string { return \"s\"; }\n" +
		"}\n"
}

// A local class implementing a foreign Sub matches every foreign ancestor interface (2- and 3-level chain).
func TestParityCrossModuleSubInterfaceLocalClass(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapes": subInterfaceDonor()},
		`import io;
import shapes;
class LocalC implements shapes.Sub {
    func base(): string { return "b"; }
    func mid(): string { return "m"; }
    func sub(): string { return "s"; }
}
let c = LocalC();
io.println(c instanceof shapes.Sub);
io.println(c instanceof shapes.Mid);
io.println(c instanceof shapes.Base);
`, "true\ntrue\ntrue\n")
}

// A foreign class implementing the foreign Sub matches every foreign ancestor interface.
func TestParityCrossModuleSubInterfaceForeignClass(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapes": subInterfaceDonor() + "export func make(): any { return Circle(); }\n"},
		`import io;
import shapes;
let c = shapes.make();
io.println(c instanceof shapes.Sub);
io.println(c instanceof shapes.Mid);
io.println(c instanceof shapes.Base);
`, "true\ntrue\ntrue\n")
}

// A local sub-interface extending a foreign base matches the foreign base.
func TestParityCrossModuleLocalSubInterfaceForeignBase(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapes": subInterfaceDonor()},
		`import io;
import shapes;
interface LocalSub extends shapes.Base { func local(): string; }
class L implements LocalSub {
    func base(): string { return "b"; }
    func local(): string { return "l"; }
}
let c = L();
io.println(c instanceof LocalSub);
io.println(c instanceof shapes.Base);
`, "true\ntrue\n")
}

// A same-named local interface stays distinct from the foreign base under module-exact instanceof.
func TestParityCrossModuleSubInterfaceSameNameNegative(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapes": subInterfaceDonor()},
		`import io;
import shapes;
interface Base { func localBase(): string; }
class LC implements shapes.Sub {
    func base(): string { return "b"; }
    func mid(): string { return "m"; }
    func sub(): string { return "s"; }
}
let c = LC();
io.println(c instanceof Base);
io.println(c instanceof shapes.Base);
io.println(c instanceof shapes.Mid);
`, "false\ntrue\ntrue\n")
}
