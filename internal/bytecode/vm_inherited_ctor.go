package bytecode

import (
	"geblang/internal/runtime"
)

// constructFrom runs the nearest constructor at or above classInfo that takes no arguments; one requiring arguments is skipped.
func (vm *VM) constructFrom(classInfo ClassInfo, instance *runtime.Instance, line int32, inline bool) error {
	if len(classInfo.ConstructorIndices) == 0 {
		return vm.constructAbove(classInfo, instance, line, inline)
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, classInfo.Name, classInfo.ConstructorIndices, nil, 1)
	if err != nil {
		return nil
	}
	if inline {
		saved := vm.hofCallLine
		vm.hofCallLine = int(line)
		_, err = vm.callBytecodeInline(functionIndex, []runtime.Value{instance})
		vm.hofCallLine = saved
	} else {
		_, err = vm.CallFunction(functionIndex, []runtime.Value{instance})
	}
	if err != nil {
		return err
	}
	for _, f := range classInfo.ImmutableFields {
		instance.LockField(f)
	}
	return nil
}

// constructAbove runs the inherited zero-argument constructor of a class that declares none.
func (vm *VM) constructAbove(classInfo ClassInfo, instance *runtime.Instance, line int32, inline bool) error {
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.curMod.Chunk.Classes) {
		return vm.constructFrom(vm.curMod.Chunk.Classes[classInfo.ParentIndex], instance, line, inline)
	}
	module, parentClass, ok := splitQualifiedClassName(classInfo.ParentName)
	if classInfo.ParentIndex >= 0 || !ok || vm.moduleLoader == nil {
		return nil
	}
	if _, err := vm.moduleLoader.LoadModule(module, module); err != nil {
		return err
	}
	err := vm.moduleLoader.CallInheritedConstructorInModule(module, parentClass, instance, vm)
	if err != nil && inline {
		return vm.boundaryError(err, int(line))
	}
	return err
}

// ConstructInheritedFrom is the loader entry for an inherited constructor declared in this module.
func (vm *VM) ConstructInheritedFrom(className string, instance *runtime.Instance) error {
	classInfo, ok := vm.classInfo(className)
	if !ok {
		return nil
	}
	return vm.constructFrom(classInfo, instance, 0, false)
}
