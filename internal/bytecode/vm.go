package bytecode

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

type VM struct {
	chunk      Chunk
	stdout     io.Writer
	stderr     io.Writer
	stack      []runtime.VMValue
	globals    []runtime.VMValue
	curGlobals []runtime.VMValue // always aliases globals; future: per-module slice on cross-module frames
	// globalsMu guards bulk operations on the globals slice used by the
	// wrap-bridge: setGlobal writes and the snapshot-in / per-slot
	// write-back done when a wrapped callable fires on a different
	// goroutine than the parent. Per-instruction OpGetGlobal stays
	// lock-free; the bridge boundary is the only race surface the
	// detector flags.
	globalsMu sync.Mutex
	// dirtyGlobals tracks which global slots a sub-VM has written so the
	// wrap-bridge write-back can copy only the touched slots back to the
	// parent rather than slicecopying the entire backing array (which
	// races the parent's lock-free OpGetGlobal on unrelated slots).
	dirtyGlobals []bool
	// persistGlobals makes the lock-free setGlobal fast path record dirty slots so a loader worker's module-global writes can be written back (cross-module module-global persistence).
	persistGlobals bool
	// Cached bound-method closures re-registered every reset; taking the bound method per call allocates.
	instanceInvokerFn   native.InstanceInvokerFunc
	classDeserializerFn native.ClassDeserializerFunc
	// bridgeActive is set once a wrap-bridge worker is, or could become,
	// alive against this VM's globals. While false, setGlobalVM /
	// setGlobal can write without taking globalsMu because no concurrent
	// reader/writer of vm.globals exists. Set monotonically: once true,
	// stays true for the VM's lifetime. The single-goroutine hot path
	// (numeric_loop) never bridges, so it stays lock-free.
	bridgeActive atomic.Bool
	// escapedRefs counts vm-capturing closures handed out during a run
	// (method wrappers, stateful-native bridges, lazy generators). A
	// wrapper VM is only recycled when none escaped.
	escapedRefs atomic.Int32
	// constantsExtra holds wrapper-call values addressed as constants at
	// indices >= len(chunk.Constants), so the constant pool stays shared.
	constantsExtra []runtime.Value
	// wrapperBase identifies the parent instruction slice this wrapper
	// VM's instructions were derived from (pool-reuse identity check).
	wrapperBase []Instruction
	// staticLocal holds call-local static writes when staticsLocalOnly
	// is set (wrapper calls); otherwise writes go to the shared overlay.
	staticLocal       map[int64]runtime.Value
	staticsLocalOnly  bool
	localsStack       []runtime.VMValue
	currentFrameBP    int
	frames            []callFrame
	maxCallDepth      int
	defers            [][]deferredAction
	exceptionHandlers []exceptionHandler
	pendingThrow      *runtime.Error
	curMod            *ModuleContext
	moduleLoader      ModuleLoader
	moduleName        string
	// reentry* implement goid-free same-call-chain re-entry detection for the module loader: reentryHost links a borrowed worker to the worker that borrowed it (nil for roots), reentryActive marks a worker borrowed and executing for a module, reentryDepth balances nested reuse before release.
	reentryHost    *VM
	reentryActive  bool
	reentryDepth   int
	modulePaths    []string
	statefulNative StatefulNativeCaller
	classIndex     map[string]int
	natives        *native.Registry
	// nil entries fall through to evalNativeCall (statefuls / errors.is).
	nativeCache          []native.Function
	syncMode             bool // when true, async functions are called synchronously
	decoratedFuncs       map[int64]runtime.Value
	decoratedClasses     map[int64]runtime.Value
	decoratorsApplied    bool
	applyingDecorators   bool
	rawFunctionCalls     map[int64]bool
	methodReceiverFuncs  map[int64]bool
	interfaceFallbacks   map[int64]map[string]crossModuleDefault
	interfaceExtraFields map[int64][]extraField
	forwardThis          *runtime.Instance
	generatorExecution   bool
	generatorYield       chan vmGeneratorItem
	generatorDone        <-chan struct{}
	typeSpecCache        map[string]vmTypeSpec
	typeAssertSpecs      map[int64]vmTypeSpec
	callArgsFree         [][]runtime.Value
	// pendingTypeBindings carries an inherited type-binding map from a
	// closure callsite into the next startFunctionWithValidation call.
	// startFunctionWithBindings sets it; startFunctionWithValidation
	// reads it during arg validation and again when planting the
	// frame's type bindings, then leaves it for the caller to clear.
	pendingTypeBindings map[string]string
	// pendingCallTypeArgs carries positional explicit `<TypeArgs>` from
	// OpPlantCallTypeArgs into the immediately following OpMethodCall,
	// which consumes (or drops) them at entry.
	pendingCallTypeArgs []string
	// destructibleInstances tracks instances of classes that declare
	// `func ~ClassName()`. The sweep in Cleanup() invokes their
	// destructors in reverse-creation order. `del x` removes the
	// corresponding entry and fires the destructor immediately.
	destructibleInstances []*runtime.Instance
	// destructMu, when non-nil, guards destructibleInstances so a caller==nil cross-module adoption from another goroutine is race-free; nil (lock-free) on transient worker VMs.
	destructMu *sync.Mutex
	// nameLowerCache memoises strings.ToLower(name) by chunk-constant
	// index. Method names recur every call in a tight loop and the
	// dispatch path needs the lowercased form to look up entries in
	// the ClassInfo.Methods map (which the compiler stores
	// case-insensitively). Hit on a 500k-call benchmark: ~6% wall.
	nameLowerCache []string
	// classInfoNameCache memoises classInfo by the runtime
	// instance.Class.Name. The map lookup itself is small, but the
	// `strings.ToLower(name)` inside vm.classInfo runs once per call
	// in the dispatch loop - cache the resolved ClassInfo so the
	// hot path is a single direct read.
	classInfoNameCache map[string]ClassInfo
	// methodLookupCache is a single-slot cache for the most-recent
	// `lookupMethodLower` result. A tight loop calling one method on
	// one class - the class_dispatch benchmark's
	// `Counter.step` x 50k - hits this on every call after the first
	// and skips the `classInfo.Methods` map access entirely. The
	// cache uses pointer-equality on the indices slice to detect
	// hits without copying.
	methodLookupClass    string
	methodLookupName     string
	methodLookupIndices  []int64
	methodLookupValid    bool
	fieldLookupClass     *runtime.Class
	fieldLookupName      string
	fieldLookupOnClass   bool
	fieldLookupHasGetMag bool
	fieldLookupHasSetMag bool
	fieldLookupValid     bool
	// Re-entry knobs used by callBytecodeInline to host a nested
	// function invocation on the same VM without rebuilding the chunk.
	runEntryIP         int
	runInlineExitDepth int // -1 = top-level (exit on OpReturn with frames==0); >=0 = inline (exit when frames drops to this)
	// traceFrameBaseline is the first frame index a stack-trace snapshot may include; set only by cross-module worker re-entry runs so an outer suspended run's frames on the same shared worker don't leak, unlike native-callback inline runs whose caller frames belong to the same chain.
	traceFrameBaseline int
	// nativeCallLine is the source line of the native call currently dispatching, so a VM re-entry from a native (the test runner) can attribute the top-level frame to that call site.
	nativeCallLine int
	// hofCallLine is the call-site line of the primitive HOF currently dispatching, stamped onto the inline callback frame so the caller frame's trace line is the HOF call site, not line 0.
	hofCallLine int
	// runHandlerBaseline is the exception-handler count owned by enclosing executions; a nested direct run (incl. a shared-worker re-entry) must not unwind into handlers below it - a throw with none above propagates out.
	runHandlerBaseline int
	// runDeferBaseline is the deferred-group count owned by enclosing executions; an uncaught throw propagating out of this run runs its own frames' defers down to here, not the enclosing run's.
	runDeferBaseline   int
	runSuppressCleanup bool
	// Tracks whether Run() is currently executing the dispatch loop on
	// this goroutine. callBytecodeInline relies on this being true so
	// that native callbacks fired from a different goroutine (e.g. a
	// stateful native module's async hook) take the runWrapper path
	// instead of mutating shared state.
	inDispatchLoop bool
	// False when chunk has no decorators/async/generators; lets
	// vm.call skip the three per-call probes for those features.
	requiresCallSitePolymorphism bool
}

type ModuleLoader interface {
	LoadModule(canonical string, alias string) (*runtime.Module, error)
	CallModuleFunction(function runtime.BytecodeFunction, args []runtime.Value, caller *VM) (runtime.Value, error)
	CallModuleClosure(closure runtime.BytecodeClosure, args []runtime.Value, caller *VM) (runtime.Value, error)
	// CallModuleHandlerIsolated runs a cross-module server handler (function or closure) on its home module's VM with per-request isolation, the same isolation a same-module handler gets, so handler state is not shared across requests.
	CallModuleHandlerIsolated(fn runtime.Value, module string, args []runtime.Value, caller *VM) (runtime.Value, error)
	// ConstructModuleClass constructs class in its home chunk. typeArgs
	// carries the call site's positional explicit `<TypeArgs>` (nil when
	// none); the home VM zips them against the class's type parameters.
	ConstructModuleClass(class runtime.BytecodeClass, args []runtime.Value, typeArgs []string, caller *VM) (runtime.Value, error)
	// ConstructModuleClassNamed selects the constructor overload and orders named/positional args in the class's home chunk (names[i] pairs with args[i], "" for positional).
	ConstructModuleClassNamed(class runtime.BytecodeClass, args []runtime.Value, names []string, typeArgs []string, caller *VM) (runtime.Value, error)
	// CallModuleOverloaded selects a positional overload of an exported overload set in its home chunk and calls it; displayName is the call-site-qualified name for selection errors.
	CallModuleOverloaded(module string, indices []int64, displayName string, args []runtime.Value, caller *VM) (runtime.Value, error)
	// CallModuleOverloadedNamed orders named/positional args and selects a matching overload of an exported overload set in its home chunk, then calls it.
	CallModuleOverloadedNamed(module string, indices []int64, displayName string, args []runtime.Value, names []string, caller *VM) (runtime.Value, error)
	CallModuleStaticMethod(class runtime.BytecodeClass, methodName string, args []runtime.Value, caller *VM) (runtime.Value, error)
	CallModuleMethod(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *VM) (runtime.Value, error)
	// CallModuleMethodValue dispatches a bound method/static value pinned to funcIndex (first overload) on its home worker; instance is the receiver, nil for a static. Applies the decorator wrapper with the receiver bound.
	CallModuleMethodValue(module string, funcIndex int64, instance *runtime.Instance, args []runtime.Value, caller *VM) (runtime.Value, error)
	// CallModuleDestructor fires a cross-module instance's `func ~Class()` in its home chunk on a home worker; caller adopts any destructibles the destructor builds (re-drain).
	CallModuleDestructor(module string, className string, instance *runtime.Instance, caller *VM) error
	// ModuleMethodParamNames exposes a cross-module method's declared
	// parameter names so dict spread can order named args at the call site.
	ModuleMethodParamNames(module string, className string, methodName string) ([]string, error)
	// ModuleFunctionParamNames exposes a cross-module free function's declared parameter names so named args can be ordered at the call site.
	ModuleFunctionParamNames(module string, index int64) ([]string, error)
	// ModuleConstructorParamNames exposes the union of a cross-module class's constructor parameter names so dict spread can drop unknown keys before construction.
	ModuleConstructorParamNames(module string, className string) ([]string, error)
	// CallParentInModule invokes the parent class's constructor or
	// method on the supplied instance inside the parent module's chunk.
	// className is looked up in the target chunk regardless of
	// instance.Class.Name, which is necessary because instance is a
	// subclass declared in another chunk.
	CallParentInModule(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *VM) (runtime.Value, error)
	// ImmutableFieldsForModuleClass returns the set-once `@immutable`
	// field names declared on a class in another module, so a subclass
	// can lock them after its cross-module parent constructor runs.
	ImmutableFieldsForModuleClass(module string, className string) []string
	// FindClassByName looks up a class by its bare (unqualified) name
	// across every loaded module. Returns nil when nothing matches.
	// Used by reflect.class so framework code can resolve a user
	// class regardless of which module declared it.
	FindClassByName(name string) (runtime.Value, bool)
	// ClassValueInModule resolves a class by name within one specific module (module-exact, unlike FindClassByName).
	ClassValueInModule(module, className string) (runtime.Value, bool)
	// RuntimeClassFor builds + caches the runtime.Class for a class in another module so a cross-module subclass's parent chain is reachable by pointer.
	RuntimeClassFor(module string, className string) (*runtime.Class, bool)
	// PersistModuleGlobals writes a worker's dirty module-global slots back to its module record (used when a generator worker finishes outside the normal acquire/release cycle).
	PersistModuleGlobals(vm *VM)
	// FindFunctionByName looks up a function by its bare name
	// across every loaded module. Returns nil when nothing matches.
	FindFunctionByName(name string) (runtime.Value, bool)
	// DeserializeModuleClass runs json.parseAs / xml.parseAs style
	// deserialization for a class declared in another chunk. The
	// loader resolves the right chunk by class.Module and dispatches
	// to a VM bound to it. Used when a stdlib module receives a
	// class value originating from the user's main program.
	DeserializeModuleClass(class runtime.BytecodeClass, value runtime.Value) (runtime.Value, error)
	// ConstructorsForModuleClass returns `reflect.constructors(class)`
	// for a class declared in another chunk. The loader dispatches to
	// a VM bound to the right chunk so the metadata reflects the
	// originating class's actual constructor list rather than the
	// caller chunk's stale view.
	ConstructorsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error)
	FieldsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error)
	LookupModuleInterface(module, name string) (InterfaceInfo, bool)
	ListAllClasses() []runtime.Value
	// ModuleClassDescendsFrom reports whether className in module is or
	// descends from a class with simple name targetSimpleName, recursing
	// across further module boundaries (cross-module instanceof).
	ModuleClassDescendsFrom(module, className, targetSimpleName string) bool
	// StaticValueForModuleClass resolves a static const/let declared on
	// className in module or an ancestor, recursing across further module
	// boundaries. found=false when no such static value exists.
	StaticValueForModuleClass(module, className, name string) (runtime.Value, bool)
	// CallModuleStaticMethodByName resolves and calls a static method on
	// className in module or an ancestor, recursing across further module
	// boundaries. found=false when no such static method exists.
	CallModuleStaticMethodByName(module, className, methodName string, args []runtime.Value) (runtime.Value, bool, error)
	// StaticMethodValueForModuleClass builds a callable value for a static method on className in module or an ancestor, for `Class.staticMethod` used as a value.
	StaticMethodValueForModuleClass(module, className, name string) (runtime.Value, bool)
	// MethodFirstOverload resolves the declaring module + function index of the first overload of instance method `name` on className in module or an ancestor.
	MethodFirstOverload(module, className, name string) (string, int64, bool)
	// UnimplementedAbstractMethods returns the @abstract methods declared
	// on className in module or an ancestor with no concrete override in
	// the cross-module chain, keyed by method name to declaring class.
	UnimplementedAbstractMethods(module, className string) map[string]string
}

type StatefulNativeCaller interface {
	CallBuiltin(module, name string, args []runtime.Value, argNames []string) (runtime.Value, error)
}

// nativeObjectMethodCaller routes NativeObject methods to the stateful
// native, which owns the underlying handle state.
type nativeObjectMethodCaller interface {
	NativeObjectMethod(obj runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error)
}

type directPrintStatefulNative interface {
	HandleDirectPrint() bool
}

type FunctionDecoratorState struct {
	decorated           map[int64]runtime.Value
	decoratedClasses    map[int64]runtime.Value
	applied             bool
	methodReceiverFuncs map[int64]bool
}

const DefaultMaxCallDepth = 10000

type callFrame struct {
	returnIP       int
	basePointer    int
	localCount     int
	returnOverride runtime.Value
	typeBindings   map[string]string
	generator      chan vmGeneratorItem
	generatorDone  <-chan struct{}
	functionName   string
	callLine       int // callLine is the frame's entry call site; stable across tail reuse.
	// tailRepeat/tailCallLine track self-tail-call frame reuse for the [xN] trace collapse.
	tailRepeat       int
	tailCallLine     int
	negateReturn     bool
	isErrorClass     bool
	isImmutableClass bool
	// Set-once `@immutable` fields this constructor frame locks on its instance
	// when it returns.
	immutableFieldsToLock []string
	lockInstance          *runtime.Instance
	// isDestructibleConstructor is true when this frame is a class
	// constructor for a class that declares a `~ClassName()`
	// destructor. OpReturn registers the constructed instance in
	// vm.destructibleInstances so the exit-time sweep finds it.
	isDestructibleConstructor bool
	// shared is true when this frame inherits its parent's locals slice
	// (a nested function statement that references the outer function's
	// locals by absolute slot number). When false, the frame owns a
	// fresh locals slice that is recycled on return.
	shared bool
}

type vmGeneratorItem struct {
	value runtime.Value
	err   error
}

type deferredCallKind int8

const (
	deferKindPrint deferredCallKind = iota
	deferKindPrintln
	deferKindNative
	deferKindFunc
	deferKindMethod
	deferKindCallable
)

type deferredAction struct {
	kind     deferredCallKind
	value    runtime.Value   // for print/println
	name     string          // for native: "module.func"; for method: method name
	funcIdx  int64           // for func
	receiver runtime.Value   // for method
	args     []runtime.Value // for native/func/method
	// spreadMask, when set, parallels args: a true entry is a frozen spread list reference whose current elements are expanded in place at fire time.
	spreadMask []bool
	// names parallels args when the deferred call was emitted by an
	// OpDefer*Named opcode. Empty entries mark positional args;
	// non-empty entries name the corresponding argument so the
	// deferred-dispatch path can reorder against the callee's
	// signature when the queue runs. nil when the defer used the
	// positional-only opcode.
	names []string
	// line is the defer statement's source line; trace frames for a throwing deferred call show it as the caller's call site.
	line int
}

type exceptionHandler struct {
	handlerIP        int
	frameDepth       int
	stackDepth       int
	localsStackDepth int
	snapshot         []runtime.VMValue
	snapshotBase     int
}

type iteratorValue struct {
	values    []runtime.Value
	index     int
	rangeIter *rangeIterator
	generator *runtime.Generator
	// userIter holds a user-defined iterator instance returned by
	// the source's __iter() method (or the source itself when it
	// already implements __next/__done). next() calls
	// userIterDone()/userIterNext() per step.
	userIter *runtime.Instance
}

func (v *iteratorValue) TypeName() string { return "iterator" }
func (v *iteratorValue) Inspect() string  { return "<iterator>" }

type rangeIterator struct {
	current   *big.Int
	end       *big.Int
	step      *big.Int
	exclusive bool
}

type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("bytecode exited with %d", e.Code)
}

// ExitCode lets exit-aware callers (e.g. signal handlers) recover the
// code across package boundaries without importing this package.
func (e ExitError) ExitCode() int { return e.Code }

type recoverableNativeError struct {
	err error
}

func (e recoverableNativeError) Error() string {
	return e.err.Error()
}

func (e recoverableNativeError) Unwrap() error {
	return e.err
}

// vmTypedError is a catchable typed exception from a primitive or native method.
type vmTypedError struct {
	class   string
	message string
}

func (e vmTypedError) Error() string { return e.class + ": " + e.message }

// vmThrownError carries a runtime.Error across a VM boundary without collapsing it to a plain string.
type vmThrownError struct {
	err runtime.Error
}

// Rendered canonically so a thrown error escaping via Go error paths prints the same as top-level uncaught.
func (e vmThrownError) Error() string {
	u := runtime.UncaughtError{
		Class:        e.err.Class,
		Message:      e.err.Message,
		ErrorLine:    e.err.ErrorLine,
		Frames:       runtime.CollapseFrames(e.err.TraceFrames),
		TopLevelLine: e.err.TopLevelLine,
	}
	return u.Render()
}

// ErrorClass exposes the carried Geblang error class so a typed throw
// (e.g. TestSkip) survives the native-to-script boundary via runtime.TypedError.
func (e vmThrownError) ErrorClass() string { return e.err.Class }

// vmFatalError marks a fault that try/catch must never intercept (VM
// bytecode corruption, stack overflow). Run() routes ordinary runtime
// faults to the active handler but lets a vmFatalError terminate.
type vmFatalError struct {
	err error
}

func (e vmFatalError) Error() string { return e.err.Error() }
func (e vmFatalError) Unwrap() error { return e.err }

type vmTypeSpec struct {
	raw       string
	base      string
	baseLower string
	nullable  bool
	kind      vmTypeKind
	args      []vmTypeSpec
}

type vmTypeKind int8

const (
	vmTypeOther vmTypeKind = iota
	vmTypeAny
	vmTypeInt
	vmTypeString
	vmTypeBool
	vmTypeFloat
	vmTypeDecimal
	vmTypeList
	vmTypeSet
	vmTypeDict
	vmTypeCallable
	vmTypeGenerator
	// vmTypeUnion holds a `T | U | ...` shape: spec.args carries the
	// branches; a value matches when any branch matches.
	vmTypeUnion
	// vmTypeIntersection holds a `T & U & ...` shape: spec.args carries
	// the branches; a value matches only when every branch matches.
	vmTypeIntersection
)

// invokeInstanceMethod implements native.InstanceInvoker for the
// bytecode VM. Returns (result, true, nil) when the method exists
// and was called, (nil, false, nil) when the class has no such
// method, (nil, false, err) on call error.
// hasInstanceMethod reports whether the instance's class defines method
// (native or bytecode). Used to detect subscript dunders (__index, __setIndex).
func (vm *VM) hasInstanceMethod(instance *runtime.Instance, name string) bool {
	if instance == nil || instance.Class == nil {
		return false
	}
	if m := instance.Class.Methods[strings.ToLower(name)]; len(m) > 0 {
		return true
	}
	if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
		if _, ok := vm.lookupMethod(classInfo, name); ok {
			return true
		}
	}
	if _, ok := vm.lookupInterfaceFallback(instance.Class.Name, strings.ToLower(name)); ok {
		return true
	}
	return false
}

func (vm *VM) invokeInstanceMethod(instance *runtime.Instance, method string, args []runtime.Value) (runtime.Value, bool, error) {
	if instance == nil || instance.Class == nil {
		return nil, false, nil
	}
	// Cross-module instance: its class lives in another chunk, so dispatch
	// through the module loader (mirrors the == operator's cross-module path).
	if instance.Class.Module != vm.moduleName {
		if vm.moduleLoader == nil || len(instance.Class.Methods[strings.ToLower(method)]) == 0 {
			return nil, false, nil
		}
		result, err := vm.moduleLoader.CallModuleMethod(instance.Class.Module, instance.Class.Name, method, instance, args, vm)
		if err != nil {
			return nil, false, err
		}
		return result, true, nil
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return nil, false, nil
	}
	indices, ok := vm.lookupMethod(classInfo, method)
	if !ok || len(indices) == 0 {
		return nil, false, nil
	}
	result, err := vm.CallMethod(instance, method, args)
	if err != nil {
		return nil, false, err
	}
	return result, true, nil
}

// displayString renders a value for println/print via __string when defined, else Inspect.
func (vm *VM) displayString(value runtime.Value) (string, error) {
	if instance, ok := value.(*runtime.Instance); ok {
		if result, handled, err := vm.invokeInstanceMethod(instance, "__string", nil); err != nil {
			return "", err
		} else if handled {
			if err := checkCastDunderReturn("string", result); err != nil {
				return "", err
			}
			return result.(runtime.String).Value, nil
		}
	}
	return value.Inspect(), nil
}

func (vm *VM) Run() (err error) {
	if !vm.runSuppressCleanup {
		// Mirror the evaluator's behaviour: after a script finishes
		// (success, exit, or runtime error) drain the destructor
		// registry so end-of-lifetime cleanup is deterministic.
		defer func() {
			cleanupErr := vm.Cleanup()
			if err == nil && cleanupErr != nil {
				err = cleanupErr
			}
		}()
		if err := vm.ensureCallableDecorators(); err != nil {
			return err
		}
	}
	inlineExitDepth := vm.runInlineExitDepth
	oldInDispatch := vm.inDispatchLoop
	vm.inDispatchLoop = true
	defer func() { vm.inDispatchLoop = oldInDispatch }()
	// Hoist instructions so the dispatch loop reads from a stable pointer.
	instructions := vm.curMod.Chunk.Instructions
	// A runtime fault returned by the dispatch loop is routed to the
	// nearest active try/catch (parity with the evaluator); fatal and
	// unhandled faults terminate. On a catch, the loop re-enters at the
	// handler IP via runEntryIP.
	for {
		loopErr := vm.dispatchLoop(instructions, inlineExitDepth)
		if loopErr == nil {
			return nil
		}
		resumeIP, fatal, routeErr := vm.routeRuntimeFault(loopErr)
		if routeErr != nil {
			return routeErr
		}
		if fatal {
			return loopErr
		}
		vm.runEntryIP = resumeIP
	}
}

