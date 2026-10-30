package bytecode

import (
	"fmt"
	"geblang/internal/ast"
	argbinding "geblang/internal/binding"
	"geblang/internal/native"
	"geblang/internal/overload"
	"geblang/internal/profiling"
	"geblang/internal/runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// tailCall replaces the top frame's function with a new one. Used by
// OpTailCall for `return f(args)` in tail position. The current
// frame's returnIP, returnOverride, and locals buffer are reused
// (or replaced if LocalCount differs). Defers and exception handlers
// in the caller MUST be empty - the compiler enforces this.
func (vm *VM) tailCall(instruction Instruction) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "tail call instruction has invalid operands")
	}
	index := instruction.Operands[0]
	argc := instruction.Operands[1]
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return 0, vm.runtimeError(instruction, "tail call function index out of range")
	}
	function := vm.curMod.Chunk.Functions[index]
	if len(vm.frames) == 0 {
		return 0, vm.runtimeError(instruction, "tail call without an active frame")
	}
	paramCount := len(function.ParamSlots)
	if int(argc) != paramCount {
		return 0, vm.runtimeError(instruction, "tail call arity mismatch: %s expects %d, got %d", function.Name, paramCount, argc)
	}
	n := len(vm.stack)
	if int(argc) > n {
		return 0, vm.fatalError(instruction, "stack underflow")
	}
	stackArgs := vm.stack[n-int(argc) : n]
	if function.requiresParamValidation {
		typeParams := function.typeParamSet
		inherited := vm.pendingTypeBindings
		specs := function.paramTypeSpecs
		for i := 0; i < paramCount; i++ {
			pt := function.ParamTypes[i]
			if pt == "" {
				continue
			}
			argKind := stackArgs[i].Kind
			if len(inherited) == 0 && i < len(specs) && specs[i].kind == vmTypeInt && argKind == runtime.VMKindSmallInt {
				continue
			}
			var spec vmTypeSpec
			if i < len(specs) && specs[i].raw != "" {
				spec = specs[i]
			} else {
				spec = vm.typeSpec(pt)
			}
			if !vm.matchVMValueToTypeSpecWith(typeParams, inherited, stackArgs[i], spec) {
				return 0, vm.runtimeError(instruction, "%s: argument %d expected %s, got %s", function.Name, i+1, pt, stackArgs[i].ToValue().TypeName())
			}
		}
	}
	args := make([]runtime.VMValue, paramCount)
	copy(args, stackArgs)
	vm.truncateStack(n - int(argc))
	// Reuse the existing top frame: keep returnIP, returnOverride.
	// Replace functionName / callLine for stack traces. Reset other
	// fields to defaults for the new function.
	frame := &vm.frames[len(vm.frames)-1]
	frame.functionName = function.Name
	if profiling.Active.Load() {
		vm.profileSample(0, true)
	}
	frame.meta = function.frameMeta
	frame.returnBindings = nil
	if function.frameMeta != nil && function.frameMeta.needsBindings && len(args) > 0 {
		frame.returnBindings = vm.returnBindingsFor(&function, args[0].ToValue())
	}
	// Keep frame.callLine at the original entry site; the tail-call site is the [xN] line.
	frame.tailRepeat++
	frame.tailCallLine = int(instruction.Line)
	frame.negateReturn = false
	frame.isErrorClass = false
	frame.isImmutableClass = false
	frame.immutableFieldsToLock = nil
	frame.lockInstance = nil
	frame.constraintInstance = nil
	frame.isDestructibleConstructor = false
	frame.typeBindings = nil
	frame.generator = nil
	frame.generatorDone = nil
	vm.pendingTypeBindings = nil
	// Release the caller-function's own locals (the frame was reusing
	// them; the eventual OpReturn will use frame.locals, which still
	// points to the original caller's locals, to restore on pop). Then
	// allocate a fresh slice for the callee. function.SharesParentFrame
	frame.shared = false
	vm.popLocalsStackFrame(frame)
	vm.pushLocalsStackFrame(frame, int(function.LocalCount), function.SharesParentFrame)
	bp := frame.basePointer
	for i := 0; i < paramCount; i++ {
		vm.localsStack[bp+int(function.ParamSlots[i])] = args[i]
	}
	frameDepth := len(vm.frames)
	if frameDepth < cap(vm.defers) {
		vm.defers = vm.defers[:frameDepth+1]
		vm.defers[frameDepth] = vm.defers[frameDepth][:0]
	}
	return int(function.Entry) - 1, nil
}

func (vm *VM) call(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 2 {
		return 0, vm.fatalError(instruction, "call instruction has invalid operands")
	}
	index := instruction.Operands[0]
	argc := instruction.Operands[1]
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return 0, vm.runtimeError(instruction, "function index out of range")
	}
	function := &vm.curMod.Chunk.Functions[index]
	if !vm.requiresCallSitePolymorphism {
		n := len(vm.stack)
		if int(argc) > n {
			return 0, vm.fatalError(instruction, "stack underflow")
		}
		stackArgs := vm.stack[n-int(argc) : n]
		vm.stack = vm.stack[:n-int(argc)]
		nextIP, err := vm.startFunctionVMValue(instruction, ip, function, stackArgs, nil)
		clear(stackArgs)
		return nextIP, err
	}
	decorated, hasDecorated := vm.decoratedFuncs[index]
	if !hasDecorated || vm.rawFunctionCalls[index] {
		if !function.Async || vm.syncMode {
			if !function.IsGenerator || vm.generatorExecution {
				n := len(vm.stack)
				if int(argc) > n {
					return 0, vm.fatalError(instruction, "stack underflow")
				}
				stackArgs := vm.stack[n-int(argc) : n]
				vm.stack = vm.stack[:n-int(argc)]
				nextIP, err := vm.startFunctionVMValue(instruction, ip, function, stackArgs, nil)
				clear(stackArgs)
				return nextIP, err
			}
		}
	}
	// Async / generator / callable branches let args escape into a
	// worker goroutine or stored task, so they keep the raw make().
	// The terminal startFunction path copies args into locals before
	// returning, so it can ride the call-args pool.
	asyncEscape := function.Async && !vm.syncMode
	generatorEscape := function.IsGenerator && !vm.generatorExecution
	callableEscape := hasDecorated && !vm.rawFunctionCalls[index]
	canPool := !asyncEscape && !generatorEscape && !callableEscape
	var args []runtime.Value
	if canPool {
		args = vm.takeCallArgsBuffer(int(argc))
	} else {
		args = make([]runtime.Value, argc)
	}
	for i := int(argc) - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			if canPool {
				vm.releaseCallArgsBuffer(args)
			}
			return 0, vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	if hasDecorated && !vm.rawFunctionCalls[index] {
		if function.Async && !vm.syncMode {
			task := vm.startAsyncCallable(decorated, args)
			vm.push(task)
			return ip, nil
		}
		result, err := vm.callCallable(decorated, args)
		if err != nil {
			return vm.propagateDecoratedCallbackError(instruction, ip, err, function.Name)
		}
		vm.push(result)
		return ip, nil
	}
	if function.Async && !vm.syncMode {
		task := vm.startAsyncFunction(index, args)
		vm.push(task)
		return ip, nil
	}
	if function.IsGenerator && !vm.generatorExecution {
		vm.push(vm.lazyGenerator(index, args))
		return ip, nil
	}
	nextIP, err := vm.startFunction(instruction, ip, function, args, nil)
	vm.releaseCallArgsBuffer(args)
	return nextIP, err
}

// startAsyncMethod starts an async method body on a worker and reports whether it did; callArgs[0] is the receiver.
func (vm *VM) startAsyncMethod(functionIndex int64, callArgs []runtime.Value) (*runtime.Task, bool) {
	if vm.syncMode || !vm.curMod.Chunk.Functions[functionIndex].Async {
		return nil, false
	}
	return vm.startAsyncFunction(functionIndex, callArgs), true
}

func (vm *VM) startAsyncFunction(index int64, args []runtime.Value) *runtime.Task {
	task := runtime.NewTask()
	bindings := vm.pendingTypeBindings
	vm.pendingTypeBindings = nil
	// Snapshot parent VM state into a private worker before spawning the
	// goroutine; the worker touches no parent fields after this point.
	worker := vm.spawnAsyncWorker()
	runtime.AsyncEnter()
	go func() {
		defer runtime.AsyncLeave()
		result, err := worker.CallFunctionBound(index, args, bindings, false)
		task.Complete(result, err)
	}()
	return task
}

// spawnAsyncWorker constructs a worker VM holding snapshots of the parent's
// mutable state (globals, decorator maps, etc.). The caller must spawn its
// goroutine AFTER this returns so the snapshot copies are happen-before
// the worker's reads.
func (parent *VM) spawnAsyncWorker() *VM {
	chunk := parent.chunk
	if parent.wrapperBase != nil {
		// A pooled wrapper's instruction tail changes when its next call starts.
		chunk.Instructions = append([]Instruction(nil), chunk.Instructions...)
	}
	worker := NewVMWithModuleLoader(chunk, parent.stdout, parent.moduleLoader)
	worker.restoreGlobalsVM(parent.globals)
	worker.SetModuleName(parent.moduleName)
	worker.SetModulePaths(parent.modulePaths)
	if parent.statefulNative != nil {
		worker.SetStatefulNativeCaller(parent.statefulNative)
	}
	worker.syncMode = true
	worker.forwardThis = parent.forwardThis
	worker.decoratedFuncs = copyRuntimeValueMap(parent.decoratedFuncs)
	worker.decoratedClasses = copyRuntimeValueMap(parent.decoratedClasses)
	worker.decoratorsApplied = true
	worker.rawFunctionCalls = copyBoolMap(parent.rawFunctionCalls)
	worker.methodReceiverFuncs = copyBoolMap(parent.methodReceiverFuncs)
	return worker
}

// enableGeneratorPersist turns on dirty-global tracking for a module worker's generator so its module-global writes can be written back when it finishes.
func (vm *VM) enableGeneratorPersist(callVM *VM) {
	if vm.moduleLoader != nil && callVM.ModuleName() != "" {
		callVM.SetPersistGlobals(true)
	}
}

// persistGeneratorGlobals writes a finished generator worker's dirty module globals back to its module record, matching the evaluator's shared-environment semantics.
func (vm *VM) persistGeneratorGlobals(callVM *VM) {
	if vm.moduleLoader != nil && callVM.ModuleName() != "" {
		vm.moduleLoader.PersistModuleGlobals(callVM)
	}
}

func (vm *VM) lazyGenerator(index int64, args []runtime.Value) *runtime.Generator {
	vm.noteEscape()
	bindings := vm.pendingTypeBindings
	vm.pendingTypeBindings = nil
	items := make(chan vmGeneratorItem)
	doneCh := make(chan struct{})
	stop := sync.Once{}
	start := sync.Once{}
	closed := false
	var pendingErr error
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		start.Do(func() {
			callVM := vm.spawnAsyncWorker()
			callVM.generatorExecution = true
			callVM.generatorYield = items
			callVM.generatorDone = doneCh
			vm.enableGeneratorPersist(callVM)
			go func() {
				defer close(items)
				_, err := callVM.CallFunctionBound(index, args, bindings, false)
				vm.persistGeneratorGlobals(callVM)
				if err != nil {
					select {
					case items <- vmGeneratorItem{err: err}:
					case <-doneCh:
					}
				}
			}()
		})
		if closed {
			return nil, false, pendingErr
		}
		item, ok := <-items
		if !ok {
			closed = true
			return nil, false, nil
		}
		if item.err != nil {
			closed = true
			pendingErr = item.err
			return nil, false, item.err
		}
		return item.value, true, nil
	}, func() {
		stop.Do(func() {
			close(doneCh)
		})
	})
}

func (vm *VM) lazyClosureGenerator(closure runtime.BytecodeClosure, args []runtime.Value) *runtime.Generator {
	vm.noteEscape()
	items := make(chan vmGeneratorItem)
	doneCh := make(chan struct{})
	stop := sync.Once{}
	start := sync.Once{}
	closed := false
	var pendingErr error
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		start.Do(func() {
			callVM := vm.spawnAsyncWorker()
			callVM.generatorExecution = true
			callVM.generatorYield = items
			callVM.generatorDone = doneCh
			vm.enableGeneratorPersist(callVM)
			go func() {
				defer close(items)
				_, err := callVM.callCallable(closure, args)
				vm.persistGeneratorGlobals(callVM)
				if err != nil {
					select {
					case items <- vmGeneratorItem{err: err}:
					case <-doneCh:
					}
				}
			}()
		})
		if closed {
			return nil, false, pendingErr
		}
		item, ok := <-items
		if !ok {
			closed = true
			return nil, false, nil
		}
		if item.err != nil {
			closed = true
			pendingErr = item.err
			return nil, false, item.err
		}
		return item.value, true, nil
	}, func() {
		stop.Do(func() {
			close(doneCh)
		})
	})
}

func (vm *VM) startAsyncCallable(fn runtime.Value, args []runtime.Value) *runtime.Task {
	return vm.startAsyncCallableWithForwardThis(fn, args, nil)
}

func (vm *VM) startAsyncCallableWithForwardThis(fn runtime.Value, args []runtime.Value, receiver *runtime.Instance) *runtime.Task {
	if len(vm.pendingTypeBindings) > 0 {
		fn = bindCallableBindings(fn, vm.pendingTypeBindings)
		vm.pendingTypeBindings = nil
	}
	task := runtime.NewTask()
	// Snapshot parent state into a private worker; the goroutine touches
	// only the worker, not the parent VM.
	worker := vm.spawnAsyncWorker()
	if receiver != nil {
		worker.forwardThis = receiver
	}
	runtime.AsyncEnter()
	go func() {
		defer runtime.AsyncLeave()
		result, err := worker.callCallable(fn, args)
		task.Complete(result, err)
	}()
	return task
}

func (vm *VM) callCallableWithForwardThis(fn runtime.Value, args []runtime.Value, receiver *runtime.Instance) (runtime.Value, error) {
	previous := vm.forwardThis
	vm.forwardThis = receiver
	defer func() {
		vm.forwardThis = previous
	}()
	return vm.callCallable(fn, args)
}

func (vm *VM) startFunction(instruction Instruction, ip int, function *FunctionInfo, provided []runtime.Value, returnOverride runtime.Value) (int, error) {
	return vm.startFunctionWithValidation(instruction, ip, function, provided, returnOverride, true)
}

// startFunctionWithBindings enters a function body with an inherited
// type-binding map planted into the new call frame. Used by the
// closure call path so that a lambda or named-generic-function value
// can resolve type-parameter names from the call site of its
// enclosing generic function.
func (vm *VM) startFunctionWithBindings(instruction Instruction, ip int, function *FunctionInfo, provided []runtime.Value, returnOverride runtime.Value, inheritedBindings map[string]string) (int, error) {
	prev := vm.pendingTypeBindings
	vm.pendingTypeBindings = inheritedBindings
	nextIP, err := vm.startFunctionWithValidation(instruction, ip, function, provided, returnOverride, true)
	vm.pendingTypeBindings = prev
	return nextIP, err
}

// startFunctionVMValue is the fast-path call entry that operates on
// VMValue arguments directly. The caller passes a slice over the VM
// stack and the number of args (argc); the slice is consumed but not
// retained beyond this call. No []runtime.Value allocation or per-arg
// ToValue conversion happens on this path.
//
// The fast path covers the common case: no variadic, no defaults
// needed, no generics inference required, all arg types either `any`
// or match the parameter spec inline. Anything else falls back to the
// interface-form path by materialising args at the boundary, paying
// the allocation cost only on the slow path.
func (vm *VM) startFunctionVMValue(instruction Instruction, ip int, function *FunctionInfo, stackArgs []runtime.VMValue, returnOverride runtime.Value) (int, error) {
	maxDepth := vm.maxCallDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxCallDepth
	}
	if len(vm.frames) >= maxDepth {
		return 0, vm.fatalError(instruction, "maximum call depth exceeded (%d)", maxDepth)
	}
	argc := len(stackArgs)
	paramCount := len(function.ParamSlots)

	// Fast path requirements: fixed arity, no variadic packing, no
	// default-arg fill, returnOverride nil. The arity check itself runs
	// inline so the fast path can stay branch-light.
	fastPath := !function.Variadic && argc == paramCount && returnOverride == nil
	if !fastPath {
		// Materialise the args as []runtime.Value and let the legacy
		// path handle defaults/variadic/etc. startFunctionWithValidation
		// copies into locals before returning so the args slice is
		// safe to recycle once it returns.
		args := vm.takeCallArgsBuffer(argc)
		for i, a := range stackArgs {
			args[i] = a.ToValue()
		}
		nextIP, err := vm.startFunctionWithValidation(instruction, ip, function, args, returnOverride, true)
		vm.releaseCallArgsBuffer(args)
		return nextIP, err
	}

	// Type validation, inline against VMValues. Skips the spec lookup
	// when a typed-int param meets a SmallInt arg (the dominant case).
	if function.requiresParamValidation {
		typeParams := function.typeParamSet
		inherited := vm.pendingTypeBindings
		if len(function.ParamNames) > 0 && function.ParamNames[0] == "this" && paramCount > 0 && stackArgs[0].Kind == runtime.VMKindBoxed && !compiledConstructorName(function.Name) {
			if inst, ok := stackArgs[0].Boxed.(*runtime.Instance); ok {
				inherited = mergedTypeBindings(inherited, inst.TypeBindings)
			}
		}
		specs := function.paramTypeSpecs
		for i := 0; i < paramCount; i++ {
			pt := function.ParamTypes[i]
			if pt == "" {
				continue
			}
			argKind := stackArgs[i].Kind
			// Fast path: typed-int param, SmallInt arg.
			if len(inherited) == 0 && i < len(specs) && specs[i].kind == vmTypeInt && argKind == runtime.VMKindSmallInt {
				continue
			}
			var spec vmTypeSpec
			if i < len(specs) && specs[i].raw != "" {
				spec = specs[i]
			} else {
				spec = vm.typeSpec(pt)
			}
			if !vm.matchVMValueToTypeSpecWith(typeParams, inherited, stackArgs[i], spec) {
				// Fall back to the slow path so the existing error
				// reporting (which formats runtime.Value descriptions)
				// runs uniformly.
				args := vm.takeCallArgsBuffer(argc)
				for j, a := range stackArgs {
					args[j] = a.ToValue()
				}
				nextIP, err := vm.startFunctionWithValidation(instruction, ip, function, args, returnOverride, true)
				vm.releaseCallArgsBuffer(args)
				return nextIP, err
			}
		}
	}

	// Push the frame in place.
	if len(vm.frames) == cap(vm.frames) {
		grown := make([]callFrame, len(vm.frames), cap(vm.frames)*2)
		copy(grown, vm.frames)
		vm.frames = grown
	}
	vm.frames = vm.frames[:len(vm.frames)+1]
	// syncMode only makes the entry call run inline; calls nested inside it start Tasks as usual.
	vm.syncMode = false
	frame := &vm.frames[len(vm.frames)-1]
	frame.returnIP = ip
	frame.returnOverride = nil
	frame.functionName = function.Name
	if profiling.Active.Load() {
		vm.profileSample(0, true)
	}
	frame.meta = function.frameMeta
	frame.returnBindings = nil
	if function.frameMeta != nil && function.frameMeta.needsBindings {
		var receiver runtime.Value
		if paramCount > 0 {
			receiver = stackArgs[0].ToValue()
		}
		frame.returnBindings = vm.returnBindingsFor(function, receiver)
	}
	frame.callLine = int(instruction.Line)
	frame.tailRepeat = 0
	frame.tailCallLine = 0
	frame.negateReturn = false
	frame.isErrorClass = false
	frame.isImmutableClass = false
	frame.immutableFieldsToLock = nil
	frame.lockInstance = nil
	frame.constraintInstance = nil
	frame.isDestructibleConstructor = false
	frame.shared = function.SharesParentFrame
	if function.IsGenerator && vm.generatorExecution {
		frame.generator = vm.generatorYield
		frame.generatorDone = vm.generatorDone
	} else {
		frame.generator = nil
		frame.generatorDone = nil
	}
	frame.typeBindings = nil
	frameDepth := len(vm.frames)
	if frameDepth < cap(vm.defers) {
		vm.defers = vm.defers[:frameDepth+1]
		vm.defers[frameDepth] = vm.defers[frameDepth][:0]
	} else {
		vm.defers = append(vm.defers, nil)
	}
	// Save parent's locals slice handle (no copy) and either reuse it
	// (nested function statement) or allocate a fresh frame slice for
	// the callee. Fresh slice avoids the per-call snapshot copy that
	// previously dominated recursion overhead for small functions.
	// Params occupying the leading slots get overwritten by the copy
	// below, so skip zeroing them on frame reuse.
	skipLeading := paramCount
	for i := 0; i < paramCount; i++ {
		if int(function.ParamSlots[i]) != i {
			skipLeading = 0
			break
		}
	}
	vm.pushLocalsStackFrameSkip(frame, int(function.LocalCount), function.SharesParentFrame, skipLeading)
	bp := frame.basePointer
	for i := 0; i < paramCount; i++ {
		vm.localsStack[bp+int(function.ParamSlots[i])] = stackArgs[i]
	}
	if len(function.TypeParameters) > 0 && len(function.ParamTypes) > 0 {
		typeBindings := vm.inferTypeBindingsFromLocals(function)
		if err := vm.checkTypeParamConstraints(instruction, function, typeBindings); err != nil {
			return 0, err
		}
	}
	if vm.forwardThis != nil {
		vm.inheritInstanceTypeBindings(vm.forwardThis)
	}
	return int(function.Entry) - 1, nil
}

