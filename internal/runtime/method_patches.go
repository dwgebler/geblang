package runtime

import (
	"strings"
	"sync"
	"sync/atomic"
)

type methodPatchKey struct {
	module string
	class  string
	method string
}

type instancePatchKey struct {
	instance *Instance
	method   string
}

// MethodPatchSnapshot is an opaque copy of the active method patches.
type MethodPatchSnapshot struct {
	classes   map[methodPatchKey]Value
	instances map[instancePatchKey]Value
}

var (
	methodPatchMu      sync.RWMutex
	classMethodPatches = map[methodPatchKey]Value{}
	instMethodPatches  = map[instancePatchKey]Value{}
	methodPatchCount   atomic.Int32
)

// MethodPatchesActive is the hot-path gate: callers skip ResolveMethodPatch while it is false.
func MethodPatchesActive() bool {
	return methodPatchCount.Load() > 0
}

func syncMethodPatchCount() {
	methodPatchCount.Store(int32(len(classMethodPatches) + len(instMethodPatches)))
}

// PatchClassMethod replaces method on class for every receiver that dispatches through it.
func PatchClassMethod(class *Class, method string, replacement Value) {
	methodPatchMu.Lock()
	defer methodPatchMu.Unlock()
	classMethodPatches[methodPatchKey{class.Module, class.Name, strings.ToLower(method)}] = replacement
	syncMethodPatchCount()
}

// PatchInstanceMethod replaces method on one object.
func PatchInstanceMethod(instance *Instance, method string, replacement Value) {
	methodPatchMu.Lock()
	defer methodPatchMu.Unlock()
	instMethodPatches[instancePatchKey{instance, strings.ToLower(method)}] = replacement
	syncMethodPatchCount()
}

// UnpatchClassMethod removes a class patch; it reports whether one was active.
func UnpatchClassMethod(class *Class, method string) bool {
	methodPatchMu.Lock()
	defer methodPatchMu.Unlock()
	key := methodPatchKey{class.Module, class.Name, strings.ToLower(method)}
	_, ok := classMethodPatches[key]
	delete(classMethodPatches, key)
	syncMethodPatchCount()
	return ok
}

// UnpatchInstanceMethod removes an instance patch; it reports whether one was active.
func UnpatchInstanceMethod(instance *Instance, method string) bool {
	methodPatchMu.Lock()
	defer methodPatchMu.Unlock()
	key := instancePatchKey{instance, strings.ToLower(method)}
	_, ok := instMethodPatches[key]
	delete(instMethodPatches, key)
	syncMethodPatchCount()
	return ok
}

// SnapshotMethodPatches copies the active patches for a later RestoreMethodPatches.
func SnapshotMethodPatches() MethodPatchSnapshot {
	methodPatchMu.RLock()
	defer methodPatchMu.RUnlock()
	snap := MethodPatchSnapshot{classes: make(map[methodPatchKey]Value, len(classMethodPatches)), instances: make(map[instancePatchKey]Value, len(instMethodPatches))}
	for k, v := range classMethodPatches {
		snap.classes[k] = v
	}
	for k, v := range instMethodPatches {
		snap.instances[k] = v
	}
	return snap
}

// RestoreMethodPatches replaces the active patches; the zero snapshot clears them all.
func RestoreMethodPatches(snap MethodPatchSnapshot) {
	methodPatchMu.Lock()
	defer methodPatchMu.Unlock()
	classMethodPatches = map[methodPatchKey]Value{}
	instMethodPatches = map[instancePatchKey]Value{}
	for k, v := range snap.classes {
		classMethodPatches[k] = v
	}
	for k, v := range snap.instances {
		instMethodPatches[k] = v
	}
	syncMethodPatchCount()
}

// ResolveMethodPatch finds the patch for entering ownerClass.method: class patches between the owner and the nearest lower redefinition (a parent call starts above it), nearest the receiver first; instance patches only on a plain dispatch.
func ResolveMethodPatch(receiver *Instance, ownerModule, ownerClass, method string) (Value, bool) {
	if receiver == nil || receiver.Class == nil {
		return nil, false
	}
	method = strings.ToLower(method)
	var chain []*Class
	owner := -1
	for c := receiver.Class; c != nil; c = c.Parent {
		if owner < 0 && c.Name == ownerClass && c.Module == ownerModule {
			owner = len(chain)
		}
		chain = append(chain, c)
	}
	if owner < 0 {
		owner = len(chain) - 1
	}
	lowest := 0
	for i := owner - 1; i >= 0; i-- {
		if _, defines := chain[i].Methods[method]; defines {
			lowest = i + 1
			break
		}
	}
	methodPatchMu.RLock()
	defer methodPatchMu.RUnlock()
	if lowest == 0 {
		if v, ok := instMethodPatches[instancePatchKey{receiver, method}]; ok {
			return v, true
		}
	}
	for i := lowest; i <= owner; i++ {
		if v, ok := classMethodPatches[methodPatchKey{chain[i].Module, chain[i].Name, method}]; ok {
			return v, true
		}
	}
	return nil, false
}

// PatchableMethod describes the method a patch targets, found on class or an ancestor.
type PatchableMethod struct {
	Overloads []Function
	Metadata  []FunctionMetadata
}

// FindPatchableMethod looks up an instance method for patching across class and its ancestors.
func FindPatchableMethod(class *Class, method string) (PatchableMethod, bool) {
	lower := strings.ToLower(method)
	for c := class; c != nil; c = c.Parent {
		if overloads, ok := c.Methods[lower]; ok && len(overloads) > 0 {
			return PatchableMethod{Overloads: overloads, Metadata: c.MethodMetadata[lower]}, true
		}
	}
	return PatchableMethod{}, false
}

// MethodShape is one overload's call shape, excluding the receiver.
type MethodShape struct {
	Required  int
	Max       int
	Variadic  bool
	Async     bool
	Generator bool
}

// Shapes describes each overload, from the functions themselves or, for a native dispatch wrapper, from the metadata.
func (m PatchableMethod) Shapes() []MethodShape {
	var shapes []MethodShape
	if len(m.Overloads) > 0 && m.Overloads[0].Native != nil && len(m.Metadata) > 0 {
		for _, meta := range m.Metadata {
			shape := MethodShape{Variadic: meta.Variadic, Async: meta.Async, Generator: meta.Generator}
			for _, p := range meta.Parameters {
				if p.Variadic {
					continue
				}
				shape.Max++
				if !p.HasDefault {
					shape.Required++
				}
			}
			shapes = append(shapes, shape)
		}
		return shapes
	}
	for _, fn := range m.Overloads {
		shape := MethodShape{Async: fn.Async, Generator: fn.IsGenerator}
		params := fn.Parameters
		if fn.OriginalParameters != nil {
			params = fn.OriginalParameters
		}
		for _, p := range params {
			if p.Variadic {
				shape.Variadic = true
				continue
			}
			shape.Max++
			if p.Default == nil {
				shape.Required++
			}
		}
		shapes = append(shapes, shape)
	}
	return shapes
}
