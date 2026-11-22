package bytecode

import (
	"strings"

	"geblang/internal/runtime"
)

// methodPatchFor returns the test.patch replacement for entering function with receiver.
func (vm *VM) methodPatchFor(function *FunctionInfo, receiver runtime.Value) (runtime.Value, bool) {
	if !runtime.MethodPatchesActive() || function.receiverOffset() == 0 || function.isCompiledConstructor() {
		return nil, false
	}
	instance, ok := receiver.(*runtime.Instance)
	if !ok {
		return nil, false
	}
	class, method, ok := strings.Cut(function.Name, ".")
	if !ok {
		return nil, false
	}
	return runtime.ResolveMethodPatch(instance, vm.moduleName, class, method)
}

// enterMethodPatch runs a replacement in place of a method body and leaves its result where the body's return value would go; no frame is pushed.
func (vm *VM) enterMethodPatch(instruction Instruction, ip int, replacement runtime.Value, args []runtime.Value, returnOverride runtime.Value) (int, error) {
	result, err := vm.callCallable(replacement, args)
	if err != nil {
		return vm.propagateCallbackError(instruction, ip, err)
	}
	if returnOverride != nil {
		result = returnOverride
	}
	vm.push(result)
	vm.methodPatchPushed = true
	return ip, nil
}

// startStackArgs enters function with arguments sliced off the stack top, then clears the vacated slots except one now holding a patched method's result.
func (vm *VM) startStackArgs(instruction Instruction, ip int, function *FunctionInfo, stackArgs []runtime.VMValue) (int, error) {
	vm.methodPatchPushed = false
	nextIP, err := vm.startFunctionVMValue(instruction, ip, function, stackArgs, nil)
	if vm.methodPatchPushed {
		vm.methodPatchPushed = false
		releaseBoxed(stackArgs[1:])
		return nextIP, err
	}
	releaseBoxed(stackArgs)
	return nextIP, err
}

// callDecoratedMethod runs a decorated method, or its patch in place of the whole decorated method.
func (vm *VM) callDecoratedMethod(functionIndex int64, decorated runtime.Value, args []runtime.Value, instance *runtime.Instance) (runtime.Value, error) {
	if instance != nil && runtime.MethodPatchesActive() {
		if replacement, ok := vm.methodPatchFor(&vm.curMod.Chunk.Functions[functionIndex], instance); ok {
			return vm.callCallable(replacement, append([]runtime.Value{instance}, args...))
		}
	}
	return vm.callCallableWithForwardThis(decorated, args, instance)
}

// RuntimeClassFor resolves a class value to the runtime class its instances carry.
func (vm *VM) RuntimeClassFor(class runtime.BytecodeClass) (*runtime.Class, bool) {
	if class.Module == vm.moduleName && class.Index >= 0 && int(class.Index) < len(vm.curMod.Chunk.Classes) && vm.curMod.Chunk.Classes[class.Index].Name == class.Name {
		return vm.RuntimeClassValue(class.Index), true
	}
	if vm.moduleLoader == nil {
		return nil, false
	}
	return vm.moduleLoader.RuntimeClassFor(class.Module, class.Name)
}

// CallableArity reports how many arguments a function value accepts; ok is false when it cannot tell.
func (vm *VM) CallableArity(fn runtime.Value) (required, max int, variadic, ok bool) {
	var module string
	var index int64
	switch f := fn.(type) {
	case runtime.BytecodeClosure:
		module, index = f.Module, f.FunctionIndex
	case runtime.BytecodeFunction:
		module, index = f.Module, f.Index
	default:
		return 0, 0, false, false
	}
	var info FunctionInfo
	if module == "" || module == vm.moduleName {
		if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
			return 0, 0, false, false
		}
		info = vm.curMod.Chunk.Functions[index]
	} else {
		if vm.moduleLoader == nil {
			return 0, 0, false, false
		}
		foreign, err := vm.moduleLoader.ModuleFunctionInfo(module, index)
		if err != nil {
			return 0, 0, false, false
		}
		info = foreign
	}
	max = len(info.ParamSlots)
	if info.Variadic && max > 0 {
		max--
		variadic = true
	}
	for i := 0; i < max; i++ {
		if i >= len(info.DefaultConstants) || info.DefaultConstants[i] < 0 {
			required++
		}
	}
	return required, max, variadic, true
}