// compiledConstructorName reports the Class.Class constructor name form;
// constructors validate at the construct site, not against the receiver.
func compiledConstructorName(name string) bool {
	dot := strings.IndexByte(name, '.')
	if dot <= 0 || dot+1 >= len(name) {
		return false
	}
	return name[:dot] == name[dot+1:]
}

// receiverTypeBindings returns the receiver's reified bindings when the
// function's slot 0 is the implicit `this` param; nil otherwise.
func receiverTypeBindings(function *FunctionInfo, first runtime.Value) map[string]string {
	if len(function.ParamNames) == 0 || function.ParamNames[0] != "this" || first == nil {
		return nil
	}
	if inst, ok := first.(*runtime.Instance); ok {
		return withoutShadowedTypeParams(inst.TypeBindings, function.TypeParameters)
	}
	return nil
}

// withoutShadowedTypeParams drops class bindings that a method-level generic of the same name hides.
func withoutShadowedTypeParams(bindings map[string]string, params []string) map[string]string {
	if len(bindings) == 0 || len(params) < 2 {
		return bindings
	}
	var out map[string]string
	for _, param := range params {
		if !shadowsClassTypeParameter(params, param) {
			continue
		}
		for key := range bindings {
			if !strings.EqualFold(key, param) {
				continue
			}
			if out == nil {
				out = make(map[string]string, len(bindings))
				for k, v := range bindings {
					out[k] = v
				}
			}
			delete(out, key)
		}
	}
	if out == nil {
		return bindings
	}
	return out
}

// mergedTypeBindings overlays primary on secondary (primary wins).
// Either map may be returned unmodified; callers must not mutate.
func mergedTypeBindings(primary, secondary map[string]string) map[string]string {
	if len(secondary) == 0 {
		return primary
	}
	if len(primary) == 0 {
		return secondary
	}
	merged := make(map[string]string, len(primary)+len(secondary))
	for k, v := range secondary {
		merged[k] = v
	}
	for k, v := range primary {
		merged[k] = v
	}
	return merged
}

// matchVMValueToTypeSpecWith applies matchValueToTypeSpecWith's
// bindings-before-own-type-param precedence to VMValues.
func (vm *VM) matchVMValueToTypeSpecWith(typeParams map[string]bool, inherited map[string]string, value runtime.VMValue, spec vmTypeSpec) bool {
	if spec.nullable && (value.Kind == runtime.VMKindNull || value.Kind == runtime.VMKindUnset) {
		return true
	}
	if len(inherited) > 0 {
		if bound, ok := inherited[spec.base]; ok && bound != "" {
			if strings.Contains(bound, ".") {
				return vm.matchBoundType(typeParams, value.ToValue(), bound)
			}
			return vm.matchVMValueToTypeSpec(typeParams, value, vm.typeSpec(bound))
		}
		if bound, ok := inherited[spec.baseLower]; ok && bound != "" {
			if strings.Contains(bound, ".") {
				return vm.matchBoundType(typeParams, value.ToValue(), bound)
			}
			return vm.matchVMValueToTypeSpec(typeParams, value, vm.typeSpec(bound))
		}
	}
	return vm.matchVMValueToTypeSpec(typeParams, value, spec)
}

// frameMeta is the per-function data a call frame needs for return checks and binding resolution.
type frameMeta struct {
	returnSpec    *vmTypeSpec
	typeParams    map[string]bool
	needsBindings bool
}

func newFrameMeta(function FunctionInfo, spec func(string) vmTypeSpec) *frameMeta {
	returnSpec := checkedReturnSpecFor(function, spec)
	if returnSpec == nil && function.typeParamSet == nil {
		return nil
	}
	meta := &frameMeta{returnSpec: returnSpec, typeParams: function.typeParamSet}
	meta.needsBindings = returnSpec != nil && returnSpec.kind != vmTypeUnion && returnSpec.kind != vmTypeIntersection && function.typeParamSet[returnSpec.baseLower]
	return meta
}

func checkedReturnSpecFor(function FunctionInfo, spec func(string) vmTypeSpec) *vmTypeSpec {
	if function.IsGenerator || compiledConstructorName(function.Name) {
		return nil
	}
	switch strings.ToLower(function.ReturnCheckType) {
	case "", "void", "any":
		return nil
	}
	s := spec(function.ReturnCheckType)
	return &s
}

func (vm *VM) returnBindingsFor(function *FunctionInfo, receiver runtime.Value) map[string]string {
	if len(function.ParamNames) > 0 && function.ParamNames[0] == "this" {
		if inst, ok := receiver.(*runtime.Instance); ok {
			return mergedTypeBindings(vm.pendingTypeBindings, inst.TypeBindings)
		}
	}
	return vm.pendingTypeBindings
}

// matchVMValueToTypeSpec is the VMValue-aware variant of
// matchValueToTypeSpec. It avoids the ToValue() round-trip for the
// common primitive cases.
func (vm *VM) matchVMValueToTypeSpec(typeParams map[string]bool, value runtime.VMValue, spec vmTypeSpec) bool {
	if typeParams[spec.baseLower] {
		return true
	}
	if spec.kind == vmTypeUnion || spec.kind == vmTypeIntersection {
		return vm.matchValueToTypeSpec(typeParams, value.ToValue(), spec)
	}
	if value.Kind == runtime.VMKindUnset || value.Kind == runtime.VMKindNull {
		return spec.nullable
	}
	switch spec.kind {
	case vmTypeAny:
		return true
	case vmTypeInt:
		if value.Kind == runtime.VMKindSmallInt {
			return true
		}
		if value.Kind == runtime.VMKindBoxed {
			if _, ok := value.Boxed.(runtime.Int); ok {
				return true
			}
		}
		return false
	case vmTypeBool:
		return value.Kind == runtime.VMKindBool
	case vmTypeFloat:
		return value.Kind == runtime.VMKindFloat
	}
	// Fall back to the interface-form check for less common types.
	return vm.matchValueToTypeSpec(typeParams, value.ToValue(), spec)
}

// inferTypeBindingsFromLocals replicates the binding-inference loop in
// startFunctionWithValidation but reads from vm.localsStack (already
// set up by startFunctionVMValue via the new frame's basePointer)
// rather than a []runtime.Value arg list. Returns the inferred
// bindings so the caller can run constraint checks.
func (vm *VM) inferTypeBindingsFromLocals(function *FunctionInfo) map[string]string {
	typeParamSet := function.typeParamSet
	typeBindings := map[string]string{}
	// Seed with any explicit `<TypeArgs>` planted by OpPlantCallTypeBindings
	// before the matching OpCall. Inference's "skip if already exists" checks
	// below then leave these alone, giving explicit args strict priority
	// over types inferred from argument values. The pending map is consumed
	// here so a subsequent call without explicit args doesn't inherit them.
	if len(vm.pendingTypeBindings) > 0 {
		for k, v := range vm.pendingTypeBindings {
			if typeParamSet[strings.ToLower(k)] {
				typeBindings[k] = v
			}
		}
		vm.pendingTypeBindings = nil
	}
	for i, paramType := range function.ParamTypes {
		if i >= len(function.ParamSlots) {
			break
		}
		var spec vmTypeSpec
		if i < len(function.paramTypeSpecs) && function.paramTypeSpecs[i].raw != "" {
			spec = function.paramTypeSpecs[i]
		} else {
			spec = vm.typeSpec(paramType)
		}
		if len(spec.args) > 0 {
			slot := function.ParamSlots[i]
			v, err := vm.getLocal(slot)
			if err != nil || v == nil || isPendingDefault(v) {
				continue
			}
			vm.inferGenericBindingsFromSpec(spec, v, typeParamSet, typeBindings)
			continue
		}
		if name := strings.TrimPrefix(paramType, "?"); typeParamSet[strings.ToLower(name)] {
			slot := function.ParamSlots[i]
			if v, err := vm.getLocal(slot); err == nil && v != nil && v.TypeName() != "null" && !isPendingDefault(v) {
				if _, exists := typeBindings[name]; !exists {
					typeBindings[name] = runtime.ReifiedTypeName(v)
				}
			}
		}
	}
	if len(typeBindings) > 0 {
		vm.frames[len(vm.frames)-1].typeBindings = typeBindings
	}
	return typeBindings
}

// inferGenericBindingsFromSpec walks a parameter's type-spec tree
// against the concrete arg value to discover bindings for any leaf
// type-parameter references. Recurses into nested container shapes so
// `list<dict<K, V>>` populates both K and V from a single arg, not
// just the immediate level. Direct type-param leaves (e.g. just `T`)
// fall through to the caller's same-level path; this helper only
// fires when the spec has args (a generic container shape). Caller
// already null-checked v.
func (vm *VM) inferGenericBindingsFromSpec(spec vmTypeSpec, v runtime.Value, typeParamSet map[string]bool, typeBindings map[string]string) {
	if len(spec.args) == 0 {
		return
	}
	switch spec.baseLower {
	case "list":
		list, ok := v.(*runtime.List)
		if !ok || len(list.Elements) == 0 {
			return
		}
		vm.bindOrRecurse(spec.args[0], list.Elements[0], typeParamSet, typeBindings)
	case "set":
		set, ok := v.(runtime.Set)
		if !ok || len(set.Elements) == 0 {
			return
		}
		for _, entry := range set.Elements {
			vm.bindOrRecurse(spec.args[0], entry.Value, typeParamSet, typeBindings)
			break
		}
	case "dict":
		if len(spec.args) != 2 {
			return
		}
		d, ok := v.(runtime.Dict)
		if !ok || d.Len() == 0 {
			return
		}
		d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			vm.bindOrRecurse(spec.args[0], entry.Key, typeParamSet, typeBindings)
			vm.bindOrRecurse(spec.args[1], entry.Value, typeParamSet, typeBindings)
			return false
		})
	}
}

// bindOrRecurse is the leaf step of inferGenericBindingsFromSpec.
// When `spec` is a bare type-param reference, record the binding from
// the concrete value. Otherwise it is a nested container; recurse.
func (vm *VM) bindOrRecurse(spec vmTypeSpec, v runtime.Value, typeParamSet map[string]bool, typeBindings map[string]string) {
	if len(spec.args) == 0 {
		if !typeParamSet[strings.ToLower(spec.base)] {
			return
		}
		if _, isNull := v.(runtime.Null); v == nil || isNull {
			return
		}
		if _, exists := typeBindings[spec.base]; exists {
			return
		}
		typeBindings[spec.base] = runtime.ReifiedTypeName(v)
		return
	}
	vm.inferGenericBindingsFromSpec(spec, v, typeParamSet, typeBindings)
}

func (vm *VM) startPrevalidatedFunction(instruction Instruction, ip int, function *FunctionInfo, provided []runtime.Value, returnOverride runtime.Value) (int, error) {
	return vm.startFunctionWithValidation(instruction, ip, function, provided, returnOverride, false)
}

func (vm *VM) startFunctionWithValidation(instruction Instruction, ip int, function *FunctionInfo, provided []runtime.Value, returnOverride runtime.Value, validateTypes bool) (int, error) {
	maxDepth := vm.maxCallDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxCallDepth
	}
	if len(vm.frames) >= maxDepth {
		return 0, vm.fatalError(instruction, "maximum call depth exceeded (%d)", maxDepth)
	}
	argc := len(provided)
	if function.Variadic && len(function.ParamSlots) > 0 {
		variadicIndex := len(function.ParamSlots) - 1
		if argc >= variadicIndex {
			variadicElements := make([]runtime.Value, argc-variadicIndex)
			copy(variadicElements, provided[variadicIndex:])
			newProvided := make([]runtime.Value, variadicIndex+1)
			copy(newProvided, provided[:variadicIndex])
			newProvided[variadicIndex] = &runtime.List{Elements: variadicElements}
			provided = newProvided
			argc = len(provided)
		}
	} else if argc > len(function.ParamSlots) {
		return 0, vm.runtimeError(instruction, "%s expects at most %d arguments, got %d", function.Name, len(function.ParamSlots), argc)
	}
	required := len(function.ParamSlots)
	if function.Variadic && required > 0 {
		required--
	}
	for required > 0 && required <= len(function.DefaultConstants) && function.DefaultConstants[required-1] >= 0 {
		required--
	}
	if argc < required {
		// Name the first unfilled parameter that has no default.
		for i := argc; i < len(function.ParamNames); i++ {
			if function.Variadic && i == len(function.ParamNames)-1 {
				break
			}
			if i >= len(function.DefaultConstants) || function.DefaultConstants[i] < 0 {
				return 0, vm.runtimeError(instruction, "%s missing argument %s", function.Name, function.displayParamName(i))
			}
		}
		return 0, vm.runtimeError(instruction, "%s missing argument %d", function.Name, argc+1)
	}
	args := provided
	if function.Variadic || argc != len(function.ParamSlots) {
		args = make([]runtime.Value, len(function.ParamSlots))
		copy(args, provided)
		if function.Variadic && len(function.ParamSlots) > 0 {
			variadicIndex := len(function.ParamSlots) - 1
			if args[variadicIndex] == nil {
				args[variadicIndex] = &runtime.List{Elements: nil}
			}
		}
		for i := argc; i < len(args); i++ {
			if args[i] != nil {
				// Variadic slot already holds its bundled (or empty) list.
				continue
			}
			if i >= len(function.DefaultConstants) || function.DefaultConstants[i] < 0 {
				if i < len(function.ParamNames) {
					return 0, vm.runtimeError(instruction, "%s missing argument %s", function.Name, function.displayParamName(i))
				}
				return 0, vm.runtimeError(instruction, "%s missing argument %d", function.Name, i+1)
			}
			defaultIndex := function.DefaultConstants[i]
			if defaultIndex < 0 || int(defaultIndex) >= vm.constantsLen() {
				return 0, vm.runtimeError(instruction, "default argument constant out of range")
			}
			args[i] = cloneContainerDefault(vm.constantValue(defaultIndex))
		}
	}
	receiverBindings := map[string]string(nil)
	if function.requiresParamValidation && len(args) > 0 && !compiledConstructorName(function.Name) {
		receiverBindings = receiverTypeBindings(function, args[0])
	}
	if (validateTypes || len(receiverBindings) > 0) && function.requiresParamValidation {
		// Variadic annotations describe each element, not the packed list.
		typeParams := function.typeParamSet
		inherited := mergedTypeBindings(vm.pendingTypeBindings, receiverBindings)
		for i, arg := range args {
			if arg == nil || isPendingDefault(arg) || i >= len(function.ParamTypes) || function.ParamTypes[i] == "" {
				continue
			}
			values := []runtime.Value{arg}
			if function.Variadic && i == len(function.ParamSlots)-1 {
				values = arg.(*runtime.List).Elements
			}
			for _, arg := range values {
				if msg, ok := vm.paramMismatchMessage(function, i, arg, typeParams, inherited); !ok {
					return vm.throwTyped(instruction, ip, "TypeError", msg)
				}
			}
		}
	}
	// Construct the frame in place rather than building a callFrame value and
	// letting append copy the whole struct. For deep call stacks (fib's
	// ~243k calls) this avoids a duffcopy per push. The pop path zeroes the
	// fields that hold references when the frame is released, so we only
	// need to assign the active fields here.
	if len(vm.frames) == cap(vm.frames) {
		grown := make([]callFrame, len(vm.frames), cap(vm.frames)*2)
		copy(grown, vm.frames)
		vm.frames = grown
	}
	vm.frames = vm.frames[:len(vm.frames)+1]
	vm.syncMode = false
	frame := &vm.frames[len(vm.frames)-1]
	frame.returnIP = ip
	frame.returnOverride = returnOverride
	frame.functionName = function.Name
	if profiling.Active.Load() {
		vm.profileSample(0, true)
	}
	frame.meta = function.frameMeta
	frame.returnBindings = nil
	if function.frameMeta != nil && function.frameMeta.needsBindings {
		var receiver runtime.Value
		if len(args) > 0 {
			receiver = args[0]
		}
		frame.returnBindings = vm.returnBindingsFor(function, receiver)
	}
	frame.callLine = int(instruction.Line)
	frame.tailRepeat = 0
	frame.tailCallLine = 0
	frame.negateReturn = false
	frame.isErrorClass = false
	frame.isImmutableClass = false
	frame.immutableFieldsToLock = nil
	frame.lockInstance = nil
	frame.constraintInstance = nil
	frame.isDestructibleConstructor = false
	frame.shared = function.SharesParentFrame
	if function.IsGenerator && vm.generatorExecution {
		frame.generator = vm.generatorYield
		frame.generatorDone = vm.generatorDone
	} else {
		frame.generator = nil
		frame.generatorDone = nil
	}
	frame.typeBindings = nil
	// Reuse the backing array slot for this frame's defers rather than
	// allocating a new inner slice each call.
	frameDepth := len(vm.frames)
	if frameDepth < cap(vm.defers) {
		vm.defers = vm.defers[:frameDepth+1]
		vm.defers[frameDepth] = vm.defers[frameDepth][:0]
	} else {
		vm.defers = append(vm.defers, nil)
	}
	// Save the parent's locals slice handle (no copy) and either reuse
	// it for a nested function statement or allocate a fresh frame
	// slice for any other callee. This replaces the previous
	// snapshotLocals() deep copy.
	vm.pushLocalsStackFrame(frame, int(function.LocalCount), function.SharesParentFrame)
	for i, slot := range function.ParamSlots {
		if err := vm.setLocal(slot, args[i]); err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
	}
	// Infer type parameter bindings from param types and the bound local values.
	if len(function.TypeParameters) > 0 && len(function.ParamTypes) > 0 {
		typeParamSet := function.typeParamSet
		typeBindings := map[string]string{}
		for name, bound := range vm.pendingTypeBindings {
			typeBindings[name] = bound
		}
		for i, paramType := range function.ParamTypes {
			if i >= len(function.ParamSlots) {
				break
			}
			var spec vmTypeSpec
			if i < len(function.paramTypeSpecs) && function.paramTypeSpecs[i].raw != "" {
				spec = function.paramTypeSpecs[i]
			} else {
				spec = vm.typeSpec(paramType)
			}
			if len(spec.args) > 0 {
				slot := function.ParamSlots[i]
				if v, err := vm.getLocal(slot); err == nil && v != nil && !isPendingDefault(v) {
					vm.inferGenericBindingsFromSpec(spec, v, typeParamSet, typeBindings)
				}
				continue
			}
			// Direct T parameter.
			if name := strings.TrimPrefix(paramType, "?"); typeParamSet[strings.ToLower(name)] {
				slot := function.ParamSlots[i]
				if v, err := vm.getLocal(slot); err == nil && v != nil && v.TypeName() != "null" && !isPendingDefault(v) {
					if _, exists := typeBindings[name]; !exists {
						typeBindings[name] = runtime.ReifiedTypeName(v)
					}
				}
			}
		}
		if len(typeBindings) > 0 {
			vm.frames[len(vm.frames)-1].typeBindings = typeBindings
		}
		if err := vm.checkTypeParamConstraints(instruction, function, typeBindings); err != nil {
			return 0, err
		}
	}
	// If called with a forwarded receiver (e.g. via a callable decorator), inherit
	// its type bindings so that instanceof T checks work inside decorated methods.
	if vm.forwardThis != nil {
		vm.inheritInstanceTypeBindings(vm.forwardThis)
	}
	// If this call inherited type bindings from a closure value (lambda
	// or generic-function-as-reference captured from an outer generic
	// frame) OR from OpPlantCallTypeBindings staging explicit `<TypeArgs>`,
	// merge them into the new frame so the body's instanceof T checks and
	// inner OpMakeClosure captures see the correct bindings. Pending
	// bindings are consumed here; the closure-inheritance path uses
	// save/restore semantics around its caller so the restore still sees
	// the original prev value regardless of this clear.
	if len(vm.pendingTypeBindings) > 0 && len(vm.frames) > 0 {
		frame := &vm.frames[len(vm.frames)-1]
		if frame.typeBindings == nil {
			frame.typeBindings = map[string]string{}
		}
		for k, v := range vm.pendingTypeBindings {
			if _, exists := frame.typeBindings[k]; !exists {
				frame.typeBindings[k] = v
			}
		}
		vm.pendingTypeBindings = nil
	}
	return int(function.Entry) - 1, nil
}

