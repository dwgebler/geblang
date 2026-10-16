package lower

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
	"strconv"
	"strings"
)

func (l *Lowerer) lowerYield(s *ast.YieldStatement) {
	if !l.inGenerator {
		l.errAt(s.Token.Line, s.Token.Column,
			"yield outside of a generator function",
			"yield is only valid inside a function declared with return type generator<T>")
		return
	}
	l.w.WriteString("if !yield(")
	if s.Value != nil {
		l.lowerExpression(s.Value)
	}
	l.w.WriteLine(") { return }")
}

func (l *Lowerer) lowerWhile(s *ast.WhileStatement) {
	l.w.WriteString("for ")
	l.lowerExpression(s.Condition)
	l.w.WriteLine(" {")
	l.w.Indent()
	l.withChildScope(func() {
		l.withLoopBody(func() { l.lowerBlock(s.Body.Statements) })
	})
	l.w.Dedent()
	l.w.WriteLine("}")
}

func (l *Lowerer) lowerReturn(s *ast.ReturnStatement) {
	if l.tryCtl != nil && !l.tryCtl.retSuspended {
		if l.tryCtl.retGo == "" {
			l.w.WriteLine("return 1")
			return
		}
		l.w.WriteString("return 1, ")
		if s.Value != nil {
			l.lowerValueAs(l.currentReturnType, s.Value)
		} else {
			l.w.WriteString(tryZeroExpr(l.tryCtl.retGo))
		}
		l.w.WriteLine("")
		return
	}
	if s.Value == nil {
		l.w.WriteLine("return")
		return
	}
	l.w.WriteString("return ")
	if rt := l.currentReturnType; rt != nil && rt.Nullable {
		l.lowerIntoNullable(rt, s.Value)
		l.w.WriteLine("")
		return
	}
	l.lowerValueAs(l.currentReturnType, s.Value)
	l.w.WriteLine("")
}

// requireUncaughtHandler registers the top-level renderer + its imports so an
// uncaught error prints in the canonical format from main.
func (l *Lowerer) requireUncaughtHandler() {
	l.Module.RequireHelper("gbUncaught")
	l.Module.AddImport(types.OrderedDictImport)
	l.Module.AddImport("fmt")
	l.Module.AddImport("os")
}

// lowerTry emits try/catch/finally as an inline block (no wrapping IIFE) so a
// return/break/continue keeps its enclosing-function/loop meaning. The
// recover-able try and each catch/finally body run in a signal closure
// returning (controlCode, value); the code is stored in __sigN and replayed by
// a real Go transfer after the block. finally always runs; an unmatched throw
// re-panics after it; a finally transfer overrides any pending one.
func (l *Lowerer) lowerTry(s *ast.TryStatement) {
	l.requireUncaughtHandler()
	l.tryDepth++
	n := l.tryDepth
	retGo := l.currentReturnGo
	if l.tryCtl != nil {
		retGo = l.tryCtl.retGo
	}
	sig := fmt.Sprintf("__sig%d", n)
	ret := fmt.Sprintf("__ret%d", n)
	exc := fmt.Sprintf("__exc%d", n)
	unhandled := fmt.Sprintf("__unh%d", n)

	l.w.WriteLine("{")
	l.w.Indent()
	l.w.WriteString("var " + exc + " any")
	l.w.WriteLine("")
	l.w.WriteString(unhandled + " := false")
	l.w.WriteLine("")
	l.w.WriteString("var " + sig + " int")
	l.w.WriteLine("")
	if retGo != "" {
		l.w.WriteString("var " + ret + " " + retGo)
		l.w.WriteLine("")
	}

	l.emitSignalBody(sig, ret, retGo, func() {
		l.w.WriteString("defer func() { " + exc + " = recover() }()")
		l.w.WriteLine("")
		l.withChildScope(func() { l.lowerBlock(s.Body.Statements) })
	})

	l.w.WriteString("if " + exc + " != nil {")
	l.w.WriteLine("")
	l.w.Indent()
	l.emitCatchDispatch(s.Catches, sig, ret, exc, unhandled, retGo)
	l.w.Dedent()
	l.w.WriteLine("}")

	if s.Finally != nil {
		fsig := fmt.Sprintf("__fsig%d", n)
		fret := fmt.Sprintf("__fret%d", n)
		l.w.WriteString("var " + fsig + " int")
		l.w.WriteLine("")
		if retGo != "" {
			l.w.WriteString("var " + fret + " " + retGo)
			l.w.WriteLine("")
		}
		l.emitSignalBody(fsig, fret, retGo, func() {
			l.withChildScope(func() { l.lowerBlock(s.Finally.Statements) })
		})
		l.w.WriteString("if " + fsig + " != 0 {")
		l.w.WriteLine("")
		l.w.Indent()
		if retGo == "" {
			l.w.WriteString(sig + " = " + fsig)
		} else {
			l.w.WriteString(sig + ", " + ret + " = " + fsig + ", " + fret)
		}
		l.w.WriteLine("")
		l.w.WriteString(unhandled + " = false")
		l.w.WriteLine("")
		l.w.Dedent()
		l.w.WriteLine("}")
	}

	l.w.WriteString("if " + unhandled + " {")
	l.w.WriteLine("")
	l.w.Indent()
	l.w.WriteString("panic(" + exc + ")")
	l.w.WriteLine("")
	l.w.Dedent()
	l.w.WriteLine("}")

	l.emitSignalReplay(sig, ret, retGo)

	l.w.Dedent()
	l.w.WriteLine("}")
	l.tryDepth--
}

