package lower

import (
	"geblang/internal/ast"
	"geblang/internal/transpiler/types"
)

// List higher-order methods lower to transpilert generics taking the lowered
// Geblang closure as a Go func. Go infers the type parameters from the slice
// and closure, so these need no element-type annotation.

func lowerListMap(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("Map", sel, args, 1)
}

func lowerListFilter(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("Filter", sel, args, 1)
}

func lowerListFind(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("Find", sel, args, 1)
}

func lowerListFindLast(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("FindLast", sel, args, 1)
}

func lowerListAny(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("AnyMatch", sel, args, 1)
}

func lowerListAll(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("AllMatch", sel, args, 1)
}

func lowerListCount(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("Count", sel, args, 1)
}

func lowerListFlatMap(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	return l.emitHOF("FlatMap", sel, args, 1)
}

func lowerListReduce(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 2 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.Reduce(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(", ")
	l.lowerExpression(args[1].Value)
	l.w.WriteString(")")
	return true
}

func (l *Lowerer) emitHOF(goFn string, sel *ast.SelectorExpression, args []ast.CallArgument, arity int) bool {
	if len(args) != arity {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.")
	l.w.WriteString(goFn)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	for _, a := range args {
		l.w.WriteString(", ")
		l.lowerExpression(a.Value)
	}
	l.w.WriteString(")")
	return true
}

// collectionsFreeFns are the collections module functions whose first argument
// is the list receiver, lowered through the identical built-in method path.
// The set mirrors the engine's collections module members exactly so the
// transpiler never accepts a name the interpreter rejects.
var collectionsFreeFns = map[string]bool{
	"map": true, "filter": true, "reduce": true, "find": true, "findLast": true,
	"any": true, "all": true, "flatMap": true, "sorted": true,
}

// lowerCollectionsFreeFn lowers `collections.fn(xs, rest...)` by reusing the
// method-form lowering on a synthetic `xs.fn(rest...)`; returns false to let
// the caller diagnose an unsupported function rather than emit raw Go.
func (l *Lowerer) lowerCollectionsFreeFn(fn string, args []ast.CallArgument, sel *ast.SelectorExpression) bool {
	if !collectionsFreeFns[fn] || len(args) < 1 {
		return false
	}
	recvTy := l.inferExpressionType(args[0].Value)
	if recvTy == nil || recvTy.Kind != types.KindList {
		return false
	}
	mSel := &ast.SelectorExpression{Token: sel.Token, Object: args[0].Value, Name: &ast.Identifier{Value: fn}}
	return l.tryLowerBuiltinMethod(mSel, recvTy, args[1:])
}

// lowerListSort dispatches list.sort: no-arg uses the in-place natural-order
// mutator; a comparator argument routes to the closure-driven path.
func lowerListSort(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) == 0 {
		return lowerListMutator(l, sel, ty, args)
	}
	return lowerListSortCmp(l, sel, ty, args)
}

// lowerListSorted returns a sorted copy, leaving the receiver untouched; only ordered element types are supported without a comparator.
func lowerListSorted(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) == 1 && !args[0].Spread && args[0].Name == nil {
		l.Module.AddImport("slices")
		l.Module.AddImport(types.OrderedDictImport)
		elemGo := types.ToGo(ty.Elem, l.Module.IntMode)
		l.w.WriteString("func() []" + elemGo.Source + " { __c := slices.Clone(")
		l.lowerExpression(sel.Object)
		l.w.WriteString("); transpilert.SortInPlaceCmp(&__c, ")
		l.emitLessFunc(elemGo.Source, args[0].Value)
		l.w.WriteString("); return __c }()")
		return true
	}
	if len(args) != 0 {
		return false
	}
	if !isOrderedElemKind(ty.Elem) {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler can only sort lists of int, float, or string without a comparator",
			"provide an ordered element type")
		l.w.WriteString("nil")
		return true
	}
	l.Module.AddImport("slices")
	elemGo := types.ToGo(ty.Elem, l.Module.IntMode)
	l.w.WriteString("func() []")
	l.w.WriteString(elemGo.Source)
	l.w.WriteString(" { __c := slices.Clone(")
	l.lowerExpression(sel.Object)
	l.w.WriteString("); slices.Sort(__c); return __c }()")
	return true
}

// lowerListReversed returns a reversed copy, leaving the receiver untouched.
func lowerListReversed(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.Module.AddImport("slices")
	elemGo := types.ToGo(ty.Elem, l.Module.IntMode)
	l.w.WriteString("func() []")
	l.w.WriteString(elemGo.Source)
	l.w.WriteString(" { __c := slices.Clone(")
	l.lowerExpression(sel.Object)
	l.w.WriteString("); slices.Reverse(__c); return __c }()")
	return true
}

