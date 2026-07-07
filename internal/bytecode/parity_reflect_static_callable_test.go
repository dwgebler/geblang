package bytecode_test

// reflect.staticMethod returns an invocable callable for a cross-module class (9.45); local, inherited, overloaded, and missing cases stay in parity.

import "testing"

const staticCallableDonor = `module sdonor;
export class Gadget {
    int v;
    func Gadget(int v) { this.v = v; }
    static func build(int seedValue): Gadget { return Gadget(seedValue * 10); }
    static func build(int seedValue, int bump): Gadget { return Gadget(seedValue * 10 + bump); }
}
export class SubGadget extends Gadget {
    func SubGadget(int v) { parent(v); }
}
`

// Cross-module static reached both directly and through a variable is invocable and picks the right overload by arity.
func TestParityReflectStaticCallableCrossModule(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"sdonor": staticCallableDonor}, `import io;
import reflect;
import sdonor as sd;
let m = reflect.staticMethod(sd.Gadget, "build");
io.println("callable:${m != null}");
io.println("one:${m(3).v}");
io.println("two:${m(4, 7).v}");
let c = sd.Gadget;
let mv = reflect.staticMethod(c, "build");
io.println("viaVar:${mv(5).v}");
`, "callable:true\none:30\ntwo:47\nviaVar:50\n")
}

// A missing cross-module static reflects to null (parity with the evaluator).
func TestParityReflectStaticCallableCrossModuleMissing(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"sdonor": staticCallableDonor}, `import io;
import reflect;
import sdonor as sd;
io.println("missing:${reflect.staticMethod(sd.Gadget, "nope") == null}");
`, "missing:true\n")
}

// An inherited static is not surfaced by reflect.staticMethod on either backend (own statics only).
func TestParityReflectStaticCallableCrossModuleInherited(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"sdonor": staticCallableDonor}, `import io;
import reflect;
import sdonor as sd;
io.println("inherited:${reflect.staticMethod(sd.SubGadget, "build") == null}");
`, "inherited:true\n")
}

// The local (same-module) static callable path still works after the cross-module route was added.
func TestParityReflectStaticCallableLocal(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Gadget {
    int v;
    func Gadget(int v) { this.v = v; }
    static func build(int seedValue): Gadget { return Gadget(seedValue * 10); }
}
let m = reflect.staticMethod(Gadget, "build");
io.println("callable:${m != null}");
io.println("v:${m(3).v}");
`, "callable:true\nv:30\n")
}
