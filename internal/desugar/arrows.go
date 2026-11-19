package desugar

import (
	"fmt"
	"reflect"

	"geblang/internal/ast"
	"geblang/internal/semantic"
)

// callbackShape says which arrow parameters a list<T> method's callback receives.
type callbackShape int

const (
	elementArg     callbackShape = iota // fn(T)
	elementPair                         // fn(T, T)
	elementAndPeer                      // fn(T, U), U from the other list argument
	accumulator                         // fn(A, T), A from the other argument
)

var listCallbacks = map[string]callbackShape{
	"map": elementArg, "filter": elementArg, "find": elementArg, "findLast": elementArg,
	"any": elementArg, "all": elementArg, "count": elementArg, "partition": elementArg,
	"groupBy": elementArg, "indexBy": elementArg, "sortBy": elementArg, "maxBy": elementArg,
	"minBy": elementArg, "sumBy": elementArg, "averageBy": elementArg, "topBy": elementArg,
	"binarySearchBy": elementArg, "differenceBy": elementArg, "intersectionBy": elementArg,
	"containsBy": elementArg, "flatMap": elementArg, "uniqueBy": elementArg,
	"takeWhile": elementArg, "dropWhile": elementArg,
	"sort": elementPair, "sorted": elementPair,
	"zipWith": elementAndPeer,
	"reduce":  accumulator, "scan": accumulator,
}

// Arrows gives untyped arrow parameters the element types of the list method they are passed to, and rejects any it cannot type.
func Arrows(program *ast.Program) error {
	return ArrowsWith(program, nil)
}

// ArrowsWith is Arrows with variables already bound outside program, such as earlier REPL lines.
func ArrowsWith(program *ast.Program, bound map[string]*ast.TypeRef) error {
	if len(inferredParams(program)) == 0 {
		return nil
	}
	// Each round can type a receiver that depends on an arrow typed in the previous one.
	for round := 0; round < 8; round++ {
		if !inferArrowTypes(program, semantic.ResolveExpressionTypesWith(program, bound)) {
			break
		}
	}
	for _, param := range inferredParams(program) {
		tok := param.Name.Token
		return &PositionError{Line: tok.Line, Column: tok.Column,
			Message: fmt.Sprintf("arrow parameter %s needs a type here; write (Type %s) => ...", param.Name.Value, param.Name.Value)}
	}
	return nil
}

// PositionError is a desugaring error tied to a source position.
type PositionError struct {
	Line, Column int
	Message      string
}

func (e *PositionError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}

// InferredDeclTypes is the static type of each top-level `let` written without a type, where the checker can see it.
func InferredDeclTypes(program *ast.Program, bound map[string]*ast.TypeRef) map[string]*ast.TypeRef {
	types := semantic.ResolveExpressionTypesWith(program, bound)
	out := map[string]*ast.TypeRef{}
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.DeclarationStatement)
		if !ok || decl.Type != nil || decl.Name == nil || decl.Value == nil {
			continue
		}
		if ref := typeRefFor(types[decl.Value]); ref != nil {
			out[decl.Name.Value] = ref
		}
	}
	return out
}

func inferArrowTypes(program *ast.Program, types map[ast.Expression]semantic.ExprType) bool {
	changed := false
	walkAST(program, func(node any) {
		call, ok := node.(*ast.CallExpression)
		if !ok {
			return
		}
		sel, ok := call.Callee.(*ast.SelectorExpression)
		if !ok || sel.Name == nil || sel.Optional {
			return
		}
		shape, ok := listCallbacks[sel.Name.Value]
		if !ok {
			return
		}
		recv := types[sel.Object]
		if !recv.Known || recv.Nullable || recv.Name != "list" || len(recv.Args) != 1 {
			return
		}
		elem := typeRefFor(recv.Args[0])
		for i, arg := range call.Arguments {
			fn, ok := arg.Value.(*ast.FunctionLiteral)
			if !ok || !fn.Arrow || arg.Spread {
				continue
			}
			var want []*ast.TypeRef
			switch shape {
			case elementArg:
				want = []*ast.TypeRef{elem}
			case elementPair:
				want = []*ast.TypeRef{elem, elem}
			case elementAndPeer:
				peer := otherArgType(call, i, types)
				if peer.Known && peer.Name == "list" && len(peer.Args) == 1 {
					want = []*ast.TypeRef{elem, typeRefFor(peer.Args[0])}
				} else {
					want = []*ast.TypeRef{elem, nil}
				}
			case accumulator:
				want = []*ast.TypeRef{typeRefFor(otherArgType(call, i, types)), elem}
			}
			for j := range fn.Parameters {
				if j < len(want) && want[j] != nil && fn.Parameters[j].Type.Inferred {
					fn.Parameters[j].Type = want[j]
					changed = true
				}
			}
		}
	})
	return changed
}

func otherArgType(call *ast.CallExpression, skip int, types map[ast.Expression]semantic.ExprType) semantic.ExprType {
	for i, arg := range call.Arguments {
		if i != skip && !arg.Spread && arg.Name == nil {
			return types[arg.Value]
		}
	}
	return semantic.ExprType{}
}

// typeRefFor converts a statically known type; unknown or any yields nil so the parameter stays untyped.
func typeRefFor(t semantic.ExprType) *ast.TypeRef {
	if !t.Known || t.Name == "" || t.Name == "any" || t.Name == "null" {
		return nil
	}
	ref := &ast.TypeRef{Name: t.Name, Nullable: t.Nullable}
	for _, arg := range t.Args {
		inner := typeRefFor(arg)
		if inner == nil {
			return nil
		}
		ref.Arguments = append(ref.Arguments, inner)
	}
	return ref
}

func inferredParams(program *ast.Program) []*ast.Parameter {
	var out []*ast.Parameter
	walkAST(program, func(node any) {
		fn, ok := node.(*ast.FunctionLiteral)
		if !ok || !fn.Arrow {
			return
		}
		for i := range fn.Parameters {
			if fn.Parameters[i].Type != nil && fn.Parameters[i].Type.Inferred {
				out = append(out, &fn.Parameters[i])
			}
		}
	})
	return out
}

// walkAST visits every AST node reachable from program, in source order.
func walkAST(program *ast.Program, visit func(any)) {
	tokenType := reflect.TypeOf(ast.TypeRef{}.Token)
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if v.IsNil() {
				return
			}
			if v.Kind() == reflect.Ptr && v.Elem().Kind() == reflect.Struct {
				visit(v.Interface())
			}
			walk(v.Elem())
		case reflect.Struct:
			if v.Type() == tokenType {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		}
	}
	walk(reflect.ValueOf(program))
}
