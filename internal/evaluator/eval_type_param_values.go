package evaluator

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// typeParamValue evaluates an in-scope type parameter used as a value; handled is false when name is not a type parameter here.
func (e *Evaluator) typeParamValue(name string, env *runtime.Environment) (value runtime.Value, handled bool, err error) {
	owner, ok := e.typeParamOwner(name)
	if !ok {
		return nil, false, nil
	}
	bound, has := env.GetTypeBinding(name)
	if !has || bound == "" {
		return nil, true, runtime.TypeParamUnboundError(name, owner)
	}
	ref, _ := parsedBoundTypeRef(bound)
	kind, className, args := runtime.ClassifyTypeParamBinding(ref)
	switch kind {
	case runtime.TypeParamBindingBuiltin:
		return runtime.Type{Name: className}, true, nil
	case runtime.TypeParamBindingClass:
		class, found := e.classByBoundName(className, env)
		if !found {
			return nil, true, runtime.TypeParamNotClassError(name, bound)
		}
		if len(args) == 0 {
			return class, true, nil
		}
		return runtime.BoundClass{Class: class, TypeArgs: args}, true, nil
	}
	return nil, true, runtime.TypeParamNotClassError(name, bound)
}

func (e *Evaluator) typeParamOwner(name string) (string, bool) {
	if fn := e.currentFunction(); fn != nil {
		for _, param := range fn.TypeParameters {
			if param == name {
				return functionDisplayName(*fn), true
			}
		}
		for _, param := range fn.OuterTypeParameters {
			if param == name {
				return "", true
			}
		}
	}
	if n := len(e.classStack); n > 0 && e.classStack[n-1] != nil {
		for _, param := range e.classStack[n-1].TypeParameters {
			if param == name {
				return e.classStack[n-1].Name, true
			}
		}
	}
	return "", false
}

// classByBoundName resolves a binding's class name, which is spelled as at the binding call site and may belong to another module.
func (e *Evaluator) classByBoundName(name string, env *runtime.Environment) (*runtime.Class, bool) {
	if prefix, member, qualified := cutLastDot(name); qualified {
		if value, ok := env.Get(prefix); ok {
			if module, ok := value.(*runtime.Module); ok {
				if class, ok := module.Exports[member].(*runtime.Class); ok {
					return class, true
				}
			}
		}
		if module := e.moduleForPrefix(prefix, env); module != nil {
			if class, ok := module.Exports[member].(*runtime.Class); ok {
				return class, true
			}
		}
		return e.bareClass(member)
	}
	if value, ok := env.Get(name); ok {
		if class, ok := value.(*runtime.Class); ok && class.Name == name {
			return class, true
		}
	}
	return e.bareClass(strings.TrimSpace(name))
}

func boundTypeBindings(class *runtime.Class, typeArgs []string) map[string]string {
	bindings := make(map[string]string, len(typeArgs))
	for i, param := range class.TypeParameters {
		if i < len(typeArgs) {
			bindings[param] = typeArgs[i]
		}
	}
	return bindings
}

func parseBoundTypeRefOrNil(bound string) *ast.TypeRef {
	ref, _ := parsedBoundTypeRef(bound)
	return ref
}

// callWithTypeArgs gives a call on a bound class value the class's type arguments, as if written Box<int>(...).
func callWithTypeArgs(call *ast.CallExpression, typeArgs []string) *ast.CallExpression {
	out := *call
	out.TypeArguments = make([]*ast.TypeRef, len(typeArgs))
	for i, arg := range typeArgs {
		if ref := parseBoundTypeRefOrNil(arg); ref != nil {
			out.TypeArguments[i] = cloneTypeRef(ref)
		}
	}
	return &out
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func unwrapClassArgsAll(fns map[string]builtinFunc) map[string]builtinFunc {
	for name, fn := range fns {
		inner := fn
		fns[name] = func(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
			return inner(call, runtime.UnwrapClassArgs(args))
		}
	}
	return fns
}
