package parser

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/token"
)

const (
	_ int = iota
	lowest
	assign
	pipe
	ternary
	nullCoalesce
	logicalOr
	logicalAnd
	bitOr
	bitXor
	bitAnd
	equality
	compare
	shift
	sum
	product
	power
	prefix
	postfix
	call
)

var precedences = map[token.Type]int{
	token.Assign:             assign,
	token.PlusAssign:         assign,
	token.MinusAssign:        assign,
	token.MulAssign:          assign,
	token.DivAssign:          assign,
	token.IntDivAssign:       assign,
	token.ModAssign:          assign,
	token.PowerAssign:        assign,
	token.BitAndAssign:       assign,
	token.BitOrAssign:        assign,
	token.BitXorAssign:       assign,
	token.LShiftAssign:       assign,
	token.RShiftAssign:       assign,
	token.NullCoalesceAssign: assign,
	token.Question:           ternary,
	token.Pipe:               pipe,
	token.NullCoalesce:       nullCoalesce,
	token.Or:                 logicalOr,
	token.And:                logicalAnd,
	token.Xor:                logicalOr,
	token.BitOr:              bitOr,
	token.BitXor:             bitXor,
	token.BitAnd:             bitAnd,
	token.Eq:                 equality,
	token.NotEq:              equality,
	token.Is:                 equality,
	token.InstanceOf:         equality,
	token.In:                 compare,
	token.LT:                 compare,
	token.LTE:                compare,
	token.GT:                 compare,
	token.GTE:                compare,
	token.LShift:             shift,
	token.RShift:             shift,
	token.Plus:               sum,
	token.Minus:              sum,
	token.Asterisk:           product,
	token.Slash:              product,
	token.IntDiv:             product,
	token.Percent:            product,
	token.Power:              power,
	token.Range:              compare,
	token.RangeExcl:          compare,
	token.As:                 compare,
	token.Dot:                call,
	token.OptionalChain:      call,
	token.LParen:             call,
	token.LBracket:           call,
	token.Inc:                postfix,
	token.Dec:                postfix,
}

type Parser struct {
	l        *lexer.Lexer
	tokens   []token.Token
	position int
	errors   []string

	curToken  token.Token
	peekToken token.Token
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l}
	for {
		tok := l.NextToken()
		p.tokens = append(p.tokens, tok)
		if tok.Type == token.EOF {
			break
		}
	}
	p.nextToken()
	p.nextToken()
	return p
}

// ParseTypeRef parses a standalone type expression such as `?list<int>` or `mod.Client`.
func ParseTypeRef(source string) (*ast.TypeRef, error) {
	p := New(lexer.New(source))
	ref := p.parseTypeRefFromCurrent()
	if len(p.errors) == 0 && !p.peekTokenIs(token.EOF) {
		p.errors = append(p.errors, fmt.Sprintf("unexpected %s after type", p.peekToken.Literal))
	}
	if len(p.errors) > 0 || ref == nil {
		return nil, fmt.Errorf("invalid type %q: %s", source, strings.Join(p.errors, "; "))
	}
	return ref, nil
}

// Comments returns the source comments captured by the lexer (used by the formatter; the AST itself ignores comments).
func (p *Parser) Comments() []lexer.Comment { return p.l.Comments() }

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	for !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}
	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.Semicolon:
		return nil
	case token.Module:
		return p.parseModuleStatement()
	case token.Import:
		return p.parseImportStatement()
	case token.Export:
		return p.parseExportStatement()
	case token.Init:
		return p.parseInitStatement()
	case token.TypeKw:
		return p.parseTypeAliasStatement()
	case token.At:
		return p.parseDecoratedStatement()
	case token.Const, token.Let:
		return p.parseDeclarationStatement()
	case token.LBrace:
		if p.peekTokenIs(token.Ident) {
			return p.parseDestructuringAssignmentStatement()
		}
	case token.Async:
		if p.peekTokenIs(token.Func) {
			return p.parseFunctionStatement(false)
		}
	case token.Static:
		if p.peekTokenIs(token.Func) {
			return p.parseFunctionStatement(true)
		}
		return p.parseDeclarationStatement()
	case token.Func:
		// `func IDENT ;` and `func IDENT = ...` are typed declarations
		// (e.g. inside a class body: `func cb;` or `func cb = handler;`).
		// `func IDENT (` is a function definition. Probe with a two-token
		// lookahead via p.tokens (curToken sits at p.position-2,
		// peekToken at p.position-1, so p.tokens[p.position] is the
		// third token of the window).
		if p.peekTokenIs(token.Ident) && p.position < len(p.tokens) {
			third := p.tokens[p.position].Type
			if third == token.Semicolon || third == token.Assign {
				return p.parseDeclarationStatement()
			}
		}
		return p.parseFunctionStatement(false)
	case token.Class:
		return p.parseClassStatement()
	case token.Interface:
		return p.parseInterfaceStatement()
	case token.If:
		return p.parseIfStatement()
	case token.While:
		return p.parseWhileStatement()
	case token.With:
		return p.parseWithStatement()
	case token.Del:
		return p.parseDelStatement()
	case token.For:
		return p.parseForStatement()
	case token.Return:
		return p.parseReturnStatement()
	case token.Yield:
		return p.parseYieldStatement()
	case token.Break, token.Continue:
		return p.parseKeywordOnlyStatement()
	case token.Defer, token.Throw:
		return p.parseSimpleValueStatement()
	case token.Try:
		return p.parseTryStatement()
	case token.Match:
		return p.parseMatchStatement()
	case token.Enum:
		return p.parseEnumStatement()
	}
	if p.curTokenIs(token.Ident) && p.curToken.Literal == "select" && p.peekTokenIs(token.LBrace) {
		return p.parseSelectStatement()
	}

	if p.looksLikeFromImport() {
		return p.parseFromImportStatement()
	}
	if p.looksLikeTypedDeclaration() {
		return p.parseDeclarationStatement()
	}
	if p.looksLikeMultiAssign() {
		return p.parseMultiAssignStatement()
	}
	return p.parseExpressionStatement(true)
}

// looksLikeFromImport reports whether the upcoming tokens form a
// from-import statement: `from IDENT (. IDENT)* import ...`. `from`
// stays an identifier in the lexer so existing `int from`-style
// parameter names still parse. Path segments accept any identifier-
// name token so reserved words like `not` work as path components,
// matching the existing parsePath helper.
func (p *Parser) looksLikeFromImport() bool {
	if !p.curTokenIs(token.Ident) || p.curToken.Literal != "from" {
		return false
	}
	if !isIdentifierNameToken(p.peekToken.Type) {
		return false
	}
	idx := p.position
	for idx < len(p.tokens) && p.tokens[idx].Type == token.Dot {
		if idx+1 >= len(p.tokens) || !isIdentifierNameToken(p.tokens[idx+1].Type) {
			return false
		}
		idx += 2
	}
	return idx < len(p.tokens) && p.tokens[idx].Type == token.Import
}

// looksLikeMultiAssign reports whether the upcoming tokens form a
// multi-target assignment statement (`a, b = ...` or `a, b, c = ...`).
// Triggered when curToken is an identifier and the comma-list ends with
// an `=`. Index expressions and selectors on the LHS aren't supported yet;
// users should fall back to `let [a, b] = ...` for those.
func (p *Parser) looksLikeMultiAssign() bool {
	if !p.curTokenIs(token.Ident) {
		return false
	}
	if !p.peekTokenIs(token.Comma) {
		return false
	}
	// Scan: IDENT (, IDENT)+ =. p.position is the index of the token
	// after peekToken; curToken sits at p.position-2, peekToken at
	// p.position-1.
	i := p.position - 1 // index of peekToken (the first ',')
	if i < 0 || i >= len(p.tokens) {
		return false
	}
	for i+1 < len(p.tokens) && p.tokens[i].Type == token.Comma && p.tokens[i+1].Type == token.Ident {
		i += 2
	}
	return i < len(p.tokens) && p.tokens[i].Type == token.Assign
}

