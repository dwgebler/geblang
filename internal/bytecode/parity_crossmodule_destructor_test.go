package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// runCrossModuleDestructorParity runs mainSource against donor modules on both backends including the program-exit sweep (VM gets an explicit Cleanup, mirroring cmd/geblang) and asserts identical stdout.
func runCrossModuleDestructorParity(t *testing.T, modules map[string]string, mainSource, want string) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range modules {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	p := parser.New(lexer.New(mainSource))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	chunk, err := bytecode.Compile(program, []byte(mainSource), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
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
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if err := vm.Cleanup(); err != nil {
		t.Fatalf("vm cleanup error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("cross-module destructor divergence:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

const cmDestructorDonor = "module res;\n" +
	"import io;\n" +
	"export class Resource {\n" +
	"    string name;\n" +
	"    func Resource(string n) { this.name = n; io.println(\"construct \" + n); }\n" +
	"    func tag(): string { return this.name; }\n" +
	"    func ~Resource() { io.println(\"destruct \" + this.name); }\n" +
	"}\n" +
	"export func make(string n): Resource { return Resource(n); }\n"

// A cross-module instance's destructor must fire at program exit, after the body, not eagerly on worker recycle.
func TestParityCrossModuleDestructorExitSweep(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"res.Resource a = res.Resource(\"A\");\n" +
		"io.println(a.tag());\n" +
		"io.println(\"after\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct A\nA\nafter\ndestruct A\n")
}

// The exit sweep visits cross-module destructibles in reverse creation order.
func TestParityCrossModuleDestructorExitOrder(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"res.Resource a = res.Resource(\"A\");\n" +
		"res.Resource b = res.Resource(\"B\");\n" +
		"res.Resource c = res.Resource(\"C\");\n" +
		"io.println(\"middle\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct A\nconstruct B\nconstruct C\nmiddle\ndestruct C\ndestruct B\ndestruct A\n")
}

// `del` fires a cross-module destructor once and removes it from the exit sweep; other instances still fire at exit.
func TestParityCrossModuleDestructorDelFiresOnce(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"res.Resource a = res.Resource(\"A\");\n" +
		"res.Resource b = res.Resource(\"B\");\n" +
		"io.println(\"before del\");\n" +
		"del a;\n" +
		"io.println(\"after del\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct A\nconstruct B\nbefore del\ndestruct A\nafter del\ndestruct B\n")
}

// A cross-module factory returning a local destructible fires at exit, not when the factory worker recycles.
func TestParityCrossModuleDestructorFactoryReturn(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"res.Resource a = res.make(\"F\");\n" +
		"io.println(a.tag());\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct F\nF\ndestruct F\n")
}

// A cross-module destructor that itself constructs a destructible re-drains at exit.
func TestParityCrossModuleDestructorReDrain(t *testing.T) {
	donor := "module tree;\nimport io;\n" +
		"export class Leaf {\n" +
		"    string name;\n" +
		"    func Leaf(string n) { this.name = n; io.println(\"construct leaf \" + n); }\n" +
		"    func ~Leaf() { io.println(\"destruct leaf \" + this.name); }\n" +
		"}\n" +
		"export class Node {\n" +
		"    string name;\n" +
		"    func Node(string n) { this.name = n; io.println(\"construct node \" + n); }\n" +
		"    func ~Node() { io.println(\"destruct node \" + this.name); Leaf(\"of-\" + this.name); }\n" +
		"}\n"
	main := "import io;\nimport tree;\n" +
		"tree.Node n = tree.Node(\"N1\");\n" +
		"io.println(\"middle\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"tree.gb": donor}, main,
		"construct node N1\nmiddle\ndestruct node N1\nconstruct leaf of-N1\ndestruct leaf of-N1\n")
}

// A main-module subclass of a foreign class with its own destructor fires locally at exit (unchanged parity).
func TestParityCrossModuleDestructorSubclassOwn(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"class Sub extends res.Resource {\n" +
		"    func Sub(string n) { parent(n); }\n" +
		"    func ~Sub() { io.println(\"destruct sub \" + this.name); }\n" +
		"}\n" +
		"Sub s = Sub(\"S\");\n" +
		"io.println(\"middle\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct S\nmiddle\ndestruct sub S\n")
}

// An async cross-module construction drops its destructible on both backends (the async worker owns no exit sweep); pins the parity, no eager fire.
func TestParityCrossModuleDestructorAsyncDropped(t *testing.T) {
	main := "import io;\nimport res;\n" +
		"async func build(string n): res.Resource { return res.Resource(n); }\n" +
		"res.Resource r = await build(\"AsyncA\");\n" +
		"io.println(r.tag());\n" +
		"io.println(\"after\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"res.gb": cmDestructorDonor}, main,
		"construct AsyncA\nAsyncA\nafter\n")
}

// Concurrent async cross-module constructions each adopt on their own goroutine; the quiet donor keeps output deterministic so the parity harness runs it under -race to prove the adoption is race-free.
func TestConcurrentCrossModuleDestructorAdoption(t *testing.T) {
	quiet := "module quiet;\n" +
		"export class Q {\n" +
		"    string name;\n" +
		"    func Q(string n) { this.name = n; }\n" +
		"    func tag(): string { return this.name; }\n" +
		"    func ~Q() {}\n" +
		"}\n"
	main := "import io;\nimport quiet;\n" +
		"async func build(int i): quiet.Q { return quiet.Q(\"R${i}\"); }\n" +
		"let tasks = [];\n" +
		"for (i in range(0, 15)) { tasks = tasks.push(build(i)); }\n" +
		"int total = 0;\n" +
		"for (t in tasks) { quiet.Q r = await t; total = total + r.tag().length(); }\n" +
		"io.println(\"awaited ${total}\");\n"
	runCrossModuleDestructorParity(t, map[string]string{"quiet.gb": quiet}, main, "awaited 38\n")
}