func (vm *VM) startClosureFunction(instruction Instruction, ip int, closure runtime.BytecodeClosure, args []runtime.Value) (int, error) {
	if int(closure.FunctionIndex) >= len(vm.curMod.Chunk.Functions) {
		return 0, vm.runtimeError(instruction, "closure function index out of range")
	}
	function := &vm.curMod.Chunk.Functions[closure.FunctionIndex]
	if function.Async && !vm.syncMode {
		vm.push(vm.startAsyncCallable(closure, args))
		return ip, nil
	}
	if function.IsGenerator && !vm.generatorExecution {
		vm.push(vm.lazyClosureGenerator(closure, args))
		return ip, nil
	}
	upvalueCount := int(function.UpvalueCount)
	provided := make([]runtime.Value, upvalueCount+len(args))
	copy(provided[:upvalueCount], closure.Upvalues)
	copy(provided[upvalueCount:], args)
	if len(closure.TypeBindings) > 0 {
		return vm.startFunctionWithBindings(instruction, ip, function, provided, nil, closure.TypeBindings)
	}
	return vm.startFunction(instruction, ip, function, provided, nil)
}

func (vm *VM) nativeCall(instruction Instruction) error {
	if len(instruction.Operands) != 2 {
		return vm.fatalError(instruction, "native call instruction has invalid operands")
	}
	nameIndex := instruction.Operands[0]
	argc := instruction.Operands[1]
	if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
		return vm.runtimeError(instruction, "native call name out of range")
	}
	if gen := native.PatchGeneration(); gen != vm.nativeCacheGen {
		vm.invalidateNativeCache()
		vm.nativeCacheGen = gen
	}
	if int(nameIndex) < len(vm.nativeCache) {
		if fn := vm.nativeCache[nameIndex]; fn != nil {
			args := vm.takeCallArgsBuffer(int(argc))
			for i := int(argc) - 1; i >= 0; i-- {
				value, err := vm.pop()
				if err != nil {
					vm.releaseCallArgsBuffer(args)
					return vm.callPropagate(instruction, err)
				}
				args[i] = value
			}
			result, err := fn(args)
			vm.releaseCallArgsBuffer(args)
			if err != nil {
				return recoverableNativeError{err: err}
			}
			vm.push(result)
			return nil
		}
	}
	name, ok := vm.constantValue(nameIndex).(runtime.String)
	if !ok {
		return vm.runtimeError(instruction, "native call name must be string")
	}
	args := make([]runtime.Value, argc)
	for i := int(argc) - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	// Pure builtins always terminate at the registry regardless of
	// whether a stateful caller is wired, so they are cacheable per
	// name-constant either way. The excluded modules have VM-inline or
	// embedder-observable routing that must stay dynamic.
	if name.Value != "errors.is" {
		if module, function, ok := strings.Cut(name.Value, "."); ok &&
			module != "collections" && module != "reflect" && module != "async" &&
			!isStatefulNativeCall(module, function) {
			if fn := vm.natives.LookupKey(name.Value); fn != nil && int(nameIndex) < len(vm.nativeCache) {
				vm.nativeCache[nameIndex] = fn
			}
		}
	}
	vm.nativeCallLine = int(instruction.Line)
	result, err := vm.evalNativeCall(name.Value, args)
	if err != nil {
		return recoverableNativeError{err: err}
	}
	vm.push(result)
	return nil
}

func (vm *VM) nativeCallSpread(instruction Instruction) error {
	if len(instruction.Operands) < 2 {
		return vm.fatalError(instruction, "native call-spread instruction has invalid operands")
	}
	nameIndex := instruction.Operands[0]
	argc := int(instruction.Operands[1])
	if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
		return vm.runtimeError(instruction, "native call name out of range")
	}
	name, ok := vm.constantValue(nameIndex).(runtime.String)
	if !ok {
		return vm.runtimeError(instruction, "native call name must be string")
	}
	if len(instruction.Operands) != 2+argc {
		return vm.runtimeError(instruction, "native call-spread argument metadata mismatch")
	}
	args, _, _, hasNamed, err := vm.collectSpreadCallArgs(instruction, instruction.Operands[2:], false)
	if err != nil {
		return err
	}
	if hasNamed {
		// The evaluator rejects named args mixed with spread on natives.
		return recoverableNativeError{err: fmt.Errorf("named arguments are only supported for Geblang functions and methods")}
	}
	vm.nativeCallLine = int(instruction.Line)
	result, err := vm.evalNativeCall(name.Value, args)
	if err != nil {
		return recoverableNativeError{err: err}
	}
	vm.push(result)
	return nil
}

func (vm *VM) nativeCallNamed(instruction Instruction) error {
	if len(instruction.Operands) < 2 {
		return vm.fatalError(instruction, "named native call instruction has invalid operands")
	}
	nameIndex := instruction.Operands[0]
	argc := instruction.Operands[1]
	if argc < 0 || len(instruction.Operands) != int(argc)+2 {
		return vm.runtimeError(instruction, "named native call argument metadata mismatch")
	}
	if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
		return vm.runtimeError(instruction, "native call name out of range")
	}
	name, ok := vm.constantValue(nameIndex).(runtime.String)
	if !ok {
		return vm.runtimeError(instruction, "native call name must be string")
	}
	args := make([]runtime.Value, argc)
	for i := int(argc) - 1; i >= 0; i-- {
		value, err := vm.pop()
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		args[i] = value
	}
	names := make([]string, argc)
	for i := int64(0); i < argc; i++ {
		argName, err := vm.constantStringAt(instruction, instruction.Operands[i+2], "argument name constant must be string")
		if err != nil {
			return err
		}
		names[i] = argName
	}
	vm.nativeCallLine = int(instruction.Line)
	result, err := vm.evalNativeCallWithNames(name.Value, args, names)
	if err != nil {
		return recoverableNativeError{err: err}
	}
	vm.push(result)
	return nil
}

func vmBindingSignature(function FunctionInfo, paramOffset int) argbinding.Signature {
	paramNames := function.ParamNames
	if paramOffset <= len(paramNames) {
		paramNames = paramNames[paramOffset:]
	} else {
		paramNames = nil
	}
	hasDefault := make([]bool, len(paramNames))
	for i := range paramNames {
		paramIndex := i + paramOffset
		hasDefault[i] = paramIndex < len(function.DefaultConstants) && function.DefaultConstants[paramIndex] >= 0
	}
	return argbinding.Signature{
		FuncName:   function.Name,
		ParamNames: paramNames,
		HasDefault: hasDefault,
		Variadic:   function.Variadic,
	}
}

func (vm *VM) orderRuntimeArguments(instruction Instruction, function FunctionInfo, args []runtime.Value, names []string, paramOffset int) ([]runtime.Value, error) {
	if len(args) != len(names) {
		return nil, vm.runtimeError(instruction, "argument metadata mismatch")
	}
	if len(function.ParamNames) < paramOffset {
		return nil, vm.runtimeError(instruction, "function metadata for %s has invalid receiver offset", function.Name)
	}
	sig := vmBindingSignature(function, paramOffset)
	bargs := make([]argbinding.Arg, len(args))
	for i, name := range names {
		bargs[i].Name = name
	}
	result, err := argbinding.Order(sig, bargs)
	if err != nil {
		return nil, vm.callPropagate(instruction, err)
	}
	ordered := make([]runtime.Value, len(result.Slots))
	assigned := make([]bool, len(result.Slots))
	for i, slot := range result.Slots {
		if slot != argbinding.DefaultSlot {
			ordered[i] = args[slot]
			assigned[i] = true
		}
	}
	for _, argIndex := range result.TailArgs {
		// Positional overflow into the variadic slot arrives unpacked;
		// downstream packing consumes the trailing run.
		ordered = append(ordered, args[argIndex])
		assigned = append(assigned, true)
	}
	variadicIndex := -1
	if function.Variadic && len(function.ParamNames) > paramOffset {
		variadicIndex = len(function.ParamNames) - 1 - paramOffset
	}
	if variadicIndex >= 0 && variadicIndex == len(ordered)-1 && !assigned[variadicIndex] {
		ordered = ordered[:variadicIndex]
		assigned = assigned[:variadicIndex]
	}
	for i := range ordered {
		if assigned[i] {
			continue
		}
		paramIndex := i + paramOffset
		defaultIndex := function.DefaultConstants[paramIndex]
		if defaultIndex < 0 || int(defaultIndex) >= vm.constantsLen() {
			return nil, vm.runtimeError(instruction, "default argument constant out of range")
		}
		ordered[i] = cloneContainerDefault(vm.constantValue(defaultIndex))
		assigned[i] = true
	}
	// Normalise away trailing arguments that equal their declared
	// default so overload selection and re-dispatch see one canonical
	// shape; downstream default fill restores them.
	for len(ordered) > 0 {
		paramIndex := len(ordered) - 1 + paramOffset
		if paramIndex >= len(function.DefaultConstants) || function.DefaultConstants[paramIndex] < 0 {
			break
		}
		defaultIndex := function.DefaultConstants[paramIndex]
		last := len(ordered) - 1
		if last < len(result.Slots) && result.Slots[last] != argbinding.DefaultSlot {
			// An explicitly supplied collection keeps its caller's identity.
			switch ordered[last].(type) {
			case *runtime.List, runtime.Dict, runtime.Set:
				return ordered, nil
			}
		}
		if assigned[len(ordered)-1] && defaultIndex >= 0 && int(defaultIndex) < vm.constantsLen() && valuesEqual(ordered[len(ordered)-1], vm.constantValue(defaultIndex)) {
			ordered = ordered[:len(ordered)-1]
			continue
		}
		break
	}
	return ordered, nil
}

func spreadDictNamedArguments(dict runtime.Dict, positional []runtime.Value, paramNames []string) ([]runtime.Value, []string, error) {
	known := map[string]bool{}
	for _, name := range paramNames {
		known[strings.ToLower(name)] = true
	}
	type namedArg struct {
		name  string
		value runtime.Value
	}
	named := make([]namedArg, 0, dict.Len())
	var keyErr error
	dict.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		key, ok := entry.Key.(runtime.String)
		if !ok {
			keyErr = fmt.Errorf("spread dict argument keys must be strings")
			return false
		}
		if len(known) > 0 && !known[strings.ToLower(key.Value)] {
			return true
		}
		named = append(named, namedArg{name: key.Value, value: entry.Value})
		return true
	})
	if keyErr != nil {
		return nil, nil, keyErr
	}
	sort.Slice(named, func(i, j int) bool { return named[i].name < named[j].name })
	args := make([]runtime.Value, 0, len(positional)+len(named))
	names := make([]string, 0, len(positional)+len(named))
	for _, value := range positional {
		args = append(args, value)
		names = append(names, "")
	}
	for _, arg := range named {
		args = append(args, arg.value)
		names = append(names, arg.name)
	}
	return args, names, nil
}

// collectSpreadCallArgs builds the flat ordered (args, names, fromSpread) a spread call's metadata describes, mirroring the evaluator so any mix of positional/named/spread agrees on both backends.
func (vm *VM) collectSpreadCallArgs(instruction Instruction, meta []int64, allowDict bool) ([]runtime.Value, []string, []bool, bool, error) {
	argc := len(meta)
	vals := make([]runtime.Value, argc)
	for i := argc - 1; i >= 0; i-- {
		v, err := vm.pop()
		if err != nil {
			return nil, nil, nil, false, vm.callPropagate(instruction, err)
		}
		vals[i] = v
	}
	args := make([]runtime.Value, 0, argc)
	names := make([]string, 0, argc)
	fromSpread := make([]bool, 0, argc)
	hasNamed := false
	for i, m := range meta {
		switch {
		case m == spreadArgMeta:
			switch sv := vals[i].(type) {
			case *runtime.List:
				for _, el := range sv.Elements {
					args = append(args, el)
					names = append(names, "")
					fromSpread = append(fromSpread, false)
				}
			case runtime.Dict:
				if !allowDict {
					return nil, nil, nil, false, vm.runtimeError(instruction, "spread argument must be a list")
				}
				dargs, dnames, err := spreadDictNamedArguments(sv, nil, nil)
				if err != nil {
					return nil, nil, nil, false, vm.runtimeError(instruction, "%s", err.Error())
				}
				for k := range dargs {
					args = append(args, dargs[k])
					names = append(names, dnames[k])
					fromSpread = append(fromSpread, true)
					hasNamed = true
				}
			default:
				if allowDict {
					return nil, nil, nil, false, vm.runtimeError(instruction, "spread argument must be a list or dict")
				}
				return nil, nil, nil, false, vm.runtimeError(instruction, "spread argument must be a list")
			}
		case m == positionalArgMeta:
			args = append(args, vals[i])
			names = append(names, "")
			fromSpread = append(fromSpread, false)
		default:
			nm, ok := vm.constantValue(m).(runtime.String)
			if !ok {
				return nil, nil, nil, false, vm.runtimeError(instruction, "argument name constant must be string")
			}
			args = append(args, vals[i])
			names = append(names, nm.Value)
			fromSpread = append(fromSpread, false)
			hasNamed = true
		}
	}
	return args, names, fromSpread, hasNamed, nil
}

// collectDeferSpreadArgs pops a spread defer's registration-time values without expanding spreads (elements are read at fire time, frozen-reference semantics).
func (vm *VM) collectDeferSpreadArgs(instruction Instruction, meta []int64) ([]runtime.Value, []string, []bool, error) {
	argc := len(meta)
	args := make([]runtime.Value, argc)
	for i := argc - 1; i >= 0; i-- {
		v, err := vm.pop()
		if err != nil {
			return nil, nil, nil, vm.runtimeError(instruction, "%s", err.Error())
		}
		args[i] = v
	}
	names := make([]string, argc)
	mask := make([]bool, argc)
	for i, m := range meta {
		switch {
		case m == spreadArgMeta:
			mask[i] = true
		case m == positionalArgMeta:
		default:
			nm, ok := vm.constantValue(m).(runtime.String)
			if !ok {
				return nil, nil, nil, vm.runtimeError(instruction, "argument name constant must be string")
			}
			names[i] = nm.Value
		}
	}
	return args, names, mask, nil
}

// expandDeferredSpreadArgs splices each frozen spread reference's current elements in place, returning the flat (args, names) plus whether any arg is named.
func expandDeferredSpreadArgs(action deferredAction) ([]runtime.Value, []string, bool, error) {
	args := make([]runtime.Value, 0, len(action.args))
	names := make([]string, 0, len(action.args))
	hasNamed := false
	for i, v := range action.args {
		if action.spreadMask[i] {
			list, ok := v.(*runtime.List)
			if !ok {
				return nil, nil, false, fmt.Errorf("spread argument must be a list")
			}
			for _, el := range list.Elements {
				args = append(args, el)
				names = append(names, "")
			}
			continue
		}
		args = append(args, v)
		name := ""
		if i < len(action.names) {
			name = action.names[i]
		}
		if name != "" {
			hasNamed = true
		}
		names = append(names, name)
	}
	return args, names, hasNamed, nil
}