// parseMultiAssignStatement parses `a, b = rhs;` where the RHS may be
// either a comma-separated list of expressions of matching arity, or a
// single expression that evaluates to a list. The statement compiles
// down to the existing DestructuringStatement infrastructure.
func (p *Parser) parseMultiAssignStatement() ast.Statement {
	stmt := &ast.DestructuringStatement{Token: p.curToken, IsList: true, Bare: true}
	for {
		if !p.curTokenIs(token.Ident) {
			p.errorf(p.curToken, "expected identifier in multi-assignment target")
			return stmt
		}
		stmt.Names = append(stmt.Names, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken() // consume ','
		p.nextToken() // advance to next identifier
	}
	if !p.expectPeek(token.Assign) {
		return stmt
	}
	p.nextToken()
	// If the RHS is a comma-separated list of expressions, pack them into
	// a single ListLiteral so the existing destructuring path can unpack
	// it. A single expression is left as-is; it must evaluate to a list
	// of matching length at runtime.
	first := p.parseExpression(lowest)
	if p.peekTokenIs(token.Comma) {
		elements := []ast.Expression{first}
		for p.peekTokenIs(token.Comma) {
			p.nextToken() // consume ','
			p.nextToken()
			elements = append(elements, p.parseExpression(lowest))
		}
		stmt.Value = &ast.ListLiteral{Token: stmt.Token, Elements: elements, Bare: true}
	} else {
		stmt.Value = first
	}
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseDecoratedStatement() ast.Statement {
	decorators := []ast.Decorator{}
	doc := p.curToken.Doc
	for p.curTokenIs(token.At) {
		decorator := ast.Decorator{Token: p.curToken}
		if !p.expectPeek(token.Ident) {
			return nil
		}
		nameToken := p.curToken
		name := p.curToken.Literal
		/* Dotted decorator name: `@Foo.bar` and `@Foo.bar.baz`
		 * are valid identifiers in the decorator namespace.
		 * Used by framework-style families like `@Assert.email` /
		 * `@Assert.minLength(2)` where the parts before the dot
		 * group related rules under a common prefix. The whole
		 * dotted name is stored as a single identifier value -
		 * decorator dispatch is by exact string match. */
		for p.peekTokenIs(token.Dot) {
			p.nextToken()
			if !p.expectPeekIdentifierName() {
				return nil
			}
			name = name + "." + p.curToken.Literal
		}
		decorator.Name = &ast.Identifier{Token: nameToken, Value: name}
		if p.peekTokenIs(token.LParen) {
			p.nextToken()
			decorator.Arguments = p.parseCallArguments()
		}
		decorators = append(decorators, decorator)
		p.nextToken()
	}
	stmt := p.parseStatement()
	switch stmt := stmt.(type) {
	case *ast.FunctionStatement:
		stmt.Decorators = decorators
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	case *ast.ClassStatement:
		stmt.Decorators = decorators
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	case *ast.DeclarationStatement:
		/* Field-level decorator: legal inside a class body but not
		 * at the top level. The class-body parser checks this. We
		 * stash the decorators on the DeclarationStatement for the
		 * class-body parser to harvest into ClassStatement.FieldDecorators. */
		stmt.Decorators = decorators
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	case *ast.ExportStatement:
		switch inner := stmt.Statement.(type) {
		case *ast.FunctionStatement:
			inner.Decorators = decorators
			if inner.Doc == "" {
				inner.Doc = doc
			}
		case *ast.ClassStatement:
			inner.Decorators = decorators
			if inner.Doc == "" {
				inner.Doc = doc
			}
		default:
			p.errorf(p.curToken, "decorators can only be applied to functions, classes, or fields")
		}
	default:
		p.errorf(p.curToken, "decorators can only be applied to functions, classes, or fields")
	}
	return stmt
}

func (p *Parser) parseModuleStatement() ast.Statement {
	stmt := &ast.ModuleStatement{Token: p.curToken}
	stmt.Path = p.parsePath()
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseImportStatement() ast.Statement {
	stmt := &ast.ImportStatement{Token: p.curToken}
	stmt.Path, stmt.ForceBuiltin = ast.NormalizeReservedImportPath(p.parsePath())
	if p.peekTokenIs(token.As) {
		p.nextToken()
		if p.expectPeek(token.Ident) {
			stmt.Alias = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		}
	}
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseFromImportStatement() ast.Statement {
	stmt := &ast.FromImportStatement{Token: p.curToken}
	p.nextToken() // advance to first path component
	stmt.Path, stmt.ForceBuiltin = ast.NormalizeReservedImportPath(p.parsePathHere())
	if !p.expectPeek(token.Import) {
		return stmt
	}
	for {
		if !p.expectPeek(token.Ident) {
			return stmt
		}
		name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		entry := ast.FromImportName{Name: name}
		if p.peekTokenIs(token.As) {
			p.nextToken()
			if !p.expectPeek(token.Ident) {
				return stmt
			}
			entry.Alias = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		}
		stmt.Names = append(stmt.Names, entry)
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken()
	}
	p.expectPeek(token.Semicolon)
	return stmt
}

// parsePathHere reads NAME (DOT NAME)* starting at the current token.
// Unlike parsePath which is called with curToken on `import` or
// `module`, this variant assumes curToken is already the first name.
func (p *Parser) parsePathHere() []string {
	parts := []string{p.curToken.Literal}
	for p.peekTokenIs(token.Dot) {
		p.nextToken()
		if !p.expectPeekIdentifierName() {
			break
		}
		parts = append(parts, p.curToken.Literal)
	}
	return parts
}

func (p *Parser) parseExportStatement() ast.Statement {
	stmt := &ast.ExportStatement{Token: p.curToken}
	doc := p.curToken.Doc
	p.nextToken()
	stmt.Statement = p.parseStatement()
	applyDocComment(stmt.Statement, doc)
	return stmt
}

func applyDocComment(stmt ast.Statement, doc string) {
	if doc == "" || stmt == nil {
		return
	}
	switch stmt := stmt.(type) {
	case *ast.FunctionStatement:
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	case *ast.ClassStatement:
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	case *ast.InterfaceStatement:
		if stmt.Doc == "" {
			stmt.Doc = doc
		}
	}
}

func (p *Parser) parseInitStatement() ast.Statement {
	stmt := &ast.InitStatement{Token: p.curToken}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseTypeAliasStatement() ast.Statement {
	stmt := &ast.TypeAliasStatement{Token: p.curToken}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.Assign) {
		return stmt
	}
	p.nextToken()
	stmt.Type = p.parseTypeRefFromCurrent()
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseDeclarationStatement() ast.Statement {
	stmt := &ast.DeclarationStatement{Token: p.curToken, Doc: p.curToken.Doc}
	static := false
	if p.curTokenIs(token.Static) {
		static = true
		// `static const NAME = ...`, `static let NAME = ...`, or a
		// typed `static <type> NAME = ...` form. The typed form treats
		// the next token as the start of a TypeRef just like a regular
		// instance field declaration.
		switch {
		case p.peekTokenIs(token.Const), p.peekTokenIs(token.Let):
			p.nextToken()
		default:
			p.nextToken()
		}
		stmt.Token = p.curToken
	}

	if p.curTokenIs(token.Const) || p.curTokenIs(token.Let) {
		stmt.Kind = p.curToken.Literal
		if static {
			stmt.Kind = "static " + stmt.Kind
		}
		if !static && (p.peekTokenIs(token.LBracket) || p.peekTokenIs(token.LBrace)) {
			return p.parseDestructuringStatement(p.curToken)
		}
		if !p.expectPeek(token.Ident) {
			return stmt
		}
		first := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		if p.peekTokenIs(token.Comma) {
			// `let a, b = f()`: bracket-less list-destructuring declaration.
			ds := &ast.DestructuringStatement{Token: stmt.Token, Define: true, IsList: true, Bare: true}
			ds.Names = append(ds.Names, first)
			for p.peekTokenIs(token.Comma) {
				p.nextToken()
				if !p.expectPeek(token.Ident) {
					return ds
				}
				ds.Names = append(ds.Names, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
			}
			if !p.expectPeek(token.Assign) {
				return ds
			}
			p.nextToken()
			rhs := p.parseExpression(lowest)
			if p.peekTokenIs(token.Comma) {
				elements := []ast.Expression{rhs}
				for p.peekTokenIs(token.Comma) {
					p.nextToken()
					p.nextToken()
					elements = append(elements, p.parseExpression(lowest))
				}
				ds.Value = &ast.ListLiteral{Token: ds.Token, Elements: elements, Bare: true}
			} else {
				ds.Value = rhs
			}
			p.expectPeek(token.Semicolon)
			return ds
		}
		if p.peekTokenIs(token.Assign) || p.peekTokenIs(token.Semicolon) {
			stmt.Name = first
		} else {
			stmt.Type = p.parseTypeRefFromCurrent()
			if !p.expectPeek(token.Ident) {
				return stmt
			}
			stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		}
	} else {
		stmt.Type = p.parseTypeRefFromCurrent()
		if !p.expectPeek(token.Ident) {
			return stmt
		}
		stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		if static {
			// Typed static class member: `static int count = 0;` is
			// mutable and behaves like static let with a declared type.
			stmt.Kind = "static let"
		}
	}

	if p.peekTokenIs(token.Assign) {
		p.nextToken()
		p.nextToken()
		stmt.Value = p.parseExpression(lowest)
	}
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseDestructuringStatement(letTok token.Token) *ast.DestructuringStatement {
	stmt := &ast.DestructuringStatement{Token: letTok, Define: true}
	p.nextToken() // consume '[' or '{'
	p.parseDestructuringPattern(stmt)
	if !p.expectPeek(token.Assign) {
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseDestructuringAssignmentStatement() *ast.DestructuringStatement {
	stmt := &ast.DestructuringStatement{Token: p.curToken}
	p.parseDestructuringPattern(stmt)
	if !p.expectPeek(token.Assign) {
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseDestructuringPattern(stmt *ast.DestructuringStatement) {
	if p.curTokenIs(token.LBracket) {
		stmt.IsList = true
		p.nextToken()
		for !p.curTokenIs(token.RBracket) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.Ident) {
				stmt.Names = append(stmt.Names, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
			}
			p.nextToken()
			if p.curTokenIs(token.Comma) {
				p.nextToken()
			}
		}
		// curToken = RBracket
	} else {
		// LBrace
		stmt.IsList = false
		p.nextToken()
		for !p.curTokenIs(token.RBrace) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.Ident) {
				name := p.curToken.Literal
				stmt.Names = append(stmt.Names, &ast.Identifier{Token: p.curToken, Value: name})
				stmt.Keys = append(stmt.Keys, name)
			}
			p.nextToken()
			if p.curTokenIs(token.Comma) {
				p.nextToken()
			}
		}
		// curToken = RBrace
	}
}

func (p *Parser) parseFunctionStatement(static bool) ast.Statement {
	stmt := &ast.FunctionStatement{Static: static, Doc: p.curToken.Doc}
	if p.curTokenIs(token.Async) {
		stmt.Async = true
		p.nextToken()
	}
	if p.curTokenIs(token.Static) {
		stmt.Static = true
		p.nextToken()
	}
	stmt.Token = p.curToken
	if stmt.Doc == "" {
		stmt.Doc = p.curToken.Doc
	}
	if !p.curTokenIs(token.Func) {
		p.errorf(p.curToken, "expected func, got %s", p.curToken.Type)
		return stmt
	}
	if !p.expectPeekIdentifierName() {
		return stmt
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	stmt.Generics = p.parseGenericNames()
	stmt.Parameters = p.parseParameterList()
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		stmt.ReturnType = p.parseTypeRefFromCurrent()
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseClassStatement() ast.Statement {
	stmt := &ast.ClassStatement{Token: p.curToken, Doc: p.curToken.Doc}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	stmt.Generics = p.parseGenericNames()
	if p.peekTokenIs(token.Extends) {
		p.nextToken()
		p.nextToken()
		stmt.Extends = p.parseTypeRefFromCurrent()
	}
	if p.peekTokenIs(token.Implements) {
		p.nextToken()
		stmt.Implements = p.parseTypeList()
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		// `func ~ClassName()` is a destructor declaration. We
		// detect it here (rather than in the generic statement
		// dispatcher) so the destructor lands on
		// ClassStatement.Destructor instead of Members, and the
		// semantic analyser can validate the name matches the
		// enclosing class.
		if p.curTokenIs(token.Func) && p.peekTokenIs(token.BitNot) {
			if dtor := p.parseDestructorStatement(stmt.Name.Value); dtor != nil {
				if stmt.Destructor != nil {
					p.errorf(dtor.Token, "class %s may declare only one destructor", stmt.Name.Value)
				} else {
					stmt.Destructor = dtor
				}
			}
			continue
		}
		member := p.parseStatement()
		if member != nil {
			stmt.Members = append(stmt.Members, member)
			/* Field decorators live on DeclarationStatement.Decorators
			 * when parsed, but downstream consumers (reflect, the
			 * VM's class compiler) want a tidy `field name -> decorators`
			 * lookup on the parent class. Lift them here. */
			if decl, ok := member.(*ast.DeclarationStatement); ok && len(decl.Decorators) > 0 && decl.Name != nil {
				if stmt.FieldDecorators == nil {
					stmt.FieldDecorators = map[string][]ast.Decorator{}
				}
				stmt.FieldDecorators[decl.Name.Value] = decl.Decorators
			}
		}
	}
	return stmt
}

// parseDestructorStatement parses `func ~ClassName() { ... }` from a
// class body. Returns nil on a hard parse failure (errors already
// recorded). The semantic analyser later checks that the name
// matches the enclosing class.
func (p *Parser) parseDestructorStatement(expectedName string) *ast.FunctionStatement {
	stmt := &ast.FunctionStatement{Token: p.curToken, Doc: p.curToken.Doc}
	// cur=Func, peek=BitNot.
	if !p.expectPeek(token.BitNot) {
		return nil
	}
	if !p.expectPeek(token.Ident) {
		return nil
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if stmt.Name.Value != expectedName {
		p.errorf(stmt.Name.Token, "destructor name %q does not match enclosing class %q", stmt.Name.Value, expectedName)
	}
	stmt.Parameters = p.parseParameterList()
	if len(stmt.Parameters) != 0 {
		p.errorf(stmt.Token, "destructor %s must take no arguments", stmt.Name.Value)
	}
	// Destructors don't declare a return type; if the user wrote
	// one, accept and ignore (semantic analyser will warn).
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		stmt.ReturnType = p.parseTypeRefFromCurrent()
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseInterfaceStatement() ast.Statement {
	stmt := &ast.InterfaceStatement{Token: p.curToken, Doc: p.curToken.Doc}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	stmt.Generics = p.parseGenericNames()
	if p.peekTokenIs(token.Extends) {
		p.nextToken()
		stmt.Parents = p.parseTypeList()
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		if p.curTokenIs(token.Func) {
			p.parseInterfaceMember(stmt)
			continue
		}
		if p.canStartFieldDeclaration() {
			if field := p.parseInterfaceField(); field != nil {
				stmt.Fields = append(stmt.Fields, field)
			}
			continue
		}
		p.errorf(p.curToken, "expected interface method signature, default method, or field, got %s", p.curToken.Type)
		p.synchronize()
	}
	return stmt
}

func (p *Parser) parseInterfaceMember(stmt *ast.InterfaceStatement) {
	startToken := p.curToken
	startDoc := p.curToken.Doc
	if !p.expectPeekIdentifierName() {
		return
	}
	name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	generics := p.parseGenericNames()
	params := p.parseParameterList()
	var returnType *ast.TypeRef
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		returnType = p.parseTypeRefFromCurrent()
	}
	if p.peekTokenIs(token.LBrace) {
		p.nextToken()
		body := p.parseBlockStatement()
		stmt.Defaults = append(stmt.Defaults, &ast.FunctionStatement{
			Token: startToken, Doc: startDoc, Name: name,
			Generics: generics, Parameters: params, ReturnType: returnType, Body: body,
		})
		return
	}
	p.expectPeek(token.Semicolon)
	stmt.Methods = append(stmt.Methods, &ast.FunctionSignature{
		Token: startToken, Doc: startDoc, Name: name,
		Generics: generics, Parameters: params, ReturnType: returnType,
	})
}

func (p *Parser) canStartFieldDeclaration() bool {
	return p.curTokenIs(token.Question) || isTypeStartToken(p.curToken.Type)
}

func (p *Parser) parseInterfaceField() *ast.DeclarationStatement {
	decl := &ast.DeclarationStatement{Token: p.curToken}
	decl.Type = p.parseTypeRefFromCurrent()
	if !p.expectPeek(token.Ident) {
		return nil
	}
	decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	p.expectPeek(token.Semicolon)
	return decl
}

func (p *Parser) parseIfStatement() ast.Statement {
	stmt := &ast.IfStatement{Token: p.curToken}
	stmt.Condition = p.parseParenthesizedExpression()
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Consequence = p.parseBlockStatement()
	for p.peekTokenIs(token.ElseIf) || (p.peekTokenIs(token.Else) && p.peekNextTokenIs(token.If)) {
		if p.peekTokenIs(token.Else) {
			p.nextToken()
		}
		p.nextToken()
		condition := p.parseParenthesizedExpression()
		if !p.expectPeek(token.LBrace) {
			return stmt
		}
		stmt.ElseIfs = append(stmt.ElseIfs, ast.ElseIfClause{Condition: condition, Body: p.parseBlockStatement()})
	}
	if p.peekTokenIs(token.Else) {
		p.nextToken()
		if !p.expectPeek(token.LBrace) {
			return stmt
		}
		stmt.Alternative = p.parseBlockStatement()
	}
	return stmt
}

func (p *Parser) parseWhileStatement() ast.Statement {
	stmt := &ast.WhileStatement{Token: p.curToken}
	stmt.Condition = p.parseParenthesizedExpression()
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseDelStatement() ast.Statement {
	stmt := &ast.DelStatement{Token: p.curToken}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Target = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	// `del a.b` / `del a[i]` are not supported; flag and skip to
	// the statement terminator so the rest of the file parses.
	if p.peekTokenIs(token.Dot) || p.peekTokenIs(token.LBracket) {
		p.errorf(p.peekToken, "del only supports identifiers, not field selectors or index expressions")
		for !p.peekTokenIs(token.Semicolon) && !p.peekTokenIs(token.EOF) {
			p.nextToken()
		}
	}
	return stmt
}

func (p *Parser) parseWithStatement() ast.Statement {
	stmt := &ast.WithStatement{Token: p.curToken}
	if !p.expectPeek(token.LParen) {
		return stmt
	}
	p.nextToken()
	// `with (name = expr)` binds; bare `with (expr)` doesn't. We
	// peek for `=` after an identifier to disambiguate, leaving
	// general expressions (e.g. `foo()` returning a resource) on
	// the non-binding path.
	if p.curTokenIs(token.Ident) && p.peekTokenIs(token.Assign) {
		stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		p.nextToken()
		p.nextToken()
	}
	stmt.Value = p.parseExpression(lowest)
	if !p.expectPeek(token.RParen) {
		return stmt
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseForStatement() ast.Statement {
	stmt := &ast.ForStatement{Token: p.curToken}
	if !p.expectPeek(token.LParen) {
		return stmt
	}
	p.nextToken()

	if p.isForInClause() {
		if p.curTokenIs(token.Ident) && p.peekTokenIs(token.Comma) {
			stmt.VarNames = append(stmt.VarNames, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
			for p.peekTokenIs(token.Comma) {
				p.nextToken()
				if !p.expectPeek(token.Ident) {
					return stmt
				}
				stmt.VarNames = append(stmt.VarNames, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
			}
			if !p.expectPeek(token.In) {
				return stmt
			}
		} else if p.curTokenIs(token.Ident) && p.peekTokenIs(token.In) {
			stmt.VarName = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
			p.expectPeek(token.In)
		} else {
			stmt.VarType = p.parseTypeRefFromCurrent()
			if !p.expectPeek(token.Ident) {
				return stmt
			}
			stmt.VarName = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
			p.expectPeek(token.In)
		}
		p.nextToken()
		stmt.Iterable = p.parseExpression(lowest)
		if p.peekTokenIs(token.By) {
			p.nextToken()
			p.nextToken()
			stmt.Step = p.parseExpression(lowest)
		}
		p.expectPeek(token.RParen)
	} else {
		if !p.curTokenIs(token.Semicolon) {
			if p.curTokenIs(token.Let) || p.curTokenIs(token.Const) || p.curTokenIs(token.Question) ||
				(p.curTokenIs(token.Ident) && (p.peekTokenIs(token.Ident) || p.peekTokenIs(token.LT) || p.peekTokenIs(token.LBracket))) {
				stmt.Init = p.parseDeclarationStatement()
			} else {
				stmt.Init = p.parseExpressionStatement(false)
				p.expectPeek(token.Semicolon)
			}
		}
		if p.curTokenIs(token.Semicolon) {
			p.nextToken()
		}
		if !p.curTokenIs(token.Semicolon) {
			stmt.Condition = p.parseExpression(lowest)
		}
		p.expectPeek(token.Semicolon)
		p.nextToken()
		if !p.curTokenIs(token.RParen) {
			stmt.Update = p.parseExpressionStatement(false)
		}
		if !p.curTokenIs(token.RParen) {
			p.expectPeek(token.RParen)
		}
	}

	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseReturnStatement() ast.Statement {
	stmt := &ast.ReturnStatement{Token: p.curToken}
	if p.peekTokenIs(token.Semicolon) {
		p.nextToken()
		return stmt
	}
	p.nextToken()
	first := p.parseExpression(lowest)
	if p.peekTokenIs(token.Comma) {
		// `return a, b` yields a list for a matching multi-assignment.
		elements := []ast.Expression{first}
		for p.peekTokenIs(token.Comma) {
			p.nextToken()
			p.nextToken()
			elements = append(elements, p.parseExpression(lowest))
		}
		stmt.Value = &ast.ListLiteral{Token: stmt.Token, Elements: elements, Bare: true}
	} else {
		stmt.Value = first
	}
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseYieldStatement() ast.Statement {
	stmt := &ast.YieldStatement{Token: p.curToken}
	if p.peekTokenIs(token.Semicolon) {
		p.nextToken()
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseKeywordOnlyStatement() ast.Statement {
	stmt := &ast.SimpleStatement{Token: p.curToken, Kind: p.curToken.Literal}
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseSimpleValueStatement() ast.Statement {
	stmt := &ast.SimpleStatement{Token: p.curToken, Kind: p.curToken.Literal}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectPeek(token.Semicolon)
	return stmt
}

func (p *Parser) parseTryStatement() ast.Statement {
	stmt := &ast.TryStatement{Token: p.curToken}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStatement()
	for p.peekTokenIs(token.Catch) {
		p.nextToken()
		clause := ast.CatchClause{}
		if p.peekTokenIs(token.LParen) {
			p.nextToken()
			p.nextToken()
			clause.Type = p.parseTypeRefFromCurrent()
			if p.peekTokenIs(token.Ident) {
				p.nextToken()
				clause.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
			}
			p.expectPeek(token.RParen)
		}
		if !p.expectPeek(token.LBrace) {
			return stmt
		}
		clause.Body = p.parseBlockStatement()
		stmt.Catches = append(stmt.Catches, clause)
	}
	if p.peekTokenIs(token.Finally) {
		p.nextToken()
		if !p.expectPeek(token.LBrace) {
			return stmt
		}
		stmt.Finally = p.parseBlockStatement()
	}
	return stmt
}

func (p *Parser) parseMatchStatement() ast.Statement {
	expr := p.parseMatchExpression()
	return &ast.MatchStatement{Token: expr.Token, Expr: expr.Expr, Cases: expr.Cases}
}

func (p *Parser) parseSelectStatement() ast.Statement {
	stmt := &ast.SelectStatement{Token: p.curToken}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		switch p.curToken.Type {
		case token.Default:
			if !p.expectPeek(token.Colon) {
				return stmt
			}
			p.nextToken()
			stmt.Default = p.parseSelectCaseBody()
		case token.Case:
			c := ast.SelectCase{Token: p.curToken}
			p.nextToken()
			if p.curTokenIs(token.Let) {
				if !p.expectPeek(token.Ident) {
					return stmt
				}
				c.Binding = p.curToken.Literal
				if !p.expectPeek(token.Assign) {
					return stmt
				}
				p.nextToken()
			}
			expr := p.parseExpression(lowest)
			call, ok := expr.(*ast.CallExpression)
			if !ok {
				p.errorf(c.Token, "select case head must be a channel.recv() or channel.send(...) call")
				return stmt
			}
			selector, ok := call.Callee.(*ast.SelectorExpression)
			if !ok || selector.Name == nil {
				p.errorf(c.Token, "select case head must be a method call")
				return stmt
			}
			switch selector.Name.Value {
			case "recv":
				if len(call.Arguments) != 0 {
					p.errorf(c.Token, "recv() takes no arguments")
					return stmt
				}
				c.Kind = "recv"
				c.Channel = selector.Object
			case "send":
				if c.Binding != "" {
					p.errorf(c.Token, "send case cannot bind a variable")
					return stmt
				}
				if len(call.Arguments) != 1 {
					p.errorf(c.Token, "send(value) takes exactly one argument")
					return stmt
				}
				c.Kind = "send"
				c.Channel = selector.Object
				c.Value = call.Arguments[0].Value
			default:
				p.errorf(c.Token, "select case must be .recv() or .send(...), got .%s", selector.Name.Value)
				return stmt
			}
			if !p.expectPeek(token.Colon) {
				return stmt
			}
			p.nextToken()
			c.Body = p.parseSelectCaseBody()
			stmt.Cases = append(stmt.Cases, c)
		default:
			p.errorf(p.curToken, "expected case or default in select, got %s", p.curToken.Type)
			p.synchronize()
		}
	}
	return stmt
}

func (p *Parser) parseSelectCaseBody() *ast.BlockStatement {
	if p.curTokenIs(token.LBrace) {
		return p.parseBlockStatement()
	}
	body := &ast.BlockStatement{Token: p.curToken}
	for !p.curTokenIs(token.Case) && !p.curTokenIs(token.Default) && !p.curTokenIs(token.RBrace) && !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			body.Statements = append(body.Statements, stmt)
		}
		p.nextToken()
	}
	p.rewindOne()
	return body
}

func (p *Parser) parseExpressionStatement(requireSemicolon bool) ast.Statement {
	stmt := &ast.ExpressionStatement{Token: p.curToken}
	stmt.Expression = p.parseExpression(lowest)
	if requireSemicolon {
		p.expectPeek(token.Semicolon)
	}
	return stmt
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		stmt := p.parseStatement()
		if stmt != nil {
			/* Top-level-only declarations: `class`, `interface`, and
			 * `enum` carry semantic state (the class table, the
			 * runtime type registry) that only makes sense when
			 * declared at the file or module top level. Nested
			 * declarations - inside a function body, a loop, or any
			 * other block - aren't currently supported; flag them
			 * with a clear message rather than letting the analyser
			 * fail with a confusing downstream error. */
			switch v := stmt.(type) {
			case *ast.ClassStatement:
				p.errorf(v.Token, "class declaration is only allowed at the top level, not inside a block")
			case *ast.InterfaceStatement:
				p.errorf(v.Token, "interface declaration is only allowed at the top level, not inside a block")
			case *ast.EnumStatement:
				p.errorf(v.Token, "enum declaration is only allowed at the top level, not inside a block")
			}
			block.Statements = append(block.Statements, stmt)
		}
	}
	block.End = p.curToken // RBrace (or EOF), so endLine sees the block's closing line
	return block
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	left := p.parsePrefix()
	if left == nil {
		return nil
	}

	for !p.peekTokenIs(token.Semicolon) && !p.peekTokenIs(token.RParen) && !p.peekTokenIs(token.RBracket) &&
		!p.peekTokenIs(token.Comma) && !p.peekTokenIs(token.Colon) && !p.peekTokenIs(token.Arrow) &&
		precedence < p.peekPrecedence() {
		p.nextToken()
		left = p.parseInfix(left)
		if left == nil {
			return nil
		}
	}
	return left
}

func (p *Parser) parsePrefix() ast.Expression {
	switch p.curToken.Type {
	case token.Ident:
		return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	case token.This, token.Parent:
		return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	case token.Int:
		return &ast.IntegerLiteral{Token: p.curToken, Value: p.curToken.Literal}
	case token.Decimal:
		return &ast.DecimalLiteral{Token: p.curToken, Value: p.curToken.Literal}
	case token.Float:
		return &ast.FloatLiteral{Token: p.curToken, Value: p.curToken.Literal}
	case token.String:
		if p.curToken.Interpolated {
			return p.parseInterpolatedString(p.curToken)
		}
		return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal, Raw: p.curToken.Raw, Quote: p.curToken.Quote, Triple: p.curToken.Triple}
	case token.True:
		return &ast.Literal{Token: p.curToken, Value: true}
	case token.False:
		return &ast.Literal{Token: p.curToken, Value: false}
	case token.Null:
		return &ast.Literal{Token: p.curToken, Value: nil}
	case token.Bool:
		// `bool` used as a type-value expression (e.g. typeof(x) == bool)
		return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	case token.Bang, token.Minus, token.BitNot, token.Inc, token.Dec:
		expr := &ast.PrefixExpression{Token: p.curToken, Operator: p.curToken.Literal}
		p.nextToken()
		expr.Right = p.parseExpression(prefix)
		return expr
	case token.Await:
		expr := &ast.AwaitExpression{Token: p.curToken}
		p.nextToken()
		expr.Value = p.parseExpression(prefix)
		return expr
	case token.LParen:
		p.nextToken()
		expr := p.parseExpression(lowest)
		p.expectPeek(token.RParen)
		// Selector: routes `(obj.x)(args)` through value-then-call. Others: lets the formatter keep the author's grouping parens.
		switch e := expr.(type) {
		case *ast.SelectorExpression:
			e.Parenthesized = true
		case *ast.InfixExpression:
			e.Parenthesized = true
		case *ast.CastExpression:
			e.Parenthesized = true
		case *ast.TernaryExpression:
			e.Parenthesized = true
		case *ast.PrefixExpression:
			e.Parenthesized = true
		}
		return expr
	case token.LBracket:
		return p.parseListLiteral()
	case token.LBrace:
		return p.parseDictLiteral()
	case token.Func:
		return p.parseFunctionLiteral(false)
	case token.Async:
		if p.peekTokenIs(token.Func) {
			p.nextToken()
			return p.parseFunctionLiteral(true)
		}
		return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	case token.Match:
		return p.parseMatchExpression()
	}
	p.errorf(p.curToken, "expected expression, got %s", p.curToken.Type)
	return nil
}

func (p *Parser) parseInfix(left ast.Expression) ast.Expression {
	switch p.curToken.Type {
	case token.Dot:
		if !p.expectPeekIdentifierName() {
			return left
		}
		return &ast.SelectorExpression{Token: p.curToken, Object: left, Name: &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}}
	case token.OptionalChain:
		if !p.expectPeekIdentifierName() {
			return left
		}
		return &ast.SelectorExpression{Token: p.curToken, Object: left, Name: &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}, Optional: true}
	case token.LParen:
		return p.parseCallExpression(left)
	case token.LBracket:
		return p.parseIndexExpression(left)
	case token.LT:
		if canBeGenericCallCallee(left) && p.looksLikeGenericCallTypeArgs() {
			return p.parseGenericCall(left)
		}
		return p.parseDefaultInfix(left)
	case token.Assign:
		expr := &ast.AssignmentExpression{Token: p.curToken, Left: left}
		p.nextToken()
		expr.Value = p.parseExpression(assign - 1)
		return expr
	case token.Inc, token.Dec:
		return &ast.PostfixExpression{Token: p.curToken, Left: left, Operator: p.curToken.Literal}
	case token.As:
		expr := &ast.CastExpression{Token: p.curToken, Value: left}
		p.nextToken()
		expr.Type = p.parseTypeRefFromCurrent()
		return expr
	case token.Range, token.RangeExcl:
		expr := &ast.RangeExpression{Token: p.curToken, Start: left, Exclusive: p.curTokenIs(token.RangeExcl)}
		if !p.peekTokenIs(token.RBracket) && !p.peekTokenIs(token.RParen) && !p.peekTokenIs(token.By) {
			p.nextToken()
			expr.End = p.parseExpression(compare)
		}
		if p.peekTokenIs(token.By) {
			p.nextToken()
			p.nextToken()
			expr.Step = p.parseExpression(compare)
		}
		return expr
	case token.Question:
		return p.parseTernaryExpression(left)
	case token.Pipe:
		expr := &ast.PipeExpression{Token: p.curToken, Left: left}
		p.nextToken()
		expr.Right = p.parseExpression(pipe)
		return expr
	case token.PlusAssign:
		return p.parseCompoundAssignment(left, token.Plus, "+")
	case token.MinusAssign:
		return p.parseCompoundAssignment(left, token.Minus, "-")
	case token.MulAssign:
		return p.parseCompoundAssignment(left, token.Asterisk, "*")
	case token.DivAssign:
		return p.parseCompoundAssignment(left, token.Slash, "/")
	case token.IntDivAssign:
		return p.parseCompoundAssignment(left, token.IntDiv, "//")
	case token.ModAssign:
		return p.parseCompoundAssignment(left, token.Percent, "%")
	case token.PowerAssign:
		return p.parseCompoundAssignment(left, token.Power, "**")
	case token.BitAndAssign:
		return p.parseCompoundAssignment(left, token.BitAnd, "&")
	case token.BitOrAssign:
		return p.parseCompoundAssignment(left, token.BitOr, "|")
	case token.BitXorAssign:
		return p.parseCompoundAssignment(left, token.BitXor, "^")
	case token.LShiftAssign:
		return p.parseCompoundAssignment(left, token.LShift, "<<")
	case token.RShiftAssign:
		return p.parseCompoundAssignment(left, token.RShift, ">>")
	case token.NullCoalesceAssign:
		return p.parseCompoundAssignment(left, token.NullCoalesce, "??")
	default:
		return p.parseDefaultInfix(left)
	}
}

func (p *Parser) parseDefaultInfix(left ast.Expression) ast.Expression {
	expr := &ast.InfixExpression{Token: p.curToken, Left: left, Operator: p.curToken.Literal}
	precedence := p.curPrecedence()
	if p.curTokenIs(token.Power) {
		precedence--
	}
	if p.curTokenIs(token.Is) && p.peekTokenIs(token.Not) {
		p.nextToken()
		expr.Operator = "is not"
	}
	// `instanceof` accepts a TypeRef on the right (e.g. `list<int>`,
	// `?dict<string, User>`) - not just a bare identifier. Detect the
	// instanceof form here and parse a TypeRef instead of a regular
	// expression, then stash the stringified TypeRef as an Identifier
	// so the evaluator's existing instanceof dispatch handles it. The
	// downstream `valueMatchesType` understands generic-arg syntax in
	// the name.
	if p.curTokenIs(token.InstanceOf) {
		opTok := p.curToken
		p.nextToken()
		typ := p.parseTypeRefFromCurrent()
		ident := &ast.Identifier{Token: opTok, Value: typ.String()}
		expr.Right = ident
		expr.RightType = typ
		return expr
	}
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

// canBeGenericCallCallee reports whether the given expression is one that may
// legitimately precede an explicit type-argument list in a call - bare
// identifiers (`assertIs<int>(x)`, `Box<string>()`) and selectors
// (`module.fn<T>(x)`). Other expression forms keep the original less-than
// interpretation.
func canBeGenericCallCallee(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.Identifier, *ast.SelectorExpression:
		return true
	default:
		return false
	}
}

// looksLikeGenericCallTypeArgs returns true when p.curToken is `<` and the
// tokens that follow form a syntactically-shaped generic type-argument list
// closing with `>` (or `>>` for a single nested level) immediately followed
// by `(`. The lookahead is structural - it only inspects token kinds, never
// invokes the actual TypeRef parser - so it must over-reject rather than
// over-accept. When this returns false the caller falls back to the regular
// less-than infix path so chained comparisons keep working.
func (p *Parser) looksLikeGenericCallTypeArgs() bool {
	if !p.curTokenIs(token.LT) {
		return false
	}
	// p.curToken sits at p.position-2 in the lexed token slice; the token
	// immediately following `<` is at p.position-1 (the current peek). Walk
	// forward from there, tracking nesting depth so nested generic
	// arguments like `Box<list<int>>(...)` close correctly.
	idx := p.position - 1
	depth := 1
	for idx < len(p.tokens) && depth > 0 {
		tok := p.tokens[idx].Type
		switch tok {
		case token.LT:
			depth++
		case token.GT:
			depth--
			if depth == 0 {
				idx++
				return idx < len(p.tokens) && p.tokens[idx].Type == token.LParen
			}
		case token.RShift:
			// `>>` closes two nested levels at once. At depth=1 a `>>` is
			// not a generic-call closer (it would over-close); bail and
			// let the standard infix path handle it.
			if depth < 2 {
				return false
			}
			depth -= 2
			if depth == 0 {
				idx++
				return idx < len(p.tokens) && p.tokens[idx].Type == token.LParen
			}
		case token.Ident, token.Bool, token.TypeKw,
			token.Comma, token.Dot, token.Question,
			token.LBracket, token.RBracket,
			token.BitOr, token.BitAnd:
			// Tokens that legitimately appear inside a TypeRef list.
		default:
			return false
		}
		idx++
	}
	return false
}

func (p *Parser) parseGenericCall(callee ast.Expression) ast.Expression {
	typeArgs := p.parseTypeArgumentsFromCurrent()
	if !p.expectPeek(token.LParen) {
		return callee
	}
	tok := p.curToken
	args := p.parseCallArguments()
	return p.buildCallOrPartial(callee, typeArgs, args, tok, p.curToken)
}

func (p *Parser) parseTernaryExpression(left ast.Expression) ast.Expression {
	expr := &ast.TernaryExpression{Token: p.curToken, Condition: left}
	p.nextToken()
	expr.ThenExpr = p.parseExpression(lowest)
	if !p.expectPeek(token.Colon) {
		return nil
	}
	p.nextToken()
	expr.ElseExpr = p.parseExpression(lowest)
	return expr
}

func (p *Parser) parseCompoundAssignment(left ast.Expression, opType token.Type, opLit string) ast.Expression {
	assignTok := p.curToken
	opTok := token.Token{Type: opType, Literal: opLit, Line: assignTok.Line, Column: assignTok.Column}
	p.nextToken()
	rhs := p.parseExpression(assign - 1)
	return &ast.AssignmentExpression{
		Token: assignTok,
		Left:  left,
		Value: &ast.InfixExpression{Token: opTok, Left: left, Operator: opLit, Right: rhs},
	}
}

func (p *Parser) parseCallExpression(callee ast.Expression) ast.Expression {
	tok := p.curToken
	args := p.parseCallArguments()
	return p.buildCallOrPartial(callee, nil, args, tok, p.curToken)
}

func (p *Parser) buildCallOrPartial(callee ast.Expression, typeArgs []*ast.TypeRef, args []ast.CallArgument, tok, end token.Token) ast.Expression {
	hasHole, hasSpread := false, false
	for _, a := range args {
		hasHole = hasHole || a.Hole
		hasSpread = hasSpread || a.Spread
	}
	if hasHole {
		if hasSpread {
			p.errorf(tok, "cannot combine a partial-application hole (_) with spread (...) in the same call")
		}
		return &ast.PartialExpression{Token: tok, TypeArguments: typeArgs, Callee: callee, Arguments: args, End: end}
	}
	return &ast.CallExpression{Token: tok, TypeArguments: typeArgs, Callee: callee, Arguments: args, End: end}
}

func (p *Parser) parseCallArguments() []ast.CallArgument {
	args := []ast.CallArgument{}
	if p.peekTokenIs(token.RParen) {
		p.nextToken()
		return args
	}
	for {
		p.nextToken()
		arg := ast.CallArgument{}
		if p.curTokenIs(token.Ellipsis) {
			arg.Spread = true
			p.nextToken()
		} else if p.curTokenIs(token.Ident) && p.peekTokenIs(token.Colon) {
			arg.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
			p.nextToken()
			p.nextToken()
		}
		arg.Value = p.parseExpression(lowest)
		if id, ok := arg.Value.(*ast.Identifier); ok && id.Value == "_" {
			arg.Hole = true
		}
		args = append(args, arg)
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken()
	}
	p.expectPeek(token.RParen)
	return args
}

func (p *Parser) parseIndexExpression(left ast.Expression) ast.Expression {
	expr := &ast.IndexExpression{Token: p.curToken, Left: left}
	// `xs[..end]` / `xs[..]` - open-start range using the `..` token.
	if p.peekTokenIs(token.Range) || p.peekTokenIs(token.RangeExcl) {
		p.nextToken()
		rng := &ast.RangeExpression{Token: p.curToken, Exclusive: p.curTokenIs(token.RangeExcl)}
		if !p.peekTokenIs(token.RBracket) {
			p.nextToken()
			rng.End = p.parseExpression(lowest)
		}
		expr.Index = rng
		p.expectPeek(token.RBracket)
		return expr
	}
	// `xs[]` - empty index reads as a full-range expression.
	if p.peekTokenIs(token.RBracket) {
		expr.Index = &ast.RangeExpression{Token: p.curToken}
		p.expectPeek(token.RBracket)
		return expr
	}
	// `xs[:end]` / `xs[::step]` / `xs[:end:step]` - Python-style slice
	// with optional end and optional step.
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		rng := &ast.RangeExpression{Token: p.curToken, Exclusive: true}
		if !p.peekTokenIs(token.RBracket) && !p.peekTokenIs(token.Colon) {
			p.nextToken()
			rng.End = p.parseExpression(lowest)
		}
		p.parseOptionalSliceStep(rng)
		expr.Index = rng
		p.expectPeek(token.RBracket)
		return expr
	}
	p.nextToken()
	first := p.parseExpression(lowest)
	// `xs[a:b]` / `xs[a:]` / `xs[a:b:step]` / `xs[a::step]` - Python-style slice.
	if p.peekTokenIs(token.Colon) {
		p.nextToken() // consume ':'
		rng := &ast.RangeExpression{Token: p.curToken, Exclusive: true, Start: first}
		if !p.peekTokenIs(token.RBracket) && !p.peekTokenIs(token.Colon) {
			p.nextToken()
			rng.End = p.parseExpression(lowest)
		}
		p.parseOptionalSliceStep(rng)
		expr.Index = rng
		p.expectPeek(token.RBracket)
		return expr
	}
	expr.Index = first
	p.expectPeek(token.RBracket)
	return expr
}

// parseOptionalSliceStep consumes a trailing `:step` from a Python-style
// slice expression, populating rng.Step if present. Caller is positioned
// after the end-expression (or after the first `:` if end was omitted).
func (p *Parser) parseOptionalSliceStep(rng *ast.RangeExpression) {
	if !p.peekTokenIs(token.Colon) {
		return
	}
	p.nextToken() // consume ':'
	if p.peekTokenIs(token.RBracket) {
		return
	}
	p.nextToken()
	rng.Step = p.parseExpression(lowest)
}

func (p *Parser) parseListLiteral() ast.Expression {
	openToken := p.curToken
	if p.peekTokenIs(token.RBracket) {
		p.nextToken()
		return &ast.ListLiteral{Token: openToken, End: p.curToken}
	}
	p.nextToken()
	var first ast.Expression
	if p.curTokenIs(token.Ellipsis) {
		spreadTok := p.curToken
		p.nextToken()
		first = &ast.SpreadExpression{Token: spreadTok, Value: p.parseExpression(lowest)}
	} else {
		first = p.parseExpression(lowest)
	}
	if p.peekTokenIs(token.For) {
		comp := &ast.ListComprehension{Token: openToken, Body: first}
		comp.Clauses = p.parseComprehensionTail()
		p.expectPeek(token.RBracket)
		return comp
	}
	lit := &ast.ListLiteral{Token: openToken, Elements: []ast.Expression{first}}
	for p.peekTokenIs(token.Comma) {
		p.nextToken()
		if p.peekTokenIs(token.RBracket) {
			break
		}
		p.nextToken()
		if p.curTokenIs(token.Ellipsis) {
			spreadTok := p.curToken
			p.nextToken()
			lit.Elements = append(lit.Elements, &ast.SpreadExpression{Token: spreadTok, Value: p.parseExpression(lowest)})
		} else {
			lit.Elements = append(lit.Elements, p.parseExpression(lowest))
		}
	}
	p.expectPeek(token.RBracket)
	lit.End = p.curToken
	return lit
}

func (p *Parser) parseDictLiteral() ast.Expression {
	openToken := p.curToken
	if p.peekTokenIs(token.RBrace) {
		p.nextToken()
		return &ast.DictLiteral{Token: openToken, End: p.curToken}
	}
	p.nextToken()
	// Leading spread `{...src, ...}` always means dict or set spread; we
	// don't yet know which until we see further entries (`...src, k: v`
	// is dict; `...src, x` is set). Parse the spread expression and a
	// trailing token to decide.
	if p.curTokenIs(token.Ellipsis) {
		spreadTok := p.curToken
		p.nextToken()
		spreadValue := p.parseExpression(lowest)
		spreadExpr := &ast.SpreadExpression{Token: spreadTok, Value: spreadValue}
		// Look at what comes after the spread. If the next non-comma
		// entry starts with `...` again or is a bare expression, we
		// can't decide here. Defer the set-vs-dict choice until we see
		// a `: ` (dict) or a comma-then-non-colon (set).
		if p.peekTokenIs(token.RBrace) {
			p.nextToken()
			_ = spreadExpr
			return &ast.DictLiteral{Token: openToken, Entries: []ast.DictEntry{{Value: spreadValue, Spread: true}}, End: p.curToken}
		}
		return p.continueDictOrSetAfterSpread(openToken, spreadValue)
	}
	first := p.parseExpression(lowest)
	if !p.peekTokenIs(token.Colon) {
		if p.peekTokenIs(token.For) {
			comp := &ast.SetComprehension{Token: openToken, Body: first}
			comp.Clauses = p.parseComprehensionTail()
			p.expectPeek(token.RBrace)
			return comp
		}
		return p.continueSetLiteral(openToken, first)
	}
	p.expectPeek(token.Colon)
	p.nextToken()
	firstValue := p.parseExpression(lowest)
	if p.peekTokenIs(token.For) {
		comp := &ast.DictComprehension{Token: openToken, KeyBody: first, ValueBody: firstValue}
		comp.Clauses = p.parseComprehensionTail()
		p.expectPeek(token.RBrace)
		return comp
	}
	return p.continueDictLiteral(openToken, []ast.DictEntry{{Key: first, Value: firstValue}})
}

// continueDictOrSetAfterSpread handles the case where the first entry of a
// brace literal was `...src`. The literal is a dict if any subsequent entry
// is `key: value` OR if every entry is a spread (all-spreads defaults to
// dict merge - the most common pattern). It's a set if at least one entry
// is a bare element (without `:`).
func (p *Parser) continueDictOrSetAfterSpread(openToken token.Token, firstSpread ast.Expression) ast.Expression {
	entries := []parseSpreadBuffer{{spread: firstSpread}}
	for p.peekTokenIs(token.Comma) {
		p.nextToken()
		if p.peekTokenIs(token.RBrace) {
			break
		}
		p.nextToken()
		if p.curTokenIs(token.Ellipsis) {
			p.nextToken()
			entries = append(entries, parseSpreadBuffer{spread: p.parseExpression(lowest)})
			continue
		}
		expr := p.parseExpression(lowest)
		if p.peekTokenIs(token.Colon) {
			p.expectPeek(token.Colon)
			p.nextToken()
			value := p.parseExpression(lowest)
			dictEntries := make([]ast.DictEntry, 0, len(entries)+1)
			for _, buf := range entries {
				if buf.spread != nil {
					dictEntries = append(dictEntries, ast.DictEntry{Value: buf.spread, Spread: true})
				} else {
					dictEntries = append(dictEntries, ast.DictEntry{Key: buf.bare})
				}
			}
			dictEntries = append(dictEntries, ast.DictEntry{Key: expr, Value: value})
			return p.continueDictLiteral(openToken, dictEntries)
		}
		entries = append(entries, parseSpreadBuffer{bare: expr})
	}
	allSpreads := true
	for _, buf := range entries {
		if buf.spread == nil {
			allSpreads = false
			break
		}
	}
	p.expectPeek(token.RBrace)
	end := p.curToken
	if allSpreads {
		dictEntries := make([]ast.DictEntry, 0, len(entries))
		for _, buf := range entries {
			dictEntries = append(dictEntries, ast.DictEntry{Value: buf.spread, Spread: true})
		}
		return &ast.DictLiteral{Token: openToken, Entries: dictEntries, End: end}
	}
	elements := make([]ast.Expression, 0, len(entries))
	for _, buf := range entries {
		if buf.spread != nil {
			elements = append(elements, &ast.SpreadExpression{Token: openToken, Value: buf.spread})
		} else {
			elements = append(elements, buf.bare)
		}
	}
	return &ast.SetLiteral{Token: openToken, Elements: elements, End: end}
}

type parseSpreadBuffer struct {
	spread ast.Expression
	bare   ast.Expression
}

// continueSetLiteral consumes set literal entries after the first element.
// Accepts `...src` spreads in subsequent positions.
func (p *Parser) continueSetLiteral(openToken token.Token, first ast.Expression) ast.Expression {
	lit := &ast.SetLiteral{Token: openToken, Elements: []ast.Expression{first}}
	for p.peekTokenIs(token.Comma) {
		p.nextToken()
		if p.peekTokenIs(token.RBrace) {
			break
		}
		p.nextToken()
		if p.curTokenIs(token.Ellipsis) {
			spreadTok := p.curToken
			p.nextToken()
			lit.Elements = append(lit.Elements, &ast.SpreadExpression{Token: spreadTok, Value: p.parseExpression(lowest)})
			continue
		}
		lit.Elements = append(lit.Elements, p.parseExpression(lowest))
	}
	p.expectPeek(token.RBrace)
	lit.End = p.curToken
	return lit
}

// continueDictLiteral consumes dict literal entries after the first key:value
// pair has been parsed. Accepts `...src` spread entries between regular ones.
func (p *Parser) continueDictLiteral(openToken token.Token, head []ast.DictEntry) ast.Expression {
	lit := &ast.DictLiteral{Token: openToken, Entries: head}
	for p.peekTokenIs(token.Comma) {
		p.nextToken()
		if p.peekTokenIs(token.RBrace) {
			break
		}
		p.nextToken()
		if p.curTokenIs(token.Ellipsis) {
			p.nextToken()
			lit.Entries = append(lit.Entries, ast.DictEntry{Value: p.parseExpression(lowest), Spread: true})
			continue
		}
		key := p.parseExpression(lowest)
		p.expectPeek(token.Colon)
		p.nextToken()
		value := p.parseExpression(lowest)
		lit.Entries = append(lit.Entries, ast.DictEntry{Key: key, Value: value})
	}
	p.expectPeek(token.RBrace)
	lit.End = p.curToken
	return lit
}

// parseComprehensionTail parses a sequence of `for ... in ...` and `if ...`
// clauses inside a comprehension. cur token must be the last token of the
// body expression; on return it sits on the last token of the final clause
// so the caller can consume the closing bracket/brace with expectPeek.
func (p *Parser) parseComprehensionTail() []ast.ComprehensionClause {
	var clauses []ast.ComprehensionClause
	for p.peekTokenIs(token.For) || p.peekTokenIs(token.If) {
		if p.peekTokenIs(token.For) {
			p.nextToken()
			clauses = append(clauses, p.parseComprehensionFor())
		} else {
			p.nextToken()
			clauses = append(clauses, p.parseComprehensionIf())
		}
	}
	return clauses
}

// parseComprehensionFor parses a `for [type] name [, name]* in iterable`
// clause. cur token is `for` on entry; on return it sits on the last token
// of the iterable expression.
func (p *Parser) parseComprehensionFor() *ast.ComprehensionFor {
	clause := &ast.ComprehensionFor{Token: p.curToken}
	p.nextToken()
	if p.curTokenIs(token.Ident) && p.peekTokenIs(token.Comma) {
		clause.VarNames = []*ast.Identifier{{Token: p.curToken, Value: p.curToken.Literal}}
		for p.peekTokenIs(token.Comma) {
			p.nextToken()
			if !p.expectPeek(token.Ident) {
				return clause
			}
			clause.VarNames = append(clause.VarNames, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
		}
		if !p.expectPeek(token.In) {
			return clause
		}
	} else if p.curTokenIs(token.Ident) && p.peekTokenIs(token.In) {
		clause.VarName = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		p.expectPeek(token.In)
	} else {
		clause.VarType = p.parseTypeRefFromCurrent()
		if !p.expectPeek(token.Ident) {
			return clause
		}
		clause.VarName = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		if !p.expectPeek(token.In) {
			return clause
		}
	}
	p.nextToken()
	clause.Iterable = p.parseExpression(lowest)
	return clause
}

// parseComprehensionIf parses an `if cond` clause. cur token is `if`
// on entry; on return it sits on the last token of the condition.
func (p *Parser) parseComprehensionIf() *ast.ComprehensionIf {
	clause := &ast.ComprehensionIf{Token: p.curToken}
	p.nextToken()
	clause.Filter = p.parseExpression(lowest)
	return clause
}

func (p *Parser) parseFunctionLiteral(async bool) ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.curToken, Async: async}
	lit.Parameters = p.parseParameterList()
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		lit.ReturnType = p.parseTypeRefFromCurrent()
	}
	if !p.expectPeek(token.LBrace) {
		return lit
	}
	lit.Body = p.parseBlockStatement()
	return lit
}

func (p *Parser) parseMatchExpression() *ast.MatchExpression {
	expr := &ast.MatchExpression{Token: p.curToken}
	expr.Expr = p.parseParenthesizedExpression()
	if !p.expectPeek(token.LBrace) {
		return expr
	}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		c := ast.MatchCase{Token: p.curToken}
		switch p.curToken.Type {
		case token.Default:
			c.Default = true
		case token.Case:
			p.nextToken()
			if p.isEnumDestructuringPattern() {
				c.EnumVariant = p.parseEnumVariantPattern()
			} else if p.curTokenIs(token.LBracket) {
				c.ListPattern = p.parseMatchListPattern()
			} else if p.isTypePattern() {
				c.Type = p.parseTypeRefFromCurrent()
				if p.peekTokenIs(token.Ident) {
					p.nextToken()
					c.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
				}
			} else {
				c.Pattern = p.parseExpression(bitOr)
			}
			if p.peekTokenIs(token.BitOr) {
				if c.Name != nil {
					p.errorf(p.peekToken, "or-pattern alternates cannot introduce bindings")
				}
				if c.ListPattern != nil || c.EnumVariant != nil {
					p.errorf(p.peekToken, "or-pattern alternates are only allowed for literal, bare-type, and enum-no-payload patterns")
				}
				for p.peekTokenIs(token.BitOr) {
					p.nextToken()
					p.nextToken()
					if p.isTypePattern() {
						typeTok := p.curToken
						typeRef := p.parseTypeRefFromCurrent()
						if p.peekTokenIs(token.Ident) {
							p.errorf(p.peekToken, "or-pattern alternates cannot introduce bindings")
						}
						c.Alternates = append(c.Alternates, &ast.Identifier{Token: typeTok, Value: typeRef.Name})
					} else {
						c.Alternates = append(c.Alternates, p.parseExpression(bitOr))
					}
				}
			}
			if p.peekTokenIs(token.If) {
				p.nextToken()
				c.Guard = p.parseParenthesizedExpression()
			}
		default:
			p.errorf(p.curToken, "expected case or default, got %s", p.curToken.Type)
			p.synchronize()
			continue
		}
		if p.peekTokenIs(token.Arrow) {
			p.nextToken()
			p.nextToken()
			c.Value = p.parseExpression(lowest)
			p.expectPeek(token.Semicolon)
		} else {
			p.expectPeek(token.Colon)
			p.nextToken()
			if p.curTokenIs(token.LBrace) {
				c.Body = p.parseBlockStatement()
			} else {
				body := &ast.BlockStatement{Token: p.curToken}
				for !p.curTokenIs(token.Case) && !p.curTokenIs(token.Default) && !p.curTokenIs(token.RBrace) && !p.curTokenIs(token.EOF) {
					stmt := p.parseStatement()
					if stmt != nil {
						body.Statements = append(body.Statements, stmt)
					}
					p.nextToken()
				}
				c.Body = body
				p.rewindOne()
			}
		}
		expr.Cases = append(expr.Cases, c)
	}
	return expr
}

func (p *Parser) parseParenthesizedExpression() ast.Expression {
	if !p.expectPeek(token.LParen) {
		return nil
	}
	p.nextToken()
	expr := p.parseExpression(lowest)
	p.expectPeek(token.RParen)
	return expr
}

func (p *Parser) parsePath() []string {
	if !p.expectPeekIdentifierName() {
		return nil
	}
	path := []string{p.curToken.Literal}
	for p.peekTokenIs(token.Dot) {
		p.nextToken()
		if !p.expectPeekIdentifierName() {
			return path
		}
		path = append(path, p.curToken.Literal)
	}
	return path
}

// lookaheadToken returns the token n positions ahead of curToken (n=1 is peekToken).
func (p *Parser) lookaheadToken(n int) token.Token {
	idx := p.position - 2 + n
	if idx < 0 || idx >= len(p.tokens) {
		return token.Token{Type: token.EOF}
	}
	return p.tokens[idx]
}

// isEnumDestructuringPattern detects `Ident.Ident(` after the `case` keyword,
// which is the enum variant destructuring syntax: `case Result.Ok(string s):`.
func (p *Parser) isEnumDestructuringPattern() bool {
	return p.curTokenIs(token.Ident) &&
		p.lookaheadToken(1).Type == token.Dot &&
		p.lookaheadToken(2).Type == token.Ident &&
		p.lookaheadToken(3).Type == token.LParen
}

func (p *Parser) parseEnumStatement() ast.Statement {
	stmt := &ast.EnumStatement{Token: p.curToken}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		stmt.BackingType = p.parseTypeRefFromCurrent()
	}
	if p.peekTokenIs(token.Implements) {
		p.nextToken()
		stmt.Implements = p.parseTypeList()
	}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	// Variant list, optionally terminated by `;` to begin a method block.
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			return stmt
		}
		if p.curTokenIs(token.Semicolon) {
			break
		}
		if !p.curTokenIs(token.Ident) {
			p.errorf(p.curToken, "expected enum variant name, got %s", p.curToken.Type)
			p.synchronize()
			continue
		}
		variant := ast.EnumVariantDef{
			Name: &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
		}
		if p.peekTokenIs(token.LParen) {
			p.nextToken() // consume '('
			for !p.peekTokenIs(token.RParen) && !p.peekTokenIs(token.EOF) {
				p.nextToken()
				typeRef := p.parseTypeRefFromCurrent()
				variant.FieldTypes = append(variant.FieldTypes, typeRef)
				if !p.peekTokenIs(token.Comma) {
					break
				}
				p.nextToken() // consume ','
			}
			p.expectPeek(token.RParen)
		}
		if p.peekTokenIs(token.Assign) {
			p.nextToken()
			p.nextToken()
			variant.BackingValue = p.parseExpression(lowest)
		}
		if len(variant.FieldTypes) > 0 && variant.BackingValue != nil {
			p.errorf(variant.Name.Token, "enum variant %s cannot have both associated data and a backing value", variant.Name.Value)
		}
		stmt.Variants = append(stmt.Variants, variant)
		if p.peekTokenIs(token.Comma) {
			p.nextToken() // consume ','
		} else if p.peekTokenIs(token.Semicolon) {
			p.nextToken() // consume ';'
			if p.peekTokenIs(token.Func) {
				break
			}
		}
	}
	for {
		p.nextToken()
		if p.curTokenIs(token.RBrace) || p.curTokenIs(token.EOF) {
			break
		}
		if !p.curTokenIs(token.Func) {
			p.errorf(p.curToken, "expected enum method declaration, got %s", p.curToken.Type)
			p.synchronize()
			continue
		}
		member := p.parseStatement()
		if fn, ok := member.(*ast.FunctionStatement); ok {
			stmt.Methods = append(stmt.Methods, fn)
		} else if member != nil {
			p.errorf(p.curToken, "enum body allows only method declarations")
		}
	}
	return stmt
}

func (p *Parser) parseEnumVariantPattern() *ast.EnumVariantPattern {
	pattern := &ast.EnumVariantPattern{Token: p.curToken}
	pattern.Enum = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	p.nextToken() // consume enum name → curToken = '.'
	p.nextToken() // consume '.'       → curToken = variant name
	pattern.Variant = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.LParen) {
		return pattern
	}
	// parse (type name, type name, ...)
	for !p.peekTokenIs(token.RParen) && !p.peekTokenIs(token.EOF) {
		p.nextToken()
		paramType := p.parseTypeRefFromCurrent()
		if !p.expectPeek(token.Ident) {
			break
		}
		name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		pattern.Params = append(pattern.Params, ast.EnumPayloadParam{Type: paramType, Name: name})
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken() // consume ','
	}
	p.expectPeek(token.RParen)
	return pattern
}

// isConstraintComma reports whether peekToken ',' continues a constraint list
// (A, B → A & B) rather than starting a new type-param (U implements ...).
// Looks 3 tokens ahead of curToken: [,] [ident] [?implements].
func (p *Parser) isConstraintComma() bool {
	return p.lookaheadToken(3).Type != token.Implements
}

func (p *Parser) parseGenericNames() []*ast.TypeParam {
	if !p.peekTokenIs(token.LT) {
		return nil
	}
	p.nextToken() // consume <
	params := []*ast.TypeParam{}
	for {
		if !p.expectPeek(token.Ident) {
			return params
		}
		tp := &ast.TypeParam{Name: &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}}
		if p.peekTokenIs(token.Implements) {
			p.nextToken() // consume implements
			p.nextToken() // move to constraint type
			tp.Constraint = p.parseTypeRefFromCurrent()
			// Handle comma-separated constraints: <T implements A, B> → A & B
			for p.peekTokenIs(token.Comma) && p.isConstraintComma() {
				p.nextToken() // consume ','
				p.nextToken() // move to next constraint type name
				next := p.parseTypeRefFromCurrent()
				tp.Constraint = &ast.TypeRef{Operator: "&", Left: tp.Constraint, Right: next}
			}
		} else if p.peekTokenIs(token.Ident) || p.peekTokenIs(token.Question) {
			// Bare constraint form: <T string|int> means <T implements string|int>.
			p.nextToken()
			tp.Constraint = p.parseTypeRefFromCurrent()
		}
		params = append(params, tp)
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken() // consume comma
	}
	p.expectPeek(token.GT)
	return params
}

func (p *Parser) parseParameterList() []ast.Parameter {
	params := []ast.Parameter{}
	if !p.expectPeek(token.LParen) {
		return params
	}
	if p.peekTokenIs(token.RParen) {
		p.nextToken()
		return params
	}
	for {
		p.nextToken()
		decorators := p.parseParameterDecorators()
		isConst := false
		if p.curTokenIs(token.Const) {
			isConst = true
			p.nextToken()
		}
		paramType := p.parseTypeRefFromCurrent()
		variadic := false
		if p.peekTokenIs(token.Ellipsis) {
			p.nextToken()
			variadic = true
		}
		if !p.expectPeek(token.Ident) {
			return params
		}
		param := ast.Parameter{Type: paramType, Name: &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}, Variadic: variadic, Const: isConst, Decorators: decorators}
		if !variadic && p.peekTokenIs(token.Assign) {
			p.nextToken()
			p.nextToken()
			param.Default = p.parseExpression(lowest)
		}
		params = append(params, param)
		if variadic || !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken()
	}
	p.expectPeek(token.RParen)
	return params
}

func (p *Parser) parseParameterDecorators() []ast.Decorator {
	var decorators []ast.Decorator
	for p.curTokenIs(token.At) {
		decorator := ast.Decorator{Token: p.curToken}
		if !p.expectPeek(token.Ident) {
			return decorators
		}
		nameToken := p.curToken
		name := p.curToken.Literal
		for p.peekTokenIs(token.Dot) {
			p.nextToken()
			if !p.expectPeekIdentifierName() {
				return decorators
			}
			name = name + "." + p.curToken.Literal
		}
		decorator.Name = &ast.Identifier{Token: nameToken, Value: name}
		if p.peekTokenIs(token.LParen) {
			p.nextToken()
			decorator.Arguments = p.parseCallArguments()
		}
		decorators = append(decorators, decorator)
		p.nextToken()
	}
	return decorators
}

func (p *Parser) parseFunctionSignature() *ast.FunctionSignature {
	sig := &ast.FunctionSignature{Token: p.curToken, Doc: p.curToken.Doc}
	if !p.expectPeekIdentifierName() {
		return sig
	}
	sig.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	sig.Generics = p.parseGenericNames()
	sig.Parameters = p.parseParameterList()
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		sig.ReturnType = p.parseTypeRefFromCurrent()
	}
	p.expectPeek(token.Semicolon)
	return sig
}

func (p *Parser) parseTypeList() []*ast.TypeRef {
	types := []*ast.TypeRef{}
	for {
		p.nextToken()
		types = append(types, p.parseTypeRefFromCurrent())
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken()
	}
	return types
}

func (p *Parser) parseTypeRefFromCurrent() *ast.TypeRef {
	nullable := false
	if p.curTokenIs(token.Question) {
		nullable = true
		p.nextToken()
	}
	if !isTypeStartToken(p.curToken.Type) {
		p.errorf(p.curToken, "expected type, got %s", p.curToken.Type)
		return &ast.TypeRef{Token: p.curToken, Name: p.curToken.Literal, Nullable: nullable}
	}
	left := &ast.TypeRef{Token: p.curToken, Name: p.curToken.Literal, Nullable: nullable}
	for p.peekTokenIs(token.Dot) {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return left
		}
		left.Name += "." + p.curToken.Literal
	}
	if p.peekTokenIs(token.LT) && !isNonGenericTypeName(left.Name) {
		left.Arguments = p.parseTypeArguments()
	}
	if p.peekTokenIs(token.LBracket) {
		p.nextToken()
		if p.expectPeek(token.RBracket) {
			left.ListAlias = true
		}
	}
	for p.peekTokenIs(token.BitOr) || p.peekTokenIs(token.BitAnd) {
		p.nextToken()
		op := p.curToken.Literal
		p.nextToken()
		right := p.parseTypeRefFromCurrent()
		left = &ast.TypeRef{Token: left.Token, Left: left, Operator: op, Right: right}
	}
	return left
}

// isNonGenericTypeName reports whether name is a primitive that never takes type arguments, so a following `<` is a comparison.
func isNonGenericTypeName(name string) bool {
	switch name {
	case "int", "string", "bool", "float", "decimal", "bytes", "void", "any", "null":
		return true
	}
	return false
}

func (p *Parser) parseTypeArguments() []*ast.TypeRef {
	p.nextToken()
	return p.parseTypeArgumentsFromCurrent()
}

// parseTypeArgumentsFromCurrent parses a generic argument list when p.curToken
// already sits on the opening `<`. Used by the generic-call disambiguation in
// parseInfix where the surrounding parseExpression loop has already advanced
// onto the `<` token.
func (p *Parser) parseTypeArgumentsFromCurrent() []*ast.TypeRef {
	args := []*ast.TypeRef{}
	for {
		p.nextToken()
		args = append(args, p.parseTypeRefFromCurrent())
		if !p.peekTokenIs(token.Comma) {
			break
		}
		p.nextToken()
	}
	p.expectPeekGTInGeneric()
	return args
}

// expectPeekGTInGeneric consumes the > that closes a generic argument list.
// If the next token is >> (RShift), it splits the token into two GT tokens so
// the second > remains available for the enclosing generic argument list.
// This handles types like list<dict<string,int>> where the lexer produces a
// single >> token for the two adjacent closing angle brackets.
func (p *Parser) expectPeekGTInGeneric() bool {
	if p.peekTokenIs(token.GT) {
		p.nextToken()
		return true
	}
	if p.peekTokenIs(token.RShift) {
		// The >> was lexed as a single token. Replace it with two GT tokens
		// in the pre-lexed slice so the second > closes the outer generic list.
		idx := p.position - 1
		line, col := p.peekToken.Line, p.peekToken.Column
		gt1 := token.Token{Type: token.GT, Literal: ">", Raw: ">", Line: line, Column: col}
		gt2 := token.Token{Type: token.GT, Literal: ">", Raw: ">", Line: line, Column: col + 1}
		p.tokens[idx] = gt1
		p.tokens = append(p.tokens, token.Token{})
		copy(p.tokens[idx+2:], p.tokens[idx+1:])
		p.tokens[idx+1] = gt2
		p.peekToken = gt1
		p.nextToken()
		return true
	}
	p.errorf(p.peekToken, "expected next token to be %s, got %s", token.GT, p.peekToken.Type)
	return false
}

func (p *Parser) looksLikeTypedDeclaration() bool {
	start := p.position - 2
	if start < 0 || start >= len(p.tokens) {
		return false
	}
	i := start
	if p.tokens[i].Type == token.Question {
		i++
	}
	if i >= len(p.tokens) || !isTypeStartToken(p.tokens[i].Type) {
		return false
	}
	i++
	for i+1 < len(p.tokens) && p.tokens[i].Type == token.Dot && p.tokens[i+1].Type == token.Ident {
		i += 2
	}
	if i < len(p.tokens) && p.tokens[i].Type == token.LT {
		depth := 1
		i++
		for i < len(p.tokens) && depth > 0 {
			switch p.tokens[i].Type {
			case token.LT:
				depth++
			case token.GT:
				depth--
			case token.RShift:
				// `>>` is the lexed form of two adjacent `>` characters; it
				// closes two generic levels at once. parseTypeArguments splits
				// it into two GT tokens at parse time, but this look-ahead
				// runs before parsing, so we account for both closings here.
				depth -= 2
			}
			i++
		}
	}
	if i+1 < len(p.tokens) && p.tokens[i].Type == token.LBracket && p.tokens[i+1].Type == token.RBracket {
		i += 2
	}
	return i < len(p.tokens) && p.tokens[i].Type == token.Ident
}

func (p *Parser) isForInClause() bool {
	savedCur, savedPeek := p.curToken, p.peekToken
	savedPosition := p.position
	for !p.curTokenIs(token.RParen) && !p.curTokenIs(token.Semicolon) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.In) || p.peekTokenIs(token.In) {
			p.curToken, p.peekToken = savedCur, savedPeek
			p.position = savedPosition
			return true
		}
		p.nextToken()
	}
	p.curToken, p.peekToken = savedCur, savedPeek
	p.position = savedPosition
	return false
}

