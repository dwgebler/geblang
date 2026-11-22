package bytecode

import "geblang/internal/runtime"

func notInvokable(value runtime.Value) bool {
	switch v := value.(type) {
	case runtime.SmallInt, runtime.Int, runtime.Float, runtime.Decimal, runtime.Bool, runtime.Null,
		runtime.String, *runtime.List, runtime.Dict, runtime.Set, runtime.Bytes, runtime.Type,
		runtime.Range, runtime.EnumVariant, runtime.Error, *runtime.Complex:
		return true
	case *runtime.Instance:
		return !runtime.IsCallableValue(v)
	}
	return false
}

// calleeText is the source text the compiler recorded for the call at instruction, or "" when none was.
func (vm *VM) calleeText(instruction Instruction) string {
	return vm.curMod.Chunk.CalleeNames[CallSiteKey(instruction.Line, instruction.Column)]
}

func calleeOrValue(text string) string {
	if text == "" {
		return "value"
	}
	return text
}

func (vm *VM) notCallableError(instruction Instruction, ip int) (int, error) {
	return vm.throwTyped(instruction, ip, "RuntimeError", calleeOrValue(vm.calleeText(instruction))+" is not callable")
}