// dropUnknownSpreadNames removes dict-spread named args whose key is not a declared parameter, matching the evaluator's fromSpread silent drop.
func dropUnknownSpreadNames(args []runtime.Value, names []string, fromSpread []bool, paramNames []string) ([]runtime.Value, []string) {
	anyFrom := false
	for _, f := range fromSpread {
		if f {
			anyFrom = true
			break
		}
	}
	if !anyFrom || len(paramNames) == 0 {
		return args, names
	}
	known := make(map[string]bool, len(paramNames))
	for _, p := range paramNames {
		known[strings.ToLower(p)] = true
	}
	fa := make([]runtime.Value, 0, len(args))
	fn := make([]string, 0, len(names))
	for i := range args {
		if fromSpread[i] && names[i] != "" && !known[strings.ToLower(names[i])] {
			continue
		}
		fa = append(fa, args[i])
		fn = append(fn, names[i])
	}
	return fa, fn
}

func (vm *VM) evalNativeCall(name string, args []runtime.Value) (runtime.Value, error) {
	return vm.evalNativeCallWithNames(name, args, nil)
}

// bindNativeNamedArgs reorders named arguments to a native's declared parameter order; returns (reordered, true) when any name was present, or an error matching the evaluator when the native has no signature.
func (vm *VM) bindNativeNamedArgs(name string, args []runtime.Value, names []string) ([]runtime.Value, bool, error) {
	named := false
	for _, n := range names {
		if n != "" {
			named = true
			break
		}
	}
	if !named {
		return args, false, nil
	}
	module, function, _ := strings.Cut(name, ".")
	bargs := make([]argbinding.Arg, len(args))
	for i := range args {
		if i < len(names) {
			bargs[i].Name = names[i]
		}
	}
	sig, ok := native.NativeCallSignature(module, function, bargs)
	if !ok {
		return nil, false, fmt.Errorf("named arguments are only supported for Geblang functions and methods")
	}
	result, err := argbinding.Order(sig, bargs)
	if err != nil {
		return nil, false, err
	}
	indices, err := argbinding.NativeArgOrder(sig, result)
	if err != nil {
		return nil, false, err
	}
	ordered := make([]runtime.Value, len(indices))
	for k, idx := range indices {
		ordered[k] = args[idx]
	}
	return ordered, true, nil
}

func (vm *VM) evalNativeCallWithNames(name string, args []runtime.Value, names []string) (runtime.Value, error) {
	if module, function, _ := strings.Cut(name, "."); len(names) > 0 && native.ForwardsNamedArgs(module, function) {
		order, outNames, err := native.ForwardedArgOrder(module, function, names)
		if err != nil {
			return nil, err
		}
		reordered := make([]runtime.Value, len(order))
		for k, idx := range order {
			reordered[k] = args[idx]
		}
		args, names = reordered, outNames
	} else if reordered, bound, err := vm.bindNativeNamedArgs(name, args, names); err != nil {
		return nil, err
	} else if bound {
		args, names = reordered, nil
	}
	if patch := native.LookupPatch(name); patch != nil {
		return patch(args)
	}
	if strings.HasPrefix(name, "collections.") {
		return vm.collectionsNativeCall(name[len("collections."):], args)
	}
	if strings.HasPrefix(name, "reflect.") {
		return vm.reflectNativeCall(name[len("reflect."):], args)
	}
	if name == "async.run" {
		return vm.asyncRun(args)
	}
	// async.{sleep,await,done,cancel,all,race,timeout} are self-contained on
	// runtime.Task primitives. When a stateful native caller has been
	// configured we still route through it so embedders can observe the calls
	// (existing TestVMRunsAsyncModuleThroughStatefulNativeBridge expects this).
	if vm.statefulNative == nil {
		if name == "async.sleep" {
			return vm.asyncSleepNative(args)
		}
		if name == "async.await" {
			return vm.asyncAwaitNative(args)
		}
		if name == "async.done" {
			return vm.asyncDoneNative(args)
		}
		if name == "async.cancel" {
			return vm.asyncCancelNative(args)
		}
		if name == "async.all" {
			return vm.asyncAllNative(args)
		}
		if name == "async.race" {
			return vm.asyncRaceNative(args)
		}
		if name == "async.timeout" {
			return vm.asyncTimeoutNative(args)
		}
	}
	// errors.is must use the VM's own hierarchy walk so user-defined class
	// hierarchies (stored in vm.chunk.Classes) are consulted correctly.
	if name == "errors.is" {
		return vm.errorsIs(args)
	}
	if module, function, ok := strings.Cut(name, "."); ok && isStatefulNativeCall(module, function) {
		return vm.statefulNativeCall(module, function, args, names)
	}
	return vm.natives.CallKey(name, args)
}

func (vm *VM) asyncRun(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.run expects one function argument")
	}
	switch fn := args[0].(type) {
	case runtime.BytecodeFunction:
		return vm.startAsyncCallable(fn, nil), nil
	case runtime.BytecodeClosure:
		return vm.startAsyncCallable(fn, nil), nil
	case runtime.OverloadedFunction:
		return vm.startAsyncCallable(fn, nil), nil
	case runtime.Function:
		// A cross-module callable arrives wrapped as a Native-backed runtime.Function (wrapStatefulNativeValue).
		if fn.Native == nil {
			return nil, fmt.Errorf("async.run expects a function, got %s", args[0].TypeName())
		}
		return vm.startAsyncCallable(fn, nil), nil
	default:
		return nil, fmt.Errorf("async.run expects a function, got %s", args[0].TypeName())
	}
}

func (vm *VM) asyncSleepNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.sleep expects one argument (milliseconds)")
	}
	ms, ok := asInt64Ms(args[0])
	if !ok {
		return nil, fmt.Errorf("async.sleep expects a numeric millisecond value")
	}
	task := runtime.NewTask()
	go func() {
		if ms > 0 {
			select {
			case <-time.After(time.Duration(ms) * time.Millisecond):
			case <-task.CancelChan():
				return
			}
		}
		task.Complete(runtime.Null{}, nil)
	}()
	return task, nil
}

func (vm *VM) asyncAwaitNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.await expects one task")
	}
	if task, ok := args[0].(*runtime.Task); ok {
		result := task.Await()
		if result.Err != nil {
			return nil, result.Err
		}
		if result.Value == nil {
			return runtime.Null{}, nil
		}
		return result.Value, nil
	}
	return args[0], nil
}

func (vm *VM) asyncDoneNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.done expects one task")
	}
	task, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("async.done expects Task")
	}
	return runtime.Bool{Value: task.Done()}, nil
}

func (vm *VM) asyncCancelNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.cancel expects one task")
	}
	task, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("async.cancel expects Task")
	}
	task.Cancel()
	return runtime.Null{}, nil
}

func (vm *VM) asyncTimeoutNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("async.timeout expects (task, milliseconds)")
	}
	inner, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("async.timeout expects a Task as the first argument")
	}
	ms, ok := asInt64Ms(args[1])
	if !ok {
		return nil, fmt.Errorf("async.timeout expects a numeric millisecond value")
	}
	out := runtime.NewTask()
	go func() {
		select {
		case <-inner.DoneChan():
			result := inner.Await()
			out.Complete(result.Value, result.Err)
		case <-time.After(time.Duration(ms) * time.Millisecond):
			inner.Cancel()
			out.Complete(runtime.Null{}, runtime.ClassifiedError{Class: "TimeoutError", Message: fmt.Sprintf("async.timeout: task did not complete within %dms", ms)})
		}
	}()
	return out, nil
}

func (vm *VM) asyncAllNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.all expects one list of tasks")
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("async.all expects a list of tasks")
	}
	tasks := make([]*runtime.Task, 0, len(list.Elements))
	for i, el := range list.Elements {
		task, ok := el.(*runtime.Task)
		if !ok {
			return nil, fmt.Errorf("async.all: element %d is not a Task", i)
		}
		tasks = append(tasks, task)
	}
	out := runtime.NewTask()
	go func() {
		results := make([]runtime.Value, len(tasks))
		for i, t := range tasks {
			r := t.Await()
			if r.Err != nil {
				for j, sibling := range tasks {
					if j != i {
						sibling.Cancel()
					}
				}
				out.Complete(runtime.Null{}, r.Err)
				return
			}
			results[i] = r.Value
		}
		out.Complete(&runtime.List{Elements: results}, nil)
	}()
	return out, nil
}

func (vm *VM) asyncRaceNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("async.race expects one list of tasks")
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("async.race expects a list of tasks")
	}
	if len(list.Elements) == 0 {
		return nil, fmt.Errorf("async.race requires at least one task")
	}
	tasks := make([]*runtime.Task, 0, len(list.Elements))
	for i, el := range list.Elements {
		task, ok := el.(*runtime.Task)
		if !ok {
			return nil, fmt.Errorf("async.race: element %d is not a Task", i)
		}
		tasks = append(tasks, task)
	}
	out := runtime.NewTask()
	go func() {
		winner := make(chan int, len(tasks))
		for i, t := range tasks {
			i, t := i, t
			go func() {
				<-t.DoneChan()
				winner <- i
			}()
		}
		first := <-winner
		for j, sibling := range tasks {
			if j != first {
				sibling.Cancel()
			}
		}
		r := tasks[first].Await()
		out.Complete(r.Value, r.Err)
	}()
	return out, nil
}

// asInt64Ms extracts a millisecond count from a runtime.Value supporting
// SmallInt, Int (big.Int that fits in int64), and Float.
func asInt64Ms(v runtime.Value) (int64, bool) {
	switch n := v.(type) {
	case runtime.SmallInt:
		return n.Value, true
	case runtime.Int:
		if n.Value.IsInt64() {
			return n.Value.Int64(), true
		}
	case runtime.Float:
		return int64(n.Value), true
	}
	return 0, false
}

func (vm *VM) statefulNativeCall(module, function string, args []runtime.Value, names []string) (runtime.Value, error) {
	if vm.statefulNative == nil {
		return nil, fmt.Errorf("stateful native module %s is not configured for VM execution", module)
	}
	return vm.statefulNative.CallBuiltin(module, function, vm.wrapStatefulNativeArgs(module, function, args), names)
}

func (vm *VM) shouldRouteDirectPrint() bool {
	if vm.statefulNative == nil {
		return false
	}
	router, ok := vm.statefulNative.(directPrintStatefulNative)
	return ok && router.HandleDirectPrint()
}

func (vm *VM) wrapStatefulNativeArgs(module, function string, args []runtime.Value) []runtime.Value {
	if len(args) == 0 {
		return args
	}
	share := serverShareHandler(module, function, args)
	wrapped := make([]runtime.Value, len(args))
	for i, arg := range args {
		wrapped[i] = vm.wrapStatefulNativeValue(arg, serverHandlerArg(module, function, i) && !share)
	}
	return wrapped
}

// serverShareHandler reports whether an http.serve/listen or net.serve call set opts.shareHandler to share (not isolate) its handler; opts sits after the handler, whose position differs per server.
func serverShareHandler(module, function string, args []runtime.Value) bool {
	optsIdx := -1
	switch {
	case module == "http" && (function == "serve" || function == "listen"):
		optsIdx = 2
	case module == "net" && function == "serve":
		optsIdx = 3
	}
	if optsIdx < 0 || len(args) <= optsIdx {
		return false
	}
	d, ok := args[optsIdx].(runtime.Dict)
	if !ok {
		return false
	}
	shared := false
	d.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
		if k, ok := e.Key.(runtime.String); ok && k.Value == "shareHandler" {
			if b, ok := e.Value.(runtime.Bool); ok {
				shared = b.Value
			}
			return false
		}
		return true
	})
	return shared
}

// serverHandlerArg reports whether arg i of a stateful-native call is a direct
// server-handler closure (http.serve/listen, net.serve) that runs per-request
// isolated so concurrent requests do not share/race handler state. Other
// callbacks (async tasks, synchronous transaction bodies) keep write-back.
// Framework route handlers that cross module boundaries (web.router/gebweb) are
// not covered here - that is a separate, scoped project
// (docs/http-concurrency-evaluation.md).
func serverHandlerArg(module, function string, argIndex int) bool {
	switch module {
	case "http":
		return (function == "serve" || function == "listen") && argIndex == 1
	case "net":
		return function == "serve" && argIndex == 2
	case "sys":
		// Signal handlers fire on a signal goroutine while the host VM
		// may still be running; they share state through `store` only.
		return function == "onSignal" && argIndex == 1
	}
	return false
}

func (vm *VM) wrapStatefulNativeValue(value runtime.Value, isolated bool) runtime.Value {
	switch callable := value.(type) {
	case runtime.BytecodeFunction:
		// The wrapped Native closure can be invoked later from another
		// goroutine (timer fires, HTTP handler, etc.). From this point on,
		// any concurrent write-back into vm.globals must serialise with
		// the parent's setGlobalVM, so flip bridgeActive monotonically.
		vm.bridgeActive.Store(true)
		vm.noteEscape()
		return runtime.Function{
			Name:        callable.Name,
			Parameters:  bridgedParameters(callable.Parameters),
			Metadata:    vm.bridgedFunctionMetadata(callable),
			NativeNamed: vm.bridgeNamedInvoker(callable, isolated),
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if isolated {
					return vm.callCallableIsolated(callable, args)
				}
				return vm.callCallableSlow(callable, args)
			},
			BridgeInvoke: func(host any, args []runtime.Value) (runtime.Value, error) {
				if isolated {
					return vm.callCallableIsolated(callable, args)
				}
				h, _ := host.(*VM)
				return vm.callCallableSlowHosted(callable, args, h)
			},
		}
	case runtime.BytecodeClosure:
		vm.bridgeActive.Store(true)
		vm.noteEscape()
		return runtime.Function{
			Name:        callable.Name,
			Parameters:  bridgedParameters(vm.closureParameters(callable)),
			Metadata:    vm.bridgedFunctionMetadata(callable),
			NativeNamed: vm.bridgeNamedInvoker(callable, isolated),
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if isolated {
					return vm.callCallableIsolated(callable, args)
				}
				return vm.callCallableSlow(callable, args)
			},
			BridgeInvoke: func(host any, args []runtime.Value) (runtime.Value, error) {
				if isolated {
					return vm.callCallableIsolated(callable, args)
				}
				h, _ := host.(*VM)
				return vm.callCallableSlowHosted(callable, args, h)
			},
		}
	case runtime.OverloadedFunction:
		if len(callable.Callables) == 0 {
			return callable
		}
		callable.Overloads = append([]runtime.Function(nil), callable.Overloads...)
		for i, bound := range callable.Callables {
			bridged := vm.wrapStatefulNativeValue(bound, isolated).(runtime.Function)
			callable.Overloads[i].Native = func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				ordered, err := bindBoundArguments(callable, i, args, nil)
				if err != nil {
					return nil, err
				}
				return bridged.Native(nil, ordered)
			}
			callable.Overloads[i].BridgeInvoke = func(host any, args []runtime.Value) (runtime.Value, error) {
				ordered, err := bindBoundArguments(callable, i, args, nil)
				if err != nil {
					return nil, err
				}
				return bridged.BridgeInvoke(host, ordered)
			}
			callable.Overloads[i].NativeNamed = func(_ *runtime.Instance, args []runtime.Value, names []string) (runtime.Value, error) {
				ordered, err := bindBoundArguments(callable, i, args, names)
				if err != nil {
					return nil, err
				}
				return bridged.Native(nil, ordered)
			}
		}
		return callable

	case *runtime.List:
		// Share the list (reference semantics like the evaluator); bridge nested closures in place.
		for i := range callable.Elements {
			switch callable.Elements[i].(type) {
			case runtime.BytecodeFunction, runtime.BytecodeClosure, runtime.OverloadedFunction:
				callable.Elements[i] = vm.wrapStatefulNativeValue(callable.Elements[i], false)
			case *runtime.List, runtime.Dict:
				vm.wrapStatefulNativeValue(callable.Elements[i], false)
			}
		}
		return callable
	case runtime.Dict:
		// Share the dict; bridge nested closures in place so callee mutations stay visible.
		var rewriteKeys []string
		var rewriteVals []runtime.Value
		callable.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			switch entry.Value.(type) {
			case runtime.BytecodeFunction, runtime.BytecodeClosure, runtime.OverloadedFunction:
				rewriteKeys = append(rewriteKeys, key)
				rewriteVals = append(rewriteVals, vm.wrapStatefulNativeValue(entry.Value, false))
			case *runtime.List, runtime.Dict:
				vm.wrapStatefulNativeValue(entry.Value, false)
			}
			return true
		})
		for i, key := range rewriteKeys {
			e, _ := callable.GetEntry(key)
			callable.PutEntry(key, runtime.DictEntry{Key: e.Key, Value: rewriteVals[i]})
		}
		return callable
	default:
		return value
	}
}

// closureParameters resolves a bridged closure's parameter metadata from
// the function table so a callback's declared parameter types survive the
// stateful-native boundary (e.g. an http.serve handler typed func(Request)).
func (vm *VM) closureParameters(closure runtime.BytecodeClosure) []runtime.ParameterMetadata {
	idx := int(closure.FunctionIndex)
	if idx < 0 || idx >= len(vm.chunk.Functions) {
		return nil
	}
	return parameterMetadataFromFunctionInfo(vm.chunk.Functions[idx], 0)
}

// bridgedFunctionMetadata resolves the reflection metadata of a bytecode function value at the module boundary so reflect.parameters/returnType survive the native-wrapper bridge; nil when the home chunk is a different module.
func (vm *VM) bridgedFunctionMetadata(value runtime.Value) *runtime.FunctionMetadata {
	switch callable := value.(type) {
	case runtime.BytecodeFunction:
		if callable.Module == "" || callable.Module == vm.moduleName {
			return bytecodeFunctionMetadata(vm.bytecodeFunctionValue(callable.Index, false))
		}
		return bytecodeFunctionMetadata(callable)
	case runtime.BytecodeClosure:
		if callable.Module == "" || callable.Module == vm.moduleName {
			return bytecodeFunctionMetadata(vm.bytecodeFunctionValue(callable.FunctionIndex, false))
		}
	}
	return nil
}

// bridgeLocalFunctionInfo returns the home FunctionInfo and receiver offset for a bridge callable declared in this VM's own chunk; ok is false for a foreign-chunk callable whose defaults this VM cannot resolve.
func (vm *VM) bridgeLocalFunctionInfo(callable runtime.Value) (FunctionInfo, int, bool) {
	switch c := callable.(type) {
	case runtime.BytecodeFunction:
		if (c.Module == "" || c.Module == vm.moduleName) && int(c.Index) >= 0 && int(c.Index) < len(vm.curMod.Chunk.Functions) {
			return vm.curMod.Chunk.Functions[c.Index], 0, true
		}
	case runtime.BytecodeClosure:
		if (c.Module == "" || c.Module == vm.moduleName) && int(c.FunctionIndex) >= 0 && int(c.FunctionIndex) < len(vm.curMod.Chunk.Functions) {
			info := vm.curMod.Chunk.Functions[c.FunctionIndex]
			return info, int(info.UpvalueCount), true
		}
	}
	return FunctionInfo{}, 0, false
}

