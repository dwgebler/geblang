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

// overloadDonorModule exports an overloaded free function selectable by arity and argument type.
const overloadDonorModule = "module helpers;\n" +
	"export func over(int v): string { return \"int:${v}\"; }\n" +
	"export func over(string s): string { return \"str:${s}\"; }\n" +
	"export func over(int a, int b): string { return \"two:${a},${b}\"; }\n"

// A cross-module exported overload set selects by type on a positional member call.
func TestParityCrossModuleOverloadPositional(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nio.println(helpers.over(5));\nio.println(helpers.over(\"x\"));\nio.println(helpers.over(3, 4));\n",
		"int:5\nstr:x\ntwo:3,4\n")
}

// A from-imported overload set is called bare and selects among its overloads.
func TestParityCrossModuleOverloadFromImport(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nfrom helpers import over;\nio.println(over(5));\nio.println(over(\"y\"));\nio.println(over(1, 2));\n",
		"int:5\nstr:y\ntwo:1,2\n")
}

// The overload set as a first-class value selects among overloads per call.
func TestParityCrossModuleOverloadValue(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nlet f = helpers.over;\nio.println(f(5));\nio.println(f(\"z\"));\nio.println(f(7, 8));\n",
		"int:5\nstr:z\ntwo:7,8\n")
}

// The overload value passed to a HOF callback resolves per element.
func TestParityCrossModuleOverloadCallback(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nlet xs = [3, 1, 2];\nlet f = helpers.over;\nio.println(\"${xs.map(f)}\");\n",
		"[\"int:3\", \"int:1\", \"int:2\"]\n")
}

// Named args on a cross-module overloaded member call select and order against the home chunk.
func TestParityCrossModuleOverloadNamed(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nio.println(helpers.over(v: 9));\nio.println(helpers.over(s: \"named\"));\nio.println(helpers.over(a: 1, b: 2));\nio.println(helpers.over(b: 2, a: 1));\n",
		"int:9\nstr:named\ntwo:1,2\ntwo:1,2\n")
}

// A deferred cross-module overloaded call fires at scope exit with the selected overload.
func TestParityCrossModuleOverloadDefer(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nfunc run(): void {\n  defer io.println(helpers.over(5));\n  io.println(\"body\");\n}\nrun();\n",
		"body\nint:5\n")
}

// A module re-exporting an imported overload set through a wrapper resolves the overload internally.
func TestParityCrossModuleOverloadReexportChain(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"helpers":  overloadDonorModule,
		"reexport": "module reexport;\nfrom helpers import over;\nexport func over2(int v): string { return \"re:${over(v)}\"; }\n",
	}, "import io;\nimport reexport;\nio.println(reexport.over2(5));\n", "re:int:5\n")
}

// A local function shadowing one overload name does not affect the module's overload set.
func TestParityCrossModuleOverloadLocalShadow(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nfunc over(int v): string { return \"local:${v}\"; }\nio.println(over(5));\nio.println(helpers.over(5));\n",
		"local:5\nint:5\n")
}

// A caught no-match on a cross-module overloaded call carries the qualified name identically.
func TestParityCrossModuleOverloadCaughtNoMatch(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\ntry {\n  io.println(helpers.over(true));\n} catch (Error e) {\n  io.println(\"caught: ${e.message}\");\n}\n",
		"caught: no matching overload for helpers.over\n")
}

// An uncaught no-match renders the qualified name identically on both backends.
func TestParityCrossModuleOverloadUncaughtNoMatch(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers;\nio.println(helpers.over(true));\n")
	if ev != vm {
		t.Fatalf("divergence:\n--- eval ---\n%s\n--- vm ---\n%s", ev, vm)
	}
	if !strings.Contains(vm, "no matching overload for helpers.over") {
		t.Fatalf("missing qualified overload name: %s", vm)
	}
}

// An aliased import qualifies the no-match error with the call-site alias.
func TestParityCrossModuleOverloadAliasName(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nimport helpers as h;\nio.println(h.over(true));\n")
	if ev != vm {
		t.Fatalf("divergence:\n--- eval ---\n%s\n--- vm ---\n%s", ev, vm)
	}
	if !strings.Contains(vm, "no matching overload for h.over") {
		t.Fatalf("missing aliased overload name: %s", vm)
	}
}

// A bare (from-import / value) no-match uses the unqualified overload name.
func TestParityCrossModuleOverloadBareName(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{"helpers": overloadDonorModule},
		"import io;\nfrom helpers import over;\nio.println(over(true));\n")
	if ev != vm {
		t.Fatalf("divergence:\n--- eval ---\n%s\n--- vm ---\n%s", ev, vm)
	}
	if !strings.Contains(vm, "no matching overload for over") {
		t.Fatalf("missing bare overload name: %s", vm)
	}
}

// An ambiguous overload selection reports the qualified name identically.
func TestParityCrossModuleOverloadAmbiguous(t *testing.T) {
	ev, vm := runMultiModuleUncaughtParity(t, map[string]string{
		"ambx": "module ambx;\nexport func amb(int v): string { return \"a\"; }\nexport func amb(int|string v): string { return \"b\"; }\n",
	}, "import io;\nimport ambx;\nio.println(ambx.amb(5));\n")
	if ev != vm {
		t.Fatalf("divergence:\n--- eval ---\n%s\n--- vm ---\n%s", ev, vm)
	}
	if !strings.Contains(vm, "ambiguous overload for ambx.amb") {
		t.Fatalf("missing qualified ambiguous name: %s", vm)
	}
}

// The VM rejects spread into a cross-module overloaded function to stay consistent with the same-module rejection (ISSUE-030); the evaluator accepts it.
func TestCrossModuleOverloadSpreadRejectedOnVM(t *testing.T) {
	modules := map[string]string{"helpers": overloadDonorModule}
	mainSrc := "import io;\nimport helpers;\nlet xs = [3, 4];\nio.println(helpers.over(...xs));\n"
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
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: expected spread accepted, got %v", err)
	}
	if evOut.String() != "two:3,4\n" {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), "two:3,4\n")
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
		t.Fatalf("vm: expected spread rejection, got none (stdout %q)", vmOut.String())
	}
	if !strings.Contains(vmErr.Error(), "cannot use spread with overloaded function over") {
		t.Fatalf("vm: wrong rejection: %v", vmErr)
	}
}