func (p *Parser) isTypePattern() bool {
	return p.curTokenIs(token.Question) || p.curTokenIs(token.Ident) && p.peekTokenIs(token.Ident) || isTypeStartToken(p.curToken.Type)
}

// parseMatchListPattern parses `[ binding (',' binding)* ]` where each
// binding is `type? name`. cur token is the opening `[`.
//
//	case [int x, int y]           => ...
//	case [string label, _]        => ...    // underscore = wildcard
//	case [user.User u]            => ...    // single-element with type
//	case []                       => ...    // empty-list literal pattern
func (p *Parser) parseMatchListPattern() *ast.ListPatternMatch {
	out := &ast.ListPatternMatch{Token: p.curToken}
	if p.peekTokenIs(token.RBracket) {
		p.nextToken() // consume ']'
		return out
	}
	for {
		p.nextToken() // step onto first token of next binding
		binding := ast.ListPatternBinding{}
		if isTypeStartToken(p.curToken.Type) && p.peekTokenIs(token.Ident) {
			binding.Type = p.parseTypeRefFromCurrent()
			p.nextToken() // step onto the name
		}
		if binding.Type == nil && !p.curTokenIs(token.Ident) {
			// Non-identifier element: a literal matched by equality.
			binding.Literal = p.parseExpression(bitOr)
			if binding.Literal == nil {
				p.errorf(p.curToken, "expected list-pattern binding name or literal, got %s", p.curToken.Type)
				return out
			}
			out.Bindings = append(out.Bindings, binding)
			if p.peekTokenIs(token.Comma) {
				p.nextToken()
				continue
			}
			if !p.expectPeek(token.RBracket) {
				return out
			}
			return out
		}
		if !p.curTokenIs(token.Ident) {
			p.errorf(p.curToken, "expected list-pattern binding name, got %s", p.curToken.Type)
			return out
		}
		binding.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		out.Bindings = append(out.Bindings, binding)
		if p.peekTokenIs(token.Comma) {
			p.nextToken()
			continue
		}
		if !p.expectPeek(token.RBracket) {
			return out
		}
		return out
	}
}