// emitSignalBody assigns sigVar(, retVar) from a closure returning the control
// code, lowering body with redirection so its return/break/continue become
// signal returns instead of escaping the closure.
func (l *Lowerer) emitSignalBody(sigVar, retVar, retGo string, body func()) {
	if retGo == "" {
		l.w.WriteString(sigVar + " = func() int {")
	} else {
		l.w.WriteString(sigVar + ", " + retVar + " = func() (int, " + retGo + ") {")
	}
	l.w.WriteLine("")
	l.w.Indent()
	saved := l.tryCtl
	l.tryCtl = &tryControl{retGo: retGo}
	body()
	l.tryCtl = saved
	if retGo == "" {
		l.w.WriteLine("return 0")
	} else {
		l.w.WriteString("return 0, " + zeroValue(retGo))
		l.w.WriteLine("")
	}
	l.w.Dedent()
	l.w.WriteLine("}()")
}

func (l *Lowerer) emitCatchDispatch(catches []ast.CatchClause, sig, ret, exc, unhandled, retGo string) {
	gerr := exc + "_e"
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString(gerr + ", " + gerr + "_ok := " + exc + ".(*transpilert.Error)")
	l.w.WriteLine("")
	l.w.WriteString("_, _ = " + gerr + ", " + gerr + "_ok")
	l.w.WriteLine("")
	l.w.WriteString(unhandled + " = true")
	l.w.WriteLine("")

	first := true
	for _, c := range catches {
		var cond string
		if c.Type == nil {
			cond = "true"
		} else {
			cond = gerr + "_ok && " + gerr + ".IsClass(" + strconv.Quote(simpleClassName(c.Type.Name)) + ")"
		}
		if first {
			l.w.WriteString("if " + cond + " {")
			first = false
		} else {
			l.w.WriteString("} else if " + cond + " {")
		}
		l.w.WriteLine("")
		l.w.Indent()
		l.w.WriteString(unhandled + " = false")
		l.w.WriteLine("")
		l.emitSignalBody(sig, ret, retGo, func() {
			l.withChildScope(func() {
				if c.Name != nil {
					name := emit.MangleIdent(c.Name.Value)
					l.scope.Define(&types.Binding{Name: c.Name.Value, Type: errorBindingType, Mutable: false})
					l.w.WriteString(name + " := " + gerr)
					l.w.WriteLine("")
					l.w.WriteString("_ = " + name)
					l.w.WriteLine("")
				}
				if c.Body != nil {
					l.lowerBlock(c.Body.Statements)
				}
			})
		})
		l.w.Dedent()
	}
	if !first {
		l.w.WriteLine("}")
	}
}

// emitSignalReplay turns the stored control code into a real Go transfer:
// return when this try is at function top level, or a re-signal when it is
// nested inside another try-region closure. break/continue are emitted only
// where a target loop exists (or to re-signal outward).
func (l *Lowerer) emitSignalReplay(sig, ret, retGo string) {
	redirect := l.tryCtl != nil && !l.tryCtl.retSuspended
	l.w.WriteString("if " + sig + " == 1 {")
	l.w.WriteLine("")
	l.w.Indent()
	switch {
	case redirect && retGo == "":
		l.w.WriteLine("return 1")
	case redirect:
		l.w.WriteLine("return 1, " + ret)
	case retGo == "":
		l.w.WriteLine("return")
	default:
		l.w.WriteLine("return " + ret)
	}
	l.w.Dedent()
	l.w.WriteLine("}")

	l.emitLoopReplay(sig, "2", retGo)
	l.emitLoopReplay(sig, "3", retGo)
}

func (l *Lowerer) emitLoopReplay(sig, code, retGo string) {
	reSignal := l.tryCtl != nil && !l.tryCtl.loopSuspended
	inLoop := l.loopDepth > 0
	if !reSignal && !inLoop {
		return // break/continue cannot occur here; nothing to replay
	}
	word := "break"
	if code == "3" {
		word = "continue"
	}
	l.w.WriteString("if " + sig + " == " + code + " {")
	l.w.WriteLine("")
	l.w.Indent()
	if reSignal {
		if retGo == "" {
			l.w.WriteLine("return " + code)
		} else {
			l.w.WriteLine("return " + code + ", " + zeroValue(retGo))
		}
	} else {
		l.w.WriteLine(word)
	}
	l.w.Dedent()
	l.w.WriteLine("}")
}

// bodyEndsWithTry reports whether the last statement is a try; Go cannot prove
// such a body terminates, so a typed function needs an explicit trailing return.
func bodyEndsWithTry(body *ast.BlockStatement) bool {
	if body == nil || len(body.Statements) == 0 {
		return false
	}
	_, ok := body.Statements[len(body.Statements)-1].(*ast.TryStatement)
	return ok
}

// simpleClassName strips an optional module prefix (errors.Foo -> Foo) so the
// catch class matches the bare name recorded in the error's parent chain.
func simpleClassName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func (l *Lowerer) lowerMatchStatement(s *ast.MatchStatement) {
	if hasVariantCases(s.Cases) {
		l.emitMatchAsTypeSwitch(s.Expr, s.Cases, false)
		return
	}
	l.emitMatchAsIfChain(s.Expr, s.Cases, false, nil)
}

func (l *Lowerer) lowerMatchExpression(e *ast.MatchExpression) {
	l.withNestedFunc(func() { l.lowerMatchExpressionInner(e) })
}

func (l *Lowerer) lowerMatchExpressionInner(e *ast.MatchExpression) {
	retGo := l.matchExprReturnType(e.Cases)
	if hasVariantCases(e.Cases) {
		l.w.WriteString("func() ")
		l.w.WriteString(retGo.Source)
		l.w.WriteLine(" {")
		l.w.Indent()
		l.emitMatchAsTypeSwitch(e.Expr, e.Cases, true)
		l.w.Dedent()
		l.w.WriteString("}()")
		return
	}
	l.w.WriteString("func() ")
	l.w.WriteString(retGo.Source)
	l.w.WriteString(" {")
	l.w.Newline()
	l.w.Indent()
	scrutinee := emit.NewWriter()
	l.w.WriteString("__m := ")
	saved := l.w
	l.w = scrutinee
	l.lowerExpression(e.Expr)
	l.w = saved
	l.w.WriteString(scrutinee.String())
	l.w.WriteLine("")
	l.emitMatchAsIfChain(nil, e.Cases, true, nil)
	l.w.Dedent()
	l.w.WriteString("}()")
}

