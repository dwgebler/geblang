package runtime

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"geblang/internal/ast"
)

type Binding struct {
	Value    Value
	Constant bool
	// Imported marks a name brought in by `from M import X`. Such a name is
	// immutable: it cannot be locally redeclared or overloaded. ImportSource
	// is "module.symbol", so re-importing the same symbol is idempotent.
	Imported     bool
	ImportSource string
	// Type is the declared annotation; nil for an untyped binding.
	Type *ast.TypeRef
}

// Environment is the evaluator's lexical scope. The mutex guards
// concurrent access from async goroutines that share a closure
// environment with the parent evaluator. The first few bindings live
// in a fixed inline array - most scopes (call frames, blocks) hold a
// handful of names, so the common case allocates no map and looks up
// by short linear scan; larger scopes spill to the map.
type envEntry struct {
	name    string
	binding Binding
}

type Environment struct {
	mu           sync.RWMutex
	inline       [4]envEntry
	inlineCount  uint8
	local        atomic.Bool
	store        map[string]Binding
	typeBindings map[string]string
	typeAliases  map[string]*ast.TypeRef
	outer        *Environment
}

func NewEnvironment() *Environment {
	return &Environment{}
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	return &Environment{outer: outer.MarkShared()}
}

func NewLocalEnvironment() *Environment {
	env := &Environment{}
	env.local.Store(true)
	return env
}

func NewLocalEnclosedEnvironment(outer *Environment) *Environment {
	env := NewLocalEnvironment()
	env.outer = outer
	return env
}

// The owner publishes the entire lexical chain before another goroutine can use it.
func (e *Environment) MarkShared() *Environment {
	for env := e; env != nil; env = env.outer {
		if !env.local.Swap(false) {
			break
		}
	}
	return e
}

func (e *Environment) readLock() bool {
	if e.local.Load() {
		return false
	}
	e.mu.RLock()
	return true
}

func (e *Environment) readUnlock(locked bool) {
	if locked {
		e.mu.RUnlock()
	}
}

func (e *Environment) writeLock() bool {
	if e.local.Load() {
		return false
	}
	e.mu.Lock()
	return true
}

func (e *Environment) writeUnlock(locked bool) {
	if locked {
		e.mu.Unlock()
	}
}

// Callers synchronize shared scopes or own the scope exclusively.
func (e *Environment) lookupLocked(name string) (Binding, bool) {
	for i := uint8(0); i < e.inlineCount; i++ {
		if e.inline[i].name == name {
			return e.inline[i].binding, true
		}
	}
	if e.store != nil {
		b, ok := e.store[name]
		return b, ok
	}
	return Binding{}, false
}

func (e *Environment) setLocked(name string, b Binding) {
	for i := uint8(0); i < e.inlineCount; i++ {
		if e.inline[i].name == name {
			e.inline[i].binding = b
			return
		}
	}
	if e.store != nil {
		if _, ok := e.store[name]; ok {
			e.store[name] = b
			return
		}
	}
	if int(e.inlineCount) < len(e.inline) {
		e.inline[e.inlineCount] = envEntry{name: name, binding: b}
		e.inlineCount++
		return
	}
	if e.store == nil {
		e.store = map[string]Binding{}
	}
	e.store[name] = b
}

func (e *Environment) deleteLocked(name string) bool {
	for i := uint8(0); i < e.inlineCount; i++ {
		if e.inline[i].name == name {
			last := e.inlineCount - 1
			e.inline[i] = e.inline[last]
			e.inline[last] = envEntry{}
			e.inlineCount = last
			return true
		}
	}
	if e.store != nil {
		if _, ok := e.store[name]; ok {
			delete(e.store, name)
			return true
		}
	}
	return false
}

// The visitor may publish the scope before iteration completes.
func (e *Environment) ForEachBinding(visit func(name string, b Binding)) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := uint8(0); i < e.inlineCount; i++ {
		visit(e.inline[i].name, e.inline[i].binding)
	}
	for name, b := range e.store {
		visit(name, b)
	}
}

func (e *Environment) bindingCountLocked() int {
	return int(e.inlineCount) + len(e.store)
}

func (e *Environment) Define(name string, value Value, constant bool) error {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if _, exists := e.lookupLocked(name); exists {
		return fmt.Errorf("%q is already declared in this scope", name)
	}
	if constant {
		value = ShallowFreeze(value)
	}
	e.setLocked(name, Binding{Value: value, Constant: constant})
	return nil
}

// DefineTyped binds a name carrying its declared type for later reassignment checks.
func (e *Environment) DefineTyped(name string, value Value, constant bool, typ *ast.TypeRef) error {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if _, exists := e.lookupLocked(name); exists {
		return fmt.Errorf("%q is already declared in this scope", name)
	}
	if constant {
		value = ShallowFreeze(value)
	}
	e.setLocked(name, Binding{Value: value, Constant: constant, Type: typ})
	return nil
}

// DeclaredType returns the declared type of the nearest binding for name.
func (e *Environment) DeclaredType(name string) *ast.TypeRef {
	for env := e; env != nil; {
		locked := env.readLock()
		binding, ok := env.lookupLocked(name)
		outer := env.outer
		env.readUnlock(locked)
		if ok {
			return binding.Type
		}
		env = outer
	}
	return nil
}

