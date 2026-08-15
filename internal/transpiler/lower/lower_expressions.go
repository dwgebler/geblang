package lower

import (
	"encoding/hex"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/token"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
	"strconv"
	"strings"
)

func (l *Lowerer) lowerExpressionInto(w *emit.Writer, expr ast.Expression) {
	saved := l.w
	l.w = w
	l.lowerExpression(expr)
	l.w = saved
}

func (l *Lowerer) lowerTernary(e *ast.TernaryExpression) {
	thenTy := l.inferExpressionType(e.ThenExpr)
	elseTy := l.inferExpressionType(e.ElseExpr)
	goRet := "any"
	if thenTy != nil && elseTy != nil && thenTy.Kind != types.KindUnknown &&
		thenTy.Kind == elseTy.Kind && thenTy.Name == elseTy.Name {
		gt := types.ToGo(thenTy, l.Module.IntMode)
		l.Module.AddTypeImports(gt)
		goRet = gt.Source
	}
	l.w.WriteString("func() ")
	l.w.WriteString(goRet)
	l.w.WriteString(" { if ")
	l.lowerExpression(e.Condition)
	l.w.WriteString(" { return ")
	l.lowerExpression(e.ThenExpr)
	l.w.WriteString(" }; return ")
	l.lowerExpression(e.ElseExpr)
	l.w.WriteString(" }()")
}

// lowerPipe rewrites `x |> f` / `x |> f(a)` to the equivalent call (x first arg).
func (l *Lowerer) lowerPipe(e *ast.PipeExpression) {
	call, ok := ast.LowerPipe(e)
	if !ok {
		l.errAt(e.Token.Line, e.Token.Column,
			"the right side of |> must be a function or a function call",
			"")
		l.w.WriteString("nil")
		return
	}
	l.lowerCall(call)
}

// lowerPartial diagnoses: lowered IIFE needs type-inferred closure params, not yet supported.
func (l *Lowerer) lowerPartial(e *ast.PartialExpression) {
	l.errAt(e.Token.Line, e.Token.Column,
		"the transpiler does not yet support partial application",
		"assign a typed wrapper function instead of using _ placeholders")
	l.w.WriteString("nil")
}

// lowerRangeValue diagnoses a range used as a value. A range value is a lazy
// object with its own string form (`1..5`) and char-range variant, distinct
// from the eager slice `range()` produces; a faithful representation needs a
// runtime Range type deferred to a later phase. for-in over a range literal is
// handled directly and never reaches here.
func (l *Lowerer) lowerRangeValue(e *ast.RangeExpression) {
	l.errAt(e.Token.Line, e.Token.Column,
		"the transpiler does not yet support a range used as a value",
		"iterate it directly in a for-in loop, or build a list with range(start, end)")
	l.w.WriteString("nil")
}

func (l *Lowerer) lowerAwait(e *ast.AwaitExpression) {
	l.lowerExpression(e.Value)
	l.w.WriteString(".Await()")
}

func (l *Lowerer) lowerPrefix(e *ast.PrefixExpression) {
	op := e.Operator
	if op == "-" && l.Module.IntMode == types.IntModeBigInt {
		if rt := l.inferExpressionType(e.Right); rt != nil && rt.Kind == types.KindInt {
			l.Module.AddImport(types.OrderedDictImport)
			l.w.WriteString("transpilert.NegInt(")
			l.lowerExpression(e.Right)
			l.w.WriteString(")")
			return
		}
	}
	switch op {
	case "-", "!":
		l.w.WriteString(op)
		l.lowerExpression(e.Right)
	case "+":
		l.lowerExpression(e.Right)
	default:
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("unsupported prefix operator %q", op),
			"the transpiler does not yet lower this prefix operator")
		l.w.WriteString("nil")
	}
}

func (l *Lowerer) lowerPostfix(e *ast.PostfixExpression) {
	if e.Operator != "++" && e.Operator != "--" {
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("unsupported postfix operator %q", e.Operator), "")
		return
	}
	l.lowerExpression(e.Left)
	l.w.WriteString(e.Operator)
}

func (l *Lowerer) lowerAssignment(e *ast.AssignmentExpression) {
	if idx, ok := e.Left.(*ast.IndexExpression); ok {
		leftTy := l.inferExpressionType(idx.Left)
		if leftTy != nil && leftTy.Kind == types.KindDict {
			l.lowerExpression(idx.Left)
			l.w.WriteString(".Set(")
			l.lowerExpression(idx.Index)
			l.w.WriteString(", ")
			l.lowerExpression(e.Value)
			l.w.WriteString(")")
			return
		}
		if leftTy == nil || leftTy.Kind == types.KindAny || leftTy.Kind == types.KindUnknown {
			// Index-assign into an any-typed receiver (e.g. a json.parse result):
			// the lhs read lowers to transpilert.Index, not a Go lvalue, so route
			// the write through the runtime helper instead.
			l.requireUncaughtHandler()
			l.Module.AddImport(types.OrderedDictImport)
			l.w.WriteString("transpilert.IndexSet(")
			l.lowerExpression(idx.Left)
			l.w.WriteString(", ")
			l.lowerExpression(idx.Index)
			l.w.WriteString(", ")
			l.lowerExpression(e.Value)
			l.w.WriteString(")")
			return
		}
	}
	if sel, ok := e.Left.(*ast.SelectorExpression); ok && !sel.Optional && l.isHierarchyFieldAccess(sel.Object, sel.Name.Value) {
		l.lowerExpression(sel.Object)
		l.w.WriteString("." + fieldSetter(sel.Name.Value) + "(")
		l.lowerExpression(e.Value)
		l.w.WriteString(")")
		return
	}
	l.lowerExpression(e.Left)
	l.w.WriteString(" = ")
	if leftTy := l.inferExpressionType(e.Left); leftTy != nil && leftTy.Nullable {
		l.lowerIntoNullable(leftTy, e.Value)
		return
	}
	l.lowerExpression(e.Value)
}

func (l *Lowerer) lowerInterpolated(e *ast.InterpolatedString) {
	var format strings.Builder
	var args []ast.Expression
	for _, p := range e.Parts {
		if sl, ok := p.(*ast.StringLiteral); ok {
			format.WriteString(strings.ReplaceAll(sl.Value, "%", "%%"))
			continue
		}
		format.WriteString("%v")
		args = append(args, p)
	}
	l.Module.AddImport("fmt")
	l.w.WriteString("fmt.Sprintf(")
	l.w.WriteString(strconv.Quote(format.String()))
	for _, a := range args {
		l.w.WriteString(", ")
		l.lowerDisplay(a)
	}
	l.w.WriteString(")")
}

