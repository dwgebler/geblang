package bytecode

import (
	"strings"

	"geblang/internal/runtime"
)

type nominalID struct {
	module, name string
	ok           bool
}

func (vm *VM) nominalIdentity(written string) (string, string, bool) {
	if id, cached := vm.nominalIDs[written]; cached {
		return id.module, id.name, id.ok
	}
	id := vm.resolveNominalIdentity(written)
	if vm.nominalIDs == nil {
		vm.nominalIDs = map[string]nominalID{}
	}
	vm.nominalIDs[written] = id
	return id.module, id.name, id.ok
}

func (vm *VM) resolveNominalIdentity(name string) nominalID {
	if name == "" || runtime.IsBuiltinTypeName(name) || strings.ContainsAny(name, "<>|&?") {
		return nominalID{}
	}
	if expanded := vm.expandTypeAliases(name); expanded != name {
		if strings.ContainsAny(expanded, "<>|&?") {
			return nominalID{}
		}
		module, bare := runtime.SplitCanonicalTypeName(expanded)
		return vm.exportedNominal(module, bare)
	}
	chunk := vm.importScopeChunk()
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		prefix := name[:dot]
		if canonical, ok := chunk.ModuleAliases[prefix]; ok {
			prefix = canonical
		}
		return vm.exportedNominal(prefix, name[dot+1:])
	}
	if target, ok := chunk.FromImports[name]; ok {
		module, bare := runtime.SplitCanonicalTypeName(target)
		return vm.exportedNominal(module, bare)
	}
	if vm.declaresNominalType(name) {
		return nominalID{module: vm.moduleName, name: name, ok: true}
	}
	return nominalID{}
}

// A non-type export, such as a factory function used as a type, has no identity.
func (vm *VM) exportedNominal(module, member string) nominalID {
	if module == "" || vm.moduleLoader == nil {
		return nominalID{}
	}
	loaded, err := vm.moduleLoader.LoadModule(module, module)
	if err != nil || loaded == nil {
		return nominalID{}
	}
	value, ok := loaded.Exports[member]
	if !ok {
		return nominalID{}
	}
	switch v := value.(type) {
	case runtime.BytecodeClass:
		return nominalID{module: v.Module, name: v.Name, ok: true}
	case *runtime.Class:
		return nominalID{module: v.DeclaringModule(), name: v.Name, ok: true}
	case *runtime.Interface:
		return nominalID{module: v.Module, name: v.Name, ok: true}
	case *runtime.EnumDef:
		return nominalID{module: v.Module, name: v.Name, ok: true}
	}
	return nominalID{}
}

func (vm *VM) declaresNominalType(name string) bool {
	if _, ok := vm.classInfo(name); ok {
		return true
	}
	if _, ok := vm.lookupInterfaceInfo(name); ok {
		return true
	}
	for _, constant := range vm.curMod.Chunk.Constants {
		if enum, ok := constant.(*runtime.EnumDef); ok && strings.EqualFold(enum.Name, name) {
			return true
		}
	}
	return false
}

func (vm *VM) identityElementTags(spec vmTypeSpec, n int) []string {
	key := spec.raw
	if cached, ok := vm.identityTags[key]; ok && len(cached) == n {
		return cached
	}
	tags := spec.elementTags(n)
	var out []string
	for i := 0; i < n && i < len(spec.args); i++ {
		arg := spec.args[i]
		if arg.kind != vmTypeOther || len(arg.args) > 0 {
			continue
		}
		module, _, ok := vm.nominalIdentity(arg.base)
		if !ok {
			continue
		}
		if out == nil {
			out = append([]string(nil), tags...)
		}
		written := strings.TrimPrefix(tags[i], "?")
		out[i] = runtime.IdentityTag(written, module)
		if strings.HasPrefix(tags[i], "?") {
			out[i] = "?" + out[i]
		}
	}
	if out == nil {
		out = tags
	}
	if vm.identityTags == nil {
		vm.identityTags = map[string][]string{}
	}
	vm.identityTags[key] = out
	return out
}

func (vm *VM) bindEnum(enum *runtime.EnumDef) {
	if enum.InterfacesBound() {
		return
	}
	if enum.Module == "" {
		enum.Module = vm.moduleName
	}
	ifaces := make([]*runtime.Interface, 0, len(enum.Implements))
	for _, name := range enum.Implements {
		if iface := vm.buildRuntimeInterface(name, map[string]bool{}); iface != nil {
			ifaces = append(ifaces, iface)
		}
	}
	enum.BindInterfaces(ifaces)
}

func (vm *VM) disambiguateParamTypes(written, declared, gotName, suffix string, arg runtime.Value) (string, string) {
	if suffix != "" {
		return declared, gotName
	}
	module, _, ok := vm.nominalIdentity(written)
	if !ok {
		return declared, gotName
	}
	if d, g, same := runtime.QualifySameNamedTypes(declared, module, arg); same {
		return d, g
	}
	return declared, gotName
}
