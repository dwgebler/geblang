package bytecode

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/parser"
	"geblang/internal/typealias"
)

type moduleTypeAliasLookup interface {
	ModuleTypeAlias(module, name string) (string, bool)
}

type typeAliasKey struct {
	mod  *ModuleContext
	name string
}

type typeAliasEntry struct {
	expanded string
	spec     *vmTypeSpec
}

func (vm *VM) importScopeChunk() *Chunk {
	if vm.curMod != nil {
		return &vm.curMod.Chunk
	}
	return &vm.chunk
}

// typeAliasTarget resolves one alias reference as written in the current module.
func (vm *VM) typeAliasTarget(name string) (string, bool) {
	lookup, ok := vm.moduleLoader.(moduleTypeAliasLookup)
	if !ok {
		return "", false
	}
	chunk := vm.importScopeChunk()
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		prefix := name[:dot]
		if canonical, imported := chunk.ModuleAliases[prefix]; imported {
			prefix = canonical
		}
		return lookup.ModuleTypeAlias(prefix, name[dot+1:])
	}
	if target, imported := chunk.FromImports[name]; imported {
		if dot := strings.LastIndexByte(target, '.'); dot > 0 {
			return lookup.ModuleTypeAlias(target[:dot], target[dot+1:])
		}
	}
	return "", false
}

func (vm *VM) mayNameTypeAlias(name string) bool {
	if vm.moduleLoader == nil {
		return false
	}
	return strings.IndexByte(name, '.') >= 0 || len(vm.importScopeChunk().FromImports) > 0
}

func (vm *VM) typeAliasEntry(name string) typeAliasEntry {
	key := typeAliasKey{mod: vm.curMod, name: name}
	if entry, ok := vm.typeAliasCache[key]; ok {
		return entry
	}
	entry := typeAliasEntry{expanded: name}
	if vm.mayNameTypeAlias(name) {
		if expanded := typealias.ExpandString(name, vm.typeAliasTarget); expanded != name {
			spec := parseVMTypeSpec(expanded)
			entry = typeAliasEntry{expanded: expanded, spec: &spec}
		}
	}
	if vm.typeAliasCache == nil {
		vm.typeAliasCache = map[typeAliasKey]typeAliasEntry{}
	}
	vm.typeAliasCache[key] = entry
	return entry
}

// expandTypeAliases rewrites exported alias references in a type string to their targets.
func (vm *VM) expandTypeAliases(typ string) string {
	if !vm.mayNameTypeAlias(typ) {
		return typ
	}
	return vm.typeAliasEntry(typ).expanded
}

func (vm *VM) aliasedTypeSpec(spec vmTypeSpec) (vmTypeSpec, bool) {
	if spec.kind != vmTypeOther || !vm.mayNameTypeAlias(spec.raw) {
		return spec, false
	}
	entry := vm.typeAliasEntry(spec.raw)
	if entry.spec == nil {
		return spec, false
	}
	return *entry.spec, true
}

// ResolveClassTypeAliases rewrites alias references in class metadata and function signatures to their targets.
func ResolveClassTypeAliases(chunk *Chunk, lookup typealias.Lookup) {
	if lookup == nil {
		return
	}
	for i := range chunk.Functions {
		function := &chunk.Functions[i]
		var params []string
		for j, typ := range function.ParamTypes {
			expanded := typealias.ExpandString(typ, lookup)
			if expanded != typ && params == nil {
				params = append([]string(nil), function.ParamTypes...)
			}
			if params != nil {
				params[j] = expanded
			}
		}
		if params != nil {
			function.ParamTypes = params
		}
		function.ReturnType = typealias.ExpandString(function.ReturnType, lookup)
		function.ReturnCheckType = typealias.ExpandString(function.ReturnCheckType, lookup)
	}
	for i := range chunk.Classes {
		class := &chunk.Classes[i]
		if class.ParentName != "" {
			class.ParentName = typealias.ExpandString(class.ParentName, lookup)
		}
		var implements []string
		for j, iface := range class.Implements {
			expanded := typealias.ExpandString(iface, lookup)
			if expanded != iface && implements == nil {
				implements = append([]string(nil), class.Implements...)
			}
			if implements != nil {
				implements[j] = expanded
			}
		}
		if implements != nil {
			class.Implements = implements
		}
	}
}

func (vm *VM) expandInstanceofTarget(target string) string {
	module, name, exact := parseExactInstanceofTarget(target)
	if !exact {
		return vm.expandTypeAliases(target)
	}
	lookup, ok := vm.moduleLoader.(moduleTypeAliasLookup)
	if !ok {
		return target
	}
	if module == "" {
		module = vm.moduleName
	}
	aliasTarget, ok := lookup.ModuleTypeAlias(module, name)
	if !ok {
		return target
	}
	expanded := vm.expandTypeAliases(aliasTarget)
	if !strings.ContainsAny(expanded, "<|&?") {
		if targetModule, targetName, qualified := splitQualifiedClassName(expanded); qualified {
			return encodeExactInstanceofTarget(targetModule, targetName)
		}
	}
	return expanded
}

func (c *Compiler) expandRecordedTypeAlias(typ string) string {
	if c.typeAliasLookup == nil || typ == "" {
		return typ
	}
	expanded := typealias.ExpandString(typ, c.typeAliasLookup)
	if expanded != typ {
		if c.chunk.TypeAliasDeps == nil {
			c.chunk.TypeAliasDeps = map[string]string{}
		}
		c.chunk.TypeAliasDeps[typ] = expanded
	}
	return expanded
}

// TypeAliasDepsFresh reports whether every alias expanded when chunk was compiled still expands the same way.
func TypeAliasDepsFresh(chunk Chunk, lookup typealias.Lookup) bool {
	for name, expanded := range chunk.TypeAliasDeps {
		if lookup == nil || typealias.ExpandString(name, lookup) != expanded {
			return false
		}
	}
	return true
}

// resolvedDeclarationType expands local and imported aliases so alias-typed declarations get element checks.
func (c *Compiler) resolvedDeclarationType(typ *ast.TypeRef) *ast.TypeRef {
	if typ == nil {
		return nil
	}
	resolved := c.resolveTypeRef(typ)
	if expanded := c.expandRecordedTypeAlias(resolved.String()); expanded != resolved.String() {
		if parsed, err := parser.ParseTypeRef(expanded); err == nil {
			return parsed
		}
	}
	return resolved
}
