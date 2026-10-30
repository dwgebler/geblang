package bytecode

import (
	"errors"
	"fmt"

	"geblang/internal/runtime"
)

func isPendingDefault(value runtime.Value) bool {
	_, ok := value.(runtime.PendingDefault)
	return ok
}

// paramMismatchMessage reports the TypeError message when arg violates parameter i's declared type.
func (vm *VM) paramMismatchMessage(function *FunctionInfo, i int, arg runtime.Value, typeParams map[string]bool, inherited map[string]string) (string, bool) {
	var matches bool
	if i < len(function.paramTypeSpecs) && function.paramTypeSpecs[i].raw != "" {
		matches = vm.matchValueToTypeSpecWith(typeParams, inherited, arg, function.paramTypeSpecs[i])
	} else {
		matches = vm.matchValueToTypeStrWith(typeParams, inherited, arg, function.ParamTypes[i])
	}
	if matches {
		return "", true
	}
	declared := vm.expandTypeAliases(function.ParamTypes[i])
	suffix := vm.collectionMismatchSuffixStr(arg, declared)
	gotName := vm.descriptiveRuntimeTypeName(arg)
	if suffix != "" {
		gotName = arg.TypeName()
	}
	if i < len(function.ParamNames) {
		return fmt.Sprintf("%s expects %s for parameter '%s', got %s%s", function.Name, declared, function.displayParamName(i), gotName, suffix), false
	}
	return fmt.Sprintf("%s expects %s, got %s%s", function.Name, declared, gotName, suffix), false
}

func (vm *VM) argSlotPending(slot int64) (bool, error) {
	value, err := vm.getLocal(slot)
	if err != nil {
		return false, err
	}
	return isPendingDefault(value), nil
}

// storeDefaultArg checks a computed default against its parameter type, as the call-entry check does for supplied arguments.
func (vm *VM) storeDefaultArg(instruction Instruction) error {
	value, err := vm.pop()
	if err != nil {
		return err
	}
	slot, funcIndex, paramIndex := instruction.Operands[0], instruction.Operands[1], int(instruction.Operands[2])
	function := &vm.curMod.Chunk.Functions[funcIndex]
	if paramIndex < len(function.ParamTypes) && function.ParamTypes[paramIndex] != "" {
		var receiver runtime.Value
		if len(function.ParamNames) > 0 && function.ParamNames[0] == "this" {
			if receiver, err = vm.getLocal(function.ParamSlots[0]); err != nil {
				return err
			}
		}
		inherited := mergedTypeBindings(vm.frames[len(vm.frames)-1].typeBindings, receiverTypeBindings(function, receiver))
		if msg, ok := vm.paramMismatchMessage(function, paramIndex, value, function.typeParamSet, inherited); !ok {
			return vmTypedError{class: "TypeError", message: msg}
		}
	}
	return vm.setLocal(slot, value)
}

// fieldDefaultValue runs a computed field default inline so its frame sits under the instantiation site.
func (vm *VM) fieldDefaultValue(funcIndex int64, instance *runtime.Instance, line int32) (runtime.Value, error) {
	saved := vm.hofCallLine
	vm.hofCallLine = int(line)
	defer func() { vm.hofCallLine = saved }()
	return vm.callBytecodeInline(funcIndex, []runtime.Value{instance})
}

func (vm *VM) checkFieldDefault(instruction Instruction) error {
	classInfo := vm.curMod.Chunk.Classes[instruction.Operands[0]]
	value, err := vm.peek()
	if err != nil {
		return err
	}
	instance, err := vm.getLocal(0)
	if err != nil {
		return err
	}
	owner, _ := instance.(*runtime.Instance)
	if msg, ok := vm.fieldWriteMismatch(owner, classInfo.FieldNames[instruction.Operands[1]], value); !ok {
		return vmTypedError{class: "TypeError", message: msg}
	}
	return nil
}

// initializeForeignFieldDefaults fills fields declared by an ancestor compiled in another module.
func (vm *VM) initializeForeignFieldDefaults(instance *runtime.Instance, class *runtime.Class, line int32) error {
	if class.Parent != nil {
		if err := vm.initializeForeignFieldDefaults(instance, class.Parent, line); err != nil {
			return err
		}
	}
	for _, field := range class.Fields {
		value := field.DefaultValue
		if field.DefaultFunc != nil {
			saved := vm.hofCallLine
			vm.hofCallLine = int(line)
			computed, err := field.DefaultFunc(vm, instance)
			vm.hofCallLine = saved
			if err != nil {
				return vm.boundaryError(err, int(line))
			}
			value = computed
		} else if value == nil {
			value = runtime.Null{}
		} else {
			value = cloneContainerDefault(value)
		}
		instance.Fields[field.Name] = value
	}
	return nil
}

// foreignFieldDefault lets another module's VM compute this module's field default on an exclusive worker.
func (vm *VM) foreignFieldDefault(funcIndex int64) func(caller any, instance *runtime.Instance) (runtime.Value, error) {
	if loader := vm.moduleLoader; loader != nil {
		function := runtime.BytecodeFunction{Module: vm.moduleName, Index: funcIndex}
		return func(caller any, instance *runtime.Instance) (runtime.Value, error) {
			invoker, _ := caller.(*VM)
			return loader.CallModuleFunction(function, []runtime.Value{instance}, invoker)
		}
	}
	vm.noteEscape()
	return func(caller any, instance *runtime.Instance) (runtime.Value, error) {
		return vm.CallFunctionRaw(funcIndex, []runtime.Value{instance})
	}
}

// boundaryError splices this VM's frames onto an error raised on another module's worker.
func (vm *VM) boundaryError(err error, line int) error {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		return vmThrownError{err: vm.mergeBoundaryFrames(boundaryErrorLine(thrown.err, line), line)}
	}
	var rt *vmRuntimeError
	if errors.As(err, &rt) {
		return vmThrownError{err: vm.mergeBoundaryFrames(boundaryErrorLine(rt.toRuntimeError(), line), line)}
	}
	return err
}

// undefinedGlobalError names a global read before its declaration runs, matching the evaluator.
func (vm *VM) undefinedGlobalError(slot int64) error {
	if names := vm.curMod.Chunk.GlobalNames; int(slot) < len(names) && names[slot] != "" {
		return fmt.Errorf("%q is not declared", names[slot])
	}
	return fmt.Errorf("global is undefined")
}

// ModuleImportError re-raises a module's failed top-level run as a throw with a module frame; nil leaves err as is.
func ModuleImportError(module string, err error) error {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		return vmThrownError{err: runtime.WithModuleFrame(thrown.err, module)}
	}
	var rt *vmRuntimeError
	if errors.As(err, &rt) {
		return vmThrownError{err: runtime.WithModuleFrame(rt.toRuntimeError(), module)}
	}
	return nil
}
