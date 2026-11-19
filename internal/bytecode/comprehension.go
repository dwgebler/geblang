package bytecode

import (
	"geblang/internal/ast"
	"geblang/internal/token"
)

const comprehensionAccName = "__compAcc"

// desugarListComprehension rewrites `[body for x in xs (if cond)*]` as an
// IIFE that accumulates results via in-place list.append. The evaluator
// handles ListComprehension directly; the bytecode compiler uses this
// lowering so parity falls out for free without adding new opcodes.
func desugarListComprehension(c *ast.ListComprehension) ast.Expression {
	tok := c.Token
	accIdent := &ast.Identifier{Token: tok, Value: comprehensionAccName}
	bodyStmt := wrapInExpressionStatement(tok, &ast.CallExpression{
		Token: tok,
		Callee: &ast.SelectorExpression{
			Token:  tok,
			Object: accIdent,
			Name:   &ast.Identifier{Token: tok, Value: "append"},
		},
		Arguments: []ast.CallArgument{{Value: c.Body}},
	})
	loopBody := ast.WrapComprehensionClauses(c.Clauses, bodyStmt)
	return buildComprehensionIIFE(tok, &ast.ListLiteral{Token: tok}, loopBody, accIdent)
}

// desugarSetComprehension rewrites `{body for x in xs (if cond)*}` as an
// IIFE that accumulates results via list.append, then casts to set at the
// end (relying on `list as set` dedup semantics).
func desugarSetComprehension(c *ast.SetComprehension) ast.Expression {
	tok := c.Token
	accIdent := &ast.Identifier{Token: tok, Value: comprehensionAccName}
	bodyStmt := wrapInExpressionStatement(tok, &ast.CallExpression{
		Token: tok,
		Callee: &ast.SelectorExpression{
			Token:  tok,
			Object: accIdent,
			Name:   &ast.Identifier{Token: tok, Value: "append"},
		},
		Arguments: []ast.CallArgument{{Value: c.Body}},
	})
	loopBody := ast.WrapComprehensionClauses(c.Clauses, bodyStmt)
	cast := &ast.CastExpression{
		Token: tok,
		Value: accIdent,
		Type:  &ast.TypeRef{Token: tok, Name: "set"},
	}
	return buildComprehensionIIFE(tok, &ast.ListLiteral{Token: tok}, loopBody, cast)
}

// desugarDictComprehension rewrites `{k: v for x in xs (if cond)*}` as an
// IIFE that accumulates via dict.set (in-place since 1.5.2).
func desugarDictComprehension(c *ast.DictComprehension) ast.Expression {
	tok := c.Token
	accIdent := &ast.Identifier{Token: tok, Value: comprehensionAccName}
	bodyStmt := wrapInExpressionStatement(tok, &ast.CallExpression{
		Token: tok,
		Callee: &ast.SelectorExpression{
			Token:  tok,
			Object: accIdent,
			Name:   &ast.Identifier{Token: tok, Value: "set"},
		},
		Arguments: []ast.CallArgument{{Value: c.KeyBody}, {Value: c.ValueBody}},
	})
	loopBody := ast.WrapComprehensionClauses(c.Clauses, bodyStmt)
	return buildComprehensionIIFE(tok, &ast.DictLiteral{Token: tok}, loopBody, accIdent)
}

func wrapInExpressionStatement(tok token.Token, expr ast.Expression) ast.Statement {
	return &ast.ExpressionStatement{Token: tok, Expression: expr}
}

func dictHasSpread(entries []ast.DictEntry) bool {
	for _, e := range entries {
		if e.Spread {
			return true
		}
	}
	return false
}

func setHasSpread(elements []ast.Expression) bool {
	for _, el := range elements {
		if _, ok := el.(*ast.SpreadExpression); ok {
			return true
		}
	}
	return false
}