// bridgeNamedInvoker builds the named-argument entry for a bridge wrapper over a home-chunk callable: it orders named args against the real signature (engaging declared defaults, including for an omitted middle parameter) then invokes positionally; nil for a foreign-chunk callable.
func (vm *VM) bridgeNamedInvoker(callable runtime.Value, isolated bool) func(*runtime.Instance, []runtime.Value, []string) (runtime.Value, error) {
	info, offset, ok := vm.bridgeLocalFunctionInfo(callable)
	if !ok {
		return nil
	}
	return func(_ *runtime.Instance, args []runtime.Value, names []string) (runtime.Value, error) {
		ordered, oerr := vm.orderRuntimeArguments(Instruction{}, info, args, names, offset)
		if oerr != nil {
			return nil, oerr
		}
		if isolated {
			return vm.callCallableIsolated(callable, ordered)
		}
		return vm.callCallableSlow(callable, ordered)
	}
}

// bridgedParameters reconstructs ast.Parameter entries (with a type name)
// from runtime parameter metadata so introspection that reads a Function's
// declared parameter types keeps working across the bridge.
func bridgedParameters(params []runtime.ParameterMetadata) []ast.Parameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]ast.Parameter, len(params))
	for i, p := range params {
		out[i] = ast.Parameter{Name: &ast.Identifier{Value: p.Name}, Variadic: p.Variadic}
		if p.Type != "" {
			out[i].Type = &ast.TypeRef{Name: p.Type}
		}
	}
	return out
}

// isStatefulNativeModule shares the compiler's canonical stateful-module list so the VM's bridge decision cannot drift from it (the drift that left amqp/kafka compilable but "unsupported native call" at runtime).
func isStatefulNativeModule(module string) bool {
	return isStatefulBytecodeBuiltinModule(module)
}

func isStatefulNativeCall(module, function string) bool {
	if native.IsPureBuiltin(module, function) {
		return false
	}
	if isStatefulNativeModule(module) {
		return true
	}
	switch module {
	case "json", "xml", "yaml":
		return function == "reader" || function == "stream"
	default:
		return false
	}
}

// reorderMethodNamedArgs reorders deferred-method-call arguments
// against the method's actual ParamNames so the runtime dispatch sees
// values in positional order. The receiver counts as paramOffset=1.
func (vm *VM) reorderMethodNamedArgs(instruction Instruction, instance *runtime.Instance, methodName string, args []runtime.Value, names []string) ([]runtime.Value, error) {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return args, nil
	}
	indices, ok := vm.lookupMethod(classInfo, methodName)
	if !ok || len(indices) == 0 {
		return args, nil
	}
	idx, err := vm.selectRuntimeFunction(instruction, methodName, indices, args, 1)
	if err != nil {
		return nil, err
	}
	function := vm.curMod.Chunk.Functions[idx]
	return vm.orderRuntimeArguments(instruction, function, args, names, 1)
}

// reorderCallableNamedArgs reorders deferred-callable-call arguments
// using the callable's signature. BytecodeFunction / BytecodeClosure
// expose a FunctionIndex pointing at ParamNames; other callable kinds
// (native functions, Instance.__invoke) can't be reordered statically,
// so the names list is rejected up front by the compiler in those
// cases - here we fall back to the positional order if the callable
// shape is unrecognised.
func (vm *VM) reorderCallableNamedArgs(instruction Instruction, callee runtime.Value, args []runtime.Value, names []string) ([]runtime.Value, error) {
	var funcIdx int64 = -1
	switch v := callee.(type) {
	case runtime.BytecodeFunction:
		// A cross-module callee's Index indexes its home chunk, not ours.
		if v.Module != vm.moduleName && vm.moduleLoader != nil {
			return vm.orderCrossModuleNamedArgs(v.Module, v.Index, 0, args, names)
		}
		funcIdx = v.Index
	case runtime.BytecodeClosure:
		if v.Module != vm.moduleName && vm.moduleLoader != nil {
			return vm.orderCrossModuleNamedArgs(v.Module, v.FunctionIndex, len(v.Upvalues), args, names)
		}
		funcIdx = v.FunctionIndex
	default:
		return args, nil
	}
	if funcIdx < 0 || int(funcIdx) >= len(vm.curMod.Chunk.Functions) {
		return args, nil
	}
	function := vm.curMod.Chunk.Functions[funcIdx]
	offset := 0
	if closure, ok := callee.(runtime.BytecodeClosure); ok {
		offset = len(closure.Upvalues)
	}
	return vm.orderRuntimeArguments(instruction, function, args, names, offset)
}

// readArgNames decodes the trailing name-index operands carried by
// OpDefer*CallNamed. Each entry is a constant-pool index pointing to
// a String holding the argument name, or -1 for a positional arg.
func (vm *VM) readArgNames(instruction Instruction, nameOperands []int64) ([]string, error) {
	names := make([]string, len(nameOperands))
	for i, idx := range nameOperands {
		if idx < 0 {
			continue
		}
		name, err := vm.constantStringAt(instruction, idx, "argument name constant must be string")
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	return names, nil
}

// callBytecodeInline invokes a bytecode function on the current VM without
// rebuilding the chunk. Used as the fast path for callCallable when the
// target is a BytecodeFunction or no-upvalue BytecodeClosure in the same
// module. Returns the function's return value and pops it off the stack.
func (vm *VM) callBytecodeInline(funcIndex int64, args []runtime.Value) (runtime.Value, error) {
	if funcIndex < 0 || int(funcIndex) >= len(vm.curMod.Chunk.Functions) {
		return nil, fmt.Errorf("function index out of range")
	}
	function := &vm.curMod.Chunk.Functions[funcIndex]
	if function.IsGenerator || function.Async {
		return nil, fmt.Errorf("inline call does not support generator/async functions")
	}
	baseline := len(vm.frames)
	stackArgs := make([]runtime.VMValue, len(args))
	for i, a := range args {
		stackArgs[i] = runtime.VMValueFromValue(a)
	}
	oldSuppress := vm.runSuppressCleanup
	// The enclosing primitive HOF's call-site line stamps this callback frame so the caller frame's trace line is the HOF call site; reset it for the body so an inner inline call does not inherit it.
	hofLine := vm.hofCallLine
	// Isolate the handler/defer baselines so an uncaught callback throw unwinds out to the native HOF (which aborts), not into an enclosing try; enterDirectRun also drops the callback's leftover frames. traceFrameBaseline stays unset: an inline callback's caller chain belongs in its trace.
	restore := vm.enterDirectRun()
	vm.hofCallLine = 0
	defer func() {
		restore()
		vm.runSuppressCleanup = oldSuppress
		vm.hofCallLine = hofLine
	}()
	nextIP, err := vm.startFunctionVMValue(Instruction{Line: int32(hofLine)}, 0, function, stackArgs, nil)
	if err != nil {
		return nil, err
	}
	// startFunctionVMValue returns Entry-1 to compensate for the dispatch loop's ip++; we enter fresh, so step to the actual entry.
	vm.runEntryIP = nextIP + 1
	vm.runInlineExitDepth = baseline
	vm.runSuppressCleanup = true
	if err := vm.Run(); err != nil {
		return nil, err
	}
	result, err := vm.popVM()
	if err != nil {
		return nil, err
	}
	return result.ToValue(), nil
}

// callCallableSlow mirrors callCallable but disables the
// callBytecodeInline fast path. Used by stateful native bridges where
// the callback can fire on a different goroutine than the VM dispatch
// loop and inline state mutations would race.
func (vm *VM) callCallableSlow(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	// Force the non-inline path without mutating the shared vm.inDispatchLoop, which a callback on another goroutine would race.
	return vm.callCallableModeHostedSync(fn, args, false, nil, false)
}

// callCallableSlowHosted runs a bridged callback threading the invoking worker as the synchronous re-entry host so a module call inside the callback reuses that module's in-flight worker.
func (vm *VM) callCallableSlowHosted(fn runtime.Value, args []runtime.Value, reentryHost *VM) (runtime.Value, error) {
	return vm.callCallableModeHostedSync(fn, args, false, reentryHost, false)
}

// callCallableIsolated runs a per-request server-handler callback on a fresh
// callVM that snapshots host globals but does NOT write them back, so handler
// state is per-request and concurrent request goroutines do not race the shared
// host globals. It never touches shared host-vm fields (no inDispatchLoop
// dance), since it is invoked from external net/http goroutines, not
// re-entrantly. Native handlers run directly; cross-module callables fall back.
// CallIsolatedHandler runs a server-handler callback with per-request isolation; it is the exported entry the loader uses to re-isolate a cross-module handler on its home module's VM.
func (vm *VM) CallIsolatedHandler(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	return vm.callCallableIsolated(fn, args)
}

func (vm *VM) callCallableIsolated(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	switch f := fn.(type) {
	case runtime.Function:
		if f.Native == nil {
			return nil, fmt.Errorf("runtime function is not callable by VM")
		}
		return f.Native(nil, args)
	case runtime.BytecodeFunction:
		if f.Module != vm.moduleName || f.Raw {
			if !f.Raw && vm.moduleLoader != nil {
				return vm.moduleLoader.CallModuleHandlerIsolated(fn, f.Module, args, vm)
			}
			return vm.callCallableSlow(fn, args)
		}
		if err := vm.ensureCallableDecorators(); err != nil {
			return nil, err
		}
		wrapper := make([]Instruction, 0, len(args)+2)
		for i := range args {
			wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
		}
		wrapper = append(wrapper, Instruction{Op: OpCall, Operands: []int64{f.Index, int64(len(args))}})
		wrapper = append(wrapper, Instruction{Op: OpReturn})
		return vm.runWrapperWithRawCall(args, wrapper, -1, true, nil, nil)
	case runtime.BytecodeClosure:
		if f.Module != vm.moduleName {
			if vm.moduleLoader != nil {
				return vm.moduleLoader.CallModuleHandlerIsolated(fn, f.Module, args, vm)
			}
			return vm.callCallableSlow(fn, args)
		}
		// Per-request isolation: clone the closure's upvalues AND the globals through one cloneState so a value aliased by both a global and an upvalue stays one shared object (parity with the evaluator's CloneFunction).
		vm.globalsMu.Lock()
		snapshot := append([]runtime.VMValue(nil), vm.globals...)
		vm.globalsMu.Unlock()
		clonedGlobals, clonedF := runtime.CloneIsolated(snapshot, f)
		f = clonedF.(runtime.BytecodeClosure)
		constants := make([]runtime.Value, 0, len(args)+2)
		constants = append(constants, f)
		constants = append(constants, args...)
		methodNameIndex := int64(len(constants))
		constants = append(constants, runtime.String{Value: "__invoke"})
		wrapper := make([]Instruction, 0, len(args)+3)
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{0}})
		for i := range args {
			wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i + 1)}})
		}
		wrapper = append(wrapper, Instruction{Op: OpMethodCall, Operands: []int64{methodNameIndex, int64(len(args))}})
		wrapper = append(wrapper, Instruction{Op: OpReturn})
		return vm.runWrapperWithRawCall(constants, wrapper, -1, true, nil, clonedGlobals)
	default:
		return vm.callCallableSlow(fn, args)
	}
}

func (vm *VM) callCallable(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	return vm.callCallableMode(fn, args, vm.inDispatchLoop)
}

// callCallableMode dispatches a callable; allowInline (the owning goroutine's dispatch-loop state, passed explicitly so cross-goroutine callbacks never read/write the shared flag) gates the inline fast path.
// overloadedValue builds a runtime.OverloadedFunction from function indices in this chunk; each overload routes through the loader (an exclusive worker per call, never reading the parent VM's dispatch state) so the value is safe to invoke from another goroutine.
func (vm *VM) overloadedValue(indices []int64) runtime.OverloadedFunction {
	loader := vm.moduleLoader
	if loader == nil {
		// No-loader overloads capture vm; keep the host VM out of the pool while the value can still call into it.
		vm.noteEscape()
	}
	moduleName := vm.moduleName
	overloads := make([]runtime.Function, 0, len(indices))
	name := ""
	for _, idx := range indices {
		fi := &vm.curMod.Chunk.Functions[idx]
		if name == "" {
			name = fi.Name
		}
		bf := runtime.BytecodeFunction{Module: moduleName, Index: idx}
		var native func(*runtime.Instance, []runtime.Value) (runtime.Value, error)
		var bridge func(any, []runtime.Value) (runtime.Value, error)
		if loader != nil {
			// caller=nil: an escaped or cross-goroutine invocation has no proven synchronous re-entry host (capturing vm could form a self-referential host chain).
			native = func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				return loader.CallModuleFunction(bf, args, nil)
			}
			// A synchronous invocation threads the proven active worker so the call sees the module's live (in-progress) globals, matching the evaluator.
			bridge = func(host any, args []runtime.Value) (runtime.Value, error) {
				return loader.CallModuleFunction(bf, args, host.(*VM))
			}
		} else {
			native = func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				return vm.callCallable(bf, args)
			}
		}
		overloads = append(overloads, runtime.Function{
			Name:           fi.Name,
			Parameters:     overloadParams(fi),
			TypeParameters: fi.TypeParameters,
			Async:          fi.Async,
			Native:         native,
			BridgeInvoke:   bridge,
		})
	}
	// Module + Indices let an async winner reconstruct its home-chunk BytecodeFunction so the async worker runs it on its own snapshot instead of the vm-capturing wrapper closure.
	return runtime.OverloadedFunction{Name: name, Overloads: overloads, Module: moduleName, Indices: append([]int64(nil), indices...)}
}

// overloadParams reconstructs ast parameters from a FunctionInfo so the shared selector sees the same names, types, defaults, and variadic flag as the evaluator's overloads.
func overloadParams(fi *FunctionInfo) []ast.Parameter {
	params := make([]ast.Parameter, len(fi.ParamNames))
	for i := range fi.ParamNames {
		var typ *ast.TypeRef
		if i < len(fi.ParamTypes) && fi.ParamTypes[i] != "" {
			typ = &ast.TypeRef{Name: fi.ParamTypes[i]}
		}
		var def ast.Expression
		if i < len(fi.DefaultConstants) && fi.DefaultConstants[i] >= 0 {
			def = &ast.Identifier{Value: "_"}
		}
		params[i] = ast.Parameter{
			Name:     &ast.Identifier{Value: fi.ParamNames[i]},
			Type:     typ,
			Default:  def,
			Variadic: fi.Variadic && i == len(fi.ParamNames)-1,
		}
	}
	return params
}

func (vm *VM) callCallableMode(fn runtime.Value, args []runtime.Value, allowInline bool) (runtime.Value, error) {
	return vm.callCallableModeHosted(fn, args, allowInline, nil)
}

func (vm *VM) callCallableModeHosted(fn runtime.Value, args []runtime.Value, allowInline bool, reentryHost *VM) (runtime.Value, error) {
	return vm.callCallableModeHostedSync(fn, args, allowInline, reentryHost, vm.syncMode)
}

// A bridged callback chooses its own mode without reading its running parent's state.
func (vm *VM) callCallableModeHostedSync(fn runtime.Value, args []runtime.Value, allowInline bool, reentryHost *VM, syncMode bool) (runtime.Value, error) {
	if len(vm.pendingTypeBindings) > 0 {
		fn = bindCallableBindings(fn, vm.pendingTypeBindings)
		vm.pendingTypeBindings = nil
	}
	switch f := fn.(type) {
	case runtime.Function:
		if f.BridgeInvoke != nil {
			if host := vm.activeReentryHost(); host != nil {
				// A bridged foreign callback's throw carries only the host chain; splice this worker's HOF caller frame back on.
				res, err := f.BridgeInvoke(host, args)
				return res, vm.stitchHofCallbackError(err)
			}
		}
		if f.Native == nil {
			return nil, fmt.Errorf("runtime function is not callable by VM")
		}
		return f.Native(nil, args)
	case runtime.OverloadedFunction:
		if len(f.TypeArguments) > 0 && len(f.Indices) > 0 && len(f.Callables) == 0 {
			if f.Module == vm.moduleName {
				return vm.CallOverloadedFast(f.Indices, f.Name, args, vm, f.TypeArguments)
			}
			old := vm.typeArgHandoff
			vm.typeArgHandoff = f.TypeArguments
			defer func() { vm.typeArgHandoff = old }()
			return vm.moduleLoader.CallModuleOverloaded(f.Module, f.Indices, f.Name, args, vm)
		}
		var pos int
		var chosen runtime.Function
		var err error
		if len(f.Callables) == 1 {
			chosen = f.Overloads[0]
		} else {
			pos, chosen, err = overload.SelectWithIndex(f.Name, f.Overloads, args)
		}
		if err != nil {
			return nil, err
		}
		if pos >= 0 && pos < len(f.Callables) {
			if chosen.Async && !syncMode {
				return vm.startAsyncCallable(f, args), nil
			}
			ordered, err := bindBoundArguments(f, pos, args, nil)
			if err != nil {
				return nil, err
			}
			if info, ok := vm.functionInfoIn(f.Module, f.Indices[pos]); ok {
				offset := int(info.UpvalueCount)
				if len(info.ParamNames)-offset > len(chosen.Parameters) {
					offset++
				}
				if message := vm.argumentMismatchMessage(info, ordered, offset); message != "" {
					return nil, vm.typedFault(Instruction{}, "TypeError", message)
				}
			}
			return vm.callCallableModeHostedSync(vm.bindCallableTypeArgs(f.Callables[pos], f.TypeArguments), ordered, allowInline, reentryHost, syncMode)
		}
		// An async winner yields a Task; route it through the home-chunk BytecodeFunction so the worker runs on its own snapshot (the wrapper closure captures a shared vm and would race), matching the evaluator's post-selection async check.
		if chosen.Async && !syncMode && pos >= 0 && pos < len(f.Indices) {
			bf := runtime.BytecodeFunction{Module: f.Module, Index: f.Indices[pos], Async: true}
			return vm.startAsyncCallable(bf, args), nil
		}
		return vm.callCallableModeHostedSync(chosen, args, allowInline, reentryHost, syncMode)
	case runtime.BytecodeFunction:
		if f.Raw && len(vm.frames) > 0 {
			f.TypeBindings = mergedTypeBindings(f.TypeBindings, vm.frames[len(vm.frames)-1].typeBindings)
		}
		// An async function invoked as a value/callback yields a Task (the worker runs the body synchronously via syncMode), matching the evaluator and the direct-call path. A raw value is a decorated function's original body, already inside the decorated call's Task.
		if f.Async && !f.Raw && !syncMode {
			return vm.startAsyncCallable(f, args), nil
		}
		if f.Module != vm.moduleName {
			// Cross-chunk function reference - the Index resolves
			// against the defining chunk's function table, not ours.
			// Includes entry-script values (Module=="") that crossed
			// into a stdlib sub-VM.
			if vm.moduleLoader == nil {
				return nil, fmt.Errorf("bytecode module loader is not configured")
			}
			res, err := vm.moduleLoader.CallModuleFunction(f, vm.wrapStatefulNativeArgs("", "", args), vm)
			return res, vm.stitchHofCallbackError(err)
		}
		if f.Raw {
			if vm.methodReceiverFuncs[f.Index] {
				receiver := vm.forwardThis
				if receiver == nil && reentryHost != nil {
					// A bridged call from a decorator in another module carries the receiver on the calling VM.
					receiver = reentryHost.forwardThis
				}
				if receiver == nil {
					return nil, fmt.Errorf("method receiver is not available")
				}
				args = append([]runtime.Value{receiver}, args...)
			}
			return vm.CallFunctionBound(f.Index, args, f.TypeBindings, true)
		}
		if len(f.TypeBindings) > 0 {
			return vm.CallFunctionBound(f.Index, args, f.TypeBindings, false)
		}
		if allowInline && !vm.requiresCallSitePolymorphism {
			return vm.callBytecodeInline(f.Index, args)
		}
		return vm.CallFunction(f.Index, args)
	case runtime.BytecodeClosure:
		if !syncMode && f.Module == vm.moduleName && int(f.FunctionIndex) >= 0 && int(f.FunctionIndex) < len(vm.curMod.Chunk.Functions) && vm.curMod.Chunk.Functions[f.FunctionIndex].Async {
			return vm.startAsyncCallable(f, args), nil
		}
		if f.Module != vm.moduleName {
			// The closure was created in a different chunk - its
			// FunctionIndex resolves against that chunk's function
			// table, not ours. Route through the module loader so
			// the call dispatches in the right VM context. Includes
			// closures from the entry script (Module=="") that
			// crossed into a stdlib module-defined class method.
			if vm.moduleLoader == nil {
				return nil, fmt.Errorf("bytecode module loader is not configured")
			}
			res, err := vm.moduleLoader.CallModuleClosure(f, vm.wrapStatefulNativeArgs("", "", args), vm)
			return res, vm.stitchHofCallbackError(err)
		}
		if allowInline && len(f.Upvalues) == 0 && f.TypeBindings == nil && !vm.requiresCallSitePolymorphism {
			return vm.callBytecodeInline(f.FunctionIndex, args)
		}
		constants := make([]runtime.Value, 0, len(args)+2)
		constants = append(constants, f)
		constants = append(constants, args...)
		methodNameIndex := int64(len(constants))
		constants = append(constants, runtime.String{Value: "__invoke"})
		wrapper := make([]Instruction, 0, len(args)+3)
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{0}})
		for i := range args {
			wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i + 1)}})
		}
		wrapper = append(wrapper, Instruction{Op: OpMethodCall, Operands: []int64{methodNameIndex, int64(len(args))}})
		wrapper = append(wrapper, Instruction{Op: OpReturn})
		res, err := vm.runWrapperWithRawCall(constants, wrapper, -1, false, reentryHost, nil)
		return res, vm.stitchHofCallbackError(err)
	case *runtime.Instance:
		constants := make([]runtime.Value, 0, len(args)+2)
		constants = append(constants, f)
		constants = append(constants, args...)
		methodNameIndex := int64(len(constants))
		constants = append(constants, runtime.String{Value: "__invoke"})
		wrapper := make([]Instruction, 0, len(args)+3)
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{0}})
		for i := range args {
			wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i + 1)}})
		}
		wrapper = append(wrapper, Instruction{Op: OpMethodCall, Operands: []int64{methodNameIndex, int64(len(args))}})
		wrapper = append(wrapper, Instruction{Op: OpReturn})
		res, err := vm.runWrapperWithRawCall(constants, wrapper, -1, false, reentryHost, nil)
		return res, vm.stitchHofCallbackError(err)
	default:
		return nil, fmt.Errorf("value is not callable")
	}
}