// lowerListJoin lowers list.join(sep). A list<string> joins directly with
// strings.Join; any other element type renders each with Show (matching the
// interpreter's "concatenate as strings").
func lowerListJoin(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	if ty.Elem != nil && ty.Elem.Kind == types.KindString {
		l.Module.AddImport("strings")
		l.w.WriteString("strings.Join(")
		l.lowerExpression(sel.Object)
		l.w.WriteString(", ")
		l.lowerExpression(args[0].Value)
		l.w.WriteString(")")
		return true
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.Join(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(")")
	return true
}

// lowerListSortCmp lowers sort(comparator)/sortBy(selector). Both mutate in
// place and return the receiver, so they reuse the addressable-slot guard the
// other list mutators use; an opaque or parameter receiver hard-fails.
func lowerListSortCmp(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	name := sel.Name.Value
	if name == "sortBy" {
		if len(args) != 1 && len(args) != 2 {
			return false
		}
	} else if len(args) != 1 {
		return false
	}
	switch l.classifyListReceiver(sel.Object) {
	case recvParam:
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler cannot mutate a list reached through a parameter; caller-visible mutation is unsupported",
			"Go passes slice headers by value, so the mutation would not propagate to the caller")
		l.w.WriteString("nil")
		return true
	case recvOpaque:
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler cannot mutate a list reached through a call or complex expression; the receiver has no addressable slot",
			"assign the list to a local variable first, then mutate it")
		l.w.WriteString("nil")
		return true
	}
	if name == "sortBy" {
		l.emitSortBy(sel, ty, args)
		return true
	}
	l.emitSortCmp(sel, args)
	return true
}

// emitSortCmp adapts the user comparator (bool less-than, or int three-way) to
// transpilert.SortInPlaceCmp's func(a, b T) bool.
func (l *Lowerer) emitSortCmp(sel *ast.SelectorExpression, args []ast.CallArgument) {
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.SortInPlaceCmp(&")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.emitLessFunc(types.ToGo(l.inferExpressionType(sel.Object).Elem, l.Module.IntMode).Source, args[0].Value)
	l.w.WriteString(")")
}

// emitLessFunc adapts a comparator (bool less-than, or int three-way) to func(a, b T) bool.
func (l *Lowerer) emitLessFunc(elemGo string, cmp ast.Expression) {
	if l.comparatorReturnKind(cmp) == types.KindInt {
		l.w.WriteString("func(__a, __b " + elemGo + ") bool { return (")
		l.lowerExpression(cmp)
		l.w.WriteString(")(__a, __b) < 0 }")
		return
	}
	l.lowerExpression(cmp)
}

// emitSortBy emits a natural-ordering less over the selector's key kind
// (int/float/string); other key kinds hard-fail since Go has no generic <.
func (l *Lowerer) emitSortBy(sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) {
	keyKind := l.lambdaReturnKind(args[0].Value)
	if keyKind != types.KindInt && keyKind != types.KindFloat && keyKind != types.KindString {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler can only sortBy an int, float, or string key",
			"the selector must return an ordered scalar key")
		l.w.WriteString("nil")
		return
	}
	keyGo := types.ToGo(&types.Type{Kind: keyKind}, l.Module.IntMode)
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.SortInPlaceBy(&")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(", func(__a, __b ")
	l.w.WriteString(keyGo.Source)
	l.w.WriteString(") bool { return __a < __b }, ")
	if len(args) == 2 {
		l.lowerExpression(args[1].Value)
	} else {
		l.w.WriteString("false")
	}
	l.w.WriteString(")")
}

// comparatorReturnKind reports the comparator's declared return kind (bool or
// int); defaults to bool when the closure has no explicit return type.
func (l *Lowerer) comparatorReturnKind(expr ast.Expression) types.Kind {
	if k := l.lambdaReturnKind(expr); k != types.KindUnknown {
		return k
	}
	return types.KindBool
}

func (l *Lowerer) lambdaReturnKind(expr ast.Expression) types.Kind {
	if fn, ok := expr.(*ast.FunctionLiteral); ok {
		if fn.ReturnType != nil {
			if t := l.resolveTypeRef(fn.ReturnType); t != nil {
				return t.Kind
			}
		} else if t := l.callbackReturns[fn]; t != nil {
			return t.Kind
		}
	}
	return types.KindUnknown
}