func (l *Lowerer) lowerList(e *ast.ListLiteral) {
	ty := l.inferExpressionType(e)
	if ty == nil || ty.Kind != types.KindList {
		ty = &types.Type{Kind: types.KindList, Elem: types.Any()}
	}
	if elemUnresolved(ty.Elem) && l.expectedType != nil && l.expectedType.Kind == types.KindList {
		ty = l.expectedType
	}
	// Semantic may widen a list element to any when an element is a non-literal
	// (e.g. a unary-minus); recover a concrete homogeneous element type here.
	if elemUnresolved(ty.Elem) && len(e.Elements) > 0 {
		if fb := l.elemFallback(e.Elements); !elemUnresolved(fb) {
			ty = &types.Type{Kind: types.KindList, Elem: fb}
		}
	}
	goTy := types.ToGo(ty, l.Module.IntMode)
	l.Module.AddTypeImports(goTy)
	l.w.WriteString(goTy.Source)
	l.w.WriteString("{")
	for i, el := range e.Elements {
		if i > 0 {
			l.w.WriteString(", ")
		}
		l.lowerExpression(el)
	}
	l.w.WriteString("}")
}

func (l *Lowerer) lowerDict(e *ast.DictLiteral) {
	ty := l.inferExpressionType(e)
	if ty == nil || ty.Kind != types.KindDict {
		ty = &types.Type{
			Kind:  types.KindDict,
			Key:   &types.Type{Kind: types.KindString},
			Value: types.Any(),
		}
	}
	if (elemUnresolved(ty.Value) || elemUnresolved(ty.Key)) && l.expectedType != nil && l.expectedType.Kind == types.KindDict {
		ty = l.expectedType
	}
	keyGo := types.ToGo(ty.Key, l.Module.IntMode)
	valGo := types.ToGo(ty.Value, l.Module.IntMode)
	l.Module.AddImport(types.OrderedDictImport)
	l.Module.AddTypeImports(keyGo)
	l.Module.AddTypeImports(valGo)
	ctor := "transpilert.NewOrderedDict[" + keyGo.Source + ", " + valGo.Source + "]()"
	if len(e.Entries) == 0 {
		l.w.WriteString(ctor)
		return
	}
	l.w.WriteString("func() *transpilert.OrderedDict[")
	l.w.WriteString(keyGo.Source)
	l.w.WriteString(", ")
	l.w.WriteString(valGo.Source)
	l.w.WriteString("] { __d := ")
	l.w.WriteString(ctor)
	l.w.WriteString("; ")
	for _, entry := range e.Entries {
		l.w.WriteString("__d.Set(")
		l.lowerExpression(entry.Key)
		l.w.WriteString(", ")
		l.lowerExpression(entry.Value)
		l.w.WriteString("); ")
	}
	l.w.WriteString("return __d }()")
}

func (l *Lowerer) lowerInfix(e *ast.InfixExpression) {
	if e.Operator == "instanceof" {
		l.lowerInstanceOf(e)
		return
	}
	if e.Operator == "??" {
		l.lowerNullCoalesce(e)
		return
	}
	if magic, ok := magicMethodForOperator(e.Operator); ok {
		if leftTy := l.inferExpressionType(e.Left); leftTy != nil && leftTy.Kind == types.KindClass {
			if l.Module.ClassHasMethod(leftTy.Name, magic) {
				l.lowerExpression(e.Left)
				l.w.WriteString(".")
				l.w.WriteString(magic)
				l.w.WriteString("(")
				l.lowerExpression(e.Right)
				l.w.WriteString(")")
				return
			}
		}
	}
	if e.Operator == "==" || e.Operator == "!=" {
		// Comparing a collection to the null literal is a nil check, not
		// structural equality; collections lower to nilable Go types.
		nullCmp := isNullLiteral(e.Left) || isNullLiteral(e.Right)
		if !nullCmp && (l.isCollectionOperand(e.Left) || l.isCollectionOperand(e.Right)) {
			l.errAt(e.Token.Line, e.Token.Column,
				"the transpiler does not yet support == / != on lists, sets, dicts, or bytes",
				"structural equality needs runtime support deferred to a later phase")
			l.w.WriteString("false")
			return
		}
		if l.lowerNullableEquality(e) {
			return
		}
	}
	if l.Module.IntMode == types.IntModeBigInt && l.bothIntOperands(e.Left, e.Right) {
		if l.lowerSafeIntInfix(e) {
			return
		}
	}
	op := goOperator(e.Operator)
	if op == "" {
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("unsupported infix operator %q", e.Operator),
			"the transpiler does not yet lower this operator")
		l.w.WriteString("nil")
		return
	}
	l.w.WriteString("(")
	l.lowerExpression(e.Left)
	l.w.WriteString(" ")
	l.w.WriteString(op)
	l.w.WriteString(" ")
	l.lowerExpression(e.Right)
	l.w.WriteString(")")
}

func (l *Lowerer) lowerInstanceOf(e *ast.InfixExpression) {
	right, ok := e.Right.(*ast.Identifier)
	if !ok {
		l.errAt(e.Token.Line, e.Token.Column,
			"instanceof requires a simple type name",
			"complex type expressions are not yet supported on the right of instanceof")
		l.w.WriteString("false")
		return
	}
	typeName := right.Value
	var goType string
	if dotIdx := strings.IndexByte(typeName, '.'); dotIdx > 0 {
		enumName := typeName[:dotIdx]
		variantName := typeName[dotIdx+1:]
		if l.Module.IsTaggedVariant(enumName, variantName) {
			goType = emit.MangleIdent(enumName) + emit.MangleIdent(variantName)
			l.w.WriteString("func() bool { _, __ok := any(")
			l.lowerExpression(e.Left)
			l.w.WriteString(").(")
			l.w.WriteString(goType)
			l.w.WriteString("); return __ok }()")
			return
		}
	}
	switch {
	case l.Module.IsClass(typeName):
		goType = "*" + emit.MangleIdent(typeName)
	case l.Module.IsInterface(typeName):
		goType = emit.MangleIdent(typeName)
	case l.Module.IsEnum(typeName):
		goType = emit.MangleIdent(typeName)
	default:
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("unknown type %q on the right of instanceof", typeName),
			"register the type with class/enum/interface declarations earlier")
		l.w.WriteString("false")
		return
	}
	l.w.WriteString("func() bool { _, __ok := any(")
	l.lowerExpression(e.Left)
	l.w.WriteString(").(")
	l.w.WriteString(goType)
	l.w.WriteString("); return __ok }()")
}

func (l *Lowerer) isCollectionOperand(expr ast.Expression) bool {
	ty := l.inferExpressionType(expr)
	if ty == nil {
		return false
	}
	switch ty.Kind {
	case types.KindList, types.KindSet, types.KindDict, types.KindBytes:
		return true
	}
	return false
}

