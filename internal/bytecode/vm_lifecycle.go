package bytecode

import (
	"errors"
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"os"
	"strings"
	"sync"
)

func NewVM(chunk Chunk, stdout io.Writer) *VM {
	classIndex := make(map[string]int, len(chunk.Classes))
	for i, classInfo := range chunk.Classes {
		classIndex[strings.ToLower(classInfo.Name)] = i
	}
	initialDefers := make([][]deferredAction, 1, 64)
	// Pre-allocate stack; 1024 entries x 32 bytes = 32 KB, avoids early growth.
	initialStack := make([]runtime.VMValue, 0, 1024)
	// Pre-size globals/locals from compiled counts; 256 is the historical minimum.
	globalSize := int(chunk.GlobalCount)
	if globalSize < 256 {
		globalSize = 256
	}
	localsCap := int(chunk.TopLevelLocalCount)
	if localsCap < 256 {
		localsCap = 256
	}
	if meta := chunk.sharedMeta; meta != nil {
		// Shared-meta path: reuse the memoised functions, class index, and type-assert specs.
		meta.prepare(chunk)
		chunk.Functions = meta.preparedFunctions
		vm := &VM{chunk: chunk, stdout: stdout, globals: make([]runtime.VMValue, globalSize), dirtyGlobals: make([]bool, globalSize), localsStack: make([]runtime.VMValue, 0, localsCap), stack: initialStack, maxCallDepth: DefaultMaxCallDepth, frames: make([]callFrame, 0, 256), defers: initialDefers, classIndex: meta.classIndex, natives: native.NewBuiltinRegistry(), nativeCache: make([]native.Function, len(chunk.Constants)), decoratedFuncs: map[int64]runtime.Value{}, decoratedClasses: map[int64]runtime.Value{}, rawFunctionCalls: map[int64]bool{}, methodReceiverFuncs: map[int64]bool{}, interfaceFallbacks: map[int64]map[string]crossModuleDefault{}, interfaceExtraFields: map[int64][]extraField{}, typeSpecCache: map[string]vmTypeSpec{}, runInlineExitDepth: -1}
		vm.typeAssertSpecs = meta.typeAssertSpecs
		vm.requiresCallSitePolymorphism = meta.requiresCallSitePolymorphism
		vm.curMod = &ModuleContext{Chunk: chunk, classIndex: meta.classIndex}
		vm.curGlobals = vm.globals
		vm.instanceInvokerFn = vm.invokeInstanceMethod
		vm.classDeserializerFn = vm.deserializeIntoClass
		vm.natives.SetConversionContext(native.ConversionContext{InstanceInvoker: vm.instanceInvokerFn, ClassDeserializer: vm.classDeserializerFn})
		return vm
	}
	// Detach Functions so per-VM metadata mutations don't race concurrent VMs sharing the chunk.
	chunk.Functions = append([]FunctionInfo(nil), chunk.Functions...)
	vm := &VM{chunk: chunk, stdout: stdout, globals: make([]runtime.VMValue, globalSize), dirtyGlobals: make([]bool, globalSize), localsStack: make([]runtime.VMValue, 0, localsCap), stack: initialStack, maxCallDepth: DefaultMaxCallDepth, frames: make([]callFrame, 0, 256), defers: initialDefers, classIndex: classIndex, natives: native.NewBuiltinRegistry(), nativeCache: make([]native.Function, len(chunk.Constants)), decoratedFuncs: map[int64]runtime.Value{}, decoratedClasses: map[int64]runtime.Value{}, rawFunctionCalls: map[int64]bool{}, methodReceiverFuncs: map[int64]bool{}, interfaceFallbacks: map[int64]map[string]crossModuleDefault{}, interfaceExtraFields: map[int64][]extraField{}, typeSpecCache: map[string]vmTypeSpec{}, runInlineExitDepth: -1}
	vm.curMod = &ModuleContext{Chunk: chunk, classIndex: classIndex}
	vm.curGlobals = vm.globals
	vm.prepareFunctionTypeMetadata()
	vm.instanceInvokerFn = vm.invokeInstanceMethod
	vm.classDeserializerFn = vm.deserializeIntoClass
	vm.natives.SetConversionContext(native.ConversionContext{InstanceInvoker: vm.instanceInvokerFn, ClassDeserializer: vm.classDeserializerFn})
	return vm
}