// dispatchLoop runs bytecode from runEntryIP to completion or to the
// first returned fault. Run() wraps it so a fault can be routed to a
// try/catch instead of terminating the VM.
func (vm *VM) dispatchLoop(instructions []Instruction, inlineExitDepth int) error {
	for ip := vm.runEntryIP; ip < len(instructions); ip++ {
		instruction := &instructions[ip]
		switch instruction.Op {
		case OpNoop:
		case OpConstant:
			index := instruction.Operands[0]
			if index < 0 || int(index) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "constant index out of range")
			}
			value := vm.constantValue(index)
			switch x := value.(type) {
			case runtime.SmallInt:
				vm.pushVM(runtime.VMValueSmallInt(x.Value))
			case runtime.Bool:
				vm.pushVM(runtime.VMValueBool(x.Value))
			case runtime.Null:
				vm.pushVM(runtime.VMValueNull)
			case runtime.BytecodeClass:
				if dec, exists := vm.decoratedClasses[x.Index]; exists {
					vm.push(dec)
				} else if x.Module == "" && vm.moduleName != "" {
					// Class-as-value constants bake an empty module; stamp the running one so a sub-module (bundle/import) resolves it to this chunk.
					x.Module = vm.moduleName
					vm.push(x)
				} else {
					vm.push(value)
				}
			default:
				vm.push(value)
			}
		case OpAdd:
			nextIP, err := vm.add(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpAppendStringConst, OpAppendGlobalStringConst:
			slot := instruction.Operands[0]
			constIdx := instruction.Operands[1]
			if constIdx < 0 || int(constIdx) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "constant index out of range")
			}
			litVal, ok := vm.constantValue(constIdx).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "literal must be string")
			}
			var target []runtime.VMValue
			var offset int
			if instruction.Op == OpAppendStringConst {
				target = vm.localsStack
				offset = vm.currentFrameBP
			} else {
				target = vm.curGlobals
			}
			idx := int(slot) + offset
			if idx >= len(target) {
				return vm.runtimeError(*instruction, "slot out of range")
			}
			cur := target[idx]
			if cur.Kind == runtime.VMKindBoxed {
				if acc, ok := cur.Boxed.(*runtime.StringAccumulator); ok {
					acc.B.WriteString(litVal.Value)
					materialised := runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: acc.Materialize()}
					target[idx] = materialised
					vm.pushVM(materialised)
					continue
				}
				if l, lok := cur.Boxed.(runtime.String); lok {
					next := runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: runtime.String{Value: l.Value + litVal.Value}}
					target[idx] = next
					vm.pushVM(next)
					continue
				}
			}
			vm.pushVM(cur)
			vm.pushVM(runtime.VMValueFromValue(litVal))
			nextIP, err := vm.add(*instruction, ip)
			if err != nil {
				return err
			}
			result, perr := vm.popVM()
			if perr != nil {
				return vm.runtimeError(*instruction, "%s", perr.Error())
			}
			target[idx] = result
			vm.pushVM(result)
			ip = nextIP
		case OpAppendStringConstStmt, OpAppendGlobalStringConstStmt:
			slot := instruction.Operands[0]
			constIdx := instruction.Operands[1]
			if constIdx < 0 || int(constIdx) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "constant index out of range")
			}
			litVal, ok := vm.constantValue(constIdx).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "literal must be string")
			}
			var target []runtime.VMValue
			var offset int
			if instruction.Op == OpAppendStringConstStmt {
				target = vm.localsStack
				offset = vm.currentFrameBP
			} else {
				target = vm.curGlobals
			}
			idx := int(slot) + offset
			if idx >= len(target) {
				return vm.runtimeError(*instruction, "slot out of range")
			}
			cur := target[idx]
			if cur.Kind == runtime.VMKindBoxed {
				if acc, ok := cur.Boxed.(*runtime.StringAccumulator); ok {
					acc.B.WriteString(litVal.Value)
					continue
				}
				if l, lok := cur.Boxed.(runtime.String); lok {
					if l.Value == "" {
						target[idx] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: runtime.String{Value: litVal.Value}}
						continue
					}
					sb := &strings.Builder{}
					sb.Grow(len(l.Value) + len(litVal.Value) + 256)
					sb.WriteString(l.Value)
					sb.WriteString(litVal.Value)
					target[idx] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: &runtime.StringAccumulator{B: sb}}
					continue
				}
			}
			vm.pushVM(cur)
			vm.pushVM(runtime.VMValueFromValue(litVal))
			nextIP, err := vm.add(*instruction, ip)
			if err != nil {
				return err
			}
			result, perr := vm.popVM()
			if perr != nil {
				return vm.runtimeError(*instruction, "%s", perr.Error())
			}
			target[idx] = result
			ip = nextIP
		case OpAddStringConst:
			n := len(vm.stack)
			if n < 1 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			constIdx := instruction.Operands[0]
			if constIdx < 0 || int(constIdx) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "constant index out of range")
			}
			litVal, ok := vm.constantValue(constIdx).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "OpAddStringConst literal must be string")
			}
			leftVM := vm.stack[n-1]
			if leftVM.Kind == runtime.VMKindBoxed {
				if l, lok := leftVM.Boxed.(runtime.String); lok {
					vm.stack[n-1] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: runtime.String{Value: l.Value + litVal.Value}}
					continue
				}
			}
			// Defer to generic OpAdd if the static-type proof missed.
			vm.pushVM(runtime.VMValueFromValue(litVal))
			nextIP, err := vm.add(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpAddString:
			n := len(vm.stack)
			if n < 2 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			leftVM := vm.stack[n-2]
			rightVM := vm.stack[n-1]
			if leftVM.Kind == runtime.VMKindBoxed && rightVM.Kind == runtime.VMKindBoxed {
				if l, lok := leftVM.Boxed.(runtime.String); lok {
					if r, rok := rightVM.Boxed.(runtime.String); rok {
						vm.stack[n-2] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: runtime.String{Value: l.Value + r.Value}}
						vm.stack = vm.stack[:n-1]
						continue
					}
				}
			}
			// Untyped local can carry a non-string at runtime; defer to vm.add.
			nextIP, err := vm.add(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpSub, OpMul, OpDiv, OpIntDiv, OpMod, OpPow:
			nextIP, err := vm.binaryNumeric(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpAddInt, OpSubInt, OpMulInt, OpModInt:
			n := len(vm.stack)
			if n < 2 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			leftVM := vm.stack[n-2]
			rightVM := vm.stack[n-1]
			if leftVM.Kind == runtime.VMKindSmallInt && rightVM.Kind == runtime.VMKindSmallInt {
				li := leftVM.I64
				ri := rightVM.I64
				var result runtime.VMValue
				switch instruction.Op {
				case OpAddInt:
					v := li + ri
					if (li^v)&(ri^v) < 0 {
						result = runtime.VMValueFromValue(runtime.Int{Value: new(big.Int).Add(big.NewInt(li), big.NewInt(ri))})
					} else {
						result = runtime.VMValueSmallInt(v)
					}
				case OpSubInt:
					v := li - ri
					if (li^ri)&(li^v) < 0 {
						result = runtime.VMValueFromValue(runtime.Int{Value: new(big.Int).Sub(big.NewInt(li), big.NewInt(ri))})
					} else {
						result = runtime.VMValueSmallInt(v)
					}
				case OpMulInt:
					v := li * ri
					if li != 0 && v/li != ri {
						result = runtime.VMValueFromValue(runtime.Int{Value: new(big.Int).Mul(big.NewInt(li), big.NewInt(ri))})
					} else {
						result = runtime.VMValueSmallInt(v)
					}
				case OpModInt:
					if ri == 0 {
						return vm.runtimeError(*instruction, "modulo by zero")
					}
					v := li % ri
					if v != 0 && (li < 0) != (ri < 0) {
						v += ri
					}
					result = runtime.VMValueSmallInt(v)
				}
				vm.stack[n-2] = result
				vm.stack = vm.stack[:n-1]
				continue
			}
			// Fallback: materialise both operands as runtime.Value, drop
			// them from the stack, and route through generic arithmetic.
			left := leftVM.ToValue()
			right := rightVM.ToValue()
			vm.stack = vm.stack[:n-2]
			genericOp := *instruction
			switch instruction.Op {
			case OpAddInt:
				genericOp.Op = OpAdd
			case OpSubInt:
				genericOp.Op = OpSub
			case OpMulInt:
				genericOp.Op = OpMul
			case OpModInt:
				genericOp.Op = OpMod
			}
			if nextIP, handled, err := vm.callBinaryOperatorMethod(genericOp, ip, left, right); handled || err != nil {
				if err != nil {
					return err
				}
				ip = nextIP
			} else if err := vm.binaryNumericValues(genericOp, left, right); err != nil {
				return err
			}
			continue
		case OpLessInt, OpGreaterInt, OpLessEqualInt, OpGreaterEqualInt, OpEqualInt:
			n := len(vm.stack)
			if n < 2 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			leftVM := vm.stack[n-2]
			rightVM := vm.stack[n-1]
			if leftVM.Kind == runtime.VMKindSmallInt && rightVM.Kind == runtime.VMKindSmallInt {
				li := leftVM.I64
				ri := rightVM.I64
				var result bool
				switch instruction.Op {
				case OpLessInt:
					result = li < ri
				case OpGreaterInt:
					result = li > ri
				case OpLessEqualInt:
					result = li <= ri
				case OpGreaterEqualInt:
					result = li >= ri
				case OpEqualInt:
					result = li == ri
				}
				vm.stack[n-2] = runtime.VMValueBool(result)
				vm.stack = vm.stack[:n-1]
				continue
			}
			// Fallback for non-SmallInt int values (runtime.Int after overflow):
			// vm.equal/vm.compare pop from stack themselves and re-push the
			// bool result, so leave both operands on the stack here.
			genericOp := *instruction
			switch instruction.Op {
			case OpLessInt:
				genericOp.Op = OpLess
			case OpGreaterInt:
				genericOp.Op = OpGreater
			case OpLessEqualInt:
				genericOp.Op = OpLessEqual
			case OpGreaterEqualInt:
				genericOp.Op = OpGreaterEqual
			case OpEqualInt:
				genericOp.Op = OpEqual
			}
			if instruction.Op == OpEqualInt {
				nextIP, err := vm.equal(genericOp, ip)
				if err != nil {
					return err
				}
				ip = nextIP
			} else {
				nextIP, err := vm.compare(genericOp, ip)
				if err != nil {
					return err
				}
				ip = nextIP
			}
			continue
		case OpIncLocalInt, OpDecLocalInt:
			slot := instruction.Operands[0]
			idx := vm.currentFrameBP + int(slot)
			if idx < len(vm.localsStack) {
				cur := vm.localsStack[idx]
				if cur.Kind == runtime.VMKindSmallInt {
					delta := int64(1)
					if instruction.Op == OpDecLocalInt {
						delta = -1
					}
					nv := cur.I64 + delta
					if (cur.I64^nv)&(delta^nv) >= 0 {
						vm.localsStack[idx] = runtime.VMValueSmallInt(nv)
						vm.pushVM(cur)
						continue
					}
				}
			}
			if err := vm.updateIntSlot(*instruction); err != nil {
				return err
			}
		case OpIncGlobalInt, OpDecGlobalInt:
			slot := instruction.Operands[0]
			if slot >= 0 && int(slot) < len(vm.curGlobals) && !vm.bridgeActive.Load() {
				cur := vm.curGlobals[slot]
				if cur.Kind == runtime.VMKindSmallInt {
					delta := int64(1)
					if instruction.Op == OpDecGlobalInt {
						delta = -1
					}
					nv := cur.I64 + delta
					if (cur.I64^nv)&(delta^nv) >= 0 {
						vm.curGlobals[slot] = runtime.VMValueSmallInt(nv)
						vm.pushVM(cur)
						continue
					}
				}
			}
			if err := vm.updateIntSlot(*instruction); err != nil {
				return err
			}
		case OpAppendLocalList, OpAppendGlobalList:
			if err := vm.appendListSlot(*instruction); err != nil {
				var typed vmTypedError
				if errors.As(err, &typed) {
					nextIP, throwErr := vm.throwTyped(*instruction, ip, typed.class, typed.message)
					if throwErr != nil {
						return throwErr
					}
					ip = nextIP
					continue
				}
				return err
			}
		case OpJumpIfNotLessInt, OpJumpIfNotLessEqualInt, OpJumpIfNotGreaterInt,
			OpJumpIfNotGreaterEqualInt, OpJumpIfNotEqualInt, OpJumpIfEqualInt:
			n := len(vm.stack)
			if n < 2 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			lvm := vm.stack[n-2]
			rvm := vm.stack[n-1]
			if lvm.Kind == runtime.VMKindSmallInt && rvm.Kind == runtime.VMKindSmallInt {
				li := lvm.I64
				ri := rvm.I64
				vm.stack = vm.stack[:n-2]
				var jump bool
				switch instruction.Op {
				case OpJumpIfNotLessInt:
					jump = !(li < ri)
				case OpJumpIfNotLessEqualInt:
					jump = !(li <= ri)
				case OpJumpIfNotGreaterInt:
					jump = !(li > ri)
				case OpJumpIfNotGreaterEqualInt:
					jump = !(li >= ri)
				case OpJumpIfNotEqualInt:
					jump = !(li == ri)
				case OpJumpIfEqualInt:
					jump = li == ri
				}
				if jump {
					ip = int(instruction.Operands[0]) - 1
				}
				continue
			}
			nextIP, err := vm.compareJumpIntFallback(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpJumpIfModNotZero, OpJumpIfModZero:
			if len(instruction.Operands) != 3 {
				return vm.fatalError(*instruction, "mod-jump instruction has invalid operands")
			}
			slot := int(instruction.Operands[1])
			idx := vm.currentFrameBP + slot
			if idx < 0 || idx >= len(vm.localsStack) {
				return vm.runtimeError(*instruction, "local slot out of range")
			}
			lv := vm.localsStack[idx]
			if lv.Kind != runtime.VMKindSmallInt {
				return vm.runtimeError(*instruction, "mod-jump fast path requires int local")
			}
			ri := instruction.Operands[2]
			if ri == 0 {
				return vm.runtimeError(*instruction, "modulo by zero")
			}
			v := lv.I64 % ri
			if v != 0 && (lv.I64 < 0) != (ri < 0) {
				v += ri
			}
			isZero := v == 0
			jump := isZero
			if instruction.Op == OpJumpIfModNotZero {
				jump = !isZero
			}
			if jump {
				ip = int(instruction.Operands[0]) - 1
			}
			continue
		case OpAddLocalIntLocal, OpSubLocalIntLocal, OpAddLocalIntConst, OpSubLocalIntConst,
			OpAddLocalIntGlobal, OpSubLocalIntGlobal,
			OpAddGlobalIntGlobal, OpSubGlobalIntGlobal, OpAddGlobalIntConst, OpSubGlobalIntConst,
			OpAddGlobalIntLocal, OpSubGlobalIntLocal:
			dst := instruction.Operands[0]
			rhsOp := instruction.Operands[1]
			dstIsGlobal := false
			var lv runtime.VMValue
			var lerr error
			switch instruction.Op {
			case OpAddGlobalIntGlobal, OpSubGlobalIntGlobal, OpAddGlobalIntConst, OpSubGlobalIntConst,
				OpAddGlobalIntLocal, OpSubGlobalIntLocal:
				lv, lerr = vm.getGlobalVM(dst)
				dstIsGlobal = true
			default:
				lv, lerr = vm.getLocalVM(dst)
			}
			if lerr != nil {
				return vm.runtimeError(*instruction, "%s", lerr.Error())
			}
			var rv runtime.VMValue
			var rerr error
			isConst := false
			switch instruction.Op {
			case OpAddLocalIntConst, OpSubLocalIntConst, OpAddGlobalIntConst, OpSubGlobalIntConst:
				isConst = true
			case OpAddLocalIntLocal, OpSubLocalIntLocal, OpAddGlobalIntLocal, OpSubGlobalIntLocal:
				rv, rerr = vm.getLocalVM(rhsOp)
			case OpAddLocalIntGlobal, OpSubLocalIntGlobal, OpAddGlobalIntGlobal, OpSubGlobalIntGlobal:
				rv, rerr = vm.getGlobalVM(rhsOp)
			}
			if rerr != nil {
				return vm.runtimeError(*instruction, "%s", rerr.Error())
			}
			subtract := false
			switch instruction.Op {
			case OpSubLocalIntLocal, OpSubLocalIntConst, OpSubGlobalIntGlobal, OpSubGlobalIntConst,
				OpSubLocalIntGlobal, OpSubGlobalIntLocal:
				subtract = true
			}
			if lv.Kind == runtime.VMKindSmallInt {
				var bv int64
				bOK := true
				if isConst {
					bv = rhsOp
				} else if rv.Kind == runtime.VMKindSmallInt {
					bv = rv.I64
				} else {
					bOK = false
				}
				if bOK {
					var v int64
					var overflow bool
					if subtract {
						v = lv.I64 - bv
						overflow = (lv.I64^bv)&(lv.I64^v) < 0
					} else {
						v = lv.I64 + bv
						overflow = (lv.I64^v)&(bv^v) < 0
					}
					var result runtime.VMValue
					if overflow {
						res := new(big.Int)
						if subtract {
							res.Sub(big.NewInt(lv.I64), big.NewInt(bv))
						} else {
							res.Add(big.NewInt(lv.I64), big.NewInt(bv))
						}
						result = runtime.VMValueFromValue(runtime.Int{Value: res})
					} else {
						result = runtime.VMValueSmallInt(v)
					}
					var storeErr error
					if dstIsGlobal {
						storeErr = vm.setGlobalVM(dst, result)
					} else {
						storeErr = vm.setLocalVM(dst, result)
					}
					if storeErr != nil {
						return vm.runtimeError(*instruction, "%s", storeErr.Error())
					}
					vm.pushVM(result)
					continue
				}
			}
			if err := vm.intSelfArith(*instruction); err != nil {
				return err
			}
		case OpEqual:
			nextIP, err := vm.equal(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpLess, OpGreater, OpLessEqual, OpGreaterEqual:
			nextIP, err := vm.compare(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpNot:
			nextIP, err := vm.not(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpBoolXor:
			if err := vm.boolXor(*instruction); err != nil {
				return err
			}
		case OpNegate:
			nextIP, err := vm.negate(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpDefineGlobal:
			slot := instruction.Operands[0]
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if err := vm.setGlobal(slot, value); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
		case OpGetGlobal:
			slot := instruction.Operands[0]
			if int(slot) < len(vm.curGlobals) {
				value := vm.curGlobals[slot]
				if value.Kind != runtime.VMKindBoxed && value.Kind != runtime.VMKindUnset {
					vm.pushVM(value)
					continue
				}
			}
			value, err := vm.getGlobalVM(slot)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.pushVM(value)
		case OpSetGlobal:
			slot := instruction.Operands[0]
			value, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if int(slot) < len(vm.curGlobals) && !vm.bridgeActive.Load() {
				cur := vm.curGlobals[slot]
				if cur.Kind != runtime.VMKindBoxed {
					vm.curGlobals[slot] = value
					if vm.persistGlobals && int(slot) < len(vm.dirtyGlobals) {
						vm.dirtyGlobals[slot] = true
					}
					vm.pushVM(value)
					continue
				}
			}
			if err := vm.setGlobalVM(slot, value); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.pushVM(value)
		case OpDefineLocal:
			slot := instruction.Operands[0]
			value, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			// Fresh binding: replace the slot rather than writing into a cell a
			// prior iteration boxed, so a re-run `let` captures a distinct value.
			if err := vm.defineLocalVM(slot, value); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
		case OpGetLocal:
			slot := instruction.Operands[0]
			idx := vm.currentFrameBP + int(slot)
			if idx < len(vm.localsStack) {
				value := vm.localsStack[idx]
				if value.Kind != runtime.VMKindBoxed && value.Kind != runtime.VMKindUnset {
					vm.pushVM(value)
					continue
				}
			}
			value, err := vm.getLocalVM(slot)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.pushVM(value)
		case OpSetLocal:
			slot := instruction.Operands[0]
			value, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			idx := vm.currentFrameBP + int(slot)
			if idx < len(vm.localsStack) {
				cur := vm.localsStack[idx]
				if cur.Kind != runtime.VMKindBoxed {
					vm.localsStack[idx] = value
					vm.pushVM(value)
					continue
				}
			}
			if err := vm.setLocalVM(slot, value); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.pushVM(value)
		case OpBuildList:
			count := instruction.Operands[0]
			if count < 0 {
				return vm.runtimeError(*instruction, "list element count out of range")
			}
			elements := make([]runtime.Value, int(count))
			for i := int(count) - 1; i >= 0; i-- {
				value, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				elements[i] = value
			}
			vm.push(&runtime.List{Elements: elements})
		case OpBuildDict:
			count := instruction.Operands[0]
			if count < 0 {
				return vm.runtimeError(*instruction, "dict entry count out of range")
			}
			// Stack is LIFO; reorder pairs so Order tracks source order.
			pairs := make([][2]runtime.Value, count)
			for i := count - 1; i >= 0; i-- {
				value, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				key, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				pairs[i] = [2]runtime.Value{key, value}
			}
			d := runtime.NewDict()
			for _, p := range pairs {
				d.PutEntry(native.DictKey(p[0]), runtime.DictEntry{Key: p[0], Value: p[1]})
			}
			vm.push(d)
		case OpBuildSet:
			count := instruction.Operands[0]
			if count < 0 {
				return vm.runtimeError(*instruction, "set element count out of range")
			}
			elements := make(map[string]runtime.SetEntry, int(count))
			for i := int64(0); i < count; i++ {
				value, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				elements[native.DictKey(value)] = runtime.SetEntry{Value: value}
			}
			vm.push(runtime.Set{Elements: elements})
		case OpIndex:
			if err := vm.index(*instruction); err != nil {
				return err
			}
		case OpContains:
			container, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			needle, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			result, err := vm.contains(needle, container)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.push(result)
		case OpSetIndex:
			nextIP, err := vm.setIndex(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpSlice:
			if err := vm.slice(*instruction); err != nil {
				return err
			}
		case OpIterInit:
			if err := vm.iterInit(*instruction); err != nil {
				return err
			}
		case OpIterNext:
			hasNext, err := vm.iterNext(*instruction)
			if err != nil {
				// A foreign-class __done / __next that threw returns a
				// wrappedError carrying the original vmThrownError; route
				// it to a try / catch via pendingThrow so the user's
				// catch fires. Non-thrown errors bubble out unchanged.
				var thrown vmThrownError
				if errors.As(err, &thrown) {
					captured := thrown.err
					vm.pendingThrow = &captured
					nextIP, perr := vm.jumpToExceptionHandler(*instruction, ip)
					if perr != nil {
						return perr
					}
					ip = nextIP
					continue
				}
				return err
			}
			if !hasNext {
				ip = int(instruction.Operands[0]) - 1
			}
		case OpIterClose:
			if err := vm.iterClose(*instruction); err != nil {
				return err
			}
		case OpTypeAssert:
			if err := vm.typeAssert(*instruction); err != nil {
				return err
			}
		case OpShallowFreeze:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.push(runtime.ShallowFreeze(value))
		case OpMatchListShape:
			if len(instruction.Operands) != 1 {
				return vm.fatalError(*instruction, "match-list-shape instruction has invalid operands")
			}
			expected := instruction.Operands[0]
			vmv, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			ok := false
			if vmv.Kind == runtime.VMKindBoxed {
				if list, isList := vmv.Boxed.(*runtime.List); isList && int64(len(list.Elements)) == expected {
					ok = true
				}
			}
			vm.pushVM(runtime.VMValueBool(ok))
		case OpUnpackList:
			if err := vm.unpackList(*instruction); err != nil {
				return err
			}
		case OpBuildRange:
			if err := vm.buildRange(*instruction); err != nil {
				return err
			}
		case OpExit:
			code, err := vm.popExitCode(*instruction)
			if err != nil {
				return err
			}
			return ExitError{Code: code}
		case OpCall:
			nextIP, err := vm.call(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpTailCall:
			nextIP, err := vm.tailCall(*instruction)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpRange:
			if err := vm.execRange(*instruction, false); err != nil {
				return err
			}
		case OpZRange:
			if err := vm.execRange(*instruction, true); err != nil {
				return err
			}
		case OpTypeOf:
			vmv, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			switch vmv.Kind {
			case runtime.VMKindSmallInt:
				vm.pushVM(runtime.VMValueFromValue(runtime.Type{Name: "int"}))
			case runtime.VMKindBool:
				vm.pushVM(runtime.VMValueFromValue(runtime.Type{Name: "bool"}))
			case runtime.VMKindFloat:
				vm.pushVM(runtime.VMValueFromValue(runtime.Type{Name: "float"}))
			case runtime.VMKindNull, runtime.VMKindUnset:
				vm.pushVM(runtime.VMValueFromValue(runtime.Type{Name: "null"}))
			default:
				value := vmv.ToValue()
				switch v := value.(type) {
				case *runtime.Class:
					vm.push(runtime.Type{Name: v.Name})
				case runtime.BytecodeClass:
					vm.push(runtime.Type{Name: v.Name})
				default:
					vm.push(runtime.Type{Name: value.TypeName()})
				}
			}
		case OpDir:
			vmv, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.push(vmDirValue(vmv.ToValue()))
		case OpDump:
			vmv, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.push(runtime.String{Value: native.DumpValue(vmv.ToValue())})
		case OpSelect:
			nextIP, err := vm.executeSelect(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpInstanceOf:
			if err := vm.instanceOf(*instruction); err != nil {
				return err
			}
		case OpCast:
			nextIP, err := vm.cast(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpMethodCall:
			nextIP, err := vm.methodCall(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCallResolvedMethod:
			nextIP, err := vm.callResolvedMethod(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpMethodCallSpread:
			nextIP, err := vm.methodCallSpread(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpWithEnter:
			nextIP, err := vm.withEnter(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpWithExit:
			nextIP, err := vm.withExit(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpDel:
			if err := vm.execDel(*instruction); err != nil {
				return err
			}
		case OpMethodCallNamed:
			nextIP, err := vm.methodCallNamed(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpNativeCall:
			if err := vm.nativeCall(*instruction); err != nil {
				var recoverable recoverableNativeError
				if errors.As(err, &recoverable) {
					nextIP, throwErr := vm.throwRecoverableError(*instruction, ip, recoverable.err)
					if throwErr != nil {
						return throwErr
					}
					ip = nextIP
					continue
				}
				return err
			}
		case OpNativeCallSpread:
			if err := vm.nativeCallSpread(*instruction); err != nil {
				var recoverable recoverableNativeError
				if errors.As(err, &recoverable) {
					nextIP, throwErr := vm.throwRecoverableError(*instruction, ip, recoverable.err)
					if throwErr != nil {
						return throwErr
					}
					ip = nextIP
					continue
				}
				return err
			}
		case OpNativeCallNamed:
			if err := vm.nativeCallNamed(*instruction); err != nil {
				var recoverable recoverableNativeError
				if errors.As(err, &recoverable) {
					nextIP, throwErr := vm.throwRecoverableError(*instruction, ip, recoverable.err)
					if throwErr != nil {
						return throwErr
					}
					ip = nextIP
					continue
				}
				return err
			}
		case OpDefineClass:
			if len(instruction.Operands) != 1 {
				return vm.fatalError(*instruction, "define class instruction has invalid operands")
			}
			classIndex := instruction.Operands[0]
			if classIndex >= 0 && int(classIndex) < len(vm.curMod.Chunk.Classes) {
				classInfo := vm.curMod.Chunk.Classes[classIndex]
				classValue, decorated, err := vm.applyCallableDecoratorsForClass(classIndex, classInfo)
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				if decorated {
					vm.decoratedClasses[classIndex] = classValue
				}
				if err := vm.resolveCrossModuleInterfaceMembers(*instruction, classIndex, classInfo); err != nil {
					return err
				}
			}
		case OpConstructClass:
			nextIP, err := vm.constructClass(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpGetField:
			nextIP, err := vm.getField(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpSetField:
			nextIP, err := vm.setField(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCallParentConstructor:
			nextIP, err := vm.callParentConstructor(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCallParentMethod:
			nextIP, err := vm.callParentMethod(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpGetStaticValue:
			nextIP, err := vm.getStaticValue(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpSetStaticValue:
			nextIP, err := vm.setStaticValue(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCallStaticMethod:
			nextIP, err := vm.callStaticMethod(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCallStaticMethodSpread:
			nextIP, err := vm.callStaticMethodSpread(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpIdentical:
			if err := vm.identical(*instruction); err != nil {
				return err
			}
		case OpImportModule:
			if err := vm.importModule(*instruction); err != nil {
				return err
			}
		case OpLoadModuleValue:
			canonical, err := vm.constantStringAt(*instruction, instruction.Operands[0], "module name must be string")
			if err != nil {
				return err
			}
			if vm.moduleLoader != nil {
				if module, lerr := vm.moduleLoader.LoadModule(canonical, ""); lerr == nil && module != nil {
					vm.push(module)
					break
				}
			}
			// Pure native modules need no loader; synthesize the module
			// value so reflect lookups work in loader-less embeddings.
			if _, ok := native.NativeModuleNames[canonical]; ok {
				vm.push(&runtime.Module{Name: canonical, Canonical: canonical, Exports: map[string]runtime.Value{}})
				break
			}
			vm.push(runtime.Null{})
		case OpImportFrom:
			if err := vm.importFrom(*instruction); err != nil {
				return err
			}
		case OpNativeValue:
			if len(instruction.Operands) != 2 {
				return vm.fatalError(*instruction, "native value instruction has invalid operands")
			}
			canonical, err := vm.constantStringAt(*instruction, instruction.Operands[0], "native value module must be string")
			if err != nil {
				return err
			}
			name, err := vm.constantStringAt(*instruction, instruction.Operands[1], "native value name must be string")
			if err != nil {
				return err
			}
			v, ok := vm.builtinValue(canonical, name)
			if !ok {
				return vm.runtimeError(*instruction, "%s.%s is not a native function", canonical, name)
			}
			vm.push(v)
		case OpFreezeLocal:
			slot := instruction.Operands[0]
			cur, err := vm.getLocalVM(slot)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			frozen := runtime.FreezeShallowCopy(cur.ToValue())
			if err := vm.setLocalVM(slot, runtime.VMValueFromValue(frozen)); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
		case OpCheckUnpackLen:
			v, err := vm.getLocal(instruction.Operands[0])
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			expected := int(instruction.Operands[1])
			if list, ok := v.(*runtime.List); ok && len(list.Elements) < expected {
				return vm.runtimeError(*instruction, "list has %d elements, destructuring expects %d", len(list.Elements), expected)
			}
		case OpFormatSpec:
			spec, err := vm.popString(*instruction, "format spec must be string")
			if err != nil {
				return err
			}
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			out, ferr := native.FormatValueWithSpec(value, spec)
			if ferr != nil {
				return vm.runtimeError(*instruction, "%s", ferr.Error())
			}
			vm.push(runtime.String{Value: out})
		case OpMakeClosure:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "make closure instruction has invalid operands")
			}
			funcIndex := instruction.Operands[0]
			upvalueCount := instruction.Operands[1]
			if funcIndex < 0 || int(funcIndex) >= len(vm.curMod.Chunk.Functions) {
				return vm.runtimeError(*instruction, "closure function index out of range")
			}
			if int64(len(instruction.Operands))-2 != upvalueCount {
				return vm.runtimeError(*instruction, "closure upvalue count mismatch")
			}
			upvalues := make([]runtime.Value, upvalueCount)
			for i := int64(0); i < upvalueCount; i++ {
				outerSlot := instruction.Operands[2+i]
				if outerSlot < 0 {
					return vm.runtimeError(*instruction, "closure upvalue slot out of range")
				}
				outerIdx := vm.currentFrameBP + int(outerSlot)
				if outerIdx >= len(vm.localsStack) {
					return vm.runtimeError(*instruction, "closure upvalue slot out of range")
				}
				slot := vm.localsStack[outerIdx]
				var cell *runtime.BytecodeCell
				if slot.Kind == runtime.VMKindBoxed {
					if c, ok := slot.Boxed.(*runtime.BytecodeCell); ok {
						cell = c
					}
				}
				if cell == nil {
					cell = &runtime.BytecodeCell{Value: slot.ToValue()}
					vm.localsStack[outerIdx] = runtime.VMValueFromValue(cell)
				}
				upvalues[i] = cell
			}
			fn := vm.curMod.Chunk.Functions[funcIndex]
			// Capture enclosing generic frame's type bindings for T-typed params and instanceof T in the closure.
			var capturedBindings map[string]string
			if len(vm.frames) > 0 {
				if outer := vm.frames[len(vm.frames)-1].typeBindings; len(outer) > 0 {
					capturedBindings = make(map[string]string, len(outer))
					for k, v := range outer {
						capturedBindings[k] = v
					}
				}
			}
			vm.push(runtime.BytecodeClosure{
				FunctionIndex: funcIndex,
				Name:          fn.Name,
				Module:        vm.moduleName,
				Upvalues:      upvalues,
				TypeBindings:  capturedBindings,
			})
		case OpMakeOverloaded:
			if len(instruction.Operands) == 0 {
				return vm.fatalError(*instruction, "make overloaded instruction has no operands")
			}
			for _, idx := range instruction.Operands {
				if idx < 0 || int(idx) >= len(vm.curMod.Chunk.Functions) {
					return vm.runtimeError(*instruction, "overloaded function index out of range")
				}
			}
			vm.push(vm.overloadedValue(instruction.Operands))
		case OpMakeError:
			if err := vm.makeError(*instruction); err != nil {
				return err
			}
		case OpPushExceptionHandler:
			if len(instruction.Operands) != 1 {
				return vm.fatalError(*instruction, "exception handler instruction has invalid operands")
			}
			snap, snapBase := vm.snapshotCurrentFrameLocals()
			vm.exceptionHandlers = append(vm.exceptionHandlers, exceptionHandler{
				handlerIP:        int(instruction.Operands[0]),
				frameDepth:       len(vm.frames),
				stackDepth:       len(vm.stack),
				localsStackDepth: len(vm.localsStack),
				snapshot:         snap,
				snapshotBase:     snapBase,
			})
		case OpPopExceptionHandler:
			if len(vm.exceptionHandlers) == 0 {
				return vm.runtimeError(*instruction, "exception handler stack is empty")
			}
			vm.exceptionHandlers = vm.exceptionHandlers[:len(vm.exceptionHandlers)-1]
		case OpThrow:
			nextIP, err := vm.throw(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpCatch:
			nextIP, err := vm.catchException(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpRethrow:
			nextIP, err := vm.rethrow(*instruction, ip)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpRuntimeError:
			message, err := vm.popString(*instruction, "runtime error message must be string")
			if err != nil {
				return err
			}
			return vm.runtimeError(*instruction, "%s", message)
		case OpMatchError:
			if len(instruction.Operands) != 1 || int(instruction.Operands[0]) >= vm.constantsLen() {
				return vm.fatalError(*instruction, "match error instruction has invalid operands")
			}
			hint, ok := vm.constantValue(instruction.Operands[0]).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "match error hint must be string")
			}
			matchValue, popErr := vm.pop()
			if popErr != nil {
				return vm.runtimeError(*instruction, "%s", popErr.Error())
			}
			msg := fmt.Sprintf("%s; got %s (type: %s)", hint.Value, matchValue.Inspect(), matchValue.TypeName())
			matchErrValue := vm.withErrorStackTrace(runtime.Error{Class: "MatchError", Message: msg}, int(instruction.Line))
			vm.pendingThrow = &matchErrValue
			nextMatchIP, throwErr := vm.jumpToExceptionHandler(*instruction, ip)
			if throwErr != nil {
				return throwErr
			}
			ip = nextMatchIP
		case OpDeferPrint:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindPrint, value: value})
		case OpDeferPrintln:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindPrintln, value: value})
		case OpDeferNativeCall:
			if len(instruction.Operands) != 2 {
				return vm.fatalError(*instruction, "defer native call instruction has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := instruction.Operands[1]
			if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "defer native call name out of range")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer native call name must be string")
			}
			args := make([]runtime.Value, argc)
			for i := int(argc) - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			vm.addDefer(deferredAction{kind: deferKindNative, name: nameConst.Value, args: args, line: int(instruction.Line)})
		case OpDeferFuncCall:
			if len(instruction.Operands) != 2 {
				return vm.fatalError(*instruction, "defer func call instruction has invalid operands")
			}
			funcIdx := instruction.Operands[0]
			argc := instruction.Operands[1]
			if funcIdx < 0 || int(funcIdx) >= len(vm.curMod.Chunk.Functions) {
				return vm.runtimeError(*instruction, "defer func call function index out of range")
			}
			args := make([]runtime.Value, argc)
			for i := int(argc) - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			vm.addDefer(deferredAction{kind: deferKindFunc, funcIdx: funcIdx, args: args, line: int(instruction.Line)})
		case OpDeferMethodCall:
			if len(instruction.Operands) != 2 {
				return vm.fatalError(*instruction, "defer method call instruction has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := instruction.Operands[1]
			if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "defer method call name out of range")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer method call name must be string")
			}
			args := make([]runtime.Value, argc)
			for i := int(argc) - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			receiver, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindMethod, name: nameConst.Value, receiver: receiver, args: args, line: int(instruction.Line)})
		case OpDeferCallableCall:
			if len(instruction.Operands) != 1 {
				return vm.fatalError(*instruction, "defer callable call instruction has invalid operands")
			}
			argc := instruction.Operands[0]
			args := make([]runtime.Value, argc)
			for i := int(argc) - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			callable, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindCallable, value: callable, args: args, line: int(instruction.Line)})
		case OpDeferNativeCallNamed:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "defer named native call has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "defer named native call argument metadata mismatch")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer named native call name must be string")
			}
			args := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			names, err := vm.readArgNames(*instruction, instruction.Operands[2:])
			if err != nil {
				return err
			}
			vm.addDefer(deferredAction{kind: deferKindNative, name: nameConst.Value, args: args, names: names, line: int(instruction.Line)})
		case OpDeferMethodCallNamed:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "defer named method call has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "defer named method call argument metadata mismatch")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer named method call name must be string")
			}
			args := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			names, err := vm.readArgNames(*instruction, instruction.Operands[2:])
			if err != nil {
				return err
			}
			receiver, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindMethod, name: nameConst.Value, receiver: receiver, args: args, names: names, line: int(instruction.Line)})
		case OpDeferCallableCallNamed:
			if len(instruction.Operands) < 1 {
				return vm.fatalError(*instruction, "defer named callable call has invalid operands")
			}
			argc := int(instruction.Operands[0])
			if len(instruction.Operands) != 1+argc {
				return vm.runtimeError(*instruction, "defer named callable call argument metadata mismatch")
			}
			args := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				args[i] = v
			}
			names, err := vm.readArgNames(*instruction, instruction.Operands[1:])
			if err != nil {
				return err
			}
			callable, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindCallable, value: callable, args: args, names: names, line: int(instruction.Line)})
		case OpDeferFuncCallSpread:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "defer func call-spread instruction has invalid operands")
			}
			funcIdx := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if funcIdx < 0 || int(funcIdx) >= len(vm.curMod.Chunk.Functions) {
				return vm.runtimeError(*instruction, "defer func call function index out of range")
			}
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "defer func call-spread argument metadata mismatch")
			}
			args, names, mask, err := vm.collectDeferSpreadArgs(*instruction, instruction.Operands[2:])
			if err != nil {
				return err
			}
			vm.addDefer(deferredAction{kind: deferKindFunc, funcIdx: funcIdx, args: args, names: names, spreadMask: mask, line: int(instruction.Line)})
		case OpDeferMethodCallSpread:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "defer method call-spread instruction has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "defer method call name out of range")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer method call name must be string")
			}
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "defer method call-spread argument metadata mismatch")
			}
			args, names, mask, err := vm.collectDeferSpreadArgs(*instruction, instruction.Operands[2:])
			if err != nil {
				return err
			}
			receiver, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindMethod, name: nameConst.Value, receiver: receiver, args: args, names: names, spreadMask: mask, line: int(instruction.Line)})
		case OpDeferNativeCallSpread:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "defer native call-spread instruction has invalid operands")
			}
			nameIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if nameIndex < 0 || int(nameIndex) >= vm.constantsLen() {
				return vm.runtimeError(*instruction, "defer native call name out of range")
			}
			nameConst, ok := vm.constantValue(nameIndex).(runtime.String)
			if !ok {
				return vm.runtimeError(*instruction, "defer native call name must be string")
			}
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "defer native call-spread argument metadata mismatch")
			}
			args, names, mask, err := vm.collectDeferSpreadArgs(*instruction, instruction.Operands[2:])
			if err != nil {
				return err
			}
			vm.addDefer(deferredAction{kind: deferKindNative, name: nameConst.Value, args: args, names: names, spreadMask: mask, line: int(instruction.Line)})
		case OpDeferCallableCallSpread:
			if len(instruction.Operands) < 1 {
				return vm.fatalError(*instruction, "defer callable call-spread instruction has invalid operands")
			}
			argc := int(instruction.Operands[0])
			if len(instruction.Operands) != 1+argc {
				return vm.runtimeError(*instruction, "defer callable call-spread argument metadata mismatch")
			}
			args, names, mask, err := vm.collectDeferSpreadArgs(*instruction, instruction.Operands[1:])
			if err != nil {
				return err
			}
			callable, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			vm.addDefer(deferredAction{kind: deferKindCallable, value: callable, args: args, names: names, spreadMask: mask, line: int(instruction.Line)})
		case OpPrintln:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			text, err := vm.displayString(value)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if vm.shouldRouteDirectPrint() {
				if _, err := vm.statefulNativeCall("io", "println", []runtime.Value{runtime.String{Value: text}}, nil); err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				continue
			}
			if _, err := fmt.Fprintln(vm.stdout, text); err != nil {
				return err
			}
		case OpPrint:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			text, err := vm.displayString(value)
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if vm.shouldRouteDirectPrint() {
				if _, err := vm.statefulNativeCall("io", "print", []runtime.Value{runtime.String{Value: text}}, nil); err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				continue
			}
			if _, err := fmt.Fprint(vm.stdout, text); err != nil {
				return err
			}
		case OpJump:
			ip = int(instruction.Operands[0]) - 1
		case OpJumpIfFalse:
			value, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			b, ok := value.AsBool()
			if !ok {
				return vm.runtimeError(*instruction, "jump condition must be bool")
			}
			if !b {
				ip = int(instruction.Operands[0]) - 1
			}
		case OpPop:
			if _, err := vm.popVM(); err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
		case OpDup:
			if len(vm.stack) == 0 {
				return vm.fatalError(*instruction, "stack underflow")
			}
			vm.pushVM(vm.stack[len(vm.stack)-1])
		case OpReturn:
			if len(vm.frames) == 0 {
				if err := vm.runDefers(*instruction); err != nil {
					return err
				}
				return nil
			}
			valueVM, err := vm.popVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			// Deferred-action lists are empty for most frames; popping the
			// level inline skips the call (and its instruction copy).
			if n := len(vm.defers); n > 0 && len(vm.defers[n-1]) == 0 {
				vm.defers = vm.defers[:n-1]
			} else if err := vm.runDefers(*instruction); err != nil {
				return err
			}
			// Read out the frame fields we need without copying the whole
			// struct, then clear references in the slot before shrinking so
			// the popped slot doesn't pin a defunct locals snapshot or
			// generator channel.
			frameIdx := len(vm.frames) - 1
			slot := &vm.frames[frameIdx]
			returnIP := slot.returnIP
			returnOverride := slot.returnOverride
			isErrorClass := slot.isErrorClass
			isImmutableClass := slot.isImmutableClass
			immutableFieldsToLock := slot.immutableFieldsToLock
			lockInstance := slot.lockInstance
			isDestructibleConstructor := slot.isDestructibleConstructor
			negateReturn := slot.negateReturn
			vm.popLocalsStackFrame(slot)
			slot.returnOverride = nil
			slot.generator = nil
			slot.generatorDone = nil
			slot.typeBindings = nil
			slot.immutableFieldsToLock = nil
			slot.lockInstance = nil
			vm.frames = vm.frames[:frameIdx]
			// Hot path: a regular return - no override, no error-class
			// reification, no negate, no immutable freeze/field-lock, not a
			// destructor-bearing constructor. Push the VMValue
			// straight back without converting through runtime.Value.
			if returnOverride == nil && !isErrorClass && !isImmutableClass && immutableFieldsToLock == nil && !negateReturn && !isDestructibleConstructor {
				vm.pushVM(valueVM)
				if inlineExitDepth >= 0 && len(vm.frames) <= inlineExitDepth {
					return nil
				}
				ip = returnIP
				continue
			}
			value := valueVM.ToValue()
			if returnOverride != nil {
				value = returnOverride
			}
			if isDestructibleConstructor {
				if inst, ok := value.(*runtime.Instance); ok {
					vm.trackDestructible(inst)
				}
			}
			if isErrorClass {
				if inst, ok := value.(*runtime.Instance); ok {
					msg := ""
					if m, ok2 := inst.Fields["__parentMsg"]; ok2 {
						if s, ok3 := m.(runtime.String); ok3 {
							msg = s.Value
						}
						delete(inst.Fields, "__parentMsg")
					}
					var fields map[string]runtime.Value
					if len(inst.Fields) > 0 {
						fields = inst.Fields
					}
					var parents []string
					if classInfo, ok := vm.classInfo(inst.Class.Name); ok {
						parents = vm.errorParentChain(classInfo)
					}
					value = runtime.Error{Class: inst.Class.Name, Message: msg, Fields: fields, Parents: parents}
				}
			}
			if isImmutableClass {
				if inst, ok := value.(*runtime.Instance); ok {
					inst.Frozen = true
				}
			}
			if len(immutableFieldsToLock) > 0 && lockInstance != nil {
				for _, f := range immutableFieldsToLock {
					lockInstance.LockField(f)
				}
			}
			if negateReturn {
				boolValue, ok := value.(runtime.Bool)
				if !ok {
					return vm.runtimeError(*instruction, "comparison operator method must return bool")
				}
				value = runtime.Bool{Value: !boolValue.Value}
			}
			vm.push(value)
			if inlineExitDepth >= 0 && len(vm.frames) <= inlineExitDepth {
				return nil
			}
			ip = returnIP
		case OpYield:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if len(vm.frames) == 0 || vm.frames[len(vm.frames)-1].generator == nil {
				return vm.runtimeError(*instruction, "yield can only be used inside a generator function")
			}
			frame := vm.frames[len(vm.frames)-1]
			select {
			case frame.generator <- vmGeneratorItem{value: value}:
			case <-frame.generatorDone:
				return nil
			}
		case OpBitAnd, OpBitOr, OpBitXor, OpLShift, OpRShift:
			nextIP, err := vm.bitwiseInfix(*instruction, ip)
			if err != nil {
				return err
			}
			if nextIP >= 0 {
				ip = nextIP
			}
		case OpBitNot:
			nextIP, err := vm.bitwiseNot(*instruction, ip)
			if err != nil {
				return err
			}
			if nextIP >= 0 {
				ip = nextIP
			}
		case OpNullCoalesce:
			top, err := vm.peekVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if top.Kind != runtime.VMKindNull && top.Kind != runtime.VMKindUnset {
				ip = int(instruction.Operands[0]) - 1
			} else {
				if _, err := vm.popVM(); err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
			}
		case OpOptionalChain:
			top, err := vm.peekVM()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if top.Kind == runtime.VMKindNull || top.Kind == runtime.VMKindUnset {
				ip = int(instruction.Operands[0]) - 1
			}
		case OpCallSpread:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "call-spread instruction has invalid operands")
			}
			funcIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if funcIndex < 0 || int(funcIndex) >= len(vm.curMod.Chunk.Functions) {
				return vm.runtimeError(*instruction, "function index out of range")
			}
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "call-spread argument metadata mismatch")
			}
			args, names, fromSpread, hasNamed, err := vm.collectSpreadCallArgs(*instruction, instruction.Operands[2:], true)
			if err != nil {
				return err
			}
			fn := vm.curMod.Chunk.Functions[funcIndex]
			if hasNamed {
				fa, fnames := dropUnknownSpreadNames(args, names, fromSpread, fn.ParamNames)
				args, err = vm.orderRuntimeArguments(*instruction, fn, fa, fnames, 0)
				if err != nil {
					return err
				}
			}
			nextIP, err := vm.startFunction(*instruction, ip, &vm.curMod.Chunk.Functions[funcIndex], args, nil)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpConstructClassSpread:
			if len(instruction.Operands) < 2 {
				return vm.fatalError(*instruction, "construct-class-spread instruction has invalid operands")
			}
			classIndex := instruction.Operands[0]
			argc := int(instruction.Operands[1])
			if classIndex < 0 || int(classIndex) >= len(vm.curMod.Chunk.Classes) {
				return vm.runtimeError(*instruction, "class index out of range")
			}
			if len(instruction.Operands) != 2+argc {
				return vm.runtimeError(*instruction, "construct-class-spread argument metadata mismatch")
			}
			args, names, fromSpread, hasNamed, err := vm.collectSpreadCallArgs(*instruction, instruction.Operands[2:], true)
			if err != nil {
				return err
			}
			if hasNamed {
				classInfo := vm.curMod.Chunk.Classes[classIndex]
				if len(classInfo.ConstructorIndices) != 1 {
					return vm.runtimeError(*instruction, "cannot use named arguments with spread without a single constructor on %s", classInfo.Name)
				}
				ctor := vm.curMod.Chunk.Functions[classInfo.ConstructorIndices[0]]
				ctorParams := ctor.ParamNames
				if len(ctorParams) > 0 {
					ctorParams = ctorParams[1:] // skip the receiver slot
				}
				fa, fnames := dropUnknownSpreadNames(args, names, fromSpread, ctorParams)
				args, err = vm.orderRuntimeArguments(*instruction, ctor, fa, fnames, 1)
				if err != nil {
					return err
				}
			}
			nextIP, err := vm.constructClassWithArgs(*instruction, ip, classIndex, args, false)
			if err != nil {
				return err
			}
			ip = nextIP
		case OpListConcat:
			if len(instruction.Operands) != 1 {
				return vm.fatalError(*instruction, "list-concat instruction has invalid operands")
			}
			n := int(instruction.Operands[0])
			segments := make([]*runtime.List, n)
			for i := n - 1; i >= 0; i-- {
				val, err := vm.pop()
				if err != nil {
					return vm.runtimeError(*instruction, "%s", err.Error())
				}
				list, ok := val.(*runtime.List)
				if !ok {
					return vm.runtimeError(*instruction, "list-concat operand must be a list")
				}
				segments[i] = list
			}
			total := 0
			for _, seg := range segments {
				total += len(seg.Elements)
			}
			result := make([]runtime.Value, 0, total)
			for _, seg := range segments {
				result = append(result, seg.Elements...)
			}
			vm.push(&runtime.List{Elements: result})
		case OpAwait:
			value, err := vm.pop()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if task, ok := value.(*runtime.Task); ok {
				result := task.Await()
				if result.Err != nil {
					nextIP, throwErr := vm.throwRecoverableError(*instruction, ip, result.Err)
					if throwErr != nil {
						return throwErr
					}
					ip = nextIP
				} else if result.Value == nil {
					vm.push(runtime.Null{})
				} else {
					vm.push(result.Value)
				}
			} else {
				vm.push(value)
			}
		case OpSetTypeBindings:
			// Operands: [count, paramName1Idx, typeName1Idx, paramName2Idx, typeName2Idx, ...]
			// Peeks at the top of stack; if it is an instance, sets TypeBindings from the
			// declaration annotation, overriding any bindings inferred from constructor args.
			if len(instruction.Operands) < 1 {
				return vm.runtimeError(*instruction, "OpSetTypeBindings: missing operands")
			}
			count := int(instruction.Operands[0])
			if len(instruction.Operands) < 1+count*2 {
				return vm.runtimeError(*instruction, "OpSetTypeBindings: operand count mismatch")
			}
			top, err := vm.peek()
			if err != nil {
				return vm.runtimeError(*instruction, "%s", err.Error())
			}
			if instance, ok := top.(*runtime.Instance); ok {
				for j := 0; j < count; j++ {
					pIdx := instruction.Operands[1+j*2]
					tIdx := instruction.Operands[2+j*2]
					if int(pIdx) >= vm.constantsLen() || int(tIdx) >= vm.constantsLen() {
						return vm.runtimeError(*instruction, "OpSetTypeBindings: constant index out of range")
					}
					paramName, pOK := vm.constantValue(pIdx).(runtime.String)
					typeName, tOK := vm.constantValue(tIdx).(runtime.String)
					if !pOK || !tOK {
						return vm.runtimeError(*instruction, "OpSetTypeBindings: constants must be strings")
					}
					if instance.TypeBindings == nil {
						instance.TypeBindings = map[string]string{}
					}
					instance.TypeBindings[paramName.Value] = typeName.Value
				}
			}
		case OpPlantCallTypeBindings:
			// Stages explicit `<TypeArgs>` from a generic function call's
			// `name<T>(args)` syntax into vm.pendingTypeBindings; the next
			// OpCall sees these alongside any closure-inherited bindings
			// and seeds inferTypeBindingsFromLocals with them, giving
			// explicit args strict priority over arg-inferred bindings.
			// Operands match OpSetTypeBindings: [count, pName1Idx,
			// tName1Idx, pName2Idx, tName2Idx, ...].
			if len(instruction.Operands) < 1 {
				return vm.runtimeError(*instruction, "OpPlantCallTypeBindings: missing operands")
			}
			count := int(instruction.Operands[0])
			if len(instruction.Operands) < 1+count*2 {
				return vm.runtimeError(*instruction, "OpPlantCallTypeBindings: operand count mismatch")
			}
			if count > 0 && vm.pendingTypeBindings == nil {
				vm.pendingTypeBindings = map[string]string{}
			}
			for j := 0; j < count; j++ {
				pIdx := instruction.Operands[1+j*2]
				tIdx := instruction.Operands[2+j*2]
				if int(pIdx) >= vm.constantsLen() || int(tIdx) >= vm.constantsLen() {
					return vm.runtimeError(*instruction, "OpPlantCallTypeBindings: constant index out of range")
				}
				paramName, pOK := vm.constantValue(pIdx).(runtime.String)
				typeName, tOK := vm.constantValue(tIdx).(runtime.String)
				if !pOK || !tOK {
					return vm.runtimeError(*instruction, "OpPlantCallTypeBindings: constants must be strings")
				}
				vm.pendingTypeBindings[paramName.Value] = typeName.Value
			}
		case OpPlantCallTypeArgs:
			if len(instruction.Operands) < 1 {
				return vm.runtimeError(*instruction, "OpPlantCallTypeArgs: missing operands")
			}
			count := int(instruction.Operands[0])
			if len(instruction.Operands) < 1+count {
				return vm.runtimeError(*instruction, "OpPlantCallTypeArgs: operand count mismatch")
			}
			names := make([]string, 0, count)
			for j := 0; j < count; j++ {
				tIdx := instruction.Operands[1+j]
				if int(tIdx) >= vm.constantsLen() {
					return vm.runtimeError(*instruction, "OpPlantCallTypeArgs: constant index out of range")
				}
				typeName, ok := vm.constantValue(tIdx).(runtime.String)
				if !ok {
					return vm.runtimeError(*instruction, "OpPlantCallTypeArgs: constants must be strings")
				}
				names = append(names, typeName.Value)
			}
			vm.pendingCallTypeArgs = names
		default:
			return vm.fatalError(*instruction, "unknown opcode %d", instruction.Op)
		}
	}
	return nil
}

