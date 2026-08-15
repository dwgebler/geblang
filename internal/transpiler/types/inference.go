package types

import "geblang/internal/ast"

func InferLiteral(expr ast.Expression) *Type {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return &Type{Kind: KindInt}
	case *ast.FloatLiteral:
		return &Type{Kind: KindFloat}
	case *ast.DecimalLiteral:
		return &Type{Kind: KindDecimal}
	case *ast.StringLiteral, *ast.InterpolatedString:
		return &Type{Kind: KindString}
	case *ast.EmbeddedLiteral:
		if e.Binary {
			return &Type{Kind: KindBytes}
		}
		return &Type{Kind: KindString}
	}
	return Unknown()
}
