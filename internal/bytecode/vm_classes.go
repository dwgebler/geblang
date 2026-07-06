package bytecode

import (
	"errors"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"math/big"
	"strconv"
	"strings"
)

func (vm *VM) constructClass(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "construct class instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	argc := int(instruction.Operands[1])
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, vm.runtimeError(instruction, "class index out of range")
	}
	args := make([]runtime.Value, argc)
	for i := argc - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	return vm.constructClassWithArgs(instruction, ip, classIndex, args, false)
}

func (vm *VM) constructClassWithArgs(instruction Instruction, ip int, classIndex int64, args []runtime.Value, raw bool) (int, error) {
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	// Consumed before field initializers can inherit or clear them.
	explicitBindings := vm.pendingTypeBindings
	vm.pendingTypeBindings = nil
	if !raw {
		if decorated, ok := vm.decoratedClasses[classIndex]; ok {
			switch dec := decorated.(type) {
			case runtime.BytecodeClass:
				if dec.Index != classIndex {
					return vm.constructDecoratedClass(instruction, ip, dec, args)
				}
			default:
				result, err := vm.callCallable(decorated, args)
				if err != nil {
					return vm.propagateDecoratedClassError(instruction, ip, err)
				}
				classInfo := vm.curMod.Chunk.Classes[classIndex]
				if instance, ok := result.(*runtime.Instance); ok && instance != nil && instance.Class != nil && instance.Class.Name != classInfo.Name {
					instance.ExtraTypeNames = append(instance.ExtraTypeNames, classInfo.Name)
				}
				vm.push(result)
				return ip, nil
			}
		}
	}
	if reason, abstract := vm.classAbstractnessReason(classInfo); abstract {
		thrown := runtime.Error{Class: "RuntimeError", Message: reason}
		vm.pendingThrow = &thrown
		return vm.jumpToExceptionHandler(instruction, ip)
	}
	instance := &runtime.Instance{
		Class:  vm.RuntimeClassValue(classIndex),
		Fields: map[string]runtime.Value{},
	}
	// Cross-module constructions have no following OpSetTypeBindings.
	if len(explicitBindings) > 0 {
		instance.TypeBindings = map[string]string{}
		for k, v := range explicitBindings {
			instance.TypeBindings[k] = v
		}
	}
	// Inherit type bindings from each parent's extends-clause arguments
	// (`class Sub extends Base<string>` propagates {T: "string"} to Sub).
	// Walk the chain via ParentIndex so multi-level inheritance composes.
	for ci := classInfo; ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(vm.curMod.Chunk.Classes); {
		parent := vm.curMod.Chunk.Classes[ci.ParentIndex]
		if len(ci.ParentArguments) > 0 && len(parent.TypeParameters) > 0 {
			if instance.TypeBindings == nil {
				instance.TypeBindings = map[string]string{}
			}
			for i, name := range parent.TypeParameters {
				if i >= len(ci.ParentArguments) {
					break
				}
				if _, exists := instance.TypeBindings[name]; !exists {
					instance.TypeBindings[name] = ci.ParentArguments[i]
				}
			}
		}
		ci = parent
	}
	if err := vm.initializeFields(instruction, instance, classIndex); err != nil {
		return 0, err
	}
	if len(classInfo.ConstructorIndices) == 0 {
		if len(args) != 0 {
			return 0, vm.runtimeError(instruction, "%s constructor expects no arguments", classInfo.Name)
		}
		if vm.classExtendsBuiltinError(classInfo) {
			var fields map[string]runtime.Value
			if len(instance.Fields) > 0 {
				fields = instance.Fields
			}
			vm.push(runtime.Error{Class: classInfo.Name, Fields: fields, Parents: vm.errorParentChain(classInfo)})
		} else {
			if classInfo.Immutable {
				instance.Frozen = true
			}
			for _, f := range classInfo.ImmutableFields {
				instance.LockField(f)
			}
			if classInfo.DestructorIndex >= 0 {
				vm.trackDestructible(instance)
			}
			vm.push(instance)
		}
		return ip, nil
	}
	functionIndex, err := vm.selectRuntimeFunctionWith(instruction, classInfo.Name, classInfo.ConstructorIndices, args, 1, explicitBindings)
	if err != nil {
		return 0, err
	}
	if len(explicitBindings) > 0 {
		fn := &vm.curMod.Chunk.Functions[functionIndex]
		for i, arg := range args {
			pi := i + 1 // slot 0 is the instance
			if fn.Variadic && pi >= len(fn.ParamTypes)-1 {
				pi = len(fn.ParamTypes) - 1
			}
			if arg == nil || pi >= len(fn.ParamTypes) || fn.ParamTypes[pi] == "" {
				continue
			}
			if !vm.matchValueToTypeSpecWith(fn.typeParamSet, explicitBindings, arg, vm.typeSpec(fn.ParamTypes[pi])) {
				paramName := ""
				if pi < len(fn.ParamNames) {
					paramName = fn.ParamNames[pi]
				}
				suffix := vm.collectionMismatchSuffixStr(arg, fn.ParamTypes[pi])
				gotName := vm.descriptiveRuntimeTypeName(arg)
				if suffix != "" {
					gotName = arg.TypeName()
				}
				msg := fmt.Sprintf("%s expects %s for parameter '%s', got %s%s", classInfo.Name, fn.ParamTypes[pi], paramName, gotName, suffix)
				return vm.throwTyped(instruction, ip, "RuntimeError", msg)
			}
		}
		vm.pendingTypeBindings = explicitBindings
	}
	callArgs := append([]runtime.Value{instance}, args...)
	nextIP, err := vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], callArgs, instance)
	if err != nil {
		return 0, err
	}
	// Mark the frame so OpReturn wraps the result as runtime.Error for error-derived classes.
	if vm.classExtendsBuiltinError(classInfo) && len(vm.frames) > 0 {
		vm.frames[len(vm.frames)-1].isErrorClass = true
	}
	if classInfo.Immutable && len(vm.frames) > 0 {
		vm.frames[len(vm.frames)-1].isImmutableClass = true
	}
	if len(classInfo.ImmutableFields) > 0 && len(vm.frames) > 0 {
		vm.frames[len(vm.frames)-1].immutableFieldsToLock = classInfo.ImmutableFields
		vm.frames[len(vm.frames)-1].lockInstance = instance
	}
	// Mark the frame so OpReturn registers the constructed instance
	// in the destructible-instances list once construction succeeds.
	if classInfo.DestructorIndex >= 0 && !vm.classExtendsBuiltinError(classInfo) && len(vm.frames) > 0 {
		vm.frames[len(vm.frames)-1].isDestructibleConstructor = true
	}
	// Persist inferred class type parameter bindings onto the instance.
	if len(classInfo.TypeParameters) > 0 && len(vm.frames) > 0 {
		classParamSet := map[string]bool{}
		for _, p := range classInfo.TypeParameters {
			classParamSet[p] = true
		}
		frame := vm.frames[len(vm.frames)-1]
		for name, typeName := range frame.typeBindings {
			if classParamSet[name] {
				if instance.TypeBindings == nil {
					instance.TypeBindings = map[string]string{}
				}
				instance.TypeBindings[name] = typeName
			}
		}
	}
	return nextIP, nil
}

func (vm *VM) constructDecoratedClass(instruction Instruction, ip int, replacement runtime.BytecodeClass, args []runtime.Value) (int, error) {
	return vm.constructClassWithArgs(instruction, ip, replacement.Index, args, false)
}

// runtimeFieldsForClass builds the per-field metadata on a
// runtime.Class. The slice is consumed by cross-chunk reflect.fields
// when the receiving VM can't look up the class by index in its own
// chunk. Decorators ride along so framework reflection sees the
// originating chunk's annotations from any other module.
func (vm *VM) runtimeFieldsForClass(classInfo ClassInfo) []runtime.Field {
	fields := make([]runtime.Field, 0, len(classInfo.FieldNames))
	for i, name := range classInfo.FieldNames {
		field := runtime.Field{Name: name}
		if i < len(classInfo.FieldDecorators) && len(classInfo.FieldDecorators[i]) > 0 {
			field.Decorators = decoratorsToAST(classInfo.FieldDecorators[i])
		}
		fields = append(fields, field)
	}
	return fields
}