func (vm *VM) boolXor(instruction Instruction) error {
	right, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	l, ok := left.(runtime.Bool)
	if !ok {
		return vm.runtimeError(instruction, "left operand must be bool")
	}
	r, ok := right.(runtime.Bool)
	if !ok {
		return vm.runtimeError(instruction, "right operand must be bool")
	}
	vm.push(runtime.Bool{Value: l.Value != r.Value})
	return nil
}

func (vm *VM) not(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if nextIP, handled, err := vm.callPrefixOperatorMethod(instruction, ip, value); handled || err != nil {
		return nextIP, err
	}
	boolValue, ok := value.(runtime.Bool)
	if !ok {
		return 0, vm.runtimeError(instruction, "! expects bool")
	}
	vm.push(runtime.Bool{Value: !boolValue.Value})
	return ip, nil
}

func (vm *VM) negate(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if nextIP, handled, err := vm.callPrefixOperatorMethod(instruction, ip, value); handled || err != nil {
		return nextIP, err
	}
	switch value := value.(type) {
	case runtime.SmallInt:
		if value.Value == math.MinInt64 {
			vm.push(runtime.Int{Value: new(big.Int).Neg(big.NewInt(value.Value))})
		} else {
			vm.push(runtime.SmallInt{Value: -value.Value})
		}
	case runtime.Int:
		vm.push(runtime.Int{Value: new(big.Int).Neg(value.Value)})
	case runtime.Decimal:
		vm.push(runtime.Decimal{Value: new(big.Rat).Neg(value.Value)})
	case runtime.Float:
		vm.push(runtime.Float{Value: -value.Value})
	default:
		if result, handled, err := native.UnaryMinusValue(value); handled {
			if err != nil {
				return 0, vm.callPropagate(instruction, err)
			}
			vm.push(result)
			return ip, nil
		}
		return 0, vm.runtimeError(instruction, "- expects numeric value, got %s", value.TypeName())
	}
	return ip, nil
}