func goOperator(op string) string {
	switch op {
	case "+", "-", "*", "/", "%",
		"==", "!=", "<", ">", "<=", ">=",
		"&&", "||":
		return op
	}
	return ""
}

// lowerNullableEquality lowers ==/!= when an operand is a nullable value-type
// pointer (?int/?float/?bool/?string/?enum), with a nil-safe deref so the
// comparison matches the interpreter (null equals only null). The value operand
// is coerced to the pointer's element type. Returns false to defer to the plain
// path (including when an operand is the null literal, where ptr == nil is fine).
func (l *Lowerer) lowerNullableEquality(e *ast.InfixExpression) bool {
	if isNullLiteral(e.Left) || isNullLiteral(e.Right) {
		return false
	}
	lt := l.inferExpressionType(e.Left)
	rt := l.inferExpressionType(e.Right)
	lp := nullableValuePtr(lt)
	rp := nullableValuePtr(rt)
	if !lp && !rp {
		return false
	}
	eq := e.Operator == "=="
	l.w.WriteString("func() bool { __l := ")
	if rp && !lp {
		elem := *rt
		elem.Nullable = false
		l.lowerExpressionAsElem(&elem, e.Left)
	} else {
		l.lowerExpression(e.Left)
	}
	l.w.WriteString("; __r := ")
	if lp && !rp {
		elem := *lt
		elem.Nullable = false
		l.lowerExpressionAsElem(&elem, e.Right)
	} else {
		l.lowerExpression(e.Right)
	}
	l.w.WriteString("; return ")
	switch {
	case lp && rp:
		if eq {
			l.w.WriteString("(__l == nil) == (__r == nil) && (__l == nil || *__l == *__r)")
		} else {
			l.w.WriteString("(__l == nil) != (__r == nil) || (__l != nil && *__l != *__r)")
		}
	case lp:
		if eq {
			l.w.WriteString("__l != nil && *__l == __r")
		} else {
			l.w.WriteString("__l == nil || *__l != __r")
		}
	default:
		if eq {
			l.w.WriteString("__r != nil && __l == *__r")
		} else {
			l.w.WriteString("__r == nil || __l != *__r")
		}
	}
	l.w.WriteString(" }()")
	return true
}

func (l *Lowerer) lowerNullCoalesce(e *ast.InfixExpression) {
	leftTy := l.inferExpressionType(e.Left)
	if leftTy != nil && !leftTy.Nullable && isNonNullableKind(leftTy.Kind) {
		l.lowerExpression(e.Left)
		return
	}
	if nullableValuePtr(leftTy) {
		// Deref the pointer when non-nil; result is the underlying value type.
		elem := *leftTy
		elem.Nullable = false
		goElem := types.ToGo(&elem, l.Module.IntMode)
		l.Module.AddTypeImports(goElem)
		l.w.WriteString("func() ")
		l.w.WriteString(goElem.Source)
		l.w.WriteString(" { __x := ")
		l.lowerExpression(e.Left)
		l.w.WriteString("; if __x != nil { return *__x }; return ")
		l.withExpectedType(&elem, func() { l.lowerExpression(e.Right) })
		l.w.WriteString(" }()")
		return
	}
	l.w.WriteString("func() any { __x := ")
	l.lowerExpression(e.Left)
	l.w.WriteString("; if __x != nil { return __x }; return ")
	l.lowerExpression(e.Right)
	l.w.WriteString(" }()")
}

func elemUnresolved(t *types.Type) bool {
	return t == nil || t.Kind == types.KindAny || t.Kind == types.KindUnknown
}

// collectionElemUnresolved reports a list/dict whose element/value type is
// unresolved, so a more specific scope binding should be preferred.
func collectionElemUnresolved(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindList, types.KindSet:
		return elemUnresolved(t.Elem)
	case types.KindDict:
		return elemUnresolved(t.Value)
	}
	return false
}

func isNonNullableKind(k types.Kind) bool {
	switch k {
	case types.KindInt, types.KindFloat, types.KindBool, types.KindString, types.KindBytes, types.KindDecimal:
		return true
	}
	return false
}

func magicMethodForOperator(op string) (string, bool) {
	switch op {
	case "+":
		return "__add", true
	case "-":
		return "__sub", true
	case "*":
		return "__mul", true
	case "/":
		return "__div", true
	case "%":
		return "__mod", true
	case "<":
		return "__lt", true
	case ">":
		return "__gt", true
	case "<=":
		return "__lte", true
	case ">=":
		return "__gte", true
	case "==":
		return "__eq", true
	}
	return "", false
}