func (vm *VM) CallFunction(index int64, args []runtime.Value) (runtime.Value, error) {
	if index < 0 || int(index) >= len(vm.chunk.Functions) {
		return nil, fmt.Errorf("function index out of range")
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return nil, err
	}
	wrapper := make([]Instruction, 0, len(args)+2)
	for i := range args {
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
	}
	wrapper = append(wrapper, Instruction{Op: OpCall, Operands: []int64{index, int64(len(args))}})
	wrapper = append(wrapper, Instruction{Op: OpReturn})
	return vm.runWrapper(args, wrapper)
}

// CallRawFunctionValue runs a function value's undecorated body, prepending the forwarded receiver when it is a decorated method's original body.
func (vm *VM) CallRawFunctionValue(f runtime.BytecodeFunction, args []runtime.Value) (runtime.Value, error) {
	if vm.methodReceiverFuncs[f.Index] {
		if vm.forwardThis == nil {
			return nil, fmt.Errorf("method receiver is not available")
		}
		args = append([]runtime.Value{vm.forwardThis}, args...)
	}
	return vm.CallFunctionBound(f.Index, args, f.TypeBindings, true)
}

// InheritForwardThis carries the caller's decorated-method receiver onto a worker about to run code for it.
func (vm *VM) InheritForwardThis(caller *VM) {
	if caller != nil && vm.forwardThis == nil {
		vm.forwardThis = caller.forwardThis
	}
}

// CallFunctionFast enters a cross-module free function directly (no synthetic wrapper bytecode / derived call VM) when it is sync, undecorated, non-generator and non-raw; otherwise it falls back to the wrapper path with identical results.
func (vm *VM) CallFunctionFast(index int64, args []runtime.Value) (runtime.Value, error) {
	if literals := vm.pendingLiteralArgs; literals != nil {
		vm.pendingLiteralArgs = nil
		if index >= 0 && int(index) < len(vm.curMod.Chunk.Functions) {
			vm.typeLiteralArgs(vm.curMod.Chunk.Functions[index], args, 0, literals, nil)
		}
	}
	if vm.directEntryEligible(index) {
		return vm.executeFunctionDirect(index, args)
	}
	return vm.CallFunction(index, args)
}

func (vm *VM) directEntryEligible(index int64) bool {
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return false
	}
	function := &vm.curMod.Chunk.Functions[index]
	if function.IsGenerator || function.Async {
		return false
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return false
	}
	_, decorated := vm.decoratedFuncs[index]
	return !decorated
}

// executeFunctionDirect pushes the callee's frame directly on this idle, exclusively-owned worker VM and runs to completion, mirroring callBytecodeInline as the outermost run (cleanup fires once, matching the wrapper-callVM path). startFunction applies arg validation / defaults / variadic.
// enterDirectRun snapshots the inline-run baselines (entry IP, frame exit depth, exception-handler count) for a nested direct execution and returns a restore closure; the handler baseline keeps a nested re-entry on a shared worker from unwinding into the enclosing execution's exception handlers.
// suppressWorkerCleanup stops a transient worker run from firing destructors; the worker hands its destructibles up to its caller at the loader boundary instead.
func (vm *VM) suppressWorkerCleanup() func() {
	old := vm.runSuppressCleanup
	vm.runSuppressCleanup = true
	return func() { vm.runSuppressCleanup = old }
}

func (vm *VM) enterDirectRun() func() {
	oldEntryIP := vm.runEntryIP
	oldExitDepth := vm.runInlineExitDepth
	oldTraceBaseline := vm.traceFrameBaseline
	oldHandlerBaseline := vm.runHandlerBaseline
	oldDeferBaseline := vm.runDeferBaseline
	entryHandlers := len(vm.exceptionHandlers)
	entryDefers := len(vm.defers)
	entryFrames := len(vm.frames)
	vm.runHandlerBaseline = entryHandlers
	vm.runDeferBaseline = entryDefers
	return func() {
		vm.runEntryIP = oldEntryIP
		vm.runInlineExitDepth = oldExitDepth
		vm.traceFrameBaseline = oldTraceBaseline
		vm.runHandlerBaseline = oldHandlerBaseline
		vm.runDeferBaseline = oldDeferBaseline
		vm.dropInlineRunFrames(entryFrames)
		if len(vm.exceptionHandlers) > entryHandlers {
			vm.exceptionHandlers = vm.exceptionHandlers[:entryHandlers]
		}
		if len(vm.defers) > entryDefers {
			vm.defers = vm.defers[:entryDefers]
		}
	}
}

func (vm *VM) executeFunctionDirect(index int64, args []runtime.Value) (runtime.Value, error) {
	function := &vm.curMod.Chunk.Functions[index]
	baseline := len(vm.frames)
	defer vm.enterDirectRun()()
	defer vm.suppressWorkerCleanup()()
	nextIP, err := vm.startFunction(Instruction{}, 0, function, args, nil)
	if err != nil {
		return nil, err
	}
	vm.runEntryIP = nextIP + 1
	vm.runInlineExitDepth = baseline
	vm.traceFrameBaseline = baseline
	if err := vm.Run(); err != nil {
		return nil, err
	}
	result, perr := vm.popVM()
	if perr != nil {
		return nil, perr
	}
	return result.ToValue(), nil
}

// CallClosureFast enters a cross-module closure directly (no wrapper) when its function is sync, undecorated and non-generator; otherwise falls back to the wrapper/inline path with identical results.
func (vm *VM) CallClosureFast(closure runtime.BytecodeClosure, args []runtime.Value) (runtime.Value, error) {
	if vm.closureDirectEligible(closure) {
		return vm.executeClosureDirect(closure, args)
	}
	return vm.callCallable(closure, args)
}

func (vm *VM) closureDirectEligible(closure runtime.BytecodeClosure) bool {
	index := closure.FunctionIndex
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
		return false
	}
	function := &vm.curMod.Chunk.Functions[index]
	if function.IsGenerator || function.Async {
		return false
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return false
	}
	_, decorated := vm.decoratedFuncs[index]
	return !decorated
}

// executeClosureDirect pushes the closure's frame directly on this idle, exclusively-owned worker VM (startClosureFunction binds upvalues + type bindings) and runs to completion, mirroring executeFunctionDirect.
func (vm *VM) executeClosureDirect(closure runtime.BytecodeClosure, args []runtime.Value) (runtime.Value, error) {
	baseline := len(vm.frames)
	defer vm.enterDirectRun()()
	defer vm.suppressWorkerCleanup()()
	nextIP, err := vm.startClosureFunction(Instruction{}, 0, closure, args)
	if err != nil {
		return nil, err
	}
	vm.runEntryIP = nextIP + 1
	vm.runInlineExitDepth = baseline
	vm.traceFrameBaseline = baseline
	if err := vm.Run(); err != nil {
		return nil, err
	}
	result, perr := vm.popVM()
	if perr != nil {
		return nil, perr
	}
	return result.ToValue(), nil
}

func (vm *VM) CallFunctionRaw(index int64, args []runtime.Value) (runtime.Value, error) {
	if index < 0 || int(index) >= len(vm.chunk.Functions) {
		return nil, fmt.Errorf("function index out of range")
	}
	wrapper := make([]Instruction, 0, len(args)+2)
	for i := range args {
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
	}
	wrapper = append(wrapper, Instruction{Op: OpCall, Operands: []int64{index, int64(len(args))}})
	wrapper = append(wrapper, Instruction{Op: OpReturn})
	return vm.runWrapperWithRawCall(args, wrapper, index, false, nil, nil)
}

func (vm *VM) CallClosure(closure runtime.BytecodeClosure, args []runtime.Value) (runtime.Value, error) {
	return vm.callCallable(closure, args)
}

// invokeCallable is the installed native callable invoker. It runs the callable
// on a fresh worker VM (its own stack and snapshot of host state), not inline on
// this VM: a native call has left the dispatch loop, so an inline callback that
// throws unwinds through and underflows the host VM's stack. The worker isolates
// that, and concurrent async callbacks never share frame state. The error keeps
// its thrown class/message for the caller's catch (see the DataFrame dispatch).
func (vm *VM) invokeCallable(callable runtime.Value, args []runtime.Value) (runtime.Value, error) {
	return vm.spawnAsyncWorker().callCallable(callable, args)
}

func (vm *VM) ReflectConstructorsForChunkClass(class runtime.BytecodeClass) (runtime.Value, error) {
	return vm.reflectConstructors(class)
}

func (vm *VM) ReflectFieldsForChunkClass(class runtime.BytecodeClass) (runtime.Value, error) {
	return vm.reflectFieldsResult(class, runtime.ClassMetadata{}), nil
}

func (vm *VM) collectChunkClasses(chunk Chunk) []runtime.Value {
	out := make([]runtime.Value, 0, len(chunk.Classes))
	for i, classInfo := range chunk.Classes {
		out = append(out, vm.bytecodeClassFromInfo(classInfo, int64(i)))
	}
	return out
}

func dedupeClassValues(values []runtime.Value) []runtime.Value {
	seen := map[string]bool{}
	out := make([]runtime.Value, 0, len(values))
	for _, v := range values {
		if bc, ok := v.(runtime.BytecodeClass); ok {
			key := bc.Module + "." + bc.Name
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, v)
	}
	return out
}

// DeserializeIntoChunkClass is the public entry the moduleLoader uses
// to deserialize a class declared in this VM's chunk. It bypasses the
// cross-module dispatch in deserializeIntoClass (we are already on the
// right VM) and goes straight to the local logic.
func (vm *VM) DeserializeIntoChunkClass(class runtime.BytecodeClass, value runtime.Value) (runtime.Value, error) {
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
		v, err := vm.ConstructClass(class.Index, nil)
		if err != nil {
			return nil, err
		}
		instance, ok := v.(*runtime.Instance)
		if !ok {
			return v, nil
		}
		for i, fieldName := range classInfo.FieldNames {
			key := runtime.String{Value: fieldName}
			entry, hit := dict.GetEntry(native.DictKey(key))
			if hit {
				value := entry.Value
				if i < len(classInfo.FieldCheckTypes) && classInfo.FieldCheckTypes[i] != "" {
					value = native.ConvertParsedNumbers(value, classInfo.FieldCheckTypes[i])
				}
				if msg, ok := vm.fieldWriteMismatch(instance, fieldName, value); !ok {
					return nil, runtime.ClassifiedError{Class: "TypeError", Message: msg}
				}
				instance.Fields[fieldName] = value
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
	for i, paramName := range ctor.ParamNames {
		// Skip the implicit "this" receiver the compiler prepends to constructor parameter lists.
		if paramName == "this" {
			continue
		}
		key := runtime.String{Value: paramName}
		entry, hit := dict.GetEntry(native.DictKey(key))
		if !hit {
			return nil, fmt.Errorf("deserialize %s: missing field %q", class.Name, paramName)
		}
		value := entry.Value
		if i < len(ctor.ParamTypes) {
			value = native.ConvertParsedNumbers(value, vm.expandTypeAliases(ctor.ParamTypes[i]))
		}
		args = append(args, value)
	}
	return vm.ConstructClass(class.Index, args)
}

// stageTypeArgsAsBindings zips positional explicit `<TypeArgs>` against
// the class's declared type parameters and stages the result in
// vm.pendingTypeBindings for the construction path to consume.
func (vm *VM) stageTypeArgsAsBindings(classIndex int64, typeArgs []string) {
	if len(typeArgs) == 0 || classIndex < 0 || int(classIndex) >= len(vm.chunk.Classes) {
		return
	}
	params := vm.chunk.Classes[classIndex].TypeParameters
	if len(params) == 0 {
		return
	}
	bindings := map[string]string{}
	for i, name := range params {
		if i >= len(typeArgs) {
			break
		}
		if typeArgs[i] != "" {
			bindings[name] = typeArgs[i]
		}
	}
	if len(bindings) > 0 {
		vm.pendingTypeBindings = bindings
	}
}

// ConstructClassWithTypeArgs embeds the bindings in the wrapper itself:
// runWrapper executes on a pooled callVM, so staging this VM's
// pendingTypeBindings would never reach the executing VM.
func (vm *VM) ConstructClassWithTypeArgs(index int64, args []runtime.Value, typeArgs []string) (runtime.Value, error) {
	if index < 0 || int(index) >= len(vm.chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	params := vm.chunk.Classes[index].TypeParameters
	if len(typeArgs) == 0 || len(params) == 0 {
		return vm.ConstructClass(index, args)
	}
	extended := make([]runtime.Value, 0, len(args)+len(params)*2)
	extended = append(extended, args...)
	plant := []int64{0}
	count := int64(0)
	for i, name := range params {
		if i >= len(typeArgs) {
			break
		}
		if typeArgs[i] == "" {
			continue
		}
		plant = append(plant, int64(len(extended)))
		extended = append(extended, runtime.String{Value: name})
		plant = append(plant, int64(len(extended)))
		extended = append(extended, runtime.String{Value: typeArgs[i]})
		count++
	}
	if count == 0 {
		return vm.ConstructClass(index, args)
	}
	plant[0] = count
	wrapper := make([]Instruction, 0, len(args)+3)
	for i := range args {
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
	}
	wrapper = append(wrapper, Instruction{Op: OpPlantCallTypeBindings, Operands: plant})
	wrapper = append(wrapper, Instruction{Op: OpConstructClass, Operands: []int64{index, int64(len(args))}})
	wrapper = append(wrapper, Instruction{Op: OpReturn})
	return vm.runWrapper(extended, wrapper)
}

// ConstructClassFast constructs a cross-module class by directly running OpConstructClass + the constructor frame on the exclusive worker VM (no synthetic wrapper bytecode / derived call VM), for a non-decorated class; decorated classes fall back to the wrapper path.
func (vm *VM) ConstructClassFast(index int64, args []runtime.Value, typeArgs []string) (runtime.Value, error) {
	if index >= 0 && int(index) < len(vm.curMod.Chunk.Classes) {
		if err := vm.ensureCallableDecorators(); err == nil {
			if _, decorated := vm.decoratedClasses[index]; !decorated {
				return vm.executeConstructDirect(index, args, typeArgs)
			}
		}
	}
	return vm.ConstructClassWithTypeArgs(index, args, typeArgs)
}

// ConstructClassFastNamed selects the constructor overload and orders named/positional args against the home chunk, then constructs through the shared positional path.
func (vm *VM) ConstructClassFastNamed(index int64, args []runtime.Value, names []string, typeArgs []string) (runtime.Value, error) {
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	classInfo := vm.curMod.Chunk.Classes[index]
	if len(classInfo.ConstructorIndices) == 0 {
		if len(args) > 0 {
			return nil, fmt.Errorf("%s constructor expects no arguments", classInfo.Name)
		}
		return vm.ConstructClassFast(index, args, typeArgs)
	}
	_, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, classInfo.Name, classInfo.ConstructorIndices, args, names, 1)
	if err != nil {
		return nil, err
	}
	return vm.ConstructClassFast(index, ordered, typeArgs)
}

// AdoptLiteralArgs moves the int-literal argument mask a caller handed off for a cross-module call onto this home VM, so the next fast call types those arguments as a same-module call would.
func (vm *VM) AdoptLiteralArgs(caller *VM) {
	if caller == nil || caller == vm {
		return
	}
	vm.pendingLiteralArgs = caller.literalHandoff
	caller.literalHandoff = nil
}

// CallOverloadedFast selects a positional overload against the home chunk and calls it; displayName carries the call-site-qualified name for selection errors. An async winner yields a Task unless the caller is already inside an async worker.
func (vm *VM) CallOverloadedFast(indices []int64, displayName string, args []runtime.Value, caller *VM, providedTypeArgs ...[]string) (runtime.Value, error) {
	typeArgs := []string(nil)
	if len(providedTypeArgs) > 0 {
		typeArgs = providedTypeArgs[0]
	}
	literals := vm.pendingLiteralArgs
	vm.pendingLiteralArgs = nil
	index, err := vm.selectRuntimeFunctionLiterals(Instruction{}, displayName, indices, args, 0, literals, nil, typeArgs)
	if err != nil {
		return nil, err
	}
	fn := vm.curMod.Chunk.Functions[index]
	bound := vm.bindCallTypeArgs(runtime.BytecodeFunction{Module: vm.moduleName, Index: index, Name: fn.Name, TypeParameters: fn.TypeParameters, Async: fn.Async}, typeArgs)
	if vm.overloadWinnerAsync(index, caller) {
		return vm.startAsyncCallable(bound, args), nil
	}
	return vm.CallFunctionBound(index, args, bound.TypeBindings, false)
}

// CallOverloadedFastNamed orders named/positional args and selects a matching overload against the home chunk, then calls it.
func (vm *VM) CallOverloadedFastNamed(indices []int64, displayName string, args []runtime.Value, names []string, caller *VM, providedTypeArgs ...[]string) (runtime.Value, error) {
	typeArgs := []string(nil)
	if len(providedTypeArgs) > 0 {
		typeArgs = providedTypeArgs[0]
	}
	index, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, displayName, indices, args, names, 0, typeArgs)
	if err != nil {
		return nil, err
	}
	fn := vm.curMod.Chunk.Functions[index]
	bound := vm.bindCallTypeArgs(runtime.BytecodeFunction{Module: vm.moduleName, Index: index, Name: fn.Name, TypeParameters: fn.TypeParameters, Async: fn.Async}, typeArgs)
	if vm.overloadWinnerAsync(index, caller) {
		return vm.startAsyncCallable(bound, ordered), nil
	}
	return vm.CallFunctionBound(index, ordered, bound.TypeBindings, false)
}

// overloadWinnerAsync reports whether the selected home-chunk overload runs as a Task: async and the caller is not already inside an async worker.
func (vm *VM) overloadWinnerAsync(index int64, caller *VM) bool {
	if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) || !vm.curMod.Chunk.Functions[index].Async {
		return false
	}
	return caller == nil || !caller.syncMode
}