func isTypeStartToken(t token.Type) bool {
	return t == token.Ident || t == token.Bool || t == token.Func || t == token.TypeKw
}

func isIdentifierNameToken(t token.Type) bool {
	switch t {
	case token.Ident,
		token.As,
		token.Async,
		token.Await,
		token.Bool,
		token.Break,
		token.By,
		token.Case,
		token.Catch,
		token.Class,
		token.Const,
		token.Continue,
		token.Default,
		token.Defer,
		token.Else,
		token.ElseIf,
		token.Extends,
		token.Export,
		token.False,
		token.Finally,
		token.For,
		token.Func,
		token.If,
		token.Implements,
		token.Import,
		token.In,
		token.Init,
		token.InstanceOf,
		token.Interface,
		token.Is,
		token.Let,
		token.Match,
		token.Module,
		token.Not,
		token.Null,
		token.Parent,
		token.Return,
		token.Static,
		token.This,
		token.Throw,
		token.TypeKw,
		token.True,
		token.Try,
		token.While,
		token.With,
		token.Del,
		token.Xor,
		token.Yield:
		return true
	default:
		return false
	}
}

// interpolationLiteral builds a literal segment of an interpolated string,
// decoding escapes (\n, \u{...}, ...) the same way a plain double-quoted
// string does. Reports an invalid \u{...} as a parse error.
func (p *Parser) interpolationLiteral(tok token.Token, seg string) *ast.StringLiteral {
	value, err := lexer.UnescapeDoubleQuoted(seg)
	if err != nil {
		p.errorf(tok, "%s", err.Error())
		value = seg
	}
	return &ast.StringLiteral{Token: tok, Value: value, Raw: seg, Quote: tok.Quote}
}