// deserializeIntoClass implements native.ClassDeserializer for
// the bytecode VM. Tries static __deserialize__ first; falls back
// to positional constructor calls with dict keys matched against
// constructor parameter names.
func (vm *VM) deserializeIntoClass(classValue runtime.Value, value runtime.Value) (runtime.Value, error) {
	class, ok := classValue.(runtime.BytecodeClass)
	if !ok {
		// `reflect.class("Name")` on the VM can compile-time-resolve
		// to a DecoratorTarget; promote it to a BytecodeClass via
		// the chunk's class index so json.parseAs et al. can
		// deserialize through the same path.
		if target, isTarget := classValue.(runtime.DecoratorTarget); isTarget && target.Target == "class" && target.Class != nil {
			if idx, ok := vm.classIndex[strings.ToLower(target.Class.Name)]; ok && int(idx) < len(vm.chunk.Classes) {
				class = vm.bytecodeClassFromInfo(vm.chunk.Classes[idx], int64(idx))
			} else if vm.moduleLoader != nil {
				if v, found := vm.moduleLoader.FindClassByName(target.Class.Name); found {
					if bc, isClass := v.(runtime.BytecodeClass); isClass {
						class = bc
					} else {
						return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
					}
				} else {
					return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
				}
			} else {
				return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
			}
		} else {
			return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
		}
	}
	// If the class was declared in a different chunk than the one
	// this VM is bound to, dispatch to a sub-VM via the moduleLoader
	// so the indices resolve against the right chunk. The check
	// covers both directions: a sub-VM holding a main-chunk class
	// (class.Module=="" while vm.moduleName!=""), and the main VM
	// holding an imported-module class.
	if vm.moduleLoader != nil && class.Module != vm.moduleName {
		return vm.moduleLoader.DeserializeModuleClass(class, value)
	}
	if class.Index < 0 || int(class.Index) >= len(vm.chunk.Classes) {
		return nil, fmt.Errorf("deserialize %s: class index out of range", class.Name)
	}
	classInfo := vm.chunk.Classes[class.Index]
	if indices, name, ok := vm.lookupStaticDunder(classInfo, "__deserialize", "__deserialize__"); ok && len(indices) > 0 {
		args := []runtime.Value{value}
		functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 0)
		if err != nil {
			return nil, fmt.Errorf("deserialize %s: %w", class.Name, err)
		}
		return vm.CallFunctionRaw(functionIndex, args)
	}
	if result, found, err := vm.deserializeViaInheritedFactory(class, value); found || err != nil {
		return result, err
	}
	dict, ok := value.(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("deserialize %s: expected dict, got %s", class.Name, value.TypeName())
	}
	hydrated, herr := native.HydrateNestedClassFields(vmFieldTypeInfos(classInfo), dict, vm.resolveDeserializeClass, vm.deserializeIntoClass)
	if herr != nil {
		return nil, herr
	}
	dict = hydrated
	if len(classInfo.ConstructorIndices) == 0 {
		// Classes without an explicit constructor get their fields
		// populated directly from the dict. Matches the evaluator's
		// data-class behaviour so `json.parseAs(text, MyDTO)` is
		// usable without writing a constructor.
		v, err := vm.ConstructClass(class.Index, nil)
		if err != nil {
			return nil, err
		}
		instance, ok := v.(*runtime.Instance)
		if !ok {
			return v, nil
		}
		for _, fieldName := range classInfo.FieldNames {
			key := runtime.String{Value: fieldName}
			entry, hit := dict.GetEntry(native.DictKey(key))
			if hit {
				instance.Fields[fieldName] = entry.Value
			}
		}
		return instance, nil
	}
	ctorIndex := classInfo.ConstructorIndices[0]
	if ctorIndex < 0 || int(ctorIndex) >= len(vm.chunk.Functions) {
		return nil, fmt.Errorf("deserialize %s: constructor index out of range", class.Name)
	}
	ctor := vm.chunk.Functions[ctorIndex]
	args := make([]runtime.Value, 0, len(ctor.ParamNames))
	for _, paramName := range ctor.ParamNames {
		// Skip the implicit "this" receiver, which the compiler
		// prepends to method/constructor parameter lists.
		if paramName == "this" {
			continue
		}
		key := runtime.String{Value: paramName}
		entry, ok := dict.GetEntry(native.DictKey(key))
		if !ok {
			return nil, fmt.Errorf("deserialize %s: missing field %q", class.Name, paramName)
		}
		args = append(args, entry.Value)
	}
	return vm.ConstructClass(class.Index, args)
}

