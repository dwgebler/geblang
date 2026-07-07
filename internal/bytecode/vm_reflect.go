package bytecode

import (
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"sort"
	"strings"
)

func (vm *VM) reflectNativeCall(fn string, args []runtime.Value) (runtime.Value, error) {
	switch fn {
	case "function", "class":
		return vm.reflectLookupNativeCall(fn, args)
	case "module":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.module expects value")
		}
		if _, ok := args[0].(runtime.Null); ok {
			return runtime.Null{}, nil
		}
		module, ok := args[0].(*runtime.Module)
		if !ok {
			return nil, fmt.Errorf("reflect.module expects module, got %s", args[0].TypeName())
		}
		return module, nil
	case "classes":
		if len(args) != 0 {
			return nil, fmt.Errorf("reflect.classes takes no arguments")
		}
		out := vm.collectChunkClasses(vm.chunk)
		if vm.moduleLoader != nil {
			out = append(out, vm.moduleLoader.ListAllClasses()...)
		}
		return &runtime.List{Elements: dedupeClassValues(out)}, nil
	case "exports":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.exports expects module")
		}
		module, ok := args[0].(*runtime.Module)
		if !ok {
			return nil, fmt.Errorf("reflect.exports expects module, got %s", args[0].TypeName())
		}
		names := make([]string, 0, len(module.Exports))
		for name := range module.Exports {
			names = append(names, name)
		}
		sort.Strings(names)
		values := make([]runtime.Value, 0, len(names))
		for _, name := range names {
			values = append(values, runtime.String{Value: name})
		}
		return &runtime.List{Elements: values}, nil
	case "method", "staticMethod":
		return vm.reflectMethodNativeCall(fn, args)
	case "decorators":
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("reflect.decorators expects value and optional decorator name")
		}
		target, ok := reflectDecoratorTarget(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.decorators expects reflect target, got %s", args[0].TypeName())
		}
		filter := ""
		if len(args) == 2 {
			name, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("reflect.decorators decorator name must be string")
			}
			filter = name.Value
		}
		values := make([]runtime.Value, 0, len(target.Decorators))
		for _, decorator := range target.Decorators {
			if filter != "" && !strings.EqualFold(decorator.Name, filter) {
				continue
			}
			values = append(values, decoratorMetadataDict(decorator))
		}
		return &runtime.List{Elements: values}, nil
	case "hasDecorator":
		if len(args) != 2 {
			return nil, fmt.Errorf("reflect.hasDecorator expects value and decorator name")
		}
		target, ok := reflectDecoratorTarget(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.hasDecorator expects reflect target, got %s", args[0].TypeName())
		}
		name, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("reflect.hasDecorator decorator name must be string")
		}
		for _, decorator := range target.Decorators {
			if strings.EqualFold(decorator.Name, name.Value) {
				return runtime.Bool{Value: true}, nil
			}
		}
		return runtime.Bool{Value: false}, nil
	case "decorator":
		if len(args) != 2 {
			return nil, fmt.Errorf("reflect.decorator expects value and decorator name")
		}
		target, ok := reflectDecoratorTarget(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.decorator expects reflect target, got %s", args[0].TypeName())
		}
		name, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("reflect.decorator decorator name must be string")
		}
		for _, decorator := range target.Decorators {
			if strings.EqualFold(decorator.Name, name.Value) {
				return decoratorMetadataDict(decorator), nil
			}
		}
		return runtime.Null{}, nil
	case "parameters":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.parameters expects value")
		}
		metadata, ok := vm.reflectFunctionMetadataValue(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.parameters expects function or method, got %s", args[0].TypeName())
		}
		values := make([]runtime.Value, 0, len(metadata.Parameters))
		for _, parameter := range metadata.Parameters {
			values = append(values, parameterMetadataDict(parameter))
		}
		return &runtime.List{Elements: values}, nil
	case "returnType":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.returnType expects value")
		}
		metadata, ok := vm.reflectFunctionMetadataValue(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.returnType expects function or method, got %s", args[0].TypeName())
		}
		return runtime.String{Value: metadata.ReturnType}, nil
	case "doc", "docs":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.%s expects value", fn)
		}
		doc, ok := vm.reflectDoc(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.%s expects function, method, class, or interface, got %s", fn, args[0].TypeName())
		}
		if doc == "" {
			return runtime.Null{}, nil
		}
		if fn == "docs" {
			return bytecodeDocMetadataDict(doc), nil
		}
		return runtime.String{Value: doc}, nil
	case "typeOf":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.typeOf expects value")
		}
		return runtime.Type{Name: args[0].TypeName()}, nil
	case "location":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.location expects value")
		}
		return vm.reflectLocation(args[0])
	case "getField":
		if len(args) != 2 {
			return nil, fmt.Errorf("reflect.getField expects (instance, fieldName)")
		}
		instance, ok := args[0].(*runtime.Instance)
		if !ok {
			return nil, fmt.Errorf("reflect.getField expects instance, got %s", args[0].TypeName())
		}
		name, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("reflect.getField field name must be string")
		}
		if v, hit := instance.Fields[name.Value]; hit {
			return v, nil
		}
		return runtime.Null{}, nil
	case "setField":
		if len(args) != 3 {
			return nil, fmt.Errorf("reflect.setField expects (instance, fieldName, value)")
		}
		instance, ok := args[0].(*runtime.Instance)
		if !ok {
			return nil, fmt.Errorf("reflect.setField expects instance, got %s", args[0].TypeName())
		}
		name, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("reflect.setField field name must be string")
		}
		if instance.Fields == nil {
			instance.Fields = map[string]runtime.Value{}
		}
		instance.Fields[name.Value] = args[2]
		return instance, nil
	case "fields", "methods", "staticMethods", "parent", "interfaces", "className":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.%s expects class", fn)
		}
		metadata, ok := vm.reflectClassMetadata(args[0])
		if !ok {
			// Fall back to built-in primitive metadata so
			// `reflect.methods([1,2,3])` and the rest of the
			// reflect API work on lists / dicts / sets / strings
			// / bytes / ranges.
			if md, primOk := vmPrimitiveTypeMetadata(args[0]); primOk {
				metadata = md
				ok = true
			}
		}
		if !ok {
			// className is total: for a primitive without class
			// metadata, return its runtime type name (symmetric
			// with how reflect.typeOf handles instances).
			if fn == "className" {
				return runtime.String{Value: args[0].TypeName()}, nil
			}
			return nil, fmt.Errorf("reflect.%s expects class, got %s", fn, args[0].TypeName())
		}
		switch fn {
		case "fields":
			return vm.reflectFieldsResult(args[0], metadata), nil
		case "methods":
			methods := append([]string(nil), metadata.Methods...)
			sort.Strings(methods)
			return bytecodeStringList(methods), nil
		case "staticMethods":
			return bytecodeStringList(metadata.StaticMethods), nil
		case "parent":
			if metadata.Parent == "" {
				return runtime.Null{}, nil
			}
			// Bare name matches the evaluator; the qualifier lives only in metadata for cross-module dispatch.
			return runtime.String{Value: bareClassName(metadata.Parent)}, nil
		case "className":
			if metadata.Name == "" {
				return runtime.Null{}, nil
			}
			return runtime.String{Value: metadata.Name}, nil
		case "interfaces":
			// Bare names match the evaluator; the module qualifier lives only in metadata for cross-module dispatch.
			bare := make([]string, len(metadata.Interfaces))
			for i, name := range metadata.Interfaces {
				bare[i] = bareClassName(name)
			}
			return bytecodeStringList(bare), nil
		}
		return nil, fmt.Errorf("unsupported native call reflect.%s", fn)
	case "constructors":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.constructors expects class")
		}
		return vm.reflectConstructors(args[0])
	case "typeBindings":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.typeBindings expects instance")
		}
		entries := map[string]runtime.DictEntry{}
		putBinding := func(name, typeName string) {
			k := runtime.String{Value: name}
			entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: typeName}}
		}
		switch v := args[0].(type) {
		case *runtime.Instance:
			for name, typeName := range v.TypeBindings {
				putBinding(name, typeName)
			}
		case *runtime.List:
			if len(v.ElementTypes) >= 1 {
				putBinding("T", elementTagBase(v.ElementTypes[0]))
			}
		case runtime.Set:
			if len(v.ElementTypes) >= 1 {
				putBinding("T", elementTagBase(v.ElementTypes[0]))
			}
		case runtime.Dict:
			if len(v.ElementTypes) >= 2 {
				putBinding("K", elementTagBase(v.ElementTypes[0]))
				putBinding("V", elementTagBase(v.ElementTypes[1]))
			}
		default:
			return nil, fmt.Errorf("reflect.typeBindings expects instance or generic collection, got %s", args[0].TypeName())
		}
		return runtime.Dict{Entries: entries}, nil
	case "interfaceMethods", "interfaceParents":
		if len(args) != 1 {
			return nil, fmt.Errorf("reflect.%s expects interface", fn)
		}
		iface, ok := vm.reflectInterfaceInfo(args[0])
		if !ok {
			return nil, fmt.Errorf("reflect.%s expects interface, got %s", fn, args[0].TypeName())
		}
		if fn == "interfaceParents" {
			parents := append([]string(nil), iface.Parents...)
			sort.Strings(parents)
			return bytecodeStringList(parents), nil
		}
		methods := append([]runtime.FunctionMetadata(nil), iface.Methods...)
		sort.Slice(methods, func(i, j int) bool {
			return strings.ToLower(methods[i].Name) < strings.ToLower(methods[j].Name)
		})
		values := make([]runtime.Value, 0, len(methods))
		for _, method := range methods {
			values = append(values, interfaceMethodMetadataDict(method))
		}
		return &runtime.List{Elements: values}, nil
	default:
		return nil, fmt.Errorf("unsupported native call reflect.%s", fn)
	}
}

