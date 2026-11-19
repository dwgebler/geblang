package desugar

import (
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func parseProgram(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return program
}

func firstArrowParamType(t *testing.T, program *ast.Program) *ast.TypeRef {
	t.Helper()
	var found *ast.TypeRef
	walkAST(program, func(node any) {
		if fn, ok := node.(*ast.FunctionLiteral); ok && fn.Arrow && found == nil && len(fn.Parameters) > 0 {
			found = fn.Parameters[0].Type
		}
	})
	if found == nil {
		t.Fatal("no arrow parameter found")
	}
	return found
}

func TestArrowsWithOuterBindings(t *testing.T) {
	program := parseProgram(t, "ws.map((w) => w.upper());\n")
	ref, err := parser.ParseTypeRef("list<string>")
	if err != nil {
		t.Fatal(err)
	}
	if err := ArrowsWith(program, map[string]*ast.TypeRef{"ws": ref}); err != nil {
		t.Fatal(err)
	}
	if got := firstArrowParamType(t, program); got.Inferred || got.String() != "string" {
		t.Fatalf("parameter type = %s (inferred=%v), want string", got.String(), got.Inferred)
	}
}