func (p *Parser) parseInterpolatedString(tok token.Token) *ast.InterpolatedString {
	node := &ast.InterpolatedString{Token: tok}
	raw := tok.Raw

	for len(raw) > 0 {
		start := strings.Index(raw, "${")
		if start == -1 {
			node.Parts = append(node.Parts, p.interpolationLiteral(tok, raw))
			break
		}
		if start > 0 {
			node.Parts = append(node.Parts, p.interpolationLiteral(tok, raw[:start]))
		}
		// find the matching '}', skipping string literals so their braces don't affect depth
		depth, i := 1, start+2
		for i < len(raw) && depth > 0 {
			ch := raw[i]
			if ch == '\'' || ch == '"' {
				q := ch
				i++
				for i < len(raw) {
					if raw[i] == '\\' {
						i += 2
						continue
					}
					if raw[i] == q {
						i++
						break
					}
					i++
				}
			} else if ch == '{' {
				depth++
				i++
			} else if ch == '}' {
				depth--
				if depth > 0 {
					i++
				}
			} else {
				i++
			}
		}
		exprSrc := raw[start+2 : i]
		raw = raw[i+1:]

		// Locate the format-spec separator `:` that sits at depth 0
		// and isn't being consumed by an enclosing ternary `? ... :`.
		exprText, spec, hasSpec := splitInterpolationSpec(exprSrc)

		subLexer := lexer.New(exprText)
		subParser := New(subLexer)
		expr := subParser.parseExpression(lowest)
		if hasSpec {
			node.Parts = append(node.Parts, &ast.FormattedInterpolation{
				Token: tok,
				Value: expr,
				Spec:  spec,
			})
		} else {
			node.Parts = append(node.Parts, expr)
		}
	}
	return node
}