func (l *Lowerer) matchExprReturnType(cases []ast.MatchCase) types.GoType {
	var inferred *types.Type
	for _, c := range cases {
		if c.Value == nil {
			continue
		}
		var t *types.Type
		if c.EnumVariant != nil && len(c.EnumVariant.Params) > 0 {
			saved := l.scope
			l.scope = saved.Child()
			for _, p := range c.EnumVariant.Params {
				l.scope.Define(&types.Binding{Name: p.Name.Value, Type: l.resolveTypeRef(p.Type)})
			}
			t = l.inferExpressionType(c.Value)
			l.scope = saved
		} else {
			t = l.inferExpressionType(c.Value)
		}
		if t == nil || t.Kind == types.KindUnknown {
			return types.GoType{Source: "any"}
		}
		if inferred == nil {
			inferred = t
			continue
		}
		if inferred.Kind != t.Kind {
			return types.GoType{Source: "any"}
		}
	}
	if inferred == nil {
		return types.GoType{Source: "any"}
	}
	got := types.ToGo(inferred, l.Module.IntMode)
	l.Module.AddTypeImports(got)
	return got
}

func hasVariantCases(cases []ast.MatchCase) bool {
	for _, c := range cases {
		if c.EnumVariant != nil {
			return true
		}
	}
	return false
}

func (l *Lowerer) emitMatchAsTypeSwitch(scrutinee ast.Expression, cases []ast.MatchCase, asExpression bool) {
	l.w.WriteString("switch __m := any(")
	l.lowerExpression(scrutinee)
	l.w.WriteLine(").(type) {")

	var defaultCase *ast.MatchCase
	for i := range cases {
		c := &cases[i]
		if c.Default {
			defaultCase = c
			continue
		}
		if c.EnumVariant == nil {
			l.errAt(c.Token.Line, c.Token.Column,
				"non-variant case in a variant-pattern match",
				"this slice supports either all-variant cases or no variant cases")
			continue
		}
		enumName := c.EnumVariant.Enum.Value
		if dot := strings.LastIndexByte(enumName, '.'); dot >= 0 {
			enumName = enumName[dot+1:]
		}
		variantStruct := emit.MangleIdent(enumName) + emit.MangleIdent(c.EnumVariant.Variant.Value)
		l.w.WriteString("case ")
		l.w.WriteString(variantStruct)
		l.w.WriteLine(":")
		l.w.Indent()
		l.withChildScope(func() {
			for i, p := range c.EnumVariant.Params {
				name := emit.MangleIdent(p.Name.Value)
				l.scope.Define(&types.Binding{Name: p.Name.Value, Type: types.Any(), Mutable: false})
				l.w.WriteString(name)
				l.w.WriteString(" := __m.V")
				l.w.WriteString(fmt.Sprintf("%d", i))
				l.w.WriteLine("")
				// The interpreter binds the param unconditionally; a body that
				// never reads it must still compile as Go.
				l.w.WriteLine("_ = " + name)
			}
			if len(c.EnumVariant.Params) == 0 {
				l.w.WriteLine("_ = __m")
			}
			l.emitMatchCaseBody(c, asExpression)
		})
		l.w.Dedent()
	}
	if defaultCase != nil {
		l.w.WriteLine("default:")
		l.w.Indent()
		l.w.WriteLine("_ = __m")
		l.emitMatchCaseBody(defaultCase, asExpression)
		l.w.Dedent()
	} else {
		// The interpreter raises an uncaught MatchError when no variant matches
		// (statement and expression alike), so mirror it rather than fall
		// through silently or panic raw.
		l.w.WriteLine("default:")
		l.w.Indent()
		l.emitMatchNoCasePanic("__m")
		l.w.Dedent()
	}
	l.w.WriteLine("}")
	if asExpression && defaultCase == nil {
		l.w.WriteLine("panic(\"unreachable\")")
	}
}

func (l *Lowerer) emitMatchAsIfChain(scrutinee ast.Expression, cases []ast.MatchCase, asExpression bool, _ *types.Scope) {
	scrutineeName := "__m"
	if scrutinee != nil {
		l.w.WriteString("__m := ")
		l.lowerExpression(scrutinee)
		l.w.WriteLine("")
	}

	var defaultCase *ast.MatchCase
	for i := range cases {
		if cases[i].Default {
			defaultCase = &cases[i]
			break
		}
	}

	first := true
	for i := range cases {
		c := &cases[i]
		if c.Default {
			continue
		}
		variantConst, isEnumVariant := l.enumVariantTypePattern(c.Type)
		if c.Pattern == nil && !isEnumVariant {
			l.errAt(c.Token.Line, c.Token.Column,
				"only literal patterns + default are supported in Phase 1",
				"type-patterns and enum-variant patterns are deferred to Phase 2/3")
			continue
		}
		keyword := "if"
		if !first {
			keyword = " else if"
		}
		first = false
		l.w.WriteString(keyword)
		l.w.WriteString(" ")
		l.w.WriteString(scrutineeName)
		l.w.WriteString(" == ")
		if isEnumVariant {
			l.w.WriteString(variantConst)
		} else {
			l.lowerExpression(c.Pattern)
		}
		if c.Guard != nil {
			l.w.WriteString(" && (")
			l.lowerExpression(c.Guard)
			l.w.WriteString(")")
		}
		l.w.WriteLine(" {")
		l.w.Indent()
		l.emitMatchCaseBody(c, asExpression)
		l.w.Dedent()
		l.w.WriteString("}")
	}
	if defaultCase != nil {
		if !first {
			l.w.WriteString(" else ")
		}
		l.w.WriteLine("{")
		l.w.Indent()
		l.emitMatchCaseBody(defaultCase, asExpression)
		l.w.Dedent()
		l.w.WriteLine("}")
	} else if !first {
		l.w.WriteLine("")
	}
	if asExpression && defaultCase == nil {
		l.emitMatchNoCasePanic(scrutineeName)
	}
}