// deserializeViaInheritedFactory runs a static __deserialize factory inherited from a cross-module ancestor (same-chunk ancestors handled by lookupStaticDunder); found is false when none declares it.
func (vm *VM) deserializeViaInheritedFactory(class runtime.BytecodeClass, value runtime.Value) (runtime.Value, bool, error) {
	if vm.moduleLoader == nil {
		return nil, false, nil
	}
	for _, dunder := range [2]string{"__deserialize", "__deserialize__"} {
		result, found, err := vm.moduleLoader.CallModuleStaticMethodByName(class.Module, class.Name, dunder, []runtime.Value{value})
		if err != nil {
			return nil, true, err // unwrapped, matching the own-factory path and the evaluator so a fault renders identically
		}
		if found {
			return result, true, nil
		}
	}
	return nil, false, nil
}

func vmFieldTypeInfos(classInfo ClassInfo) []native.FieldTypeInfo {
	infos := make([]native.FieldTypeInfo, len(classInfo.FieldNames))
	for i, name := range classInfo.FieldNames {
		t := ""
		if i < len(classInfo.FieldTypes) {
			t = classInfo.FieldTypes[i]
		}
		infos[i] = native.FieldTypeInfo{Name: name, Type: t}
	}
	return infos
}

// resolveDeserializeClass resolves a field's declared class name to a
// BytecodeClass for nested parseAs, locally then via the module loader.
func (vm *VM) resolveDeserializeClass(name string) (runtime.Value, bool) {
	if idx, ok := vm.classIndex[strings.ToLower(name)]; ok && int(idx) < len(vm.chunk.Classes) {
		return vm.bytecodeClassFromInfo(vm.chunk.Classes[idx], int64(idx)), true
	}
	if vm.moduleLoader != nil {
		if v, ok := vm.moduleLoader.FindClassByName(name); ok {
			if bc, isClass := v.(runtime.BytecodeClass); isClass {
				return bc, true
			}
		}
	}
	return nil, false
}

func NewVMWithModuleLoader(chunk Chunk, stdout io.Writer, loader ModuleLoader) *VM {
	vm := NewVM(chunk, stdout)
	vm.moduleLoader = loader
	// Conversion context set on this VM's own registry after moduleLoader is assigned; serialize/deserialize routes through the loader (a fresh exclusive worker per call) with no process-global, so no partially built VM is ever published.
	vm.instanceInvokerFn = loaderInstanceInvoker(loader)
	vm.classDeserializerFn = loaderClassDeserializer(loader)
	vm.natives.SetConversionContext(native.ConversionContext{InstanceInvoker: vm.instanceInvokerFn, ClassDeserializer: vm.classDeserializerFn})
	return vm
}

