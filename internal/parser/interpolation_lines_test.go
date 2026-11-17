package parser

import (
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
)

func TestInterpolationExpressionsCarryTheirSourceLine(t *testing.T) {
	src := "let a = 1;\nlet s = \"x ${a} y\";\nlet t = \"\"\"\nfirst\nsecond ${a}\n\"\"\";\n"
	p := New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatal(p.Errors())
	}
	want := []int{2, 5}
	for i, stmt := range program.Statements[1:] {
		value := stmt.(*ast.DeclarationStatement).Value.(*ast.InterpolatedString)
		var ident *ast.Identifier
		for _, part := range value.Parts {
			if id, ok := part.(*ast.Identifier); ok {
				ident = id
			}
		}
		if ident == nil || ident.Token.Line != want[i] {
			t.Fatalf("statement %d: interpolated identifier line = %v, want %d", i+2, ident, want[i])
		}
	}
}