// DefineImported binds a from-imported name. Re-importing the same symbol is
// idempotent; any other existing binding (or a different import) is a clash.
func (e *Environment) DefineImported(name string, value Value, source string) error {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if existing, exists := e.lookupLocked(name); exists && !(existing.Imported && existing.ImportSource == source) {
		return fmt.Errorf("%q is already declared in this scope", name)
	}
	e.setLocked(name, Binding{Value: ShallowFreeze(value), Constant: true, Imported: true, ImportSource: source})
	return nil
}

func (e *Environment) DefineFunction(name string, fn Function) error {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if binding, exists := e.lookupLocked(name); exists {
		if binding.Imported {
			return fmt.Errorf("%q is already declared in this scope", name)
		}
		switch value := binding.Value.(type) {
		case Function:
			e.setLocked(name, Binding{Value: OverloadedFunction{Name: name, Overloads: []Function{value, fn}}, Constant: true})
			return nil
		case OverloadedFunction:
			value.Overloads = append(value.Overloads, fn)
			e.setLocked(name, Binding{Value: value, Constant: true})
			return nil
		default:
			return fmt.Errorf("%q is already declared in this scope", name)
		}
	}
	e.setLocked(name, Binding{Value: fn, Constant: true})
	return nil
}

func (e *Environment) Assign(name string, value Value) error {
	locked := e.writeLock()
	if binding, exists := e.lookupLocked(name); exists {
		if binding.Constant {
			e.writeUnlock(locked)
			return fmt.Errorf("cannot assign to constant %q", name)
		}
		binding.Value = value
		e.setLocked(name, binding)
		e.writeUnlock(locked)
		return nil
	}
	outer := e.outer
	e.writeUnlock(locked)
	if outer != nil {
		return outer.Assign(name, value)
	}
	return fmt.Errorf("%q is not declared", name)
}

func (e *Environment) Get(name string) (Value, bool) {
	locked := e.readLock()
	if binding, exists := e.lookupLocked(name); exists {
		e.readUnlock(locked)
		return binding.Value, true
	}
	outer := e.outer
	e.readUnlock(locked)
	if outer != nil {
		return outer.Get(name)
	}
	return nil, false
}

// Delete removes the named binding from the first scope that
// contains it. Returns true when a binding was found and removed.
// Used by `del x` to retire a binding after its destructor has
// fired.
func (e *Environment) Delete(name string) bool {
	locked := e.writeLock()
	if e.deleteLocked(name) {
		if e.typeBindings != nil {
			delete(e.typeBindings, name)
		}
		e.writeUnlock(locked)
		return true
	}
	outer := e.outer
	e.writeUnlock(locked)
	if outer != nil {
		return outer.Delete(name)
	}
	return false
}

// DefineTypeAlias binds a `type` alias in this scope; names are case-insensitive.
func (e *Environment) DefineTypeAlias(name string, target *ast.TypeRef) {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if e.typeAliases == nil {
		e.typeAliases = map[string]*ast.TypeRef{}
	}
	e.typeAliases[strings.ToLower(name)] = target
}

// GetTypeAlias resolves a `type` alias through the enclosing scopes.
func (e *Environment) GetTypeAlias(name string) (*ast.TypeRef, bool) {
	key := strings.ToLower(name)
	for env := e; env != nil; {
		locked := env.readLock()
		target, ok := env.typeAliases[key]
		outer := env.outer
		env.readUnlock(locked)
		if ok {
			return target, true
		}
		env = outer
	}
	return nil, false
}

func (e *Environment) DefineTypeBinding(name, typeName string) {
	locked := e.writeLock()
	defer e.writeUnlock(locked)
	if e.typeBindings == nil {
		e.typeBindings = map[string]string{}
	}
	if _, exists := e.typeBindings[name]; !exists {
		e.typeBindings[name] = typeName
	}
}

func (e *Environment) GetTypeBinding(name string) (string, bool) {
	if e == nil {
		return "", false
	}
	locked := e.readLock()
	if e.typeBindings != nil {
		if t, ok := e.typeBindings[name]; ok {
			e.readUnlock(locked)
			return t, true
		}
	}
	outer := e.outer
	e.readUnlock(locked)
	if outer != nil {
		return outer.GetTypeBinding(name)
	}
	return "", false
}

// TypeBindingNames returns all type-binding names reachable through
// this environment chain. Order is unspecified; callers should not
// rely on it.
func (e *Environment) TypeBindingNames() []string {
	if e == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var names []string
	for env := e; env != nil; env = env.outer {
		locked := env.readLock()
		for name := range env.typeBindings {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		env.readUnlock(locked)
	}
	return names
}

func (e *Environment) Names() []string {
	locked := e.readLock()
	defer e.readUnlock(locked)
	names := make([]string, 0, e.bindingCountLocked())
	for i := uint8(0); i < e.inlineCount; i++ {
		names = append(names, e.inline[i].name)
	}
	for name := range e.store {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *Environment) VisibleNames() []string {
	seen := map[string]bool{}
	var names []string
	for env := e; env != nil; env = env.outer {
		locked := env.readLock()
		for i := uint8(0); i < env.inlineCount; i++ {
			name := env.inline[i].name
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		for name := range env.store {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		env.readUnlock(locked)
	}
	sort.Strings(names)
	return names
}
