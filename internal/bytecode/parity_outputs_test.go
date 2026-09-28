package bytecode_test

import (
	"bytes"
	"testing"

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
