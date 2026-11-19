package semantic

import (
	"strings"

	"geblang/internal/ast"
)

// ExprType is the exported best-effort type of a single expression node.
// Name uses Geblang type names ("int", "list", class names); Args carries
// generic arguments (list<int> -> Args = [int]).
type ExprType struct {
	Name     string
	Nullable bool
	Known    bool
	Args     []ExprType
}

func exprTypeFromInfo(t typeInfo) ExprType {
	out := ExprType{Name: t.name, Nullable: t.nullable, Known: t.known}
	if len(t.args) > 0 {
		out.Args = make([]ExprType, 0, len(t.args))
		for _, arg := range t.args {
			out.Args = append(out.Args, exprTypeFromInfo(arg))
		}
	}
	return out
}

// ResolveExpressionTypes runs a recording analysis over program and returns
// best-effort types keyed by expression node pointer. The map reflects what
// the analyzer could prove; absent or Known=false entries mean "unknown".
// Diagnostics from this run are discarded - callers wanting diagnostics run
// Analyze separately.
func ResolveExpressionTypes(program *ast.Program) map[ast.Expression]ExprType {
	return ResolveExpressionTypesWith(program, nil)
}

// ResolveExpressionTypesWith is ResolveExpressionTypes with names already bound outside program, such as earlier REPL lines.
func ResolveExpressionTypesWith(program *ast.Program, bound map[string]*ast.TypeRef) map[ast.Expression]ExprType {
	a := New()
	for name, ref := range bound {
		a.declare(name, a.typeInfoFromRef(ref))
	}
	a.exprTypes = map[ast.Expression]ExprType{}
	// A within-program field resolver types `this.f` / `obj.f` accesses
	// for classes declared in the program. Direct fields only; inherited
	// and cross-module fields fall back to unknown (the transpiler then
	// uses its own local certainty).
	if fields := programFieldTypes(program); len(fields) > 0 {
		a.classFieldType = func(className, fieldLower string) (*ast.TypeRef, bool) {
			if byField, ok := fields[className]; ok {
				if ref, ok := byField[fieldLower]; ok {
					return ref, true
				}
			}
			return nil, false
		}
	}
	a.Analyze(program)
	return a.exprTypes
}

// programFieldTypes maps className -> lowercased field name -> declared
// type, walking class declarations in the program (including exports).
func programFieldTypes(program *ast.Program) map[string]map[string]*ast.TypeRef {
	out := map[string]map[string]*ast.TypeRef{}
	var visit func(stmt ast.Statement)
	visit = func(stmt ast.Statement) {
		if exp, ok := stmt.(*ast.ExportStatement); ok {
			visit(exp.Statement)
			return
		}
		cls, ok := stmt.(*ast.ClassStatement)
		if !ok || cls.Name == nil {
			return
		}
		byField := map[string]*ast.TypeRef{}
		for _, member := range cls.Members {
			if decl, ok := member.(*ast.DeclarationStatement); ok && decl.Name != nil && decl.Type != nil {
				byField[strings.ToLower(decl.Name.Value)] = decl.Type
			}
		}
		if len(byField) > 0 {
			out[cls.Name.Value] = byField
		}
	}
	for _, stmt := range program.Statements {
		visit(stmt)
	}
	return out
}

func (a *Analyzer) recordExprType(expr ast.Expression) {
	if expr == nil {
		return
	}
	a.exprTypes[expr] = exprTypeFromInfo(a.expressionTypeName(expr))
}

// recordSkippedExpression visits child expressions the normal pass never
// descends into, so their nodes get recorded too. Recording mode only.
func (a *Analyzer) recordSkippedExpression(expr ast.Expression) {
	switch e := expr.(type) {
	case *ast.TernaryExpression:
		a.analyzeExpression(e.Condition)
		a.analyzeExpression(e.ThenExpr)
		a.analyzeExpression(e.ElseExpr)
	case *ast.CastExpression:
		a.analyzeExpression(e.Value)
	case *ast.AwaitExpression:
		a.analyzeExpression(e.Value)
	case *ast.SpreadExpression:
		a.analyzeExpression(e.Value)
	case *ast.FormattedInterpolation:
		a.analyzeExpression(e.Value)
	case *ast.RangeExpression:
		a.analyzeExpression(e.Start)
		a.analyzeExpression(e.End)
		if e.Step != nil {
			a.analyzeExpression(e.Step)
		}
	case *ast.PipeExpression:
		a.analyzeExpression(e.Left)
		a.analyzeExpression(e.Right)
	case *ast.PartialExpression:
		a.analyzeExpression(e.Callee)
		for _, arg := range e.Arguments {
			if !arg.Hole {
				a.analyzeExpression(arg.Value)
			}
		}
	case *ast.InterpolatedString:
		for _, part := range e.Parts {
			a.analyzeExpression(part)
		}
	case *ast.MatchExpression:
		a.analyzeExpression(e.Expr)
		a.recordMatchCases(e.Cases, nil)
	case *ast.FunctionLiteral:
		a.pushScope()
		for _, param := range e.Parameters {
			if param.Name != nil {
				a.declare(param.Name.Value, a.parameterBindingType(param))
			}
		}
		a.analyzeBlock(e.Body, nil)
		a.popScope()
	}
}