// asyncOverloadDonorModule exports an overloaded ASYNC function selectable by argument type.
const asyncOverloadDonorModule = "module asyncmod;\n" +
	"export async func af(int v): string { return \"int:${v}\"; }\n" +
	"export async func af(string s): string { return \"str:${s}\"; }\n"

// A cross-module overloaded async member call yields a Task per selected overload, awaited to the selected result.
func TestParityCrossModuleOverloadAsyncMember(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"asyncmod": asyncOverloadDonorModule},
		"import io;\nimport async;\nimport asyncmod;\nlet a = asyncmod.af(5);\nlet b = asyncmod.af(\"x\");\nio.println(\"${typeof(a)} ${typeof(b)}\");\nio.println(\"${async.await(a)} ${async.await(b)}\");\n",
		"Task Task\nint:5 str:x\n")
}

// A named cross-module overloaded async member call selects against the home chunk and yields a Task.
func TestParityCrossModuleOverloadAsyncNamed(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"asyncmod": asyncOverloadDonorModule},
		"import io;\nimport async;\nimport asyncmod;\nlet a = asyncmod.af(v: 5);\nio.println(\"${typeof(a)} ${async.await(a)}\");\n",
		"Task int:5\n")
}

// The cross-module overloaded async set as a first-class value yields a Task per invocation.
func TestParityCrossModuleOverloadAsyncValue(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"asyncmod": asyncOverloadDonorModule},
		"import io;\nimport async;\nimport asyncmod;\nlet f = asyncmod.af;\nlet a = f(5);\nlet b = f(\"x\");\nio.println(\"${typeof(a)} ${typeof(b)}\");\nio.println(\"${async.await(a)} ${async.await(b)}\");\n",
		"Task Task\nint:5 str:x\n")
}

// A from-imported cross-module overloaded async set is called bare and yields a Task.
func TestParityCrossModuleOverloadAsyncFromImport(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"asyncmod": asyncOverloadDonorModule},
		"import io;\nimport async;\nfrom asyncmod import af;\nlet a = af(5);\nio.println(\"${typeof(a)} ${async.await(a)}\");\n",
		"Task int:5\n")
}

// A mixed async/sync cross-module overload set wraps only the async winner in a Task on both the member and value paths.
func TestParityCrossModuleOverloadMixedAsync(t *testing.T) {
	mixed := "module mixmod;\n" +
		"export async func mx(int v): string { return \"async:${v}\"; }\n" +
		"export func mx(string s): string { return \"sync:${s}\"; }\n"
	runMultiModuleParity(t, map[string]string{"mixmod": mixed},
		"import io;\nimport async;\nimport mixmod;\nlet a = mixmod.mx(5);\nlet b = mixmod.mx(\"x\");\nlet f = mixmod.mx;\nlet c = f(5);\nlet d = f(\"y\");\nio.println(\"${typeof(a)} ${typeof(b)} ${typeof(c)} ${typeof(d)}\");\nio.println(\"${async.await(a)} ${b} ${async.await(c)} ${d}\");\n",
		"Task string Task string\nasync:5 sync:x async:5 sync:y\n")
}