// loaderInstanceInvoker dispatches an instance method (e.g. __serialize) through the loader; the existence check reads the immutable Class.Methods (incl. inherited) so a non-__serialize value never acquires a worker.
func loaderInstanceInvoker(loader ModuleLoader) native.InstanceInvokerFunc {
	return func(instance *runtime.Instance, method string, args []runtime.Value) (runtime.Value, bool, error) {
		if instance == nil || instance.Class == nil {
			return nil, false, nil
		}
		if !classHasMethod(instance.Class, method) {
			return nil, false, nil
		}
		result, err := loader.CallModuleMethod(instance.Class.Module, instance.Class.Name, method, instance, args, nil)
		if err != nil {
			return nil, false, err
		}
		return result, true, nil
	}
}

// classHasMethod walks the immutable class + parent chain (race-safe) for a method, so inherited dunders like __serialize are found without touching a VM's mutable lookup cache.
func classHasMethod(class *runtime.Class, method string) bool {
	lower := strings.ToLower(method)
	for c := class; c != nil; c = c.Parent {
		if len(c.Methods[lower]) > 0 {
			return true
		}
	}
	return false
}

// loaderClassDeserializer deserializes into a class on a loader worker; a reflect-style class target resolves through FindClassByName.
func loaderClassDeserializer(loader ModuleLoader) native.ClassDeserializerFunc {
	return func(classValue runtime.Value, value runtime.Value) (runtime.Value, error) {
		class, ok := classValue.(runtime.BytecodeClass)
		if !ok {
			target, isTarget := classValue.(runtime.DecoratorTarget)
			if !isTarget || target.Target != "class" || target.Class == nil {
				return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
			}
			resolved, found := loader.FindClassByName(target.Class.Name)
			if !found {
				return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
			}
			class, ok = resolved.(runtime.BytecodeClass)
			if !ok {
				return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
			}
		}
		return loader.DeserializeModuleClass(class, value)
	}
}

// Cleanup invokes the destructor of every tracked instance whose
// class declared `func ~ClassName()`. Called at program exit by the
// CLI shutdown path. Instances are visited in reverse-creation
// order so younger objects clean up first. A destructor that
// instantiates more destructor-bearing objects is handled by
// re-draining the registry, bounded by maxDepth to guard against
// pathological recursion.
func (vm *VM) Cleanup() error {
	const maxDepth = 4
	for depth := 0; depth < maxDepth; depth++ {
		batch := vm.TakeDestructibles()
		if len(batch) == 0 {
			return nil
		}
		for i := len(batch) - 1; i >= 0; i-- {
			inst := batch[i]
			if inst == nil || inst.Destroyed || inst.Class == nil {
				continue
			}
			if inst.Class.Module != vm.moduleName && vm.moduleLoader != nil {
				inst.Destroyed = true
				if err := vm.moduleLoader.CallModuleDestructor(inst.Class.Module, inst.Class.Name, inst, vm); err != nil {
					fmt.Fprintln(vm.destructorStderr(), runtime.RenderDestructorFailure(inst.Class.Name, destructorFailure(err)))
				}
				continue
			}
			classInfo, ok := vm.classInfo(inst.Class.Name)
			if !ok || classInfo.DestructorIndex < 0 {
				continue
			}
			inst.Destroyed = true
			if _, err := vm.CallFunctionRaw(classInfo.DestructorIndex, []runtime.Value{inst}); err != nil {
				fmt.Fprintln(vm.destructorStderr(), runtime.RenderDestructorFailure(inst.Class.Name, destructorFailure(err)))
			}
		}
	}
	return nil
}

// EnableDestructibleLocking guards this VM's destructible list so a caller==nil cross-module adoption from another goroutine is race-free; used on the entry VM.
func (vm *VM) EnableDestructibleLocking() { vm.destructMu = &sync.Mutex{} }

func (vm *VM) lockDestruct() {
	if vm.destructMu != nil {
		vm.destructMu.Lock()
	}
}

func (vm *VM) unlockDestruct() {
	if vm.destructMu != nil {
		vm.destructMu.Unlock()
	}
}