func (vm *VM) callPrefixOperatorMethod(instruction Instruction, ip int, value runtime.Value) (int, bool, error) {
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return ip, false, nil
	}
	methodName, ok := prefixOperatorMethodName(instruction.Op)
	if !ok {
		return ip, false, nil
	}
	if instance.Class.Module != vm.moduleName {
		if vm.moduleLoader == nil {
			return 0, true, vm.runtimeError(instruction, "bytecode module loader is not configured")
		}
		result, err := vm.moduleLoader.CallModuleMethod(instance.Class.Module, instance.Class.Name, methodName, instance, nil, vm)
		if err != nil {
			nextIP, perr := vm.propagateModuleError(instruction, ip, err)
			return nextIP, true, perr
		}
		vm.push(result)
		return ip, true, nil
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return 0, true, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
	}
	indices, ok := vm.lookupMethod(classInfo, methodName)
	if !ok {
		return ip, false, nil
	}
	functionIndex, err := vm.selectRuntimeFunction(instruction, methodName, indices, nil, 1)
	if err != nil {
		return 0, true, err
	}
	nextIP, err := vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance}, nil)
	return nextIP, true, err
}

func prefixOperatorMethodName(op Op) (string, bool) {
	switch op {
	case OpNot:
		return "__not", true
	case OpNegate:
		return "__neg", true
	default:
		return "", false
	}
}

func (vm *VM) compare(instruction Instruction, ip int) (int, error) {
	right, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	switch instruction.Op {
	case OpLess, OpGreater, OpLessEqual, OpGreaterEqual:
		// Resolution chain mirrors the evaluator's comparisonAttempts:
		// direct dunder, negated inverse on the left, swapped on the right.
		type attempt struct {
			name   string
			recv   runtime.Value
			arg    runtime.Value
			negate bool
		}
		var attempts []attempt
		switch instruction.Op {
		case OpLess:
			attempts = []attempt{{"__lt", left, right, false}, {"__gt", right, left, false}}
		case OpGreater:
			attempts = []attempt{{"__gt", left, right, false}, {"__lt", right, left, false}}
		case OpLessEqual:
			attempts = []attempt{{"__lte", left, right, false}, {"__gt", left, right, true}, {"__lt", right, left, true}}
		case OpGreaterEqual:
			attempts = []attempt{{"__gte", left, right, false}, {"__lt", left, right, true}, {"__gt", right, left, true}}
		}
		for _, at := range attempts {
			instance, ok := at.recv.(*runtime.Instance)
			if !ok {
				continue
			}
			classInfo, ok := vm.classInfo(instance.Class.Name)
			if !ok {
				return 0, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
			}
			indices, ok := vm.lookupMethod(classInfo, at.name)
			if !ok {
				continue
			}
			functionIndex, err := vm.selectRuntimeFunction(instruction, at.name, indices, []runtime.Value{at.arg}, 1)
			if err != nil {
				return 0, err
			}
			nextIP, err := vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, at.arg}, nil)
			if err != nil {
				return 0, err
			}
			if at.negate {
				vm.frames[len(vm.frames)-1].negateReturn = true
			}
			return nextIP, nil
		}
		opSymbol := map[Op]string{OpLess: "<", OpGreater: ">", OpLessEqual: "<=", OpGreaterEqual: ">="}[instruction.Op]
		if result, handled, err := native.BinaryOperatorValue(opSymbol, left, right); handled {
			if err != nil {
				return 0, vm.callPropagate(instruction, err)
			}
			vm.push(result)
			return ip, nil
		}
		cmp, err := native.NumericCompare(left, right)
		if err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
		switch instruction.Op {
		case OpLess:
			vm.push(runtime.Bool{Value: cmp < 0})
		case OpGreater:
			vm.push(runtime.Bool{Value: cmp > 0})
		case OpLessEqual:
			vm.push(runtime.Bool{Value: cmp <= 0})
		case OpGreaterEqual:
			vm.push(runtime.Bool{Value: cmp >= 0})
		}
		return ip, nil
	default:
		return 0, vm.runtimeError(instruction, "unknown comparison opcode")
	}
}

func (vm *VM) equal(instruction Instruction, ip int) (int, error) {
	right, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if instance, ok := left.(*runtime.Instance); ok {
		if instance.Class.Module != vm.moduleName {
			if len(instance.Class.Methods["__eq"]) == 0 {
				eq, err := vm.deepEqual(left, right)
				if err != nil {
					return vm.propagateModuleError(instruction, ip, err)
				}
				vm.push(runtime.Bool{Value: eq})
				return ip, nil
			}
			if vm.moduleLoader == nil {
				return 0, vm.runtimeError(instruction, "bytecode module loader is not configured")
			}
			result, err := vm.moduleLoader.CallModuleMethod(instance.Class.Module, instance.Class.Name, "__eq", instance, vm.wrapStatefulNativeArgs("", "", []runtime.Value{right}), vm)
			if err != nil {
				return vm.propagateModuleError(instruction, ip, err)
			}
			vm.push(result)
			return ip, nil
		}
		classInfo, ok := vm.classInfo(instance.Class.Name)
		if !ok {
			return 0, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
		}
		if indices, ok := vm.lookupMethod(classInfo, "__eq"); ok {
			functionIndex, err := vm.selectRuntimeFunction(instruction, "__eq", indices, []runtime.Value{right}, 1)
			if err != nil {
				return 0, err
			}
			return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, right}, nil)
		}
	}
	eq, err := vm.deepEqual(left, right)
	if err != nil {
		return vm.propagateModuleError(instruction, ip, err)
	}
	vm.push(runtime.Bool{Value: eq})
	return ip, nil
}

// deepEqual is the == operator's structural comparison: it recurses through containers and instance fields, dispatching a user __eq at every nesting depth so a nested instance compares exactly as it does at top level.
func (vm *VM) deepEqual(left runtime.Value, right runtime.Value) (bool, error) {
	if eq, both := runtime.NumericValuesEqual(left, right); both {
		return eq, nil
	}
	switch leftValue := left.(type) {
	case *runtime.List:
		rightValue, ok := right.(*runtime.List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false, nil
		}
		for i, element := range leftValue.Elements {
			eq, err := vm.deepEqual(element, rightValue.Elements[i])
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	case runtime.Dict:
		rightValue, ok := right.(runtime.Dict)
		if !ok || leftValue.Len() != rightValue.Len() {
			return false, nil
		}
		equal := true
		var iterErr error
		leftValue.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			other, ok := rightValue.GetEntry(key)
			if !ok {
				equal = false
				return false
			}
			keyEq, err := vm.deepEqual(entry.Key, other.Key)
			if err != nil {
				iterErr, equal = err, false
				return false
			}
			valEq, err := vm.deepEqual(entry.Value, other.Value)
			if err != nil {
				iterErr, equal = err, false
				return false
			}
			if !keyEq || !valEq {
				equal = false
				return false
			}
			return true
		})
		return equal, iterErr
	case runtime.Set:
		rightValue, ok := right.(runtime.Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false, nil
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok {
				return false, nil
			}
			eq, err := vm.deepEqual(entry.Value, other.Value)
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	case runtime.EnumVariant:
		rightValue, ok := right.(runtime.EnumVariant)
		if !ok || leftValue.Enum != rightValue.Enum || leftValue.Variant != rightValue.Variant || len(leftValue.Fields) != len(rightValue.Fields) {
			return false, nil
		}
		for i, field := range leftValue.Fields {
			eq, err := vm.deepEqual(field, rightValue.Fields[i])
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	case *runtime.Instance:
		return vm.instanceDeepEqual(leftValue, right)
	}
	return primitiveEqual(left, right), nil
}

// instanceDeepEqual dispatches a user __eq if the class declares one, else compares fields structurally.
func (vm *VM) instanceDeepEqual(instance *runtime.Instance, right runtime.Value) (bool, error) {
	if instance.Class != nil && len(instance.Class.Methods["__eq"]) > 0 {
		result, err := vm.CallMethod(instance, "__eq", []runtime.Value{right})
		if err != nil {
			return false, err
		}
		boolResult, ok := result.(runtime.Bool)
		if !ok {
			return false, fmt.Errorf("%s.__eq must return bool", instance.Class.Name)
		}
		return boolResult.Value, nil
	}
	other, ok := right.(*runtime.Instance)
	if !ok || !strings.EqualFold(instance.Class.Name, other.Class.Name) || len(instance.Fields) != len(other.Fields) {
		return false, nil
	}
	for name, value := range instance.Fields {
		otherValue, ok := other.Fields[name]
		if !ok {
			return false, nil
		}
		eq, err := vm.deepEqual(value, otherValue)
		if err != nil || !eq {
			return eq, err
		}
	}
	return true, nil
}

func (vm *VM) identical(instruction Instruction) error {
	right, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	vm.push(runtime.Bool{Value: valuesIdentical(left, right)})
	return nil
}

func (vm *VM) makeError(instruction Instruction) error {
	if len(instruction.Operands) != 2 {
		return vm.fatalError(instruction, "make error instruction has invalid operands")
	}
	class, err := vm.constantStringAt(instruction, instruction.Operands[0], "error class must be string")
	if err != nil {
		return err
	}
	argc := int(instruction.Operands[1])
	if argc > 1 {
		return vm.runtimeError(instruction, "%s expects zero or one argument", class)
	}
	message := ""
	if argc == 1 {
		value, err := vm.pop()
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		stringValue, ok := value.(runtime.String)
		if !ok {
			return vm.runtimeError(instruction, "%s message must be string", class)
		}
		message = stringValue.Value
	}
	vm.push(runtime.Error{Class: class, Message: message})
	return nil
}

func (vm *VM) importFrom(instruction Instruction) error {
	if len(instruction.Operands) < 3 {
		return vm.fatalError(instruction, "import-from instruction has invalid operands")
	}
	canonical, err := vm.constantStringAt(instruction, instruction.Operands[0], "module name must be string")
	if err != nil {
		return err
	}
	isNative := instruction.Operands[1] != 0
	count := int(instruction.Operands[2])
	if len(instruction.Operands) != 3+2*count {
		return vm.runtimeError(instruction, "import-from instruction operand count mismatch")
	}
	var module *runtime.Module
	if !isNative {
		if vm.moduleLoader == nil {
			return vm.runtimeError(instruction, "bytecode module loader is not configured")
		}
		loaded, err := vm.moduleLoader.LoadModule(canonical, "")
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		module = loaded
	}
	for i := 0; i < count; i++ {
		nameIdx := instruction.Operands[3+2*i]
		slot := instruction.Operands[3+2*i+1]
		name, err := vm.constantStringAt(instruction, nameIdx, "import-from name must be string")
		if err != nil {
			return err
		}
		var value runtime.Value
		if isNative {
			key := native.Key(canonical, name)
			fn := vm.natives.LookupKey(key)
			if fn == nil && vm.statefulNative != nil {
				value, err = vm.wrapStatefulNativeImport(canonical, name)
				if err != nil {
					return vm.callPropagate(instruction, err)
				}
			} else if fn != nil {
				captured := fn
				value = runtime.Function{
					Name: canonical + "." + name,
					Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
						return captured(args)
					},
				}
			} else {
				return vm.runtimeError(instruction, "from %s import %s: %s is not exported", canonical, name, name)
			}
		} else {
			v, ok := module.Exports[name]
			if !ok {
				return vm.runtimeError(instruction, "from %s import %s: %s is not exported", canonical, name, name)
			}
			value = v
		}
		if err := vm.setGlobal(slot, value); err != nil {
			return err
		}
	}
	return nil
}

// wrapStatefulNativeImport bridges from-imports of stateful native
// modules (http, db, etc.) by routing each call back through the
// statefulNative caller exposed on the VM.
// builtinValue wraps a pure native builtin (canonical.name) as a first-class
// callable value, matching the evaluator's nativeBuiltinValue. Gated on the
// pure-native registry so both backends resolve the identical set.
func (vm *VM) builtinValue(canonical, name string) (runtime.Value, bool) {
	fn := vm.natives.LookupKey(native.Key(canonical, name))
	if fn == nil {
		return nil, false
	}
	captured := fn
	return runtime.Function{
		Name: canonical + "." + name,
		Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			return captured(args)
		},
	}, true
}

func (vm *VM) wrapStatefulNativeImport(canonical, name string) (runtime.Value, error) {
	caller := vm.statefulNative
	if caller == nil {
		return nil, fmt.Errorf("from %s import %s: stateful natives unavailable", canonical, name)
	}
	return runtime.Function{
		Name: canonical + "." + name,
		Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			return caller.CallBuiltin(canonical, name, args, nil)
		},
	}, nil
}

func (vm *VM) importModule(instruction Instruction) error {
	if len(instruction.Operands) != 3 {
		return vm.fatalError(instruction, "import module instruction has invalid operands")
	}
	if vm.moduleLoader == nil {
		return vm.runtimeError(instruction, "bytecode module loader is not configured")
	}
	canonical, err := vm.constantStringAt(instruction, instruction.Operands[0], "module name must be string")
	if err != nil {
		return err
	}
	alias, err := vm.constantStringAt(instruction, instruction.Operands[1], "module alias must be string")
	if err != nil {
		return err
	}
	module, err := vm.moduleLoader.LoadModule(canonical, alias)
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	return vm.setGlobal(instruction.Operands[2], module)
}

func (vm *VM) throw(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	errValue, ok := value.(runtime.Error)
	if !ok {
		return 0, vm.runtimeError(instruction, "throw expects Error, got %s", value.TypeName())
	}
	errValue = vm.withErrorStackTrace(errValue, int(instruction.Line))
	vm.pendingThrow = &errValue
	return vm.jumpToExceptionHandler(instruction, ip)
}

func (vm *VM) throwRecoverableError(instruction Instruction, ip int, err error) (int, error) {
	// A vmThrownError carries an original typed throw from another VM; rethrow so catch matches.
	var thrown vmThrownError
	if errors.As(err, &thrown) && !thrown.err.IsFatal() {
		captured := thrown.err
		vm.pendingThrow = &captured
		return vm.jumpToExceptionHandler(instruction, ip)
	}
	// A cross-module fault surfaced through a native call (json.parseAs -> __deserialize) arrives as *vmRuntimeError; rethrow with its clean message, not the rendered blob.
	var rt *vmRuntimeError
	if errors.As(err, &rt) {
		captured := rt.toRuntimeError()
		vm.pendingThrow = &captured
		return vm.jumpToExceptionHandler(instruction, ip)
	}
	errValue := vm.withErrorStackTrace(runtime.NewRecoverableError(err), int(instruction.Line))
	vm.pendingThrow = &errValue
	return vm.jumpToExceptionHandler(instruction, ip)
}

// fatalError wraps a fault as non-catchable so Run() lets it terminate
// the VM rather than routing it to a try/catch. Used for bytecode
// corruption and other conditions where continuing is meaningless.
func (vm *VM) fatalError(instruction Instruction, format string, args ...any) error {
	return vmFatalError{err: vm.runtimeError(instruction, format, args...)}
}

// routeRuntimeFault decides the fate of an error the dispatch loop
// returned. A fatal fault or one with no active handler terminates
// (fatal=true). Otherwise the fault is converted to a catchable
// runtime.Error, installed as the pending throw, and control unwinds to
// the nearest handler; the returned IP is where dispatch resumes. This
// is what makes implicit runtime faults (division by zero, bad index,
// conversion failures) catchable on the VM, matching the evaluator.
func (vm *VM) routeRuntimeFault(loopErr error) (resumeIP int, fatal bool, routeErr error) {
	value, isFatal := vm.faultToRuntimeError(loopErr)
	if isFatal || len(vm.exceptionHandlers) == 0 {
		return 0, true, nil
	}
	vm.pendingThrow = &value
	nextIP, err := vm.jumpToExceptionHandler(Instruction{}, 0)
	if err != nil {
		return 0, false, err
	}
	return nextIP + 1, false, nil
}

// faultToRuntimeError converts a dispatch-loop error into a catchable
// runtime.Error, reporting whether it is fatal (non-catchable).
func (vm *VM) faultToRuntimeError(loopErr error) (runtime.Error, bool) {
	var thrown vmThrownError
	if errors.As(loopErr, &thrown) {
		return thrown.err, thrown.err.IsFatal()
	}
	var fatalErr vmFatalError
	if errors.As(loopErr, &fatalErr) {
		return runtime.Error{}, true
	}
	var rt *vmRuntimeError
	if errors.As(loopErr, &rt) {
		return rt.toRuntimeError(), false
	}
	full := loopErr.Error()
	msg := cleanRuntimeFaultMessage(full)
	// Preserve the stack trace the dispatch error already carries so a
	// caught runtime fault exposes errors.stackTrace like the evaluator.
	trace := ""
	if idx := strings.Index(full, "\n"); idx >= 0 {
		trace = full[idx+1:]
	}
	return runtime.Error{Class: "RuntimeError", Message: msg, Parents: []string{"RuntimeError", "Error"}, StackTrace: trace}, false
}

func cleanRuntimeFaultMessage(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// boundaryFaultToError extracts the clean catchable runtime.Error from a sub-VM boundary fault (typed throw or non-typed *vmRuntimeError) so a boundary handler re-raises the clean error instead of the rendered "uncaught ..." blob.
func boundaryFaultToError(err error) (runtime.Error, bool) {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		return thrown.err, true
	}
	var rt *vmRuntimeError
	if errors.As(err, &rt) {
		return rt.toRuntimeError(), true
	}
	return runtime.Error{}, false
}

func (vm *VM) withErrorStackTrace(err runtime.Error, line int) runtime.Error {
	if !err.HasStackTrace() {
		frames, topLevel := vm.snapshotContractFrames()
		if len(frames) == 0 && topLevel == 0 {
			// Top-level throw: show the failing instruction's line.
			topLevel = line
		}
		err.TraceFrames = frames
		err.ErrorLine = line
		err.TopLevelLine = topLevel
	}
	return err
}

func (vm *VM) throwTyped(instruction Instruction, ip int, class, message string) (int, error) {
	errValue := vm.withErrorStackTrace(runtime.Error{Class: class, Message: message}, int(instruction.Line))
	vm.pendingThrow = &errValue
	return vm.jumpToExceptionHandler(instruction, ip)
}

func (vm *VM) rethrow(instruction Instruction, ip int) (int, error) {
	if vm.pendingThrow == nil {
		return ip, nil
	}
	return vm.jumpToExceptionHandler(instruction, ip)
}

func (vm *VM) jumpToExceptionHandler(instruction Instruction, ip int) (int, error) {
	if len(vm.exceptionHandlers) <= vm.runHandlerBaseline {
		// Run this run's frames' defers before the uncaught throw propagates out (e.g. across a module boundary, where the caller's worker resumes); runDefers leaves vm.frames for the trace.
		for len(vm.defers) > vm.runDeferBaseline {
			if derr := vm.runDefers(instruction); derr != nil {
				return 0, derr
			}
		}
		return 0, vm.uncaughtThrowError(instruction, *vm.pendingThrow)
	}
	handler := vm.exceptionHandlers[len(vm.exceptionHandlers)-1]
	vm.exceptionHandlers = vm.exceptionHandlers[:len(vm.exceptionHandlers)-1]
	for len(vm.frames) > handler.frameDepth {
		if err := vm.runDefers(instruction); err != nil {
			return 0, err
		}
		frame := vm.frames[len(vm.frames)-1]
		vm.frames = vm.frames[:len(vm.frames)-1]
		if !frame.shared && frame.basePointer < len(vm.localsStack) {
			vm.localsStack = vm.localsStack[:frame.basePointer]
		}
	}
	if len(vm.stack) > handler.stackDepth {
		vm.stack = vm.stack[:handler.stackDepth]
	}
	if len(vm.localsStack) > handler.localsStackDepth {
		vm.localsStack = vm.localsStack[:handler.localsStackDepth]
	}
	if len(handler.snapshot) > 0 && handler.snapshotBase+len(handler.snapshot) <= len(vm.localsStack) {
		copy(vm.localsStack[handler.snapshotBase:handler.snapshotBase+len(handler.snapshot)], handler.snapshot)
	}
	if len(vm.frames) > 0 {
		vm.currentFrameBP = vm.frames[len(vm.frames)-1].basePointer
	} else {
		vm.currentFrameBP = 0
	}
	return handler.handlerIP - 1, nil
}

