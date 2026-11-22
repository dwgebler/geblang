package evaluator

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// methodPatchFor returns the test.patch replacement and receiver for entering method fn.
func (e *Evaluator) methodPatchFor(fn runtime.Function, this *runtime.Instance) (runtime.Value, *runtime.Instance, bool) {
	owner := fn.OwnerClass
	if owner == nil || fn.Target != "method" || fn.Name == owner.Name || strings.HasPrefix(fn.Name, "~") {
		return nil, nil, false
	}
	receiver := this
	if receiver == nil && fn.Env != nil {
		if value, ok := fn.Env.Get("this"); ok {
			receiver, _ = value.(*runtime.Instance)
		}
	}
	if receiver == nil {
		return nil, nil, false
	}
	replacement, ok := runtime.ResolveMethodPatch(receiver, owner.Module, owner.Name, fn.Name)
	return replacement, receiver, ok
}

// patchTarget resolves test.patch's first argument to the class it patches and, for an object, the object.
func (e *Evaluator) patchTarget(label string, target runtime.Value) (*runtime.Class, *runtime.Instance, error) {
	switch t := target.(type) {
	case *runtime.Instance:
		if t != nil && t.Class != nil {
			return t.Class, t, nil
		}
	case *runtime.Class:
		return t, nil, nil
	case runtime.BytecodeClass:
		if e.vmDispatcher != nil {
			if class, ok := e.vmDispatcher.RuntimeClassFor(t); ok {
				return class, nil, nil
			}
		}
	}
	return nil, nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s expects a class or an object, got %s", label, target.TypeName())}
}

// testPatch(classOrObject, method, replacement) replaces an instance method until the current @test method ends.
func (e *Evaluator) testPatch(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	label := call.Callee.String()
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (classOrObject, methodName, replacement)", label)
	}
	class, instance, err := e.patchTarget(label, args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s method name must be a string, got %s", label, args[1].TypeName())}
	}
	replacement := args[2]
	if !runtime.IsCallableValue(replacement) {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s replacement must be callable, got %s", label, replacement.TypeName())}
	}
	method, found := runtime.FindPatchableMethod(class, name.Value)
	if !found {
		return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s: %s", label, missingPatchMethodReason(class, name.Value))}
	}
	if err := e.checkPatchShape(label, class, name.Value, method, replacement); err != nil {
		return nil, err
	}
	if instance != nil {
		runtime.PatchInstanceMethod(instance, name.Value, replacement)
	} else {
		runtime.PatchClassMethod(class, name.Value, replacement)
	}
	return runtime.Null{}, nil
}

func missingPatchMethodReason(class *runtime.Class, name string) string {
	lower := strings.ToLower(name)
	for c := class; c != nil; c = c.Parent {
		if len(c.StaticMethods[lower]) > 0 || len(c.StaticMetadata[lower]) > 0 {
			return fmt.Sprintf("%s.%s is a static method; only instance methods can be patched", class.Name, name)
		}
		for _, field := range c.Fields {
			if strings.EqualFold(field.Name, name) {
				return fmt.Sprintf("%s.%s is a field, not a method; assign the field instead", class.Name, name)
			}
		}
	}
	if strings.EqualFold(name, class.Name) {
		return fmt.Sprintf("%s constructors cannot be patched", class.Name)
	}
	return fmt.Sprintf("%s has no method %s", class.Name, name)
}

// checkPatchShape rejects async and generator methods and a replacement that cannot take the receiver plus every overload's arguments.
func (e *Evaluator) checkPatchShape(label string, class *runtime.Class, name string, method runtime.PatchableMethod, replacement runtime.Value) error {
	required, max, variadic, known := e.callableArity(replacement)
	for _, shape := range method.Shapes() {
		if shape.Async || shape.Generator {
			return runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s: %s.%s is an async or generator method, which cannot be patched", label, class.Name, name)}
		}
		if !known {
			continue
		}
		if required > shape.Required+1 || (!variadic && (max < shape.Max+1 || (shape.Variadic && !variadic))) {
			return runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s: the replacement for %s.%s must accept the receiver followed by %s", label, class.Name, name, describeShape(shape))}
		}
	}
	return nil
}

func describeShape(shape runtime.MethodShape) string {
	switch {
	case shape.Variadic:
		return fmt.Sprintf("at least %d argument(s) and a variadic rest", shape.Required)
	case shape.Required == shape.Max:
		return fmt.Sprintf("%d argument(s)", shape.Max)
	default:
		return fmt.Sprintf("%d to %d argument(s)", shape.Required, shape.Max)
	}
}

func (e *Evaluator) callableArity(fn runtime.Value) (required, max int, variadic, ok bool) {
	f, isFunction := fn.(runtime.Function)
	if !isFunction {
		if e.vmDispatcher != nil {
			return e.vmDispatcher.CallableArity(fn)
		}
		return 0, 0, false, false
	}
	if f.Native != nil && f.Metadata != nil {
		for _, p := range f.Metadata.Parameters {
			if p.Variadic {
				variadic = true
				continue
			}
			max++
			if !p.HasDefault {
				required++
			}
		}
		return required, max, variadic, true
	}
	if f.Native != nil || f.Bound != nil {
		return 0, 0, false, false
	}
	params := f.Parameters
	if f.OriginalParameters != nil {
		params = f.OriginalParameters
	}
	for _, p := range params {
		if p.Variadic {
			variadic = true
			continue
		}
		max++
		if p.Default == nil {
			required++
		}
	}
	return required, max, variadic, true
}

// testRestorePatch(classOrObject, method) removes one test.patch replacement.
func (e *Evaluator) testRestorePatch(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	label := call.Callee.String()
	class, instance, err := e.patchTarget(label, args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s method name must be a string, got %s", label, args[1].TypeName())}
	}
	if instance != nil {
		runtime.UnpatchInstanceMethod(instance, name.Value)
	} else {
		runtime.UnpatchClassMethod(class, name.Value)
	}
	return runtime.Null{}, nil
}
