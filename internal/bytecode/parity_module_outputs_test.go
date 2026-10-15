package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// parityModuleOutputs writes modules into a temp dir and returns the evaluator and VM stdout of mainSource.
func parityModuleOutputs(t *testing.T, modules map[string]string, mainSource string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, source := range modules {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
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
	aliases := bcloader.SourceTypeAliasLookup(program, "", []string{dir})
	chunk, err := bytecode.CompileWithOptions(program, []byte(mainSource), "parity", bytecode.CompileOptions{TypeAliasLookup: aliases})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	bytecode.ResolveClassTypeAliases(&chunk, aliases)
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
	return evOut.String(), vmOut.String()
}
