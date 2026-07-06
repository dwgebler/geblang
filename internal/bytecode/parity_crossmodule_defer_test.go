package bytecode_test

import "testing"

// deferModDonor exports free functions and a class with a static method for the module-qualified defer tests.
const deferModDonor = `module dm;
import io;
export func cleanup(): void { io.println("cleanup ran"); }
export func cleanupArg(string label): void { io.println("cleanup " + label); }
export class Helper {
    static func staticHi(): void { io.println("static hi"); }
}
`

// A deferred module-qualified function (import alias) fires on both backends; the VM previously errored "deferred method call receiver is not an instance".
func TestParityDeferModuleFunctionAliased(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": deferModDonor},
		"import io;\nimport dm as m;\nfunc w(): void {\n    defer m.cleanup();\n    io.println(\"body\");\n}\nw();\nio.println(\"after\");\n",
		"body\ncleanup ran\nafter\n")
}

// A deferred module-qualified function through the unaliased import name.
func TestParityDeferModuleFunctionUnaliased(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": deferModDonor},
		"import io;\nimport dm;\nfunc w(): void {\n    defer dm.cleanup();\n    io.println(\"body\");\n}\nw();\n",
		"body\ncleanup ran\n")
}

// A deferred module-qualified function with a literal argument.
func TestParityDeferModuleFunctionWithArg(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": deferModDonor},
		"import io;\nimport dm as m;\nfunc w(): void {\n    defer m.cleanupArg(\"A\");\n    io.println(\"body\");\n}\nw();\n",
		"body\ncleanup A\n")
}

// A deferred cross-module static method (alias.Class.staticMethod()).
func TestParityDeferModuleStaticMethod(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": deferModDonor},
		"import io;\nimport dm as m;\nfunc w(): void {\n    defer m.Helper.staticHi();\n    io.println(\"body\");\n}\nw();\n",
		"body\nstatic hi\n")
}

// A deferred LOCAL static method still defers correctly (the thunk fallback also serves same-module static calls the specialized opcodes could not record).
func TestParityDeferLocalStaticMethod(t *testing.T) {
	runParity(t,
		"import io;\nclass Loc { static func hi(): void { io.println(\"loc hi\"); } }\nfunc w(): void {\n    defer Loc.hi();\n    io.println(\"body\");\n}\nw();\n",
		"body\nloc hi\n")
}

// Multiple defers (module func, module func with arg, native) run last-in-first-out identically on both backends.
func TestParityDeferModuleFunctionOrdering(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"dm": deferModDonor},
		"import io;\nimport dm as m;\nfunc w(): void {\n    defer m.cleanup();\n    defer m.cleanupArg(\"X\");\n    defer io.println(\"native\");\n    io.println(\"body\");\n}\nw();\n",
		"body\nnative\ncleanup X\ncleanup ran\n")
}

// A deferred module-qualified function that faults is caught by the host's try/catch on both backends.
func TestParityDeferModuleFunctionFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"boomm": "module boomm;\nexport func boom(): void { int z = 0; let q = 4 // z; }\n",
	},
		"import io;\nimport boomm as b;\nfunc w(): void { defer b.boom(); }\ntry {\n    w();\n} catch (RuntimeError e) {\n    io.println(\"caught: \" + e.message);\n}\nio.println(\"after\");\n",
		"caught: integer division by zero\nafter\n")
}