func (vm *VM) catchException(instruction Instruction, ip int) (int, error) {
	if len(instruction.Operands) != 3 {
		return 0, vm.fatalError(instruction, "catch instruction has invalid operands")
	}
	if vm.pendingThrow == nil {
		return ip, nil
	}
	nextIP := int(instruction.Operands[0])
	typeIndex := instruction.Operands[1]
	slot := instruction.Operands[2]
	// Fatal errors are never caught, not even by catch(any); skip this
	// clause so the pending throw propagates to the top.
	if vm.pendingThrow.IsFatal() {
		return nextIP - 1, nil
	}
	if typeIndex >= 0 {
		target, err := vm.constantStringAt(instruction, typeIndex, "catch type must be string")
		if err != nil {
			return 0, err
		}
		if !vm.errorValueMatches(*vm.pendingThrow, target) {
			return nextIP - 1, nil
		}
	}
	caught := *vm.pendingThrow
	vm.pendingThrow = nil
	if slot >= 0 {
		if err := vm.setLocal(slot, caught); err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
	}
	return ip, nil
}

func (vm *VM) binaryNumeric(instruction Instruction, ip int) (int, error) {
	right, err := vm.popVM()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.popVM()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if left.Kind == runtime.VMKindSmallInt && right.Kind == runtime.VMKindSmallInt {
		if err := vm.smallIntBinary(instruction, left.I64, right.I64); err != nil {
			return 0, err
		}
		return ip, nil
	}
	if left.Kind == runtime.VMKindFloat && right.Kind == runtime.VMKindFloat {
		l, _ := left.AsFloat()
		r, _ := right.AsFloat()
		if err := vm.floatBinary(instruction, runtime.Float{Value: l}, runtime.Float{Value: r}); err != nil {
			return 0, err
		}
		return ip, nil
	}
	leftV := left.ToValue()
	rightV := right.ToValue()
	if nextIP, handled, err := vm.callBinaryOperatorMethod(instruction, ip, leftV, rightV); handled || err != nil {
		return nextIP, err
	}
	if err := vm.binaryNumericValues(instruction, leftV, rightV); err != nil {
		return 0, err
	}
	return ip, nil
}

func (vm *VM) binaryNumericValues(instruction Instruction, left runtime.Value, right runtime.Value) error {
	// Fast path: both SmallInt - zero allocation for common integer arithmetic.
	if l, ok := left.(runtime.SmallInt); ok {
		if r, ok := right.(runtime.SmallInt); ok {
			return vm.smallIntBinary(instruction, l.Value, r.Value)
		}
		if r, ok := right.(runtime.Int); ok {
			return vm.intBinary(instruction, runtime.Int{Value: big.NewInt(l.Value)}, r)
		}
		if r, ok := right.(runtime.Decimal); ok {
			return vm.decimalBinary(instruction, native.SmallIntToDecimal(l), r)
		}
		if r, ok := right.(runtime.Float); ok {
			return vm.floatBinary(instruction, intToFloatVal(left), r)
		}
	}
	if l, ok := left.(runtime.Decimal); ok {
		if r, ok := right.(runtime.SmallInt); ok {
			return vm.decimalBinary(instruction, l, native.SmallIntToDecimal(r))
		}
		switch r := right.(type) {
		case runtime.Int:
			return vm.decimalBinary(instruction, l, native.IntToDecimal(r))
		case runtime.Decimal:
			return vm.decimalBinary(instruction, l, r)
		case runtime.Float:
			return vm.decimalFloatArithError(instruction, left, right)
		}
	}
	if l, ok := left.(runtime.Int); ok {
		if r, ok := right.(runtime.SmallInt); ok {
			return vm.intBinary(instruction, l, runtime.Int{Value: big.NewInt(r.Value)})
		}
		switch r := right.(type) {
		case runtime.Decimal:
			return vm.decimalBinary(instruction, native.IntToDecimal(l), r)
		case runtime.Int:
			return vm.intBinary(instruction, l, r)
		case runtime.Float:
			return vm.floatBinary(instruction, intToFloatVal(left), r)
		}
	}
	if l, ok := left.(runtime.Float); ok {
		switch r := right.(type) {
		case runtime.Float:
			return vm.floatBinary(instruction, l, r)
		case runtime.SmallInt, runtime.Int:
			return vm.floatBinary(instruction, l, intToFloatVal(right))
		case runtime.Decimal:
			return vm.decimalFloatArithError(instruction, left, right)
		}
	}
	if isNumericValue(left) && isNumericValue(right) {
		return vm.decimalFloatArithError(instruction, left, right)
	}
	if result, handled, err := native.BinaryOperatorValue(binaryOpSymbol(instruction.Op), left, right); handled {
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		vm.push(result)
		return nil
	}
	return vm.runtimeError(instruction, "%s", native.UnsupportedOperandsError(binaryOpSymbol(instruction.Op), left.TypeName(), right.TypeName()).Error())
}

func intToFloatVal(v runtime.Value) runtime.Float {
	f, _ := runtime.NumericToFloat(v)
	return runtime.Float{Value: f}
}

// decimalFloatArithError reports the precision wall: arithmetic mixing decimal
// and float, which would silently lose decimal exactness. Comparisons are fine.
func (vm *VM) decimalFloatArithError(instruction Instruction, left, right runtime.Value) error {
	return vm.runtimeError(instruction, "cannot mix decimal and float in %s (got %s and %s): cast one side - 'as float' drops decimal exactness, 'as decimal' adopts the float's imprecision", binaryOpSymbol(instruction.Op), left.TypeName(), right.TypeName())
}

// compareJumpIntFallback handles the fused integer compare-and-branch opcodes
// for non-SmallInt operands (e.g., runtime.Int after overflow). The SmallInt
// fast path is inlined directly into the Run() dispatch.
func (vm *VM) compareJumpIntFallback(instruction Instruction, ip int) (int, error) {
	var cmpOp Op
	jumpIfTrue := false
	switch instruction.Op {
	case OpJumpIfNotLessInt:
		cmpOp = OpLess
	case OpJumpIfNotLessEqualInt:
		cmpOp = OpLessEqual
	case OpJumpIfNotGreaterInt:
		cmpOp = OpGreater
	case OpJumpIfNotGreaterEqualInt:
		cmpOp = OpGreaterEqual
	case OpJumpIfNotEqualInt:
		cmpOp = OpEqual
	case OpJumpIfEqualInt:
		cmpOp = OpEqual
		jumpIfTrue = true
	}
	genericOp := instruction
	genericOp.Op = cmpOp
	var nextIP int
	var err error
	if cmpOp == OpEqual {
		nextIP, err = vm.equal(genericOp, ip)
	} else {
		nextIP, err = vm.compare(genericOp, ip)
	}
	if err != nil {
		return ip, err
	}
	value, err := vm.pop()
	if err != nil {
		return ip, vm.callPropagate(instruction, err)
	}
	b, ok := value.(runtime.Bool)
	if !ok {
		return ip, vm.runtimeError(instruction, "compare result must be bool")
	}
	shouldJump := b.Value
	if !jumpIfTrue {
		shouldJump = !b.Value
	}
	if shouldJump {
		return int(instruction.Operands[0]) - 1, nil
	}
	return nextIP, nil
}

// intSelfArith handles the fused self-update integer arithmetic opcodes
// (OpAdd/SubLocalIntLocal, OpAdd/SubLocalIntConst, and Global mirrors). The
// opcode reads the destination slot, applies the right-hand operand, stores
// the result back, and pushes it onto the stack.
func (vm *VM) intSelfArith(instruction Instruction) error {
	if len(instruction.Operands) != 2 {
		return vm.fatalError(instruction, "int self-arith has invalid operands")
	}
	dst := instruction.Operands[0]
	rhsOperand := instruction.Operands[1]
	var lhs runtime.Value
	var err error
	isGlobal := false
	switch instruction.Op {
	case OpAddLocalIntLocal, OpSubLocalIntLocal, OpAddLocalIntConst, OpSubLocalIntConst,
		OpAddLocalIntGlobal, OpSubLocalIntGlobal:
		lhs, err = vm.getLocal(dst)
	case OpAddGlobalIntGlobal, OpSubGlobalIntGlobal, OpAddGlobalIntConst, OpSubGlobalIntConst,
		OpAddGlobalIntLocal, OpSubGlobalIntLocal:
		lhs, err = vm.getGlobal(dst)
		isGlobal = true
	default:
		return vm.runtimeError(instruction, "unknown int self-arith opcode")
	}
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	var rhs runtime.Value
	switch instruction.Op {
	case OpAddLocalIntLocal, OpSubLocalIntLocal,
		OpAddGlobalIntLocal, OpSubGlobalIntLocal:
		rhs, err = vm.getLocal(rhsOperand)
	case OpAddGlobalIntGlobal, OpSubGlobalIntGlobal,
		OpAddLocalIntGlobal, OpSubLocalIntGlobal:
		rhs, err = vm.getGlobal(rhsOperand)
	default:
		rhs = runtime.SmallInt{Value: rhsOperand}
	}
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	subtract := false
	switch instruction.Op {
	case OpSubLocalIntLocal, OpSubLocalIntConst, OpSubGlobalIntGlobal, OpSubGlobalIntConst,
		OpSubGlobalIntLocal, OpSubLocalIntGlobal:
		subtract = true
	}
	result, err := addOrSubInt(lhs, rhs, subtract)
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	if isGlobal {
		if err := vm.setGlobal(dst, result); err != nil {
			return vm.callPropagate(instruction, err)
		}
	} else {
		if err := vm.setLocal(dst, result); err != nil {
			return vm.callPropagate(instruction, err)
		}
	}
	vm.push(result)
	return nil
}

// addOrSubInt computes a+b or a-b with SmallInt fast path and big.Int overflow
// fallback. Returns an error if either operand is not an integer.
func addOrSubInt(a, b runtime.Value, subtract bool) (runtime.Value, error) {
	al, aOK := a.(runtime.SmallInt)
	bl, bOK := b.(runtime.SmallInt)
	if aOK && bOK {
		if subtract {
			v := al.Value - bl.Value
			if (al.Value^bl.Value)&(al.Value^v) < 0 {
				return runtime.Int{Value: new(big.Int).Sub(big.NewInt(al.Value), big.NewInt(bl.Value))}, nil
			}
			return runtime.SmallInt{Value: v}, nil
		}
		v := al.Value + bl.Value
		if (al.Value^v)&(bl.Value^v) < 0 {
			return runtime.Int{Value: new(big.Int).Add(big.NewInt(al.Value), big.NewInt(bl.Value))}, nil
		}
		return runtime.SmallInt{Value: v}, nil
	}
	// Slow path: promote both operands to big.Int.
	aBig, ok := intToBigInt(a)
	if !ok {
		return nil, fmt.Errorf("int arithmetic expects int, got %s", a.TypeName())
	}
	bBig, ok := intToBigInt(b)
	if !ok {
		return nil, fmt.Errorf("int arithmetic expects int, got %s", b.TypeName())
	}
	result := new(big.Int)
	if subtract {
		result.Sub(aBig, bBig)
	} else {
		result.Add(aBig, bBig)
	}
	if result.IsInt64() {
		return runtime.SmallInt{Value: result.Int64()}, nil
	}
	return runtime.Int{Value: result}, nil
}

func intToBigInt(v runtime.Value) (*big.Int, bool) {
	switch x := v.(type) {
	case runtime.SmallInt:
		return big.NewInt(x.Value), true
	case runtime.Int:
		return x.Value, true
	}
	return nil, false
}

func (vm *VM) updateIntSlot(instruction Instruction) error {
	if len(instruction.Operands) != 1 {
		return vm.fatalError(instruction, "integer slot update has invalid operands")
	}
	slot := instruction.Operands[0]
	var old runtime.VMValue
	var err error
	switch instruction.Op {
	case OpIncLocalInt, OpDecLocalInt:
		old, err = vm.getLocalVM(slot)
	case OpIncGlobalInt, OpDecGlobalInt:
		old, err = vm.getGlobalVM(slot)
	default:
		return vm.runtimeError(instruction, "unknown integer slot update opcode")
	}
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	delta := int64(1)
	if instruction.Op == OpDecLocalInt || instruction.Op == OpDecGlobalInt {
		delta = -1
	}
	var next runtime.VMValue
	if old.Kind == runtime.VMKindSmallInt {
		nv := old.I64 + delta
		if (old.I64^nv)&(delta^nv) < 0 {
			next = runtime.VMValueFromValue(runtime.Int{Value: new(big.Int).Add(big.NewInt(old.I64), big.NewInt(delta))})
		} else {
			next = runtime.VMValueSmallInt(nv)
		}
	} else {
		nextVal, err := updatedIntValue(instruction, old.ToValue())
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		next = runtime.VMValueFromValue(nextVal)
	}
	switch instruction.Op {
	case OpIncLocalInt, OpDecLocalInt:
		if err := vm.setLocalVM(slot, next); err != nil {
			return vm.callPropagate(instruction, err)
		}
	case OpIncGlobalInt, OpDecGlobalInt:
		if err := vm.setGlobalVM(slot, next); err != nil {
			return vm.callPropagate(instruction, err)
		}
	}
	vm.pushVM(old)
	return nil
}

func updatedIntValue(instruction Instruction, old runtime.Value) (runtime.Value, error) {
	delta := int64(1)
	if instruction.Op == OpDecLocalInt || instruction.Op == OpDecGlobalInt {
		delta = -1
	}
	switch value := old.(type) {
	case runtime.SmallInt:
		next := value.Value + delta
		if (value.Value^next)&(delta^next) < 0 {
			return runtime.Int{Value: new(big.Int).Add(big.NewInt(value.Value), big.NewInt(delta))}, nil
		}
		return runtime.SmallInt{Value: next}, nil
	case runtime.Int:
		return runtime.Int{Value: new(big.Int).Add(value.Value, big.NewInt(delta))}, nil
	default:
		return nil, fmt.Errorf("integer update expects int, got %s", old.TypeName())
	}
}

func (vm *VM) appendListSlot(instruction Instruction) error {
	if len(instruction.Operands) != 1 {
		return vm.fatalError(instruction, "list append slot update has invalid operands")
	}
	value, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	slot := instruction.Operands[0]
	var current runtime.Value
	switch instruction.Op {
	case OpAppendLocalList:
		current, err = vm.getLocal(slot)
	case OpAppendGlobalList:
		current, err = vm.getGlobal(slot)
	default:
		return vm.runtimeError(instruction, "unknown list append slot update opcode")
	}
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	list, ok := current.(*runtime.List)
	if !ok {
		return vm.runtimeError(instruction, "list append requires list, got %s", current.TypeName())
	}
	if list.Frozen {
		return vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
	}
	if len(list.ElementTypes) > 0 && !vmValueSatisfiesElementTag(value, list.ElementTypes[0]) {
		return vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot push %s to list<%s>", value.TypeName(), list.ElementTypes[0])}
	}
	list.Elements = append(list.Elements, value)
	switch instruction.Op {
	case OpAppendLocalList:
		if err := vm.setLocal(slot, list); err != nil {
			return vm.callPropagate(instruction, err)
		}
	case OpAppendGlobalList:
		if err := vm.setGlobal(slot, list); err != nil {
			return vm.callPropagate(instruction, err)
		}
	}
	vm.push(list)
	return nil
}

// smallIntBinary performs arithmetic on two int64 values, promoting to big.Int on overflow.
func (vm *VM) smallIntBinary(instruction Instruction, l int64, r int64) error {
	switch instruction.Op {
	case OpAdd:
		result := l + r
		// Overflow: same-sign operands produce opposite-sign result.
		if (l^result)&(r^result) < 0 {
			vm.push(runtime.Int{Value: new(big.Int).Add(big.NewInt(l), big.NewInt(r))})
		} else {
			vm.push(runtime.SmallInt{Value: result})
		}
	case OpSub:
		result := l - r
		// Overflow: different-sign operands produce opposite sign from l.
		if (l^r)&(l^result) < 0 {
			vm.push(runtime.Int{Value: new(big.Int).Sub(big.NewInt(l), big.NewInt(r))})
		} else {
			vm.push(runtime.SmallInt{Value: result})
		}
	case OpMul:
		result := l * r
		// Overflow check: result/l != r (when l != 0).
		if l != 0 && result/l != r {
			vm.push(runtime.Int{Value: new(big.Int).Mul(big.NewInt(l), big.NewInt(r))})
		} else {
			vm.push(runtime.SmallInt{Value: result})
		}
	case OpDiv:
		return vm.decimalBinary(instruction, native.SmallIntToDecimal(runtime.SmallInt{Value: l}), native.SmallIntToDecimal(runtime.SmallInt{Value: r}))
	case OpIntDiv:
		if r == 0 {
			return vm.runtimeError(instruction, "integer division by zero")
		}
		// math.MinInt64 / -1 is the only overflow case for int64 division.
		if l == math.MinInt64 && r == -1 {
			vm.push(runtime.Int{Value: new(big.Int).Neg(big.NewInt(l))})
		} else {
			q := l / r
			rem := l - q*r
			if rem != 0 && ((l < 0) != (r < 0)) {
				q--
			}
			vm.push(runtime.SmallInt{Value: q})
		}
	case OpMod:
		if r == 0 {
			return vm.runtimeError(instruction, "modulo by zero")
		}
		m := l % r
		if m != 0 && ((l < 0) != (r < 0)) {
			m += r
		}
		vm.push(runtime.SmallInt{Value: m})
	case OpPow:
		if r < 0 {
			return vm.runtimeError(instruction, "exponent must be a non-negative int64")
		}
		result := new(big.Int).Exp(big.NewInt(l), big.NewInt(r), nil)
		if result.IsInt64() {
			vm.push(runtime.SmallInt{Value: result.Int64()})
		} else {
			vm.push(runtime.Int{Value: result})
		}
	default:
		return vm.runtimeError(instruction, "unsupported operator %s for int values", binaryOpSymbol(instruction.Op))
	}
	return nil
}

func isNumericValue(value runtime.Value) bool {
	switch value.(type) {
	case runtime.SmallInt, runtime.Int, runtime.Decimal, runtime.Float:
		return true
	default:
		return false
	}
}

func binaryOpSymbol(op Op) string {
	switch op {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpDiv:
		return "/"
	case OpIntDiv:
		return "//"
	case OpMod:
		return "%"
	case OpPow:
		return "**"
	default:
		return fmt.Sprintf("opcode %d", op)
	}
}

func (vm *VM) callBinaryOperatorMethod(instruction Instruction, ip int, left runtime.Value, right runtime.Value) (int, bool, error) {
	instance, ok := left.(*runtime.Instance)
	if !ok {
		return ip, false, nil
	}
	methodName, ok := binaryOperatorMethodName(instruction.Op)
	if !ok {
		return ip, false, nil
	}
	if instance.Class.Module != vm.moduleName {
		if len(instance.Class.Methods[strings.ToLower(methodName)]) == 0 {
			return ip, false, nil
		}
		if vm.moduleLoader == nil {
			return 0, true, vm.runtimeError(instruction, "bytecode module loader is not configured")
		}
		result, err := vm.moduleLoader.CallModuleMethod(instance.Class.Module, instance.Class.Name, methodName, instance, vm.wrapStatefulNativeArgs("", "", []runtime.Value{right}), vm)
		if err != nil {
			nextIP, perr := vm.propagateModuleError(instruction, ip, err)
			return nextIP, true, perr
		}
		vm.push(result)
		return ip, true, nil
	}
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return 0, true, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
	}
	indices, ok := vm.lookupMethod(classInfo, methodName)
	if !ok {
		return ip, false, nil
	}
	functionIndex, err := vm.selectRuntimeFunction(instruction, methodName, indices, []runtime.Value{right}, 1)
	if err != nil {
		return 0, true, err
	}
	nextIP, err := vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, right}, nil)
	return nextIP, true, err
}

func binaryOperatorMethodName(op Op) (string, bool) {
	switch op {
	case OpAdd:
		return "__add", true
	case OpSub:
		return "__sub", true
	case OpMul:
		return "__mul", true
	case OpDiv:
		return "__div", true
	case OpIntDiv:
		return "__intdiv", true
	case OpMod:
		return "__mod", true
	case OpPow:
		return "__pow", true
	default:
		return "", false
	}
}

func (vm *VM) intBinary(instruction Instruction, l runtime.Int, r runtime.Int) error {
	switch instruction.Op {
	case OpAdd:
		vm.push(runtime.Int{Value: new(big.Int).Add(l.Value, r.Value)})
	case OpSub:
		vm.push(runtime.Int{Value: new(big.Int).Sub(l.Value, r.Value)})
	case OpMul:
		vm.push(runtime.Int{Value: new(big.Int).Mul(l.Value, r.Value)})
	case OpDiv:
		return vm.decimalBinary(instruction, native.IntToDecimal(l), native.IntToDecimal(r))
	case OpIntDiv:
		if r.Value.Sign() == 0 {
			return vm.runtimeError(instruction, "integer division by zero")
		}
		quotient, _ := intFloorDivMod(l.Value, r.Value)
		vm.push(runtime.Int{Value: quotient})
	case OpMod:
		if r.Value.Sign() == 0 {
			return vm.runtimeError(instruction, "modulo by zero")
		}
		_, remainder := intFloorDivMod(l.Value, r.Value)
		vm.push(runtime.Int{Value: remainder})
	case OpPow:
		if !r.Value.IsInt64() || r.Value.Sign() < 0 {
			return vm.runtimeError(instruction, "exponent must be a non-negative int64")
		}
		vm.push(runtime.Int{Value: new(big.Int).Exp(l.Value, big.NewInt(r.Value.Int64()), nil)})
	default:
		return vm.runtimeError(instruction, "unsupported operator %s for int values", binaryOpSymbol(instruction.Op))
	}
	return nil
}

func (vm *VM) decimalBinary(instruction Instruction, l runtime.Decimal, r runtime.Decimal) error {
	switch instruction.Op {
	case OpAdd:
		vm.push(runtime.Decimal{Value: new(big.Rat).Add(l.Value, r.Value)})
	case OpSub:
		vm.push(runtime.Decimal{Value: new(big.Rat).Sub(l.Value, r.Value)})
	case OpMul:
		vm.push(runtime.Decimal{Value: new(big.Rat).Mul(l.Value, r.Value)})
	case OpDiv:
		if r.Value.Sign() == 0 {
			return vm.runtimeError(instruction, "decimal division by zero")
		}
		vm.push(runtime.Decimal{Value: new(big.Rat).Quo(l.Value, r.Value)})
	case OpIntDiv:
		if r.Value.Sign() == 0 {
			return vm.runtimeError(instruction, "decimal integer division by zero")
		}
		quotient := new(big.Rat).Quo(l.Value, r.Value)
		/* Same-kind result: decimal // decimal stays a decimal
		 * (with an integer numerator). Returning Int here would
		 * silently collapse arbitrary-precision values. */
		vm.push(runtime.Decimal{Value: new(big.Rat).SetInt(ratFloorInt(quotient))})
	case OpMod:
		if r.Value.Sign() == 0 {
			return vm.runtimeError(instruction, "decimal modulo by zero")
		}
		quotient := new(big.Rat).Quo(l.Value, r.Value)
		floor := new(big.Rat).SetInt(ratFloorInt(quotient))
		product := new(big.Rat).Mul(floor, r.Value)
		vm.push(runtime.Decimal{Value: new(big.Rat).Sub(l.Value, product)})
	case OpPow:
		value, err := decimalPow(l.Value, r.Value)
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		vm.push(runtime.Decimal{Value: value})
	default:
		return vm.runtimeError(instruction, "unsupported operator %s for decimal values", binaryOpSymbol(instruction.Op))
	}
	return nil
}

func (vm *VM) floatBinary(instruction Instruction, l runtime.Float, r runtime.Float) error {
	switch instruction.Op {
	case OpAdd:
		vm.push(runtime.Float{Value: l.Value + r.Value})
	case OpSub:
		vm.push(runtime.Float{Value: l.Value - r.Value})
	case OpMul:
		vm.push(runtime.Float{Value: l.Value * r.Value})
	case OpDiv:
		vm.push(runtime.Float{Value: l.Value / r.Value})
	case OpIntDiv:
		if r.Value == 0 {
			return vm.runtimeError(instruction, "float division by zero")
		}
		vm.push(runtime.Float{Value: math.Floor(l.Value / r.Value)})
	case OpMod:
		if r.Value == 0 {
			return vm.runtimeError(instruction, "float modulo by zero")
		}
		/* Floor modulo: sign of the remainder follows the divisor.
		 * Consistent with Geblang's int and decimal modulo, and
		 * with Python's `%` for floats. (Go's `math.Mod` is
		 * truncated modulo, which we deliberately don't expose.) */
		vm.push(runtime.Float{Value: l.Value - math.Floor(l.Value/r.Value)*r.Value})
	case OpPow:
		vm.push(runtime.Float{Value: math.Pow(l.Value, r.Value)})
	default:
		return vm.runtimeError(instruction, "unsupported operator %s for float values", binaryOpSymbol(instruction.Op))
	}
	return nil
}

