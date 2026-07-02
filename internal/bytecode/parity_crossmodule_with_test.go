package bytecode_test

import (
	"strings"
	"testing"
)

// withDonorModule members fault or throw inside __enter and __exit for cross-module context-manager tests.
const withDonorModule = `module withmod;
export class EnterFault {
    func EnterFault() {}
    func __enter(): EnterFault { int z = 0; let q = 4 // z; return this; }
    func __exit(): void {}
}
export class EnterThrow {
    func EnterThrow() {}
    func __enter(): EnterThrow { throw ValueError("enter boom"); }
    func __exit(): void {}
}
export class ExitFault {
    func ExitFault() {}
    func __enter(): ExitFault { return this; }
    func __exit(): void { int z = 0; let q = 4 // z; }
}
export class ExitThrow {
    func ExitThrow() {}
    func __enter(): ExitThrow { return this; }
    func __exit(): void { throw ValueError("exit boom"); }
}
`

func withMods() map[string]string { return map[string]string{"withmod": withDonorModule} }

// A caught non-typed fault in a cross-module __enter exposes the clean message.
func TestParityWithCrossModuleEnterFaultCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\ntry {\n    with (r = withmod.EnterFault()) { io.println(\"body\"); }\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"MSG=[integer division by zero]\nafter\n")
}

// A typed throw in a cross-module __enter is caught by its real class (not RuntimeError).
func TestParityWithCrossModuleEnterThrowCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\ntry {\n    with (r = withmod.EnterThrow()) { io.println(\"body\"); }\n} catch (ValueError e) {\n    io.println(\"VE=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"VE=[enter boom]\nafter\n")
}

// A caught non-typed fault in a cross-module __exit exposes the clean message; the body runs first.
func TestParityWithCrossModuleExitFaultCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\ntry {\n    with (r = withmod.ExitFault()) { io.println(\"body\"); }\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"body\nMSG=[integer division by zero]\nafter\n")
}

// A typed throw in a cross-module __exit is caught by its real class.
func TestParityWithCrossModuleExitThrowCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\ntry {\n    with (r = withmod.ExitThrow()) { io.println(\"body\"); }\n} catch (ValueError e) {\n    io.println(\"VE=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"body\nVE=[exit boom]\nafter\n")
}

// Same-module control: a fault in a same-module __enter is clean too (no with: wrapping).
func TestParityWithSameModuleEnterFaultCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nclass Local {\n    func Local() {}\n    func __enter(): Local { int z = 0; let q = 4 // z; return this; }\n    func __exit(): void {}\n}\ntry {\n    with (r = Local()) { io.println(\"body\"); }\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"MSG=[integer division by zero]\nafter\n")
}

// A subclass whose __enter is inherited from a cross-module parent invokes it, faulting cleanly.
func TestParityWithSubclassInheritedEnterFaultCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\nclass SubE extends withmod.EnterFault { func SubE() { parent(); } }\ntry {\n    with (r = SubE()) { io.println(\"body\"); }\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"MSG=[integer division by zero]\nafter\n")
}

// A subclass whose __exit is inherited from a cross-module parent invokes it, throwing its real class.
func TestParityWithSubclassInheritedExitThrowCaught(t *testing.T) {
	runMultiModuleParity(t, withMods(),
		"import io;\nimport withmod;\nclass SubX extends withmod.ExitThrow { func SubX() { parent(); } }\ntry {\n    with (r = SubX()) { io.println(\"body\"); }\n} catch (ValueError e) {\n    io.println(\"VE=[${e.message}]\");\n}\nio.println(\"after\");\n",
		"body\nVE=[exit boom]\nafter\n")
}

// An UNCAUGHT cross-module __enter fault renders the full trace identically on both backends.
func TestParityWithCrossModuleEnterFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, withMods(),
		"import io;\nimport withmod;\nlet s = withmod.EnterFault();\nwith (r = s) { io.println(\"body\"); }\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at EnterFault.__enter (line 4)") {
		t.Fatalf("uncaught render missing cross-module frame:\n%s", vm)
	}
}

// An UNCAUGHT cross-module __exit fault renders the full trace identically on both backends.
func TestParityWithCrossModuleExitFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, withMods(),
		"import io;\nimport withmod;\nlet s = withmod.ExitFault();\nwith (r = s) { io.println(\"body\"); }\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at ExitFault.__exit (line 15)") {
		t.Fatalf("uncaught render missing cross-module frame:\n%s", vm)
	}
}

// An UNCAUGHT fault in a cross-module inherited __enter renders the full trace identically on both backends.
func TestParityWithSubclassInheritedEnterFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, withMods(),
		"import io;\nimport withmod;\nclass SubE extends withmod.EnterFault { func SubE() { parent(); } }\nlet s = SubE();\nwith (r = s) { io.println(\"body\"); }\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at EnterFault.__enter (line 4)") {
		t.Fatalf("uncaught render missing inherited cross-module frame:\n%s", vm)
	}
}