// executeConstructDirect builds the instance + runs its constructor directly on this idle, exclusively-owned worker VM. constructClassWithArgs pushes a constructor frame (run it) or leaves the instance/result on the stack (no-ctor); detect via the frame depth.
func (vm *VM) executeConstructDirect(index int64, args []runtime.Value, typeArgs []string) (runtime.Value, error) {
	baseline := len(vm.frames)
	defer vm.enterDirectRun()()
	defer vm.suppressWorkerCleanup()()
	vm.stageTypeArgsAsBindings(index, typeArgs)
	nextIP, err := vm.constructClassWithArgs(Instruction{}, 0, index, args, false)
	if err != nil {
		return nil, err
	}
	if len(vm.frames) > baseline {
		vm.runEntryIP = nextIP + 1
		vm.runInlineExitDepth = baseline
		vm.traceFrameBaseline = baseline
		if err := vm.Run(); err != nil {
			return nil, err
		}
	}
	result, perr := vm.popVM()
	if perr != nil {
		return nil, perr
	}
	return result.ToValue(), nil
}

func (vm *VM) ConstructClass(index int64, args []runtime.Value) (runtime.Value, error) {
	if index < 0 || int(index) >= len(vm.chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	wrapper := make([]Instruction, 0, len(args)+2)
	for i := range args {
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
	}
	wrapper = append(wrapper, Instruction{Op: OpConstructClass, Operands: []int64{index, int64(len(args))}})
	wrapper = append(wrapper, Instruction{Op: OpReturn})
	return vm.runWrapper(args, wrapper)
}

// CallMethodAs invokes `name` on `instance` using the supplied
// className to locate the method definition. Used by cross-module
// parent dispatch where the subclass instance is declared in
// another chunk: `instance.Class.Name` would point at the subclass,
// which doesn't exist in the parent module's chunk.
func (vm *VM) CallMethodAs(className string, instance *runtime.Instance, name string, args []runtime.Value) (runtime.Value, error) {
	classInfo, ok := vm.classInfo(className)
	if !ok {
		return nil, fmt.Errorf("unknown class %s", className)
	}
	indices, ok := vm.lookupMethod(classInfo, name)
	if !ok {
		if strings.EqualFold(name, className) {
			indices = append([]int64(nil), classInfo.ConstructorIndices...)
			if len(indices) == 0 {
				if len(args) > 0 {
					return nil, fmt.Errorf("%s constructor expects no arguments", className)
				}
				return runtime.Null{}, nil
			}
		} else {
			if module, parentClass, mok := vm.crossModuleBoundary(classInfo); mok && vm.moduleLoader != nil {
				if _, lerr := vm.moduleLoader.LoadModule(module, module); lerr == nil {
					return vm.moduleLoader.CallParentInModule(module, parentClass, name, instance, args, vm)
				}
			}
			return nil, &runtime.MethodNotFoundError{Class: className, Method: name}
		}
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 1)
	if err != nil {
		return nil, err
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return nil, err
	}
	if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
		if vm.chunk.Functions[functionIndex].Async && !vm.syncMode {
			return vm.startAsyncCallableWithForwardThis(decorated, args, instance), nil
		}
		return vm.callCallableWithForwardThis(decorated, args, instance)
	}
	callArgs := append([]runtime.Value{instance}, args...)
	if task, ok := vm.startAsyncMethod(functionIndex, callArgs); ok {
		return task, nil
	}
	return vm.CallFunction(functionIndex, callArgs)
}

func (vm *VM) CallMethod(instance *runtime.Instance, name string, args []runtime.Value) (runtime.Value, error) {
	// Only a foreign-class instance dispatches via the trampoline wrapper; a local instance resolves directly (the wrapper routes cross-module to the loader, so using it on the instance's own module VM would recurse).
	if instance.Class.Module != vm.moduleName {
		if nativeMethods := instance.Class.Methods[strings.ToLower(name)]; len(nativeMethods) > 0 && nativeMethods[0].Native != nil {
			return vm.callNativeMethod(nativeMethods[0], instance, args)
		}
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return nil, fmt.Errorf("unknown class %s", instance.Class.Name)
	}
	indices, ok := vm.lookupMethod(classInfo, name)
	if !ok {
		if module, parentClass, mok := vm.crossModuleBoundary(classInfo); mok && vm.moduleLoader != nil {
			if _, lerr := vm.moduleLoader.LoadModule(module, module); lerr == nil {
				return vm.moduleLoader.CallParentInModule(module, parentClass, name, instance, args, vm)
			}
		}
		if result, handled, derr := vm.callInterfaceDefaultValue(instance, strings.ToLower(name), args); handled {
			return result, derr
		}
		return nil, &runtime.MethodNotFoundError{Class: instance.Class.Name, Method: name}
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 1)
	if err != nil {
		return nil, err
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return nil, err
	}
	if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
		if vm.chunk.Functions[functionIndex].Async && !vm.syncMode {
			return vm.startAsyncCallableWithForwardThis(decorated, args, instance), nil
		}
		return vm.callCallableWithForwardThis(decorated, args, instance)
	}
	callArgs := append([]runtime.Value{instance}, args...)
	if task, ok := vm.startAsyncMethod(functionIndex, callArgs); ok {
		return task, nil
	}
	return vm.CallFunction(functionIndex, callArgs)
}

// CallMethodFast enters a cross-module instance method directly (no method-wrapper / derived call VM) when it resolves to a sync, undecorated, non-generator method; otherwise it falls back to CallMethod (wrapper, decorators, cross-module-parent / interface-default resolution).
func (vm *VM) CallMethodFast(instance *runtime.Instance, name string, args []runtime.Value) (runtime.Value, error) {
	if idx, ok := vm.methodDirectIndex(instance, name, args); ok {
		callArgs := append([]runtime.Value{instance}, args...)
		return vm.executeFunctionDirect(idx, callArgs)
	}
	return vm.CallMethod(instance, name, args)
}

// CallMethodTyped selects in the declaring chunk before handing bindings to the body.
func (vm *VM) CallMethodTyped(instance *runtime.Instance, name string, args []runtime.Value, names []string, types []string) (runtime.Value, error) {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return nil, fmt.Errorf("unknown class %s", instance.Class.Name)
	}
	indices, ok := vm.lookupMethod(classInfo, name)
	if !ok {
		return nil, &runtime.MethodNotFoundError{Class: instance.Class.Name, Method: name}
	}
	var index int64
	var err error
	if len(names) > 0 {
		index, args, err = vm.selectRuntimeNamedFunction(Instruction{}, name, indices, args, names, 1, types)
	} else {
		index, err = vm.selectRuntimeFunctionWith(Instruction{}, name, indices, args, 1, nil, types)
	}
	if err != nil {
		return nil, err
	}
	return vm.CallBoundMethodValue(instance, index, args, nil, types)
}

// CallMethodFastNamed binds named arguments against the method's home signature (engaging declared defaults, including for an omitted middle parameter) then dispatches positionally through CallMethodFast; the caller runs it on the method's home-module VM.
func (vm *VM) CallMethodFastNamed(instance *runtime.Instance, name string, args []runtime.Value, names []string) (runtime.Value, error) {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if ok {
		if indices, found := vm.lookupMethod(classInfo, name); found {
			if _, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, name, indices, args, names, 1); err == nil {
				return vm.CallMethodFast(instance, name, ordered)
			} else {
				return nil, err
			}
		}
	}
	return vm.CallMethodFast(instance, name, args)
}

// callBoundDecoratedValue applies a bound method/static value's decorator wrapper with the receiver forwarded (nil for a static), so the value runs the decorator like a direct call; an uncaught throw's frames are restamped to match. handled is false when funcIndex is not decorated.
func (vm *VM) callBoundDecoratedValue(instance *runtime.Instance, funcIndex int64, args []runtime.Value) (runtime.Value, error, bool) {
	if err := vm.ensureCallableDecorators(); err != nil {
		return nil, err, true
	}
	decorated, ok := vm.decoratedFuncs[funcIndex]
	if !ok {
		return nil, nil, false
	}
	name := vm.curMod.Chunk.Functions[funcIndex].Name
	if vm.curMod.Chunk.Functions[funcIndex].Async && !vm.syncMode {
		return vm.startAsyncCallableWithForwardThis(decorated, args, instance), nil, true
	}
	res, err := vm.callCallableWithForwardThis(decorated, args, instance)
	if err != nil {
		if fault, ok := boundaryFaultToError(err); ok {
			restampDecoratedFrames(&fault, name)
			return nil, vm.uncaughtThrowError(Instruction{Line: int32(fault.ErrorLine)}, fault), true
		}
	}
	return res, err, true
}

// CallBoundMethodValue dispatches a bound method value pinned to funcIndex (first overload, no re-selection) with the receiver, honoring a decorator wrapper. Run on the home-module worker so a bound-method value stays goroutine-safe.
func (vm *VM) CallBoundMethodValue(instance *runtime.Instance, funcIndex int64, args []runtime.Value, names []string, explicit ...[]string) (runtime.Value, error) {
	if len(explicit) > 0 && len(explicit[0]) > 0 {
		if funcIndex < 0 || int(funcIndex) >= len(vm.curMod.Chunk.Functions) {
			return nil, fmt.Errorf("method function index out of range")
		}
		fn := vm.curMod.Chunk.Functions[funcIndex]
		bound := vm.bindCallTypeArgs(runtime.BytecodeFunction{TypeParameters: fn.TypeParameters}, explicit[0])
		vm.pendingTypeBindings = bound.TypeBindings
	}
	if len(names) > 0 {
		if funcIndex < 0 || int(funcIndex) >= len(vm.curMod.Chunk.Functions) {
			return nil, fmt.Errorf("method function index out of range")
		}
		offset := 0
		if instance != nil {
			offset = 1
		}
		ordered, err := vm.orderRuntimeArguments(Instruction{}, vm.curMod.Chunk.Functions[funcIndex], args, names, offset)
		if err != nil {
			return nil, err
		}
		args = ordered
	}
	if res, err, handled := vm.callBoundDecoratedValue(instance, funcIndex, args); handled {
		return res, err
	}
	if instance != nil {
		callArgs := append([]runtime.Value{instance}, args...)
		if task, ok := vm.startAsyncMethod(funcIndex, callArgs); ok {
			return task, nil
		}
		bindings := vm.pendingTypeBindings
		vm.pendingTypeBindings = nil
		return vm.CallFunctionBound(funcIndex, callArgs, bindings, false)
	}
	bindings := vm.pendingTypeBindings
	vm.pendingTypeBindings = nil
	return vm.CallFunctionBound(funcIndex, args, bindings, false)
}

// methodDirectIndex resolves a directly-enterable method to its function index, returning ok=false for anything CallMethod must handle (unknown class/method, overload error, decorated, generator, async).
func (vm *VM) methodDirectIndex(instance *runtime.Instance, name string, args []runtime.Value) (int64, bool) {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return 0, false
	}
	indices, ok := vm.lookupMethod(classInfo, name)
	if !ok {
		return 0, false
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 1)
	if err != nil {
		return 0, false
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return 0, false
	}
	function := &vm.curMod.Chunk.Functions[functionIndex]
	if function.IsGenerator || function.Async {
		return 0, false
	}
	if _, decorated := vm.decoratedFuncs[functionIndex]; decorated {
		return 0, false
	}
	return functionIndex, true
}

func (vm *VM) CallStaticMethod(classIndex int64, name string, args []runtime.Value) (runtime.Value, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	indices, ok := vm.lookupStaticMethod(vm.chunk.Classes[classIndex], name)
	if !ok {
		return nil, fmt.Errorf("unknown static method %s.%s", vm.chunk.Classes[classIndex].Name, name)
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 0)
	if err != nil {
		return nil, err
	}
	return vm.callStaticFunction(functionIndex, args)
}

func (vm *VM) CallStaticMethodTyped(classIndex int64, name string, args []runtime.Value, names []string, types []string) (runtime.Value, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	indices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], name)
	if !ok {
		return nil, fmt.Errorf("unknown static method %s", name)
	}
	var index int64
	var err error
	if len(names) > 0 {
		index, args, err = vm.selectRuntimeNamedFunction(Instruction{}, name, indices, args, names, 0, types)
	} else {
		index, err = vm.selectRuntimeFunctionWith(Instruction{}, name, indices, args, 0, nil, types)
	}
	if err != nil {
		return nil, err
	}
	return vm.CallBoundMethodValue(nil, index, args, nil, types)
}

func (vm *VM) callStaticFunction(functionIndex int64, args []runtime.Value) (runtime.Value, error) {
	if err := vm.ensureCallableDecorators(); err != nil {
		return nil, err
	}
	if decorated, ok := vm.decoratedFuncs[functionIndex]; ok {
		if vm.chunk.Functions[functionIndex].Async && !vm.syncMode {
			return vm.startAsyncCallable(decorated, args), nil
		}
		return vm.callCallable(decorated, args)
	}
	if task, ok := vm.startAsyncMethod(functionIndex, args); ok {
		return task, nil
	}
	return vm.CallFunction(functionIndex, args)
}

// CallStaticMethodNamed binds named arguments against a static method's own signature (engaging declared defaults), then calls it; a static inherited from another module's class is called in that module.
func (vm *VM) CallStaticMethodNamed(classIndex int64, name string, args []runtime.Value, names []string) (runtime.Value, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	classInfo := vm.curMod.Chunk.Classes[classIndex]
	indices, ok := vm.lookupStaticMethod(classInfo, name)
	if !ok {
		if module, parentClass, boundary := vm.crossModuleBoundary(classInfo); boundary && vm.moduleLoader != nil {
			if parent, found := vm.moduleLoader.ClassValueInModule(module, parentClass); found {
				if class, isClass := parent.(runtime.BytecodeClass); isClass {
					return vm.moduleLoader.CallModuleStaticMethodNamed(class, name, args, names, vm)
				}
			}
		}
		if fallback, ok := vm.lookupStaticMethod(classInfo, "__callStatic"); ok {
			callArgs := []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}}
			functionIndex, err := vm.selectRuntimeFunction(Instruction{}, "__callStatic", fallback, callArgs, 0)
			if err != nil {
				return nil, err
			}
			return vm.callStaticFunction(functionIndex, callArgs)
		}
		return nil, fmt.Errorf("unknown static method %s.%s", classInfo.Name, name)
	}
	functionIndex, ordered, err := vm.selectRuntimeNamedFunction(Instruction{}, name, indices, args, names, 0)
	if err != nil {
		return nil, err
	}
	return vm.callStaticFunction(functionIndex, ordered)
}

// CallStaticMethodFast enters a cross-module static method directly on the exclusive worker VM (no wrapper) when it resolves to a sync, undecorated, non-generator method; otherwise it falls back to CallStaticMethod.
func (vm *VM) CallStaticMethodFast(classIndex int64, name string, args []runtime.Value) (runtime.Value, error) {
	if idx, ok := vm.staticDirectIndex(classIndex, name, args); ok {
		return vm.executeFunctionDirect(idx, args)
	}
	return vm.CallStaticMethod(classIndex, name, args)
}

func (vm *VM) staticDirectIndex(classIndex int64, name string, args []runtime.Value) (int64, bool) {
	if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
		return 0, false
	}
	indices, ok := vm.lookupStaticMethod(vm.curMod.Chunk.Classes[classIndex], name)
	if !ok {
		return 0, false
	}
	functionIndex, err := vm.selectRuntimeFunction(Instruction{}, name, indices, args, 0)
	if err != nil {
		return 0, false
	}
	if err := vm.ensureCallableDecorators(); err != nil {
		return 0, false
	}
	function := &vm.curMod.Chunk.Functions[functionIndex]
	if function.IsGenerator || function.Async {
		return 0, false
	}
	if _, decorated := vm.decoratedFuncs[functionIndex]; decorated {
		return 0, false
	}
	return functionIndex, true
}

func (vm *VM) runWrapper(args []runtime.Value, wrapper []Instruction) (runtime.Value, error) {
	return vm.runWrapperWithRawCall(args, wrapper, -1, false, nil, nil)
}