func (l *Lowerer) lowerCall(e *ast.CallExpression) {
	if id, ok := e.Callee.(*ast.Identifier); ok && id.Value == "parent" && l.inConstructor && l.parentClass != "" {
		if l.currentClassIface != "" {
			// Build the embedded parent impl from its interface constructor.
			l.w.WriteString("this." + implName(l.parentClass) + " = New" + l.parentClass)
			l.emitCallArgs("New"+l.parentClass, e.Arguments, e.Token, "parent()")
			l.w.WriteString(".(*" + implName(l.parentClass) + ")")
			return
		}
		l.w.WriteString("this.")
		l.w.WriteString(l.parentClass)
		l.w.WriteString(" = New")
		l.w.WriteString(l.parentClass)
		l.emitCallArgs("New"+l.parentClass, e.Arguments, e.Token, "parent()")
		return
	}
	// parent.method(): static super-call to the embedded parent impl.
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok && !sel.Optional {
		if base, ok := sel.Object.(*ast.Identifier); ok && base.Value == "parent" && l.currentClassIface != "" && l.parentClass != "" {
			l.w.WriteString("this." + implName(l.parentClass) + "." + emit.MangleIdent(sel.Name.Value))
			l.emitPositionalArgs(e.Arguments)
			return
		}
	}
	// this.method(): virtual dispatch through self so overrides late-bind.
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok && !sel.Optional {
		if base, ok := sel.Object.(*ast.Identifier); ok && base.Value == "this" &&
			l.currentClassIface != "" && l.isVisibleMethod(l.currentClassGb, sel.Name.Value) {
			l.w.WriteString("this.self." + emit.MangleIdent(sel.Name.Value))
			l.emitMethodArgs(l.currentClassIface, sel.Name.Value, e.Arguments, sel.Token)
			return
		}
	}
	if id, ok := e.Callee.(*ast.Identifier); ok && isBareBuiltin(id.Value) {
		l.lowerBareBuiltin(id, e)
		return
	}
	if id, ok := e.Callee.(*ast.Identifier); ok && l.isBuiltinErrorConstructor(id.Value) {
		l.lowerBuiltinErrorConstructor(id.Value, e)
		return
	}
	if id, ok := e.Callee.(*ast.Identifier); ok {
		if target, ok := l.Module.FromImport(id.Value); ok {
			l.lowerFromImportedCall(target, e)
			return
		}
	}
	if id, ok := e.Callee.(*ast.Identifier); ok && l.Module.IsClass(id.Value) {
		l.w.WriteString("New")
		l.w.WriteString(emit.MangleIdent(id.Value))
		l.emitTypeArguments(e.TypeArguments)
		l.emitCallArgs("New"+emit.MangleIdent(id.Value), e.Arguments, e.Token, "this constructor")
		return
	}
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok && sel.Optional {
		l.lowerOptionalCall(sel, e)
		return
	}
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok {
		if base, ok := sel.Object.(*ast.Identifier); ok {
			if l.Module.IsStdlibModule(base.Value) {
				l.lowerNativeCall(base.Value, sel.Name.Value, e.Arguments, sel)
				return
			}
			if prefix, ok := l.Module.UserModulePrefix(base.Value); ok {
				l.lowerUserModuleCall(prefix, sel.Name.Value, e.Arguments)
				return
			}
			if l.Module.IsClass(base.Value) {
				key := emit.MangleIdent(base.Value) + "_" + emit.MangleIdent(sel.Name.Value)
				l.w.WriteString(key)
				l.emitCallArgs(key, e.Arguments, sel.Token, "this static method")
				return
			}
			if l.Module.IsTaggedVariant(base.Value, sel.Name.Value) {
				l.w.WriteString("New")
				l.w.WriteString(emit.MangleIdent(base.Value))
				l.w.WriteString(emit.MangleIdent(sel.Name.Value))
				if !l.requirePositionalArgs(e.Arguments, sel.Token, "an enum variant constructor") {
					l.w.WriteString("()")
					return
				}
				l.emitPositionalArgs(e.Arguments)
				return
			}
			if l.Module.IsEnum(base.Value) {
				switch sel.Name.Value {
				case "values":
					l.w.WriteString(emit.MangleIdent(base.Value))
					l.w.WriteString("Values()")
					return
				case "fromName":
					l.w.WriteString(emit.MangleIdent(base.Value))
					l.w.WriteString("FromName")
					if !l.requirePositionalArgs(e.Arguments, sel.Token, "fromName") {
						l.w.WriteString("()")
						return
					}
					l.emitPositionalArgs(e.Arguments)
					return
				}
				l.errAt(sel.Token.Line, sel.Token.Column,
					fmt.Sprintf("geblang build --native does not support enum static call %s.%s", base.Value, sel.Name.Value),
					"only values() and fromName() are available on an enum type")
				l.w.WriteString("nil")
				return
			}
		}
		receiverTy := l.inferExpressionType(sel.Object)
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.RePatternName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a re.Pattern method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerRePatternMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.URLValueName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a url.URL method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerURLValueMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.TemplateValueName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a template.Template method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerTemplateValueMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.TemplateEngineName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a template.Engine method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerTemplateEngineMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeInstantName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a datetime.Instant method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerDateTimeInstantMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeDurationName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a datetime.Duration method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerDateTimeDurationMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeZoneName {
			if !l.requirePositionalArgs(e.Arguments, sel.Token, "a datetime.Zone method") {
				l.w.WriteString("nil")
				return
			}
			l.lowerDateTimeZoneMethod(sel, e.Arguments)
			return
		}
		if receiverTy != nil && receiverTy.Kind != types.KindUnknown {
			if l.tryLowerBuiltinMethod(sel, receiverTy, e.Arguments) {
				return
			}
			if receiverTy.Kind == types.KindAny {
				l.lowerAnyMethodCall(sel, e)
				return
			}
			// A built-in method on a primitive/collection receiver that has no
			// lowering would otherwise emit raw Go; diagnose so it fails loud.
			if isBuiltinReceiverKind(receiverTy.Kind) {
				l.errAt(sel.Token.Line, sel.Token.Column,
					fmt.Sprintf("the transpiler does not yet support method %q on a %s value", sel.Name.Value, kindName(receiverTy.Kind)),
					"build with 'geblang build' for the bundled VM binary")
				l.w.WriteString("nil")
				return
			}
		} else if (receiverTy == nil || receiverTy.Kind == types.KindUnknown) &&
			!l.isResolvableMethodReceiver(sel) {
			// No silent invalid Go: an unresolved receiver would emit raw .method().
			l.errAt(sel.Token.Line, sel.Token.Column,
				"the transpiler cannot resolve the type of the receiver for method '"+sel.Name.Value+"'",
				"annotate the receiver's type so --native can route the call, or use 'geblang build' for the VM binary")
			l.w.WriteString("nil")
			return
		}
	}
	l.lowerExpression(e.Callee)
	l.emitTypeArguments(e.TypeArguments)
	calleeKey := ""
	if id, ok := e.Callee.(*ast.Identifier); ok {
		calleeKey = l.qualifiedTopLevelName(id.Value)
	}
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok && !sel.Optional {
		if recv := l.inferExpressionType(sel.Object); recv != nil {
			if key := methodCalleeKey(recv.Name, sel.Name.Value); recv.Name != "" {
				if _, known := l.Module.CalleeParams(key); known {
					calleeKey = key
				}
			}
		}
	}
	l.emitCallArgs(calleeKey, e.Arguments, e.Token, "this call")
}

// anyHofPredKind maps a supported higher-order method on an any receiver to the
// Go return kind its any-typed callback must lower to: a predicate (func(any)
// bool) or a mapper/fold (func(any) any). The runtime dispatcher asserts the
// matching signature, so a callback that lowers otherwise is diagnosed here.
var anyHofPredKind = map[string]types.Kind{
	"map":      types.KindAny,
	"flatMap":  types.KindAny,
	"reduce":   types.KindAny,
	"filter":   types.KindBool,
	"find":     types.KindBool,
	"findLast": types.KindBool,
	"any":      types.KindBool,
	"all":      types.KindBool,
	"count":    types.KindBool,
	// *By family: key selectors lower to func(any) any, partition to func(any) bool.
	"sortBy":    types.KindAny,
	"groupBy":   types.KindAny,
	"maxBy":     types.KindAny,
	"minBy":     types.KindAny,
	"sumBy":     types.KindAny,
	"averageBy": types.KindAny,
	"uniqueBy":  types.KindAny,
	"partition": types.KindBool,
}

// anyHofMethods are the remaining closure-taking methods still unsupported on an
// any receiver; they diagnose and hint to cast the receiver to a concrete list
// type first.
var anyHofMethods = map[string]bool{
	"takeWhile": true, "dropWhile": true, "indexBy": true, "scan": true,
	"zipWith": true, "containsBy": true, "differenceBy": true,
	"intersectionBy": true, "topBy": true, "binarySearchBy": true,
}

