package ast

import "geblang/internal/token"

// GeneratorComprehension is the lazy `(body for x in xs if cond)` form.
type GeneratorComprehension struct {
	Token   token.Token
	Body    Expression
	Clauses []ComprehensionClause
}

func (*GeneratorComprehension) expressionNode()        {}
func (e *GeneratorComprehension) TokenLiteral() string { return e.Token.Literal }
func (e *GeneratorComprehension) String() string       { return "(" + e.Body.String() + " for ...)" }

const generatorSourceName = "__genSrc"

// IterableSource evaluates Value and raises a TypeError unless for-in could iterate it; only LowerGeneratorComprehension produces it.
type IterableSource struct {
	Token token.Token
	Value Expression
}

func (*IterableSource) expressionNode()        {}
func (e *IterableSource) TokenLiteral() string { return e.Token.Literal }
func (e *IterableSource) String() string       { return e.Value.String() }

// LowerGeneratorComprehension rewrites the comprehension as a generator closure called with the first source, so that source is evaluated where the comprehension is written.
func LowerGeneratorComprehension(c *GeneratorComprehension) Expression {
	tok := c.Token
	first := *c.Clauses[0].(*ComprehensionFor)
	source := first.Iterable
	first.Iterable = &Identifier{Token: tok, Value: generatorSourceName}
	clauses := append([]ComprehensionClause{&first}, c.Clauses[1:]...)
	yield := &YieldStatement{Token: tok, Value: c.Body}
	fn := &FunctionLiteral{
		Token:      tok,
		Parameters: []Parameter{{Name: &Identifier{Token: tok, Value: generatorSourceName}, Type: &TypeRef{Token: tok, Name: "any"}}},
		ReturnType: &TypeRef{Token: tok, Name: "generator", Arguments: []*TypeRef{{Token: tok, Name: "any"}}},
		Body:       ComprehensionBlock(WrapComprehensionClauses(clauses, yield)),
	}
	return &CallExpression{Token: tok, Callee: fn, Arguments: []CallArgument{{Value: &IterableSource{Token: tok, Value: source}}}}
}

// WrapComprehensionClauses nests inner inside the clauses' loops and filters, outermost first.
func WrapComprehensionClauses(clauses []ComprehensionClause, inner Statement) Statement {
	for i := len(clauses) - 1; i >= 0; i-- {
		switch c := clauses[i].(type) {
		case *ComprehensionFor:
			inner = freshBindingFor(c, inner)
		case *ComprehensionIf:
			inner = &IfStatement{Token: c.Token, Condition: c.Filter, Consequence: ComprehensionBlock(inner)}
		}
	}
	return inner
}

// freshBindingFor loops over hidden variables and declares the clause's names anew each iteration, so closures capture that item's value on both runtimes.
func freshBindingFor(c *ComprehensionFor, inner Statement) Statement {
	hidden := func(name *Identifier) *Identifier {
		return &Identifier{Token: name.Token, Value: "__comp_" + name.Value}
	}
	loop := &ForStatement{Token: c.Token, VarType: c.VarType, Iterable: c.Iterable}
	names := c.VarNames
	if c.VarName != nil {
		names = []*Identifier{c.VarName}
		loop.VarName = hidden(c.VarName)
	} else {
		for _, n := range c.VarNames {
			loop.VarNames = append(loop.VarNames, hidden(n))
		}
	}
	var body []Statement
	for _, n := range names {
		body = append(body, &DeclarationStatement{Token: n.Token, Kind: "let", Name: n, Value: hidden(n)})
	}
	loop.Body = &BlockStatement{Statements: append(body, ComprehensionBlock(inner).Statements...)}
	return loop
}

// ComprehensionBlock wraps stmt in a block unless it already is one.
func ComprehensionBlock(stmt Statement) *BlockStatement {
	if b, ok := stmt.(*BlockStatement); ok {
		return b
	}
	return &BlockStatement{Statements: []Statement{stmt}}
}