func (vm *VM) runWrapperWithRawCall(args []runtime.Value, wrapper []Instruction, rawIndex int64, isolated bool, reentryHost *VM, preCloned []runtime.VMValue) (runtime.Value, error) {
	chunk := vm.chunk
	meta := vm.chunk.sharedMeta
	var callVM *VM
	var wrapperPool *sync.Pool
	if meta != nil {
		// Shared layout: the callVM reuses the parent's constants,
		// functions, classes, and meta untouched. Call arguments live in
		// the constantsExtra tail (indices >= len(Constants)) and the
		// wrapper runs appended after the parent code, so nothing is
		// shifted or copied per call.
		base := vm.chunk.Instructions
		translated := make([]Instruction, len(wrapper))
		for i, instruction := range wrapper {
			copied := instruction
			copied.Operands = append([]int64(nil), instruction.Operands...)
			shiftInstructionOperands(&copied, 0, len(vm.chunk.Constants))
			translated[i] = copied
		}
		pool := meta.poolFor(wrapperShape{baseLen: len(base), wrapperLen: len(translated)})
		wrapperPool = pool
		if pooled, _ := pool.Get().(*VM); pooled != nil {
			if sameInstructionBase(pooled.wrapperBase, base) {
				copy(pooled.chunk.Instructions[len(base):], translated)
			} else {
				pooled.chunk.Instructions = appendWrapper(base, translated)
				pooled.curMod.Chunk.Instructions = pooled.chunk.Instructions
				pooled.wrapperBase = base
			}
			pooled.stdout = vm.stdout
			pooled.moduleLoader = vm.moduleLoader
			callVM = pooled
		} else {
			chunk.Instructions = appendWrapper(base, translated)
			callVM = NewVMWithModuleLoader(chunk, vm.stdout, vm.moduleLoader)
			callVM.wrapperBase = base
		}
		callVM.constantsExtra = args
		// Wrapper-call static writes stay call-local (matching the
		// per-call constants copy this path used to take).
		callVM.staticsLocalOnly = true
		callVM.runEntryIP = len(base)
	} else {
		shift := len(wrapper)
		chunk.Constants = append(append(make([]runtime.Value, 0, len(args)+len(vm.chunk.Constants)), args...), vm.chunk.Constants...)
		chunk.Instructions = append([]Instruction(nil), wrapper...)
		chunk.Functions = append([]FunctionInfo(nil), vm.chunk.Functions...)
		for i := range chunk.Functions {
			chunk.Functions[i].Entry += int64(shift)
			chunk.Functions[i].DefaultConstants = append([]int64(nil), chunk.Functions[i].DefaultConstants...)
			for j, defaultIndex := range chunk.Functions[i].DefaultConstants {
				if defaultIndex >= 0 {
					chunk.Functions[i].DefaultConstants[j] = defaultIndex + int64(len(args))
				}
			}
		}
		chunk.Classes = copyClasses(vm.chunk.Classes)
		for i := range chunk.Classes {
			for j, defaultIndex := range chunk.Classes[i].FieldDefaults {
				if defaultIndex >= 0 {
					chunk.Classes[i].FieldDefaults[j] = defaultIndex + int64(len(args))
				}
			}
			for name, constantIndex := range chunk.Classes[i].StaticValues {
				if constantIndex >= 0 {
					chunk.Classes[i].StaticValues[name] = constantIndex + int64(len(args))
				}
			}
		}
		for _, instruction := range vm.chunk.Instructions {
			copied := instruction
			copied.Operands = append([]int64(nil), instruction.Operands...)
			shiftInstructionOperands(&copied, shift, len(args))
			chunk.Instructions = append(chunk.Instructions, copied)
		}
	}
	if callVM == nil {
		callVM = NewVMWithModuleLoader(chunk, vm.stdout, vm.moduleLoader)
	}
	// Chain a synchronous same-goroutine module re-entry from inside this callback to the live worker that invoked it (nil when not re-entering).
	callVM.reentryHost = reentryHost
	// Profiler link: a VM that has handed out no closures is reachable only from this goroutine, so it stays blocked here while callVM runs.
	if vm.escapedRefs.Load() == 0 {
		callVM.profileParent = vm
	} else {
		callVM.profileParent = nil
	}
	// The callVM is a wrap-bridge worker: its setGlobalVM writes feed the
	// write-back loop below, so it must take the locked + dirty-tracking
	// path even if no further wrap fires inside it.
	callVM.bridgeActive.Store(true)
	// Mark this parent VM as bridging so its subsequent setGlobalVM
	// writes serialise with the snapshot-in / write-back done by the
	// callVM on whichever goroutine eventually invokes the wrapped
	// callable. The store happens on the parent's goroutine before any
	// possibility of cross-goroutine read.
	vm.bridgeActive.Store(true)
	// Snapshot parent globals under the mutex so a wrap-bridge callback
	// invoked from a worker goroutine doesn't race with the parent's
	// setGlobal writes.
	if isolated {
		// Per-request isolation: deep-clone the globals so reference values are copied, not shared. preCloned (when the caller already cloned globals together with the callable through one cloneState, preserving upvalue/global alias identity) is used as-is; otherwise snapshot under the lock and clone outside it.
		if preCloned != nil {
			callVM.restoreGlobalsVM(preCloned)
		} else {
			vm.globalsMu.Lock()
			snapshot := append([]runtime.VMValue(nil), vm.globals...)
			vm.globalsMu.Unlock()
			callVM.restoreGlobalsVM(runtime.CloneVMValues(snapshot))
		}
	} else {
		vm.globalsMu.Lock()
		callVM.restoreGlobalsVM(vm.globals)
		vm.globalsMu.Unlock()
	}
	callVM.SetModuleName(vm.moduleName)
	if vm.statefulNative != nil {
		callVM.SetStatefulNativeCaller(vm.statefulNative)
	}
	callVM.syncMode = true
	callVM.forwardThis = vm.forwardThis
	callVM.decoratedFuncs = copyRuntimeValueMap(vm.decoratedFuncs)
	callVM.decoratedClasses = copyRuntimeValueMap(vm.decoratedClasses)
	callVM.decoratorsApplied = true
	// Carry cross-module interface defaults so a function body run on this
	// wrapper VM still dispatches them (built binaries hit this path).
	callVM.RestoreInterfaceFallbackState(vm.InterfaceFallbackState())
	callVM.rawFunctionCalls = copyBoolMap(vm.rawFunctionCalls)
	callVM.methodReceiverFuncs = vm.methodReceiverFuncs
	callVM.generatorExecution = vm.generatorExecution
	callVM.generatorYield = vm.generatorYield
	callVM.generatorDone = vm.generatorDone
	if rawIndex >= 0 {
		callVM.rawFunctionCalls[rawIndex] = true
	}
	// A transient wrap-bridge run never owns a destructor lifetime; it hands its destructibles up to the caller. The per-request isolated root keeps sweeping its own at request end.
	callVM.runSuppressCleanup = !isolated
	runErr := callVM.Run()
	if !isolated {
		vm.AdoptDestructibles(callVM.TakeDestructibles())
	}
	if runErr != nil {
		// A run error leaves throw/handler state behind; collect this VM instead of pooling it.
		return nil, runErr
	}
	// Propagate any global-state mutations the callVM produced back to the
	// caller. Without this, deferred function calls and any other paths that
	// invoke CallFunction from inside a running VM silently discard global
	// writes the callee performed (defer-fired closures over module-level
	// state were the symptom). Write only the slots the callee touched -
	// a slicecopy of the entire backing array would race the parent's
	// lock-free OpGetGlobal on unrelated slots when the bridge fires on a
	// goroutine. Isolated calls (per-request server handlers) skip write-back:
	// their state stays per-request, which is what keeps concurrent handlers
	// from racing the shared host globals.
	if !isolated {
		vm.globalsMu.Lock()
		for _, i := range callVM.dirtySlots {
			if i >= len(callVM.globals) || i >= len(vm.globals) {
				continue
			}
			vm.globals[i] = callVM.globals[i]
			if i < len(vm.dirtyGlobals) {
				vm.markDirtySlot(i)
			}
		}
		vm.globalsMu.Unlock()
	}
	var result runtime.Value = runtime.Null{}
	var popErr error
	if len(callVM.stack) > 0 {
		result, popErr = callVM.pop()
	}
	if wrapperPool != nil && popErr == nil && callVM.escapedRefs.Load() == 0 {
		callVM.resetForReuse()
		wrapperPool.Put(callVM)
	}
	return result, popErr
}

func copyRuntimeValueMap(values map[int64]runtime.Value) map[int64]runtime.Value {
	copied := make(map[int64]runtime.Value, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyBoolMap(values map[int64]bool) map[int64]bool {
	copied := make(map[int64]bool, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyClasses(classes []ClassInfo) []ClassInfo {
	copied := append([]ClassInfo(nil), classes...)
	for i := range copied {
		copied[i].FieldNames = append([]string(nil), classes[i].FieldNames...)
		copied[i].FieldDefaults = append([]int64(nil), classes[i].FieldDefaults...)
		copied[i].FieldDefaultFuncs = append([]int64(nil), classes[i].FieldDefaultFuncs...)
		copied[i].Methods = copyStringInt64SliceMap(classes[i].Methods)
		copied[i].StaticValues = copyStringInt64Map(classes[i].StaticValues)
		copied[i].StaticMethods = copyStringInt64SliceMap(classes[i].StaticMethods)
		copied[i].Implements = append([]string(nil), classes[i].Implements...)
		copied[i].Decorators = copyDecoratorMetadata(classes[i].Decorators)
		copied[i].MethodDecorators = copyDecoratorMetadataMap(classes[i].MethodDecorators)
		copied[i].StaticDecorators = copyDecoratorMetadataMap(classes[i].StaticDecorators)
	}
	return copied
}

func copyDecoratorMetadata(values []runtime.DecoratorMetadata) []runtime.DecoratorMetadata {
	if values == nil {
		return nil
	}
	copied := make([]runtime.DecoratorMetadata, len(values))
	for i, value := range values {
		copied[i] = value
		copied[i].Args = append([]runtime.Value(nil), value.Args...)
		if value.NamedArgs != nil {
			copied[i].NamedArgs = make(map[string]runtime.Value, len(value.NamedArgs))
			for key, arg := range value.NamedArgs {
				copied[i].NamedArgs[key] = arg
			}
		}
	}
	return copied
}

func copyDecoratorMetadataMap(values map[string][]runtime.DecoratorMetadata) map[string][]runtime.DecoratorMetadata {
	if values == nil {
		return nil
	}
	copied := make(map[string][]runtime.DecoratorMetadata, len(values))
	for key, value := range values {
		copied[key] = copyDecoratorMetadata(value)
	}
	return copied
}

func copyStringInt64Map(values map[string]int64) map[string]int64 {
	if values == nil {
		return nil
	}
	copied := make(map[string]int64, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyStringInt64SliceMap(values map[string][]int64) map[string][]int64 {
	if values == nil {
		return nil
	}
	copied := make(map[string][]int64, len(values))
	for key, value := range values {
		copied[key] = append([]int64(nil), value...)
	}
	return copied
}

func shiftInstructionOperands(instruction *Instruction, instructionShift int, constantShift int) {
	switch instruction.Op {
	case OpConstant, OpRuntimeError, OpMatchError, OpNativeCall, OpNativeCallNamed, OpNativeCallSpread, OpGetField, OpSetField, OpCallParentMethod, OpGetStaticValue, OpSetStaticValue, OpCallStaticMethod, OpCallStaticMethodSpread, OpMethodCall, OpMethodCallSpread, OpMethodCallNamed, OpCallSpread, OpConstructClassSpread, OpMakeError, OpImportModule, OpLoadModuleValue, OpCatch, OpDeferNativeCall, OpDeferNativeCallNamed, OpDeferMethodCall, OpDeferMethodCallNamed, OpDeferCallableCallNamed, OpDeferMethodCallSpread, OpDeferNativeCallSpread, OpDeferFuncCallSpread, OpDeferCallableCallSpread, OpTypeAssert, OpAddStringConst, OpAppendStringConst, OpAppendGlobalStringConst, OpAppendStringConstStmt, OpAppendGlobalStringConstStmt:
		for i := range instruction.Operands {
			if isConstantOperand(instruction.Op, i) && instruction.Operands[i] >= 0 {
				instruction.Operands[i] += int64(constantShift)
			}
		}
	case OpSetTypeBindings, OpPlantCallTypeBindings, OpPlantCallTypeArgs:
		// Operands: count followed by constant indices (paired for the
		// bindings ops, single for the positional type-args op).
		for i := 1; i < len(instruction.Operands); i++ {
			if instruction.Operands[i] >= 0 {
				instruction.Operands[i] += int64(constantShift)
			}
		}
	}
	switch instruction.Op {
	case OpJump, OpJumpIfFalse, OpIterNext, OpPushExceptionHandler,
		OpJumpIfNotLessInt, OpJumpIfNotLessEqualInt, OpJumpIfNotGreaterInt,
		OpJumpIfNotGreaterEqualInt, OpJumpIfNotEqualInt, OpJumpIfEqualInt,
		OpNullCoalesce, OpOptionalChain, OpJumpIfArgPresent:
		if len(instruction.Operands) > 0 && instruction.Operands[0] >= 0 {
			instruction.Operands[0] += int64(instructionShift)
		}
	case OpCatch:
		if len(instruction.Operands) > 0 && instruction.Operands[0] >= 0 {
			instruction.Operands[0] += int64(instructionShift)
		}
	}
}

func isConstantOperand(op Op, index int) bool {
	switch op {
	case OpConstant, OpRuntimeError, OpMatchError, OpNativeCall, OpGetField, OpSetField, OpMethodCall, OpMakeError, OpDeferNativeCall, OpDeferMethodCall, OpTypeAssert, OpAddStringConst, OpLoadModuleValue:
		return index == 0
	case OpAppendStringConst, OpAppendGlobalStringConst, OpAppendStringConstStmt, OpAppendGlobalStringConstStmt:
		return index == 1
	case OpNativeCallNamed:
		return index == 0 || index >= 2
	case OpNativeCallSpread, OpMethodCallSpread:
		return index == 0 || index >= 2
	case OpCallSpread, OpConstructClassSpread:
		return index >= 2
	case OpDeferNativeCallNamed:
		return index == 0 || index >= 2
	case OpDeferMethodCallNamed:
		return index == 0 || index >= 2
	case OpDeferMethodCallSpread, OpDeferNativeCallSpread:
		return index == 0 || index >= 2
	case OpDeferFuncCallSpread:
		return index >= 2
	case OpDeferCallableCallNamed, OpDeferCallableCallSpread:
		return index >= 1
	case OpCallParentMethod:
		return index == 1
	case OpGetStaticValue, OpSetStaticValue:
		return index == 1
	case OpCallStaticMethod:
		return index == 1
	case OpCallStaticMethodSpread:
		return index == 1 || index >= 3
	case OpMethodCallNamed:
		return index == 0 || index >= 2
	case OpImportModule:
		return index == 0 || index == 1
	case OpCatch:
		return index == 1
	default:
		return false
	}
}

// CallTypeArguments is consumed synchronously before a loader starts any async body.
func (vm *VM) CallTypeArguments() []string {
	if vm == nil {
		return nil
	}
	return vm.typeArgHandoff
}

func (vm *VM) bindCallTypeArgs(function runtime.BytecodeFunction, args []string) runtime.BytecodeFunction {
	if len(args) == 0 || len(function.TypeParameters) == 0 {
		return function
	}
	bindings := map[string]string{}
	for name, bound := range function.TypeBindings {
		bindings[name] = bound
	}
	for i, name := range function.TypeParameters {
		if i < len(args) {
			bindings[name] = args[i]
		}
	}
	function.TypeBindings = bindings
	return function
}

func (vm *VM) bindCallableTypeArgs(value runtime.Value, args []string) runtime.Value {
	if len(args) == 0 {
		return value
	}
	switch f := value.(type) {
	case runtime.BytecodeFunction:
		return vm.bindCallTypeArgs(f, args)
	case runtime.BytecodeClosure:
		if info, ok := vm.functionInfoIn(f.Module, f.FunctionIndex); ok {
			bound := vm.bindCallTypeArgs(runtime.BytecodeFunction{TypeParameters: info.TypeParameters, TypeBindings: f.TypeBindings}, args)
			f.TypeBindings = bound.TypeBindings
		}
		return f
	case runtime.OverloadedFunction:
		f.TypeArguments = args
		f.Overloads = append([]runtime.Function(nil), f.Overloads...)
		for i, overload := range f.Overloads {
			bound := vm.bindCallTypeArgs(runtime.BytecodeFunction{TypeParameters: overload.TypeParameters, TypeBindings: overload.TypeBindings}, args)
			f.Overloads[i].TypeBindings = bound.TypeBindings
		}
		return f
	case runtime.Function:
		if f.NativeTyped != nil {
			typed := f.NativeTyped
			f.Native = func(_ *runtime.Instance, values []runtime.Value) (runtime.Value, error) {
				return typed(vm, values, nil, args)
			}
			f.NativeNamed = func(_ *runtime.Instance, values []runtime.Value, names []string) (runtime.Value, error) {
				return typed(vm, values, names, args)
			}
			f.BridgeInvoke = func(host any, values []runtime.Value) (runtime.Value, error) {
				return typed(host, values, nil, args)
			}
		}
		return f
	}
	return value
}

func bindCallableBindings(value runtime.Value, bindings map[string]string) runtime.Value {
	switch f := value.(type) {
	case runtime.BytecodeFunction:
		f.TypeBindings = mergedTypeBindings(bindings, f.TypeBindings)
		return f
	case runtime.BytecodeClosure:
		f.TypeBindings = mergedTypeBindings(bindings, f.TypeBindings)
		return f
	case runtime.OverloadedFunction:
		f.Overloads = append([]runtime.Function(nil), f.Overloads...)
		f.Callables = append([]runtime.Value(nil), f.Callables...)
		for i := range f.Overloads {
			f.Overloads[i].TypeBindings = mergedTypeBindings(bindings, f.Overloads[i].TypeBindings)
		}
		for i := range f.Callables {
			f.Callables[i] = bindCallableBindings(f.Callables[i], bindings)
		}
		return f
	}
	return value
}

// CallFunctionBound preserves invocation-specific bindings through wrapper and async entry.
func (vm *VM) CallFunctionBound(index int64, args []runtime.Value, bindings map[string]string, raw bool) (runtime.Value, error) {
	if len(bindings) == 0 {
		if raw {
			return vm.CallFunctionRaw(index, args)
		}
		return vm.CallFunctionFast(index, args)
	}
	extended := append([]runtime.Value(nil), args...)
	operands := []int64{int64(len(bindings))}
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		operands = append(operands, int64(len(extended)))
		extended = append(extended, runtime.String{Value: name})
		operands = append(operands, int64(len(extended)))
		extended = append(extended, runtime.String{Value: bindings[name]})
	}
	wrapper := make([]Instruction, 0, len(args)+3)
	for i := range args {
		wrapper = append(wrapper, Instruction{Op: OpConstant, Operands: []int64{int64(i)}})
	}
	wrapper = append(wrapper, Instruction{Op: OpPlantCallTypeBindings, Operands: operands}, Instruction{Op: OpCall, Operands: []int64{index, int64(len(args))}}, Instruction{Op: OpReturn})
	rawIndex := int64(-1)
	if raw {
		rawIndex = index
	}
	return vm.runWrapperWithRawCall(extended, wrapper, rawIndex, false, nil, nil)
}
