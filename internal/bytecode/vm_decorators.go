package bytecode

import (
	"errors"
	"fmt"
	"geblang/internal/runtime"
	"sort"
	"strings"
)

func (vm *VM) ensureCallableDecorators() error {
	if vm.decoratorsApplied || vm.applyingDecorators {
		return nil
	}
	return vm.applyCallableDecorators()
}

func (vm *VM) applyCallableDecorators() error {
	vm.applyingDecorators = true
	defer func() {
		vm.applyingDecorators = false
	}()
	// Restore leaves these nil for an undecorated module; lazy decorator application (this path) writes them.
	if vm.decoratedFuncs == nil {
		vm.decoratedFuncs = map[int64]runtime.Value{}
	}
	if vm.methodReceiverFuncs == nil {
		vm.methodReceiverFuncs = map[int64]bool{}
	}
	for index, function := range vm.curMod.Chunk.Functions {
		if function.UpvalueCount > 0 {
			continue
		}
		current, decorated, err := vm.applyCallableDecoratorsForFunction(int64(index), function)
		if err != nil {
			return err
		}
		if !decorated {
			continue
		}
		if raw, ok := current.(runtime.BytecodeFunction); ok && raw.Raw && raw.Index == int64(index) {
			delete(vm.decoratedFuncs, int64(index))
			continue
		}
		vm.decoratedFuncs[int64(index)] = vm.boundFunctionValue(int64(index), current)
	}
	vm.decoratorsApplied = true
	return nil
}