func (vm *VM) reflectInterfaceInfo(value runtime.Value) (InterfaceInfo, bool) {
	switch value := value.(type) {
	case runtime.String:
		return vm.lookupInterfaceInfo(value.Value)
	default:
		return InterfaceInfo{}, false
	}
}

func (vm *VM) reflectDoc(value runtime.Value) (string, bool) {
	if metadata, ok := vm.reflectFunctionMetadataValue(value); ok {
		return metadata.Doc, true
	}
	if metadata, ok := vm.reflectClassMetadata(value); ok {
		return metadata.Doc, true
	}
	if iface, ok := vm.reflectInterfaceInfo(value); ok {
		return iface.Doc, true
	}
	return "", false
}

func (vm *VM) reflectLookupNativeCall(fn string, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("reflect.%s expects value or module and export name", fn)
	}
	value := args[0]
	if len(args) == 2 {
		module, ok := args[0].(*runtime.Module)
		if !ok {
			return nil, fmt.Errorf("reflect.%s qualified lookup expects module, got %s", fn, args[0].TypeName())
		}
		exportName, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("reflect.%s qualified export name must be string", fn)
		}
		exported, ok := module.Exports[exportName.Value]
		if !ok {
			if fn == "function" {
				canonical := module.Canonical
				if canonical == "" {
					canonical = module.Name
				}
				if builtin, found := vm.builtinValue(canonical, exportName.Value); found {
					return builtin, nil
				}
			}
			return runtime.Null{}, nil
		}
		value = exported
	}
	switch fn {
	case "function":
		switch value := value.(type) {
		case runtime.DecoratorTarget:
			if value.Target == "function" {
				return value, nil
			}
		case runtime.Function, runtime.OverloadedFunction:
			return value, nil
		case runtime.BytecodeFunction:
			return value, nil
		case runtime.String:
			// Look up a function in the chunk by name (or fall
			// through to module loader). Returns Null when the
			// name isn't found, matching the eval semantics.
			if vm.moduleLoader != nil {
				if found, ok := vm.moduleLoader.FindFunctionByName(value.Value); ok {
					return found, nil
				}
			}
			return runtime.Null{}, nil
		}
		return nil, fmt.Errorf("reflect.function expects function, got %s", value.TypeName())
	case "class":
		switch value := value.(type) {
		case runtime.DecoratorTarget:
			if value.Target == "class" {
				return value, nil
			}
		case runtime.BytecodeClass:
			return value, nil
		case *runtime.Class:
			// Native module class export (e.g. http.Request); the evaluator
			// returns it as-is, so match that.
			return value, nil
		case runtime.String:
			// Look up the class by name in the chunk's class table
			// first; fall back to cross-module search through the
			// module loader so framework helpers can resolve a
			// user-declared class from another module.
			if classIndex, ok := vm.classIndex[strings.ToLower(value.Value)]; ok {
				classInfo := vm.chunk.Classes[classIndex]
				return vm.bytecodeClassFromInfo(classInfo, int64(classIndex)), nil
			}
			if vm.moduleLoader != nil {
				if found, ok := vm.moduleLoader.FindClassByName(value.Value); ok {
					return found, nil
				}
			}
			return runtime.Null{}, nil
		case *runtime.Instance:
			// A cross-module instance's class lives in its home chunk, not this VM's; resolve module-exactly so the value equals the declaring module's own class value (two same-named classes in different modules must stay distinct).
			if value.Class != nil && value.Class.Module != "" && value.Class.Module != vm.moduleName && vm.moduleLoader != nil {
				if found, ok := vm.moduleLoader.ClassValueInModule(value.Class.Module, value.Class.Name); ok {
					return found, nil
				}
			}
			classIndex, ok := vm.classIndex[strings.ToLower(value.Class.Name)]
			if !ok {
				if metadata, ok := runtimeClassMetadata(value.Class); ok {
					target := runtime.DecoratorTarget{Target: "class", Class: &metadata}
					// The runtime class carries methods/fields but not its
					// class-level decorators when reflected from another module.
					// Pull those from the declaring chunk via the loader so
					// reflect.decorators works cross-module (the runtimeClass
					// metadata path already covers reflect.methods/fields).
					if vm.moduleLoader != nil {
						if found, ok := vm.moduleLoader.FindClassByName(value.Class.Name); ok {
							if bc, ok := found.(runtime.BytecodeClass); ok {
								target.Decorators = bc.Decorators
							}
						}
					}
					return target, nil
				}
				return nil, fmt.Errorf("reflect.class unknown class %s", value.Class.Name)
			}
			classInfo := vm.chunk.Classes[classIndex]
			return vm.bytecodeClassFromInfo(classInfo, int64(classIndex)), nil
		}
		return nil, fmt.Errorf("reflect.class expects class, instance, or name string, got %s", value.TypeName())
	default:
		return nil, fmt.Errorf("unsupported reflect lookup %s", fn)
	}
}

