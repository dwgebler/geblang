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
		vm.decoratedFuncs[int64(index)] = current
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
		indices := vm.decoratorFunctionIndices(decorator.Name)
		if len(indices) == 0 {
			continue
		}
		args, names, err := decoratorCallArguments(current, decorator)
		if err != nil {
			return nil, false, vm.decoratorFault(classInfo.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		functionIndex, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, decorator.Name, indices, args, names, 0)
		if err != nil {
			return nil, false, vm.decoratorFault(classInfo.DefLine, "decorator @"+decorator.Name+" cannot be called for class "+classInfo.Name+": ", err)
		}
		result, err := vm.CallFunctionRaw(functionIndex, ordered)
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
	var current runtime.Value = vm.bytecodeFunctionValue(index, true)
	decorated := false
	for i := len(function.Decorators) - 1; i >= 0; i-- {
		decorator := function.Decorators[i]
		if decorator.Target != "function" && decorator.Target != "method" && decorator.Target != "staticMethod" {
			continue
		}
		if decorator.Target == "method" {
			vm.methodReceiverFuncs[index] = true
		}
		indices := vm.decoratorFunctionIndices(decorator.Name)
		if len(indices) == 0 {
			continue
		}
		args, names, err := decoratorCallArguments(current, decorator)
		if err != nil {
			return nil, false, vm.decoratorFault(function.DefLine, "decorator @"+decorator.Name+": ", err)
		}
		functionIndex, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, decorator.Name, indices, args, names, 0)
		if err != nil {
			return nil, false, vm.decoratorFault(function.DefLine, "decorator @"+decorator.Name+" cannot be called for "+bareFunctionName(function.Name)+": ", err)
		}
		result, err := vm.CallFunctionRaw(functionIndex, ordered)
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
	originalOffset := 0
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
		if callable.Index < 0 || int(callable.Index) >= len(vm.curMod.Chunk.Functions) {
			return 0, 0, false, false
		}
		min, max, variadic := bytecodeFunctionArityRange(vm.curMod.Chunk.Functions[callable.Index], 0)
		return min, max, variadic, true
	case runtime.BytecodeClosure:
		if callable.FunctionIndex < 0 || int(callable.FunctionIndex) >= len(vm.curMod.Chunk.Functions) {
			return 0, 0, false, false
		}
		info := vm.curMod.Chunk.Functions[callable.FunctionIndex]
		min, max, variadic := bytecodeFunctionArityRange(info, int(info.UpvalueCount))
		return min, max, variadic, true
	default:
		return 0, 0, false, false
	}
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
	function := runtime.BytecodeFunction{Index: index, Raw: raw}
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return function
	}
	info := vm.curMod.Chunk.Functions[index]
	function.Module = vm.moduleName
	function.Name = info.Name
	function.Doc = info.Doc
	function.TypeParameters = append([]string(nil), info.TypeParameters...)
	function.Parameters = parameterMetadataFromFunctionInfo(info, 0)
	function.ReturnType = info.ReturnType
	function.Async = info.Async
	function.Variadic = info.Variadic
	function.Decorators = append([]runtime.DecoratorMetadata(nil), info.Decorators...)
	function.DefLine = info.DefLine
	function.DefColumn = info.DefColumn
	return function
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
		indices := vm.decoratorFunctionIndices(dec.Name)
		if len(indices) == 0 {
			continue
		}
		callArgs := make([]runtime.Value, 0, len(dec.Args)+1)
		callArgs = append(callArgs, dec.Args...)
		callArgs = append(callArgs, value)
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