// decoratorsToAST converts persisted DecoratorMetadata (chunk-format)
// back into ast.Decorator values for the reflection surface. Used when
// populating runtime.Class.Fields from the bytecode ClassInfo.
func decoratorsToAST(metas []runtime.DecoratorMetadata) []ast.Decorator {
	out := make([]ast.Decorator, 0, len(metas))
	for _, m := range metas {
		dec := ast.Decorator{Name: &ast.Identifier{Value: m.Name}}
		for _, arg := range m.Args {
			dec.Arguments = append(dec.Arguments, ast.CallArgument{Value: literalExpressionForValue(arg)})
		}
		for k, v := range m.NamedArgs {
			dec.Arguments = append(dec.Arguments, ast.CallArgument{
				Name:  &ast.Identifier{Value: k},
				Value: literalExpressionForValue(v),
			})
		}
		out = append(out, dec)
	}
	return out
}

// literalExpressionForValue is a thin shim that returns an AST node
// wrapping a runtime value. Only used for decorator arg reconstruction
// where the user previously passed a literal (string / int / etc.).
// Returns nil for anything more elaborate - the decorator-reading
// helpers know to fall back to the raw metadata in that case.
func literalExpressionForValue(v runtime.Value) ast.Expression {
	switch x := v.(type) {
	case runtime.String:
		return &ast.StringLiteral{Value: x.Value}
	case runtime.SmallInt:
		return &ast.IntegerLiteral{Value: fmt.Sprintf("%d", x.Value)}
	case runtime.Int:
		return &ast.IntegerLiteral{Value: x.Value.String()}
	case runtime.Float:
		return &ast.FloatLiteral{Value: fmt.Sprintf("%g", x.Value)}
	case runtime.Bool:
		return &ast.Literal{Value: x.Value}
	case runtime.Null:
		return &ast.Literal{Value: nil}
	}
	return nil
}

func (vm *VM) runtimeMethodWrappers(classIndex int64) map[string][]runtime.Function {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return nil
	}
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	loader := vm.moduleLoader
	methods := map[string][]runtime.Function{}
	for key, indices := range classInfo.Methods {
		methodKey := key
		methodIndices := append([]int64(nil), indices...)
		if len(methodIndices) == 0 {
			continue
		}
		name := methodKey
		firstIndex := methodIndices[0]
		if firstIndex >= 0 && int(firstIndex) < len(vm.curMod.Chunk.Functions) && vm.curMod.Chunk.Functions[firstIndex].Name != "" {
			name = vm.curMod.Chunk.Functions[firstIndex].Name
		}
		if loader != nil {
			// Data-only dispatch: the wrapper closes over the loader (not this VM), so it pins nothing and routes each call to an exclusive worker.
			methods[methodKey] = []runtime.Function{{
				Name:   name,
				Target: "method",
				Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
					if this == nil || this.Class == nil {
						return nil, fmt.Errorf("method receiver is not available")
					}
					return loader.CallModuleMethod(this.Class.Module, this.Class.Name, methodKey, this, args, nil)
				},
			}}
			continue
		}
		// No loader (single-VM context): the wrapper closes over this VM, so keep it out of the pool while an instance can still call it.
		vm.noteEscape()
		methods[methodKey] = []runtime.Function{{
			Name:   name,
			Target: "method",
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if this == nil {
					return nil, fmt.Errorf("method receiver is not available")
				}
				functionIndex, err := vm.selectRuntimeFunction(Instruction{}, methodKey, methodIndices, args, 1)
				if err != nil {
					return nil, err
				}
				if err := vm.ensureCallableDecorators(); err != nil {
					return nil, err
				}
				if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
					return vm.callCallableWithForwardThis(decorated, args, this)
				}
				callArgs := append([]runtime.Value{this}, args...)
				return vm.CallFunctionRaw(functionIndex, callArgs)
			},
		}}
	}
	return methods
}

func (vm *VM) initializeFields(instruction Instruction, instance *runtime.Instance, classIndex int64) error {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return vm.runtimeError(instruction, "class index out of range")
	}
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	if classInfo.ParentIndex >= 0 {
		if err := vm.initializeFields(instruction, instance, classInfo.ParentIndex); err != nil {
			return err
		}
	}
	for i, field := range classInfo.FieldNames {
		value := runtime.Value(runtime.Null{})
		if i < len(classInfo.FieldDefaults) && classInfo.FieldDefaults[i] >= 0 {
			defaultIndex := classInfo.FieldDefaults[i]
			if defaultIndex < 0 || int(defaultIndex) >= vm.constantsLen() {
				return vm.runtimeError(instruction, "field default constant out of range")
			}
			/* Clone container defaults so each new instance gets a
			 * fresh empty dict/list/set. Sharing across instances
			 * is the Python-style mutable-default trap. */
			value = cloneContainerDefault(vm.constantValue(defaultIndex))
		}
		instance.Fields[field] = value
	}
	for _, extra := range vm.interfaceExtraFields[classIndex] {
		if _, exists := instance.Fields[extra.name]; !exists {
			instance.Fields[extra.name] = runtime.Null{}
		}
	}
	return nil
}