func (vm *VM) reflectMethodNativeCall(fn string, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("reflect.%s expects class and method name", fn)
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("reflect.%s method name must be string", fn)
	}
	key := strings.ToLower(name.Value)
	var class runtime.BytecodeClass
	var instance *runtime.Instance
	switch value := args[0].(type) {
	case runtime.BytecodeClass:
		class = vm.canonicalClassValue(value)
	case *runtime.Instance:
		instance = value
		classIndex, ok := vm.classIndex[strings.ToLower(value.Class.Name)]
		if !ok {
			if target, ok := vm.reflectedRuntimeInstanceMethod(fn, key, value); ok {
				return target, nil
			}
			if value.Class.Module != vm.moduleName {
				return runtime.Null{}, nil
			}
			return nil, fmt.Errorf("reflect.%s unknown class %s", fn, value.Class.Name)
		}
		classInfo := vm.chunk.Classes[classIndex]
		class = runtime.BytecodeClass{
			Name:             classInfo.Name,
			Doc:              classInfo.Doc,
			Index:            int64(classIndex),
			Decorators:       classInfo.Decorators,
			MethodDecorators: classInfo.MethodDecorators,
			StaticDecorators: classInfo.StaticDecorators,
			MethodMetadata:   vm.classFunctionMetadata(classInfo.Methods, "method", 1),
			StaticMetadata:   vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0),
		}
	default:
		return nil, fmt.Errorf("reflect.%s expects bytecode class or instance, got %s", fn, args[0].TypeName())
	}
	target := "method"
	decorators := []runtime.DecoratorMetadata(nil)
	var callable runtime.Value
	if fn == "staticMethod" {
		target = "staticMethod"
		decorators = class.StaticDecorators[key]
		callable = vm.reflectStaticMethodCallable(class, key)
	} else {
		decorators = class.MethodDecorators[key]
		if instance != nil {
			callable = vm.reflectBoundMethodCallable(class, key, instance)
		}
	}
	metadata := classMethodMetadata(class, key, fn == "staticMethod")
	if decorators == nil && metadata == nil && callable == nil {
		return runtime.Null{}, nil
	}
	if decorators == nil {
		decorators = []runtime.DecoratorMetadata{}
	}
	return runtime.DecoratorTarget{Target: target, Decorators: decorators, Function: metadata, Callable: callable}, nil
}

func (vm *VM) reflectBoundMethodCallable(class runtime.BytecodeClass, key string, instance *runtime.Instance) runtime.Value {
	if class.Index < 0 || int(class.Index) >= len(vm.chunk.Classes) {
		return nil
	}
	indices := vm.chunk.Classes[class.Index].Methods[key]
	if len(indices) == 0 {
		return nil
	}
	vm.noteEscape()
	return runtime.Function{
		Name: key,
		Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			functionIndex, err := vm.selectRuntimeFunction(Instruction{}, key, indices, args, 1)
			if err != nil {
				return nil, err
			}
			if err := vm.ensureCallableDecorators(); err != nil {
				return nil, err
			}
			if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
				return vm.callCallableWithForwardThis(decorated, args, instance)
			}
			callArgs := append([]runtime.Value{instance}, args...)
			return vm.CallFunctionRaw(functionIndex, callArgs)
		},
	}
}

func (vm *VM) reflectedRuntimeInstanceMethod(fn, key string, instance *runtime.Instance) (runtime.Value, bool) {
	if fn == "staticMethod" || instance == nil || instance.Class == nil {
		return runtime.Null{}, false
	}
	metadata := runtime.FunctionMetadata{}
	if overloads := instance.Class.MethodMetadata[key]; len(overloads) > 0 {
		metadata = overloads[0]
	}
	methods := instance.Class.Methods[key]
	if len(methods) == 0 && metadata.Name == "" {
		return runtime.Null{}, false
	}
	decorators := append([]runtime.DecoratorMetadata(nil), metadata.Decorators...)
	var callable runtime.Value
	if len(methods) > 0 {
		method := methods[0]
		loader := vm.moduleLoader
		callable = runtime.Function{
			Name: metadata.Name,
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if method.Native == nil {
					return nil, fmt.Errorf("reflected method is not callable")
				}
				return method.Native(instance, args)
			},
			NativeNamed: func(this *runtime.Instance, args []runtime.Value, names []string) (runtime.Value, error) {
				if loader == nil || instance.Class == nil {
					if method.Native == nil {
						return nil, fmt.Errorf("reflected method is not callable")
					}
					return method.Native(instance, args)
				}
				return loader.CallModuleMethodNamed(instance.Class.Module, instance.Class.Name, key, instance, args, names, nil)
			},
		}
	}
	if metadata.Name == "" {
		metadata.Name = key
	}
	return runtime.DecoratorTarget{Target: "method", Decorators: decorators, Function: &metadata, Callable: callable}, true
}

func (vm *VM) reflectStaticMethodCallable(class runtime.BytecodeClass, key string) runtime.Value {
	// A "" module is the entry chunk; foreign whenever this VM is an imported module, so route through the loader (which resolves "" to the main chunk).
	if class.Module != vm.moduleName {
		if vm.moduleLoader == nil || len(class.StaticMetadata[key]) == 0 {
			return nil
		}
		vm.noteEscape()
		return runtime.Function{
			Name: key,
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				return vm.moduleLoader.CallModuleStaticMethod(class, key, vm.wrapStatefulNativeArgs("", "", args), vm)
			},
		}
	}
	if class.Index < 0 || int(class.Index) >= len(vm.chunk.Classes) {
		return nil
	}
	indices := vm.chunk.Classes[class.Index].StaticMethods[key]
	if len(indices) == 0 {
		return nil
	}
	vm.noteEscape()
	return runtime.Function{
		Name: key,
		Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			functionIndex, err := vm.selectRuntimeFunction(Instruction{}, key, indices, args, 0)
			if err != nil {
				return nil, err
			}
			if err := vm.ensureCallableDecorators(); err != nil {
				return nil, err
			}
			if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
				return vm.callCallable(decorated, args)
			}
			return vm.CallFunctionRaw(functionIndex, args)
		},
	}
}

func reflectDecoratorTarget(value runtime.Value) (runtime.DecoratorTarget, bool) {
	switch value := value.(type) {
	case runtime.DecoratorTarget:
		return value, true
	case runtime.BytecodeFunction:
		return runtime.DecoratorTarget{Target: "function", Decorators: value.Decorators, Function: bytecodeFunctionMetadata(value)}, true
	case runtime.BytecodeClass:
		return runtime.DecoratorTarget{Target: "class", Decorators: value.Decorators}, true
	default:
		return runtime.DecoratorTarget{}, false
	}
}