func (vm *VM) applyCallableDecoratorsForClass(index int64, classInfo ClassInfo) (runtime.Value, bool, error) {
	if len(classInfo.Decorators) == 0 {
		return nil, false, nil
	}
	methodMetadata := vm.classFunctionMetadata(classInfo.Methods, "method", 1)
	staticMetadata := vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0)
	initial := runtime.BytecodeClass{
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
		MethodMetadata:      methodMetadata,
		StaticMetadata:      staticMetadata,
		ConstructorMetadata: vm.constructorFunctionMetadata(classInfo.ConstructorIndices),
		Raw:                 true,
	}
	var current runtime.Value = initial
	decorated := false
	for i := len(classInfo.Decorators) - 1; i >= 0; i-- {
		decorator := classInfo.Decorators[i]
		if decorator.Name == "" {
			continue
		}
		args, names, err := decoratorCallArguments(current, decorator)
		if err != nil {
			return nil, false, vm.decoratorFault(classInfo.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		result, found, err := vm.callDecorator(decorator.Name, args, names)
		if !found {
			continue
		}
		var bind *decoratorBindError
		if errors.As(err, &bind) {
			return nil, false, vm.decoratorFault(classInfo.DefLine, "decorator @"+decorator.Name+" cannot be called for class "+classInfo.Name+": ", bind.err)
		}
		if err != nil {
			return nil, false, vm.decoratorFault(classInfo.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		switch result.(type) {
		case runtime.BytecodeClass, runtime.BytecodeClosure, runtime.Function, runtime.OverloadedFunction, runtime.BytecodeFunction, runtime.DecoratorTarget:
			current = result
			decorated = true
		default:
			return nil, false, vm.decoratorFault(classInfo.DefLine, fmt.Sprintf("decorator @%s must return class or callable, got %s", decorator.Name, result.TypeName()), nil)
		}
	}
	return current, decorated, nil
}

func (vm *VM) applyCallableDecoratorsForFunction(index int64, function FunctionInfo) (runtime.Value, bool, error) {
	if len(function.Decorators) == 0 {
		return nil, false, nil
	}
	return vm.applyFunctionDecorators(function, vm.bytecodeFunctionValue(index, true))
}

func (vm *VM) applyFunctionDecorators(function FunctionInfo, current runtime.Value) (runtime.Value, bool, error) {
	decorated := false
	for i := len(function.Decorators) - 1; i >= 0; i-- {
		decorator := function.Decorators[i]
		if decorator.Target != "function" && decorator.Target != "method" && decorator.Target != "staticMethod" {
			continue
		}
		if decorator.Target == "method" {
			if raw, ok := current.(runtime.BytecodeFunction); ok {
				vm.methodReceiverFuncs[raw.Index] = true
			}
		}
		args, names, err := decoratorCallArguments(current, decorator)
		if err != nil {
			return nil, false, vm.decoratorFault(function.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		result, found, err := vm.callDecorator(decorator.Name, args, names)
		if !found {
			continue
		}
		var bind *decoratorBindError
		if errors.As(err, &bind) {
			return nil, false, vm.decoratorFault(function.DefLine, "decorator @"+decorator.Name+" cannot be called for "+bareFunctionName(function.Name)+": ", bind.err)
		}
		if err != nil {
			return nil, false, vm.decoratorFault(function.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		switch result.(type) {
		case runtime.BytecodeFunction, runtime.BytecodeClosure:
			if !vm.decoratorWrapperCompatible(function, decorator.Target, result) {
				return nil, false, vm.decoratorFault(function.DefLine, fmt.Sprintf("decorator @%s returned incompatible wrapper for %s", decorator.Name, bareFunctionName(function.Name)), nil)
			}
			current = result
			decorated = true
		default:
			return nil, false, vm.decoratorFault(function.DefLine, fmt.Sprintf("decorator @%s must return function, got %s", decorator.Name, result.TypeName()), nil)
		}
	}
	return current, decorated, nil
}

func (vm *VM) decoratorWrapperCompatible(original FunctionInfo, target string, wrapper runtime.Value) bool {
	originalOffset := int(original.UpvalueCount)
	if target == "method" && len(original.ParamNames) > 0 {
		originalOffset = 1
	}
	origMin, origMax, origVariadic := bytecodeFunctionArityRange(original, originalOffset)
	wrapMin, wrapMax, wrapVariadic, ok := vm.callableArityRange(wrapper)
	if !ok {
		return false
	}
	if wrapMin > origMin {
		return false
	}
	if origVariadic {
		return wrapVariadic
	}
	return wrapVariadic || wrapMax >= origMax
}

func (vm *VM) callableArityRange(value runtime.Value) (int, int, bool, bool) {
	switch callable := value.(type) {
	case runtime.BytecodeFunction:
		info, ok := vm.functionInfoIn(callable.Module, callable.Index)
		if !ok {
			return 0, 0, false, false
		}
		min, max, variadic := bytecodeFunctionArityRange(info, 0)
		return min, max, variadic, true
	case runtime.BytecodeClosure:
		info, ok := vm.functionInfoIn(callable.Module, callable.FunctionIndex)
		if !ok {
			return 0, 0, false, false
		}
		min, max, variadic := bytecodeFunctionArityRange(info, int(info.UpvalueCount))
		return min, max, variadic, true
	default:
		return 0, 0, false, false
	}
}

// functionInfoIn finds a function declaration in its home chunk, which differs from the current one for a callable from another module.
func (vm *VM) functionInfoIn(module string, index int64) (FunctionInfo, bool) {
	if module != vm.moduleName && vm.moduleLoader != nil {
		info, err := vm.moduleLoader.ModuleFunctionInfo(module, index)
		return info, err == nil
	}
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return FunctionInfo{}, false
	}
	return vm.curMod.Chunk.Functions[index], true
}

func bytecodeFunctionArityRange(function FunctionInfo, offset int) (int, int, bool) {
	if offset > len(function.ParamNames) {
		offset = len(function.ParamNames)
	}
	max := len(function.ParamNames) - offset
	variadic := function.Variadic && max > 0
	min := max
	if variadic {
		// The variadic slot is always optional; defaults may sit before it.
		min--
	}
	for min > 0 {
		paramIndex := offset + min - 1
		if paramIndex >= len(function.DefaultConstants) || function.DefaultConstants[paramIndex] < 0 {
			break
		}
		min--
	}
	return min, max, variadic
}

// decoratorBindError marks a decorator whose arguments match none of its signatures, as opposed to one that failed while running.
type decoratorBindError struct{ err error }

func (e *decoratorBindError) Error() string { return e.err.Error() }

// callDecorator calls the function a decorator name resolves to (local or `from`-imported); found is false for a metadata-only decorator.
func (vm *VM) callDecorator(name string, args []runtime.Value, names []string) (runtime.Value, bool, error) {
	if indices := vm.decoratorFunctionIndices(name); len(indices) > 0 {
		functionIndex, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, name, indices, args, names, 0)
		if err != nil {
			return nil, true, &decoratorBindError{err}
		}
		result, err := vm.CallFunctionRaw(functionIndex, ordered)
		return result, true, err
	}
	imported, err := vm.importedDecorator(name)
	if err != nil || imported == nil {
		return nil, err != nil, err
	}
	ordered, err := vm.reorderCallableNamedArgs(Instruction{}, imported, args, names)
	if err != nil {
		return nil, true, &decoratorBindError{err}
	}
	result, err := vm.callCallable(imported, ordered)
	return result, true, err
}

// importedDecorator resolves a name bound by `from m import f` to m's exported callable, loading m if needed.
func (vm *VM) importedDecorator(name string) (runtime.Value, error) {
	target, ok := vm.curMod.Chunk.FromImports[name]
	if !ok || vm.moduleLoader == nil {
		return nil, nil
	}
	dot := strings.LastIndexByte(target, '.')
	if dot < 0 {
		return nil, nil
	}
	module, err := vm.moduleLoader.LoadModule(target[:dot], "")
	if err != nil {
		return nil, err
	}
	value, ok := module.Exports[target[dot+1:]]
	if !ok || !runtime.IsCallableValue(value) {
		return nil, nil
	}
	return value, nil
}

func (vm *VM) decoratorFunctionIndices(name string) []int64 {
	if name == "" {
		return nil
	}
	var indices []int64
	lowerName := strings.ToLower(name)
	for index, function := range vm.curMod.Chunk.Functions {
		if strings.EqualFold(function.Name, lowerName) {
			indices = append(indices, int64(index))
		}
	}
	return indices
}

func (vm *VM) bytecodeFunctionValue(index int64, raw bool) runtime.BytecodeFunction {
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return runtime.BytecodeFunction{Index: index, Raw: raw}
	}
	return bytecodeFunctionFromInfo(vm.curMod.Chunk.Functions[index], index, vm.moduleName, raw)
}

func bytecodeFunctionFromInfo(info FunctionInfo, index int64, module string, raw bool) runtime.BytecodeFunction {
	return runtime.BytecodeFunction{
		Index:          index,
		Raw:            raw,
		Module:         module,
		Name:           info.Name,
		Doc:            info.Doc,
		TypeParameters: append([]string(nil), info.TypeParameters...),
		Parameters:     parameterMetadataFromFunctionInfo(info, 0),
		ReturnType:     info.ReturnType,
		Async:          info.Async,
		Variadic:       info.Variadic,
		Decorators:     append([]runtime.DecoratorMetadata(nil), info.Decorators...),
		DefLine:        info.DefLine,
		DefColumn:      info.DefColumn,
	}
}

func decoratorCallArguments(current runtime.Value, decorator runtime.DecoratorMetadata) ([]runtime.Value, []string, error) {
	args := make([]runtime.Value, 0, 1+len(decorator.Args)+len(decorator.NamedArgs))
	names := make([]string, 0, 1+len(decorator.Args)+len(decorator.NamedArgs))
	args = append(args, current)
	names = append(names, "")
	for _, arg := range decorator.Args {
		if err := validateDecoratorArgument(arg); err != nil {
			return nil, nil, err
		}
		args = append(args, arg)
		names = append(names, "")
	}
	namedNames := make([]string, 0, len(decorator.NamedArgs))
	for name := range decorator.NamedArgs {
		namedNames = append(namedNames, name)
	}
	sort.Strings(namedNames)
	for _, name := range namedNames {
		arg := decorator.NamedArgs[name]
		if err := validateDecoratorArgument(arg); err != nil {
			return nil, nil, err
		}
		args = append(args, arg)
		names = append(names, name)
	}
	return args, names, nil
}

func validateDecoratorArgument(value runtime.Value) error {
	if value == nil {
		return nil
	}
	if errValue, ok := value.(runtime.Error); ok {
		return fmt.Errorf("%s", errValue.Message)
	}
	return nil
}

// fieldBarrier runs a field's write-barrier decorators in the module whose class declares the field, whichever module performs the assignment.
func (vm *VM) fieldBarrier(instruction Instruction, instance *runtime.Instance, name string, value runtime.Value) (runtime.Value, error) {
	if instance.Class == nil {
		return value, nil
	}
	if instance.Class.Module != vm.moduleName {
		if vm.moduleLoader == nil {
			return value, nil
		}
		return vm.moduleLoader.ApplyModuleFieldDecorators(instance.Class.Module, instance.Class.Name, name, value, vm)
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return value, nil
	}
	return vm.applyInheritedFieldDecorators(instruction, classInfo, name, value)
}

// applyInheritedFieldDecorators finds the nearest class in the chain declaring decorators for the field, crossing into a parent's module when the chain leaves this chunk.
func (vm *VM) applyInheritedFieldDecorators(instruction Instruction, classInfo ClassInfo, name string, value runtime.Value) (runtime.Value, error) {
	for ci := classInfo; ; {
		if fieldHasDecorators(ci, name) {
			return vm.applyFieldDecorators(instruction, ci, name, value)
		}
		if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(vm.curMod.Chunk.Classes) {
			ci = vm.curMod.Chunk.Classes[ci.ParentIndex]
			continue
		}
		if module, parent, ok := splitQualifiedClassName(ci.ParentName); ok && vm.moduleLoader != nil {
			return vm.moduleLoader.ApplyModuleFieldDecorators(module, parent, name, value, vm)
		}
		return value, nil
	}
}

// ApplyFieldDecoratorsForClass runs the write barrier for a field assigned on an instance of the class at classIndex, from another module.
func (vm *VM) ApplyFieldDecoratorsForClass(classIndex int64, name string, value runtime.Value) (runtime.Value, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return value, nil
	}
	return vm.applyInheritedFieldDecorators(Instruction{}, vm.curMod.Chunk.Classes[classIndex], name, value)
}

func fieldHasDecorators(classInfo ClassInfo, name string) bool {
	for i, fname := range classInfo.FieldNames {
		if strings.EqualFold(fname, name) {
			return i < len(classInfo.FieldDecorators) && len(classInfo.FieldDecorators[i]) > 0
		}
	}
	return false
}

func (vm *VM) applyFieldDecorators(instruction Instruction, classInfo ClassInfo, name string, value runtime.Value) (runtime.Value, error) {
	fieldIndex := -1
	for i, fname := range classInfo.FieldNames {
		if strings.EqualFold(fname, name) {
			fieldIndex = i
			break
		}
	}
	if fieldIndex < 0 || fieldIndex >= len(classInfo.FieldDecorators) {
		return value, nil
	}
	decorators := classInfo.FieldDecorators[fieldIndex]
	if len(decorators) == 0 {
		return value, nil
	}
	for i := len(decorators) - 1; i >= 0; i-- {
		dec := decorators[i]
		if dec.Name == "" {
			continue
		}
		callArgs := make([]runtime.Value, 0, len(dec.Args)+1)
		callArgs = append(callArgs, dec.Args...)
		callArgs = append(callArgs, value)
		indices := vm.decoratorFunctionIndices(dec.Name)
		if len(indices) == 0 {
			imported, err := vm.importedDecorator(dec.Name)
			if err != nil {
				return nil, err
			}
			if imported == nil {
				continue
			}
			result, err := vm.callCallable(imported, callArgs)
			if err != nil {
				return nil, err
			}
			value = result
			continue
		}
		functionIndex, err := vm.selectRuntimeFunction(instruction, dec.Name, indices, callArgs, 0)
		if err != nil {
			return nil, vm.runtimeError(instruction, "field decorator @%s: %s", dec.Name, err.Error())
		}
		result, err := vm.CallFunction(functionIndex, callArgs)
		if err != nil {
			return nil, err
		}
		value = result
	}
	return value, nil
}

// decoratorFault raises a decorator failure at the decorated declaration, keeping the cause's error class.
func (vm *VM) decoratorFault(line int64, message string, cause error) error {
	class := "RuntimeError"
	if cause != nil {
		detail := cause.Error()
		var thrown vmThrownError
		var fault *vmRuntimeError
		switch {
		case errors.As(cause, &thrown):
			class, detail = thrown.err.Class, thrown.err.Message
		case errors.As(cause, &fault):
			detail = fault.message
		}
		message += detail
	}
	return vm.typedFault(Instruction{Line: int32(line)}, class, message)
}

func bareFunctionName(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		return name[dot+1:]
	}
	return name
}
