package bytecode

import (
	"fmt"
	"sort"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/typealias"
)

func (vm *VM) collectionTypeRef(name string) *ast.TypeRef {
	key := typeAliasKey{mod: vm.curMod, name: name}
	if ref, ok := vm.collectionTypeRefs[key]; ok {
		return ref
	}
	ref, _ := parser.ParseTypeRef(vm.expandTypeAliases(name))
	vm.resolveCollectionNominals(ref)
	if vm.collectionTypeRefs == nil {
		vm.collectionTypeRefs = map[typeAliasKey]*ast.TypeRef{}
	}
	vm.collectionTypeRefs[key] = ref
	return ref
}

func (vm *VM) collectionFieldType(name string, class *runtime.Class) string {
	name = vm.canonicalTypeQualifiers(name, class.Module)
	if class.Module == vm.moduleName || vm.moduleLoader == nil {
		return name
	}
	ref, err := parser.ParseTypeRef(name)
	if err != nil {
		return name
	}
	params := classTypeParamSet(class)
	return typealias.Expand(ref, func(leaf string) (string, bool) {
		if runtime.IsBuiltinTypeName(leaf) || params[strings.ToLower(leaf)] || strings.Contains(leaf, ".") {
			return "", false
		}
		if _, ok := vm.moduleLoader.RuntimeClassFor(class.Module, leaf); ok {
			return runtime.QualifiedTypeName(class.Module, leaf), true
		}
		if _, ok := vm.moduleLoader.LookupModuleInterface(class.Module, leaf); ok {
			return runtime.QualifiedTypeName(class.Module, leaf), true
		}
		return "", false
	}).String()
}

func (vm *VM) resolveCollectionNominals(ref *ast.TypeRef) {
	if ref == nil {
		return
	}
	for _, arg := range ref.Arguments {
		vm.resolveCollectionNominals(arg)
	}
	vm.resolveCollectionNominals(ref.Left)
	vm.resolveCollectionNominals(ref.Right)
	if ref.Operator != "" || runtime.IsBuiltinTypeName(ref.Name) {
		return
	}
	name := vm.canonicalNominalName(ref.Name)
	if module, bare, ok := vm.nominalIdentity(ref.Name); ok {
		name = runtime.QualifiedTypeName(module, bare)
	}
	ref.CanonicalName = name
	module, bare, qualified := splitQualifiedClassName(name)
	if !qualified {
		bare = name
	}
	if !qualified || module == vm.moduleName {
		if index, ok := vm.curMod.classIndex[strings.ToLower(bare)]; ok {
			ref.Supertypes = runtime.NominalSupertypes(vm.RuntimeClassValue(int64(index)), ref.Arguments)
			return
		}
		if _, ok := vm.lookupInterfaceInfo(bare); ok {
			ref.Supertypes = runtime.NominalSupertypes(vm.buildRuntimeInterface(bare, map[string]bool{}), ref.Arguments)
			return
		}
		for _, value := range vm.importScopeChunk().Constants {
			if enum, ok := value.(*runtime.EnumDef); ok && strings.EqualFold(enum.Name, bare) {
				ref.Supertypes = runtime.NominalSupertypes(enum, ref.Arguments)
				return
			}
		}
		return
	}
	if vm.moduleLoader != nil {
		if class, ok := vm.moduleLoader.RuntimeClassFor(module, bare); ok {
			ref.Supertypes = runtime.NominalSupertypes(class, ref.Arguments)
		} else {
			if module, err := vm.moduleLoader.LoadModule(module, module); err == nil {
				if enum, ok := module.Exports[bare].(*runtime.EnumDef); ok {
					ref.Supertypes = runtime.NominalSupertypes(enum, ref.Arguments)
					return
				}
			}
			ref.Supertypes = runtime.NominalSupertypes(vm.buildRuntimeInterface(name, map[string]bool{}), ref.Arguments)
		}
	}
}

func (vm *VM) bindCollection(value runtime.Value, spec vmTypeSpec, params map[string]bool, bindings map[string]string) bool {
	if !specHasCollection(spec) {
		return true
	}
	ref := vm.collectionTypeRef(spec.raw)
	if len(bindings) > 0 && runtime.TypeRefUsesTypeParameter(ref, params) {
		ref = typealias.Expand(ref, func(name string) (string, bool) { bound, ok := bindings[name]; return bound, ok })
		ref = vm.collectionTypeRef(ref.String())
	}
	if runtime.TypeRefUsesTypeParameter(ref, params) {
		return true
	}
	matched, handled := runtime.CheckCollectionType(value, ref, func(value runtime.Value, typ *ast.TypeRef) bool {
		return vm.matchValueToTypeSpec(params, value, vm.typeRefSpec(typ))
	})
	return !handled || matched
}