// lowerAnyMethodCall routes a method call on an any-typed receiver through the
// runtime dispatcher; the result is any so chaining composes. Spread/named args
// diagnose; supported HOFs route when their callback is any-typed, else diagnose.
func (l *Lowerer) lowerAnyMethodCall(sel *ast.SelectorExpression, e *ast.CallExpression) {
	if retKind, ok := anyHofPredKind[sel.Name.Value]; ok {
		l.lowerAnyHofMethodCall(sel, e, retKind)
		return
	}
	if anyHofMethods[sel.Name.Value] {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler does not support the higher-order method '"+sel.Name.Value+"' on an any-typed value",
			"cast the receiver to a concrete list/dict type first (e.g. 'x as list<...>'), or build with 'geblang build' for the VM binary")
		l.w.WriteString("nil")
		return
	}
	if !l.requirePositionalArgs(e.Arguments, sel.Token, "a method call on an any-typed value") {
		l.w.WriteString("nil")
		return
	}
	l.emitCallMethod(sel, e.Arguments)
}

// lowerAnyHofMethodCall routes a supported HOF on an any receiver through the
// dispatcher when its callback lowers to the asserted any-typed Go signature; a
// concrete-typed callback (which would lower to a non-assertable func) diagnoses.
func (l *Lowerer) lowerAnyHofMethodCall(sel *ast.SelectorExpression, e *ast.CallExpression, retKind types.Kind) {
	if !l.requirePositionalArgs(e.Arguments, sel.Token, "a method call on an any-typed value") {
		l.w.WriteString("nil")
		return
	}
	if len(e.Arguments) == 0 || !l.anyTypedCallback(e.Arguments[0].Value, retKind) {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler needs an any-typed callback for '"+sel.Name.Value+"' on an any value",
			"use an any-typed callback (e.g. func(any x): "+anyCallbackReturnHint(retKind)+") when mapping over an any value, or cast the receiver to list<...> first")
		l.w.WriteString("nil")
		return
	}
	l.emitCallMethod(sel, e.Arguments)
}

// anyTypedCallback reports whether cb is a function literal whose parameters all
// lower to Go `any` and whose return lowers to wantRet, i.e. the closure becomes
// the `func(any) any`/`func(any) bool` the runtime dispatcher type-asserts.
func (l *Lowerer) anyTypedCallback(cb ast.Expression, wantRet types.Kind) bool {
	fn, ok := cb.(*ast.FunctionLiteral)
	if !ok || fn.Async {
		return false
	}
	for _, p := range fn.Parameters {
		if p.Variadic || !isAnyParamType(p.Type) {
			return false
		}
	}
	return l.resolveTypeRef(fn.ReturnType).Kind == wantRet
}

// isAnyParamType reports whether a closure parameter lowers to Go `any`: an
// explicit `any` annotation or an unannotated parameter (KindUnknown -> any).
func isAnyParamType(t *ast.TypeRef) bool {
	return t == nil || t.Name == "any"
}

func anyCallbackReturnHint(retKind types.Kind) string {
	if retKind == types.KindBool {
		return "bool"
	}
	return "any"
}