func (vm *VM) getField(instruction Instruction, ip int) (int, error) {
	name, err := vm.constantString(instruction, "field name must be string")
	if err != nil {
		return 0, err
	}
	receiver, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if enumDef, ok := receiver.(*runtime.EnumDef); ok {
		for _, v := range enumDef.Variants {
			if strings.EqualFold(v.Name, name) {
				if v.FieldCount == 0 {
					vm.push(runtime.EnumVariant{Enum: enumDef, Variant: v.Name})
					return ip, nil
				}
				return 0, vm.runtimeError(instruction, "enum variant %s.%s requires arguments", enumDef.Name, v.Name)
			}
		}
		return 0, vm.runtimeError(instruction, "enum %s has no variant %s", enumDef.Name, name)
	}
	if ev, ok := receiver.(runtime.EnumVariant); ok {
		if name == "variant" {
			vm.push(runtime.String{Value: ev.Variant})
			return ip, nil
		}
		if name == "fields" {
			vm.push(&runtime.List{Elements: ev.Fields})
			return ip, nil
		}
		if name == "value" {
			if value, ok := runtime.EnumVariantBackingValue(ev); ok {
				vm.push(value)
				return ip, nil
			}
		}
		if idx, err2 := strconv.Atoi(name); err2 == nil && idx >= 0 && idx < len(ev.Fields) {
			vm.push(ev.Fields[idx])
			return ip, nil
		}
		return 0, vm.runtimeError(instruction, "enum variant %s.%s has no field %s", ev.Enum.Name, ev.Variant, name)
	}
	instance, ok := receiver.(*runtime.Instance)
	if !ok {
		if module, ok := receiver.(*runtime.Module); ok {
			value, ok := module.Exports[name]
			if !ok {
				return 0, vm.runtimeError(instruction, "module %s has no export %s", module.Name, name)
			}
			vm.push(value)
			return ip, nil
		}
		if typeVal, ok := receiver.(runtime.Type); ok {
			if v, ok := vm.builtinValue(typeVal.Name, name); ok {
				vm.push(v)
				return ip, nil
			}
			return 0, vm.runtimeError(instruction, "%s has no static member %s", typeVal.Name, name)
		}
		if class, ok := receiver.(runtime.BytecodeClass); ok {
			if class.Module == vm.moduleName && int(class.Index) >= 0 && int(class.Index) < len(vm.curMod.Chunk.Classes) {
				classInfo := vm.curMod.Chunk.Classes[class.Index]
				if value, found, rerr := vm.resolveStaticMember(classInfo, name); rerr != nil {
					return 0, vm.runtimeError(instruction, "%s", rerr.Error())
				} else if found {
					vm.push(value)
					return ip, nil
				}
				return 0, vm.runtimeError(instruction, "unknown static member %s.%s", class.Name, name)
			}
			if vm.moduleLoader != nil {
				if value, found := vm.moduleLoader.StaticValueForModuleClass(class.Module, class.Name, name); found {
					vm.push(value)
					return ip, nil
				}
				if value, found := vm.moduleLoader.StaticMethodValueForModuleClass(class.Module, class.Name, name); found {
					vm.push(value)
					return ip, nil
				}
			}
			return 0, vm.runtimeError(instruction, "unknown static member %s.%s", class.Name, name)
		}
		if errValue, ok := receiver.(runtime.Error); ok {
			switch name {
			case "class", "name":
				vm.push(runtime.String{Value: errValue.Class})
				return ip, nil
			case "message":
				vm.push(runtime.String{Value: errValue.Message})
				return ip, nil
			default:
				if errValue.Fields != nil {
					if v, ok := errValue.Fields[name]; ok {
						vm.push(v)
						return ip, nil
					}
				}
				return 0, vm.runtimeError(instruction, "%s has no field %s", errValue.Class, name)
			}
		}
		if task, ok := receiver.(*runtime.Task); ok {
			switch name {
			case "done":
				vm.push(runtime.Bool{Value: task.Done()})
				return ip, nil
			case "cancelled":
				vm.push(runtime.Bool{Value: task.Cancelled()})
				return ip, nil
			default:
				return 0, vm.runtimeError(instruction, "Task has no field %s", name)
			}
		}
		if r, ok := receiver.(runtime.Range); ok {
			switch name {
			case "start":
				vm.push(runtime.Int{Value: new(big.Int).Set(r.Start)})
				return ip, nil
			case "end":
				vm.push(runtime.Int{Value: new(big.Int).Set(r.End)})
				return ip, nil
			case "step":
				vm.push(runtime.Int{Value: new(big.Int).Set(r.Step)})
				return ip, nil
			case "length":
				vm.push(runtime.Int{Value: r.Length()})
				return ip, nil
			case "first":
				if r.Length().Sign() == 0 {
					vm.push(runtime.Null{})
					return ip, nil
				}
				vm.push(runtime.Int{Value: new(big.Int).Set(r.Start)})
				return ip, nil
			case "last":
				n := r.Length()
				if n.Sign() == 0 {
					vm.push(runtime.Null{})
					return ip, nil
				}
				last := new(big.Int).Mul(r.Step, new(big.Int).Sub(n, big.NewInt(1)))
				last.Add(last, r.Start)
				vm.push(runtime.Int{Value: last})
				return ip, nil
			default:
				return 0, vm.runtimeError(instruction, "range has no field %s", name)
			}
		}
		if name == "length" {
			switch v := receiver.(type) {
			case runtime.String:
				vm.push(runtime.SmallInt{Value: int64(len([]rune(v.Value)))})
				return ip, nil
			case runtime.Bytes:
				vm.push(runtime.SmallInt{Value: int64(len(v.Value))})
				return ip, nil
			case *runtime.List:
				vm.push(runtime.SmallInt{Value: int64(len(v.Elements))})
				return ip, nil
			case runtime.Dict:
				vm.push(runtime.SmallInt{Value: int64(v.Len())})
				return ip, nil
			case runtime.Set:
				vm.push(runtime.SmallInt{Value: int64(len(v.Elements))})
				return ip, nil
			}
		}
		return 0, vm.runtimeError(instruction, "%s has no field %s", receiver.TypeName(), name)
	}
	if value, ok := instance.GetField(name); ok {
		vm.cacheFieldShape(instance.Class, name, true)
		vm.push(value)
		return ip, nil
	}
	classInfo, classOk := vm.classInfo(instance.Class.Name)
	if !classOk {
		// Cross-module instance: bind a method value if one exists.
		if bound, ok := vm.bindCrossModuleMethod(instance, name); ok {
			vm.push(bound)
			return ip, nil
		}
		return 0, vm.runtimeError(instruction, "%s has no field %s", instance.Class.Name, name)
	}
	// A method resolves to a bound-method value, taking priority over __get to match the evaluator.
	if bound, ok := vm.bindLocalMethod(instance, classInfo, name); ok {
		vm.push(bound)
		return ip, nil
	}
	if vm.fieldLookupValid && vm.fieldLookupClass == instance.Class && vm.fieldLookupName == name && !vm.fieldLookupHasGetMag {
		return 0, vm.runtimeError(instruction, "%s has no field %s", instance.Class.Name, name)
	}
	indices, hasGet := vm.lookupMethod(classInfo, "__get")
	vm.cacheFieldShapeFull(instance.Class, name, false, hasGet, classInfo)
	if hasGet {
		functionIndex, err := vm.selectRuntimeFunction(instruction, "__get", indices, []runtime.Value{runtime.String{Value: name}}, 1)
		if err != nil {
			return 0, err
		}
		return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, runtime.String{Value: name}}, nil)
	}
	return 0, vm.runtimeError(instruction, "%s has no field %s", instance.Class.Name, name)
}

func (vm *VM) cacheFieldShape(class *runtime.Class, name string, onClass bool) {
	if vm.fieldLookupValid && vm.fieldLookupClass == class && vm.fieldLookupName == name && vm.fieldLookupOnClass == onClass {
		return
	}
	vm.fieldLookupClass = class
	vm.fieldLookupName = name
	vm.fieldLookupOnClass = onClass
	vm.fieldLookupHasGetMag = false
	vm.fieldLookupHasSetMag = false
	vm.fieldLookupValid = true
}

func (vm *VM) cacheFieldShapeFull(class *runtime.Class, name string, onClass, hasGet bool, classInfo ClassInfo) {
	_, hasSet := vm.lookupMethod(classInfo, "__set")
	vm.fieldLookupClass = class
	vm.fieldLookupName = name
	vm.fieldLookupOnClass = onClass
	vm.fieldLookupHasGetMag = hasGet
	vm.fieldLookupHasSetMag = hasSet
	vm.fieldLookupValid = true
}

func (vm *VM) setField(instruction Instruction, ip int) (int, error) {
	name, err := vm.constantString(instruction, "field name must be string")
	if err != nil {
		return 0, err
	}
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	receiver, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	instance, ok := receiver.(*runtime.Instance)
	if !ok {
		return 0, vm.runtimeError(instruction, "%s has no field %s", receiver.TypeName(), name)
	}
	if instance.Frozen {
		return vm.throwTyped(instruction, ip, "ImmutableError", "cannot modify frozen instance of "+instance.Class.Name)
	}
	if instance.LockedFields[name] {
		return vm.throwTyped(instruction, ip, "ImmutableError", "cannot modify immutable field "+name+" of "+instance.Class.Name)
	}
	if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
		transformed, err := vm.applyFieldDecorators(instruction, classInfo, name, value)
		if err != nil {
			return vm.propagateModuleError(instruction, ip, err)
		}
		value = transformed
	}
	if vm.fieldLookupValid && vm.fieldLookupClass == instance.Class && vm.fieldLookupName == name && vm.fieldLookupOnClass {
		instance.SetField(name, value)
		vm.push(value)
		return ip, nil
	}
	if instance.HasField(name) {
		vm.cacheFieldShape(instance.Class, name, true)
		instance.SetField(name, value)
		vm.push(value)
		return ip, nil
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		// Cross-module subclass: the instance was created from a class
		// in another chunk (e.g. a user subclass running its parent's
		// constructor here). The __set magic method can't be looked up
		// from this chunk; fall back to a direct field write to match
		// evaluator semantics.
		instance.SetField(name, value)
		vm.push(value)
		return ip, nil
	}
	indices, hasSet := vm.lookupMethod(classInfo, "__set")
	vm.cacheFieldShapeFull(instance.Class, name, false, false, classInfo)
	vm.fieldLookupHasSetMag = hasSet
	if hasSet {
		functionIndex, err := vm.selectRuntimeFunction(instruction, "__set", indices, []runtime.Value{runtime.String{Value: name}, value}, 1)
		if err != nil {
			return 0, err
		}
		return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, runtime.String{Value: name}, value}, value)
	}
	instance.SetField(name, value)
	vm.push(value)
	return ip, nil
}