func (vm *VM) bindVMCollection(value runtime.VMValue, spec vmTypeSpec, params map[string]bool, bindings map[string]string) bool {
	if !specHasCollection(spec) {
		return true
	}
	boundary, ok := vm.collectionBoundary(value.ToValue(), spec, params, bindings)
	if !ok {
		return true
	}
	matched, handled := runtime.CheckCollectionType(boundary.Value, boundary.Type, func(value runtime.Value, ref *ast.TypeRef) bool {
		return vm.matchValueToTypeSpec(nil, value, vm.typeRefSpec(ref))
	})
	return !handled || matched
}

func specHasCollection(spec vmTypeSpec) bool {
	switch spec.kind {
	case vmTypeList, vmTypeDict, vmTypeSet:
		return true
	case vmTypeUnion, vmTypeIntersection:
		for _, arg := range spec.args {
			if specHasCollection(arg) {
				return true
			}
		}
	}
	return false
}

func (vm *VM) collectionBoundary(value runtime.Value, spec vmTypeSpec, params map[string]bool, bindings map[string]string) (runtime.CollectionBoundary, bool) {
	if !specHasCollection(spec) || runtime.HasCollectionType(value) {
		return runtime.CollectionBoundary{}, false
	}
	ref := vm.collectionTypeRef(spec.raw)
	if specMentionsTypeParam(spec, params) {
		ref = typealias.Expand(ref, func(name string) (string, bool) { value, ok := bindings[name]; return value, ok })
		ref = vm.collectionTypeRef(ref.String())
		if runtime.TypeRefUsesTypeParameter(ref, params) {
			return runtime.CollectionBoundary{}, false
		}
		if matched, handled := runtime.MatchCollectionType(value, ref, func(value runtime.Value, typ *ast.TypeRef) bool {
			return vm.matchValueToTypeSpec(nil, value, vm.typeRefSpec(typ))
		}); handled && !matched {
			return runtime.CollectionBoundary{}, false
		}
	}
	return runtime.CollectionBoundary{Value: value, Type: ref}, true
}

func (vm *VM) bindCollectionParameters(instruction Instruction) error {
	if len(instruction.Operands) != 1 || len(vm.frames) == 0 {
		return fmt.Errorf("invalid collection parameter binding")
	}
	index := instruction.Operands[0]
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return fmt.Errorf("collection function index out of range")
	}
	function := &vm.curMod.Chunk.Functions[index]
	frame := &vm.frames[len(vm.frames)-1]
	var boundaries []runtime.CollectionBoundary
	for i, spec := range function.paramTypeSpecs {
		if function.Variadic && i == len(function.ParamSlots)-1 && spec.raw != "" {
			spec = vm.typeSpec("list<" + spec.raw + ">")
		}
		if !specHasCollection(spec) {
			continue
		}
		value, err := vm.getLocal(function.ParamSlots[i])
		if err != nil {
			return err
		}
		if boundary, ok := vm.collectionBoundary(value, spec, function.matchTypeParams, frame.typeBindings); ok {
			boundaries = append(boundaries, boundary)
		}
	}
	if matched, _ := runtime.CheckCollectionBoundaries(boundaries, func(value runtime.Value, typ *ast.TypeRef) bool {
		return vm.matchValueToTypeSpec(nil, value, vm.typeRefSpec(typ))
	}); !matched {
		return fmt.Errorf("incompatible collection arguments")
	}
	return nil
}

func (vm *VM) bindClassStaticCollections(instruction Instruction, class ClassInfo) error {
	var names []string
	for name := range class.StaticValues {
		spec := vm.typeSpec(class.StaticTypes[name])
		if len(spec.args) == 0 || (spec.kind != vmTypeList && spec.kind != vmTypeDict && spec.kind != vmTypeSet) {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return class.StaticValues[names[i]] < class.StaticValues[names[j]] })
	for _, name := range names {
		index := class.StaticValues[name]
		spec := vm.typeSpec(class.StaticTypes[name])
		value := vm.staticValueAt(index)
		if runtime.IsNull(value) || runtime.HasCollectionType(value) {
			continue
		}
		value = cloneContainerDefault(value)
		var params map[string]bool
		if len(class.TypeParameters) > 0 {
			params = map[string]bool{}
			for _, param := range class.TypeParameters {
				params[strings.ToLower(param)] = true
			}
		}
		if !vm.bindCollection(value, spec, params, nil) {
			return vm.typedFault(instruction, "TypeError", vm.fieldMismatchMessage(value, spec.raw, class.Name, name))
		}
		vm.writeStaticValue(index, value)
	}
	return nil
}