// emitCallMethod emits the transpilert.CallMethod dispatch for an any-receiver
// method, boxing each argument (a closure boxes as a Go func value) into []any.
func (l *Lowerer) emitCallMethod(sel *ast.SelectorExpression, args []ast.CallArgument) {
	// CallMethod can panic a runtime *Error (unknown method, bad arg, a bad cast
	// inside a callback); install the top-level renderer so an uncaught one
	// renders like the interpreter instead of a raw Go panic.
	l.requireUncaughtHandler()
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.CallMethod(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.w.WriteString(strconv.Quote(sel.Name.Value))
	l.w.WriteString(", []any{")
	for i, a := range args {
		if i > 0 {
			l.w.WriteString(", ")
		}
		l.lowerExpression(a.Value)
	}
	l.w.WriteString("})")
}

// emitMethodArgs emits a method call's args, applying defaults/spread when the
// method's signature is registered, else falling back to positional.
func (l *Lowerer) emitMethodArgs(classMangled, method string, args []ast.CallArgument, tok token.Token) {
	key := methodCalleeKey(classMangled, method)
	if _, ok := l.Module.CalleeParams(key); ok {
		l.emitCallArgs(key, args, tok, "this method")
		return
	}
	if !l.requirePositionalArgs(args, tok, "this method") {
		l.w.WriteString("()")
		return
	}
	l.emitPositionalArgs(args)
}

func (l *Lowerer) emitTypeArguments(args []*ast.TypeRef) {
	if len(args) == 0 {
		return
	}
	l.w.WriteString("[")
	for i, t := range args {
		if i > 0 {
			l.w.WriteString(", ")
		}
		goTy := types.ToGo(l.resolveTypeRef(t), l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		l.w.WriteString(goTy.Source)
	}
	l.w.WriteString("]")
}

// lowerFromImportedCall lowers a call to a from-imported symbol, dispatching to
// the native bridge (stdlib) or the prefixed user-module function.
func (l *Lowerer) lowerFromImportedCall(target FromImportTarget, e *ast.CallExpression) {
	if target.IsStdlib {
		sel := &ast.SelectorExpression{Token: e.Token, Name: &ast.Identifier{Value: target.Name}}
		l.lowerNativeCall(target.Module, target.Name, e.Arguments, sel)
		return
	}
	prefix := UserModulePrefixFor(target.Module)
	key := prefix + emit.MangleIdent(target.Name)
	l.w.WriteString(key)
	l.emitCallArgs(key, e.Arguments, e.Token, "a from-imported call")
}

func (l *Lowerer) lowerUserModuleCall(prefix, name string, args []ast.CallArgument) {
	key := prefix + emit.MangleIdent(name)
	l.w.WriteString(key)
	l.emitCallArgs(key, args, token.Token{}, "a cross-module call")
}

func paramNames(params []ast.Parameter) []string {
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = p.Name.Value
	}
	return out
}

func paramDefaults(params []ast.Parameter) []ast.Expression {
	out := make([]ast.Expression, len(params))
	for i, p := range params {
		out[i] = p.Default
	}
	return out
}

func lastVariadic(params []ast.Parameter) bool {
	return len(params) > 0 && params[len(params)-1].Variadic
}

func callArgsToken(args []ast.CallArgument, fallback token.Token) token.Token {
	for _, a := range args {
		if t := exprToken(a.Value); t.Line != 0 || t.Column != 0 {
			return t
		}
	}
	return fallback
}

func (l *Lowerer) requirePositionalArgs(args []ast.CallArgument, fallback token.Token, what string) bool {
	for _, a := range args {
		if a.Spread {
			tok := callArgsToken(args, fallback)
			l.errAt(tok.Line, tok.Column,
				"the transpiler does not yet support spread arguments in "+what,
				"this needs runtime support deferred to a later phase")
			return false
		}
		if a.Name != nil {
			tok := callArgsToken(args, fallback)
			l.errAt(tok.Line, tok.Column,
				"the transpiler does not yet support named arguments to "+what,
				"the callee's parameter order is not statically known here")
			return false
		}
	}
	return true
}

func (l *Lowerer) emitArgsKnown(calleeKey string, args []ast.CallArgument, params []string, tok token.Token) {
	variadic := l.Module.CalleeVariadic(calleeKey)
	defaults := l.Module.CalleeDefaults(calleeKey)

	// A trailing spread `f(...xs)` passes the slice straight into a variadic
	// Go param; a spread into a non-variadic callee has no static lowering.
	if i, ok := spreadArgIndex(args); ok {
		if !variadic || i != len(args)-1 || len(args) != len(params) {
			l.errAt(tok.Line, tok.Column,
				"the transpiler supports a spread argument only as the final argument to a variadic function",
				"")
			l.w.WriteString("()")
			return
		}
		l.w.WriteString("(")
		for j := 0; j < i; j++ {
			l.lowerExpression(args[j].Value)
			l.w.WriteString(", ")
		}
		l.lowerExpression(args[i].Value)
		l.w.WriteString("...)")
		return
	}

	hasNamed := false
	for _, a := range args {
		if a.Name != nil {
			hasNamed = true
			break
		}
	}
	if !hasNamed && !variadic && len(args) >= len(params) {
		l.emitPositionalArgs(args)
		return
	}

	// fixed counts the non-variadic leading params; a variadic last param
	// absorbs the remaining positional args.
	fixed := len(params)
	if variadic {
		fixed--
	}
	ordered := make([]ast.Expression, fixed)
	var tail []ast.Expression
	pos := 0
	for _, a := range args {
		if a.Name == nil {
			if pos < fixed {
				ordered[pos] = a.Value
			} else if variadic {
				tail = append(tail, a.Value)
			} else {
				l.errAt(tok.Line, tok.Column, "too many positional arguments", "")
				l.w.WriteString("()")
				return
			}
			pos++
			continue
		}
		idx := indexOfString(params, a.Name.Value)
		if idx < 0 || idx >= fixed {
			l.errAt(tok.Line, tok.Column,
				fmt.Sprintf("unknown named argument %q", a.Name.Value), "")
			l.w.WriteString("()")
			return
		}
		ordered[idx] = a.Value
	}
	for i := range ordered {
		if ordered[i] == nil {
			if i < len(defaults) && defaults[i] != nil {
				ordered[i] = defaults[i]
				continue
			}
			l.errAt(tok.Line, tok.Column,
				fmt.Sprintf("missing argument for parameter %q", params[i]), "")
			l.w.WriteString("()")
			return
		}
	}
	l.w.WriteString("(")
	emitted := 0
	for _, expr := range append(append([]ast.Expression{}, ordered...), tail...) {
		if emitted > 0 {
			l.w.WriteString(", ")
		}
		l.lowerExpression(expr)
		emitted++
	}
	l.w.WriteString(")")
}

func spreadArgIndex(args []ast.CallArgument) (int, bool) {
	for i, a := range args {
		if a.Spread {
			return i, true
		}
	}
	return 0, false
}

func (l *Lowerer) emitPositionalArgs(args []ast.CallArgument) {
	l.w.WriteString("(")
	for i, a := range args {
		if i > 0 {
			l.w.WriteString(", ")
		}
		l.lowerExpression(a.Value)
	}
	l.w.WriteString(")")
}

func (l *Lowerer) emitCallArgs(calleeKey string, args []ast.CallArgument, tok token.Token, what string) {
	if params, ok := l.Module.CalleeParams(calleeKey); ok {
		l.emitArgsKnown(calleeKey, args, params, tok)
		return
	}
	if !l.requirePositionalArgs(args, tok, what) {
		l.w.WriteString("()")
		return
	}
	l.emitPositionalArgs(args)
}

func indexOfString(xs []string, target string) int {
	for i, x := range xs {
		if x == target {
			return i
		}
	}
	return -1
}

func (l *Lowerer) lowerNativeCall(module, fn string, args []ast.CallArgument, sel *ast.SelectorExpression) {
	module = l.Module.StdlibCanonical(module)
	if !l.requirePositionalArgs(args, sel.Token, "a native stdlib call") {
		l.w.WriteString("nil")
		return
	}
	if module == "collections" {
		if l.lowerCollectionsFreeFn(fn, args, sel) {
			return
		}
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("the transpiler does not yet support collections.%s", fn),
			"build with 'geblang build' for the bundled VM binary")
		l.w.WriteString("nil")
		return
	}
	entry, ok := l.Bridge.Lookup(module, fn)
	if !ok {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("no transpiler bridge for %s.%s", module, fn),
			"add a NativeBridge.Register entry for this stdlib function")
		l.w.WriteString("nil")
		return
	}
	for _, imp := range entry.Imports {
		l.Module.AddImport(imp)
	}
	plain := make([]ast.Expression, 0, len(args))
	for _, a := range args {
		plain = append(plain, a.Value)
	}
	if entry.Emit != nil {
		entry.Emit(plain, &EmitContext{Writer: l.w, Module: l.Module, Lower: l.lowerExpression, AsFloat: l.emitAsFloat, Display: l.lowerDisplay})
		return
	}
	l.w.WriteString(entry.GoFunc)
	l.w.WriteString("(")
	for i, a := range plain {
		if i > 0 {
			l.w.WriteString(", ")
		}
		l.lowerExpression(a)
	}
	l.w.WriteString(")")
}

func (l *Lowerer) emitAsFloat(expr ast.Expression) {
	src := l.inferExpressionType(expr)
	if src != nil && src.Kind == types.KindDecimal {
		l.Module.AddImport("math/big")
		l.Module.RequireHelper("gbDecimalToFloat")
		l.w.WriteString("gbDecimalToFloat(")
		l.lowerExpression(expr)
		l.w.WriteString(")")
		return
	}
	l.w.WriteString("float64(")
	l.lowerExpression(expr)
	l.w.WriteString(")")
}

// isHierarchyFieldAccess reports whether obj.name reads a field through a
// hierarchy-class interface value (so it must go through a getter). Access via
// `this` stays direct: the method receiver is the concrete impl struct.
func (l *Lowerer) isHierarchyFieldAccess(obj ast.Expression, name string) bool {
	if id, ok := obj.(*ast.Identifier); ok && id.Value == "this" {
		return false
	}
	ty := l.inferExpressionType(obj)
	if ty == nil || ty.Kind != types.KindInterface {
		return false
	}
	return l.Module.ClassHasField(ty.Name, name) && l.Module.InClassHierarchy(ty.Name)
}

