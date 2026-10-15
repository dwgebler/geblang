package bytecode

import (
	"fmt"
	"regexp"
	"strings"

	"geblang/internal/runtime"
)

// setFieldIntLiteral marks an OpSetField whose value is an integer literal, typed by the target field.
const setFieldIntLiteral = 1

func (vm *VM) literalFieldValue(instance *runtime.Instance, name string, value runtime.Value) runtime.Value {
	switch value.(type) {
	case runtime.Int, runtime.SmallInt:
	default:
		return value
	}
	if _, ok := vm.fieldWriteMismatch(instance, name, value); ok {
		return value
	}
	for _, target := range []string{"float", "decimal"} {
		if converted, err := castValue(value, target); err == nil {
			if _, ok := vm.fieldWriteMismatch(instance, name, converted); ok {
				return converted
			}
		}
	}
	return value
}

// fieldTypeCache memoises the last field type resolved for a write, the common hot loop shape.
type fieldTypeKey struct {
	module string
	class  string
}

// fieldTypeList holds one class's resolved field types; classes have few fields, so a scan beats hashing.
type fieldTypeList struct {
	names   []string
	entries []*fieldTypeCache
}

// fieldTypeCache is the resolved declared type of one field; depth locates the declaring class in the instance's chain.
type fieldTypeCache struct {
	depth      int
	declared   string
	spec       vmTypeSpec
	typeParams map[string]bool
	tags       []string
}

func (vm *VM) resolveFieldType(class *runtime.Class, name string) *fieldTypeCache {
	if vm.fieldTypesClass != class {
		classKey := fieldTypeKey{module: class.Module, class: class.Name}
		fields, ok := vm.fieldTypes[classKey]
		if !ok {
			fields = &fieldTypeList{}
			if vm.fieldTypes == nil {
				vm.fieldTypes = map[fieldTypeKey]*fieldTypeList{}
			}
			vm.fieldTypes[classKey] = fields
		}
		vm.fieldTypesClass = class
		vm.fieldTypesFields = fields
	}
	for i := range vm.fieldTypesFields.names {
		if vm.fieldTypesFields.names[i] == name {
			return vm.fieldTypesFields.entries[i]
		}
	}
	cache := &fieldTypeCache{}
	depth := 0
	for c := class; c != nil; c = c.Parent {
		if field, ok := declaredField(c, name); ok {
			declared := field.TypeName
			if declared == "" && field.Type != nil {
				declared = field.Type.String()
			}
			if declared != "" && declared != "any" {
				cache.depth = depth
				cache.declared = declared
				cache.spec = vm.typeSpec(vm.canonicalTypeQualifiers(declared, c.Module))
				if params := classTypeParamSet(c); typeMentionsParams(declared, params) {
					cache.typeParams = params
				}
				for _, arg := range cache.spec.args {
					cache.tags = append(cache.tags, elementSpecTag(arg))
				}
			}
			break
		}
		depth++
	}
	vm.fieldTypesFields.names = append(vm.fieldTypesFields.names, name)
	vm.fieldTypesFields.entries = append(vm.fieldTypesFields.entries, cache)
	return cache
}

func declaredField(class *runtime.Class, name string) (runtime.Field, bool) {
	for _, field := range class.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return runtime.Field{}, false
}

func ancestorAt(class *runtime.Class, depth int) *runtime.Class {
	for ; depth > 0 && class != nil; depth-- {
		class = class.Parent
	}
	return class
}

// fieldWriteMismatch reports a TypeError message when value violates the field's declared type.
func (vm *VM) fieldWriteMismatch(instance *runtime.Instance, name string, value runtime.Value) (string, bool) {
	if instance == nil || instance.Class == nil {
		return "", true
	}
	field := vm.resolveFieldType(instance.Class, name)
	if field.declared == "" || primitiveFieldMatch(field.spec.kind, value) || collectionTagMatch(field.spec.kind, field.tags, value) {
		return "", true
	}
	var bindings map[string]string
	if field.typeParams != nil {
		bindings = runtime.DeclaringClassBindings(instance, ancestorAt(instance.Class, field.depth))
	}
	if vm.matchValueToTypeSpecWith(field.typeParams, bindings, value, field.spec) {
		return "", true
	}
	return vm.fieldMismatchMessage(value, field.declared, instance.Class.Name, name), false
}

func (vm *VM) fieldMismatchMessage(value runtime.Value, declared, className, name string) string {
	declared = vm.expandTypeAliases(declared)
	suffix := vm.collectionMismatchSuffixStr(value, declared)
	got := vm.descriptiveRuntimeTypeName(value)
	if suffix != "" {
		got = value.TypeName()
	}
	return fmt.Sprintf("cannot assign %s to %s field %s.%s%s", got, declared, className, name, suffix)
}

// primitiveFieldMatch accepts the plain primitive matches without the general matcher; false means "check fully".
func primitiveFieldMatch(kind vmTypeKind, value runtime.Value) bool {
	switch kind {
	case vmTypeInt:
		switch value.(type) {
		case runtime.SmallInt, runtime.Int:
			return true
		}
	case vmTypeString:
		_, ok := value.(runtime.String)
		return ok
	case vmTypeBool:
		_, ok := value.(runtime.Bool)
		return ok
	case vmTypeFloat:
		_, ok := value.(runtime.Float)
		return ok
	case vmTypeDecimal:
		_, ok := value.(runtime.Decimal)
		return ok
	}
	return false
}

