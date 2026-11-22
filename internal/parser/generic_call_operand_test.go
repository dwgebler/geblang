package parser_test

import (
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func parseSingleExpression(t *testing.T, src string) ast.Expression {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("%s: parser errors: %v", src, errs)
	}
	es, ok := prog.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("%s: got %T, want *ast.ExpressionStatement", src, prog.Statements[0])
	}
	return es.Expression
}

func assertGenericCall(t *testing.T, src string, expr ast.Expression) {
	t.Helper()
	call, ok := expr.(*ast.CallExpression)
	if !ok || len(call.TypeArguments) != 1 {
		t.Fatalf("%s: operand is %T (%s), want a generic call", src, expr, expr)
	}
}

func TestGenericCallIsOperandOfPrefixOperators(t *testing.T) {
	for _, src := range []string{`-f<int>(x);`, `!f<int>(x);`, `~f<int>(x);`} {
		prefix, ok := parseSingleExpression(t, src).(*ast.PrefixExpression)
		if !ok {
			t.Fatalf("%s: want *ast.PrefixExpression", src)
		}
		assertGenericCall(t, src, prefix.Right)
	}
	src := `await f<int>();`
	await, ok := parseSingleExpression(t, src).(*ast.AwaitExpression)
	if !ok {
		t.Fatalf("%s: want *ast.AwaitExpression", src)
	}
	assertGenericCall(t, src, await.Value)
}

func TestGenericCallIsRightOperandOfTighterBinaryOperators(t *testing.T) {
	for _, src := range []string{`a + f<int>(x);`, `a * f<int>(x);`, `a << f<int>(x);`, `a < f<int>(x);`, `a >= f<int>(x);`} {
		infix, ok := parseSingleExpression(t, src).(*ast.InfixExpression)
		if !ok {
			t.Fatalf("%s: want *ast.InfixExpression", src)
		}
		assertGenericCall(t, src, infix.Right)
	}
}

func TestComparisonsThatAreNotGenericCallsStayComparisons(t *testing.T) {
	for _, src := range []string{`a < b;`, `-a < b;`, `a + b < f(x);`, `i < n >> 1;`} {
		infix, ok := parseSingleExpression(t, src).(*ast.InfixExpression)
		if !ok || infix.Operator != "<" {
			t.Fatalf("%s: want a top-level < comparison, got %T", src, parseSingleExpression(t, src))
		}
	}
}