func (l *Lowerer) lowerSelector(e *ast.SelectorExpression) {
	if base, ok := e.Object.(*ast.Identifier); ok && l.Module.IsEnum(base.Value) {
		switch {
		case !l.Module.EnumHasVariant(base.Value, e.Name.Value):
			l.errAt(e.Token.Line, e.Token.Column,
				fmt.Sprintf("the transpiler does not support %s.%s as a value", base.Value, e.Name.Value),
				"call it instead, e.g. "+base.Value+"."+e.Name.Value+"(...)")
			l.w.WriteString("nil")
		case l.Module.IsScalarEnum(base.Value):
			l.w.WriteString(emit.MangleIdent(base.Value) + emit.MangleIdent(e.Name.Value))
		default:
			// Tagged-enum variant as a value: a nullary one is constructed; a
			// fielded one cannot be reached without its arguments.
			if arity, _ := l.Module.TaggedVariantArity(base.Value, e.Name.Value); arity > 0 {
				l.errAt(e.Token.Line, e.Token.Column,
					fmt.Sprintf("enum variant %s.%s carries fields; construct it with a call", base.Value, e.Name.Value),
					"e.g. "+base.Value+"."+e.Name.Value+"(...)")
				l.w.WriteString("nil")
			} else {
				l.w.WriteString("New" + emit.MangleIdent(base.Value) + emit.MangleIdent(e.Name.Value) + "()")
			}
		}
		return
	}
	if e.Optional {
		l.lowerOptionalSelector(e)
		return
	}
	if recv := l.inferExpressionType(e.Object); recv == errorBindingType && e.Name.Value == "message" {
		l.lowerExpression(e.Object)
		l.w.WriteString(".Message")
		return
	}
	if l.isHierarchyFieldAccess(e.Object, e.Name.Value) {
		l.lowerExpression(e.Object)
		l.w.WriteString("." + fieldGetter(e.Name.Value) + "()")
		return
	}
	// A property on a primitive/collection receiver (e.g. list.length) has no Go
	// struct field; emitting obj.name would be invalid Go. Diagnose instead.
	if recv := l.inferExpressionType(e.Object); recv != nil && isBuiltinReceiverKind(recv.Kind) {
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("the transpiler does not support property %q on a %s value", e.Name.Value, kindName(recv.Kind)),
			"call the equivalent method (e.g. .length()), or build with 'geblang build' for the bundled VM binary")
		l.w.WriteString("nil")
		return
	}
	l.lowerExpression(e.Object)
	l.w.WriteString(".")
	l.w.WriteString(emit.MangleIdent(e.Name.Value))
}

func (l *Lowerer) lowerOptionalSelector(e *ast.SelectorExpression) {
	l.w.WriteString("func() any { __r := ")
	l.lowerExpression(e.Object)
	l.w.WriteString("; if __r == nil { return nil }; return __r.")
	l.w.WriteString(emit.MangleIdent(e.Name.Value))
	l.w.WriteString(" }()")
}

func (l *Lowerer) lowerCast(e *ast.CastExpression) {
	target := l.resolveTypeRef(e.Type)
	source := l.inferExpressionType(e.Value)
	fromAny := source != nil && source.Kind == types.KindAny
	switch target.Kind {
	case types.KindString:
		if fromAny {
			l.emitAsHelper("AsString", e.Value)
			return
		}
		if source != nil && source.Kind == types.KindDecimal {
			l.lowerExpression(e.Value)
			l.w.WriteString(".FloatString(10)")
			return
		}
		l.Module.AddImport("fmt")
		l.w.WriteString(`fmt.Sprintf("%v", `)
		l.lowerExpression(e.Value)
		l.w.WriteString(")")
	case types.KindInt:
		if fromAny {
			helper := "AsIntFast"
			if l.Module.IntMode == types.IntModeBigInt {
				helper = "AsInt"
			}
			l.emitAsHelper(helper, e.Value)
			return
		}
		if source != nil && source.Kind == types.KindString {
			helper := "AsIntFast"
			if l.Module.IntMode == types.IntModeBigInt {
				helper = "AsInt"
			}
			l.emitAsHelper(helper, e.Value)
			return
		}
		if source != nil && (source.Kind == types.KindFloat || source.Kind == types.KindDecimal) {
			l.Module.RequireHelper("gbFloatToInt")
			l.w.WriteString("gbFloatToInt(")
			l.emitAsFloat(e.Value)
			l.w.WriteString(")")
			return
		}
		l.w.WriteString("int64(")
		l.lowerExpression(e.Value)
		l.w.WriteString(")")
	case types.KindFloat:
		if fromAny {
			l.emitAsHelper("AsFloat", e.Value)
			return
		}
		if source != nil && source.Kind == types.KindString {
			l.emitAsHelper("AsFloat", e.Value)
			return
		}
		l.emitAsFloat(e.Value)
	case types.KindBool:
		if fromAny {
			l.emitAsHelper("AsBool", e.Value)
			return
		}
		if source != nil && source.Kind != types.KindBool && source.Kind != types.KindUnknown {
			l.emitAsHelper("AsBool", e.Value)
			return
		}
		l.lowerExpression(e.Value)
	case types.KindDecimal:
		switch {
		case fromAny:
			l.emitAsHelper("AsDecimal", e.Value)
		case source != nil && source.Kind == types.KindDecimal:
			l.lowerExpression(e.Value)
		case source != nil && source.Kind == types.KindString:
			l.emitAsHelper("StringToDecimal", e.Value)
		case source != nil && source.Kind == types.KindInt:
			l.Module.AddImport(types.OrderedDictImport)
			l.w.WriteString("transpilert.IntToDecimal(")
			l.lowerExpression(e.Value)
			l.w.WriteString(")")
		case source != nil && source.Kind == types.KindFloat:
			l.Module.AddImport(types.OrderedDictImport)
			l.w.WriteString("transpilert.FloatToDecimal(")
			l.lowerExpression(e.Value)
			l.w.WriteString(")")
		default:
			l.errAt(e.Token.Line, e.Token.Column,
				fmt.Sprintf("cast to %s from this type is not yet supported", e.Type.String()), "")
			l.w.WriteString("nil")
		}
	case types.KindBytes:
		l.w.WriteString("[]byte(")
		l.lowerExpression(e.Value)
		l.w.WriteString(")")
	case types.KindList:
		if fromAny {
			l.emitAsHelper("AsList", e.Value)
			return
		}
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("cast to %s is not yet supported", e.Type.String()),
			"")
		l.w.WriteString("nil")
	case types.KindDict:
		if fromAny && target.Key != nil && target.Key.Kind == types.KindString {
			l.emitAsHelper("AsDict", e.Value)
			return
		}
		// Same dict shape (e.g. unwrapping ?dict<string,any> to dict<string,any>):
		// identical Go type, so the cast is a no-op on the value.
		if source != nil && source.Kind == types.KindDict && sameDictShape(source, target) {
			l.lowerExpression(e.Value)
			return
		}
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("cast to %s is not yet supported", e.Type.String()),
			"")
		l.w.WriteString("nil")
	default:
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("cast to %s is not yet supported", e.Type.String()),
			"")
		l.w.WriteString("nil")
	}
}