// emitMatchNoCasePanic renders the no-default fall-through of a match expression
// as an uncaught MatchError (mirroring the interpreter), so a typed-return IIFE
// stays valid Go and an unmatched value exits cleanly rather than panicking raw.
func (l *Lowerer) emitMatchNoCasePanic(scrutineeName string) {
	l.requireUncaughtHandler()
	l.w.WriteString("panic(transpilert.NewError(\"MatchError\", \"no match case matched (add a 'default:' case to handle all values); got \" + transpilert.Show(")
	l.w.WriteString(scrutineeName)
	l.w.WriteString(") + \" (type: \" + transpilert.GbTypeName(")
	l.w.WriteString(scrutineeName)
	l.w.WriteLine(") + \")\"))")
}

// enumVariantTypePattern reports a `case Enum.Variant` over an untagged enum
// (parsed as a type ref `Enum.Variant`), returning the Go const for an equality
// test; the interpreter treats this pattern as a variant identity check.
func (l *Lowerer) enumVariantTypePattern(t *ast.TypeRef) (string, bool) {
	if t == nil {
		return "", false
	}
	dot := strings.IndexByte(t.Name, '.')
	if dot < 0 {
		return "", false
	}
	enumName, variant := t.Name[:dot], t.Name[dot+1:]
	if !l.Module.IsEnum(enumName) || !l.Module.EnumHasVariant(enumName, variant) {
		return "", false
	}
	return emit.MangleIdent(enumName) + emit.MangleIdent(variant), true
}

func (l *Lowerer) emitMatchCaseBody(c *ast.MatchCase, asExpression bool) {
	if asExpression {
		if c.Value != nil {
			l.w.WriteString("return ")
			l.lowerExpression(c.Value)
			l.w.WriteLine("")
			return
		}
		if c.Body != nil {
			l.lowerBlock(c.Body.Statements)
		}
		l.w.WriteLine("return nil")
		return
	}
	if c.Body != nil {
		l.lowerBlock(c.Body.Statements)
		return
	}
	if c.Value != nil {
		l.lowerExpression(c.Value)
		l.w.WriteLine("")
	}
}

func (l *Lowerer) lowerSimple(s *ast.SimpleStatement) {
	switch s.Kind {
	case "break", "continue":
		if l.tryCtl != nil && !l.tryCtl.loopSuspended {
			code := "2"
			if s.Kind == "continue" {
				code = "3"
			}
			if l.tryCtl.retGo == "" {
				l.w.WriteLine("return " + code)
			} else {
				l.w.WriteLine("return " + code + ", " + tryZeroExpr(l.tryCtl.retGo))
			}
			return
		}
		l.w.WriteLine(s.Kind)
	case "defer":
		if s.Value == nil {
			l.errAt(s.Token.Line, s.Token.Column, "defer requires an expression", "")
			return
		}
		l.w.WriteString("defer ")
		l.lowerExpression(s.Value)
		l.w.WriteLine("")
	case "throw":
		if s.Value == nil {
			l.errAt(s.Token.Line, s.Token.Column, "throw requires an expression", "")
			return
		}
		l.requireUncaughtHandler()
		l.w.WriteString("panic(")
		l.lowerExpression(s.Value)
		l.w.WriteLine(")")
	default:
		l.errAt(s.Token.Line, s.Token.Column,
			fmt.Sprintf("unsupported simple statement %q", s.Kind),
			"")
	}
}

func (l *Lowerer) lowerBlock(stmts []ast.Statement) {
	l.recordEmptyCollectionRefinements(stmts)
	for _, s := range stmts {
		l.lowerStatement(s)
	}
}

// recordEmptyCollectionRefinements types untyped empty list/dict declarations
// from later push/index assignments in the same statement sequence.
func (l *Lowerer) recordEmptyCollectionRefinements(stmts []ast.Statement) {
	for i, s := range stmts {
		decl, ok := s.(*ast.DeclarationStatement)
		if !ok {
			continue
		}
		if elem := l.refineEmptyCollectionElem(decl, stmts[i+1:]); elem != nil {
			if l.refinedDecls == nil {
				l.refinedDecls = map[*ast.DeclarationStatement]*types.Type{}
			}
			l.refinedDecls[decl] = elem
		}
	}
}

// refineEmptyCollectionElem types an untyped `let x = []` / `let x = {}` from a
// later `x = x.push(e)` or `x[k] = v` in the same block, so iterating or
// indexing x lowers to a concrete element type instead of any.
func (l *Lowerer) refineEmptyCollectionElem(decl *ast.DeclarationStatement, tail []ast.Statement) *types.Type {
	if decl.Type != nil || decl.Value == nil {
		return nil
	}
	name := decl.Name.Value
	switch lit := decl.Value.(type) {
	case *ast.ListLiteral:
		if len(lit.Elements) != 0 {
			return nil
		}
		var found *types.Type
		walkStatements(tail, func(s ast.Statement) {
			if found == nil {
				if e := l.listPushElemType(name, s); e != nil {
					found = &types.Type{Kind: types.KindList, Elem: e}
				}
			}
		})
		return found
	case *ast.DictLiteral:
		if len(lit.Entries) != 0 {
			return nil
		}
		var found *types.Type
		walkStatements(tail, func(s ast.Statement) {
			if found == nil {
				if k, v := l.dictAssignTypes(name, s); v != nil {
					found = &types.Type{Kind: types.KindDict, Key: k, Value: v}
				}
			}
		})
		return found
	}
	return nil
}

