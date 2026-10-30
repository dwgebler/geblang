package bytecode

import (
	"fmt"
	argbinding "geblang/internal/binding"
	"geblang/internal/overload"
	"geblang/internal/runtime"
)

func (vm *VM) bindNestedFunction(instruction Instruction) error {
	value, err := vm.pop()
	if err != nil {
		return err
	}
	index := instruction.Operands[1]
	info := vm.curMod.Chunk.Functions[index]
	if len(info.Decorators) > 0 {
		value, _, err = vm.applyFunctionDecorators(info, value)
		if err != nil {
			return err
		}
	}
	previous, err := vm.getLocal(instruction.Operands[0])
	if err != nil {
		return err
	}
	addition := vm.boundFunctionValue(index, value)
	bound := addition
	if old, ok := previous.(runtime.OverloadedFunction); ok {
		bound = old
		bound.Overloads = append(append([]runtime.Function(nil), old.Overloads...), addition.Overloads...)
		bound.Callables = append(append([]runtime.Value(nil), old.Callables...), addition.Callables...)
		bound.Indices = append(append([]int64(nil), old.Indices...), index)
		bound.Defaults = append(append([][]runtime.Value(nil), old.Defaults...), addition.Defaults...)
	}

	return vm.setLocal(instruction.Operands[0], bound)
}

func (vm *VM) boundFunctionValue(index int64, value runtime.Value) runtime.OverloadedFunction {
	info := vm.curMod.Chunk.Functions[index]
	offset := int(info.UpvalueCount)
	if vm.methodReceiverFuncs[index] {
		offset++
	}
	metadata := runtime.Function{Name: info.Name, Parameters: overloadParams(&info)[offset:], TypeParameters: info.TypeParameters, Async: info.Async}
	defaults := make([]runtime.Value, len(metadata.Parameters))
	for i := range defaults {
		if constant := info.DefaultConstants[offset+i]; constant >= 0 {
			defaults[i] = vm.constantValue(constant)
		}
	}
	return runtime.OverloadedFunction{Name: info.Name, Module: vm.moduleName, Overloads: []runtime.Function{metadata}, Callables: []runtime.Value{value}, Indices: []int64{index}, Defaults: [][]runtime.Value{defaults}}
}

func bindBoundArguments(set runtime.OverloadedFunction, pos int, args []runtime.Value, names []string) ([]runtime.Value, error) {
	fn := set.Overloads[pos]
	sig := argbinding.Signature{FuncName: set.Name, ParamNames: make([]string, len(fn.Parameters)), HasDefault: make([]bool, len(fn.Parameters))}
	for i, param := range fn.Parameters {
		sig.ParamNames[i] = param.Name.Value
		sig.HasDefault[i] = param.Default != nil
	}
	sig.Variadic = len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	if sig.Variadic {
		sig.HasDefault[len(sig.HasDefault)-1] = true
	}
	provided := make([]argbinding.Arg, len(args))
	for i := range args {
		if i < len(names) {
			provided[i].Name = names[i]
		}
	}
	result, err := argbinding.Order(sig, provided)
	if err != nil {
		return nil, err
	}
	ordered := make([]runtime.Value, len(result.Slots))
	for i, slot := range result.Slots {
		if slot != argbinding.DefaultSlot && args[slot] != nil {
			ordered[i] = args[slot]
		} else if i < len(set.Defaults[pos]) {
			ordered[i] = cloneContainerDefault(set.Defaults[pos][i])
		}
	}
	if sig.Variadic && len(ordered) > 0 && ordered[len(ordered)-1] == nil {
		ordered = ordered[:len(ordered)-1]
	}
	for _, arg := range result.TailArgs {
		ordered = append(ordered, args[arg])
	}
	return ordered, nil
}

func (vm *VM) selectBoundNamed(instruction Instruction, set runtime.OverloadedFunction, args []runtime.Value, names []string) (runtime.Value, []runtime.Value, error) {
	if len(set.Callables) == 1 {
		ordered, err := bindBoundArguments(set, 0, args, names)
		return set, ordered, err
	}
	var selected runtime.Value
	var ordered []runtime.Value
	for pos := range set.Callables {
		candidate, err := bindBoundArguments(set, pos, args, names)
		if err != nil {
			continue
		}
		if _, err := overload.Select(set.Name, []runtime.Function{set.Overloads[pos]}, candidate); err != nil {
			continue
		}
		if selected != nil {
			return nil, nil, fmt.Errorf("ambiguous overload for %s", set.Name)
		}
		selected, ordered = runtime.OverloadedFunction{Name: set.Name, Module: set.Module, Indices: []int64{set.Indices[pos]}, Overloads: []runtime.Function{set.Overloads[pos]}, Callables: []runtime.Value{set.Callables[pos]}, Defaults: [][]runtime.Value{set.Defaults[pos]}, TypeArguments: set.TypeArguments}, candidate
	}
	if selected == nil {
		return nil, nil, fmt.Errorf("no matching overload for %s", set.Name)
	}
	return selected, ordered, nil
}
