// Package bcloader is the production bytecode module loader, importing neither evaluator nor check (those two couplings are injected via Options).
package bcloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/native"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

// Options injects the two dependencies bcloader must not import directly.
type Options struct {
	// Compile turns module source into a chunk (production owns .gbc cache + cross-module analysis; harness is no-cache); modulePaths is the loader's live path list at the call point.
	Compile func(canonical, sourcePath string, source []byte, program *ast.Program, modulePaths []string) (bytecode.Chunk, error)

	// LookupBuiltin resolves a native Go-side module by canonical/alias, or nil.
	LookupBuiltin func(canonical, alias string) *runtime.Module
}

// moduleRecord is a loaded module's published state, stored once and read lock-free. globalsVM is mutated in place under globalsMu (cross-module global write-back); the record pointer is stable.
type moduleRecord struct {
	module         *runtime.Module
	chunk          bytecode.Chunk
	globalsVM      []runtime.VMValue
	decorators     bytecode.FunctionDecoratorState
	ifaceFallbacks bytecode.InterfaceFallbackState
}

// reentrantMutex serializes module loads across goroutines but lets a load's own goroutine re-enter for nested imports; load-time only, so the goid lookup cost is irrelevant.
type reentrantMutex struct {
	sem   chan struct{}
	owner atomic.Int64
	count int
}

func newReentrantMutex() *reentrantMutex { return &reentrantMutex{sem: make(chan struct{}, 1)} }

func (m *reentrantMutex) Lock() {
	gid := native.GoroutineID()
	if m.owner.Load() == gid {
		m.count++
		return
	}
	m.sem <- struct{}{}
	m.owner.Store(gid)
	m.count = 1
}

func (m *reentrantMutex) Unlock() {
	m.count--
	if m.count == 0 {
		m.owner.Store(0)
		<-m.sem
	}
}

// Loader implements bytecode.ModuleLoader for a host VM.
type Loader struct {
	stdout io.Writer
	// modulePaths is augmented under loadLock during a load for nested resolution; basePaths is the stable list dispatch workers read without locking.
	modulePaths []string
	basePaths   []string
	stateful    bytecode.StatefulNativeCaller
	opts        Options

	// records holds one *moduleRecord per canonical module; reads are lock-free, loads serialize on loadLock.
	records sync.Map
	// runtimeClasses caches the built *runtime.Class per "module.Class" so a cross-module parent chain is materialised once (immutable).
	runtimeClasses sync.Map
	mainVM         *bytecode.VM
	// loadLock serializes cross-goroutine loads (so the shared modulePaths mutation and cycle set stay safe) while allowing nested same-goroutine loads.
	loadLock *reentrantMutex
	loading  map[string]bool
	vmPools  sync.Map
	// globalsMu serializes the restore-from / write-back-to a record's globalsVM so concurrent cross-module calls don't race the slice; module-global writes persist with lost-update semantics under concurrency (use a thread-safe handle for shared state).
	globalsMu sync.Mutex
	// mainChunk is the entry chunk so cross-module reflect lookups resolve user-script classes.
	mainChunk    bytecode.Chunk
	hasMainChunk bool
}

// New builds a loader; stateful may be nil and supplied later via SetStateful.
func New(stdout io.Writer, modulePaths []string, stateful bytecode.StatefulNativeCaller, opts Options) *Loader {
	return &Loader{
		stdout:      stdout,
		modulePaths: append([]string(nil), modulePaths...),
		basePaths:   append([]string(nil), modulePaths...),
		stateful:    stateful,
		opts:        opts,
		loadLock:    newReentrantMutex(),
		loading:     map[string]bool{},
	}
}

func (l *Loader) recordFor(module string) (*moduleRecord, bool) {
	if v, ok := l.records.Load(module); ok {
		return v.(*moduleRecord), true
	}
	return nil, false
}

func (l *Loader) chunkValue(module string) (bytecode.Chunk, bool) {
	if rec, ok := l.recordFor(module); ok {
		return rec.chunk, true
	}
	return bytecode.Chunk{}, false
}

var _ bytecode.ModuleLoader = (*Loader)(nil)

// SetMainChunk registers the entry-point chunk for cross-module reflect lookups.
func (l *Loader) SetMainChunk(chunk bytecode.Chunk) { l.mainChunk = chunk; l.hasMainChunk = true }