// TakeDestructibles returns and clears the tracked destructibles so a transient worker hands its destructor lifetimes up to a non-transient owner.
func (vm *VM) TakeDestructibles() []*runtime.Instance {
	vm.lockDestruct()
	defer vm.unlockDestruct()
	list := vm.destructibleInstances
	vm.destructibleInstances = nil
	return list
}

// AdoptDestructibles appends destructibles handed up from a transient worker so they fire at this VM's program-exit sweep.
func (vm *VM) AdoptDestructibles(list []*runtime.Instance) {
	if len(list) == 0 {
		return
	}
	vm.lockDestruct()
	defer vm.unlockDestruct()
	vm.destructibleInstances = append(vm.destructibleInstances, list...)
}

// trackDestructible registers a freshly constructed destructor-bearing instance under the destructible-list guard.
func (vm *VM) trackDestructible(inst *runtime.Instance) {
	vm.lockDestruct()
	vm.destructibleInstances = append(vm.destructibleInstances, inst)
	vm.unlockDestruct()
}

// removeDestructible unregisters an instance from the exit sweep so `del` does not double-fire it.
func (vm *VM) removeDestructible(instance *runtime.Instance) {
	vm.lockDestruct()
	defer vm.unlockDestruct()
	for i, tracked := range vm.destructibleInstances {
		if tracked == instance {
			vm.destructibleInstances = append(vm.destructibleInstances[:i], vm.destructibleInstances[i+1:]...)
			return
		}
	}
}

func (vm *VM) destructorStderr() io.Writer {
	if vm.stderr != nil {
		return vm.stderr
	}
	return os.Stderr
}

// destructorFailure recovers the typed throw from a destructor error; non-throw faults class as RuntimeError.
func destructorFailure(err error) runtime.Error {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		return thrown.err
	}
	return runtime.NewRecoverableError(err)
}

// GlobalsSnapshot returns a copy of the current globals as a
// []runtime.Value for callers that interoperate with the evaluator's
// environment model. Internally globals are stored as []VMValue; the
// snapshot pays the conversion cost once at the boundary.
func (vm *VM) GlobalsSnapshot() []runtime.Value {
	out := make([]runtime.Value, len(vm.globals))
	for i, v := range vm.globals {
		out[i] = v.ToValue()
	}
	return out
}

// RestoreGlobals replaces the globals with a converted copy of the caller-provided []runtime.Value.
func (vm *VM) RestoreGlobals(globals []runtime.Value) {
	if cap(vm.globals) >= len(globals) {
		vm.globals = vm.globals[:len(globals)]
	} else {
		vm.globals = make([]runtime.VMValue, len(globals))
	}
	vm.curGlobals = vm.globals
	for i, v := range globals {
		vm.globals[i] = runtime.VMValueFromValue(v)
	}
}

// PersistDirtyGlobals copies the slots this run reassigned back into dst (the loader's live module globals), so cross-module module-global writes persist across calls. The caller serializes access to dst.
func (vm *VM) PersistDirtyGlobals(dst []runtime.VMValue) {
	n := len(dst)
	if len(vm.globals) < n {
		n = len(vm.globals)
	}
	if len(vm.dirtyGlobals) < n {
		n = len(vm.dirtyGlobals)
	}
	for i := 0; i < n; i++ {
		if vm.dirtyGlobals[i] {
			dst[i] = vm.globals[i]
		}
	}
}

// GlobalsSnapshotVM returns a copy of the raw VM globals, avoiding the VMValue<->Value round-trip a loader pays restoring an immutable module snapshot every cross-module call.
func (vm *VM) GlobalsSnapshotVM() []runtime.VMValue {
	out := make([]runtime.VMValue, len(vm.globals))
	copy(out, vm.globals)
	return out
}