func (vm *VM) callParentConstructor(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "parent constructor instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	argc := int(instruction.Operands[1])
	instance, args, err := vm.popInstanceAndArgs(instruction, argc)
	if err != nil {
		return 0, err
	}
	// Check if the parent is a builtin error class (ParentIndex == -1, ParentName set).
	if classIndex >= 0 && int(classIndex) < len(vm.curMod.Chunk.Classes) {
		classInfo := vm.curMod.Chunk.Classes[classIndex]
		if isBuiltinErrorClass(classInfo.ParentName) {
			// Capture the message from the first string argument for use in OpReturn.
			if len(args) >= 1 {
				if s, ok := args[0].(runtime.String); ok {
					instance.Fields["__parentMsg"] = s
				}
			}
			vm.push(runtime.Null{})
			return ip, nil
		}
		// Cross-module parent: ParentIndex == -1 but ParentName carries a
		// qualified `module.Class` reference. Dispatch through the module
		// loader so the parent's constructor runs against this instance
		// inside its own chunk.
		if classInfo.ParentIndex < 0 && strings.Contains(classInfo.ParentName, ".") {
			if vm.moduleLoader == nil {
				return 0, vm.runtimeError(instruction, "cross-module parent constructor requires a module loader")
			}
			module, parentClass, ok := splitQualifiedClassName(classInfo.ParentName)
			if !ok {
				return 0, vm.runtimeError(instruction, "cross-module parent name %q is malformed", classInfo.ParentName)
			}
			if _, err := vm.moduleLoader.LoadModule(module, module); err != nil {
				return 0, vm.runtimeError(instruction, "load parent module %s: %s", module, err.Error())
			}
			if _, err := vm.moduleLoader.CallParentInModule(module, parentClass, parentClass, instance, args, vm); err != nil {
				return 0, vm.callPropagate(instruction, err)
			}
			for _, f := range vm.moduleLoader.ImmutableFieldsForModuleClass(module, parentClass) {
				instance.LockField(f)
			}
			vm.push(runtime.Null{})
			return ip, nil
		}
	}
	parent, err := vm.parentClassInfo(instruction, classIndex)
	if err != nil {
		return 0, err
	}
	if len(parent.ConstructorIndices) == 0 {
		if argc != 0 {
			return 0, vm.runtimeError(instruction, "%s constructor expects no arguments", parent.Name)
		}
		for _, f := range parent.ImmutableFields {
			instance.LockField(f)
		}
		vm.push(runtime.Null{})
		return ip, nil
	}
	functionIndex, err := vm.selectRuntimeFunction(instruction, parent.Name, parent.ConstructorIndices, args, 1)
	if err != nil {
		return 0, err
	}
	callArgs := append([]runtime.Value{instance}, args...)
	nextIP, err := vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], callArgs, runtime.Null{})
	if err == nil && len(parent.ImmutableFields) > 0 && len(vm.frames) > 0 {
		vm.frames[len(vm.frames)-1].immutableFieldsToLock = parent.ImmutableFields
		vm.frames[len(vm.frames)-1].lockInstance = instance
	}
	return nextIP, err
}

func (vm *VM) callParentMethod(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 3 {
		return 0, vm.fatalError(instruction, "parent method instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	nameIndex := instruction.Operands[1]
	argc := int(instruction.Operands[2])
	if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
		return 0, vm.runtimeError(instruction, "method name constant out of range")
	}
	name, ok := vm.constantValue(nameIndex).(runtime.String)
	if !ok {
		return 0, vm.runtimeError(instruction, "method name constant must be string")
	}
	instance, args, err := vm.popInstanceAndArgs(instruction, argc)
	if err != nil {
		return 0, err
	}
	// `parent.method()`: lookup starts at the parent. Search the same-chunk
	// parent chain first; on a miss, hop to a cross-module ancestor (which may
	// sit above same-chunk intermediates, hence crossModuleBoundary).
	if classIndex >= 0 && int(classIndex) < len(vm.curMod.Chunk.Classes) {
		classInfo := vm.curMod.Chunk.Classes[classIndex]
		if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
			parent := vm.curMod.Chunk.Classes[classInfo.ParentIndex]
			if indices, ok := vm.lookupMethod(parent, name.Value); ok {
				functionIndex, err := vm.selectRuntimeFunction(instruction, name.Value, indices, args, 1)
				if err != nil {
					return 0, err
				}
				callArgs := append([]runtime.Value{instance}, args...)
				return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], callArgs, nil)
			}
		}
		boundary := classInfo
		if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
			boundary = vm.curMod.Chunk.Classes[classInfo.ParentIndex]
		}
		if module, parentClass, ok := vm.crossModuleBoundary(boundary); ok && vm.moduleLoader != nil {
			if _, err := vm.moduleLoader.LoadModule(module, module); err != nil {
				return 0, vm.runtimeError(instruction, "load parent module %s: %s", module, err.Error())
			}
			result, err := vm.moduleLoader.CallParentInModule(module, parentClass, name.Value, instance, args, vm)
			if err == nil {
				vm.push(result)
				return ip, nil
			}
			var notFound *runtime.MethodNotFoundError
			if !errors.As(err, &notFound) {
				return 0, vm.callPropagate(instruction, err)
			}
		}
	}
	parent, err := vm.parentClassInfo(instruction, classIndex)
	if err != nil {
		return 0, err
	}
	indices, ok := vm.lookupMethod(parent, name.Value)
	if !ok {
		return 0, vm.runtimeError(instruction, "unknown parent method %s.%s", parent.Name, name.Value)
	}
	functionIndex, err := vm.selectRuntimeFunction(instruction, name.Value, indices, args, 1)
	if err != nil {
		return 0, err
	}
	callArgs := append([]runtime.Value{instance}, args...)
	return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], callArgs, nil)
}

func (vm *VM) popInstanceAndArgs(instruction Instruction, argc int) (*runtime.Instance, []runtime.Value, error) {
	args := make([]runtime.Value, argc)
	for i := argc - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			return nil, nil, vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	receiver, err := vm.pop()
	if err != nil {
		return nil, nil, vm.callPropagate(instruction, err)
	}
	instance, ok := receiver.(*runtime.Instance)
	if !ok {
		return nil, nil, vm.runtimeError(instruction, "%s is not an instance", receiver.TypeName())
	}
	return instance, args, nil
}

// splitQualifiedClassName separates a `module.path.Class` reference into
// the canonical module path and the class name. Returns ok=false when
// the input has no dot (and therefore isn't qualified).
func splitQualifiedClassName(qualified string) (string, string, bool) {
	dot := strings.LastIndex(qualified, ".")
	if dot < 0 {
		return "", "", false
	}
	return qualified[:dot], qualified[dot+1:], true
}

// crossModuleBoundary finds the module+class to hop to when a cross-module
// ancestor sits above one or more local intermediates, not just the direct parent.
func (vm *VM) crossModuleBoundary(classInfo ClassInfo) (string, string, bool) {
	top := classInfo
	for top.ParentIndex >= 0 && int(top.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		top = vm.curMod.Chunk.Classes[top.ParentIndex]
	}
	if strings.Contains(top.ParentName, ".") {
		return splitQualifiedClassName(top.ParentName)
	}
	return "", "", false
}

func (vm *VM) parentClassInfo(instruction Instruction, classIndex int64) (ClassInfo, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return ClassInfo{}, vm.runtimeError(instruction, "class index out of range")
	}
	parentIndex := vm.curMod.Chunk.Classes[classIndex].ParentIndex
	if parentIndex < 0 || int(parentIndex) >= len(vm.curMod.Chunk.Classes) {
		return ClassInfo{}, vm.runtimeError(instruction, "%s has no parent class", vm.curMod.Chunk.Classes[classIndex].Name)
	}
	return vm.curMod.Chunk.Classes[parentIndex], nil
}