func collectionWriteMatches(collection runtime.Value, argument int, value runtime.Value) bool {
	if runtime.HasCollectionType(collection) {
		return runtime.CollectionWriteMatches(collection, argument, value)
	}
	tags := runtime.CollectionElementTypes(collection)
	return argument >= len(tags) || vmValueSatisfiesElementTag(value, tags[argument])
}

func collectionExtendMatches(collection runtime.Value, values []runtime.Value) (bool, int) {
	if runtime.HasCollectionType(collection) {
		return runtime.CollectionExtendMatches(collection, values)
	}
	for i, value := range values {
		if !collectionWriteMatches(collection, 0, value) {
			return false, i
		}
	}
	return true, 0
}

func functionHasCollectionParameters(function FunctionInfo) bool {
	if len(function.Decorators) > 0 {
		return true
	}
	for i, spec := range function.paramTypeSpecs {
		if specHasCollection(spec) || (function.Variadic && i == len(function.paramTypeSpecs)-1 && spec.raw != "") {
			return true
		}
	}
	return false
}

func (vm *VM) prepareDecoratedCollectionArguments(signature runtime.Value, args []runtime.Value, host *VM) ([]runtime.Value, error) {
	var function runtime.BytecodeFunction
	var prefix []runtime.Value
	switch original := signature.(type) {
	case runtime.BytecodeFunction:
		function = original
	case runtime.BytecodeClosure:
		function = runtime.BytecodeFunction{Module: original.Module, Index: original.FunctionIndex, TypeBindings: original.TypeBindings}
		prefix = append(prefix, original.Upvalues...)
	default:
		return args, nil
	}
	info, ok := vm.functionInfoIn(function.Module, function.Index)
	if !ok || !functionHasCollectionParameters(info) {
		return args, nil
	}
	offset := int(info.UpvalueCount)
	receiverCount := 0
	if len(info.ParamNames) > offset && info.ParamNames[offset] == "this" {
		receiverCount = 1
	}
	if receiverCount > 0 {
		receiver := vm.forwardThis
		if receiver == nil && host != nil {
			receiver = host.forwardThis
		}
		if receiver == nil {
			return nil, fmt.Errorf("method receiver is not available")
		}
		prefix = append(prefix, receiver)
	}
	function.ParameterOnly, function.Async = true, false
	prepared, err := vm.callCallableModeHostedSync(function, append(prefix, args...), true, host, true)
	if err != nil {
		return nil, err
	}
	return prepared.(*runtime.List).Elements[offset+receiverCount:], nil
}

// PrepareCollectionParameters runs the original parameter prologue before a decorator wrapper.
func (vm *VM) PrepareCollectionParameters(function runtime.BytecodeFunction, args []runtime.Value) (runtime.Value, error) {
	if function.Index < 0 || int(function.Index) >= len(vm.curMod.Chunk.Functions) {
		return nil, fmt.Errorf("function index out of range")
	}
	info := vm.curMod.Chunk.Functions[function.Index]
	info.Entry, info.Async, info.IsGenerator = info.ParameterEntry, false, false
	if info.frameMeta != nil {
		meta := *info.frameMeta
		meta.returnSpec, meta.yieldSpec = nil, nil
		info.frameMeta = &meta
	}
	baseline := len(vm.frames)
	oldSuppress := vm.runSuppressCleanup
	restore := vm.enterDirectRun()
	defer func() { restore(); vm.runSuppressCleanup = oldSuppress }()
	nextIP, err := vm.startFunctionWithBindings(Instruction{}, 0, &info, args, nil, function.TypeBindings)
	if err != nil {
		return nil, err
	}
	vm.frames[len(vm.frames)-1].parameterOnly = true
	vm.runEntryIP, vm.runInlineExitDepth, vm.runSuppressCleanup = nextIP+1, baseline, true
	if err := vm.Run(); err != nil {
		return nil, err
	}
	result, err := vm.popVM()
	if err != nil {
		return nil, err
	}
	return result.ToValue(), nil
}

func collectionSpecUsesBuiltins(spec vmTypeSpec) bool {
	if spec.kind != vmTypeUnion && spec.kind != vmTypeIntersection && !runtime.IsBuiltinTypeName(spec.baseLower) {
		return false
	}
	for _, arg := range spec.args {
		if !collectionSpecUsesBuiltins(arg) {
			return false
		}
	}
	return true
}