// sameDictShape reports whether two dict types have the same key and value
// shape (ignoring nullability), so a cast between them is a Go no-op.
func sameDictShape(a, b *types.Type) bool {
	return sameTypeShape(a.Key, b.Key) && sameTypeShape(a.Value, b.Value)
}

func sameTypeShape(a, b *types.Type) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind {
		return false
	}
	return sameTypeShape(a.Elem, b.Elem) && sameTypeShape(a.Key, b.Key) && sameTypeShape(a.Value, b.Value)
}

// emitAsHelper emits transpilert.<name>(value) for an any -> concrete cast.
func (l *Lowerer) emitAsHelper(name string, value ast.Expression) {
	// An any->concrete cast can panic a runtime *Error; ensure the top-level
	// renderer is installed so an uncaught one renders like the interpreter.
	l.requireUncaughtHandler()
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert." + name + "(")
	l.lowerExpression(value)
	l.w.WriteString(")")
}

func (l *Lowerer) lowerIndex(e *ast.IndexExpression) {
	leftTy := l.inferExpressionType(e.Left)
	if leftTy != nil && leftTy.Kind == types.KindString {
		l.Module.RequireHelper("gbStringIndex")
		l.w.WriteString("gbStringIndex(")
		l.lowerExpression(e.Left)
		l.w.WriteString(", int64(")
		l.lowerExpression(e.Index)
		l.w.WriteString("))")
		return
	}
	if leftTy != nil && leftTy.Kind == types.KindDict {
		// d[k] yields the zero value of V on a miss, matching the interpreter.
		valGo := types.ToGo(leftTy.Value, l.Module.IntMode)
		l.Module.AddTypeImports(valGo)
		l.w.WriteString("func() ")
		l.w.WriteString(valGo.Source)
		l.w.WriteString(" { __v, _ := ")
		l.lowerExpression(e.Left)
		l.w.WriteString(".Get(")
		l.lowerExpression(e.Index)
		l.w.WriteString("); return __v }()")
		return
	}
	if leftTy == nil || leftTy.Kind == types.KindAny || leftTy.Kind == types.KindUnknown {
		// Index into an any-typed receiver (e.g. a json.parse result): the
		// runtime type-switches and returns any so chained navigation composes.
		l.requireUncaughtHandler()
		l.Module.AddImport(types.OrderedDictImport)
		l.w.WriteString("transpilert.Index(")
		l.lowerExpression(e.Left)
		l.w.WriteString(", ")
		l.lowerExpression(e.Index)
		l.w.WriteString(")")
		return
	}
	l.lowerExpression(e.Left)
	l.w.WriteString("[")
	if _, isLit := e.Index.(*ast.IntegerLiteral); isLit {
		l.lowerExpression(e.Index)
	} else {
		l.w.WriteString("int(")
		l.lowerExpression(e.Index)
		l.w.WriteString(")")
	}
	l.w.WriteString("]")
}

func (l *Lowerer) emitStringLiteral(s *ast.StringLiteral) {
	l.w.WriteString(strconv.Quote(s.Value))
}

// lowerEmbeddedLiteral emits folded embed() content; binary goes through BytesFromHex to avoid raw bytes in Go source.
func (l *Lowerer) lowerEmbeddedLiteral(e *ast.EmbeddedLiteral) {
	if e.Binary {
		l.Module.AddImport(types.OrderedDictImport)
		l.w.WriteString("transpilert.BytesFromHex(")
		l.w.WriteString(strconv.Quote(hex.EncodeToString(e.Content)))
		l.w.WriteString(")")
		return
	}
	l.w.WriteString(strconv.Quote(string(e.Content)))
}

func (l *Lowerer) emitIntegerLiteral(s *ast.IntegerLiteral) {
	if l.Module.IntMode == types.IntModeBigInt {
		l.Module.AddImport(types.OrderedDictImport)
		l.w.WriteString("transpilert.FromInt64(")
		l.w.WriteString(s.Value)
		l.w.WriteString(")")
		return
	}
	l.w.WriteString(s.Value)
}

// emitFloatLiteral strips the Geblang 'f' suffix; Go float literals carry no
// suffix (underscore separators are valid Go and kept).
func (l *Lowerer) emitFloatLiteral(e *ast.FloatLiteral) {
	v := e.Value
	if len(v) > 0 && (v[len(v)-1] == 'f' || v[len(v)-1] == 'F') {
		v = v[:len(v)-1]
	}
	l.w.WriteString(v)
}

func (l *Lowerer) emitDecimalLiteral(e *ast.DecimalLiteral) {
	l.Module.AddImport("math/big")
	l.Module.RequireHelper("gbDecimalLit")
	l.w.WriteString("gbDecimalLit(")
	l.w.WriteString(strconv.Quote(strings.ReplaceAll(e.Value, "_", "")))
	l.w.WriteString(")")
}

func (l *Lowerer) lowerKeywordLiteral(e *ast.Literal) {
	switch v := e.Value.(type) {
	case bool:
		if v {
			l.w.WriteString("true")
		} else {
			l.w.WriteString("false")
		}
	case nil:
		l.w.WriteString("nil")
	default:
		l.errAt(e.Token.Line, e.Token.Column,
			fmt.Sprintf("unsupported keyword literal %v", v), "")
		l.w.WriteString("nil")
	}
}

// isResolvableMethodReceiver guards the unresolved-receiver diagnostic: every
// receiver whose type infers is handled before this point, so an uninferred one
// has no known-good fallthrough and must diagnose rather than emit raw Go. A
// bare-identifier receiver that is not a bound local is module-/class-like and
// is left to the existing fallthrough; only a value receiver whose type failed
// to infer is diagnosed.
func (l *Lowerer) isResolvableMethodReceiver(sel *ast.SelectorExpression) bool {
	if id, ok := sel.Object.(*ast.Identifier); ok {
		if _, bound := l.scope.Lookup(id.Value); !bound {
			return true
		}
	}
	return false
}