func (vm *VM) getStaticValue(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "static value instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	nameIndex := instruction.Operands[1]
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, vm.runtimeError(instruction, "class index out of range")
	}
	name, err := vm.constantStringAt(instruction, nameIndex, "static value name must be string")
	if err != nil {
		return 0, err
	}
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	if value, found, rerr := vm.resolveStaticMember(classInfo, name); rerr != nil {
		return 0, vm.runtimeError(instruction, "%s", rerr.Error())
	} else if found {
		vm.push(value)
		return ip, nil
	}
	if indices, ok := vm.lookupStaticMethod(classInfo, "__getStatic"); ok {
		functionIndex, err := vm.selectRuntimeFunction(instruction, "__getStatic", indices, []runtime.Value{runtime.String{Value: name}}, 0)
		if err != nil {
			return 0, err
		}
		return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{runtime.String{Value: name}}, nil)
	}
	return 0, vm.runtimeError(instruction, "unknown static member %s.%s", classInfo.Name, name)
}

// resolveStaticMember resolves a static const/let value or static-method value on classInfo or an ancestor; consts win over methods to match the evaluator's ordering (__getStatic is control flow, left to the caller).
func (vm *VM) resolveStaticMember(classInfo ClassInfo, name string) (runtime.Value, bool, error) {
	if constIndex, ok := vm.lookupStaticValue(classInfo, name); ok {
		if constIndex < 0 || int(constIndex) >= vm.constantsLen() {
			return nil, false, fmt.Errorf("static value constant out of range")
		}
		return vm.staticValueAt(constIndex), true, nil
	}
	module, parentClass, boundary := vm.crossModuleBoundary(classInfo)
	if boundary && vm.moduleLoader != nil {
		if value, found := vm.moduleLoader.StaticValueForModuleClass(module, parentClass, name); found {
			return value, true, nil
		}
	}
	if indices, ok := vm.lookupStaticMethod(classInfo, name); ok {
		return vm.staticMethodValue(indices), true, nil
	}
	if boundary && vm.moduleLoader != nil {
		if value, found := vm.moduleLoader.StaticMethodValueForModuleClass(module, parentClass, name); found {
			return value, true, nil
		}
	}
	return nil, false, nil
}

// staticMethodValue pins to the first overload, matching the evaluator's lookupStaticMethod (methods[0]) value semantics; unlike a free-function value it does not carry the whole overload set.
func (vm *VM) staticMethodValue(indices []int64) runtime.Value {
	funcIndex := indices[0]
	name := ""
	if int(funcIndex) >= 0 && int(funcIndex) < len(vm.curMod.Chunk.Functions) {
		name = vm.curMod.Chunk.Functions[funcIndex].Name
	}
	if err := vm.ensureCallableDecorators(); err == nil {
		if _, ok := vm.decoratedFuncs[funcIndex]; ok {
			return vm.decoratedStaticValue(funcIndex, name)
		}
	}
	return runtime.BytecodeClosure{FunctionIndex: funcIndex, Name: name, Module: vm.moduleName}
}

// decoratedStaticValue wraps a decorated static method as a callable value that applies the decorator; with a loader each call routes to a fresh home-module worker (goroutine-safe), otherwise it captures this VM.
func (vm *VM) decoratedStaticValue(funcIndex int64, name string) runtime.Value {
	loader := vm.moduleLoader
	module := vm.moduleName
	if loader == nil {
		vm.noteEscape()
		return runtime.Function{
			Name: name,
			Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				res, err, _ := vm.callBoundDecoratedValue(nil, funcIndex, args)
				return res, err
			},
		}
	}
	return runtime.Function{
		Name: name,
		Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			return loader.CallModuleMethodValue(module, funcIndex, nil, args, nil)
		},
		BridgeInvoke: func(host any, args []runtime.Value) (runtime.Value, error) {
			return loader.CallModuleMethodValue(module, funcIndex, nil, args, host.(*VM))
		},
	}
}

// StaticMethodValueFast builds a static-method callable value on this module's own VM so the loader can serve a cross-module `Class.staticMethod` value.
func (vm *VM) StaticMethodValueFast(classIndex int64, name string) (runtime.Value, bool) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return nil, false
	}
	indices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], name)
	if !ok {
		return nil, false
	}
	return vm.staticMethodValue(indices), true
}

// bindLocalMethod binds a same-chunk instance method's first overload to the receiver, pinning that overload to match the evaluator's methods[0] value semantics.
func (vm *VM) bindLocalMethod(instance *runtime.Instance, classInfo ClassInfo, name string) (runtime.Value, bool) {
	indices, ok := vm.lookupMethod(classInfo, name)
	if !ok || len(indices) == 0 {
		return nil, false
	}
	return vm.bindMethodValue(instance, vm.moduleName, indices[0], name), true
}

// bindCrossModuleMethod binds an instance method whose class lives in another chunk, resolving the first overload through the loader.
func (vm *VM) bindCrossModuleMethod(instance *runtime.Instance, name string) (runtime.Value, bool) {
	if vm.moduleLoader == nil || instance.Class == nil {
		return nil, false
	}
	module, funcIndex, ok := vm.moduleLoader.MethodFirstOverload(instance.Class.Module, instance.Class.Name, name)
	if !ok {
		return nil, false
	}
	return vm.bindMethodValue(instance, module, funcIndex, name), true
}

// bindMethodValue wraps a specific method function bound to a receiver as a callable value. With a loader each call routes to a fresh worker (goroutine-safe); without one it captures this VM, so the VM must stay out of the pool.
func (vm *VM) bindMethodValue(instance *runtime.Instance, module string, funcIndex int64, name string) runtime.Value {
	loader := vm.moduleLoader
	if loader == nil {
		vm.noteEscape()
		return runtime.Function{
			Name: name,
			Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if res, err, handled := vm.callBoundDecoratedValue(instance, funcIndex, args); handled {
					return res, err
				}
				return vm.CallFunctionRaw(funcIndex, append([]runtime.Value{instance}, args...))
			},
		}
	}
	return runtime.Function{
		Name: name,
		Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			return loader.CallModuleMethodValue(module, funcIndex, instance, args, nil)
		},
		BridgeInvoke: func(host any, args []runtime.Value) (runtime.Value, error) {
			return loader.CallModuleMethodValue(module, funcIndex, instance, args, host.(*VM))
		},
	}
}

func (vm *VM) setStaticValue(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "static assignment instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	nameIndex := instruction.Operands[1]
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, vm.runtimeError(instruction, "class index out of range")
	}
	name, err := vm.constantStringAt(instruction, nameIndex, "static value name must be string")
	if err != nil {
		return 0, err
	}
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	// Statics live in an overlay keyed by their declared constant-pool
	// index; the pool itself stays immutable so chunks can share it.
	for ci := classIndex; ci >= 0; {
		classInfo := vm.curMod.Chunk.Classes[ci]
		if constIdx, present := classInfo.StaticValues[name]; present && constIdx >= 0 && int(constIdx) < vm.constantsLen() {
			vm.writeStaticValue(constIdx, value)
			vm.push(value)
			return ip, nil
		}
		ci = classInfo.ParentIndex
	}
	if indices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], "__setStatic"); ok {
		functionIndex, err := vm.selectRuntimeFunction(instruction, "__setStatic", indices, []runtime.Value{runtime.String{Value: name}, value}, 0)
		if err != nil {
			return 0, err
		}
		return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{runtime.String{Value: name}, value}, nil)
	}
	return 0, vm.runtimeError(instruction, "unknown static member %s.%s", vm.curMod.Chunk.Classes[classIndex].Name, name)
}