// walkStatements visits each statement and recurses into nested block bodies so
// an assignment inside a loop/if refines a declaration in an outer scope.
func walkStatements(stmts []ast.Statement, visit func(ast.Statement)) {
	for _, s := range stmts {
		visit(s)
		switch n := s.(type) {
		case *ast.ForStatement:
			if n.Body != nil {
				walkStatements(n.Body.Statements, visit)
			}
		case *ast.WhileStatement:
			if n.Body != nil {
				walkStatements(n.Body.Statements, visit)
			}
		case *ast.IfStatement:
			if n.Consequence != nil {
				walkStatements(n.Consequence.Statements, visit)
			}
			for _, ei := range n.ElseIfs {
				if ei.Body != nil {
					walkStatements(ei.Body.Statements, visit)
				}
			}
			if n.Alternative != nil {
				walkStatements(n.Alternative.Statements, visit)
			}
		case *ast.BlockStatement:
			walkStatements(n.Statements, visit)
		}
	}
}

// listPushElemType returns the element type of `name = name.push(e)`.
func (l *Lowerer) listPushElemType(name string, s ast.Statement) *types.Type {
	es, ok := s.(*ast.ExpressionStatement)
	if !ok {
		return nil
	}
	asg, ok := es.Expression.(*ast.AssignmentExpression)
	if !ok || !isIdent(asg.Left, name) {
		return nil
	}
	call, ok := asg.Value.(*ast.CallExpression)
	if !ok {
		return nil
	}
	sel, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || sel.Name.Value != "push" || !isIdent(sel.Object, name) || len(call.Arguments) != 1 {
		return nil
	}
	if t := l.inferExpressionType(call.Arguments[0].Value); t != nil && t.Kind != types.KindUnknown {
		return t
	}
	return nil
}

// dictAssignTypes returns the key/value types of `name[k] = v`.
func (l *Lowerer) dictAssignTypes(name string, s ast.Statement) (*types.Type, *types.Type) {
	es, ok := s.(*ast.ExpressionStatement)
	if !ok {
		return nil, nil
	}
	asg, ok := es.Expression.(*ast.AssignmentExpression)
	if !ok {
		return nil, nil
	}
	idx, ok := asg.Left.(*ast.IndexExpression)
	if !ok || !isIdent(idx.Left, name) {
		return nil, nil
	}
	v := l.inferExpressionType(asg.Value)
	if v == nil || v.Kind == types.KindUnknown {
		return nil, nil
	}
	k := l.inferExpressionType(idx.Index)
	if k == nil || k.Kind == types.KindUnknown {
		k = &types.Type{Kind: types.KindString}
	}
	return k, v
}

func isIdent(e ast.Expression, name string) bool {
	id, ok := e.(*ast.Identifier)
	return ok && id.Value == name
}

func (l *Lowerer) lowerFor(s *ast.ForStatement) {
	if s.Iterable != nil {
		if r, ok := s.Iterable.(*ast.RangeExpression); ok {
			l.lowerForRange(s, r)
			return
		}
		l.lowerForIn(s)
		return
	}
	l.lowerForC(s)
}

func (l *Lowerer) lowerForC(s *ast.ForStatement) {
	l.withChildScope(func() {
		l.w.WriteString("for ")
		if s.Init != nil {
			l.lowerInlineStatement(s.Init)
		}
		l.w.WriteString("; ")
		if s.Condition != nil {
			l.lowerExpression(s.Condition)
		}
		l.w.WriteString("; ")
		if s.Update != nil {
			l.lowerInlineStatement(s.Update)
		}
		l.w.WriteLine(" {")
		l.w.Indent()
		l.withLoopBody(func() { l.lowerBlock(s.Body.Statements) })
		l.w.Dedent()
		l.w.WriteLine("}")
	})
}

func (l *Lowerer) lowerForRange(s *ast.ForStatement, r *ast.RangeExpression) {
	name := "_"
	if s.VarName != nil {
		name = emit.MangleIdent(s.VarName.Value)
	}
	l.withChildScope(func() {
		if s.VarName != nil {
			l.scope.Define(&types.Binding{
				Name:    s.VarName.Value,
				Type:    &types.Type{Kind: types.KindInt},
				Mutable: true,
			})
		}
		l.w.WriteString("for ")
		l.w.WriteString(name)
		l.w.WriteString(" := int64(")
		l.lowerExpression(r.Start)
		l.w.WriteString("); ")
		l.w.WriteString(name)
		if r.Exclusive {
			l.w.WriteString(" < ")
		} else {
			l.w.WriteString(" <= ")
		}
		l.w.WriteString("int64(")
		l.lowerExpression(r.End)
		l.w.WriteString("); ")
		l.w.WriteString(name)
		step := s.Step
		if step == nil {
			step = r.Step
		}
		if step != nil {
			l.w.WriteString(" += int64(")
			l.lowerExpression(step)
			l.w.WriteString(")")
		} else {
			l.w.WriteString("++")
		}
		l.w.WriteLine(" {")
		l.w.Indent()
		l.withLoopBody(func() { l.lowerBlock(s.Body.Statements) })
		l.w.Dedent()
		l.w.WriteLine("}")
	})
}

func (l *Lowerer) lowerForIn(s *ast.ForStatement) {
	receiver, isDictItems := l.dictItemsReceiver(s.Iterable)
	if isDictItems && len(s.VarNames) == 2 {
		l.lowerForInDict(s, receiver)
		return
	}
	l.lowerForInList(s)
}

func (l *Lowerer) dictItemsReceiver(iter ast.Expression) (ast.Expression, bool) {
	call, ok := iter.(*ast.CallExpression)
	if !ok {
		return nil, false
	}
	sel, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || sel.Name.Value != "items" || len(call.Arguments) != 0 {
		return nil, false
	}
	ty := l.inferExpressionType(sel.Object)
	if ty == nil || ty.Kind != types.KindDict {
		return nil, false
	}
	return sel.Object, true
}

