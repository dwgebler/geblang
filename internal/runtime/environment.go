package runtime

import (
	"fmt"
	"sort"
	"strings"
	"sync"

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
	store        map[string]Binding
	typeBindings map[string]string
	typeAliases  map[string]*ast.TypeRef
	outer        *Environment
}

func NewEnvironment() *Environment {
	return &Environment{}
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	return &Environment{outer: outer}
}

// lookupLocked finds the binding in this scope only. Callers hold mu.
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

// setLocked inserts or overwrites the binding in this scope only.
// Callers hold mu.
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

// deleteLocked removes the binding from this scope only, reporting
// whether it existed. Callers hold mu.
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

// ForEachBinding visits every binding declared directly in this scope
// under the read lock. Iteration order is unspecified.
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
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.lookupLocked(name); exists {
		return fmt.Errorf("%q is already declared in this scope", name)
	}
	if constant {
		value = ShallowFreeze(value)
	}
	e.setLocked(name, Binding{Value: value, Constant: constant})
	return nil
}

// DefineImported binds a from-imported name. Re-importing the same symbol is
// idempotent; any other existing binding (or a different import) is a clash.
func (e *Environment) DefineImported(name string, value Value, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, exists := e.lookupLocked(name); exists && !(existing.Imported && existing.ImportSource == source) {
		return fmt.Errorf("%q is already declared in this scope", name)
	}
	e.setLocked(name, Binding{Value: ShallowFreeze(value), Constant: true, Imported: true, ImportSource: source})
	return nil
}

func (e *Environment) DefineFunction(name string, fn Function) error {
	e.mu.Lock()
	defer e.mu.Unlock()
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
	e.mu.Lock()
	if binding, exists := e.lookupLocked(name); exists {
		if binding.Constant {
			e.mu.Unlock()
			return fmt.Errorf("cannot assign to constant %q", name)
		}
		binding.Value = value
		e.setLocked(name, binding)
		e.mu.Unlock()
		return nil
	}
	outer := e.outer
	e.mu.Unlock()
	if outer != nil {
		return outer.Assign(name, value)
	}
	return fmt.Errorf("%q is not declared", name)
}

func (e *Environment) Get(name string) (Value, bool) {
	e.mu.RLock()
	if binding, exists := e.lookupLocked(name); exists {
		e.mu.RUnlock()
		return binding.Value, true
	}
	outer := e.outer
	e.mu.RUnlock()
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
	e.mu.Lock()
	if e.deleteLocked(name) {
		if e.typeBindings != nil {
			delete(e.typeBindings, name)
		}
		e.mu.Unlock()
		return true
	}
	outer := e.outer
	e.mu.Unlock()
	if outer != nil {
		return outer.Delete(name)
	}
	return false
}

// DefineTypeAlias binds a `type` alias in this scope; names are case-insensitive.
func (e *Environment) DefineTypeAlias(name string, target *ast.TypeRef) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.typeAliases == nil {
		e.typeAliases = map[string]*ast.TypeRef{}
	}
	e.typeAliases[strings.ToLower(name)] = target
}

// GetTypeAlias resolves a `type` alias through the enclosing scopes.
func (e *Environment) GetTypeAlias(name string) (*ast.TypeRef, bool) {
	key := strings.ToLower(name)
	for env := e; env != nil; {
		env.mu.RLock()
		target, ok := env.typeAliases[key]
		outer := env.outer
		env.mu.RUnlock()
		if ok {
			return target, true
		}
		env = outer
	}
	return nil, false
}

func (e *Environment) DefineTypeBinding(name, typeName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
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
	e.mu.RLock()
	if e.typeBindings != nil {
		if t, ok := e.typeBindings[name]; ok {
			e.mu.RUnlock()
			return t, true
		}
	}
	outer := e.outer
	e.mu.RUnlock()
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
		env.mu.RLock()
		for name := range env.typeBindings {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		env.mu.RUnlock()
	}
	return names
}

func (e *Environment) Names() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
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
		env.mu.RLock()
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
		env.mu.RUnlock()
	}
	sort.Strings(names)
	return names
}
