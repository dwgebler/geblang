package bytecode_test

import (
	"strings"
	"testing"
)

// boundaryFaultDonor exposes an instance method that faults non-typed.
const boundaryFaultDonor = `module bmod;
export class Helper {
    func Helper() {}
    func blow(): void { int z = 0; let q = 4 // z; }
}
`

// A cross-module method fault reached through a DEFERRED call is caught with the clean message, not the VM's "deferred method call ...: uncaught ..." blob.
func TestParityCrossModuleDeferredMethodFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"bmod": boundaryFaultDonor},
		"import io;\nimport bmod;\nfunc work(): void {\n    let h = bmod.Helper();\n    defer h.blow();\n}\ntry {\n    work();\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// A cross-module fault raised in a `del`-fired destructor body is caught with the clean message, not the "del: destructor: uncaught ..." blob.
func TestParityCrossModuleDelDestructorFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"bmod": boundaryFaultDonor},
		"import io;\nimport bmod;\nclass LocalRes {\n    func LocalRes() {}\n    func ~LocalRes() { let h = bmod.Helper(); h.blow(); }\n}\ntry {\n    let r = LocalRes();\n    del r;\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// A local deferred FUNCTION fault is caught with the clean message too (the defer normalization is not cross-module-specific).
func TestParityDeferredFunctionFaultCaught(t *testing.T) {
	runParity(t,
		"import io;\nfunc boom(): void { int z = 0; let q = 4 // z; }\nfunc work(): void { defer boom(); }\ntry {\n    work();\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// An UNCAUGHT cross-module fault in a `del`-fired destructor renders the clean single trace identically on both backends.
func TestParityCrossModuleDelDestructorFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"bmod": boundaryFaultDonor},
		"import io;\nimport bmod;\nclass LocalRes {\n    func LocalRes() {}\n    func ~LocalRes() { let h = bmod.Helper(); h.blow(); }\n}\nlet r = LocalRes();\ndel r;\nio.println(\"no fault\");\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at Helper.blow (line 4)") {
		t.Fatalf("uncaught render missing cross-module destructor frame:\n%s", vm)
	}
}
