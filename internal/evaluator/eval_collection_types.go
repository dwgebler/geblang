package evaluator

import (
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"geblang/internal/typealias"
)

func collectionWriteMatches(collection runtime.Value, argument int, value runtime.Value) bool {
	if runtime.HasCollectionType(collection) {
		return runtime.CollectionWriteMatches(collection, argument, value)
	}
	tags := runtime.CollectionElementTypes(collection)
	return argument >= len(tags) || valueSatisfiesElementTag(value, tags[argument])
}

func collectionExtendMatches(collection runtime.Value, values []runtime.Value) (bool, int) {
	if runtime.HasCollectionType(collection) {
		return runtime.CollectionExtendMatches(collection, values)
	}
	for i, value := range values {
		if !collectionWriteMatches(collection, 0, value) {
			return false, i
		}
	}
	return true, 0
}

func (e *Evaluator) collectionBoundary(value runtime.Value, ref *ast.TypeRef, env *runtime.Environment, params map[string]bool) (runtime.CollectionBoundary, bool) {
	if ref == nil || runtime.HasCollectionType(value) || (!isCollectionTypeRef(ref) && ref.Operator == "") {
		return runtime.CollectionBoundary{}, false
	}
	if runtime.TypeRefUsesTypeParameter(ref, params) {
		ref = e.resolveTypeRef(typealias.Expand(ref, env.GetTypeBinding), env)
		if runtime.TypeRefUsesTypeParameter(ref, params) {
			return runtime.CollectionBoundary{}, false
		}
		if matched, handled := runtime.MatchCollectionType(value, ref, func(value runtime.Value, typ *ast.TypeRef) bool { return matchValueToTypeRef(nil, value, typ) }); handled && !matched {
			return runtime.CollectionBoundary{}, false
		}
	}
	return runtime.CollectionBoundary{Value: value, Type: ref}, true
}

func (e *Evaluator) bindCollection(value runtime.Value, ref *ast.TypeRef, env *runtime.Environment, params map[string]bool) {
	if boundary, ok := e.collectionBoundary(value, ref, env, params); ok {
		tagCollectionWithTypeRef(boundary.Value, boundary.Type)
	}
}
