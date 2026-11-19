package lower

import (
	"reflect"

	"geblang/internal/ast"
	"geblang/internal/transpiler/types"
)

var predicateCallbacks = map[string]bool{
	"filter": true, "find": true, "findLast": true, "any": true, "all": true, "count": true,
	"partition": true, "takeWhile": true, "dropWhile": true, "indexBy": true,
}

var accumulatorCallbacks = map[string]bool{"reduce": true, "scan": true}

// inferCallbackReturns gives callback literals without a return type the Go result type their list method needs.
func (l *Lowerer) inferCallbackReturns(prog *ast.Program) {
	l.callbackReturns = map[*ast.FunctionLiteral]*types.Type{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if v.IsNil() {
				return
			}
			if call, ok := v.Interface().(*ast.CallExpression); ok {
				l.typeCallbacks(call)
			}
			walk(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(prog))
}

func (l *Lowerer) typeCallbacks(call *ast.CallExpression) {
	sel, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || sel.Name == nil {
		return
	}
	if recv := l.exprTypes[sel.Object]; recv == nil || recv.Kind != types.KindList {
		return
	}
	method := sel.Name.Value
	for i, arg := range call.Arguments {
		fn, ok := arg.Value.(*ast.FunctionLiteral)
		if !ok || fn.ReturnType != nil {
			continue
		}
		var result *types.Type
		switch {
		case predicateCallbacks[method]:
			result = &types.Type{Kind: types.KindBool}
		case accumulatorCallbacks[method]:
			for j, other := range call.Arguments {
				if j != i {
					result = l.exprTypes[other.Value]
				}
			}
		default:
			result = l.exprTypes[singleReturnValue(fn)]
			if (method == "sort" || method == "sorted") && (result == nil || result.Kind == types.KindUnknown || result.Kind == types.KindAny) {
				// A comparator whose result type is not visible is a less-than predicate, as comparatorReturnKind assumes.
				result = &types.Type{Kind: types.KindBool}
			}
		}
		if result != nil && result.Kind != types.KindUnknown && result.Kind != types.KindAny {
			l.callbackReturns[fn] = result
		}
	}
}

func singleReturnValue(fn *ast.FunctionLiteral) ast.Expression {
	if fn.Body == nil || len(fn.Body.Statements) != 1 {
		return nil
	}
	if ret, ok := fn.Body.Statements[0].(*ast.ReturnStatement); ok {
		return ret.Value
	}
	return nil
}