// splitInterpolationSpec finds the format-spec separator `:` inside a
// `${...}` body. It must be at depth 0 (parens/brackets/braces) and not
// consumed by an unmatched ternary `?`. Returns (expr, spec, true) when
// a separator is found, else (whole, "", false).
func splitInterpolationSpec(src string) (string, string, bool) {
	depth := 0
	ternary := 0
	inStr := byte(0)
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if inStr != 0 {
			if ch == '\\' && i+1 < len(src) {
				i++
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			inStr = ch
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '?':
			if depth == 0 {
				ternary++
			}
		case ':':
			if depth == 0 {
				if ternary > 0 {
					ternary--
					continue
				}
				return src[:i], src[i+1:], true
			}
		}
	}
	return src, "", false
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	if p.position >= len(p.tokens) {
		p.peekToken = token.Token{Type: token.EOF}
	} else {
		p.peekToken = p.tokens[p.position]
		p.position++
	}
	if p.peekToken.Type == token.Illegal {
		p.errors = append(p.errors, p.errorAt(p.peekToken, "%s", p.peekToken.Literal))
	}
}

func (p *Parser) rewindOne() {
	if p.position > 0 {
		p.position--
		p.peekToken = p.curToken
	}
}

func (p *Parser) expectPeek(t token.Type) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.errorf(p.peekToken, "expected next token to be %s, got %s", t, p.peekToken.Type)
	return false
}

