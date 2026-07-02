package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// faultDonorModule members fault as non-typed (static + instance), typed, and via a faulting static __deserialize factory.
const faultDonorModule = `module faultmod;
export class Base {
    int amount;
    func Base(int amount) { this.amount = amount; }
    static func boom(): int { int z = 0; return 4 // z; }
    func instBoom(): int { int z = 0; return this.amount // z; }
    static func typedBoom(): int { throw ValueError("typed boom"); }
    static func __deserialize(dict d): Base { int z = 0; return Base(d["amount"] // z); }
}
`

// A caught non-typed fault from a cross-module STATIC call exposes the clean message, not the VM's rendered blob.
func TestParityCrossModuleStaticFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport faultmod;\ntry {\n    let x = faultmod.Base.boom();\n    io.println(\"no fault\");\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// A caught non-typed fault from a cross-module INSTANCE method is clean too.
func TestParityCrossModuleInstanceFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport faultmod;\ntry {\n    let b = faultmod.Base(10);\n    let x = b.instBoom();\n    io.println(\"no fault\");\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// A caught non-typed fault from a cross-module inherited __deserialize factory surfaces cleanly through the native call path.
func TestParityCrossModuleDeserializeFaultCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport json;\nimport faultmod;\nclass Sub extends faultmod.Base {\n    func Sub(int amount) { parent(amount); }\n}\ntry {\n    let x = json.parseAs(\"{\\\"amount\\\": 4}\", Sub);\n    io.println(\"no fault\");\n} catch (RuntimeError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[RuntimeError: integer division by zero]\nafter\n")
}

// Regression guard: a TYPED throw across a module boundary is still caught by class with its message intact.
func TestParityCrossModuleTypedThrowCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport faultmod;\ntry {\n    let x = faultmod.Base.typedBoom();\n} catch (ValueError e) {\n    io.println(\"MSG=[${e}]\");\n}\nio.println(\"after\");\n",
		"MSG=[ValueError: typed boom]\nafter\n")
}

// runMultiModuleUncaughtParity runs mainSrc against named modules on both backends, requires each to fault uncaught, and returns the two rendered error strings.
func runMultiModuleUncaughtParity(t *testing.T, modules map[string]string, mainSrc string) (evStr, vmStr string) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range modules {
		if err := os.WriteFile(filepath.Join(dir, name+".gb"), []byte(src), 0o644); err != nil {
			t.Fatalf("write module %s: %v", name, err)
		}
	}

	p := parser.New(lexer.New(mainSrc))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	_, evErr := ev.Eval(program)
	if evErr == nil {
		t.Fatalf("evaluator: expected an uncaught error, got none (stdout %q)", evOut.String())
	}

	chunk, err := bytecode.Compile(program, []byte(mainSrc), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	vmErr := vm.Run()
	if vmErr == nil {
		t.Fatalf("vm: expected an uncaught error, got none (stdout %q)", vmOut.String())
	}
	return evErr.Error(), vmErr.Error()
}

// assertCleanUncaughtParity requires byte-identical renders in the clean single-render form (no doubled prefix, no flattened blob).
func assertCleanUncaughtParity(t *testing.T, evStr, vmStr string) {
	t.Helper()
	if evStr != vmStr {
		t.Fatalf("uncaught render divergence:\n--- eval ---\n%s\n--- vm ---\n%s", evStr, vmStr)
	}
	if strings.Contains(vmStr, "uncaught RuntimeError: uncaught") || strings.Contains(vmStr, "RuntimeError: uncaught RuntimeError") {
		t.Fatalf("uncaught render is a doubled/flattened blob, not the clean form:\n%s", vmStr)
	}
	if !strings.HasPrefix(vmStr, "uncaught RuntimeError: integer division by zero\n") {
		t.Fatalf("uncaught render missing clean leading message:\n%s", vmStr)
	}
}

// An UNCAUGHT non-typed fault from a cross-module static call still renders the full trace, identically on both backends.
func TestParityCrossModuleStaticFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport faultmod;\nlet x = faultmod.Base.boom();\nio.println(\"no fault\");\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at Base.boom (line 5)") {
		t.Fatalf("uncaught render missing cross-module frame:\n%s", vm)
	}
}

// An UNCAUGHT non-typed fault from a cross-module instance method renders the full trace identically on both backends.
func TestParityCrossModuleInstanceFaultUncaught(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"faultmod": faultDonorModule},
		"import io;\nimport faultmod;\nlet b = faultmod.Base(10);\nlet x = b.instBoom();\nio.println(\"no fault\");\n")
	assertCleanUncaughtParity(t, ev, vm)
	if !strings.Contains(vm, "at Base.instBoom (line 6)") {
		t.Fatalf("uncaught render missing cross-module frame:\n%s", vm)
	}
}