func ratFloorInt(value *big.Rat) *big.Int {
	quotient := new(big.Int).Quo(value.Num(), value.Denom())
	if value.Sign() < 0 && new(big.Int).Rem(value.Num(), value.Denom()).Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}

func intFloorDivMod(left *big.Int, right *big.Int) (*big.Int, *big.Int) {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(left, right, remainder)
	if remainder.Sign() != 0 && ((left.Sign() < 0) != (right.Sign() < 0)) {
		quotient.Sub(quotient, big.NewInt(1))
		remainder.Add(remainder, right)
	}
	return quotient, remainder
}

func decimalPow(base *big.Rat, exponent *big.Rat) (*big.Rat, error) {
	if exponent.IsInt() {
		if !exponent.Num().IsInt64() {
			return nil, fmt.Errorf("decimal exponent is out of int64 range")
		}
		exp := exponent.Num().Int64()
		if exp == 0 {
			return big.NewRat(1, 1), nil
		}
		result := big.NewRat(1, 1)
		factor := new(big.Rat).Set(base)
		if exp < 0 {
			if factor.Sign() == 0 {
				return nil, fmt.Errorf("zero cannot be raised to a negative exponent")
			}
			factor.Inv(factor)
			exp = -exp
		}
		for exp > 0 {
			if exp&1 == 1 {
				result.Mul(result, factor)
			}
			factor.Mul(factor, factor)
			exp >>= 1
		}
		return result, nil
	}
	baseFloat, _ := base.Float64()
	exponentFloat, _ := exponent.Float64()
	result := math.Pow(baseFloat, exponentFloat)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil, fmt.Errorf("decimal exponent result is not finite")
	}
	decimal, err := runtime.NewDecimalLiteral(strconv.FormatFloat(result, 'g', -1, 64))
	if err != nil {
		return nil, err
	}
	return decimal.Value, nil
}

func (vm *VM) add(instruction Instruction, ip int) (int, error) {
	right, err := vm.popVM()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.popVM()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if left.Kind == runtime.VMKindSmallInt && right.Kind == runtime.VMKindSmallInt {
		result := left.I64 + right.I64
		if (left.I64^result)&(right.I64^result) >= 0 {
			vm.pushVM(runtime.VMValueSmallInt(result))
			return ip, nil
		}
		vm.pushVM(runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: runtime.Int{Value: new(big.Int).Add(big.NewInt(left.I64), big.NewInt(right.I64))}})
		return ip, nil
	}
	if left.Kind == runtime.VMKindFloat && right.Kind == runtime.VMKindFloat {
		l, _ := left.AsFloat()
		r, _ := right.AsFloat()
		vm.pushVM(runtime.VMValueFloat(l + r))
		return ip, nil
	}
	leftV := left.ToValue()
	rightV := right.ToValue()
	// String fast path: skip the method-dispatch detour because the
	// built-in `string` has no __add__ overload.
	if l, ok := leftV.(runtime.String); ok {
		if r, ok := rightV.(runtime.String); ok {
			vm.push(runtime.String{Value: l.Value + r.Value})
			return ip, nil
		}
	}
	if nextIP, handled, err := vm.callBinaryOperatorMethod(instruction, ip, leftV, rightV); handled || err != nil {
		return nextIP, err
	}
	if l, ok := leftV.(runtime.String); ok {
		r, ok := rightV.(runtime.String)
		if !ok {
			return 0, vm.runtimeError(instruction, "right operand must be string")
		}
		vm.push(runtime.String{Value: l.Value + r.Value})
		return ip, nil
	}
	if err := vm.binaryNumericValues(instruction, leftV, rightV); err != nil {
		return 0, err
	}
	return ip, nil
}

// contains implements `needle in container` for the OpContains opcode.
func (vm *VM) contains(needle, container runtime.Value) (runtime.Value, error) {
	switch c := container.(type) {
	case *runtime.List:
		for _, el := range c.Elements {
			if valuesEqual(needle, el) {
				return runtime.Bool{Value: true}, nil
			}
		}
		return runtime.Bool{Value: false}, nil
	case runtime.Dict:
		_, ok := c.GetEntry(dictKeyFor(needle))
		return runtime.Bool{Value: ok}, nil
	case runtime.Set:
		_, ok := c.Elements[dictKeyFor(needle)]
		return runtime.Bool{Value: ok}, nil
	case runtime.String:
		s, ok := needle.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("in: left operand must be a string when the right operand is a string")
		}
		return runtime.Bool{Value: strings.Contains(c.Value, s.Value)}, nil
	case runtime.Range:
		n, ok := native.IntValueToBigInt(needle)
		if !ok {
			return runtime.Bool{Value: false}, nil
		}
		return runtime.Bool{Value: c.ContainsInt(n)}, nil
	case *runtime.Instance:
		if vm.hasInstanceMethod(c, "__contains") {
			return vm.CallMethod(c, "__contains", []runtime.Value{needle})
		}
		return nil, fmt.Errorf("%s does not support 'in' (define __contains)", c.TypeName())
	default:
		return nil, fmt.Errorf("'in' requires a list, dict, set, string, range, or an object with __contains, got %s", container.TypeName())
	}
}

func (vm *VM) index(instruction Instruction) error {
	index, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	switch value := left.(type) {
	case *runtime.List:
		i, err := indexInt(index)
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		if i < 0 {
			i = len(value.Elements) + i
		}
		if i < 0 || i >= len(value.Elements) {
			return vm.runtimeError(instruction, "list index out of range")
		}
		vm.push(value.Elements[i])
	case runtime.String:
		i, err := indexInt(index)
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		ri := runtime.StringRuneInfo(value.Value)
		n := ri.RuneCount(value.Value)
		if i < 0 {
			i = n + i
		}
		if i < 0 || i >= n {
			return vm.runtimeError(instruction, "string index out of range")
		}
		vm.push(runtime.String{Value: ri.RuneAt(value.Value, i)})
	case runtime.Bytes:
		i, err := indexInt(index)
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		if i < 0 {
			i = len(value.Value) + i
		}
		if i < 0 || i >= len(value.Value) {
			return vm.runtimeError(instruction, "bytes index out of range")
		}
		vm.push(runtime.NewInt64(int64(value.Value[i])))
	case runtime.Dict:
		entry, ok := value.GetEntry(dictKeyFor(index))
		if !ok {
			vm.push(runtime.Null{})
			return nil
		}
		vm.push(entry.Value)
	case *runtime.Instance:
		if vm.hasInstanceMethod(value, "__index") {
			result, err := vm.CallMethod(value, "__index", []runtime.Value{index})
			if err != nil {
				return vm.callPropagate(instruction, err)
			}
			vm.push(result)
			return nil
		}
		return vm.runtimeError(instruction, "%s is not indexable", left.TypeName())
	default:
		return vm.runtimeError(instruction, "%s is not indexable", left.TypeName())
	}
	return nil
}

func (vm *VM) setIndex(instruction Instruction, ip int) (int, error) {
	newValue, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	index, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	switch value := left.(type) {
	case *runtime.List:
		if value.Frozen {
			return vm.throwTyped(instruction, ip, "ImmutableError", "cannot modify frozen list")
		}
		if len(value.ElementTypes) > 0 && !vmValueSatisfiesElementTag(newValue, value.ElementTypes[0]) {
			return vm.throwTyped(instruction, ip, "TypeError", fmt.Sprintf("cannot assign %s to list<%s>", newValue.TypeName(), value.ElementTypes[0]))
		}
		i, err := indexInt(index)
		if err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
		if i < 0 {
			i = len(value.Elements) + i
		}
		if i < 0 || i >= len(value.Elements) {
			return 0, vm.runtimeError(instruction, "list index out of range")
		}
		value.Elements[i] = newValue
	case runtime.Dict:
		if value.Frozen {
			return vm.throwTyped(instruction, ip, "ImmutableError", "cannot modify frozen dict")
		}
		if err := vmCheckDictWriteTags(value, index, newValue); err != nil {
			if typed, ok := err.(vmTypedError); ok {
				return vm.throwTyped(instruction, ip, typed.class, typed.message)
			}
			return 0, vm.callPropagate(instruction, err)
		}
		value.PutEntry(dictKeyFor(index), runtime.DictEntry{Key: index, Value: newValue})
	case *runtime.Instance:
		if vm.hasInstanceMethod(value, "__setIndex") {
			if _, err := vm.CallMethod(value, "__setIndex", []runtime.Value{index, newValue}); err != nil {
				return 0, vm.callPropagate(instruction, err)
			}
			vm.push(newValue)
			return ip, nil
		}
		return 0, vm.runtimeError(instruction, "%s does not support index assignment", left.TypeName())
	default:
		return 0, vm.runtimeError(instruction, "%s does not support index assignment", left.TypeName())
	}
	vm.push(newValue)
	return ip, nil
}

func (vm *VM) slice(instruction Instruction) error {
	stepValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	endValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	startValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	exclusive := instruction.Operands[0] != 0
	switch value := left.(type) {
	case *runtime.List:
		indices, err := sliceIndices(startValue, endValue, stepValue, exclusive, len(value.Elements))
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		elements := make([]runtime.Value, len(indices))
		for i, idx := range indices {
			elements[i] = value.Elements[idx]
		}
		vm.push(&runtime.List{Elements: elements})
	case runtime.String:
		runes := []rune(value.Value)
		indices, err := sliceIndices(startValue, endValue, stepValue, exclusive, len(runes))
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		out := make([]rune, len(indices))
		for i, idx := range indices {
			out[i] = runes[idx]
		}
		vm.push(runtime.String{Value: string(out)})
	case runtime.Bytes:
		indices, err := sliceIndices(startValue, endValue, stepValue, exclusive, len(value.Value))
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		out := make([]byte, len(indices))
		for i, idx := range indices {
			out[i] = value.Value[idx]
		}
		vm.push(runtime.Bytes{Value: out})
	default:
		return vm.runtimeError(instruction, "%s is not sliceable", left.TypeName())
	}
	return nil
}

// sliceIndices computes the list of original indices a slice expression
// produces. Honours Python-style step (including negative) when stepValue
// is non-null; otherwise falls back to sliceBounds for the common
// contiguous case. Returns the indices in iteration order.
func sliceIndices(startValue runtime.Value, endValue runtime.Value, stepValue runtime.Value, exclusive bool, length int) ([]int, error) {
	step := 1
	if _, ok := stepValue.(runtime.Null); !ok {
		s, err := indexInt(stepValue)
		if err != nil {
			return nil, err
		}
		step = s
	}
	if step == 0 {
		return nil, fmt.Errorf("slice step cannot be zero")
	}
	if step == 1 {
		start, end, err := sliceBounds(startValue, endValue, exclusive, length)
		if err != nil {
			return nil, err
		}
		out := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, i)
		}
		return out, nil
	}
	// Python-style start/stop defaults depend on step's sign.
	var start, stop int
	if step > 0 {
		start = 0
		stop = length
	} else {
		start = length - 1
		stop = -1
	}
	if _, ok := startValue.(runtime.Null); !ok {
		i, err := indexInt(startValue)
		if err != nil {
			return nil, err
		}
		if i < 0 {
			i += length
		}
		if step > 0 {
			if i < 0 {
				i = 0
			}
			if i > length {
				i = length
			}
		} else {
			if i < -1 {
				i = -1
			}
			if i > length-1 {
				i = length - 1
			}
		}
		start = i
	}
	if _, ok := endValue.(runtime.Null); !ok {
		i, err := indexInt(endValue)
		if err != nil {
			return nil, err
		}
		if !exclusive {
			if step > 0 {
				i++
			} else {
				i--
			}
		}
		if i < 0 {
			i += length
		}
		if step > 0 {
			if i < 0 {
				i = 0
			}
			if i > length {
				i = length
			}
		} else {
			if i < -1 {
				i = -1
			}
			if i > length-1 {
				i = length - 1
			}
		}
		stop = i
	}
	out := []int{}
	if step > 0 {
		for i := start; i < stop; i += step {
			out = append(out, i)
		}
	} else {
		for i := start; i > stop; i += step {
			out = append(out, i)
		}
	}
	return out, nil
}

func sliceBounds(startValue runtime.Value, endValue runtime.Value, exclusive bool, length int) (int, int, error) {
	start := 0
	if _, ok := startValue.(runtime.Null); !ok {
		i, err := indexInt(startValue)
		if err != nil {
			return 0, 0, err
		}
		start = i
	}
	end := length
	if _, ok := endValue.(runtime.Null); !ok {
		i, err := indexInt(endValue)
		if err != nil {
			return 0, 0, err
		}
		end = i
		if !exclusive {
			end++
		}
	}
	if start < 0 {
		start = length + start
	}
	if end < 0 {
		end = length + end
	}
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > length {
		start = length
	}
	if end > length {
		end = length
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

func (vm *VM) iterInit(instruction Instruction) error {
	value, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	iter, err := vm.iteratorFor(instruction, value)
	if err != nil {
		return err
	}
	vm.push(iter)
	return nil
}

// iteratorFor turns any iterable value into an iteratorValue. List /
// Generator / Range take the existing fast paths; user-class
// instances dispatch __iter() (or use __next/__done directly when
// __iter is absent) and the result is itself fed back into this
// function so a Stream-style class can return a Generator, another
// Instance, a List, etc.
func (vm *VM) iteratorFor(instruction Instruction, value runtime.Value) (*iteratorValue, error) {
	switch v := value.(type) {
	case *runtime.List:
		values := append([]runtime.Value(nil), v.Elements...)
		return &iteratorValue{values: values}, nil
	case *runtime.Generator:
		return &iteratorValue{generator: v}, nil
	case runtime.Range:
		return newRangeIterator(v), nil
	case runtime.Dict:
		return &iteratorValue{values: runtime.DictPairs(v)}, nil
	case runtime.Set:
		return &iteratorValue{values: orderedSetValues(v)}, nil
	case runtime.String:
		return &iteratorValue{values: runtime.StringChars(v)}, nil
	case *runtime.Instance:
		return vm.userInstanceIterator(instruction, v)
	}
	return nil, vm.runtimeError(instruction, "%s is not iterable", value.TypeName())
}

// userInstanceIterator resolves the iterator for a *runtime.Instance.
// Calls obj.__iter() when defined and recursively iteratorFor's the
// result. Falls back to the instance-as-iterator path when only
// __next is defined.
func (vm *VM) userInstanceIterator(instruction Instruction, instance *runtime.Instance) (*iteratorValue, error) {
	if instance == nil || instance.Class == nil {
		return nil, vm.runtimeError(instruction, "cannot iterate uninitialised instance")
	}
	hasIter, hasNext := vm.instanceIteratorHooks(instance)
	if hasIter {
		result, err := vm.CallMethod(instance, "__iter", nil)
		if err != nil {
			return nil, vm.runtimeError(instruction, "%s.__iter: %s", instance.Class.Name, err.Error())
		}
		// If __iter returns the same instance and the instance is its
		// own iterator (has __next/__done), break the recursion.
		if inst, ok := result.(*runtime.Instance); ok && inst == instance {
			if hasNext {
				return &iteratorValue{userIter: instance}, nil
			}
		}
		return vm.iteratorFor(instruction, result)
	}
	if hasNext {
		return &iteratorValue{userIter: instance}, nil
	}
	return nil, vm.runtimeError(instruction, "%s is not iterable: define __iter() or __next()/__done()", instance.Class.Name)
}

// instanceIteratorHooks reports whether an instance's class exposes
// __iter and __next. Falls back to the runtime.Class method table for
// instances whose ClassInfo lives in a different chunk (e.g. a class
// defined in an imported stdlib module).
func (vm *VM) instanceIteratorHooks(instance *runtime.Instance) (hasIter, hasNext bool) {
	if instance.Class == nil {
		return false, false
	}
	if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
		_, hasIter = vm.lookupMethod(classInfo, "__iter")
		_, hasNext = vm.lookupMethod(classInfo, "__next")
		if !hasIter || !hasNext {
			// A dunder inherited from a cross-module ancestor is invisible to this-chunk lookupMethod; resolve it via the runtime ancestor chain.
			if _, _, mok := vm.crossModuleBoundary(classInfo); mok {
				hasIter = hasIter || classHasMethod(instance.Class, "__iter")
				hasNext = hasNext || classHasMethod(instance.Class, "__next")
			}
		}
		return hasIter, hasNext
	}
	return classHasMethod(instance.Class, "__iter"), classHasMethod(instance.Class, "__next")
}

func (vm *VM) iterNext(instruction Instruction) (bool, error) {
	if len(instruction.Operands) != 3 {
		return false, vm.fatalError(instruction, "iterator instruction has invalid operands")
	}
	iterSlot := instruction.Operands[1]
	valueSlot := instruction.Operands[2]
	value, err := vm.getLocal(iterSlot)
	if err != nil {
		return false, vm.callPropagate(instruction, err)
	}
	iter, ok := value.(*iteratorValue)
	if !ok {
		return false, vm.runtimeError(instruction, "local is not an iterator")
	}
	if iter.userIter != nil {
		next, ok, err := vm.advanceUserIterator(instruction, iter.userIter)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if err := vm.setLocal(valueSlot, next); err != nil {
			return false, vm.callPropagate(instruction, err)
		}
		return true, nil
	}
	next, ok, err := iter.next()
	if err != nil {
		return false, vm.raiseIteratorFault(instruction, err)
	}
	if !ok {
		return false, nil
	}
	if err := vm.setLocal(valueSlot, next); err != nil {
		return false, vm.callPropagate(instruction, err)
	}
	return true, nil
}

// raiseIteratorFault re-raises an iterator/generator error without
// flattening it to a string, so a typed throw inside a generator body
// keeps its class for catch dispatch in the consuming loop.
func (vm *VM) raiseIteratorFault(instruction Instruction, err error) error {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		return vm.uncaughtThrowError(instruction, thrown.err)
	}
	return vm.callPropagate(instruction, err)
}

// advanceUserIterator drives one step of a user-defined iterator
// (Instance with __next()/__done() methods). Returns (value, true)
// for an item, (nil, false) when the iterator reports done.
//
// CallMethod errors are returned unchanged so the OpIterNext
// dispatch site can detect a wrapped vmThrownError (from a method
// defined in another module) and forward it to the calling VM's
// pendingThrow via propagateModuleError. Wrapping with runtimeError
// here would collapse the chain to a string and silently bypass any
// try / catch the user code had installed.
func (vm *VM) advanceUserIterator(instruction Instruction, iter *runtime.Instance) (runtime.Value, bool, error) {
	if iter == nil || iter.Class == nil {
		return nil, false, vm.runtimeError(instruction, "user iterator has no class")
	}
	isForeign := iter.Class.Module != vm.moduleName
	hasDone, hasNext := vm.userIteratorMethodPresence(iter, isForeign)
	if !hasDone && !hasNext {
		return nil, false, vm.runtimeError(instruction, "%s is not an iterator", iter.Class.Name)
	}
	if hasDone {
		doneResult, err := vm.CallMethod(iter, "__done", nil)
		if err != nil {
			return nil, false, err
		}
		doneBool, ok := doneResult.(runtime.Bool)
		if !ok {
			return nil, false, vm.runtimeError(instruction, "%s.__done must return bool, got %s", iter.Class.Name, doneResult.TypeName())
		}
		if doneBool.Value {
			return nil, false, nil
		}
	}
	if !hasNext {
		return nil, false, vm.runtimeError(instruction, "%s is not an iterator: define __next()", iter.Class.Name)
	}
	value, err := vm.CallMethod(iter, "__next", nil)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// userIteratorMethodPresence reports whether the iterator instance
// exposes __done / __next. Foreign classes are checked via the
// trampoline table on iter.Class.Methods (the module loader
// populates it when the class is imported); local classes go
// through the running chunk's classInfo so overload selection and
// inheritance work the same way they do for direct method calls.
func (vm *VM) userIteratorMethodPresence(iter *runtime.Instance, isForeign bool) (bool, bool) {
	if isForeign {
		return classHasMethod(iter.Class, "__done"), classHasMethod(iter.Class, "__next")
	}
	classInfo, ok := vm.classInfo(iter.Class.Name)
	if !ok {
		return false, false
	}
	_, hasDone := vm.lookupMethod(classInfo, "__done")
	_, hasNext := vm.lookupMethod(classInfo, "__next")
	if !hasDone || !hasNext {
		// A dunder inherited from a cross-module ancestor is invisible to this-chunk lookupMethod; resolve it via the runtime ancestor chain.
		if _, _, mok := vm.crossModuleBoundary(classInfo); mok {
			hasDone = hasDone || classHasMethod(iter.Class, "__done")
			hasNext = hasNext || classHasMethod(iter.Class, "__next")
		}
	}
	return hasDone, hasNext
}

// callContextDunder invokes the first present of names (a context-manager __enter/__exit pair) on instance, resolving same-chunk, cross-module-direct, and cross-module-inherited. called reports whether a dunder ran; a probe miss for a name is absence, while a fault inside a found body propagates via err.
func (vm *VM) callContextDunder(instance *runtime.Instance, names [2]string) (result runtime.Value, called bool, err error) {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		if vm.moduleLoader == nil || instance.Class.Module == vm.moduleName {
			return nil, false, nil
		}
		for _, name := range names {
			r, cerr := vm.moduleLoader.CallModuleMethod(instance.Class.Module, instance.Class.Name, name, instance, nil, vm)
			var notFound *runtime.MethodNotFoundError
			if errors.As(cerr, &notFound) {
				continue
			}
			return r, cerr == nil, cerr
		}
		return nil, false, nil
	}
	if indices, name, found := vm.lookupDunder(classInfo, names[0], names[1]); found && len(indices) > 0 {
		r, cerr := vm.CallMethod(instance, name, nil)
		return r, cerr == nil, cerr
	}
	// A dunder may be inherited from a cross-module ancestor, which lookupDunder (this chunk only) misses; resolve via CallMethod and treat not-found as absence.
	if _, _, mok := vm.crossModuleBoundary(classInfo); mok {
		for _, name := range names {
			r, cerr := vm.CallMethod(instance, name, nil)
			var notFound *runtime.MethodNotFoundError
			if errors.As(cerr, &notFound) {
				continue
			}
			return r, cerr == nil, cerr
		}
	}
	return nil, false, nil
}

// withEnter runs the resource's __enter__ and pushes its result, or pushes the resource itself when there is no context manager.
func (vm *VM) withEnter(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	instance, ok := value.(*runtime.Instance)
	if !ok {
		vm.push(value)
		return ip, nil
	}
	result, called, err := vm.callContextDunder(instance, [2]string{"__enter", "__enter__"})
	if err != nil {
		return vm.propagateModuleError(instruction, ip, err)
	}
	if called {
		vm.push(result)
	} else {
		vm.push(value)
	}
	return ip, nil
}

// withExit runs the resource's __exit__ when present; otherwise a no-op (destructors are end-of-lifetime hooks, not block-scoped cleanup).
func (vm *VM) withExit(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return ip, nil
	}
	if _, _, err := vm.callContextDunder(instance, [2]string{"__exit", "__exit__"}); err != nil {
		return vm.propagateModuleError(instruction, ip, err)
	}
	return ip, nil
}

// execDel implements OpDel. Looks up the binding, fires the
// destructor if the value is a class instance whose class
// declares one and hasn't already been destroyed, unregisters
// the instance from the destructible-instance list, and clears
// the slot to Null{}.
func (vm *VM) execDel(instruction Instruction) error {
	if len(instruction.Operands) != 2 {
		return vm.fatalError(instruction, "del instruction has invalid operands")
	}
	slot := instruction.Operands[0]
	kind := instruction.Operands[1]
	var value runtime.Value
	switch kind {
	case 0:
		v, err := vm.getLocal(slot)
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		value = v
	case 1:
		if slot < 0 || int(slot) >= len(vm.curGlobals) {
			return vm.runtimeError(instruction, "del: global slot out of range")
		}
		value = vm.curGlobals[slot].ToValue()
	default:
		return vm.runtimeError(instruction, "del: unknown binding kind %d", kind)
	}
	if instance, ok := value.(*runtime.Instance); ok && instance != nil && !instance.Destroyed && instance.Class != nil {
		if instance.Class.Module != vm.moduleName && vm.moduleLoader != nil {
			instance.Destroyed = true
			vm.removeDestructible(instance)
			if err := vm.moduleLoader.CallModuleDestructor(instance.Class.Module, instance.Class.Name, instance, vm); err != nil {
				if fault, ok := boundaryFaultToError(err); ok {
					return vm.uncaughtThrowError(instruction, vm.mergeBoundaryFrames(fault, int(instruction.Line)))
				}
				return vm.runtimeError(instruction, "del: destructor: %s", err.Error())
			}
		} else if classInfo, ok := vm.classInfo(instance.Class.Name); ok && classInfo.DestructorIndex >= 0 {
			instance.Destroyed = true
			vm.removeDestructible(instance)
			if _, err := vm.CallFunctionRaw(classInfo.DestructorIndex, []runtime.Value{instance}); err != nil {
				if fault, ok := boundaryFaultToError(err); ok {
					return vm.uncaughtThrowError(instruction, vm.mergeBoundaryFrames(fault, int(instruction.Line)))
				}
				return vm.runtimeError(instruction, "del: destructor: %s", err.Error())
			}
		}
	}
	switch kind {
	case 0:
		if err := vm.setLocal(slot, runtime.Null{}); err != nil {
			return vm.callPropagate(instruction, err)
		}
	case 1:
		vm.curGlobals[slot] = runtime.VMValueNull
	}
	return nil
}