func (l *Lowerer) lowerForInDict(s *ast.ForStatement, receiver ast.Expression) {
	keyName := emit.MangleIdent(s.VarNames[0].Value)
	valName := emit.MangleIdent(s.VarNames[1].Value)
	if !referencesName(s.Body, s.VarNames[0].Value) {
		keyName = "_"
	}
	if !referencesName(s.Body, s.VarNames[1].Value) {
		valName = "_"
	}
	recvTy := l.inferExpressionType(receiver)
	var keyTy, valTy *types.Type
	if recvTy != nil && recvTy.Kind == types.KindDict {
		keyTy, valTy = recvTy.Key, recvTy.Value
	}
	// OrderedDict has no range form; walk ordered keys and look up each value.
	l.withChildScope(func() {
		l.scope.Define(&types.Binding{Name: s.VarNames[0].Value, Type: keyTy, Mutable: false})
		l.scope.Define(&types.Binding{Name: s.VarNames[1].Value, Type: valTy, Mutable: false})
		rangeKey := keyName
		if valName != "_" && rangeKey == "_" {
			rangeKey = "__k"
		}
		l.w.WriteString("for _, ")
		l.w.WriteString(rangeKey)
		l.w.WriteString(" := range ")
		l.lowerExpression(receiver)
		l.w.WriteLine(".Keys() {")
		l.w.Indent()
		if valName != "_" {
			l.w.WriteString(valName)
			l.w.WriteString(", _ := ")
			l.lowerExpression(receiver)
			l.w.WriteString(".Get(")
			l.w.WriteString(rangeKey)
			l.w.WriteLine(")")
		}
		l.withLoopBody(func() { l.lowerBlock(s.Body.Statements) })
		l.w.Dedent()
		l.w.WriteLine("}")
	})
}

func (l *Lowerer) lowerForInList(s *ast.ForStatement) {
	name := "_"
	if s.VarName != nil {
		name = emit.MangleIdent(s.VarName.Value)
	} else if len(s.VarNames) > 0 {
		name = emit.MangleIdent(s.VarNames[0].Value)
	}
	iterTy := l.inferExpressionType(s.Iterable)
	var elemTy *types.Type
	isGen := false
	if iterTy != nil {
		switch iterTy.Kind {
		case types.KindList, types.KindSet:
			elemTy = iterTy.Elem
		case types.KindGenerator:
			elemTy = iterTy.Elem
			isGen = true
		}
	}
	l.withChildScope(func() {
		if s.VarName != nil {
			l.scope.Define(&types.Binding{Name: s.VarName.Value, Type: elemTy, Mutable: false})
		} else if len(s.VarNames) > 0 {
			l.scope.Define(&types.Binding{Name: s.VarNames[0].Value, Type: elemTy, Mutable: false})
		}
		if isGen {
			l.w.WriteString("for ")
			l.w.WriteString(name)
			l.w.WriteString(" := range ")
		} else {
			l.w.WriteString("for _, ")
			l.w.WriteString(name)
			l.w.WriteString(" := range ")
		}
		l.lowerExpression(s.Iterable)
		l.w.WriteLine(" {")
		l.w.Indent()
		l.withLoopBody(func() { l.lowerBlock(s.Body.Statements) })
		l.w.Dedent()
		l.w.WriteLine("}")
	})
}

func (l *Lowerer) lowerInlineStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.DeclarationStatement:
		l.lowerInlineDeclaration(s)
	case *ast.ExpressionStatement:
		l.lowerExpression(s.Expression)
	default:
		l.errAt(0, 0, fmt.Sprintf("unsupported inline statement: %T", stmt),
			"only declarations and expression statements may appear in a for-loop header")
	}
}

func (l *Lowerer) lowerInlineDeclaration(s *ast.DeclarationStatement) {
	name := emit.MangleIdent(s.Name.Value)
	var declared *types.Type
	if s.Type != nil {
		declared = l.resolveTypeRef(s.Type)
	} else if s.Value != nil {
		declared = l.inferExpressionType(s.Value)
	}
	if declared == nil {
		declared = types.Unknown()
	}
	l.scope.Define(&types.Binding{Name: s.Name.Value, Type: declared, Mutable: true})

	l.w.WriteString(name)
	l.w.WriteString(" := ")
	if s.Value == nil {
		l.w.WriteString("nil")
		return
	}
	if declared != nil && declared.Kind == types.KindInt {
		l.w.WriteString("int64(")
		l.lowerExpression(s.Value)
		l.w.WriteString(")")
		return
	}
	l.withExpectedType(declared, func() { l.lowerValueAs(declared, s.Value) })
}

func (l *Lowerer) lowerIf(s *ast.IfStatement) {
	l.w.WriteString("if ")
	l.lowerExpression(s.Condition)
	l.w.WriteLine(" {")
	l.w.Indent()
	l.lowerBlock(s.Consequence.Statements)
	l.w.Dedent()
	for _, ei := range s.ElseIfs {
		l.w.WriteString("} else if ")
		l.lowerExpression(ei.Condition)
		l.w.WriteLine(" {")
		l.w.Indent()
		l.lowerBlock(ei.Body.Statements)
		l.w.Dedent()
	}
	if s.Alternative != nil {
		l.w.WriteLine("} else {")
		l.w.Indent()
		l.lowerBlock(s.Alternative.Statements)
		l.w.Dedent()
	}
	l.w.WriteLine("}")
}

