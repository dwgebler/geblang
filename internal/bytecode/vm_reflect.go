package bytecode

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"sort"
	"strconv"
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
		metadata, ok := reflectFunctionMetadata(args[0])
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
		metadata, ok := reflectFunctionMetadata(args[0])
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
		metadata, ok := reflectClassMetadata(args[0])
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
			return runtime.String{Value: metadata.Parent}, nil
		case "className":
			if metadata.Name == "" {
				return runtime.Null{}, nil
			}
			return runtime.String{Value: metadata.Name}, nil
		case "interfaces":
			return bytecodeStringList(metadata.Interfaces), nil
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
	if metadata, ok := reflectFunctionMetadata(value); ok {
		return metadata.Doc, true
	}
	if metadata, ok := reflectClassMetadata(value); ok {
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
		class = value
	case *runtime.Instance:
		instance = value
		classIndex, ok := vm.classIndex[strings.ToLower(value.Class.Name)]
		if !ok {
			if target, ok := reflectedRuntimeInstanceMethod(fn, key, value); ok {
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
	if metadata == nil && class.Index >= 0 && int(class.Index) < len(vm.chunk.Classes) {
		classInfo := vm.chunk.Classes[class.Index]
		if fn == "staticMethod" {
			class.StaticMetadata = vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0)
		} else {
			class.MethodMetadata = vm.classFunctionMetadata(classInfo.Methods, "method", 1)
		}
		metadata = classMethodMetadata(class, key, fn == "staticMethod")
	}
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

func reflectedRuntimeInstanceMethod(fn, key string, instance *runtime.Instance) (runtime.Value, bool) {
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
		callable = runtime.Function{
			Name: metadata.Name,
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if method.Native == nil {
					return nil, fmt.Errorf("reflected method is not callable")
				}
				return method.Native(instance, args)
			},
		}
	}
	if metadata.Name == "" {
		metadata.Name = key
	}
	return runtime.DecoratorTarget{Target: "method", Decorators: decorators, Function: &metadata, Callable: callable}, true
}

func (vm *VM) reflectStaticMethodCallable(class runtime.BytecodeClass, key string) runtime.Value {
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
		// Native-wrapped functions carry no source structure; degrade to
		// empty metadata like the evaluator.
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
	return runtime.BytecodeClass{
		Module:              vm.moduleName,
		Name:                classInfo.Name,
		Doc:                 classInfo.Doc,
		Index:               index,
		Parent:              classInfo.ParentName,
		Fields:              append([]string(nil), classInfo.FieldNames...),
		Interfaces:          append([]string(nil), classInfo.Implements...),
		Decorators:          classInfo.Decorators,
		MethodDecorators:    classInfo.MethodDecorators,
		StaticDecorators:    classInfo.StaticDecorators,
		MethodMetadata:      vm.classFunctionMetadata(classInfo.Methods, "method", 1),
		StaticMetadata:      vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0),
		ConstructorMetadata: vm.constructorFunctionMetadata(classInfo.ConstructorIndices),
		DefLine:             classInfo.DefLine,
		DefColumn:           classInfo.DefColumn,
	}
}

func reflectClassMetadata(value runtime.Value) (runtime.ClassMetadata, bool) {
	switch value := value.(type) {
	case runtime.DecoratorTarget:
		if value.Class != nil {
			return *value.Class, true
		}
	case runtime.BytecodeClass:
		return bytecodeClassMetadata(value), true
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
func vmDirValue(value runtime.Value) runtime.Value {
	var names []string
	switch v := value.(type) {
	case *runtime.Module:
		for name := range v.Exports {
			names = append(names, name)
		}
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

// decoratorMetadataDictFromAST mirrors decoratorMetadataDict for AST-
// shaped decorators carried on runtime.Class.Fields. Used by the
// cross-chunk reflect.fields path where the field's decorators were
// rebuilt from ast nodes (see decoratorsToAST). Only literal-shape
// args are reproduced; the named-args map is preserved for parity.
func decoratorMetadataDictFromAST(decorator ast.Decorator) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	name := ""
	if decorator.Name != nil {
		name = decorator.Name.Value
	}
	putBytecodeDict(entries, "name", runtime.String{Value: name})
	putBytecodeDict(entries, "target", runtime.String{Value: "field"})
	args := []runtime.Value{}
	named := map[string]runtime.DictEntry{}
	for _, arg := range decorator.Arguments {
		v := literalValueForExpression(arg.Value)
		if arg.Name != nil {
			putBytecodeDict(named, arg.Name.Value, v)
		} else {
			args = append(args, v)
		}
	}
	putBytecodeDict(entries, "args", &runtime.List{Elements: args})
	putBytecodeDict(entries, "namedArgs", runtime.Dict{Entries: named})
	return runtime.Dict{Entries: entries}
}

// literalValueForExpression is the inverse of literalExpressionForValue
// (used during decoratorsToAST). For decorator-arg AST nodes we know
// were rebuilt from literal values, recover the runtime value.
func literalValueForExpression(expr ast.Expression) runtime.Value {
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return runtime.String{Value: e.Value}
	case *ast.IntegerLiteral:
		n, err := runtime.NewIntLiteral(e.Value)
		if err == nil && n.Value.IsInt64() {
			return runtime.SmallInt{Value: n.Value.Int64()}
		}
		if err == nil {
			return n
		}
	case *ast.FloatLiteral:
		f, err := strconv.ParseFloat(e.Value, 64)
		if err == nil {
			return runtime.Float{Value: f}
		}
	case *ast.Literal:
		switch v := e.Value.(type) {
		case bool:
			return runtime.Bool{Value: v}
		case nil:
			return runtime.Null{}
		}
	}
	return runtime.Null{}
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
	result := make([]runtime.FunctionMetadata, 0, len(indices))
	for _, index := range indices {
		if index < 0 || int(index) >= len(vm.chunk.Functions) {
			continue
		}
		info := vm.chunk.Functions[index]
		result = append(result, runtime.FunctionMetadata{
			Name:       info.Name,
			Target:     "constructor",
			Doc:        info.Doc,
			Parameters: parameterMetadataFromFunctionInfo(info, 1),
			ReturnType: info.ReturnType,
			Async:      info.Async,
			Variadic:   info.Variadic,
			Decorators: append([]runtime.DecoratorMetadata(nil), info.Decorators...),
			Module:     vm.moduleName,
			DefLine:    info.DefLine,
			DefColumn:  info.DefColumn,
		})
	}
	return result
}

func (vm *VM) classFunctionMetadata(methods map[string][]int64, target string, paramOffset int) map[string][]runtime.FunctionMetadata {
	metadata := map[string][]runtime.FunctionMetadata{}
	for name, indices := range methods {
		for _, index := range indices {
			if index < 0 || int(index) >= len(vm.chunk.Functions) {
				continue
			}
			info := vm.chunk.Functions[index]
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
				Module:         vm.moduleName,
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
			Name:       info.ParamNames[i],
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
				/* Cross-chunk reflection: the field decorators come
				 * from the originating chunk's ClassInfo and were
				 * persisted onto runtime.Class.Fields at instance
				 * construction time. Surface them on the same shape
				 * as the bytecode-class path. */
				if len(field.Decorators) > 0 {
					decValues := make([]runtime.Value, 0, len(field.Decorators))
					for _, dec := range field.Decorators {
						decValues = append(decValues, decoratorMetadataDictFromAST(dec))
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
