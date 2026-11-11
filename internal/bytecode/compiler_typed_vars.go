package bytecode

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// emitTypedVariableCheck asserts the value on the stack against a typed variable's declared type unless the compiler can prove it fits.
func (c *Compiler) emitTypedVariableCheck(declRef *ast.TypeRef, value ast.Expression, line, column int) {
	if declRef == nil {
		return
	}
	c.emitTypedAssert(declRef, !isCollectionTypeRef(declRef) && value != nil && c.provenAssignable(declRef, value), line, column)
}

func (c *Compiler) emitTypedAssert(declRef *ast.TypeRef, proven bool, line, column int) {
	if declRef == nil || proven {
		return
	}
	if !isCollectionTypeRef(declRef) {
		if declRef.Operator == "" && strings.EqualFold(declRef.Name, "any") {
			return
		}
		if runtime.TypeRefUsesTypeParameter(declRef, c.inScopeTypeParams()) {
			return
		}
	}
	fullTypeName := c.bytecodeTypeNameForParam(declRef, nil)
	typeStrIdx := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: fullTypeName})
	operands := []int64{typeStrIdx}
	if display := c.declaredTypeName(declRef); display != fullTypeName {
		operands = append(operands, int64(len(c.chunk.Constants)))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: display})
	}
	c.emitAt(OpTypeAssert, line, column, operands...)
}

// parameterDeclaredType is nil for variadic parameters, whose local holds the packed list.
func (c *Compiler) parameterDeclaredType(param ast.Parameter) *ast.TypeRef {
	if param.Type == nil || param.Variadic {
		return nil
	}
	return c.resolvedDeclarationType(param.Type)
}

func isCollectionTypeRef(ref *ast.TypeRef) bool {
	if ref.Operator != "" {
		return false
	}
	if ref.ListAlias && len(ref.Arguments) == 0 {
		return true
	}
	if len(ref.Arguments) == 0 {
		return false
	}
	switch strings.ToLower(ref.Name) {
	case "list", "set", "dict":
		return true
	}
	return false
}

func (c *Compiler) inScopeTypeParams() map[string]bool {
	names := c.enclosingTypeParameters()
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = true
	}
	return set
}

// provenAssignable is stricter than staticTypeAssignable: an unknown or any actual is never proven.
func (c *Compiler) provenAssignable(target *ast.TypeRef, value ast.Expression) bool {
	targetName := c.bytecodeTypeNameForParam(target, nil)
	if isNumericLiteralTarget(targetName) && ast.IsIntegerLiteral(value) {
		return true
	}
	actual := c.provenStaticType(value)
	if actual == "" || strings.EqualFold(strings.TrimPrefix(actual, "?"), "any") || strings.ContainsAny(actual, "<[") {
		return false
	}
	if strings.ContainsAny(targetName, "<[") {
		return false
	}
	return c.staticTypeAssignable(targetName, actual)
}

func isNumericLiteralTarget(target string) bool {
	switch strings.ToLower(strings.TrimPrefix(target, "?")) {
	case "int", "float", "decimal":
		return true
	}
	return false
}

func (c *Compiler) provenStaticType(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.InfixExpression:
		return infixStaticResult(e.Operator, c.provenStaticType(e.Left), c.provenStaticType(e.Right))
	case *ast.PrefixExpression:
		operand := c.provenStaticType(e.Right)
		switch e.Operator {
		case "!":
			if operand == "bool" {
				return "bool"
			}
		case "-":
			if isNumericStatic(operand) {
				return operand
			}
		}
		return ""
	case *ast.CallExpression:
		if typ, ok := c.provenCallReturnType(e); ok {
			return typ
		}
		if typ := c.provenPrimitiveMethodType(e); typ != "" {
			return typ
		}
	}
	return c.expressionStaticType(expr)
}

func isNumericStatic(typ string) bool {
	return typ == "int" || typ == "float" || typ == "decimal"
}

func isPrimitiveStatic(typ string) bool {
	return isNumericStatic(typ) || typ == "string" || typ == "bool"
}

func infixStaticResult(op, left, right string) string {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=":
		if isPrimitiveStatic(left) && isPrimitiveStatic(right) {
			return "bool"
		}
		return ""
	case "&&", "||":
		if left == "bool" && right == "bool" {
			return "bool"
		}
		return ""
	case "+":
		if left == "string" && right == "string" {
			return "string"
		}
	case "%", "//":
		if left == "int" && right == "int" {
			return "int"
		}
		return ""
	case "-", "*":
	default:
		return ""
	}
	if !isNumericStatic(left) || !isNumericStatic(right) {
		return ""
	}
	switch {
	case left == right:
		return left
	case left == "int":
		return right
	case right == "int":
		return left
	}
	return ""
}

// provenCallReturnType types a same-module function call; its return value is checked at runtime.
func (c *Compiler) provenCallReturnType(call *ast.CallExpression) (string, bool) {
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok || len(call.TypeArguments) > 0 {
		return "", false
	}
	if _, shadowed := c.resolveName(ident.Value); shadowed {
		return "", false
	}
	indices := c.funcs[strings.ToLower(ident.Value)]
	if len(indices) == 0 {
		return "", false
	}
	result := ""
	for _, index := range indices {
		if index < 0 || int(index) >= len(c.chunk.Functions) {
			return "", false
		}
		fn := c.chunk.Functions[index]
		if fn.Name != ident.Value || fn.Async || fn.IsGenerator || len(fn.Decorators) > 0 || len(fn.TypeParameters) > 0 || fn.ReturnCheckType == "" {
			return "", false
		}
		if result != "" && result != fn.ReturnCheckType {
			return "", false
		}
		result = fn.ReturnCheckType
	}
	return result, true
}

// provenLoopElementType types the elements of an int range; other iterables are checked per element.
func (c *Compiler) provenLoopElementType(iterable ast.Expression) string {
	rng, ok := iterable.(*ast.RangeExpression)
	if !ok || c.provenStaticType(rng.Start) != "int" || c.provenStaticType(rng.End) != "int" {
		return ""
	}
	return "int"
}

func (c *Compiler) loopVarStaticType(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	return c.bytecodeTypeName(ref)
}

// provenPrimitiveMethodType types the inline-dispatched primitive queries on a receiver of known primitive type.
func (c *Compiler) provenPrimitiveMethodType(call *ast.CallExpression) string {
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || selector.Name == nil || selector.Optional {
		return ""
	}
	receiver := c.provenStaticType(selector.Object)
	if lt := strings.Index(receiver, "<"); lt >= 0 {
		receiver = receiver[:lt]
	}
	switch receiver {
	case "string", "list", "dict", "set", "bytes":
	default:
		return ""
	}
	switch {
	case selector.Name.Value == "length" && len(call.Arguments) == 0:
		return "int"
	case selector.Name.Value == "isEmpty" && len(call.Arguments) == 0:
		return "bool"
	case selector.Name.Value == "contains" && len(call.Arguments) == 1:
		return "bool"
	}
	return ""
}