func (l *Lowerer) lowerDeclaration(s *ast.DeclarationStatement) {
	name := emit.MangleIdent(s.Name.Value)

	var declared *types.Type
	refined := l.refinedDecls[s]
	if s.Type != nil {
		declared = l.resolveTypeRef(s.Type)
	} else if refined != nil {
		declared = refined
	} else if s.Value != nil {
		declared = l.inferExpressionType(s.Value)
	}
	if declared == nil {
		declared = types.Unknown()
	}

	l.scope.Define(&types.Binding{Name: s.Name.Value, Type: declared, Mutable: true})

	// A refined empty list/dict declares the concrete Go type and emits the
	// typed-empty literal so later element use type-checks.
	if refined != nil {
		goTy := types.ToGo(declared, l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		l.w.WriteString(name)
		l.w.WriteString(" := ")
		l.withExpectedType(declared, func() { l.lowerExpression(s.Value) })
		l.w.WriteLine("")
		return
	}

	if s.Type != nil {
		goTy := types.ToGo(declared, l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		l.w.WriteString("var ")
		l.w.WriteString(name)
		l.w.WriteString(" ")
		l.w.WriteString(goTy.Source)
		if s.Value != nil {
			l.w.WriteString(" = ")
			if declared.Nullable {
				l.lowerIntoNullable(declared, s.Value)
			} else {
				l.withExpectedType(declared, func() { l.lowerValueAs(declared, s.Value) })
			}
		}
		l.w.WriteLine("")
		return
	}

	if declared != nil && (declared.Kind == types.KindInt || declared.Kind == types.KindInterface) {
		goTy := types.ToGo(declared, l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		l.w.WriteString("var ")
		l.w.WriteString(name)
		l.w.WriteString(" ")
		l.w.WriteString(goTy.Source)
		l.w.WriteString(" = ")
		if s.Value != nil {
			l.lowerExpression(s.Value)
		} else if declared.Kind == types.KindInt {
			if l.Module.IntMode == types.IntModeBigInt {
				l.Module.AddImport(types.OrderedDictImport)
				l.w.WriteString("transpilert.FromInt64(0)")
			} else {
				l.w.WriteString("0")
			}
		} else {
			l.w.WriteString("nil")
		}
		l.w.WriteLine("")
		return
	}

	if s.Value != nil && isNullLiteral(s.Value) {
		l.w.WriteString("var ")
		l.w.WriteString(name)
		l.w.WriteLine(" any = nil")
		return
	}

	l.w.WriteString(name)
	l.w.WriteString(" := ")
	if s.Value != nil {
		l.lowerExpression(s.Value)
	} else {
		l.w.WriteString("nil")
	}
	l.w.WriteLine("")
}

// lowerModuleVar emits a non-entry module's top-level let/const as a package-level
// var. The Go name carries NamePrefix so same-module references bind to it; the
// binding is left out of the lexical scope so those references prefix rather than
// resolve as locals. A non-entry module runs no top-level code, so this is pure.
func (l *Lowerer) lowerModuleVar(s *ast.DeclarationStatement) {
	name := l.NamePrefix + emit.MangleIdent(s.Name.Value)

	var declared *types.Type
	if s.Type != nil {
		declared = l.resolveTypeRef(s.Type)
	} else if s.Value != nil {
		declared = l.inferExpressionType(s.Value)
	}
	hasType := declared != nil && declared.Kind != types.KindUnknown && declared.Kind != types.KindAny

	l.w.WriteString("var ")
	l.w.WriteString(name)
	if hasType {
		goTy := types.ToGo(declared, l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		l.w.WriteString(" ")
		l.w.WriteString(goTy.Source)
	}
	if s.Value != nil {
		l.w.WriteString(" = ")
		switch {
		case declared != nil && declared.Nullable:
			l.lowerIntoNullable(declared, s.Value)
		case hasType:
			l.withExpectedType(declared, func() { l.lowerValueAs(declared, s.Value) })
		default:
			l.lowerExpression(s.Value)
		}
	} else if !hasType {
		l.w.WriteString(" any = nil")
	}
	l.w.WriteLine("")
}

func isNullLiteral(expr ast.Expression) bool {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return false
	}
	return lit.Value == nil
}

// lowerDestructuring lowers `let [a, b] = list` / `let a, b = f()` (list) and
// `let {a, b} = dict` (dict). List form indexes the slice positionally; the
// bare-literal form binds each element directly so heterogeneous values keep
// their own types. Reassignment (Define false) emits `=`, declaration `:=`.
func (l *Lowerer) lowerDestructuring(s *ast.DestructuringStatement) {
	if s.IsList {
		l.lowerListDestructuring(s)
		return
	}
	l.lowerDictDestructuring(s)
}

func (l *Lowerer) lowerListDestructuring(s *ast.DestructuringStatement) {
	if lit, ok := s.Value.(*ast.ListLiteral); ok && s.Bare && len(lit.Elements) >= len(s.Names) {
		for i, name := range s.Names {
			l.bindDestructured(s, name, lit.Elements[i], l.inferExpressionType(lit.Elements[i]))
		}
		return
	}
	elemTy := types.Any()
	if vt := l.inferExpressionType(s.Value); vt != nil && vt.Kind == types.KindList && vt.Elem != nil {
		elemTy = vt.Elem
	}
	tmp := l.nextTmp()
	l.w.WriteString(tmp + " := ")
	l.lowerExpression(s.Value)
	l.w.WriteLine("")
	for i, name := range s.Names {
		mangled := emit.MangleIdent(name.Value)
		l.defineDestructured(s, name, elemTy)
		op := ":="
		if !s.Define {
			op = "="
		}
		l.w.WriteString(mangled + " " + op + " " + tmp + "[" + strconv.Itoa(i) + "]")
		l.w.WriteLine("")
		if s.Define {
			l.w.WriteString("_ = " + mangled)
			l.w.WriteLine("")
		}
	}
}

func (l *Lowerer) lowerDictDestructuring(s *ast.DestructuringStatement) {
	vt := l.inferExpressionType(s.Value)
	if vt == nil || vt.Kind != types.KindDict || vt.Value == nil || vt.Value.Kind != types.KindAny {
		l.errAt(s.Token.Line, s.Token.Column,
			"the transpiler supports dict destructuring only on a dict<string, any>",
			"a typed-value dict returns the value's zero on a missing key, not null")
		return
	}
	tmp := l.nextTmp()
	l.w.WriteString(tmp + " := ")
	l.lowerExpression(s.Value)
	l.w.WriteLine("")
	for i, name := range s.Names {
		key := name.Value
		if i < len(s.Keys) && s.Keys[i] != "" {
			key = s.Keys[i]
		}
		mangled := emit.MangleIdent(name.Value)
		l.defineDestructured(s, name, types.Any())
		op := ":="
		if !s.Define {
			op = "="
		}
		l.w.WriteString(mangled + " " + op + " func() any { __v, __ok := " + tmp + ".Get(" + strconv.Quote(key) + "); if !__ok { return nil }; return __v }()")
		l.w.WriteLine("")
		if s.Define {
			l.w.WriteString("_ = " + mangled)
			l.w.WriteLine("")
		}
	}
}

// defineDestructured records the binding when declaring; reassignment leaves the
// existing binding in place.
func (l *Lowerer) defineDestructured(s *ast.DestructuringStatement, name *ast.Identifier, ty *types.Type) {
	if s.Define {
		l.scope.Define(&types.Binding{Name: name.Value, Type: ty, Mutable: true})
	}
}

func (l *Lowerer) bindDestructured(s *ast.DestructuringStatement, name *ast.Identifier, value ast.Expression, ty *types.Type) {
	mangled := emit.MangleIdent(name.Value)
	l.defineDestructured(s, name, ty)
	op := ":="
	if !s.Define {
		op = "="
	}
	l.w.WriteString(mangled + " " + op + " ")
	l.lowerExpression(value)
	l.w.WriteLine("")
	if s.Define {
		l.w.WriteString("_ = " + mangled)
		l.w.WriteLine("")
	}
}

func (l *Lowerer) lowerImport(s *ast.ImportStatement) {
	if len(s.Path) == 0 {
		return
	}
	canonical := strings.Join(s.Path, ".")
	binding := canonical
	if s.Alias != nil {
		binding = s.Alias.Value
	} else {
		binding = s.Path[len(s.Path)-1]
	}
	// Stdlib wins externally; a self-import (`import profiler as native` inside
	// profiler) falls through to the native bridge, matching the runtime rule.
	if l.Module.IsSourceModule(canonical) && canonical != l.Canonical {
		l.Module.RegisterUserModule(binding, canonical)
		return
	}
	// A bridged module routes to lowerNativeCall directly; an unbridged native
	// module also registers as stdlib so calls diagnose cleanly (no transpiler
	// bridge) instead of mangling to a phantom user-module function.
	if l.Bridge.IsKnownModule(canonical) || native.IsNativeModule(canonical) {
		l.Module.RegisterStdlibModule(binding, canonical)
		return
	}
	l.Module.RegisterUserModule(binding, canonical)
}

// lowerWith lowers `with (expr) { ... }`. The resource's __enter() (if present)
// supplies the binding; __exit() (if present) runs on block exit, including a
// panic, via a deferred call in an inline closure. The evaluator fires only
// __exit on with-exit (not the destructor), so no destructor support is needed.
// A return/break/continue in the body would escape the closure with the wrong
// target, so that case is diagnosed.
func (l *Lowerer) lowerWith(s *ast.WithStatement) {
	if blockHasTopLevelControlFlow(s.Body) {
		l.errAt(s.Token.Line, s.Token.Column,
			"the transpiler does not yet support return/break/continue inside a with block",
			"these need the cleanup-signal machinery deferred to a later phase")
		return
	}
	resTy := l.inferExpressionType(s.Value)
	hasEnter := resTy != nil && resTy.Kind == types.KindClass && l.Module.ClassHasMethod(resTy.Name, "__enter")
	hasExit := resTy != nil && resTy.Kind == types.KindClass && l.Module.ClassHasMethod(resTy.Name, "__exit")

	l.w.WriteLine("func() {")
	l.w.Indent()
	res := l.nextTmp()
	l.w.WriteString(res + " := ")
	l.lowerExpression(s.Value)
	l.w.WriteLine("")
	l.w.WriteString("_ = " + res)
	l.w.WriteLine("")
	if hasExit {
		l.w.WriteString("defer " + res + ".__exit()")
		l.w.WriteLine("")
	}
	l.withChildScope(func() {
		if s.Name != nil {
			bound := emit.MangleIdent(s.Name.Value)
			l.scope.Define(&types.Binding{Name: s.Name.Value, Type: resTy, Mutable: false})
			l.w.WriteString(bound + " := " + res)
			if hasEnter {
				l.w.WriteString(".__enter()")
			}
			l.w.WriteLine("")
			l.w.WriteString("_ = " + bound)
			l.w.WriteLine("")
		} else if hasEnter {
			l.w.WriteString(res + ".__enter()")
			l.w.WriteLine("")
		}
		l.lowerBlock(s.Body.Statements)
	})
	l.w.Dedent()
	l.w.WriteLine("}()")
}

// blockHasTopLevelControlFlow reports a return/break/continue directly in the
// block (not nested in an inner loop/func), which would escape a wrapping
// cleanup closure with the wrong target.
func blockHasTopLevelControlFlow(b *ast.BlockStatement) bool {
	if b == nil {
		return false
	}
	for _, st := range b.Statements {
		switch s := st.(type) {
		case *ast.ReturnStatement:
			return true
		case *ast.SimpleStatement:
			if s.Kind == "break" || s.Kind == "continue" {
				return true
			}
		}
	}
	return false
}

// lowerFromImport records each imported name's local binding so a bare call to
// it resolves to the native bridge (stdlib) or the prefixed user-module symbol.
func (l *Lowerer) lowerFromImport(s *ast.FromImportStatement) {
	if len(s.Path) == 0 {
		return
	}
	canonical := strings.Join(s.Path, ".")
	isStdlib := l.Bridge.IsKnownModule(canonical) && !l.Module.IsSourceModule(canonical)
	if isStdlib {
		l.Module.RegisterStdlibModule(canonical, canonical)
	}
	for _, n := range s.Names {
		if n.Name == nil {
			continue
		}
		l.Module.RegisterFromImport(n.Local(), FromImportTarget{
			Module:   canonical,
			Name:     n.Name.Value,
			IsStdlib: isStdlib,
		})
	}
}

func (l *Lowerer) lowerExpressionStmt(s *ast.ExpressionStatement) {
	l.lowerExpression(s.Expression)
	l.w.WriteLine("")
}