func (vm *VM) callStaticMethod(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 3 {
		return 0, vm.fatalError(instruction, "static method instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	nameIndex := instruction.Operands[1]
	argc := int(instruction.Operands[2])
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, vm.runtimeError(instruction, "class index out of range")
	}
	name, err := vm.constantStringAt(instruction, nameIndex, "static method name must be string")
	if err != nil {
		return 0, err
	}
	args := make([]runtime.Value, argc)
	for i := argc - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	return vm.callStaticMethodWithArgs(instruction, ip, classIndex, name, args)
}

func (vm *VM) callStaticMethodSpread(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) < 3 {
		return 0, vm.fatalError(instruction, "static method spread instruction has invalid operands")
	}
	classIndex := instruction.Operands[0]
	nameIndex := instruction.Operands[1]
	argc := int(instruction.Operands[2])
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, vm.runtimeError(instruction, "class index out of range")
	}
	if len(instruction.Operands) != 3+argc {
		return 0, vm.runtimeError(instruction, "static method spread argument metadata mismatch")
	}
	name, err := vm.constantStringAt(instruction, nameIndex, "static method name must be string")
	if err != nil {
		return 0, err
	}
	args, names, fromSpread, hasNamed, err := vm.collectSpreadCallArgs(instruction, instruction.Operands[3:], true)
	if err != nil {
		return 0, err
	}
	if hasNamed {
		indices, found := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], name)
		if !found || len(indices) != 1 {
			return 0, vm.runtimeError(instruction, "cannot use named arguments with spread on static method %s.%s", vm.curMod.Chunk.Classes[classIndex].Name, name)
		}
		fn := vm.curMod.Chunk.Functions[indices[0]]
		fa, fnames := dropUnknownSpreadNames(args, names, fromSpread, fn.ParamNames)
		args, err = vm.orderRuntimeArguments(instruction, fn, fa, fnames, 0)
		if err != nil {
			return 0, err
		}
	}
	return vm.callStaticMethodWithArgs(instruction, ip, classIndex, name, args)
}

func (vm *VM) callStaticMethodWithArgs(instruction Instruction, ip int, classIndex int64, name string, args []runtime.Value) (int, error) {
	indices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], name)
	if !ok {
		// Same-chunk walk exhausted: the static method may live on a cross-module ancestor.
		if module, parentClass, boundary := vm.crossModuleBoundary(vm.curMod.Chunk.Classes[classIndex]); boundary && vm.moduleLoader != nil {
			if result, found, err := vm.moduleLoader.CallModuleStaticMethodByName(module, parentClass, name, args); found {
				if err != nil {
					return 0, vm.callPropagate(instruction, err)
				}
				vm.push(result)
				return ip, nil
			}
		}
		if fallbackIndices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], "__callStatic"); ok {
			functionIndex, err := vm.selectRuntimeFunction(instruction, "__callStatic", fallbackIndices, []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}}, 0)
			if err != nil {
				return 0, err
			}
			return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}}, nil)
		}
		return 0, vm.runtimeError(instruction, "unknown static method %s.%s", vm.curMod.Chunk.Classes[classIndex].Name, name)
	}
	functionIndex, err := vm.selectRuntimeFunction(instruction, name, indices, args, 0)
	if err != nil {
		return 0, err
	}
	if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
		if vm.curMod.Chunk.Functions[functionIndex].Async && !vm.syncMode {
			vm.push(vm.startAsyncCallable(decorated, args))
			return ip, nil
		}
		result, err := vm.callCallable(decorated, args)
		if err != nil {
			return vm.propagateDecoratedCallbackError(instruction, ip, err, vm.curMod.Chunk.Functions[functionIndex].Name)
		}
		vm.push(result)
		return ip, nil
	}
	return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], args, nil)
}

// inheritInstanceTypeBindings merges instance.TypeBindings into the current top call frame,
// adding any bindings not already set by inference from the function's own type params.
func (vm *VM) inheritInstanceTypeBindings(instance *runtime.Instance) {
	if len(instance.TypeBindings) == 0 || len(vm.frames) == 0 {
		return
	}
	frame := &vm.frames[len(vm.frames)-1]
	if frame.typeBindings == nil {
		frame.typeBindings = map[string]string{}
	}
	for name, typeName := range instance.TypeBindings {
		if _, exists := frame.typeBindings[name]; !exists {
			frame.typeBindings[name] = typeName
		}
	}
}

func (vm *VM) classInfo(name string) (ClassInfo, bool) {
	if cached, ok := vm.classInfoNameCache[name]; ok {
		return cached, true
	}
	index, ok := vm.curMod.classIndex[strings.ToLower(name)]
	if !ok || index < 0 || index >= len(vm.curMod.Chunk.Classes) {
		return ClassInfo{}, false
	}
	info := vm.curMod.Chunk.Classes[index]
	if vm.classInfoNameCache == nil {
		vm.classInfoNameCache = make(map[string]ClassInfo, 8)
	}
	vm.classInfoNameCache[name] = info
	return info, true
}

// staticValueAt resolves a static member: call-local writes first, then
// the chunk's shared overlay, then the declared constant.
func (vm *VM) staticValueAt(constIdx int64) runtime.Value {
	if vm.staticLocal != nil {
		if v, ok := vm.staticLocal[constIdx]; ok {
			return v
		}
	}
	if meta := vm.chunk.sharedMeta; meta != nil {
		meta.staticMu.RLock()
		v, ok := meta.staticOverlay[constIdx]
		meta.staticMu.RUnlock()
		if ok {
			return v
		}
	}
	return vm.constantValue(constIdx)
}

func (vm *VM) writeStaticValue(constIdx int64, value runtime.Value) {
	meta := vm.chunk.sharedMeta
	if meta == nil {
		vm.chunk.Constants[constIdx] = value
		return
	}
	if vm.staticsLocalOnly {
		if vm.staticLocal == nil {
			vm.staticLocal = map[int64]runtime.Value{}
		}
		vm.staticLocal[constIdx] = value
		return
	}
	meta.staticMu.Lock()
	if meta.staticOverlay == nil {
		meta.staticOverlay = map[int64]runtime.Value{}
	}
	meta.staticOverlay[constIdx] = value
	meta.staticMu.Unlock()
}

func (vm *VM) lookupMethod(classInfo ClassInfo, name string) ([]int64, bool) {
	return vm.lookupMethodLower(classInfo, strings.ToLower(name))
}

// lookupMethodLower is the cache-friendly entry point: callers that
// already have the lowercased method name pass it directly so the
// dispatch loop avoids repeated strings.ToLower(...) on every call.
// A single-slot cache short-circuits repeated lookups on the same
// (class, method) pair (the common hot-loop case).
func (vm *VM) lookupMethodLower(classInfo ClassInfo, lowered string) ([]int64, bool) {
	if vm.methodLookupValid && vm.methodLookupName == lowered && vm.methodLookupClass == classInfo.Name {
		return vm.methodLookupIndices, true
	}
	indices, ok := vm.lookupMethodLowerUncached(classInfo, lowered)
	if ok {
		vm.methodLookupClass = classInfo.Name
		vm.methodLookupName = lowered
		vm.methodLookupIndices = indices
		vm.methodLookupValid = true
	}
	return indices, ok
}

func (vm *VM) lookupMethodLowerUncached(classInfo ClassInfo, lowered string) ([]int64, bool) {
	if indices, ok := classInfo.Methods[lowered]; ok {
		return indices, true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.lookupMethodLowerUncached(vm.curMod.Chunk.Classes[classInfo.ParentIndex], lowered)
	}
	return nil, false
}

type crossModuleDefault struct {
	module string
	index  int64
}

type extraField struct {
	name string
	typ  string
}

// Walks the parent chain so a subclass inherits an ancestor's interface default.
func (vm *VM) lookupInterfaceFallback(className, methodName string) (crossModuleDefault, bool) {
	idx, ok := vm.curMod.classIndex[strings.ToLower(className)]
	if !ok {
		return crossModuleDefault{}, false
	}
	for ci := int64(idx); ci >= 0 && int(ci) < len(vm.curMod.Chunk.Classes); ci = vm.curMod.Chunk.Classes[ci].ParentIndex {
		if fallback, ok := vm.interfaceFallbacks[ci][methodName]; ok {
			return fallback, true
		}
	}
	return crossModuleDefault{}, false
}

