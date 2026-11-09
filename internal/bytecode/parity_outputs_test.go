package bytecode_test

import (
	"bytes"
	"testing"

	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// parityOutputs returns the evaluator and VM stdout so callers assert inline.
func parityOutputs(t *testing.T, source string) (string, string) {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	if err := bytecode.NewVM(chunk, &vmOut).Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return evOut.String(), vmOut.String()
}

// parityOutputsModulesDir is parityOutputs with modules under dir importable on both backends.
func parityOutputsModulesDir(t *testing.T, dir, source string) (string, string) {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	if _, err := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir}).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}
	aliases := bcloader.SourceTypeAliasLookup(program, "", []string{dir})
	chunk, err := bytecode.CompileWithOptions(program, []byte(source), "parity", bytecode.CompileOptions{TypeAliasLookup: aliases})
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