// reflectLocation returns the source position of a function or class
// declaration as `{module: string, line: int, column: int}`. Returns
// null when the value has no recorded location (native stdlib, etc.).
func (vm *VM) reflectLocation(value runtime.Value) (runtime.Value, error) {
	switch v := value.(type) {
	case runtime.BytecodeFunction:
		if v.DefLine == 0 && v.DefColumn == 0 {
			return runtime.Null{}, nil
		}
		return makeLocationDict(v.Module, v.DefLine, v.DefColumn), nil
	case runtime.BytecodeClosure:
		if int(v.FunctionIndex) >= len(vm.chunk.Functions) {
			return runtime.Null{}, nil
		}
		info := vm.chunk.Functions[v.FunctionIndex]
		if info.DefLine == 0 && info.DefColumn == 0 {
			return runtime.Null{}, nil
		}
		return makeLocationDict(v.Module, info.DefLine, info.DefColumn), nil
	case runtime.BytecodeClass:
		v = vm.canonicalClassValue(v)
		if v.DefLine == 0 && v.DefColumn == 0 {
			return runtime.Null{}, nil
		}
		return makeLocationDict(v.Module, v.DefLine, v.DefColumn), nil
	case runtime.DecoratorTarget:
		if v.Function != nil && (v.Function.DefLine != 0 || v.Function.DefColumn != 0) {
			return makeLocationDict(v.Function.Module, v.Function.DefLine, v.Function.DefColumn), nil
		}
		if v.Class != nil && (v.Class.DefLine != 0 || v.Class.DefColumn != 0) {
			return makeLocationDict(v.Class.Module, v.Class.DefLine, v.Class.DefColumn), nil
		}
		return runtime.Null{}, nil
	case *runtime.Instance:
		if v == nil || v.Class == nil {
			return runtime.Null{}, nil
		}
		if classInfo, ok := vm.classInfo(v.Class.Name); ok {
			if classInfo.DefLine == 0 && classInfo.DefColumn == 0 {
				return runtime.Null{}, nil
			}
			return makeLocationDict(v.Class.Module, classInfo.DefLine, classInfo.DefColumn), nil
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, nil
}

func makeLocationDict(module string, line, column int64) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putBytecodeDict(entries, "module", runtime.String{Value: module})
	putBytecodeDict(entries, "line", runtime.NewInt64(line))
	putBytecodeDict(entries, "column", runtime.NewInt64(column))
	return runtime.Dict{Entries: entries}
}

// reflectFunctionMetadataValue resolves a same-module BytecodeClosure (a free function passed by value through an `any` binding) to its populated function metadata before delegating.
func (vm *VM) reflectFunctionMetadataValue(value runtime.Value) (runtime.FunctionMetadata, bool) {
	if closure, ok := value.(runtime.BytecodeClosure); ok && (closure.Module == "" || closure.Module == vm.moduleName) {
		value = vm.bytecodeFunctionValue(closure.FunctionIndex, false)
	}
	return reflectFunctionMetadata(value)
}

func reflectFunctionMetadata(value runtime.Value) (runtime.FunctionMetadata, bool) {
	switch value := value.(type) {
	case runtime.DecoratorTarget:
		if value.Function != nil {
			return *value.Function, true
		}
	case runtime.BytecodeFunction:
		metadata := bytecodeFunctionMetadata(value)
		if metadata != nil {
			return *metadata, true
		}
	case runtime.Function:
		// A module-boundary bridge wrapper carries the source function's full metadata; genuine natives degrade to empty like the evaluator.
		if value.Metadata != nil {
			return *value.Metadata, true
		}
		metadata := runtime.FunctionMetadata{Name: value.Name, ReturnType: "void"}
		if value.ReturnType != nil {
			metadata.ReturnType = value.ReturnType.String()
		}
		return metadata, true
	}
	return runtime.FunctionMetadata{}, false
}

// bytecodeClassFromInfo builds a BytecodeClass value from a class index.
// Used by reflect.class when looking up a class by name or instance so
// the produced value matches the one users get from passing the class
// reference directly.
func (vm *VM) bytecodeClassFromInfo(classInfo ClassInfo, index int64) runtime.BytecodeClass {
	return BuildBytecodeClass(vm.chunk, classInfo, index, vm.moduleName)
}

// Canonical reflectable-class builder shared with the loader so a cross-module class value carries the same method/static/constructor metadata as the declaring VM's.
func BuildBytecodeClass(chunk Chunk, classInfo ClassInfo, index int64, module string) runtime.BytecodeClass {
	return runtime.BytecodeClass{
		Module:              module,
		Name:                classInfo.Name,
		Doc:                 classInfo.Doc,
		Index:               index,
		Parent:              classInfo.ParentName,
		Fields:              append([]string(nil), classInfo.FieldNames...),
		Interfaces:          append([]string(nil), classInfo.Implements...),
		Decorators:          classInfo.Decorators,
		MethodDecorators:    classInfo.MethodDecorators,
		StaticDecorators:    classInfo.StaticDecorators,
		MethodMetadata:      classFunctionMetadataForChunk(chunk, module, classInfo.Methods, "method", 1),
		StaticMetadata:      classFunctionMetadataForChunk(chunk, module, classInfo.StaticMethods, "staticMethod", 0),
		StaticConsts:        staticConstNames(classInfo.StaticValues),
		ConstructorMetadata: constructorFunctionMetadataForChunk(chunk, module, classInfo.ConstructorIndices),
		DefLine:             classInfo.DefLine,
		DefColumn:           classInfo.DefColumn,
	}
}

// reflectClassMetadata rehydrates a metadata-poor BytecodeClass (one that arrived via a variable, not a direct identifier) before reading its metadata.
func (vm *VM) reflectClassMetadata(value runtime.Value) (runtime.ClassMetadata, bool) {
	switch value := value.(type) {
	case runtime.DecoratorTarget:
		if value.Class != nil {
			return *value.Class, true
		}
	case runtime.BytecodeClass:
		return bytecodeClassMetadata(vm.canonicalClassValue(value)), true
	case *runtime.Class:
		return runtimeClassMetadata(value)
	case *runtime.Instance:
		// Accept an instance and walk to its class so framework
		// code that has the instance in hand doesn't need to
		// recover the class separately.
		if value != nil && value.Class != nil {
			return runtimeClassMetadata(value.Class)
		}
	case *runtime.EnumDef:
		md := runtime.ClassMetadata{Name: value.Name, Module: value.Module}
		for name := range value.MethodIndices {
			md.Methods = append(md.Methods, name)
		}
		md.Interfaces = append(md.Interfaces, value.Implements...)
		sort.Strings(md.Methods)
		sort.Strings(md.Interfaces)
		return md, true
	}
	return runtime.ClassMetadata{}, false
}

// vmPrimitiveTypeMetadata mirrors the evaluator's primitiveTypeMetadata
// for the VM. See evaluator.go for the rationale.
func vmPrimitiveTypeMetadata(value runtime.Value) (runtime.ClassMetadata, bool) {
	switch value.(type) {
	case *runtime.List:
		return runtime.ClassMetadata{Name: "list", Methods: vmPrimitiveMethodNamesFor("list")}, true
	case runtime.Dict:
		return runtime.ClassMetadata{Name: "dict", Methods: vmPrimitiveMethodNamesFor("dict")}, true
	case runtime.Set:
		return runtime.ClassMetadata{Name: "set", Methods: vmPrimitiveMethodNamesFor("set")}, true
	case runtime.String:
		return runtime.ClassMetadata{Name: "string", Methods: vmPrimitiveMethodNamesFor("string")}, true
	case runtime.Bytes:
		return runtime.ClassMetadata{Name: "bytes", Methods: vmPrimitiveMethodNamesFor("bytes")}, true
	case runtime.Range:
		return runtime.ClassMetadata{Name: "range", Methods: vmPrimitiveMethodNamesFor("range")}, true
	}
	return runtime.ClassMetadata{}, false
}

// vmDirValue mirrors the evaluator's dirValue: the sorted method names
// callable on a value. The numeric/bool lists and the collection lists
// (via vmPrimitiveMethodNamesFor) are kept identical to the evaluator so
// dir(value) produces byte-identical output on both backends.
func (vm *VM) vmDirValue(value runtime.Value) runtime.Value {
	var names []string
	switch v := value.(type) {
	case *runtime.Module:
		for name := range v.Exports {
			names = append(names, name)
		}
	case runtime.BytecodeClass:
		names = vm.dirClassNames(v)
	case *runtime.Class:
		seen := map[string]bool{}
		for class := v; class != nil; class = class.Parent {
			for _, field := range class.Fields {
				seen[field.Name] = true
			}
			for name := range class.Methods {
				seen[name] = true
			}
			for name := range class.StaticMethods {
				seen[name] = true
			}
			for name := range class.StaticValues {
				seen[name] = true
			}
		}
		for name := range seen {
			names = append(names, name)
		}
	case *runtime.Instance:
		seen := map[string]bool{}
		for name := range v.Fields {
			seen[name] = true
		}
		for class := v.Class; class != nil; class = class.Parent {
			for _, field := range class.Fields {
				seen[field.Name] = true
			}
			for name := range class.Methods {
				seen[name] = true
			}
		}
		for name := range seen {
			names = append(names, name)
		}
	case runtime.Dict:
		names = vmPrimitiveMethodNamesFor("dict")
	case runtime.Set:
		names = vmPrimitiveMethodNamesFor("set")
	case *runtime.List:
		names = vmPrimitiveMethodNamesFor("list")
	case runtime.String:
		names = vmPrimitiveMethodNamesFor("string")
	case runtime.Bytes:
		names = vmPrimitiveMethodNamesFor("bytes")
	case runtime.Range:
		names = vmPrimitiveMethodNamesFor("range")
	case runtime.SmallInt, runtime.Int:
		names = vmPrimitiveMethodNamesFor("int")
	case runtime.Decimal:
		names = vmPrimitiveMethodNamesFor("decimal")
	case runtime.Float:
		names = vmPrimitiveMethodNamesFor("float")
	case runtime.Bool:
		names = vmPrimitiveMethodNamesFor("bool")
	case *runtime.Generator:
		names = append([]string(nil), native.GeneratorMethods...)
	case *runtime.NDArray:
		names = append([]string(nil), native.NDArrayMethods...)
	case *runtime.HtmlNode:
		names = append([]string(nil), native.HtmlNodeMethods...)
	case *runtime.Distribution:
		names = append([]string(nil), native.DistributionMethods...)
	case *runtime.Complex:
		names = append([]string(nil), native.ComplexMethods...)
	case *runtime.DataFrame:
		names = append([]string(nil), native.DataFrameMethods...)
	case *runtime.DFSeries:
		names = append([]string(nil), native.DFSeriesMethods...)
	case *runtime.DFExpr:
		names = append([]string(nil), native.DFExprMethods...)
	case *runtime.DFGroupBy:
		names = append([]string(nil), native.DFGroupByMethods...)
	case runtime.DateTimeInstant:
		names = append([]string(nil), native.DateTimeInstantMethods...)
	case runtime.DateTimeDuration:
		names = append([]string(nil), native.DateTimeDurationMethods...)
	case runtime.DateTimeZone:
		names = append([]string(nil), native.DateTimeZoneMethods...)
	case runtime.Function, runtime.OverloadedFunction:
		names = []string{"call"}
	default:
		names = []string{}
	}
	sort.Strings(names)
	elements := make([]runtime.Value, 0, len(names))
	for _, name := range names {
		elements = append(elements, runtime.String{Value: name})
	}
	return &runtime.List{Elements: elements}
}

func vmPrimitiveMethodNamesFor(typeName string) []string {
	return append([]string(nil), native.PrimitiveMethods[typeName]...)
}

func staticConstNames(values map[string]int64) []string {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return names
}

// dirClassNames gathers a class's deduped member surface up the parent chain, matching the evaluator's dir().
func (vm *VM) dirClassNames(class runtime.BytecodeClass) []string {
	seen := map[string]bool{}
	visited := map[string]bool{}
	current := vm.canonicalClassValue(class)
	for {
		key := current.Module + "." + current.Name
		if visited[key] {
			break
		}
		visited[key] = true
		for _, field := range current.Fields {
			seen[field] = true
		}
		for name := range current.MethodMetadata {
			seen[name] = true
		}
		for name := range current.StaticMetadata {
			seen[name] = true
		}
		for _, name := range current.StaticConsts {
			seen[name] = true
		}
		parent, ok := vm.resolveParentClassValue(current)
		if !ok {
			break
		}
		current = parent
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

// canonicalClassValue rehydrates full metadata for a bare class identifier, which compiles to a name+index-only constant.
func (vm *VM) canonicalClassValue(class runtime.BytecodeClass) runtime.BytecodeClass {
	if len(class.MethodMetadata) > 0 || len(class.StaticMetadata) > 0 || len(class.Fields) > 0 || len(class.StaticConsts) > 0 {
		return class
	}
	if class.Module == "" || class.Module == vm.moduleName {
		if class.Index >= 0 && int(class.Index) < len(vm.chunk.Classes) && strings.EqualFold(vm.chunk.Classes[class.Index].Name, class.Name) {
			return BuildBytecodeClass(vm.chunk, vm.chunk.Classes[class.Index], class.Index, vm.moduleName)
		}
	}
	// Foreign class (including the "" entry chunk viewed from an imported VM): resolve through the loader.
	if class.Module != vm.moduleName && vm.moduleLoader != nil {
		if resolved, ok := vm.moduleLoader.ClassValueInModule(class.Module, class.Name); ok {
			if bc, ok := resolved.(runtime.BytecodeClass); ok {
				return bc
			}
		}
	}
	return class
}

// resolveParentClassValue resolves a class's parent to a full class value, hopping modules for a qualified or home-module parent.
func (vm *VM) resolveParentClassValue(class runtime.BytecodeClass) (runtime.BytecodeClass, bool) {
	if class.Parent == "" {
		return runtime.BytecodeClass{}, false
	}
	if module, parentName, ok := splitQualifiedClassName(class.Parent); ok {
		if vm.moduleLoader != nil {
			if resolved, found := vm.moduleLoader.ClassValueInModule(module, parentName); found {
				if bc, ok := resolved.(runtime.BytecodeClass); ok {
					return bc, true
				}
			}
		}
		return runtime.BytecodeClass{}, false
	}
	home := class.Module
	if home == "" || home == vm.moduleName {
		if idx, ok := vm.classIndex[strings.ToLower(class.Parent)]; ok {
			return vm.bytecodeClassFromInfo(vm.chunk.Classes[idx], int64(idx)), true
		}
	}
	if vm.moduleLoader != nil {
		if home != "" {
			if resolved, found := vm.moduleLoader.ClassValueInModule(home, class.Parent); found {
				if bc, ok := resolved.(runtime.BytecodeClass); ok {
					return bc, true
				}
			}
		}
		if resolved, found := vm.moduleLoader.FindClassByName(class.Parent); found {
			if bc, ok := resolved.(runtime.BytecodeClass); ok {
				return bc, true
			}
		}
	}
	return runtime.BytecodeClass{}, false
}

func runtimeClassMetadata(value *runtime.Class) (runtime.ClassMetadata, bool) {
	if value == nil {
		return runtime.ClassMetadata{}, false
	}
	methods := map[string]string{}
	for name, overloads := range value.MethodMetadata {
		methods[name] = bytecodeFunctionMetadataName(name, overloads)
	}
	staticMethods := map[string]string{}
	for name, overloads := range value.StaticMetadata {
		staticMethods[name] = bytecodeFunctionMetadataName(name, overloads)
	}
	metadata := runtime.ClassMetadata{
		Name:          value.Name,
		Doc:           value.Doc,
		Methods:       sortedStringMapValues(methods),
		StaticMethods: sortedStringMapValues(staticMethods),
	}
	if value.Parent != nil {
		metadata.Parent = value.Parent.Name
	}
	for _, field := range value.Fields {
		metadata.Fields = append(metadata.Fields, field.Name)
	}
	for _, iface := range value.Implements {
		metadata.Interfaces = append(metadata.Interfaces, iface.Name)
	}
	sort.Strings(metadata.Fields)
	sort.Strings(metadata.Interfaces)
	return metadata, len(metadata.Methods) > 0 || len(metadata.StaticMethods) > 0 || len(metadata.Fields) > 0 || metadata.Name != ""
}

func bytecodeClassMetadata(value runtime.BytecodeClass) runtime.ClassMetadata {
	methods := map[string]string{}
	for name, overloads := range value.MethodMetadata {
		methods[name] = bytecodeFunctionMetadataName(name, overloads)
	}
	staticMethods := map[string]string{}
	for name, overloads := range value.StaticMetadata {
		staticMethods[name] = bytecodeFunctionMetadataName(name, overloads)
	}
	metadata := runtime.ClassMetadata{
		Name:          value.Name,
		Doc:           value.Doc,
		Parent:        value.Parent,
		Fields:        append([]string(nil), value.Fields...),
		Methods:       sortedStringMapValues(methods),
		StaticMethods: sortedStringMapValues(staticMethods),
		Interfaces:    append([]string(nil), value.Interfaces...),
		Module:        value.Module,
		DefLine:       value.DefLine,
		DefColumn:     value.DefColumn,
	}
	sort.Strings(metadata.Fields)
	sort.Strings(metadata.Interfaces)
	return metadata
}

func bytecodeFunctionMetadataName(fallback string, overloads []runtime.FunctionMetadata) string {
	if len(overloads) > 0 && overloads[0].Name != "" {
		if _, name, ok := strings.Cut(overloads[0].Name, "."); ok {
			return name
		}
		return overloads[0].Name
	}
	return fallback
}

func sortedStringMapValues(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func bytecodeStringList(values []string) *runtime.List {
	elements := make([]runtime.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, runtime.String{Value: value})
	}
	return &runtime.List{Elements: elements}
}

func bytecodeFunctionMetadata(value runtime.BytecodeFunction) *runtime.FunctionMetadata {
	return &runtime.FunctionMetadata{
		Name:           value.Name,
		Target:         "function",
		Doc:            value.Doc,
		TypeParameters: append([]string(nil), value.TypeParameters...),
		Parameters:     append([]runtime.ParameterMetadata(nil), value.Parameters...),
		ReturnType:     value.ReturnType,
		Async:          value.Async,
		Variadic:       value.Variadic,
		Decorators:     append([]runtime.DecoratorMetadata(nil), value.Decorators...),
		Module:         value.Module,
		DefLine:        value.DefLine,
		DefColumn:      value.DefColumn,
	}
}

func classMethodMetadata(class runtime.BytecodeClass, key string, static bool) *runtime.FunctionMetadata {
	var methods []runtime.FunctionMetadata
	if static {
		methods = class.StaticMetadata[key]
	} else {
		methods = class.MethodMetadata[key]
	}
	if len(methods) == 0 {
		return nil
	}
	metadata := methods[0]
	return &metadata
}

func decoratorMetadataDict(decorator runtime.DecoratorMetadata) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putBytecodeDict(entries, "name", runtime.String{Value: decorator.Name})
	putBytecodeDict(entries, "target", runtime.String{Value: decorator.Target})
	putBytecodeDict(entries, "position", runtime.NewInt64(decorator.Position))
	putBytecodeDict(entries, "overload", runtime.NewInt64(decorator.Overload))
	putBytecodeDict(entries, "args", &runtime.List{Elements: append([]runtime.Value(nil), decorator.Args...)})
	named := map[string]runtime.DictEntry{}
	for name, value := range decorator.NamedArgs {
		putBytecodeDict(named, name, value)
	}
	putBytecodeDict(entries, "namedArgs", runtime.Dict{Entries: named})
	putBytecodeDict(entries, "line", runtime.NewInt64(decorator.Line))
	putBytecodeDict(entries, "column", runtime.NewInt64(decorator.Column))
	return runtime.Dict{Entries: entries}
}

func parameterMetadataDict(parameter runtime.ParameterMetadata) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putBytecodeDict(entries, "name", runtime.String{Value: parameter.Name})
	putBytecodeDict(entries, "type", runtime.String{Value: parameter.Type})
	putBytecodeDict(entries, "variadic", runtime.Bool{Value: parameter.Variadic})
	putBytecodeDict(entries, "hasDefault", runtime.Bool{Value: parameter.HasDefault})
	if len(parameter.Decorators) > 0 {
		decValues := make([]runtime.Value, 0, len(parameter.Decorators))
		for _, dec := range parameter.Decorators {
			decValues = append(decValues, decoratorMetadataDict(dec))
		}
		putBytecodeDict(entries, "decorators", &runtime.List{Elements: decValues})
	}
	return runtime.Dict{Entries: entries}
}

func interfaceMethodMetadataDict(method runtime.FunctionMetadata) runtime.Dict {
	params := make([]runtime.Value, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		params = append(params, parameterMetadataDict(parameter))
	}
	entries := map[string]runtime.DictEntry{}
	putBytecodeDict(entries, "name", runtime.String{Value: method.Name})
	if method.Doc == "" {
		putBytecodeDict(entries, "doc", runtime.Null{})
	} else {
		putBytecodeDict(entries, "doc", runtime.String{Value: method.Doc})
	}
	putBytecodeDict(entries, "parameters", &runtime.List{Elements: params})
	putBytecodeDict(entries, "returnType", runtime.String{Value: method.ReturnType})
	return runtime.Dict{Entries: entries}
}

func bytecodeDocMetadataDict(doc string) runtime.Dict {
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	lineValues := make([]runtime.Value, 0, len(lines))
	for _, line := range lines {
		lineValues = append(lineValues, runtime.String{Value: line})
	}
	summary := ""
	summaryIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			summary = strings.TrimSpace(line)
			summaryIndex = i
			break
		}
	}
	body := ""
	if summaryIndex >= 0 && summaryIndex+1 < len(lines) {
		body = strings.TrimSpace(strings.Join(lines[summaryIndex+1:], "\n"))
	}
	entries := map[string]runtime.DictEntry{}
	putBytecodeDict(entries, "text", runtime.String{Value: doc})
	putBytecodeDict(entries, "summary", runtime.String{Value: summary})
	putBytecodeDict(entries, "body", runtime.String{Value: body})
	putBytecodeDict(entries, "lines", &runtime.List{Elements: lineValues})
	return runtime.Dict{Entries: entries}
}

