package runtime

import "strings"

// tagIdentitySep joins an element tag's written type to its declaring module; type names never contain it.
const tagIdentitySep = "@"

func IdentityTag(written, module string) string {
	return written + tagIdentitySep + module
}

func SplitIdentityTag(tag string) (written, module string, ok bool) {
	written, module, ok = strings.Cut(tag, tagIdentitySep)
	return written, module, ok
}

func ElementTagDisplay(tag string) string {
	if written, _, ok := SplitIdentityTag(tag); ok {
		return written
	}
	return tag
}

func SplitCanonicalTypeName(canonical string) (module, name string) {
	if dot := strings.LastIndexByte(canonical, '.'); dot >= 0 {
		return canonical[:dot], canonical[dot+1:]
	}
	return "", canonical
}

func EnumMatchesExact(enum *EnumDef, module, name string) bool {
	if enum == nil {
		return false
	}
	if strings.EqualFold(enum.Name, name) && moduleIdentityEqual(enum.Module, module) {
		return true
	}
	for _, iface := range enum.Interfaces() {
		if InterfaceMatchesExact(iface, module, name) {
			return true
		}
	}
	return false
}

func EnumImplements(enum *EnumDef, name string) bool {
	if enum == nil {
		return false
	}
	if ifaces := enum.Interfaces(); ifaces != nil {
		for _, iface := range ifaces {
			if InterfaceMatches(iface, name) {
				return true
			}
		}
		return false
	}
	for _, iface := range enum.Implements {
		if strings.EqualFold(iface, name) || strings.EqualFold(bareTypeName(iface), bareTypeName(name)) {
			return true
		}
	}
	return false
}

func ValueIsNominalType(value Value, module, name string) bool {
	switch v := value.(type) {
	case *Instance:
		if InstanceClassMatchesExact(v.Class, module, name) {
			return true
		}
		for _, extra := range v.ExtraTypeNames {
			if strings.EqualFold(bareTypeName(extra), name) {
				return true
			}
		}
		return false
	case EnumVariant:
		return EnumMatchesExact(v.Enum, module, name)
	case Error:
		return ErrorValueIsA(v, name, strings.EqualFold)
	}
	return false
}

// QualifySameNamedTypes spells out both modules when a rejected value's type shares the declared type's bare name.
func QualifySameNamedTypes(declared, declaredModule string, value Value) (string, string, bool) {
	var valueModule, valueName string
	switch v := value.(type) {
	case *Instance:
		valueModule, valueName = v.Class.DeclaringModule(), v.Class.Name
	case EnumVariant:
		valueModule, valueName = v.Enum.Module, v.Enum.Name
	default:
		return declared, "", false
	}
	bare := bareTypeName(strings.TrimPrefix(declared, "?"))
	if !strings.EqualFold(bare, valueName) || moduleIdentityEqual(declaredModule, valueModule) {
		return declared, "", false
	}
	return QualifiedTypeName(declaredModule, bare), QualifiedTypeName(valueModule, valueName), true
}

func ElementTagFor(value Value, tag string) string {
	if declared, _, same := sameNamedTag(value, tag); same {
		return declared
	}
	return ElementTagDisplay(tag)
}

func ElementValueName(value Value, tag string) string {
	if _, got, same := sameNamedTag(value, tag); same {
		return got
	}
	return value.TypeName()
}

func sameNamedTag(value Value, tag string) (string, string, bool) {
	nullable := strings.HasPrefix(tag, "?")
	written, module, ok := SplitIdentityTag(strings.TrimPrefix(tag, "?"))
	if !ok {
		return "", "", false
	}
	declared, got, same := QualifySameNamedTypes(written, module, value)
	if same && nullable {
		declared = "?" + declared
	}
	return declared, got, same
}