// RestoreGlobalsVM copies a pre-converted VMValue snapshot into the globals buffer (memmove), the fast path for a stable immutable module snapshot.
func (vm *VM) RestoreGlobalsVM(globals []runtime.VMValue) {
	if cap(vm.globals) >= len(globals) {
		vm.globals = vm.globals[:len(globals)]
	} else {
		vm.globals = make([]runtime.VMValue, len(globals))
	}
	copy(vm.globals, globals)
	vm.curGlobals = vm.globals
}

// restoreGlobalsVM is the VM-internal fast path used when one VM hands
// its globals to a wrapper / generator VM. It copies the VMValue slice
// directly without round-tripping through runtime.Value.
func (vm *VM) restoreGlobalsVM(globals []runtime.VMValue) {
	vm.globals = append(vm.globals[:0], globals...)
	vm.curGlobals = vm.globals
}

func (vm *VM) FunctionDecoratorState() FunctionDecoratorState {
	return FunctionDecoratorState{
		decorated:           copyRuntimeValueMap(vm.decoratedFuncs),
		decoratedClasses:    copyRuntimeValueMap(vm.decoratedClasses),
		applied:             vm.decoratorsApplied,
		methodReceiverFuncs: copyBoolMap(vm.methodReceiverFuncs),
	}
}

func (vm *VM) RestoreFunctionDecoratorState(state FunctionDecoratorState) {
	vm.decoratorsApplied = state.applied
	// nil, not an empty-map copy: an undecorated module restores these every cross-module call, and a pooled worker only reads them (decoratorsApplied is true, so it never populates them).
	if len(state.decorated) > 0 {
		vm.decoratedFuncs = copyRuntimeValueMap(state.decorated)
	} else {
		vm.decoratedFuncs = nil
	}
	if len(state.decoratedClasses) > 0 {
		vm.decoratedClasses = copyRuntimeValueMap(state.decoratedClasses)
	} else {
		vm.decoratedClasses = nil
	}
	if len(state.methodReceiverFuncs) > 0 {
		vm.methodReceiverFuncs = copyBoolMap(state.methodReceiverFuncs)
	} else {
		vm.methodReceiverFuncs = nil
	}
}

// InterfaceFallbackState carries a module's runtime interface-default tables
// (built at OpDefineClass) so cross-module-spawned VMs can resolve interface
// default methods and interface-provided fields, mirroring globals/decorator
// state restoration.
type InterfaceFallbackState struct {
	fallbacks   map[int64]map[string]crossModuleDefault
	extraFields map[int64][]extraField
}

func (vm *VM) InterfaceFallbackState() InterfaceFallbackState {
	fallbacks := make(map[int64]map[string]crossModuleDefault, len(vm.interfaceFallbacks))
	for idx, methods := range vm.interfaceFallbacks {
		inner := make(map[string]crossModuleDefault, len(methods))
		for name, def := range methods {
			inner[name] = def
		}
		fallbacks[idx] = inner
	}
	extras := make(map[int64][]extraField, len(vm.interfaceExtraFields))
	for idx, fields := range vm.interfaceExtraFields {
		extras[idx] = append([]extraField(nil), fields...)
	}
	return InterfaceFallbackState{fallbacks: fallbacks, extraFields: extras}
}

func (vm *VM) RestoreInterfaceFallbackState(state InterfaceFallbackState) {
	if state.fallbacks != nil {
		vm.interfaceFallbacks = state.fallbacks
	}
	if state.extraFields != nil {
		vm.interfaceExtraFields = state.extraFields
	}
}

func (vm *VM) noteEscape() { vm.escapedRefs.Add(1) }

// ResetForReuse clears per-run state so a pooled VM can host another call against the same chunk.
func (vm *VM) ResetForReuse() { vm.resetForReuse() }

// Recyclable reports whether this VM handed out no vm-capturing
// closures during its run (safe to pool).
func (vm *VM) Recyclable() bool { return vm.escapedRefs.Load() == 0 }

