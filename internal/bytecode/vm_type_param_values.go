package bytecode

import (
	"strings"
	"sync"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

var bindingRefCache sync.Map

func parsedBindingRef(bound string) *ast.TypeRef {
	if cached, ok := bindingRefCache.Load(bound); ok {
		ref, _ := cached.(*ast.TypeRef)
		return ref
	}
	ref, err := parser.ParseTypeRef(bound)
	if err != nil {
		ref = nil
	}
	bindingRefCache.Store(bound, ref)
	return ref
}

// execRareOp runs opcodes kept out of the dispatch loop so they do not grow its hot path.
func (vm *VM) execRareOp(instruction Instruction) error {
	switch instruction.Op {
	case OpLoadInterface:
		return vm.loadInterface(instruction)
	case OpLoadTypeParam:
		return vm.loadTypeParam(instruction)
	}
	return vm.fatalError(instruction, "unknown opcode %d", instruction.Op)
}

func (vm *VM) loadTypeParam(instruction Instruction) error {
	if len(instruction.Operands) < 2 {
		return vm.fatalError(instruction, "type parameter load has invalid operands")
	}
	name, err := vm.constantStringAt(instruction, instruction.Operands[0], "type parameter name must be string")
	if err != nil {
		return err
	}
	owner, err := vm.constantStringAt(instruction, instruction.Operands[1], "type parameter owner must be string")
	if err != nil {
		return err
	}
	bound := ""
	if n := len(vm.frames); n > 0 {
		bound = vm.frames[n-1].typeBindings[name]
	}
	if bound == "" {
		return vm.callPropagate(instruction, runtime.TypeParamUnboundError(name, owner))
	}
	value, err := vm.typeParamBindingValue(name, bound)
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	if _, builtin := value.(runtime.Type); builtin && len(instruction.Operands) > 2 && instruction.Operands[2] == 1 {
		return vm.callPropagate(instruction, runtime.TypeParamNotConstructibleError(name, bound))
	}
	vm.push(value)
	return nil
}

func (vm *VM) typeParamBindingValue(name, bound string) (runtime.Value, error) {
	kind, className, args := runtime.ClassifyTypeParamBinding(parsedBindingRef(bound))
	switch kind {
	case runtime.TypeParamBindingBuiltin:
		return runtime.Type{Name: className}, nil
	case runtime.TypeParamBindingClass:
		class, ok := vm.classByBoundName(className)
		if !ok {
			return nil, runtime.TypeParamNotClassError(name, bound)
		}
		if len(args) == 0 {
			return class, nil
		}
		return runtime.BoundClass{Class: class, TypeArgs: args}, nil
	}
	return nil, runtime.TypeParamNotClassError(name, bound)
}

// classByBoundName resolves a binding's class name, which is spelled as at the binding call site and may belong to another module.
func (vm *VM) classByBoundName(name string) (runtime.Value, bool) {
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		prefix, member := name[:dot], name[dot+1:]
		if vm.moduleLoader != nil {
			if class, ok := vm.moduleLoader.ClassValueInModule(prefix, member); ok {
				return class, true
			}
		}
		name = member
	}
	class, ok := vm.resolveDeserializeClass(name)
	if !ok {
		return nil, false
	}
	if bc, isClass := class.(runtime.BytecodeClass); isClass && bc.Name != name {
		return nil, false
	}
	return class, true
}

func classTypeBindings(params []string, typeArgs []string) map[string]string {
	if len(typeArgs) == 0 {
		return nil
	}
	bindings := make(map[string]string, len(typeArgs))
	for i, param := range params {
		if i < len(typeArgs) {
			bindings[param] = typeArgs[i]
		}
	}
	return bindings
}

func (vm *VM) boundFieldTypeInfos(class runtime.BytecodeClass, typeArgs []string) []native.FieldTypeInfo {
	infos := vm.resolvedFieldTypeInfos(class)
	if len(typeArgs) == 0 || class.Index < 0 || int(class.Index) >= len(vm.chunk.Classes) {
		return infos
	}
	return native.BindFieldTypes(infos, classTypeBindings(vm.chunk.Classes[class.Index].TypeParameters, typeArgs), parsedBindingRef)
}