// SetMainVM connects the running entry VM so its globals/interface-defaults read live.
func (l *Loader) SetMainVM(vm *bytecode.VM) {
	l.mainVM = vm
	// The entry VM owns program-lifetime destructibles a caller==nil path may adopt from another goroutine; guard its list.
	vm.EnableDestructibleLocking()
}

// SetStateful injects the stateful native caller after construction.
func (l *Loader) SetStateful(s bytecode.StatefulNativeCaller) { l.stateful = s }

// SetModulePaths replaces the module search paths used to resolve imports.
func (l *Loader) SetModulePaths(paths []string) {
	l.modulePaths = append([]string(nil), paths...)
	l.basePaths = append([]string(nil), paths...)
}

func (l *Loader) lookupBuiltin(canonical, alias string) *runtime.Module {
	if l.opts.LookupBuiltin == nil {
		return nil
	}
	return l.opts.LookupBuiltin(canonical, alias)
}

func (l *Loader) LoadModule(canonical string, alias string) (*runtime.Module, error) {
	if rec, ok := l.recordFor(canonical); ok {
		return rec.module, nil
	}
	// Serialize cross-goroutine loads (keeps the shared modulePaths mutation + cycle set safe); nested same-goroutine loads re-enter.
	l.loadLock.Lock()
	defer l.loadLock.Unlock()
	if rec, ok := l.recordFor(canonical); ok {
		return rec.module, nil
	}
	path, err := modules.NewResolver(l.modulePaths).Resolve(canonical)
	if err != nil {
		if native := l.lookupBuiltin(canonical, alias); native != nil {
			l.records.Store(canonical, &moduleRecord{module: native})
			return native, nil
		}
		return nil, err
	}
	if l.loading[path] {
		if native := l.lookupBuiltin(canonical, alias); native != nil {
			l.records.Store(canonical, &moduleRecord{module: native})
			return native, nil
		}
		return nil, fmt.Errorf("circular import detected for %s", canonical)
	}
	l.loading[path] = true
	defer delete(l.loading, path)

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module %s: %w", canonical, err)
	}
	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse module %s: %s", canonical, strings.Join(p.Errors(), "\n"))
	}
	if l.opts.Compile == nil {
		return nil, fmt.Errorf("compile module %s: loader has no Compile callback", canonical)
	}
	chunk, err := l.opts.Compile(canonical, path, source, program, l.modulePaths)
	if err != nil {
		return nil, fmt.Errorf("compile module %s: %w", canonical, err)
	}
	previousPaths := l.modulePaths
	l.modulePaths = append([]string{filepath.Dir(path)}, l.modulePaths...)
	vm := bytecode.NewVMWithModuleLoader(chunk, l.stdout, l)
	vm.SetModuleName(canonical)
	vm.SetModulePaths(l.modulePaths)
	vm.SetStatefulNativeCaller(l.stateful)
	err = vm.Run()
	l.modulePaths = previousPaths
	if err != nil {
		return nil, fmt.Errorf("evaluate module %s: %w", canonical, err)
	}
	exports, err := vm.Exports()
	if err != nil {
		return nil, fmt.Errorf("export module %s: %w", canonical, err)
	}
	module := &runtime.Module{Name: alias, Canonical: canonical, Exports: exports}
	for name, value := range module.Exports {
		if function, ok := value.(runtime.BytecodeFunction); ok {
			function.Module = canonical
			module.Exports[name] = function
		}
		if class, ok := value.(runtime.BytecodeClass); ok {
			class.Module = canonical
			module.Exports[name] = class
		}
		if overloaded, ok := value.(runtime.OverloadedFunction); ok {
			overloaded.Module = canonical
			module.Exports[name] = overloaded
		}
	}
	l.records.Store(canonical, &moduleRecord{
		module:         module,
		chunk:          chunk,
		globalsVM:      vm.GlobalsSnapshotVM(),
		decorators:     vm.FunctionDecoratorState(),
		ifaceFallbacks: vm.InterfaceFallbackState(),
	})
	return module, nil
}

// moved verbatim from cmd/geblang/main.go (receiver renamed)
func (l *Loader) ifaceFallbackStateFor(module string) bytecode.InterfaceFallbackState {
	if module == "" && l.mainVM != nil {
		return l.mainVM.InterfaceFallbackState()
	}
	if rec, ok := l.recordFor(module); ok {
		return rec.ifaceFallbacks
	}
	return bytecode.InterfaceFallbackState{}
}

