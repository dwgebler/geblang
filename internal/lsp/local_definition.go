package lsp

import (
	"reflect"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/token"
)

// span is an inclusive source region in 0-based LSP coordinates.
type span struct {
	startLine, startCol int
	endLine, endCol     int
}

func (s span) contains(line, col int) bool {
	if line < s.startLine || (line == s.startLine && col < s.startCol) {
		return false
	}
	return line < s.endLine || (line == s.endLine && col <= s.endCol)
}

func (s span) startsAfter(o span) bool {
	return s.startLine > o.startLine || (s.startLine == o.startLine && s.startCol > o.startCol)
}

type localBinding struct {
	name      string
	line, col int
	scope     span
	// hoisted bindings are visible before their own position (comprehension variables).
	hoisted bool
	member  bool
}

type bindingContext struct {
	scope   span
	inClass bool
}

type bindingCollector struct {
	bindings []localBinding
}

var tokenType = reflect.TypeOf(token.Token{})

func localDefinition(source string, line, char int, word, qualifier string) (Range, bool) {
	if qualifier != "" && qualifier != "this" {
		return Range{}, false
	}
	prog := parser.New(lexer.New(source)).ParseProgram()
	c := &bindingCollector{}
	file := span{endLine: int(^uint(0) >> 1)}
	c.walk(reflect.ValueOf(prog), bindingContext{scope: file})

	var best *localBinding
	for i := range c.bindings {
		b := &c.bindings[i]
		if b.name != word || b.member != (qualifier == "this") || !b.scope.contains(line, char) {
			continue
		}
		before := b.line < line || (b.line == line && b.col <= char)
		if !b.hoisted && !b.member && !before {
			continue
		}
		if best == nil || b.scope.startsAfter(best.scope) || (!best.scope.startsAfter(b.scope) && before) {
			best = b
		}
	}
	if best == nil {
		return Range{}, false
	}
	return Range{
		Start: Position{Line: best.line, Character: best.col},
		End:   Position{Line: best.line, Character: best.col + len(best.name)},
	}, true
}

func (c *bindingCollector) bind(id *ast.Identifier, scope span, hoisted, member bool) {
	if id == nil || id.Value == "" || id.Value == "_" || id.Token.Line == 0 {
		return
	}
	c.bindings = append(c.bindings, localBinding{
		name:    id.Value,
		line:    id.Token.Line - 1,
		col:     id.Token.Column - 1,
		scope:   scope,
		hoisted: hoisted,
		member:  member,
	})
}

func (c *bindingCollector) bindParameters(params []ast.Parameter, scope span) {
	for _, p := range params {
		c.bind(p.Name, scope, false, false)
	}
}

func (c *bindingCollector) walk(v reflect.Value, ctx bindingContext) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
		if v.Kind() == reflect.Pointer && v.Elem().Kind() == reflect.Struct && v.CanInterface() {
			ctx = c.enter(v.Interface(), ctx)
		}
		c.walk(v.Elem(), ctx)
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			c.walk(v.Index(i), ctx)
		}
	case reflect.Struct:
		if v.Type() == tokenType {
			return
		}
		if v.CanInterface() {
			ctx = c.enter(v.Interface(), ctx)
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				c.walk(v.Field(i), ctx)
			}
		}
	}
}

// enter records the bindings a node introduces and returns the context for its children.
func (c *bindingCollector) enter(node any, ctx bindingContext) bindingContext {
	switch n := node.(type) {
	case *ast.BlockStatement:
		ctx.scope = nodeSpan(n)
	case *ast.ClassStatement:
		ctx.scope = nodeSpan(n)
		ctx.inClass = true
	case *ast.DeclarationStatement:
		c.bind(n.Name, ctx.scope, false, ctx.inClass)
	case *ast.DestructuringStatement:
		for _, name := range n.Names {
			c.bind(name, ctx.scope, false, false)
		}
	case *ast.FunctionStatement:
		if ctx.inClass {
			c.bind(n.Name, ctx.scope, false, true)
		}
		ctx = bindingContext{scope: nodeSpan(n)}
		c.bindParameters(n.Parameters, ctx.scope)
	case *ast.FunctionLiteral:
		ctx = bindingContext{scope: nodeSpan(n)}
		c.bindParameters(n.Parameters, ctx.scope)
	case *ast.ForStatement:
		ctx.scope = nodeSpan(n)
		c.bind(n.VarName, ctx.scope, false, false)
		for _, name := range n.VarNames {
			c.bind(name, ctx.scope, false, false)
		}
	case *ast.WithStatement:
		ctx.scope = nodeSpan(n)
		c.bind(n.Name, ctx.scope, false, false)
	case ast.CatchClause:
		if n.Body != nil {
			scope := nodeSpan(n.Body)
			if n.Name != nil && n.Name.Token.Line > 0 {
				scope.startLine, scope.startCol = n.Name.Token.Line-1, n.Name.Token.Column-1
			}
			c.bind(n.Name, scope, false, false)
		}
	case ast.MatchCase:
		ctx.scope = nodeSpan(n)
		c.bind(n.Name, ctx.scope, false, false)
		if n.EnumVariant != nil {
			for _, p := range n.EnumVariant.Params {
				c.bind(p.Name, ctx.scope, false, false)
			}
		}
		if n.ListPattern != nil {
			for _, b := range n.ListPattern.Bindings {
				c.bind(b.Name, ctx.scope, false, false)
			}
		}
	case *ast.ListComprehension:
		ctx.scope = nodeSpan(n)
	case *ast.SetComprehension:
		ctx.scope = nodeSpan(n)
	case *ast.DictComprehension:
		ctx.scope = nodeSpan(n)
	case *ast.ComprehensionFor:
		c.bind(n.VarName, ctx.scope, true, false)
		for _, name := range n.VarNames {
			c.bind(name, ctx.scope, true, false)
		}
	}
	return ctx
}

// nodeSpan covers every token reachable from node.
func nodeSpan(node any) span {
	s := span{startLine: -1}
	growSpan(reflect.ValueOf(node), &s)
	if s.startLine < 0 {
		return span{}
	}
	return s
}

func growSpan(v reflect.Value, s *span) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			growSpan(v.Elem(), s)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			growSpan(v.Index(i), s)
		}
	case reflect.Struct:
		if v.Type() == tokenType {
			tok := v.Interface().(token.Token)
			if tok.Line == 0 {
				return
			}
			line, col := tok.Line-1, tok.Column-1
			end := col + len(tok.Literal)
			if s.startLine < 0 || line < s.startLine || (line == s.startLine && col < s.startCol) {
				s.startLine, s.startCol = line, col
			}
			if line > s.endLine || (line == s.endLine && end > s.endCol) {
				s.endLine, s.endCol = line, end
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				growSpan(v.Field(i), s)
			}
		}
	}
}