func (vm *VM) resetForReuse() {
	vm.stack = vm.stack[:0]
	vm.localsStack = vm.localsStack[:0]
	vm.frames = vm.frames[:0]
	if cap(vm.defers) > 0 {
		vm.defers = vm.defers[:1]
		vm.defers[0] = vm.defers[0][:0]
	} else {
		vm.defers = make([][]deferredAction, 1, 64)
	}
	for i := range vm.dirtyGlobals {
		vm.dirtyGlobals[i] = false
	}
	vm.pendingThrow = nil
	vm.exceptionHandlers = vm.exceptionHandlers[:0]
	vm.constantsExtra = nil
	vm.staticLocal = nil
	vm.staticsLocalOnly = false
	vm.runEntryIP = 0
	vm.runInlineExitDepth = -1
	vm.traceFrameBaseline = 0
	vm.inDispatchLoop = false
	vm.destructibleInstances = nil
	vm.pendingTypeBindings = nil
	vm.forwardThis = nil
	vm.generatorExecution = false
	vm.generatorYield = nil
	vm.generatorDone = nil
	// nil, not a fresh map: the loader path overwrites these via RestoreInterfaceFallbackState and the wrapper-callVM path only reads them; only class-definition (load) writes them, never a pooled worker.
	vm.interfaceFallbacks = nil
	vm.interfaceExtraFields = nil
	vm.bridgeActive.Store(false)
	vm.escapedRefs.Store(0)
	vm.reentryHost = nil
	vm.reentryActive = false
	vm.reentryDepth = 0
	vm.curMod.Chunk = vm.chunk
}

func (vm *VM) prepareFunctionTypeMetadata() {
	hasCallSitePolymorphism := false
	for i := range vm.chunk.Functions {
		function := &vm.chunk.Functions[i]
		function.typeParamSet = functionTypeParameterSetOrNil(*function)
		function.requiresParamValidation = functionRequiresParamValidation(*function)
		if function.Async || function.IsGenerator || len(function.Decorators) > 0 {
			hasCallSitePolymorphism = true
		}
		if len(function.ParamTypes) == 0 {
			continue
		}
		function.paramTypeSpecs = make([]vmTypeSpec, len(function.ParamTypes))
		for j, typ := range function.ParamTypes {
			if typ != "" {
				function.paramTypeSpecs[j] = vm.typeSpec(typ)
			}
		}
	}
	if !hasCallSitePolymorphism {
		for _, classInfo := range vm.chunk.Classes {
			if len(classInfo.Decorators) > 0 || len(classInfo.MethodDecorators) > 0 {
				hasCallSitePolymorphism = true
				break
			}
		}
	}
	vm.requiresCallSitePolymorphism = hasCallSitePolymorphism
	for _, instruction := range vm.chunk.Instructions {
		if instruction.Op != OpTypeAssert || len(instruction.Operands) != 1 {
			continue
		}
		constIdx := instruction.Operands[0]
		if constIdx < 0 || int(constIdx) >= len(vm.chunk.Constants) {
			continue
		}
		typeStr, ok := vm.chunk.Constants[constIdx].(runtime.String)
		if !ok {
			continue
		}
		if vm.typeAssertSpecs == nil {
			vm.typeAssertSpecs = map[int64]vmTypeSpec{}
		}
		vm.typeAssertSpecs[constIdx] = vm.typeSpec(typeStr.Value)
	}
}

// takeCallArgsBuffer returns a []runtime.Value of length size, reusing
// a previously released slice from the per-VM free list when one with
// sufficient capacity is available. Pair every call with
// releaseCallArgsBuffer; the slice must not escape the call boundary.
func (vm *VM) takeCallArgsBuffer(size int) []runtime.Value {
	for i := len(vm.callArgsFree) - 1; i >= 0; i-- {
		buf := vm.callArgsFree[i]
		if cap(buf) < size {
			continue
		}
		vm.callArgsFree[i] = vm.callArgsFree[len(vm.callArgsFree)-1]
		vm.callArgsFree = vm.callArgsFree[:len(vm.callArgsFree)-1]
		return buf[:size]
	}
	return make([]runtime.Value, size)
}