// restoreModuleGlobals loads a worker's globals: the entry chunk reads live from the main VM; a sub-module restores its live module globals (mutations persist via releaseModuleVM's write-back).
func (l *Loader) restoreModuleGlobals(vm *bytecode.VM, module string) {
	if module == "" && l.mainVM != nil {
		vm.RestoreGlobals(l.mainVM.GlobalsSnapshot())
		return
	}
	rec, ok := l.recordFor(module)
	if !ok {
		return
	}
	vm.SetPersistGlobals(true)
	l.globalsMu.Lock()
	vm.RestoreGlobalsVM(rec.globalsVM)
	l.globalsMu.Unlock()
}

// moduleVM returns a pooled (or fresh) host VM bound to the module's
// chunk and configured with the module's canonical state. Hosts carry
// per-call configuration only; pooling them avoids a full VM
// construction per cross-module call.
func (l *Loader) moduleVM(module string, chunk bytecode.Chunk, caller *bytecode.VM) (*bytecode.VM, *sync.Pool) {
	// Re-entry: a still-active ancestor worker for this module on the same call chain (hence same goroutine) is reused so the nested call shares its live globals (nil pool marks the borrowed reuse).
	steps := 0
	for h := caller; h != nil; h = h.ReentryHost() {
		if h.ReentryActive() && h.ModuleName() == module {
			h.IncReentryDepth()
			return h, nil
		}
		if steps++; steps > 4096 { // defensive: never loop on a malformed/cyclic host chain
			break
		}
	}
	var pool *sync.Pool
	if p, ok := l.vmPools.Load(module); ok {
		pool = p.(*sync.Pool)
	} else {
		p, _ := l.vmPools.LoadOrStore(module, &sync.Pool{})
		pool = p.(*sync.Pool)
	}
	vm, _ := pool.Get().(*bytecode.VM)
	if vm == nil {
		vm = bytecode.NewVMWithModuleLoader(chunk, l.stdout, l)
	} else {
		vm.ResetForReuse()
	}
	vm.SetModuleName(module)
	vm.SetModulePaths(l.basePaths)
	vm.SetStatefulNativeCaller(l.stateful)
	l.restoreModuleGlobals(vm, module)
	var decorators bytecode.FunctionDecoratorState
	if rec, ok := l.recordFor(module); ok {
		decorators = rec.decorators
	}
	vm.RestoreFunctionDecoratorState(decorators)
	vm.RestoreInterfaceFallbackState(l.ifaceFallbackStateFor(module))
	// A worker must never be its own re-entry host (a reacquired-from-pool worker passed as its own caller would otherwise form a self-loop).
	if caller == vm {
		caller = nil
	}
	vm.SetReentryHost(caller)
	vm.SetReentryActive(true)
	return vm, pool
}

func (l *Loader) releaseModuleVM(pool *sync.Pool, vm *bytecode.VM, err error) {
	if pool == nil { // borrowed reuse; the owner release persists + recycles.
		vm.DecReentryDepth()
		return
	}
	// Persist module-global writes even when the call threw: the evaluator mutates its live environment immediately, so a write before a throw must survive (the dirty slots already hold only the pre-throw writes).
	if module := vm.ModuleName(); module != "" {
		if rec, ok := l.recordFor(module); ok {
			l.globalsMu.Lock()
			vm.PersistDirtyGlobals(rec.globalsVM)
			l.globalsMu.Unlock()
		}
	}
	vm.SetReentryActive(false)
	vm.SetReentryHost(nil)
	if err == nil && vm.Recyclable() {
		pool.Put(vm)
	}
}

// adoptDestructibles hands a transient worker's destructor lifetimes up to the caller (or the entry VM when caller==nil), so they fire at that owner's exit sweep, not on worker recycle. A borrowed reuse (pool==nil) keeps them with the in-flight owner.
func (l *Loader) adoptDestructibles(pool *sync.Pool, worker, caller *bytecode.VM) {
	if pool == nil {
		return
	}
	target := caller
	if target == nil {
		target = l.mainVM
	}
	if target == nil || target == worker {
		return
	}
	target.AdoptDestructibles(worker.TakeDestructibles())
}