func (vm *VM) iterClose(instruction Instruction) error {
	if len(instruction.Operands) != 1 {
		return vm.fatalError(instruction, "iterator close instruction has invalid operands")
	}
	value, err := vm.getLocal(instruction.Operands[0])
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	iter, ok := value.(*iteratorValue)
	if !ok || iter.generator == nil {
		return nil
	}
	iter.generator.Close()
	return nil
}

// typeAssert checks the top-of-stack value against the type string stored in the constant pool
// at operand[0]. Leaves the value on the stack. Throws a runtime error on mismatch.
func (vm *VM) typeAssert(instruction Instruction) error {
	if len(instruction.Operands) != 1 {
		return vm.fatalError(instruction, "type assert instruction has invalid operands")
	}
	constIdx := instruction.Operands[0]
	if constIdx < 0 || int(constIdx) >= vm.constantsLen() {
		return vm.runtimeError(instruction, "type assert: constant index out of range")
	}
	typeStr, ok := vm.constantValue(constIdx).(runtime.String)
	if !ok {
		return vm.runtimeError(instruction, "type assert: expected string constant")
	}
	value, err := vm.peek()
	if err != nil {
		return vm.runtimeError(instruction, "type assert: %s", err.Error())
	}
	spec, ok := vm.typeAssertSpecs[constIdx]
	if !ok {
		spec = vm.typeSpec(typeStr.Value)
	}
	if !vm.matchValueToTypeSpec(nil, value, spec) {
		suffix := vm.collectionMismatchSuffixStr(value, typeStr.Value)
		gotName := vm.descriptiveRuntimeTypeName(value)
		if suffix != "" {
			gotName = value.TypeName()
		}
		return vm.runtimeError(instruction, "type error: cannot assign %s to %s%s", gotName, typeStr.Value, suffix)
	}
	// Attach the reified element-type tag so reflect.typeBindings() and
	// `instanceof list<T>` see the declared bindings on the tagged value.
	// Mutate the top of the stack in place: List/Dict/Set are value
	// types, so we pop, tag the copy, and push the tagged version.
	if tagged, ok := vm.tagCollectionWithSpec(value, spec); ok {
		if _, err := vm.pop(); err != nil {
			return vm.callPropagate(instruction, err)
		}
		vm.push(tagged)
	}
	return nil
}

// tagCollectionWithSpec mirrors the evaluator's tagCollectionWithTypeRef
// for the VM's vmTypeSpec form. Returns (tagged, true) when the value is
// a List/Dict/Set whose tag should be set; otherwise (value, false).
func (vm *VM) tagCollectionWithSpec(value runtime.Value, spec vmTypeSpec) (runtime.Value, bool) {
	switch v := value.(type) {
	case *runtime.List:
		if len(spec.args) >= 1 {
			tag := make([]string, len(spec.args))
			for i, a := range spec.args {
				tag[i] = elementSpecTag(a)
			}
			v.ElementTypes = tag
			return v, true
		}
	case runtime.Set:
		if len(spec.args) >= 1 {
			v.ElementTypes = []string{elementSpecTag(spec.args[0])}
			return v, true
		}
	case runtime.Dict:
		if len(spec.args) >= 2 {
			v.ElementTypes = []string{elementSpecTag(spec.args[0]), elementSpecTag(spec.args[1])}
			return v, true
		}
	}
	return value, false
}

// execRange implements OpRange: pops 2 or 3 integer values and pushes a
// list<int> covering the inclusive range. With 3 args the step is
// explicit; with 2 args the step defaults to +1 (or -1 when start > end).
func (vm *VM) executeSelect(instruction Instruction, ip int) (int, error) {
	ops := instruction.Operands
	if len(ops) < 2 {
		return 0, vm.runtimeError(instruction, "select: invalid operands")
	}
	numCases := int(ops[0])
	hasDefault := ops[1] == 1
	expected := 2 + numCases*3
	if hasDefault {
		expected++
	}
	if len(ops) != expected {
		return 0, vm.runtimeError(instruction, "select: operand count mismatch")
	}
	kinds := make([]string, numCases)
	bodyOffsets := make([]int, numCases)
	bindingSlots := make([]int64, numCases)
	for i := 0; i < numCases; i++ {
		base := 2 + i*3
		if ops[base] == 1 {
			kinds[i] = "send"
		} else {
			kinds[i] = "recv"
		}
		bodyOffsets[i] = int(ops[base+1])
		bindingSlots[i] = ops[base+2]
	}
	defaultOffset := -1
	if hasDefault {
		defaultOffset = int(ops[expected-1])
	}
	handles := make([]*native.ChannelHandle, numCases)
	sendValues := make([]runtime.Value, numCases)
	// Stack layout (top to bottom): for the LAST case first - handle (and
	// value if send). Pop in reverse order to keep parallel arrays.
	for i := numCases - 1; i >= 0; i-- {
		if kinds[i] == "send" {
			v, err := vm.pop()
			if err != nil {
				return 0, vm.callPropagate(instruction, err)
			}
			sendValues[i] = v
		}
		hVal, err := vm.pop()
		if err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
		h, ok := native.ChannelHandleFromValue(hVal)
		if !ok {
			return 0, vm.runtimeError(instruction, "select case channel handle is invalid")
		}
		handles[i] = h
	}
	chosen, recvValue, err := native.SelectChannels(handles, kinds, sendValues, hasDefault)
	if err != nil {
		return vm.throwTyped(instruction, ip, "RuntimeError", "select: "+err.Error())
	}
	if chosen == -1 {
		return defaultOffset - 1, nil
	}
	if kinds[chosen] == "recv" && bindingSlots[chosen] >= 0 {
		if err := vm.setLocal(bindingSlots[chosen], recvValue); err != nil {
			return 0, vm.callPropagate(instruction, err)
		}
	}
	return bodyOffsets[chosen] - 1, nil
}

func (vm *VM) execRange(instruction Instruction, exclusive bool) error {
	if len(instruction.Operands) != 1 {
		return vm.runtimeError(instruction, "range expects argument count operand")
	}
	argc := int(instruction.Operands[0])
	minArgc := 2
	if exclusive {
		minArgc = 1
	}
	if argc < minArgc || argc > 3 {
		return vm.runtimeError(instruction, "range expects (start, end) or (start, end, step)")
	}
	var step *big.Int
	if argc == 3 {
		stepVal, err := vm.pop()
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		s, ok := native.IntValueToBigInt(stepVal)
		if !ok {
			return vm.runtimeError(instruction, "range step must be int")
		}
		step = s
	}
	endVal, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	endBig, ok := native.IntValueToBigInt(endVal)
	if !ok {
		return vm.runtimeError(instruction, "range end must be int")
	}
	// One-arg zrange(n) ranges from 0; otherwise the start is on the stack.
	startBig := big.NewInt(0)
	if argc >= 2 {
		startVal, err := vm.pop()
		if err != nil {
			return vm.callPropagate(instruction, err)
		}
		s, ok := native.IntValueToBigInt(startVal)
		if !ok {
			return vm.runtimeError(instruction, "range start must be int")
		}
		startBig = s
	}
	if step == nil {
		if startBig.Cmp(endBig) > 0 {
			step = big.NewInt(-1)
		} else {
			step = big.NewInt(1)
		}
	}
	if step.Sign() == 0 {
		return vm.runtimeError(instruction, "range step cannot be zero")
	}
	var elements []runtime.Value
	current := new(big.Int).Set(startBig)
	for {
		cmp := current.Cmp(endBig)
		if step.Sign() > 0 {
			if (exclusive && cmp >= 0) || (!exclusive && cmp > 0) {
				break
			}
		} else {
			if (exclusive && cmp <= 0) || (!exclusive && cmp < 0) {
				break
			}
		}
		elements = append(elements, runtime.Int{Value: new(big.Int).Set(current)})
		current.Add(current, step)
	}
	vm.push(&runtime.List{Elements: elements})
	return nil
}

func newRangeIterator(value runtime.Range) *iteratorValue {
	return &iteratorValue{rangeIter: &rangeIterator{
		current:   new(big.Int).Set(value.Start),
		end:       new(big.Int).Set(value.End),
		step:      new(big.Int).Set(value.Step),
		exclusive: value.Exclusive,
	}}
}

func (iter *iteratorValue) next() (runtime.Value, bool, error) {
	if iter.generator != nil {
		return iter.generator.Next()
	}
	if iter.rangeIter != nil {
		value, ok := iter.rangeIter.next()
		return value, ok, nil
	}
	if iter.userIter != nil {
		return nil, false, errUserIterPlaceholder
	}
	if iter.index >= len(iter.values) {
		return nil, false, nil
	}
	value := iter.values[iter.index]
	iter.index++
	return value, true, nil
}

// errUserIterPlaceholder signals that the iterator backs a user
// *Instance and must be driven via the VM (which holds the call
// dispatcher). Callers that observe this error read userIter and
// dispatch __done()/__next() themselves.
var errUserIterPlaceholder = errors.New("user iterator must be advanced via VM")

func (iter *rangeIterator) next() (runtime.Value, bool) {
	if !rangeContains(iter.current, iter.end, iter.step, iter.exclusive) {
		return nil, false
	}
	value := runtime.Int{Value: new(big.Int).Set(iter.current)}
	iter.current.Add(iter.current, iter.step)
	return value, true
}

func rangeContains(current *big.Int, end *big.Int, step *big.Int, exclusive bool) bool {
	cmp := current.Cmp(end)
	if step.Sign() > 0 {
		if exclusive {
			return cmp < 0
		}
		return cmp <= 0
	}
	if exclusive {
		return cmp > 0
	}
	return cmp >= 0
}

func (vm *VM) unpackList(instruction Instruction) error {
	if len(instruction.Operands) < 2 {
		return vm.fatalError(instruction, "unpack instruction has invalid operands")
	}
	value, err := vm.getLocal(instruction.Operands[0])
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	list, ok := value.(*runtime.List)
	if !ok {
		return vm.runtimeError(instruction, "cannot destructure %s into %d loop variables", value.TypeName(), len(instruction.Operands)-1)
	}
	if len(list.Elements) != len(instruction.Operands)-1 {
		return vm.runtimeError(instruction, "cannot destructure list of length %d into %d loop variables", len(list.Elements), len(instruction.Operands)-1)
	}
	for i, slot := range instruction.Operands[1:] {
		if err := vm.setLocal(slot, list.Elements[i]); err != nil {
			return vm.callPropagate(instruction, err)
		}
	}
	return nil
}

func (vm *VM) buildRange(instruction Instruction) error {
	stepValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	endValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	startValue, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	// Char-range fast path: 'a'..'z' (single-character string operands)
	// builds an eager list<string> of single-character entries rather
	// than a Range value. Step is ignored - char ranges always step by 1.
	if startStr, ok := startValue.(runtime.String); ok && len([]rune(startStr.Value)) == 1 {
		endStr, ok := endValue.(runtime.String)
		if !ok || len([]rune(endStr.Value)) != 1 {
			return vm.runtimeError(instruction, "char range end must be a single-character string")
		}
		exclusive := instruction.Operands[0] != 0
		startRune := []rune(startStr.Value)[0]
		endRune := []rune(endStr.Value)[0]
		var elements []runtime.Value
		if startRune <= endRune {
			for r := startRune; r <= endRune; r++ {
				if exclusive && r == endRune {
					break
				}
				elements = append(elements, runtime.String{Value: string(r)})
			}
		} else {
			for r := startRune; r >= endRune; r-- {
				if exclusive && r == endRune {
					break
				}
				elements = append(elements, runtime.String{Value: string(r)})
			}
		}
		vm.push(&runtime.List{Elements: elements})
		return nil
	}
	start := big.NewInt(0)
	if _, ok := startValue.(runtime.Null); !ok {
		switch sv := startValue.(type) {
		case runtime.SmallInt:
			start = big.NewInt(sv.Value)
		case runtime.Int:
			start = sv.Value
		default:
			return vm.runtimeError(instruction, "range start must be int")
		}
	}
	var endBig *big.Int
	switch ev := endValue.(type) {
	case runtime.SmallInt:
		endBig = big.NewInt(ev.Value)
	case runtime.Int:
		endBig = ev.Value
	default:
		return vm.runtimeError(instruction, "range end must be int")
	}
	step := big.NewInt(1)
	if _, ok := stepValue.(runtime.Null); !ok {
		switch sv := stepValue.(type) {
		case runtime.SmallInt:
			step = big.NewInt(sv.Value)
		case runtime.Int:
			step = sv.Value
		default:
			return vm.runtimeError(instruction, "range step must be int")
		}
	}
	if step.Sign() == 0 {
		return vm.runtimeError(instruction, "range step cannot be zero")
	}
	vm.push(runtime.Range{
		Start:     new(big.Int).Set(start),
		End:       new(big.Int).Set(endBig),
		Exclusive: instruction.Operands[0] != 0,
		Step:      new(big.Int).Set(step),
	})
	return nil
}

func (vm *VM) popExitCode(instruction Instruction) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	switch v := value.(type) {
	case runtime.SmallInt:
		return int(v.Value), nil
	case runtime.Int:
		if !v.Value.IsInt64() {
			return 0, vm.runtimeError(instruction, "exit code is out of range")
		}
		return int(v.Value.Int64()), nil
	}
	return 0, vm.runtimeError(instruction, "sys.exit expects int")
}

func (vm *VM) addDefer(action deferredAction) {
	current := len(vm.defers) - 1
	vm.defers[current] = append(vm.defers[current], action)
}

// expandDeferSpread reads the frozen spread reference's CURRENT elements (fire time, not registration time) so an in-place mutation after defer is visible, matching the evaluator.
// mergeBoundaryFrames splices this VM's frames onto an error thrown in a sub-VM (defer wrapper, module hop), whose trace stops at the boundary; boundaryLine is the call site in this VM.
func (vm *VM) mergeBoundaryFrames(thrown runtime.Error, boundaryLine int) runtime.Error {
	outer, outerTop := vm.snapshotContractFrames()
	if len(outer) > 0 {
		outer[0].CallLine = boundaryLine
		thrown.TraceFrames = append(append([]runtime.StackFrame{}, thrown.TraceFrames...), outer...)
		thrown.TopLevelLine = outerTop
	} else if thrown.TopLevelLine == 0 {
		thrown.TopLevelLine = boundaryLine
	}
	return thrown
}

// wrapDeferError re-raises a deferred call's typed throw or runtime fault with its clean message (catch class and trace survive); only a non-fault Go error gets the deferred-context wrap.
func (vm *VM) wrapDeferError(instruction Instruction, deferLine int, err error, format string, args ...any) error {
	if fault, ok := boundaryFaultToError(err); ok {
		return vm.uncaughtThrowError(instruction, vm.mergeBoundaryFrames(fault, deferLine))
	}
	return vm.runtimeError(instruction, format, args...)
}

// hofCallLine != 0 only while a native HOF dispatches a separate-VM callback, whose uncaught throw drops the host caller frames; splice them back on.
func (vm *VM) stitchHofCallbackError(err error) error {
	if err == nil || vm.hofCallLine == 0 {
		return err
	}
	if fault, ok := boundaryFaultToError(err); ok {
		return vm.uncaughtThrowError(Instruction{Line: int32(vm.hofCallLine)}, vm.mergeBoundaryFrames(fault, vm.hofCallLine))
	}
	return err
}

func (vm *VM) runDefers(instruction Instruction) error {
	if len(vm.defers) == 0 {
		return nil
	}
	current := len(vm.defers) - 1
	actions := vm.defers[current]
	vm.defers = vm.defers[:current]
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		switch action.kind {
		case deferKindPrint:
			text, err := vm.displayString(action.value)
			if err != nil {
				return vm.runtimeError(instruction, "deferred print: %v", err)
			}
			if vm.shouldRouteDirectPrint() {
				if _, err := vm.statefulNativeCall("io", "print", []runtime.Value{runtime.String{Value: text}}, nil); err != nil {
					return vm.runtimeError(instruction, "deferred print: %v", err)
				}
				continue
			}
			if _, err := fmt.Fprint(vm.stdout, text); err != nil {
				return err
			}
		case deferKindPrintln:
			text, err := vm.displayString(action.value)
			if err != nil {
				return vm.runtimeError(instruction, "deferred println: %v", err)
			}
			if vm.shouldRouteDirectPrint() {
				if _, err := vm.statefulNativeCall("io", "println", []runtime.Value{runtime.String{Value: text}}, nil); err != nil {
					return vm.runtimeError(instruction, "deferred println: %v", err)
				}
				continue
			}
			if _, err := fmt.Fprintln(vm.stdout, text); err != nil {
				return err
			}
		case deferKindNative:
			args := action.args
			if action.spreadMask != nil {
				expanded, _, hasNamed, err := expandDeferredSpreadArgs(action)
				if err != nil {
					return vm.runtimeError(instruction, "deferred call %s: %v", action.name, err)
				}
				if hasNamed {
					// The evaluator rejects named args mixed with spread on natives.
					err := fmt.Errorf("named arguments are only supported for Geblang functions and methods")
					return vm.wrapDeferError(instruction, action.line, err, "deferred call %s: %v", action.name, err)
				}
				args = expanded
			}
			if len(action.names) > 0 && action.spreadMask == nil {
				if _, err := vm.evalNativeCallWithNames(action.name, args, action.names); err != nil {
					return vm.wrapDeferError(instruction, action.line, err, "deferred call %s: %v", action.name, err)
				}
			} else if _, err := vm.evalNativeCall(action.name, args); err != nil {
				return vm.wrapDeferError(instruction, action.line, err, "deferred call %s: %v", action.name, err)
			}
		case deferKindFunc:
			args := action.args
			if action.spreadMask != nil {
				expanded, names, hasNamed, err := expandDeferredSpreadArgs(action)
				if err != nil {
					return vm.runtimeError(instruction, "deferred call: %v", err)
				}
				if hasNamed {
					ordered, oerr := vm.orderRuntimeArguments(instruction, vm.curMod.Chunk.Functions[action.funcIdx], expanded, names, 0)
					if oerr != nil {
						return oerr
					}
					expanded = ordered
				}
				args = expanded
			}
			if _, err := vm.CallFunction(action.funcIdx, args); err != nil {
				return vm.wrapDeferError(instruction, action.line, err, "deferred call: %v", err)
			}
		case deferKindMethod:
			instance, ok := action.receiver.(*runtime.Instance)
			if !ok {
				return vm.runtimeError(instruction, "deferred method call receiver is not an instance")
			}
			args := action.args
			names := action.names
			if action.spreadMask != nil {
				expanded, expandedNames, hasNamed, err := expandDeferredSpreadArgs(action)
				if err != nil {
					return vm.runtimeError(instruction, "deferred method call %s: %v", action.name, err)
				}
				args = expanded
				if hasNamed {
					names = expandedNames
				} else {
					names = nil
				}
			}
			if len(names) > 0 {
				reordered, err := vm.reorderMethodNamedArgs(instruction, instance, action.name, args, names)
				if err != nil {
					return vm.runtimeError(instruction, "deferred method call %s: %v", action.name, err)
				}
				args = reordered
			}
			if _, err := vm.CallMethod(instance, action.name, args); err != nil {
				return vm.wrapDeferError(instruction, action.line, err, "deferred method call %s: %v", action.name, err)
			}
		case deferKindCallable:
			args := action.args
			names := action.names
			if action.spreadMask != nil {
				expanded, expandedNames, hasNamed, err := expandDeferredSpreadArgs(action)
				if err != nil {
					return vm.runtimeError(instruction, "deferred callable call: %v", err)
				}
				args = expanded
				if hasNamed {
					names = expandedNames
				} else {
					names = nil
				}
			}
			if len(names) > 0 {
				reordered, err := vm.reorderCallableNamedArgs(instruction, action.value, args, names)
				if err != nil {
					return vm.runtimeError(instruction, "deferred callable call: %v", err)
				}
				args = reordered
			}
			if _, err := vm.callCallable(action.value, args); err != nil {
				return vm.wrapDeferError(instruction, action.line, err, "deferred callable call: %v", err)
			}
		default:
			return vm.runtimeError(instruction, "unknown deferred action kind")
		}
	}
	return nil
}

func (vm *VM) Exports() (map[string]runtime.Value, error) {
	exports := map[string]runtime.Value{}
	for _, export := range vm.chunk.Exports {
		if len(export.OverloadIndices) > 0 {
			ov := vm.overloadedValue(export.OverloadIndices)
			ov.Name = export.Name
			ov.Module = vm.moduleName
			ov.Indices = append([]int64(nil), export.OverloadIndices...)
			exports[export.Name] = ov
			continue
		}
		if export.FunctionIndex >= 0 {
			function := runtime.BytecodeFunction{Name: export.Name, Index: export.FunctionIndex, Module: vm.moduleName}
			if int(export.FunctionIndex) < len(vm.chunk.Functions) {
				info := vm.chunk.Functions[export.FunctionIndex]
				function.Doc = info.Doc
				function.TypeParameters = append([]string(nil), info.TypeParameters...)
				function.Parameters = parameterMetadataFromFunctionInfo(info, 0)
				function.ReturnType = info.ReturnType
				function.Async = info.Async
				function.Variadic = info.Variadic
				function.Decorators = append([]runtime.DecoratorMetadata(nil), info.Decorators...)
				function.DefLine = info.DefLine
				function.DefColumn = info.DefColumn
			}
			exports[export.Name] = function
			continue
		}
		if export.ClassIndex >= 0 {
			decorators := []runtime.DecoratorMetadata(nil)
			methodDecorators := map[string][]runtime.DecoratorMetadata(nil)
			staticDecorators := map[string][]runtime.DecoratorMetadata(nil)
			methodMetadata := map[string][]runtime.FunctionMetadata(nil)
			staticMetadata := map[string][]runtime.FunctionMetadata(nil)
			if int(export.ClassIndex) < len(vm.chunk.Classes) {
				classInfo := vm.chunk.Classes[export.ClassIndex]
				decorators = classInfo.Decorators
				methodDecorators = classInfo.MethodDecorators
				staticDecorators = classInfo.StaticDecorators
				methodMetadata = vm.classFunctionMetadata(classInfo.Methods, "method", 1)
				staticMetadata = vm.classFunctionMetadata(classInfo.StaticMethods, "staticMethod", 0)
			}
			if dec, exists := vm.decoratedClasses[export.ClassIndex]; exists {
				exports[export.Name] = dec
				continue
			}
			classValue := runtime.BytecodeClass{Name: export.Name, Index: export.ClassIndex, Decorators: decorators, MethodDecorators: methodDecorators, StaticDecorators: staticDecorators, MethodMetadata: methodMetadata, StaticMetadata: staticMetadata}
			if int(export.ClassIndex) < len(vm.chunk.Classes) {
				classInfo := vm.chunk.Classes[export.ClassIndex]
				classValue.Module = vm.moduleName
				classValue.Doc = classInfo.Doc
				classValue.Parent = classInfo.ParentName
				classValue.Fields = append([]string(nil), classInfo.FieldNames...)
				classValue.Interfaces = append([]string(nil), classInfo.Implements...)
				classValue.ConstructorMetadata = vm.constructorFunctionMetadata(classInfo.ConstructorIndices)
				classValue.DefLine = classInfo.DefLine
				classValue.DefColumn = classInfo.DefColumn
				sort.Strings(classValue.Fields)
				sort.Strings(classValue.Interfaces)
			}
			exports[export.Name] = classValue
			continue
		}
		if export.InterfaceIndex >= 0 {
			exports[export.Name] = vm.buildRuntimeInterface(export.Name, map[string]bool{})
			continue
		}
		value, err := vm.getGlobal(export.Slot)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", export.Name, err)
		}
		// Stamp an exported enum's home module so a foreign VM routes its
		// method calls back to this chunk's function table.
		if enumDef, ok := value.(*runtime.EnumDef); ok && enumDef.Module == "" {
			enumDef.Module = vm.moduleName
		}
		exports[export.Name] = value
	}
	return exports, nil
}

// lowerConstantName returns the lowercased form of a String constant
// at the given index, memoised in nameLowerCache so the lookup amortises
// to a slice read on the hot method-dispatch path.
func (vm *VM) constantsLen() int {
	return len(vm.curMod.Chunk.Constants) + len(vm.constantsExtra)
}

func (vm *VM) constantValue(index int64) runtime.Value {
	if base := vm.curMod.Chunk.Constants; int(index) < len(base) {
		return base[index]
	}
	return vm.constantsExtra[int(index)-len(vm.curMod.Chunk.Constants)]
}

func sameInstructionBase(a, b []Instruction) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

func appendWrapper(base, wrapper []Instruction) []Instruction {
	merged := make([]Instruction, 0, len(base)+len(wrapper))
	merged = append(merged, base...)
	return append(merged, wrapper...)
}

func (vm *VM) lowerConstantName(index int64, original string) string {
	if index < 0 {
		return strings.ToLower(original)
	}
	if int(index) >= len(vm.curMod.Chunk.Constants) {
		// Extras-tail constant (wrapper call argument): not cacheable.
		return strings.ToLower(original)
	}
	if int(index) < len(vm.nameLowerCache) {
		if lowered := vm.nameLowerCache[index]; lowered != "" {
			return lowered
		}
	} else {
		// Grow cache to match constants pool size.
		grown := make([]string, len(vm.curMod.Chunk.Constants))
		copy(grown, vm.nameLowerCache)
		vm.nameLowerCache = grown
	}
	lowered := strings.ToLower(original)
	if lowered == "" {
		lowered = original // preserve the "not cached" sentinel for empty original
	}
	vm.nameLowerCache[index] = lowered
	return lowered
}

