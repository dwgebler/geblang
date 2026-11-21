package bytecode

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/token"
)

// positionalPrimitiveCall rewrites a named-argument call on a statically typed primitive receiver into the equivalent positional call, so the VM binds nothing at run time.
func (c *Compiler) positionalPrimitiveCall(expr *ast.CallExpression, selector *ast.SelectorExpression) (*ast.CallExpression, bool) {
	typeName := primitiveCatalogTypeName(c.staticReceiverType(selector.Object))
	if typeName == "" {
		return nil, false
	}
	names := make([]string, len(expr.Arguments))
	for i, arg := range expr.Arguments {
		if arg.Spread || arg.Hole {
			return nil, false
		}
		if arg.Name != nil {
			names[i] = arg.Name.Value
		}
	}
	sources, found, err := native.BindPrimitiveCall(typeName, selector.Name.Value, native.ArgNames(names, len(names)))
	if !found || err != nil {
		return nil, false
	}
	last, inOrder := -1, true
	for _, s := range sources {
		if !s.IsDefault() {
			inOrder = inOrder && s.Index > last
			last = s.Index
		}
	}
	if !inOrder {
		for _, arg := range expr.Arguments {
			if !reorderSafe(arg.Value) {
				return nil, false
			}
		}
	}
	args := make([]ast.CallArgument, len(sources))
	for i, s := range sources {
		if s.IsDefault() {
			args[i] = ast.CallArgument{Value: defaultLiteralExpression(s.Default, expr.Token)}
		} else {
			args[i] = ast.CallArgument{Value: expr.Arguments[s.Index].Value}
		}
	}
	return &ast.CallExpression{Token: expr.Token, TypeArguments: expr.TypeArguments, Callee: expr.Callee, Arguments: args, End: expr.End}, true
}

func primitiveCatalogTypeName(static string) string {
	if base, _, generic := strings.Cut(static, "<"); generic {
		static = base
	}
	if _, ok := native.PrimitiveTypeNames[static]; ok {
		return static
	}
	return ""
}

// reorderSafe reports whether evaluating expr earlier or later than written is unobservable.
func reorderSafe(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.IntegerLiteral, *ast.DecimalLiteral, *ast.FloatLiteral, *ast.StringLiteral, *ast.Literal, *ast.Identifier, *ast.FunctionLiteral:
		return true
	case *ast.PrefixExpression:
		return e.Operator == "-" && reorderSafe(e.Right)
	}
	return false
}

func defaultLiteralExpression(d native.DefaultLiteral, tok token.Token) ast.Expression {
	text, negative := strings.CutPrefix(d.Text, "-")
	var lit ast.Expression
	switch d.Kind {
	case native.DefaultInt:
		lit = &ast.IntegerLiteral{Token: tok, Value: text}
	case native.DefaultDecimal:
		lit = &ast.DecimalLiteral{Token: tok, Value: text}
	case native.DefaultFloat:
		lit = &ast.FloatLiteral{Token: tok, Value: text}
	case native.DefaultString:
		return &ast.StringLiteral{Token: tok, Value: d.Text, Quote: '"'}
	case native.DefaultBool:
		return &ast.Literal{Token: tok, Value: d.Text == "true"}
	case native.DefaultList:
		return &ast.ListLiteral{Token: tok}
	case native.DefaultDict:
		return &ast.DictLiteral{Token: tok}
	case native.DefaultSet:
		return &ast.SetLiteral{Token: tok}
	default:
		return &ast.Literal{Token: tok, Value: nil}
	}
	if negative {
		return &ast.PrefixExpression{Token: tok, Operator: "-", Right: lit}
	}
	return lit
}