func putBytecodeDict(entries map[string]runtime.DictEntry, key string, value runtime.Value) {
	keyValue := runtime.String{Value: key}
	entries[native.DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
}

func (vm *VM) constructorFunctionMetadata(indices []int64) []runtime.FunctionMetadata {
	return constructorFunctionMetadataForChunk(vm.chunk, vm.moduleName, indices)
}

func constructorFunctionMetadataForChunk(chunk Chunk, module string, indices []int64) []runtime.FunctionMetadata {
	result := make([]runtime.FunctionMetadata, 0, len(indices))
	for _, index := range indices {
		if index < 0 || int(index) >= len(chunk.Functions) {
			continue
		}
		info := chunk.Functions[index]
		result = append(result, runtime.FunctionMetadata{
			Name:       info.Name,
			Target:     "constructor",
			Doc:        info.Doc,
			Parameters: parameterMetadataFromFunctionInfo(info, 1),
			ReturnType: info.ReturnType,
			Async:      info.Async,
			Variadic:   info.Variadic,
			Decorators: append([]runtime.DecoratorMetadata(nil), info.Decorators...),
			Module:     module,
			DefLine:    info.DefLine,
			DefColumn:  info.DefColumn,
		})
	}
	return result
}

func (vm *VM) classFunctionMetadata(methods map[string][]int64, target string, paramOffset int) map[string][]runtime.FunctionMetadata {
	return classFunctionMetadataForChunk(vm.chunk, vm.moduleName, methods, target, paramOffset)
}

func classFunctionMetadataForChunk(chunk Chunk, module string, methods map[string][]int64, target string, paramOffset int) map[string][]runtime.FunctionMetadata {
	metadata := map[string][]runtime.FunctionMetadata{}
	for name, indices := range methods {
		for _, index := range indices {
			if index < 0 || int(index) >= len(chunk.Functions) {
				continue
			}
			info := chunk.Functions[index]
			metadata[name] = append(metadata[name], runtime.FunctionMetadata{
				Name:           info.Name,
				Target:         target,
				Doc:            info.Doc,
				TypeParameters: append([]string(nil), info.TypeParameters...),
				Parameters:     parameterMetadataFromFunctionInfo(info, paramOffset),
				ReturnType:     info.ReturnType,
				Async:          info.Async,
				Variadic:       info.Variadic,
				Decorators:     append([]runtime.DecoratorMetadata(nil), info.Decorators...),
				Module:         module,
				DefLine:        info.DefLine,
				DefColumn:      info.DefColumn,
			})
		}
	}
	return metadata
}

func (vm *VM) reflectConstructors(arg runtime.Value) (runtime.Value, error) {
	/* Cross-chunk class: dispatch through the moduleLoader so the
	 * index resolves against the chunk that declared the class. */
	if bc, ok := arg.(runtime.BytecodeClass); ok && bc.Module != vm.moduleName && vm.moduleLoader != nil {
		return vm.moduleLoader.ConstructorsForModuleClass(bc)
	}
	var classIndex int64 = -1
	switch value := arg.(type) {
	case runtime.BytecodeClass:
		classIndex = value.Index
	case runtime.DecoratorTarget:
		if value.Class != nil {
			if idx, ok := vm.classIndex[strings.ToLower(value.Class.Name)]; ok {
				classIndex = int64(idx)
			}
		}
	case *runtime.Instance:
		if idx, ok := vm.classIndex[strings.ToLower(value.Class.Name)]; ok {
			classIndex = int64(idx)
		}
	}
	if classIndex < 0 || int(classIndex) >= len(vm.chunk.Classes) {
		return &runtime.List{Elements: []runtime.Value{}}, nil
	}
	classInfo := vm.chunk.Classes[classIndex]
	overloads := make([]runtime.Value, 0, len(classInfo.ConstructorIndices))
	for _, idx := range classInfo.ConstructorIndices {
		if idx < 0 || int(idx) >= len(vm.chunk.Functions) {
			continue
		}
		info := vm.chunk.Functions[idx]
		params := parameterMetadataFromFunctionInfo(info, 1) // skip 'this'
		paramValues := make([]runtime.Value, 0, len(params))
		for _, p := range params {
			paramValues = append(paramValues, parameterMetadataDict(p))
		}
		overloads = append(overloads, &runtime.List{Elements: paramValues})
	}
	return &runtime.List{Elements: overloads}, nil
}

func parameterMetadataFromFunctionInfo(info FunctionInfo, paramOffset int) []runtime.ParameterMetadata {
	if paramOffset > len(info.ParamNames) {
		paramOffset = len(info.ParamNames)
	}
	parameters := make([]runtime.ParameterMetadata, 0, len(info.ParamNames)-paramOffset)
	for i := paramOffset; i < len(info.ParamNames); i++ {
		typ := "any"
		if i < len(info.ParamTypes) && info.ParamTypes[i] != "" {
			typ = info.ParamTypes[i]
		}
		hasDefault := false
		if i < len(info.DefaultConstants) {
			hasDefault = info.DefaultConstants[i] >= 0
		}
		var decs []runtime.DecoratorMetadata
		if i < len(info.ParamDecorators) {
			decs = info.ParamDecorators[i]
		}
		parameters = append(parameters, runtime.ParameterMetadata{
			Name:       info.displayParamName(i),
			Type:       typ,
			Variadic:   info.Variadic && i == len(info.ParamNames)-1,
			HasDefault: hasDefault,
			Decorators: decs,
		})
	}
	return parameters
}

// reflectFieldsResult returns the per-field metadata list shape that
// reflect.fields produces - {name, type, nullable, hasDefault} dicts.
// Pulls type info from the chunk's class table when available, falling
// back to "any" for builtin classes or compile-time targets without
// type info.
// loaderClassValue resolves a cross-chunk instance's class to its home-chunk BytecodeClass so reflection reads full metadata from the declaring module.
func (vm *VM) loaderClassValue(module, className string) (runtime.BytecodeClass, bool) {
	if vm.moduleLoader == nil {
		return runtime.BytecodeClass{}, false
	}
	value, ok := vm.moduleLoader.ClassValueInModule(module, className)
	if !ok {
		return runtime.BytecodeClass{}, false
	}
	bc, ok := value.(runtime.BytecodeClass)
	return bc, ok
}

func (vm *VM) reflectFieldsResult(target runtime.Value, metadata runtime.ClassMetadata) runtime.Value {
	if bc, ok := target.(runtime.BytecodeClass); ok && vm.moduleLoader != nil && bc.Module != vm.moduleName {
		if result, err := vm.moduleLoader.FieldsForModuleClass(bc); err == nil {
			return result
		}
	}
	if instance, ok := target.(*runtime.Instance); ok && instance != nil && instance.Class != nil {
		// Prefer the chunk-local ClassInfo when the instance's class
		// is in this VM's classIndex - it carries full FieldTypes
		// strings. Fall back to runtime.Class.Fields (populated at
		// construction in any originating chunk) for cross-chunk
		// instances; that path loses the type-string detail but
		// retains decorators so framework reflection still works
		// across module boundaries.
		if idx, ok := vm.classIndex[strings.ToLower(instance.Class.Name)]; ok && int(idx) < len(vm.chunk.Classes) {
			target = vm.bytecodeClassFromInfo(vm.chunk.Classes[idx], int64(idx))
		} else if bc, bok := vm.loaderClassValue(instance.Class.Module, instance.Class.Name); bok {
			// Route through the class's home chunk so the field type strings match the class-value path (the runtime.Class.Fields fallback below loses them).
			if result, ferr := vm.moduleLoader.FieldsForModuleClass(bc); ferr == nil {
				return result
			}
			target = bc
		} else if len(instance.Class.Fields) > 0 {
			entries := make([]runtime.Value, 0, len(instance.Class.Fields))
			for _, field := range instance.Class.Fields {
				fieldType := "any"
				nullable := false
				if field.Type != nil {
					fieldType = field.Type.String()
					nullable = field.Type.Nullable
				}
				dictEntries := map[string]runtime.DictEntry{
					native.DictKey(runtime.String{Value: "name"}):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: field.Name}},
					native.DictKey(runtime.String{Value: "type"}):       {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: fieldType}},
					native.DictKey(runtime.String{Value: "nullable"}):   {Key: runtime.String{Value: "nullable"}, Value: runtime.Bool{Value: nullable}},
					native.DictKey(runtime.String{Value: "hasDefault"}): {Key: runtime.String{Value: "hasDefault"}, Value: runtime.Bool{Value: field.Default != nil}},
				}
				// Cross-chunk reflection: decorators ride along as runtime metadata persisted at construction; render on the same shape as the bytecode-class path.
				if len(field.DecoratorMeta) > 0 {
					decValues := make([]runtime.Value, 0, len(field.DecoratorMeta))
					for _, dec := range field.DecoratorMeta {
						decValues = append(decValues, decoratorMetadataDict(dec))
					}
					key := runtime.String{Value: "decorators"}
					dictEntries[native.DictKey(key)] = runtime.DictEntry{Key: key, Value: &runtime.List{Elements: decValues}}
				}
				entries = append(entries, runtime.Dict{Entries: dictEntries})
			}
			return &runtime.List{Elements: entries}
		}
	}
	// Pull type info from the chunk's class table when reachable
	// (BytecodeClass / DecoratorTarget / string-name).
	var classInfo ClassInfo
	var haveClass bool
	switch v := target.(type) {
	case runtime.BytecodeClass:
		if v.Index >= 0 && int(v.Index) < len(vm.chunk.Classes) {
			classInfo = vm.chunk.Classes[v.Index]
			haveClass = true
		}
	case runtime.DecoratorTarget:
		if v.Class != nil {
			if idx, ok := vm.classIndex[strings.ToLower(v.Class.Name)]; ok && int(idx) < len(vm.chunk.Classes) {
				classInfo = vm.chunk.Classes[idx]
				haveClass = true
			}
		}
	case runtime.String:
		if idx, ok := vm.classIndex[strings.ToLower(v.Value)]; ok && int(idx) < len(vm.chunk.Classes) {
			classInfo = vm.chunk.Classes[idx]
			haveClass = true
		}
	}
	if haveClass {
		type fieldEntry struct {
			name string
			dict runtime.Value
		}
		entries := make([]fieldEntry, 0, len(classInfo.FieldNames))
		for i, name := range classInfo.FieldNames {
			fieldType := "any"
			nullable := false
			if i < len(classInfo.FieldTypes) && classInfo.FieldTypes[i] != "" {
				fieldType = classInfo.FieldTypes[i]
				nullable = strings.HasPrefix(fieldType, "?")
			}
			var doc runtime.Value = runtime.Null{}
			if i < len(classInfo.FieldDocs) && classInfo.FieldDocs[i] != "" {
				doc = runtime.String{Value: classInfo.FieldDocs[i]}
			}
			fieldDict := map[string]runtime.DictEntry{
				native.DictKey(runtime.String{Value: "name"}):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: name}},
				native.DictKey(runtime.String{Value: "type"}):       {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: fieldType}},
				native.DictKey(runtime.String{Value: "nullable"}):   {Key: runtime.String{Value: "nullable"}, Value: runtime.Bool{Value: nullable}},
				native.DictKey(runtime.String{Value: "hasDefault"}): {Key: runtime.String{Value: "hasDefault"}, Value: runtime.Bool{Value: false}},
				native.DictKey(runtime.String{Value: "doc"}):        {Key: runtime.String{Value: "doc"}, Value: doc},
			}
			if i < len(classInfo.FieldDecorators) {
				decValues := make([]runtime.Value, 0, len(classInfo.FieldDecorators[i]))
				for _, dec := range classInfo.FieldDecorators[i] {
					decValues = append(decValues, decoratorMetadataDict(dec))
				}
				key := runtime.String{Value: "decorators"}
				fieldDict[native.DictKey(key)] = runtime.DictEntry{Key: key, Value: &runtime.List{Elements: decValues}}
			}
			entries = append(entries, fieldEntry{name: name, dict: runtime.Dict{Entries: fieldDict}})
		}
		// Sort alphabetically by name to match the evaluator's ordering.
		sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
		out := make([]runtime.Value, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.dict)
		}
		return &runtime.List{Elements: out}
	}
	// Last resort: name-only entries with type="any".
	entries := make([]runtime.Value, 0, len(metadata.Fields))
	for _, name := range metadata.Fields {
		entries = append(entries, runtime.Dict{Entries: map[string]runtime.DictEntry{
			native.DictKey(runtime.String{Value: "name"}):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: name}},
			native.DictKey(runtime.String{Value: "type"}):       {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: "any"}},
			native.DictKey(runtime.String{Value: "nullable"}):   {Key: runtime.String{Value: "nullable"}, Value: runtime.Bool{Value: false}},
			native.DictKey(runtime.String{Value: "hasDefault"}): {Key: runtime.String{Value: "hasDefault"}, Value: runtime.Bool{Value: false}},
		}})
	}
	return &runtime.List{Elements: entries}
}