func (vm *VM) releaseCallArgsBuffer(buf []runtime.Value) {
	if cap(buf) == 0 {
		return
	}
	clear(buf)
	vm.callArgsFree = append(vm.callArgsFree, buf[:0])
}

func (vm *VM) pushLocalsStackFrame(frame *callFrame, localCount int, shares bool) {
	vm.pushLocalsStackFrameSkip(frame, localCount, shares, 0)
}

// pushLocalsStackFrameSkip is pushLocalsStackFrame with the first
// skipLeading slots left unzeroed; the caller must overwrite them
// before any opcode runs (the fast call path's param copy).
func (vm *VM) pushLocalsStackFrameSkip(frame *callFrame, localCount int, shares bool, skipLeading int) {
	if shares {
		frame.basePointer = vm.currentFrameBP
	} else {
		frame.basePointer = len(vm.localsStack)
	}
	frame.localCount = localCount
	vm.currentFrameBP = frame.basePointer
	vm.ensureLocalsStackFrom(frame.basePointer+localCount, frame.basePointer+skipLeading)
}

func (vm *VM) ensureLocalsStack(end int) {
	vm.ensureLocalsStackFrom(end, 0)
}

func (vm *VM) ensureLocalsStackFrom(end, clearFrom int) {
	cur := len(vm.localsStack)
	if end <= cur {
		return
	}
	if end > cap(vm.localsStack) {
		newCap := cap(vm.localsStack) * 2
		if newCap < end {
			newCap = end
		}
		grown := make([]runtime.VMValue, end, newCap)
		copy(grown, vm.localsStack)
		vm.localsStack = grown
		return
	}
	vm.localsStack = vm.localsStack[:end]
	if clearFrom < cur {
		clearFrom = cur
	}
	if clearFrom < end {
		clear(vm.localsStack[clearFrom:end])
	}
}

func (vm *VM) popLocalsStackFrame(frame *callFrame) {
	if !frame.shared && frame.basePointer < len(vm.localsStack) {
		vm.localsStack = vm.localsStack[:frame.basePointer]
	}
	if len(vm.frames) > 1 {
		vm.currentFrameBP = vm.frames[len(vm.frames)-2].basePointer
	} else {
		vm.currentFrameBP = 0
	}
}

func (vm *VM) SetModuleName(name string) {
	vm.moduleName = name
}

func (vm *VM) ModuleName() string {
	return vm.moduleName
}

func (vm *VM) ReentryHost() *VM        { return vm.reentryHost }
func (vm *VM) SetReentryHost(h *VM)    { vm.reentryHost = h }
func (vm *VM) ReentryActive() bool     { return vm.reentryActive }
func (vm *VM) SetReentryActive(b bool) { vm.reentryActive = b }
func (vm *VM) IncReentryDepth()        { vm.reentryDepth++ }
func (vm *VM) DecReentryDepth() int    { vm.reentryDepth--; return vm.reentryDepth }

// activeReentryHost returns the nearest borrowed-active worker reachable from vm (vm itself when active), the host a bridged callback re-enters through.
func (vm *VM) activeReentryHost() *VM {
	for h := vm; h != nil; h = h.reentryHost {
		if h.reentryActive {
			return h
		}
	}
	return nil
}

// SetPersistGlobals enables dirty-slot tracking on the lock-free setGlobal path so a loader worker's module-global writes can be written back after the call.
func (vm *VM) SetPersistGlobals(on bool) {
	vm.persistGlobals = on
}

func (vm *VM) SetModulePaths(paths []string) {
	vm.modulePaths = append([]string(nil), paths...)
}

func (vm *VM) SetStatefulNativeCaller(caller StatefulNativeCaller) {
	vm.statefulNative = caller
}

func (vm *VM) SetMaxCallDepth(limit int) {
	vm.maxCallDepth = limit
}