func (vm *VM) callInterfaceDefault(instruction Instruction, ip int, instance *runtime.Instance, methodName string, args []runtime.Value) (int, error, bool) {
	fallback, ok := vm.lookupInterfaceFallback(instance.Class.Name, methodName)
	if !ok {
		return 0, nil, false
	}
	if vm.moduleLoader == nil {
		return 0, nil, false
	}
	fn := runtime.BytecodeFunction{Module: fallback.module, Index: fallback.index}
	callArgs := append([]runtime.Value{instance}, args...)
	result, err := vm.moduleLoader.CallModuleFunction(fn, callArgs, vm)
	if err != nil {
		return vm.propagateModuleErrorReturn(instruction, ip, err)
	}
	vm.push(result)
	return ip, nil, true
}

// callInterfaceDefaultValue invokes an interface default method and returns
// its value (the value-returning twin of callInterfaceDefault, for callers
// outside the method-call opcode flow such as the `in` operator).
func (vm *VM) callInterfaceDefaultValue(instance *runtime.Instance, methodName string, args []runtime.Value) (runtime.Value, bool, error) {
	fallback, ok := vm.lookupInterfaceFallback(instance.Class.Name, methodName)
	if !ok || vm.moduleLoader == nil {
		return nil, false, nil
	}
	fn := runtime.BytecodeFunction{Module: fallback.module, Index: fallback.index}
	result, err := vm.moduleLoader.CallModuleFunction(fn, append([]runtime.Value{instance}, args...), vm)
	return result, true, err
}

func (vm *VM) propagateModuleErrorReturn(instruction Instruction, ip int, err error) (int, error, bool) {
	nextIP, perr := vm.propagateModuleError(instruction, ip, err)
	return nextIP, perr, true
}

func (vm *VM) resolveCrossModuleInterfaceMembers(instruction Instruction, classIndex int64, classInfo ClassInfo) error {
	if len(classInfo.Implements) == 0 {
		return nil
	}
	declaredFields := map[string]bool{}
	for _, name := range classInfo.FieldNames {
		declaredFields[strings.ToLower(name)] = true
	}
	defaultSource := map[string]string{}
	for _, ifaceRef := range classInfo.Implements {
		module, name, ok := splitQualifiedClassName(ifaceRef)
		if !ok {
			continue
		}
		if vm.moduleLoader == nil {
			continue
		}
		if _, err := vm.moduleLoader.LoadModule(module, module); err != nil {
			continue
		}
		iface, ok := vm.moduleLoader.LookupModuleInterface(module, name)
		if !ok {
			continue
		}
		for i, fieldName := range iface.Fields {
			lower := strings.ToLower(fieldName)
			if declaredFields[lower] {
				continue
			}
			declaredFields[lower] = true
			typ := ""
			if i < len(iface.FieldTypes) {
				typ = iface.FieldTypes[i]
			}
			vm.interfaceExtraFields[classIndex] = append(vm.interfaceExtraFields[classIndex], extraField{name: fieldName, typ: typ})
		}
		for methodName, fnIndex := range iface.Defaults {
			if _, exists := classInfo.Methods[methodName]; exists {
				continue
			}
			if existing, ok := vm.interfaceFallbacks[classIndex][methodName]; ok {
				if existing.module != module || existing.index != fnIndex {
					return vm.runtimeError(instruction, "class %s inherits multiple defaults for %s from %s and %s; class must override", classInfo.Name, methodName, defaultSource[methodName], ifaceRef)
				}
				continue
			}
			if vm.interfaceFallbacks[classIndex] == nil {
				vm.interfaceFallbacks[classIndex] = map[string]crossModuleDefault{}
			}
			vm.interfaceFallbacks[classIndex][methodName] = crossModuleDefault{module: module, index: fnIndex}
			defaultSource[methodName] = ifaceRef
		}
	}
	return nil
}

// Reports (reason, true) when the class is @abstract or carries an
// unoverridden @abstract method, crossing module boundaries.
func (vm *VM) classAbstractnessReason(classInfo ClassInfo) (string, bool) {
	for _, dec := range classInfo.Decorators {
		if strings.EqualFold(dec.Name, "abstract") {
			return "cannot instantiate abstract class " + classInfo.Name, true
		}
	}
	overridden := map[string]bool{}
	abstractDecl := map[string]string{}
	walk := func(ci ClassInfo) {
		for method := range ci.Methods {
			isAbstract := false
			for _, dec := range ci.MethodDecorators[method] {
				if strings.EqualFold(dec.Name, "abstract") {
					isAbstract = true
					break
				}
			}
			if isAbstract {
				if !overridden[method] {
					if _, seen := abstractDecl[method]; !seen {
						abstractDecl[method] = ci.Name
					}
				}
			} else {
				overridden[method] = true
				delete(abstractDecl, method)
			}
		}
	}
	walk(classInfo)
	for ci := classInfo; ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(vm.curMod.Chunk.Classes); {
		ci = vm.curMod.Chunk.Classes[ci.ParentIndex]
		walk(ci)
	}
	// Same-chunk walk exhausted: abstract methods may be declared on a
	// cross-module ancestor that this chunk's overrides must satisfy.
	if module, parentClass, boundary := vm.crossModuleBoundary(classInfo); boundary && vm.moduleLoader != nil {
		for method, owner := range vm.moduleLoader.UnimplementedAbstractMethods(module, parentClass) {
			if !overridden[method] {
				if _, seen := abstractDecl[method]; !seen {
					abstractDecl[method] = owner
				}
			}
		}
	}
	if len(abstractDecl) == 0 {
		return "", false
	}
	var sample, sampleClass string
	for name, owner := range abstractDecl {
		if sample == "" || name < sample {
			sample = name
			sampleClass = owner
		}
	}
	return "cannot instantiate " + classInfo.Name + ": abstract method " + sampleClass + "." + sample + " is not implemented", true
}

func (vm *VM) lookupDunder(classInfo ClassInfo, canonical, legacy string) ([]int64, string, bool) {
	if indices, ok := vm.lookupMethod(classInfo, canonical); ok {
		return indices, canonical, true
	}
	if indices, ok := vm.lookupMethod(classInfo, legacy); ok {
		return indices, legacy, true
	}
	return nil, "", false
}

func (vm *VM) lookupStaticDunder(classInfo ClassInfo, canonical, legacy string) ([]int64, string, bool) {
	if indices, ok := vm.lookupStaticMethod(classInfo, canonical); ok {
		return indices, canonical, true
	}
	if indices, ok := vm.lookupStaticMethod(classInfo, legacy); ok {
		return indices, legacy, true
	}
	return nil, "", false
}

func (vm *VM) lookupStaticValue(classInfo ClassInfo, name string) (int64, bool) {
	if index, ok := classInfo.StaticValues[name]; ok {
		return index, true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.lookupStaticValue(vm.curMod.Chunk.Classes[classInfo.ParentIndex], name)
	}
	return 0, false
}

func (vm *VM) lookupStaticMethod(classInfo ClassInfo, name string) ([]int64, bool) {
	if indices, ok := classInfo.StaticMethods[strings.ToLower(name)]; ok {
		return indices, true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classInfo.ParentIndex], name)
	}
	return nil, false
}

func (vm *VM) classMatches(classInfo ClassInfo, target string) bool {
	target = stripModulePrefix(target)
	if strings.EqualFold(classInfo.Name, target) {
		return true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.classMatches(vm.curMod.Chunk.Classes[classInfo.ParentIndex], target)
	}
	// Same-chunk walk exhausted: a cross-module ancestor may still match.
	if module, parentClass, ok := vm.crossModuleBoundary(classInfo); ok && vm.moduleLoader != nil {
		return vm.moduleLoader.ModuleClassDescendsFrom(module, parentClass, target)
	}
	return false
}

// runtimeInterfacesForClass builds the runtime.Class.Implements slice
// for cross-chunk instanceof. Each implemented interface is captured by
// name (and a flattened parent chain when reachable in the current
// chunk) so `instance instanceof iface` resolves regardless of which
// module declared the interface.
func (vm *VM) runtimeInterfacesForClass(classInfo ClassInfo) []*runtime.Interface {
	if len(classInfo.Implements) == 0 {
		return nil
	}
	out := make([]*runtime.Interface, 0, len(classInfo.Implements))
	for _, name := range classInfo.Implements {
		out = append(out, vm.buildRuntimeInterface(name, map[string]bool{}))
	}
	return out
}