func (l *Loader) CallModuleFunction(function runtime.BytecodeFunction, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if function.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script function invoked without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(function.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", function.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(function.Module, chunk, caller)
	result, err := vm.CallFunctionFast(function.Index, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

// CallModuleMethodValue dispatches a bound method/static value pinned to funcIndex on its home module worker; instance is the receiver (nil for a static). Applies the method's decorator wrapper with the receiver bound, matching a bound-method value's semantics.
func (l *Loader) CallModuleMethodValue(module string, funcIndex int64, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script method value invoked without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallBoundMethodValue(instance, funcIndex, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

// CallModuleHandlerIsolated runs a cross-module server handler on its home module's VM. There the handler is same-module, so the VM's per-request isolation applies (a fresh callVM with cloned globals and, for a closure, cloned upvalues); the home VM's own globals are read but never mutated, so nothing persists back and handler state stays per-request.
func (l *Loader) CallModuleHandlerIsolated(fn runtime.Value, module string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script handler invoked without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallIsolatedHandler(fn, args)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) CallModuleClosure(closure runtime.BytecodeClosure, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if closure.Module == "" {
		// Closures created in the entry script carry Module="".
		// Dispatch against the main chunk so the FunctionIndex
		// resolves to the closure's body rather than whatever
		// happens to live at that index in some sub-VM's chunk.
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script closure invoked without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(closure.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", closure.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(closure.Module, chunk, caller)
	result, err := vm.CallClosureFast(closure, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) ConstructModuleClass(class runtime.BytecodeClass, args []runtime.Value, typeArgs []string, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if class.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("construct %s: main chunk is not registered", class.Name)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(class.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", class.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(class.Module, chunk, caller)
	result, err := vm.ConstructClassFast(class.Index, args, typeArgs)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) ConstructModuleClassNamed(class runtime.BytecodeClass, args []runtime.Value, names []string, typeArgs []string, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if class.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("construct %s: main chunk is not registered", class.Name)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(class.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", class.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(class.Module, chunk, caller)
	result, err := vm.ConstructClassFastNamed(class.Index, args, names, typeArgs)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) CallModuleOverloaded(module string, indices []int64, displayName string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	chunk, ok := l.overloadChunk(module)
	if !ok {
		return nil, fmt.Errorf("module %s is not loaded", module)
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallOverloadedFast(indices, displayName, args, caller)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) CallModuleOverloadedNamed(module string, indices []int64, displayName string, args []runtime.Value, names []string, caller *bytecode.VM) (runtime.Value, error) {
	chunk, ok := l.overloadChunk(module)
	if !ok {
		return nil, fmt.Errorf("module %s is not loaded", module)
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallOverloadedFastNamed(indices, displayName, args, names, caller)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) overloadChunk(module string) (bytecode.Chunk, bool) {
	if module == "" {
		if !l.hasMainChunk {
			return bytecode.Chunk{}, false
		}
		return l.mainChunk, true
	}
	return l.chunkValue(module)
}

// ConstructorsForModuleClass evaluates `reflect.constructors(class)`
// against the chunk that declared the class.
func (l *Loader) ConstructorsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if class.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("reflect.constructors %s: main chunk is not registered", class.Name)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(class.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", class.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(class.Module, chunk, nil)
	result, err := vm.ReflectConstructorsForChunkClass(class)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) FieldsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if class.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("reflect.fields %s: main chunk is not registered", class.Name)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(class.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", class.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(class.Module, chunk, nil)
	result, err := vm.ReflectFieldsForChunkClass(class)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

// DeserializeModuleClass picks the right chunk for a class returned
// from another module (or the main program) and runs the local
// deserialize path on a sub-VM bound to that chunk.
func (l *Loader) DeserializeModuleClass(class runtime.BytecodeClass, value runtime.Value) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if class.Module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("deserialize %s: main chunk is not registered", class.Name)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(class.Module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", class.Module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(class.Module, chunk, nil)
	result, err := vm.DeserializeIntoChunkClass(class, value)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) CallModuleStaticMethod(class runtime.BytecodeClass, methodName string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	chunk, ok := l.chunkValue(class.Module)
	if !ok {
		return nil, fmt.Errorf("module %s is not loaded", class.Module)
	}
	vm, pool := l.moduleVM(class.Module, chunk, caller)
	result, err := vm.CallStaticMethodFast(class.Index, methodName, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) CallParentInModule(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script parent dispatch called without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallMethodAs(className, instance, methodName, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

func (l *Loader) ImmutableFieldsForModuleClass(module string, className string) []string {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil
		}
		chunk = c
	}
	for i := range chunk.Classes {
		if chunk.Classes[i].Name == className {
			return chunk.Classes[i].ImmutableFields
		}
	}
	return nil
}

func (l *Loader) chunkFor(module string) (bytecode.Chunk, bool) {
	if module == "" {
		if !l.hasMainChunk {
			return bytecode.Chunk{}, false
		}
		return l.mainChunk, true
	}
	c, ok := l.chunkValue(module)
	return c, ok
}

func (l *Loader) ModuleClassDescendsFrom(module, className, targetSimpleName string) bool {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return false
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		for ci := chunk.Classes[i]; ; {
			if strings.EqualFold(ci.Name, targetSimpleName) {
				return true
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				return l.ModuleClassDescendsFrom(ci.ParentName[:dot], ci.ParentName[dot+1:], targetSimpleName)
			}
			return false
		}
	}
	return false
}

func (l *Loader) StaticValueForModuleClass(module, className, name string) (runtime.Value, bool) {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return nil, false
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		for ci := chunk.Classes[i]; ; {
			if idx, present := ci.StaticValues[name]; present && idx >= 0 && int(idx) < len(chunk.Constants) {
				return chunk.Constants[idx], true
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				return l.StaticValueForModuleClass(ci.ParentName[:dot], ci.ParentName[dot+1:], name)
			}
			return nil, false
		}
	}
	return nil, false
}

func (l *Loader) CallModuleStaticMethodByName(module, className, methodName string, args []runtime.Value) (runtime.Value, bool, error) {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return nil, false, nil
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		for ci := chunk.Classes[i]; ; {
			if _, present := ci.StaticMethods[strings.ToLower(methodName)]; present {
				vm, pool := l.moduleVM(module, chunk, nil)
				result, err := vm.CallStaticMethodFast(int64(i), methodName, args)
				l.releaseModuleVM(pool, vm, err)
				return result, true, err
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				return l.CallModuleStaticMethodByName(ci.ParentName[:dot], ci.ParentName[dot+1:], methodName, args)
			}
			return nil, false, nil
		}
	}
	return nil, false, nil
}

func (l *Loader) StaticMethodValueForModuleClass(module, className, name string) (runtime.Value, bool) {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return nil, false
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		for ci := chunk.Classes[i]; ; {
			if _, present := ci.StaticMethods[strings.ToLower(name)]; present {
				vm, pool := l.moduleVM(module, chunk, nil)
				value, found := vm.StaticMethodValueFast(int64(i), name)
				l.releaseModuleVM(pool, vm, nil)
				return value, found
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				return l.StaticMethodValueForModuleClass(ci.ParentName[:dot], ci.ParentName[dot+1:], name)
			}
			return nil, false
		}
	}
	return nil, false
}

func (l *Loader) MethodFirstOverload(module, className, name string) (string, int64, bool) {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return "", 0, false
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		for ci := chunk.Classes[i]; ; {
			if indices, present := ci.Methods[strings.ToLower(name)]; present && len(indices) > 0 {
				return module, indices[0], true
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				return l.MethodFirstOverload(ci.ParentName[:dot], ci.ParentName[dot+1:], name)
			}
			return "", 0, false
		}
	}
	return "", 0, false
}

func (l *Loader) UnimplementedAbstractMethods(module, className string) map[string]string {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return nil
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		overridden := map[string]bool{}
		abstractDecl := map[string]string{}
		for ci := chunk.Classes[i]; ; {
			for method := range ci.Methods {
				isAbstract := false
				for _, dec := range ci.MethodDecorators[method] {
					if strings.EqualFold(dec.Name, "abstract") {
						isAbstract = true
						break
					}
				}
				if isAbstract {
					if !overridden[method] {
						if _, seen := abstractDecl[method]; !seen {
							abstractDecl[method] = ci.Name
						}
					}
				} else {
					overridden[method] = true
					delete(abstractDecl, method)
				}
			}
			if ci.ParentIndex >= 0 && int(ci.ParentIndex) < len(chunk.Classes) {
				ci = chunk.Classes[ci.ParentIndex]
				continue
			}
			if dot := strings.LastIndex(ci.ParentName, "."); dot >= 0 {
				for method, owner := range l.UnimplementedAbstractMethods(ci.ParentName[:dot], ci.ParentName[dot+1:]) {
					if !overridden[method] {
						if _, seen := abstractDecl[method]; !seen {
							abstractDecl[method] = owner
						}
					}
				}
			}
			break
		}
		return abstractDecl
	}
	return nil
}

func (l *Loader) ModuleMethodParamNames(module string, className string, methodName string) ([]string, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script class %s referenced without a main chunk", className)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	return chunk.MethodParamNames(className, methodName)
}

func (l *Loader) ModuleConstructorParamNames(module string, className string) ([]string, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script class %s referenced without a main chunk", className)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	return chunk.ConstructorParamNames(className)
}

func (l *Loader) ModuleFunctionParamNames(module string, index int64) ([]string, error) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script function referenced without a main chunk")
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	return chunk.FunctionParamNames(index)
}

func (l *Loader) CallModuleMethod(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	var chunk bytecode.Chunk
	if module == "" {
		// Main-chunk classes carry Module="". Sub-VMs running stdlib
		// modules dispatch into the entry chunk through this branch
		// when invoking a method on a user instance (e.g. the F3
		// dunder protocol: streams.copy(src, ...) calls
		// src.__read(n) where src is a main-chunk class).
		if !l.hasMainChunk {
			return nil, fmt.Errorf("entry-script instance method %s.%s called without a main chunk", className, methodName)
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			// Native modules (http, process, ...) carry no Geblang chunk, so an
			// unresolved method on one of their class instances is simply
			// undefined, not an unloaded module; report it as such so the VM
			// matches the evaluator's "unknown method" error.
			if native.IsNativeModule(module) {
				return nil, &runtime.MethodNotFoundError{Class: className, Method: methodName}
			}
			return nil, fmt.Errorf("module %s is not loaded", module)
		}
		chunk = c
	}
	vm, pool := l.moduleVM(module, chunk, caller)
	result, err := vm.CallMethodFast(instance, methodName, args)
	l.adoptDestructibles(pool, vm, caller)
	l.releaseModuleVM(pool, vm, err)
	return result, err
}

// CallModuleDestructor fires a cross-module instance's `func ~Class()` in its home chunk on a home worker. The worker suppresses its own cleanup, so a destructor that builds more destructibles hands them up to caller for the re-drain.
func (l *Loader) CallModuleDestructor(module string, className string, instance *runtime.Instance, caller *bytecode.VM) error {
	chunk, ok := l.chunkFor(module)
	if !ok {
		return nil
	}
	for i := range chunk.Classes {
		if !strings.EqualFold(chunk.Classes[i].Name, className) {
			continue
		}
		di := chunk.Classes[i].DestructorIndex
		if di < 0 {
			return nil
		}
		vm, pool := l.moduleVM(module, chunk, caller)
		_, err := vm.CallFunctionRaw(di, []runtime.Value{instance})
		l.adoptDestructibles(pool, vm, caller)
		l.releaseModuleVM(pool, vm, err)
		return err
	}
	return nil
}

func (l *Loader) ListAllClasses() []runtime.Value {
	out := []runtime.Value{}
	appendChunkClasses := func(module string, chunk bytecode.Chunk) {
		for i, classInfo := range chunk.Classes {
			out = append(out, runtime.BytecodeClass{
				Name:             classInfo.Name,
				Doc:              classInfo.Doc,
				Index:            int64(i),
				Module:           module,
				Parent:           classInfo.ParentName,
				Fields:           append([]string(nil), classInfo.FieldNames...),
				Interfaces:       append([]string(nil), classInfo.Implements...),
				Decorators:       classInfo.Decorators,
				MethodDecorators: classInfo.MethodDecorators,
				StaticDecorators: classInfo.StaticDecorators,
			})
		}
	}
	l.records.Range(func(k, v any) bool {
		appendChunkClasses(k.(string), v.(*moduleRecord).chunk)
		return true
	})
	// Entry chunk too, so reflect.classes() from an imported module sees it.
	if l.hasMainChunk {
		appendChunkClasses("", l.mainChunk)
	}
	return out
}

func (l *Loader) LookupModuleInterface(module, name string) (bytecode.InterfaceInfo, bool) {
	var chunk bytecode.Chunk
	if module == "" {
		if !l.hasMainChunk {
			return bytecode.InterfaceInfo{}, false
		}
		chunk = l.mainChunk
	} else {
		c, ok := l.chunkValue(module)
		if !ok {
			return bytecode.InterfaceInfo{}, false
		}
		chunk = c
	}
	for _, iface := range chunk.Interfaces {
		if strings.EqualFold(iface.Name, name) {
			return iface, true
		}
	}
	return bytecode.InterfaceInfo{}, false
}

// FindFunctionByName scans every loaded module's chunk for an
// exported function by name. Returns nil when no match.
func (l *Loader) FindFunctionByName(name string) (runtime.Value, bool) {
	var found runtime.Value
	var ok bool
	l.records.Range(func(_, v any) bool {
		module := v.(*moduleRecord).module
		if module == nil {
			return true
		}
		if value, has := module.Exports[name]; has {
			switch fn := value.(type) {
			case runtime.Function, runtime.OverloadedFunction, runtime.BytecodeFunction, runtime.DecoratorTarget:
				found, ok = fn, true
				return false
			}
		}
		return true
	})
	return found, ok
}

// FindClassByName scans every loaded module's chunk for a class with
// the given bare name and returns a BytecodeClass value bound to that
// chunk. Used by reflect.class(name) so framework helpers can resolve
// a user class without needing the originating module on import.
func classFromChunk(chunk bytecode.Chunk, module, name, key string) (runtime.Value, bool) {
	for idx, classInfo := range chunk.Classes {
		if strings.EqualFold(classInfo.Name, name) || strings.ToLower(classInfo.Name) == key {
			return runtime.BytecodeClass{
				Name:             classInfo.Name,
				Doc:              classInfo.Doc,
				Index:            int64(idx),
				Module:           module,
				Parent:           classInfo.ParentName,
				Fields:           append([]string(nil), classInfo.FieldNames...),
				Interfaces:       append([]string(nil), classInfo.Implements...),
				Decorators:       classInfo.Decorators,
				MethodDecorators: classInfo.MethodDecorators,
				StaticDecorators: classInfo.StaticDecorators,
				DefLine:          classInfo.DefLine,
				DefColumn:        classInfo.DefColumn,
			}, true
		}
	}
	return nil, false
}

func (l *Loader) FindClassByName(name string) (runtime.Value, bool) {
	key := strings.ToLower(name)
	var result runtime.Value
	var ok bool
	l.records.Range(func(k, v any) bool {
		if r, found := classFromChunk(v.(*moduleRecord).chunk, k.(string), name, key); found {
			result, ok = r, true
			return false
		}
		return true
	})
	if ok {
		return result, true
	}
	if l.hasMainChunk {
		return classFromChunk(l.mainChunk, "", name, key)
	}
	return nil, false
}

func (l *Loader) ClassValueInModule(module, className string) (runtime.Value, bool) {
	chunk, ok := l.chunkValue(module)
	if !ok {
		return nil, false
	}
	return classFromChunk(chunk, module, className, strings.ToLower(className))
}

func (l *Loader) PersistModuleGlobals(vm *bytecode.VM) {
	module := vm.ModuleName()
	if module == "" {
		return
	}
	if rec, ok := l.recordFor(module); ok {
		l.globalsMu.Lock()
		vm.PersistDirtyGlobals(rec.globalsVM)
		l.globalsMu.Unlock()
	}
}

func (l *Loader) RuntimeClassFor(module string, className string) (*runtime.Class, bool) {
	cacheKey := module + "." + className
	if v, ok := l.runtimeClasses.Load(cacheKey); ok {
		rc, _ := v.(*runtime.Class)
		return rc, rc != nil
	}
	chunk, ok := l.chunkValue(module)
	if !ok {
		l.runtimeClasses.Store(cacheKey, (*runtime.Class)(nil))
		return nil, false
	}
	idx := -1
	for i := range chunk.Classes {
		if strings.EqualFold(chunk.Classes[i].Name, className) {
			idx = i
			break
		}
	}
	if idx < 0 {
		l.runtimeClasses.Store(cacheKey, (*runtime.Class)(nil))
		return nil, false
	}
	vm, pool := l.moduleVM(module, chunk, nil)
	rc := vm.RuntimeClassValue(int64(idx))
	l.releaseModuleVM(pool, vm, nil)
	l.runtimeClasses.Store(cacheKey, rc)
	return rc, rc != nil
}
