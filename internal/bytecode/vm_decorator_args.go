package bytecode

import (
	"fmt"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

func decoratorValueHasThunk(value runtime.Value) bool {
	switch v := value.(type) {
	case runtime.DecoratorThunk:
		return true
	case *runtime.List:
		for _, el := range v.Elements {
			if decoratorValueHasThunk(el) {
				return true
			}
		}
	case runtime.Dict:
		found := false
		v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			found = decoratorValueHasThunk(entry.Key) || decoratorValueHasThunk(entry.Value)
			return !found
		})
		return found
	case runtime.Set:
		for _, entry := range v.Elements {
			if decoratorValueHasThunk(entry.Value) {
				return true
			}
		}
	}
	return false
}

func decoratorHasThunk(decorator runtime.DecoratorMetadata) bool {
	for _, arg := range decorator.Args {
		if decoratorValueHasThunk(arg) {
			return true
		}
	}
	for _, arg := range decorator.NamedArgs {
		if decoratorValueHasThunk(arg) {
			return true
		}
	}
	return false
}

// resolveDecorator returns decorator with every type or function argument evaluated in module.
func (vm *VM) resolveDecorator(decorator runtime.DecoratorMetadata, module string) (runtime.DecoratorMetadata, error) {
	if !decoratorHasThunk(decorator) {
		return decorator, nil
	}
	out := decorator
	out.Args = make([]runtime.Value, len(decorator.Args))
	for i, arg := range decorator.Args {
		value, err := vm.resolveDecoratorValue(decorator.Name, arg, module)
		if err != nil {
			return decorator, err
		}
		out.Args[i] = value
	}
	out.NamedArgs = make(map[string]runtime.Value, len(decorator.NamedArgs))
	for name, arg := range decorator.NamedArgs {
		value, err := vm.resolveDecoratorValue(decorator.Name, arg, module)
		if err != nil {
			return decorator, err
		}
		out.NamedArgs[name] = value
	}
	return out, nil
}

func (vm *VM) resolveDecoratorValue(decoratorName string, value runtime.Value, module string) (runtime.Value, error) {
	if !decoratorValueHasThunk(value) {
		return value, nil
	}
	switch v := value.(type) {
	case runtime.DecoratorThunk:
		result, err := vm.callCallable(runtime.BytecodeClosure{FunctionIndex: v.Function, Name: "<decorator argument>", Module: module}, nil)
		if err != nil {
			return nil, err
		}
		if v.Type && !isDecoratorTypeValue(result) {
			return nil, fmt.Errorf("decorator @%s: argument %s must be a literal, a type, or a function", decoratorName, v.Source)
		}
		return result, nil
	case *runtime.List:
		elements := make([]runtime.Value, len(v.Elements))
		for i, el := range v.Elements {
			resolved, err := vm.resolveDecoratorValue(decoratorName, el, module)
			if err != nil {
				return nil, err
			}
			elements[i] = resolved
		}
		return &runtime.List{Elements: elements}, nil
	case runtime.Dict:
		entries := map[string]runtime.DictEntry{}
		var failure error
		v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			key, err := vm.resolveDecoratorValue(decoratorName, entry.Key, module)
			if err != nil {
				failure = err
				return false
			}
			resolved, err := vm.resolveDecoratorValue(decoratorName, entry.Value, module)
			if err != nil {
				failure = err
				return false
			}
			entries[native.DictKey(key)] = runtime.DictEntry{Key: key, Value: resolved}
			return true
		})
		if failure != nil {
			return nil, failure
		}
		return runtime.DictFromEntries(entries, nil), nil
	case runtime.Set:
		return nil, fmt.Errorf("decorator @%s: a set cannot hold type or function arguments", decoratorName)
	}
	return value, nil
}

func (vm *VM) resolveParameterDecorators(parameter runtime.ParameterMetadata, module string) (runtime.ParameterMetadata, error) {
	if len(parameter.Decorators) == 0 {
		return parameter, nil
	}
	decorators := make([]runtime.DecoratorMetadata, len(parameter.Decorators))
	for i, dec := range parameter.Decorators {
		resolved, err := vm.resolveDecorator(dec, module)
		if err != nil {
			return parameter, err
		}
		decorators[i] = resolved
	}
	parameter.Decorators = decorators
	return parameter, nil
}

func isDecoratorTypeValue(value runtime.Value) bool {
	switch value.(type) {
	case runtime.BytecodeClass, *runtime.Class, *runtime.Interface, *runtime.EnumDef:
		return true
	}
	return false
}