func (vm *VM) buildRuntimeInterface(name string, seen map[string]bool) *runtime.Interface {
	if name == "" || seen[strings.ToLower(name)] {
		return nil
	}
	seen[strings.ToLower(name)] = true
	module := vm.moduleName
	bareName := name
	if mod, bare, ok := splitQualifiedClassName(name); ok {
		module = mod
		bareName = bare
	}
	iface := &runtime.Interface{Name: name, Module: module}
	if info, ok := vm.lookupInterfaceInfo(name); ok {
		for _, parentName := range info.Parents {
			if p := vm.buildRuntimeInterface(parentName, seen); p != nil {
				iface.Parents = append(iface.Parents, p)
			}
		}
		return iface
	}
	// A foreign interface's parent chain lives in its declaring chunk, so resolve it through the loader; bare parents are same-module, qualify them (finding 9.17).
	if module != vm.moduleName && vm.moduleLoader != nil {
		if _, err := vm.moduleLoader.LoadModule(module, module); err == nil {
			if info, ok := vm.moduleLoader.LookupModuleInterface(module, bareName); ok {
				for _, parentName := range info.Parents {
					if !strings.Contains(parentName, ".") {
						parentName = module + "." + parentName
					}
					if p := vm.buildRuntimeInterface(parentName, seen); p != nil {
						iface.Parents = append(iface.Parents, p)
					}
				}
			}
		}
	}
	return iface
}

// runtimeClassForParent constructs a runtime.Class for the parent of the
// given ClassInfo, walking the ParentIndex chain. Returns nil when the
// class has no parent. The returned value's Parent field is set
// recursively so the full chain is reachable via Go pointers - this
// lets cross-module `instanceof` / `reflect.parent` find ancestors
// without falling back to the chunk-local classIndex.
// RuntimeClassValue builds the runtime.Class for a class at classIndex in the executing chunk, with its full ancestor chain (crossing module boundaries via the loader). The loader calls it to materialise a cross-module parent so instanceof / overload / inherited-dunder lookups can walk the whole hierarchy by pointer.
func (vm *VM) RuntimeClassValue(classIndex int64) *runtime.Class {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return nil
	}
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	return &runtime.Class{
		Name:           classInfo.Name,
		Module:         vm.moduleName,
		TypeParameters: append([]string(nil), classInfo.TypeParameters...),
		Methods:        vm.runtimeMethodWrappers(classIndex),
		MethodMetadata: vm.classFunctionMetadata(classInfo.Methods, "method", 1),
		StaticMetadata: vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0),
		Parent:         vm.runtimeClassForParent(classInfo),
		Implements:     vm.runtimeInterfacesForClass(classInfo),
		Fields:         vm.runtimeFieldsForClass(classInfo),
	}
}

func (vm *VM) runtimeClassForParent(classInfo ClassInfo) *runtime.Class {
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.RuntimeClassValue(classInfo.ParentIndex)
	}
	// Cross-module parent: resolve its runtime.Class through the loader (cached) so the chain crosses the module boundary instead of truncating to nil.
	if classInfo.ParentIndex < 0 && vm.moduleLoader != nil && strings.Contains(classInfo.ParentName, ".") {
		if module, parentClass, ok := splitQualifiedClassName(classInfo.ParentName); ok {
			if rc, found := vm.moduleLoader.RuntimeClassFor(module, parentClass); found {
				return rc
			}
		}
	}
	return nil
}

// runtimeClassMatches walks an instance's runtime.Class parent chain
// (Go pointer-following) and returns true if any ancestor name (or any
// implemented interface name) matches the target. Used as a fallback
// in instanceof / cast / catch when the instance's class was defined
// in a module other than the active chunk, so vm.classIndex doesn't
// have it.
func runtimeClassMatches(class *runtime.Class, target string) bool {
	for c := class; c != nil; c = c.Parent {
		if strings.EqualFold(stripModulePrefix(c.Name), target) {
			return true
		}
		for _, iface := range c.Implements {
			if iface != nil && runtimeInterfaceMatches(iface, target) {
				return true
			}
		}
	}
	return false
}

func runtimeInterfaceMatches(iface *runtime.Interface, target string) bool {
	if iface == nil {
		return false
	}
	if strings.EqualFold(stripModulePrefix(iface.Name), target) {
		return true
	}
	for _, parent := range iface.Parents {
		if runtimeInterfaceMatches(parent, target) {
			return true
		}
	}
	return false
}

func (vm *VM) classImplements(classInfo ClassInfo, target string) bool {
	target = stripModulePrefix(target)
	for _, implemented := range classInfo.Implements {
		if vm.interfaceMatches(implemented, target) {
			return true
		}
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.classImplements(vm.curMod.Chunk.Classes[classInfo.ParentIndex], target)
	}
	return false
}

func (vm *VM) checkTypeParamConstraints(instruction Instruction, function *FunctionInfo, typeBindings map[string]string) error {
	if len(function.TypeParamConstraintExprs) == 0 {
		return nil
	}
	for i, expr := range function.TypeParamConstraintExprs {
		if expr == "" || i >= len(function.TypeParameters) {
			continue
		}
		paramName := function.TypeParameters[i]
		boundName, ok := typeBindings[paramName]
		if !ok {
			continue
		}
		classInfo, found := vm.classInfo(boundName)
		if !vm.constraintExprSatisfied(boundName, classInfo, found, expr) {
			return vm.runtimeError(instruction, "type %s does not satisfy constraint %s for type parameter %s", boundName, stripOuterConstraintParens(strings.TrimSpace(expr)), paramName)
		}
	}
	return nil
}

func (vm *VM) constraintExprSatisfied(boundName string, classInfo ClassInfo, found bool, expr string) bool {
	expr = stripOuterConstraintParens(strings.TrimSpace(expr))
	if idx := topLevelConstraintOperator(expr, "|"); idx >= 0 {
		return vm.constraintExprSatisfied(boundName, classInfo, found, expr[:idx]) || vm.constraintExprSatisfied(boundName, classInfo, found, expr[idx+1:])
	}
	if idx := topLevelConstraintOperator(expr, "&"); idx >= 0 {
		return vm.constraintExprSatisfied(boundName, classInfo, found, expr[:idx]) && vm.constraintExprSatisfied(boundName, classInfo, found, expr[idx+1:])
	}
	// Identity covers primitives and exact class names.
	if strings.EqualFold(boundName, expr) {
		return true
	}
	if !found {
		return false
	}
	return vm.classImplements(classInfo, expr) || vm.classMatches(classInfo, expr)
}

func stripOuterConstraintParens(expr string) string {
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		depth := 0
		wraps := true
		for i, r := range expr {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i != len(expr)-1 {
					wraps = false
				}
			}
			if depth < 0 {
				return expr
			}
		}
		if !wraps || depth != 0 {
			return expr
		}
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
}

func topLevelConstraintOperator(expr string, op string) int {
	depth := 0
	for i, r := range expr {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && string(r) == op {
				return i
			}
		}
	}
	return -1
}

func (vm *VM) interfaceMatches(name string, target string) bool {
	/* Stored interface names may be module-qualified (e.g.
	 * "repository.Repository") for cross-module `implements` refs;
	 * the caller already strips the prefix from `target`, so match
	 * the stored name's trailing identifier too. */
	if strings.EqualFold(stripModulePrefix(name), target) {
		return true
	}
	for _, iface := range vm.chunk.Interfaces {
		if !strings.EqualFold(iface.Name, name) {
			continue
		}
		for _, parent := range iface.Parents {
			if vm.interfaceMatches(parent, target) {
				return true
			}
		}
	}
	return false
}

func (vm *VM) lookupInterfaceInfo(name string) (InterfaceInfo, bool) {
	for _, iface := range vm.chunk.Interfaces {
		if strings.EqualFold(iface.Name, name) {
			return iface, true
		}
	}
	return InterfaceInfo{}, false
}