func (p *Parser) expectPeekIdentifierName() bool {
	if isIdentifierNameToken(p.peekToken.Type) {
		p.nextToken()
		return true
	}
	p.errorf(p.peekToken, "expected next token to be identifier name, got %s", p.peekToken.Type)
	return false
}

func (p *Parser) expectCurrent(t token.Type) bool {
	if p.curTokenIs(t) {
		return true
	}
	p.errorf(p.curToken, "expected token to be %s, got %s", t, p.curToken.Type)
	return false
}

func (p *Parser) curTokenIs(t token.Type) bool  { return p.curToken.Type == t }
func (p *Parser) peekTokenIs(t token.Type) bool { return p.peekToken.Type == t }
func (p *Parser) peekNextTokenIs(t token.Type) bool {
	if p.position >= len(p.tokens) {
		return t == token.EOF
	}
	return p.tokens[p.position].Type == t
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return lowest
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return lowest
}

func (p *Parser) synchronize() {
	for !p.curTokenIs(token.Semicolon) && !p.curTokenIs(token.RBrace) && !p.curTokenIs(token.EOF) {
		p.nextToken()
	}
}

func (p *Parser) errorf(tok token.Token, format string, args ...any) {
	p.errors = append(p.errors, p.errorAt(tok, format, args...))
}

func (p *Parser) errorAt(tok token.Token, format string, args ...any) string {
	return fmt.Sprintf("%d:%d: %s", tok.Line, tok.Column, fmt.Sprintf(format, args...))
}