// collectionTagMatch accepts a collection whose element-type tag already equals the declared one; tags are write-barrier enforced.
func collectionTagMatch(kind vmTypeKind, want []string, value runtime.Value) bool {
	if len(want) == 0 {
		return false
	}
	var tags []string
	switch v := value.(type) {
	case *runtime.List:
		if kind != vmTypeList {
			return false
		}
		tags = v.ElementTypes
	case runtime.Set:
		if kind != vmTypeSet {
			return false
		}
		tags = v.ElementTypes
	case runtime.Dict:
		if kind != vmTypeDict {
			return false
		}
		tags = v.ElementTypes
	default:
		return false
	}
	if len(tags) != len(want) {
		return false
	}
	for i := range want {
		if !strings.EqualFold(tags[i], want[i]) {
			return false
		}
	}
	return true
}

func typeMentionsParams(typ string, params map[string]bool) bool {
	if len(params) == 0 {
		return false
	}
	for _, name := range typeNameWords.FindAllString(typ, -1) {
		if params[strings.ToLower(name)] {
			return true
		}
	}
	return false
}

var typeNameWords = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func classTypeParamSet(class *runtime.Class) map[string]bool {
	if len(class.TypeParameters) == 0 {
		return nil
	}
	names := make(map[string]bool, len(class.TypeParameters))
	for _, param := range class.TypeParameters {
		names[strings.ToLower(param)] = true
	}
	return names
}

type moduleStaticWriter interface {
	StaticSlotForModuleClass(module, className, name string) (string, string, bool)
	SetStaticValueForModuleClass(module, className, name string, value runtime.Value) bool
}

// setClassStatic writes a static through a class value, local or from another module, with the declared-type check.
func (vm *VM) setClassStatic(instruction Instruction, ip int, class runtime.BytecodeClass, name string, value runtime.Value, intLiteral bool) (int, error) {
	declared, owner, ownerModule, write := "", "", vm.moduleName, func() bool { return false }
	remoteModule, remoteClass := class.Module, class.Name
	if class.Module == vm.moduleName {
		remoteModule, remoteClass = "", ""
		if info, ok := vm.classInfo(class.Name); ok {
			for ci := info; ; {
				if idx, present := ci.StaticValues[name]; present && idx >= 0 && int(idx) < vm.constantsLen() {
					declared, owner = ci.StaticTypes[name], ci.Name
					write = func() bool { vm.writeStaticValue(idx, value); return true }
					break
				}
				if ci.ParentIndex < 0 || int(ci.ParentIndex) >= len(vm.curMod.Chunk.Classes) {
					if module, parent, ok := splitQualifiedClassName(ci.ParentName); ok {
						remoteModule, remoteClass = module, parent
					}
					break
				}
				ci = vm.curMod.Chunk.Classes[ci.ParentIndex]
			}
		}
	}
	if owner == "" && remoteClass != "" {
		if writer, ok := vm.moduleLoader.(moduleStaticWriter); ok {
			if typ, ownerName, found := writer.StaticSlotForModuleClass(remoteModule, remoteClass, name); found {
				declared, owner, ownerModule = typ, ownerName, remoteModule
				write = func() bool { return writer.SetStaticValueForModuleClass(remoteModule, remoteClass, name, value) }
			}
		}
	}
	if owner == "" {
		return 0, vm.runtimeError(instruction, "unknown static member %s.%s", class.Name, name)
	}
	if declared != "" {
		if intLiteral {
			value = typedIntLiteralConstant(value, vm.expandTypeAliases(declared))
		}
		if !vm.matchValueToTypeStrWith(nil, nil, value, vm.canonicalTypeQualifiers(declared, ownerModule)) {
			return vm.throwTyped(instruction, ip, "TypeError", vm.fieldMismatchMessage(value, declared, owner, name))
		}
	}
	write()
	vm.push(value)
	return ip, nil
}

type moduleAliasSource interface {
	ModuleAliasesFor(module string) map[string]string
}

var qualifiedTypeName = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.`)

// canonicalTypeQualifiers rewrites import-alias qualifiers in typ to canonical modules, as module declares them.
func (vm *VM) canonicalTypeQualifiers(typ, module string) string {
	if !strings.Contains(typ, ".") {
		return typ
	}
	var aliases map[string]string
	if module == vm.moduleName {
		aliases = vm.importScopeChunk().ModuleAliases
	} else if source, ok := vm.moduleLoader.(moduleAliasSource); ok {
		aliases = source.ModuleAliasesFor(module)
	}
	if len(aliases) == 0 {
		return typ
	}
	return qualifiedTypeName.ReplaceAllStringFunc(typ, func(match string) string {
		if canonical, ok := aliases[strings.TrimSuffix(match, ".")]; ok {
			return canonical + "."
		}
		return match
	})
}
