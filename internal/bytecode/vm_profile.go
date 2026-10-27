package bytecode

import (
	"geblang/internal/native"
	"geblang/internal/profiling"
	"geblang/internal/runtime"
	"strconv"
)

// maxProfileDepth bounds a sampled stack (and the host-chain walk).
const maxProfileDepth = 512

// profileSample records this goroutine's Geblang call stack once per
// profiling tick: this VM's frames, then those of the workers it was called
// from on the same goroutine (a cross-module call runs on the callee
// module's worker). At a loop back-edge, line is the current line; at a call
// (entering) the sample goes to the caller, at the call's line, since that is
// the code that ran up to this safepoint.
func (vm *VM) profileSample(line int, entering bool) {
	t := profiling.Tick()
	if t == vm.profileTick {
		return
	}
	// One sample per tick per call chain: the chain's outermost worker
	// holds the tick it was last sampled at.
	chain := make([]*VM, 0, 8)
	for h := vm; h != nil && len(chain) < maxProfileDepth; h = h.profileLink() {
		chain = append(chain, h)
	}
	for _, h := range chain {
		h.profileTick = t
	}
	root := chain[len(chain)-1]
	if root.profileRootTick == t {
		return
	}
	root.profileRootTick = t
	skip := 0
	if entering && len(vm.frames) > 0 {
		line = vm.frames[len(vm.frames)-1].callLine
		skip = 1
	}
	var inner []string
	leaf := ""
	for _, h := range chain {
		module := h.moduleName
		if module == "" {
			module = "main"
		}
		for i := len(h.frames) - 1 - skip; i >= 0 && len(inner) < maxProfileDepth; i-- {
			name := module + "." + h.frames[i].functionName
			if len(inner) == 0 && line > 0 {
				leaf = name + ":" + strconv.Itoa(line)
			}
			inner = append(inner, name)
		}
		skip = 0
		if len(inner) == 0 && line > 0 {
			leaf = module + ".<top level>:" + strconv.Itoa(line)
		}
	}
	frames := make([]string, 0, len(inner)+1)
	frames = append(frames, "<top level>")
	for i := len(inner) - 1; i >= 0; i-- {
		frames = append(frames, inner[i])
	}
	profiling.RecordThread(native.GoroutineID(), t, frames, leaf)
}

// profileLink is the worker this one was invoked from on the same goroutine.
func (vm *VM) profileLink() *VM {
	if vm.profileParent != nil {
		return vm.profileParent
	}
	return vm.reentryHost
}

// SetProfileParent records the worker that invoked this one on the same goroutine, for the profiler.
func (vm *VM) SetProfileParent(parent *VM) { vm.profileParent = parent }

// callNativeMethod invokes a native method; while profiling, a cross-module wrapper is told which worker invoked it.
func (vm *VM) callNativeMethod(method runtime.Function, instance *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if profiling.Active.Load() && method.NativeFrom != nil {
		return method.NativeFrom(vm, instance, args)
	}
	return method.Native(instance, args)
}