// cloneContainerDefault returns a fresh copy of a mutable container
// value used as a function-parameter default. Sharing the same dict
// or set across calls would let one call's mutations leak into a
// later call (the Python "mutable default argument" trap). Empty
// containers (the common case for `dict opts = {}` / `list xs = []` /
// `set s = set()`) clone in constant time; non-container values are
// returned as-is. Lists are technically immutable in Geblang (push
// returns a new list), but the clone is cheap and defensive.
func cloneContainerDefault(v runtime.Value) runtime.Value {
	switch val := v.(type) {
	case runtime.Dict:
		cloned := make(map[string]runtime.DictEntry, val.Len())
		val.ForEachEntry(func(k string, entry runtime.DictEntry) bool {
			cloned[k] = entry
			return true
		})
		return runtime.Dict{Entries: cloned}
	case runtime.Set:
		cloned := make(map[string]runtime.SetEntry, len(val.Elements))
		for k, entry := range val.Elements {
			cloned[k] = entry
		}
		return runtime.Set{Elements: cloned}
	case *runtime.List:
		if len(val.Elements) == 0 {
			return &runtime.List{Elements: nil}
		}
		cloned := make([]runtime.Value, len(val.Elements))
		copy(cloned, val.Elements)
		return &runtime.List{Elements: cloned}
	}
	return v
}

// stripModulePrefix returns the trailing identifier of a possibly
// qualified type name. "mod.Sub" -> "Sub"; "Sub" -> "Sub". Matches
// `simpleTypeName` in the evaluator.
func stripModulePrefix(typeName string) string {
	if dot := strings.LastIndexByte(typeName, '.'); dot >= 0 {
		return typeName[dot+1:]
	}
	return typeName
}

// dictKeyFor is a fast-path wrapper around native.DictKey for the
// dict / set hot paths. User code keys dicts overwhelmingly by
// string or small integer; both build their canonical key string
// without any allocation beyond the result, while
// `native.DictKey` dispatches through a type switch that also
// handles composite keys recursively. This helper inlines those
// two common cases and falls through to the canonical
// implementation for everything else; the Go compiler can inline
// the wrapper itself when the call site has a known key shape.
func dictKeyFor(value runtime.Value) string {
	switch v := value.(type) {
	case runtime.String:
		return "s" + v.Value
	case runtime.SmallInt:
		return "i" + strconv.FormatInt(v.Value, 10)
	}
	return native.DictKey(value)
}

// errorParentChain returns the parent chain for an error-derived
// ClassInfo, immediate parent first, the built-in chain (RuntimeError
// -> Error) terminating at the root. Used at error-value construction
// to capture the chain so cross-module instanceof checks work without
// requiring access to the originating chunk.
func (vm *VM) errorParentChain(classInfo ClassInfo) []string {
	var parents []string
	visited := map[string]bool{}
	current := classInfo
	for {
		if current.ParentName == "" {
			break
		}
		if visited[current.ParentName] {
			break
		}
		visited[current.ParentName] = true
		parents = append(parents, current.ParentName)
		if classIndex, ok := vm.curMod.classIndex[strings.ToLower(current.ParentName)]; ok && int(classIndex) < len(vm.curMod.Chunk.Classes) {
			current = vm.curMod.Chunk.Classes[classIndex]
			continue
		}
		// Cross-chunk or built-in parent - extend via the static
		// built-in chain.
		for next := isBuiltinErrorChainParent(current.ParentName); next != ""; next = isBuiltinErrorChainParent(next) {
			if visited[next] {
				break
			}
			visited[next] = true
			parents = append(parents, next)
		}
		break
	}
	return parents
}

// isBuiltinErrorChainParent returns the static parent name for built-in
// error class names (RuntimeError -> Error etc.). Empty when the input
// has no static parent.
func isBuiltinErrorChainParent(class string) string {
	switch class {
	case "TimeoutError", "TlsError":
		return "IOError"
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError":
		return "Error"
	}
	return ""
}

func (vm *VM) popString(instruction Instruction, message string) (string, error) {
	value, err := vm.pop()
	if err != nil {
		return "", vm.callPropagate(instruction, err)
	}
	stringValue, ok := value.(runtime.String)
	if !ok {
		return "", vm.runtimeError(instruction, "%s", message)
	}
	return stringValue.Value, nil
}

func (vm *VM) constantString(instruction Instruction, message string) (string, error) {
	if len(instruction.Operands) < 1 {
		return "", vm.runtimeError(instruction, "instruction missing constant operand")
	}
	return vm.constantStringAt(instruction, instruction.Operands[0], message)
}

func (vm *VM) constantStringAt(instruction Instruction, index int64, message string) (string, error) {
	if index < 0 || int(index) >= vm.constantsLen() {
		return "", vm.runtimeError(instruction, "constant index out of range")
	}
	stringValue, ok := vm.constantValue(index).(runtime.String)
	if !ok {
		return "", vm.runtimeError(instruction, "%s", message)
	}
	return stringValue.Value, nil
}

func indexInt(value runtime.Value) (int, error) {
	switch v := value.(type) {
	case runtime.SmallInt:
		return int(v.Value), nil
	case runtime.Int:
		if !v.Value.IsInt64() {
			return 0, fmt.Errorf("index is out of range")
		}
		return int(v.Value.Int64()), nil
	}
	return 0, fmt.Errorf("index must be int, got %s", value.TypeName())
}

// push wraps a runtime.Value into a VMValue and appends it to the stack.
// Hot opcode handlers that already produce VMValue should call pushVM
// directly to skip the conversion.
func (vm *VM) push(value runtime.Value) {
	vm.stack = append(vm.stack, runtime.VMValueFromValue(value))
}

// pushVM pushes a pre-converted VMValue. Used by the hot integer/bool
// opcode fast paths to avoid round-tripping through the runtime.Value
// interface.
func (vm *VM) pushVM(value runtime.VMValue) {
	vm.stack = append(vm.stack, value)
}

func (vm *VM) pop() (runtime.Value, error) {
	if len(vm.stack) == 0 {
		return nil, fmt.Errorf("stack underflow")
	}
	value := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return value.ToValue(), nil
}

// popVM returns the top VMValue without converting to runtime.Value.
func (vm *VM) popVM() (runtime.VMValue, error) {
	if len(vm.stack) == 0 {
		return runtime.VMValueNull, fmt.Errorf("stack underflow")
	}
	value := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return value, nil
}

func (vm *VM) peek() (runtime.Value, error) {
	if len(vm.stack) == 0 {
		return nil, fmt.Errorf("stack underflow")
	}
	return vm.stack[len(vm.stack)-1].ToValue(), nil
}

// peekVM returns the top VMValue without converting to runtime.Value.
// Use when the caller only needs Kind/AsBool/I64 access and would
// otherwise pay an interface boxing in peek().
func (vm *VM) peekVM() (runtime.VMValue, error) {
	if len(vm.stack) == 0 {
		return runtime.VMValueNull, fmt.Errorf("stack underflow")
	}
	return vm.stack[len(vm.stack)-1], nil
}

func bitwiseMagicName(op Op) (string, bool) {
	switch op {
	case OpBitAnd:
		return "__bitand", true
	case OpBitOr:
		return "__bitor", true
	case OpBitXor:
		return "__bitxor", true
	case OpLShift:
		return "__lshift", true
	case OpRShift:
		return "__rshift", true
	}
	return "", false
}

func (vm *VM) bitwiseInfix(instruction Instruction, ip int) (int, error) {
	right, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	left, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if instance, ok := left.(*runtime.Instance); ok {
		if methodName, ok := bitwiseMagicName(instruction.Op); ok {
			classInfo, ok := vm.classInfo(instance.Class.Name)
			if !ok {
				return 0, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
			}
			if indices, ok := vm.lookupMethod(classInfo, methodName); ok {
				functionIndex, err := vm.selectRuntimeFunction(instruction, methodName, indices, []runtime.Value{right}, 1)
				if err != nil {
					return 0, err
				}
				return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance, right}, nil)
			}
		}
	}
	lb, lok := native.IntValueToBigInt(left)
	rb, rok := native.IntValueToBigInt(right)
	if !lok || !rok {
		return 0, vm.runtimeError(instruction, "bitwise operators require int operands, got %s and %s", left.TypeName(), right.TypeName())
	}
	var result *big.Int
	switch instruction.Op {
	case OpBitAnd:
		result = new(big.Int).And(lb, rb)
	case OpBitOr:
		result = new(big.Int).Or(lb, rb)
	case OpBitXor:
		result = new(big.Int).Xor(lb, rb)
	case OpLShift:
		if !rb.IsUint64() {
			return 0, vm.runtimeError(instruction, "shift amount must be a non-negative int")
		}
		result = new(big.Int).Lsh(lb, uint(rb.Uint64()))
	case OpRShift:
		if !rb.IsUint64() {
			return 0, vm.runtimeError(instruction, "shift amount must be a non-negative int")
		}
		result = new(big.Int).Rsh(lb, uint(rb.Uint64()))
	}
	if result.IsInt64() {
		vm.push(runtime.SmallInt{Value: result.Int64()})
	} else {
		vm.push(runtime.Int{Value: result})
	}
	return -1, nil
}

func (vm *VM) bitwiseNot(instruction Instruction, ip int) (int, error) {
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	if instance, ok := value.(*runtime.Instance); ok {
		classInfo, ok := vm.classInfo(instance.Class.Name)
		if !ok {
			return 0, vm.runtimeError(instruction, "unknown class %s", instance.Class.Name)
		}
		if indices, ok := vm.lookupMethod(classInfo, "__bitnot"); ok {
			functionIndex, err := vm.selectRuntimeFunction(instruction, "__bitnot", indices, nil, 1)
			if err != nil {
				return 0, err
			}
			return vm.startPrevalidatedFunction(instruction, ip, &vm.curMod.Chunk.Functions[functionIndex], []runtime.Value{instance}, nil)
		}
	}
	intVal, ok := native.IntValueToBigInt(value)
	if !ok {
		return 0, vm.runtimeError(instruction, "~ requires int, got %s", value.TypeName())
	}
	result := new(big.Int).Not(intVal)
	if result.IsInt64() {
		vm.push(runtime.SmallInt{Value: result.Int64()})
	} else {
		vm.push(runtime.Int{Value: result})
	}
	return -1, nil
}

func (vm *VM) setGlobal(slot int64, value runtime.Value) error {
	if int(slot) >= len(vm.curGlobals) {
		if err := ensureSlot(&vm.globals, slot, "global"); err != nil {
			return err
		}
		vm.curGlobals = vm.globals
	}
	if !vm.bridgeActive.Load() {
		vm.curGlobals[slot] = runtime.VMValueFromValue(value)
		return nil
	}
	vm.globalsMu.Lock()
	vm.curGlobals[slot] = runtime.VMValueFromValue(value)
	vm.markGlobalDirtyLocked(int(slot))
	vm.globalsMu.Unlock()
	return nil
}

func (vm *VM) setGlobalVM(slot int64, value runtime.VMValue) error {
	if int(slot) >= len(vm.curGlobals) {
		if err := ensureSlot(&vm.globals, slot, "global"); err != nil {
			return err
		}
		vm.curGlobals = vm.globals
	}
	// Fast path: no wrap-bridge worker can be touching this VM's globals.
	if !vm.bridgeActive.Load() {
		vm.curGlobals[slot] = value
		if vm.persistGlobals && int(slot) < len(vm.dirtyGlobals) {
			vm.dirtyGlobals[slot] = true
		}
		return nil
	}
	vm.globalsMu.Lock()
	vm.curGlobals[slot] = value
	vm.markGlobalDirtyLocked(int(slot))
	vm.globalsMu.Unlock()
	return nil
}

// markGlobalDirtyLocked records that slot has been written and grows the
// dirty-bitset alongside vm.globals if a previous ensureSlot grew the
// globals slice beyond the bitset's capacity. Caller holds globalsMu.
func (vm *VM) markGlobalDirtyLocked(slot int) {
	if slot < 0 {
		return
	}
	if slot >= len(vm.dirtyGlobals) {
		grown := make([]bool, len(vm.globals))
		copy(grown, vm.dirtyGlobals)
		vm.dirtyGlobals = grown
	}
	vm.dirtyGlobals[slot] = true
}

func (vm *VM) getGlobal(slot int64) (runtime.Value, error) {
	if int(slot) >= len(vm.curGlobals) {
		if err := ensureSlot(&vm.globals, slot, "global"); err != nil {
			return nil, err
		}
		vm.curGlobals = vm.globals
	}
	value := vm.curGlobals[slot]
	if value.Kind == runtime.VMKindUnset {
		return nil, fmt.Errorf("global is undefined")
	}
	if value.Kind == runtime.VMKindBoxed {
		if acc, ok := value.Boxed.(*runtime.StringAccumulator); ok {
			s := acc.Materialize()
			vm.curGlobals[slot] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: s}
			return s, nil
		}
	}
	return value.ToValue(), nil
}

func (vm *VM) getGlobalVM(slot int64) (runtime.VMValue, error) {
	if int(slot) >= len(vm.curGlobals) {
		if err := ensureSlot(&vm.globals, slot, "global"); err != nil {
			return runtime.VMValueNull, err
		}
		vm.curGlobals = vm.globals
	}
	value := vm.curGlobals[slot]
	if value.Kind == runtime.VMKindUnset {
		return runtime.VMValueNull, fmt.Errorf("global is undefined")
	}
	if value.Kind == runtime.VMKindBoxed {
		if acc, ok := value.Boxed.(*runtime.StringAccumulator); ok {
			materialized := runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: acc.Materialize()}
			vm.curGlobals[slot] = materialized
			return materialized, nil
		}
	}
	return value, nil
}

func (vm *VM) setLocal(slot int64, value runtime.Value) error {
	idx := vm.currentFrameBP + int(slot)
	if idx >= len(vm.localsStack) {
		if err := vm.ensureLocalSlot(slot); err != nil {
			return err
		}
	}
	cur := vm.localsStack[idx]
	if cur.Kind == runtime.VMKindBoxed {
		if cell, ok := cur.Boxed.(*runtime.BytecodeCell); ok {
			if _, replacingCell := value.(*runtime.BytecodeCell); !replacingCell {
				cell.Value = value
				return nil
			}
		}
	}
	vm.localsStack[idx] = runtime.VMValueFromValue(value)
	return nil
}

func (vm *VM) setLocalVM(slot int64, value runtime.VMValue) error {
	idx := vm.currentFrameBP + int(slot)
	if idx >= len(vm.localsStack) {
		if err := vm.ensureLocalSlot(slot); err != nil {
			return err
		}
	}
	cur := vm.localsStack[idx]
	if cur.Kind == runtime.VMKindBoxed {
		if cell, ok := cur.Boxed.(*runtime.BytecodeCell); ok {
			if _, replacingCell := value.Boxed.(*runtime.BytecodeCell); !replacingCell || value.Kind != runtime.VMKindBoxed {
				cell.Value = value.ToValue()
				return nil
			}
		}
	}
	vm.localsStack[idx] = value
	return nil
}

// defineLocalVM binds slot to a fresh value, replacing any cell an earlier
// closure boxed, so a re-executed `let` starts a new binding.
func (vm *VM) defineLocalVM(slot int64, value runtime.VMValue) error {
	idx := vm.currentFrameBP + int(slot)
	if idx >= len(vm.localsStack) {
		if err := vm.ensureLocalSlot(slot); err != nil {
			return err
		}
		idx = vm.currentFrameBP + int(slot)
	}
	vm.localsStack[idx] = value
	return nil
}

func (vm *VM) getLocal(slot int64) (runtime.Value, error) {
	idx := vm.currentFrameBP + int(slot)
	if idx >= len(vm.localsStack) {
		if err := vm.ensureLocalSlot(slot); err != nil {
			return nil, err
		}
	}
	value := vm.localsStack[idx]
	if value.Kind == runtime.VMKindUnset {
		return nil, fmt.Errorf("local is undefined")
	}
	if value.Kind == runtime.VMKindBoxed {
		if cell, ok := value.Boxed.(*runtime.BytecodeCell); ok {
			if cell.Value == nil {
				return nil, fmt.Errorf("local is undefined")
			}
			return cell.Value, nil
		}
		if acc, ok := value.Boxed.(*runtime.StringAccumulator); ok {
			s := acc.Materialize()
			vm.localsStack[idx] = runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: s}
			return s, nil
		}
	}
	return value.ToValue(), nil
}

func (vm *VM) getLocalVM(slot int64) (runtime.VMValue, error) {
	idx := vm.currentFrameBP + int(slot)
	if idx >= len(vm.localsStack) {
		if err := vm.ensureLocalSlot(slot); err != nil {
			return runtime.VMValueNull, err
		}
	}
	value := vm.localsStack[idx]
	if value.Kind == runtime.VMKindUnset {
		return runtime.VMValueNull, fmt.Errorf("local is undefined")
	}
	if value.Kind == runtime.VMKindBoxed {
		if cell, ok := value.Boxed.(*runtime.BytecodeCell); ok {
			if cell.Value == nil {
				return runtime.VMValueNull, fmt.Errorf("local is undefined")
			}
			return runtime.VMValueFromValue(cell.Value), nil
		}
		if acc, ok := value.Boxed.(*runtime.StringAccumulator); ok {
			materialized := runtime.VMValue{Kind: runtime.VMKindBoxed, Boxed: acc.Materialize()}
			vm.localsStack[idx] = materialized
			return materialized, nil
		}
	}
	return value, nil
}

func (vm *VM) ensureLocalSlot(slot int64) error {
	if slot < 0 {
		return fmt.Errorf("local slot out of range")
	}
	maxInt := int64(int(^uint(0) >> 1))
	if slot > maxInt {
		return fmt.Errorf("local slot out of range")
	}
	end := vm.currentFrameBP + int(slot) + 1
	vm.ensureLocalsStack(end)
	return nil
}

func ensureSlot(values *[]runtime.VMValue, slot int64, label string) error {
	if slot < 0 {
		return fmt.Errorf("%s slot out of range", label)
	}
	maxInt := int64(int(^uint(0) >> 1))
	if slot > maxInt {
		return fmt.Errorf("%s slot out of range", label)
	}
	target := int(slot) + 1
	if target <= len(*values) {
		return nil
	}
	newLen := len(*values)
	if newLen == 0 {
		newLen = 1
	}
	for newLen < target {
		newLen *= 2
	}
	grown := make([]runtime.VMValue, newLen)
	copy(grown, *values)
	*values = grown
	return nil
}

// Pop frames a completed inline run left above baseline (after an uncaught throw) so a shared re-entry worker's next run does not inherit them.
func (vm *VM) dropInlineRunFrames(baseline int) {
	for len(vm.frames) > baseline {
		idx := len(vm.frames) - 1
		slot := &vm.frames[idx]
		vm.popLocalsStackFrame(slot)
		*slot = callFrame{}
		vm.frames = vm.frames[:idx]
	}
}

// Converts outermost-first frame snapshot to innermost-first contract frames; frame i pairs its CallLine with frame i+1's callLine.
func (vm *VM) snapshotContractFrames() (frames []runtime.StackFrame, topLevelLine int) {
	n := len(vm.frames)
	// A synchronous re-entry on a shared worker starts this run's frames at traceFrameBaseline; frames below it belong to the caller's run and must not leak into this run's trace.
	base := vm.traceFrameBaseline
	if base < 0 {
		base = 0
	}
	if n <= base {
		return nil, 0
	}
	frames = make([]runtime.StackFrame, 0, n-base)
	for i := n - 1; i >= base; i-- {
		callLine := 0
		if i+1 < n {
			callLine = vm.frames[i+1].callLine
		}
		frames = append(frames, runtime.StackFrame{Name: vm.frames[i].functionName, CallLine: callLine})
		if r := vm.frames[i].tailRepeat; r > 0 {
			// Self-recursive TCE reused one frame; surface the elided repeats as a collapsed [xN] entry.
			frames = append(frames, runtime.StackFrame{Name: vm.frames[i].functionName, CallLine: vm.frames[i].tailCallLine, Repeat: r})
		}
	}
	return frames, vm.frames[base].callLine
}

// vmRuntimeError is the error vm.runtimeError returns. It snapshots the
// call frames cheaply at throw time and renders through the canonical
// runtime.UncaughtError so the user-facing output matches the evaluator.
type vmRuntimeError struct {
	line         int
	message      string
	frames       []runtime.StackFrame
	topLevelLine int
}

func (e *vmRuntimeError) uncaught() *runtime.UncaughtError {
	return &runtime.UncaughtError{
		Class:        "RuntimeError",
		Message:      e.message,
		ErrorLine:    e.line,
		Frames:       runtime.CollapseFrames(e.frames),
		TopLevelLine: e.topLevelLine,
	}
}

func (e *vmRuntimeError) Error() string { return e.uncaught().Render() }

// toRuntimeError converts a dispatch-loop fault into a catchable runtime.Error, preserving frames.
func (e *vmRuntimeError) toRuntimeError() runtime.Error {
	return runtime.Error{
		Class:        "RuntimeError",
		Message:      e.message,
		Parents:      []string{"RuntimeError", "Error"},
		TraceFrames:  e.frames,
		ErrorLine:    e.line,
		TopLevelLine: e.topLevelLine,
	}
}

func (vm *VM) runtimeError(instruction Instruction, format string, args ...any) error {
	frames, topLevel := vm.snapshotContractFrames()
	if len(frames) == 0 && topLevel == 0 {
		// Top-level fault: show the failing instruction's line.
		topLevel = int(instruction.Line)
	}
	return &vmRuntimeError{
		line:         int(instruction.Line),
		message:      fmt.Sprintf(format, args...),
		frames:       frames,
		topLevelLine: topLevel,
	}
}

// callPropagate keeps a callee's sys.exit terminating instead of reframing it as a runtime fault.
func (vm *VM) callPropagate(instruction Instruction, err error) error {
	var exitErr ExitError
	if errors.As(err, &exitErr) {
		return exitErr
	}
	return vm.runtimeError(instruction, "%s", err.Error())
}

// Prefers the thrown error's captured frames over the live snapshot so the trace shows the original throw site.
func (vm *VM) uncaughtThrowError(instruction Instruction, thrown runtime.Error) error {
	frames := thrown.TraceFrames
	errorLine := thrown.ErrorLine
	topLevel := thrown.TopLevelLine
	if len(frames) == 0 {
		var topLine int
		frames, topLine = vm.snapshotContractFrames()
		if errorLine == 0 {
			errorLine = int(instruction.Line)
		}
		if topLevel == 0 {
			topLevel = topLine
		}
		// Top-level throw with no frames: show the failing instruction's line.
		if topLevel == 0 && len(frames) == 0 {
			topLevel = int(instruction.Line)
		}
		// Stamp resolved location back so the unwrapped error and the wrapper agree.
		thrown.TraceFrames = frames
		thrown.ErrorLine = errorLine
		thrown.TopLevelLine = topLevel
	}
	u := &runtime.UncaughtError{
		Class:        thrown.Class,
		Message:      thrown.Message,
		ErrorLine:    errorLine,
		Frames:       runtime.CollapseFrames(frames),
		TopLevelLine: topLevel,
	}
	return &wrappedError{prefix: u.Render(), inner: vmThrownError{err: thrown}}
}

// wrappedError preserves Unwrap support so callers can recover the
// inner vmThrownError via errors.As while still seeing the formatted
// runtime-error stack trace via Error().
type wrappedError struct {
	prefix string
	inner  error
}

func (e *wrappedError) Error() string { return e.prefix }
func (e *wrappedError) Unwrap() error { return e.inner }

// boundaryErrorLine stamps a Fast-path worker's zero-frame, zero-line error to the call site so the merged trace's innermost frame still shows a line.
func boundaryErrorLine(thrown runtime.Error, boundaryLine int) runtime.Error {
	if thrown.ErrorLine == 0 && len(thrown.TraceFrames) == 0 {
		thrown.ErrorLine = boundaryLine
	}
	return thrown
}

// propagateModuleError converts a Go error returned by a moduleLoader
// dispatch into the calling VM's exception state. If the error wraps a
// vmThrownError, the embedded runtime.Error is set as the calling VM's
// pendingThrow and control jumps to the nearest exception handler so
// `catch` clauses see the original typed throw rather than a stringified
// "uncaught X: Y" message. All other errors are returned verbatim.
func (vm *VM) propagateModuleError(instruction Instruction, ip int, err error) (int, error) {
	var thrown vmThrownError
	if errors.As(err, &thrown) {
		captured := vm.mergeBoundaryFrames(boundaryErrorLine(thrown.err, int(instruction.Line)), int(instruction.Line))
		vm.pendingThrow = &captured
		return vm.jumpToExceptionHandler(instruction, ip)
	}
	// A non-typed fault uncaught in the callee VM returns as *vmRuntimeError; carry it as a throw so a cross-module catch sees the clean message, not the rendered "uncaught ..." blob (parity with typed throws and the evaluator).
	var rt *vmRuntimeError
	if errors.As(err, &rt) {
		captured := vm.mergeBoundaryFrames(boundaryErrorLine(rt.toRuntimeError(), int(instruction.Line)), int(instruction.Line))
		vm.pendingThrow = &captured
		return vm.jumpToExceptionHandler(instruction, ip)
	}
	return 0, vm.callPropagate(instruction, err)
}