// desugarDictLiteralWithSpread rewrites `{a: 1, ...src, b: 2}` as an IIFE
// that builds the dict via in-place dict.set. Spread entries iterate the
// source dict's items() and re-insert key/value pairs in order. Last
// write wins on key collision, matching the requested JS-style semantics.
func desugarDictLiteralWithSpread(d *ast.DictLiteral) ast.Expression {
	tok := d.Token
	accIdent := &ast.Identifier{Token: tok, Value: comprehensionAccName}
	stmts := []ast.Statement{
		&ast.DeclarationStatement{
			Token: tok,
			Kind:  "let",
			Name:  &ast.Identifier{Token: tok, Value: comprehensionAccName},
			Value: &ast.DictLiteral{Token: tok},
		},
	}
	for _, entry := range d.Entries {
		if entry.Spread {
			// for (__compKey, __compVal in spread.items()) acc.set(__compKey, __compVal);
			itemsCall := &ast.CallExpression{
				Token:  tok,
				Callee: &ast.SelectorExpression{Token: tok, Object: entry.Value, Name: &ast.Identifier{Token: tok, Value: "items"}},
			}
			setCall := &ast.CallExpression{
				Token:  tok,
				Callee: &ast.SelectorExpression{Token: tok, Object: accIdent, Name: &ast.Identifier{Token: tok, Value: "set"}},
				Arguments: []ast.CallArgument{
					{Value: &ast.Identifier{Token: tok, Value: "__compKey"}},
					{Value: &ast.Identifier{Token: tok, Value: "__compVal"}},
				},
			}
			forStmt := &ast.ForStatement{
				Token:    tok,
				VarNames: []*ast.Identifier{{Token: tok, Value: "__compKey"}, {Token: tok, Value: "__compVal"}},
				Iterable: itemsCall,
				Body:     &ast.BlockStatement{Statements: []ast.Statement{&ast.ExpressionStatement{Token: tok, Expression: setCall}}},
			}
			stmts = append(stmts, forStmt)
		} else {
			setCall := &ast.CallExpression{
				Token:  tok,
				Callee: &ast.SelectorExpression{Token: tok, Object: accIdent, Name: &ast.Identifier{Token: tok, Value: "set"}},
				Arguments: []ast.CallArgument{
					{Value: entry.Key},
					{Value: entry.Value},
				},
			}
			stmts = append(stmts, &ast.ExpressionStatement{Token: tok, Expression: setCall})
		}
	}
	stmts = append(stmts, &ast.ReturnStatement{Token: tok, Value: accIdent})
	iife := &ast.FunctionLiteral{Token: tok, Body: &ast.BlockStatement{Token: tok, Statements: stmts}}
	return &ast.CallExpression{Token: tok, Callee: iife}
}

// desugarSetLiteralWithSpread rewrites `{x, ...src, y}` as an IIFE that
// accumulates into a list via list.append, then casts to set at the end
// (the cast dedups). Spread sources can be set or list and are iterated
// in their natural order.
func desugarSetLiteralWithSpread(s *ast.SetLiteral) ast.Expression {
	tok := s.Token
	accIdent := &ast.Identifier{Token: tok, Value: comprehensionAccName}
	stmts := []ast.Statement{
		&ast.DeclarationStatement{
			Token: tok,
			Kind:  "let",
			Name:  &ast.Identifier{Token: tok, Value: comprehensionAccName},
			Value: &ast.ListLiteral{Token: tok},
		},
	}
	for _, element := range s.Elements {
		if spread, ok := element.(*ast.SpreadExpression); ok {
			// for (__compVal in src as list) acc.append(__compVal);
			srcAsList := &ast.CastExpression{
				Token: tok,
				Value: spread.Value,
				Type:  &ast.TypeRef{Token: tok, Name: "list"},
			}
			appendCall := &ast.CallExpression{
				Token:  tok,
				Callee: &ast.SelectorExpression{Token: tok, Object: accIdent, Name: &ast.Identifier{Token: tok, Value: "append"}},
				Arguments: []ast.CallArgument{
					{Value: &ast.Identifier{Token: tok, Value: "__compVal"}},
				},
			}
			forStmt := &ast.ForStatement{
				Token:    tok,
				VarName:  &ast.Identifier{Token: tok, Value: "__compVal"},
				Iterable: srcAsList,
				Body:     &ast.BlockStatement{Statements: []ast.Statement{&ast.ExpressionStatement{Token: tok, Expression: appendCall}}},
			}
			stmts = append(stmts, forStmt)
		} else {
			appendCall := &ast.CallExpression{
				Token:  tok,
				Callee: &ast.SelectorExpression{Token: tok, Object: accIdent, Name: &ast.Identifier{Token: tok, Value: "append"}},
				Arguments: []ast.CallArgument{
					{Value: element},
				},
			}
			stmts = append(stmts, &ast.ExpressionStatement{Token: tok, Expression: appendCall})
		}
	}
	cast := &ast.CastExpression{Token: tok, Value: accIdent, Type: &ast.TypeRef{Token: tok, Name: "set"}}
	stmts = append(stmts, &ast.ReturnStatement{Token: tok, Value: cast})
	iife := &ast.FunctionLiteral{Token: tok, Body: &ast.BlockStatement{Token: tok, Statements: stmts}}
	return &ast.CallExpression{Token: tok, Callee: iife}
}

func buildComprehensionIIFE(tok token.Token, accInit ast.Expression, loopBody ast.Statement, result ast.Expression) ast.Expression {
	accDecl := &ast.DeclarationStatement{
		Token: tok,
		Kind:  "let",
		Name:  &ast.Identifier{Token: tok, Value: comprehensionAccName},
		Value: accInit,
	}
	ret := &ast.ReturnStatement{Token: tok, Value: result}
	body := &ast.BlockStatement{
		Token:      tok,
		Statements: []ast.Statement{accDecl, loopBody, ret},
	}
	iife := &ast.FunctionLiteral{Token: tok, Body: body}
	return &ast.CallExpression{Token: tok, Callee: iife}
}
