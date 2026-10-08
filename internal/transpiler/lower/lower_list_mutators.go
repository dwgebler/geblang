package lower

import (
	"geblang/internal/ast"
	"geblang/internal/transpiler/types"
	"strings"
)

// listMutatorRecv classifies a mutator receiver. Go passes slice headers by
// value, so a caller-visible mutation needs an addressable slot to reassign.
type listMutatorRecv int

const (
	recvAddressable listMutatorRecv = iota // local/global var, this.field/obj.field, addressable index
	recvParam                              // list-typed function parameter (aliasing gap)
	recvOpaque                             // call result or complex expression
)

func (l *Lowerer) classifyListReceiver(obj ast.Expression) listMutatorRecv {
	switch o := obj.(type) {
	case *ast.Identifier:
		if b, ok := l.scope.Lookup(o.Value); ok && b.IsParam {
			return recvParam
		}
		return recvAddressable
	case *ast.SelectorExpression:
		if o.Optional {
			return recvOpaque
		}
		return l.selectorReceiverClass(o)
	case *ast.IndexExpression:
		// A list element is addressable only when its container itself is.
		if l.classifyListReceiver(o.Left) == recvAddressable {
			leftTy := l.inferExpressionType(o.Left)
			if leftTy != nil && leftTy.Kind == types.KindList {
				return recvAddressable
			}
		}
		return recvOpaque
	default:
		return recvOpaque
	}
}

// selectorReceiverClass treats this.field / obj.field as addressable; a field
// reached through a non-addressable base is opaque.
func (l *Lowerer) selectorReceiverClass(o *ast.SelectorExpression) listMutatorRecv {
	switch base := o.Object.(type) {
	case *ast.Identifier:
		if base.Value == "parent" || base.Value == "this" {
			return recvAddressable
		}
		if b, ok := l.scope.Lookup(base.Value); ok {
			if b.IsParam {
				return recvParam
			}
			return recvAddressable
		}
		return recvAddressable
	case *ast.SelectorExpression:
		return l.selectorReceiverClass(base)
	default:
		return recvOpaque
	}
}

func lowerListMutator(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	name := sel.Name.Value
	if !listMutatorArityOK(name, len(args)) {
		return false
	}
	if name == "sort" && !isOrderedElemKind(ty.Elem) {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler can only sort lists of int, float, or string without a comparator",
			"provide an ordered element type or sort in Geblang via a comparator (deferred)")
		l.w.WriteString("nil")
		return true
	}
	if name == "remove" && !isComparableElemKind(ty.Elem) {
		l.errAt(sel.Token.Line, sel.Token.Column,
			"the transpiler can only remove from lists with a comparable element type",
			"remove relies on Go equality; use removeAt with an index instead")
		l.w.WriteString("nil")
		return true
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
	l.emitListMutator(name, sel.Object, ty, args)
	return true
}

func listMutatorArityOK(name string, n int) bool {
	switch name {
	case "push", "prepend", "unshift", "removeAt", "remove":
		return n == 1
	case "pop", "shift", "reverse", "takeFirst", "takeLast":
		return n == 0
	case "takeAt":
		return n == 1
	case "insert":
		return n == 2
	case "sort":
		return n == 0 || n == 1
	case "sortBy":
		return n == 1 || n == 2
	}
	return false
}

// emitListMutator emits an IIFE taking the slot by pointer, mutating *p in
// place, and returning *p so the call yields the post-mutation receiver.
func (l *Lowerer) emitListMutator(name string, obj ast.Expression, ty *types.Type, args []ast.CallArgument) {
	elemGo := types.ToGo(ty.Elem, l.Module.IntMode)
	l.Module.AddTypeImports(elemGo)
	sliceTy := "[]" + elemGo.Source
	if strings.HasPrefix(name, "take") {
		l.emitListTake(name, obj, sliceTy, elemGo.Source, args)
		return
	}
	l.w.WriteString("func(__p *")
	l.w.WriteString(sliceTy)
	l.w.WriteString(") ")
	l.w.WriteString(sliceTy)
	l.w.WriteString(" { ")
	l.emitMutatorBody(name, sliceTy, args)
	l.w.WriteString("return *__p }(&")
	l.lowerExpression(obj)
	l.w.WriteString(")")
}

func (l *Lowerer) emitMutatorBody(name, sliceTy string, args []ast.CallArgument) {
	switch name {
	case "push":
		l.w.WriteString("*__p = append(*__p, ")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("); ")
	case "pop":
		l.w.WriteString("if len(*__p) > 0 { *__p = (*__p)[:len(*__p)-1] }; ")
	case "shift":
		l.Module.AddImport("slices")
		l.w.WriteString("if len(*__p) > 0 { *__p = slices.Delete(*__p, 0, 1) }; ")
	case "prepend", "unshift":
		l.w.WriteString("*__p = append(")
		l.w.WriteString(sliceTy)
		l.w.WriteString("{")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("}, (*__p)...); ")
	case "insert":
		// Index is clamped into [0, len] like the interpreter.
		l.w.WriteString("__i := int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("); if __i < 0 { __i = len(*__p) + __i }; if __i < 0 { __i = 0 }; if __i > len(*__p) { __i = len(*__p) }; ")
		l.Module.AddImport("slices")
		l.w.WriteString("*__p = slices.Insert(*__p, __i, ")
		l.lowerExpression(args[1].Value)
		l.w.WriteString("); ")
	case "removeAt":
		// Negative index wraps; out of range panics like the interpreter errors.
		l.w.WriteString("__i := int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("); if __i < 0 { __i += len(*__p) }; if __i < 0 || __i >= len(*__p) { panic(\"list index out of range\") }; ")
		l.Module.AddImport("slices")
		l.w.WriteString("*__p = slices.Delete(*__p, __i, __i+1); ")
	case "remove":
		// Removes the first element equal to the argument.
		l.Module.AddImport("slices")
		l.w.WriteString("if __i := slices.Index(*__p, ")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("); __i >= 0 { *__p = slices.Delete(*__p, __i, __i+1) }; ")
	case "reverse":
		l.Module.AddImport("slices")
		l.w.WriteString("slices.Reverse(*__p); ")
	case "sort":
		l.Module.AddImport("slices")
		l.w.WriteString("slices.Sort(*__p); ")
	}
}

// emitListTake emits an IIFE that removes one element from the slot and returns it.
func (l *Lowerer) emitListTake(name string, obj ast.Expression, sliceTy, elemTy string, args []ast.CallArgument) {
	l.Module.AddImport("slices")
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("func(__p *" + sliceTy + ") " + elemTy + " { ")
	switch name {
	case "takeAt":
		l.w.WriteString("__i := int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("); if __i < 0 { __i += len(*__p) }; if __i < 0 || __i >= len(*__p) { panic(transpilert.NewError(\"ValueError\", \"list.takeAt: index out of range\")) }; ")
	default:
		l.w.WriteString("if len(*__p) == 0 { panic(transpilert.NewError(\"ValueError\", \"list." + name + " on empty list\")) }; __i := 0; ")
		if name == "takeLast" {
			l.w.WriteString("__i = len(*__p) - 1; ")
		}
	}
	l.w.WriteString("__v := (*__p)[__i]; *__p = slices.Delete(*__p, __i, __i+1); return __v }(&")
	l.lowerExpression(obj)
	l.w.WriteString(")")
}

func isOrderedElemKind(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindInt, types.KindFloat, types.KindString:
		return true
	}
	return false
}

func isComparableElemKind(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindInt, types.KindFloat, types.KindString, types.KindBool:
		return true
	}
	return false
}