// recordMatchCases analyzes match cases with their pattern bindings in
// scope so binder uses inside guards/values/bodies get typed. Recording
// mode only; the normal pass analyzes only case bodies, without bindings.
func (a *Analyzer) recordMatchCases(cases []ast.MatchCase, fn *ast.FunctionStatement) {
	for i := range cases {
		c := &cases[i]
		a.pushScope()
		if c.EnumVariant != nil {
			for _, p := range c.EnumVariant.Params {
				if p.Name == nil {
					continue
				}
				a.declare(p.Name.Value, a.payloadBindingType(p.Type))
			}
		}
		if c.Name != nil {
			a.declare(c.Name.Value, a.typeInfoFromRef(c.Type))
		}
		if c.ListPattern != nil {
			for _, b := range c.ListPattern.Bindings {
				if b.Name != nil && b.Name.Value != "_" {
					a.declare(b.Name.Value, a.typeInfoFromRef(b.Type))
				}
			}
		}
		if c.Pattern != nil {
			a.analyzeExpression(c.Pattern)
		}
		if c.Guard != nil {
			a.analyzeExpression(c.Guard)
		}
		if c.Value != nil {
			a.analyzeExpression(c.Value)
		}
		a.analyzeBlock(c.Body, fn)
		a.popScope()
	}
}

// payloadBindingType treats an untyped variant payload binder as `any`,
// mirroring the runtime's untyped binding semantics.
func (a *Analyzer) payloadBindingType(ref *ast.TypeRef) typeInfo {
	if ref == nil {
		return typeInfo{name: "any", known: true}
	}
	return a.typeInfoFromRef(ref)
}

// recordForBody handles both for-loop forms in recording mode, in a
// scope so the loop variable is typed for the condition and body: the
// C-style init (which declares the counter), the for-in iterable with
// its bound loop variables, then condition/update/body.
func (a *Analyzer) recordForBody(stmt *ast.ForStatement, fn *ast.FunctionStatement) {
	a.pushScope()
	defer a.popScope()
	if stmt.Init != nil {
		a.analyzeStatement(stmt.Init, fn)
	}
	if stmt.Iterable != nil {
		a.analyzeExpression(stmt.Iterable)
		if stmt.Step != nil {
			a.analyzeExpression(stmt.Step)
		}
		names := stmt.VarNames
		if stmt.VarName != nil {
			names = []*ast.Identifier{stmt.VarName}
		}
		binds := a.forInBindingTypes(stmt.Iterable, len(names))
		for i, name := range names {
			if name == nil {
				continue
			}
			var t typeInfo
			if stmt.VarType != nil {
				t = a.typeInfoFromRef(stmt.VarType)
			} else if i < len(binds) {
				t = binds[i]
			}
			a.declare(name.Value, t)
		}
	}
	if stmt.Condition != nil {
		a.analyzeExpression(stmt.Condition)
	}
	if stmt.Update != nil {
		a.analyzeStatement(stmt.Update, fn)
	}
	a.analyzeBlock(stmt.Body, fn)
}

// forInBindingTypes infers the loop-variable types for `for (v in iter)`.
func (a *Analyzer) forInBindingTypes(iter ast.Expression, n int) []typeInfo {
	if _, ok := iter.(*ast.RangeExpression); ok {
		return []typeInfo{{name: "int", known: true}}
	}
	t := a.expressionTypeName(iter)
	if !t.known {
		if recv, ok := dictItemsCallReceiver(iter); ok {
			rt := a.expressionTypeName(recv)
			if rt.known && rt.name == "dict" && len(rt.args) == 2 {
				return rt.args
			}
		}
		return nil
	}
	switch t.name {
	case "list", "set", "generator", "iterable":
		if len(t.args) == 1 {
			return []typeInfo{t.args[0]}
		}
	case "string":
		return []typeInfo{{name: "string", known: true}}
	case "dict":
		if len(t.args) == 2 {
			if n >= 2 {
				return t.args
			}
			return []typeInfo{t.args[0]}
		}
	}
	return nil
}

func dictItemsCallReceiver(iter ast.Expression) (ast.Expression, bool) {
	call, ok := iter.(*ast.CallExpression)
	if !ok || len(call.Arguments) != 0 {
		return nil, false
	}
	sel, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || sel.Name == nil || sel.Name.Value != "items" {
		return nil, false
	}
	return sel.Object, true
}
