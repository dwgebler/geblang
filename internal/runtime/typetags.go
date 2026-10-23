package runtime

import "strings"

// ClassImplementsInterface walks the class parent chain and each
// class's interface graph for a name match.
func ClassImplementsInterface(class *Class, name string) bool {
	for current := class; current != nil; current = current.Parent {
		for _, iface := range current.Implements {
			if InterfaceMatches(iface, name) {
				return true
			}
		}
	}
	return false
}

// InterfaceMatches reports whether iface or any of its parents has name.
// The VM records an imported interface under its module-qualified name
// (shapes.Shape) while element tags and hierarchy leaves are bare (Shape),
// so a qualified side is compared by its bare name, as class names are.
func InterfaceMatches(iface *Interface, name string) bool {
	if iface == nil {
		return false
	}
	if strings.EqualFold(iface.Name, name) || strings.EqualFold(bareTypeName(iface.Name), bareTypeName(name)) {
		return true
	}
	for _, parent := range iface.Parents {
		if InterfaceMatches(parent, name) {
			return true
		}
	}
	return false
}

// ValueSatisfiesHierarchyLeaf reports whether value's class hierarchy
// (parent chain, implemented interfaces, error parent names) satisfies
// the bare type name leaf. Both backends' element-tag write barriers
// consult this after the name-level check misses.
func ValueSatisfiesHierarchyLeaf(value Value, leaf string) bool {
	switch v := value.(type) {
	case *Instance:
		for c := v.Class; c != nil; c = c.Parent {
			if strings.EqualFold(c.Name, leaf) || ClassImplementsInterface(c, leaf) {
				return true
			}
		}
	case Error:
		if strings.EqualFold(v.Class, leaf) {
			return true
		}
		for _, p := range v.Parents {
			if strings.EqualFold(p, leaf) {
				return true
			}
		}
	case EnumVariant:
		if v.Enum != nil {
			if strings.EqualFold(v.Enum.Name, leaf) {
				return true
			}
			for _, iface := range v.Enum.Implements {
				if strings.EqualFold(iface, leaf) {
					return true
				}
			}
		}
	}
	return false
}

// bareTypeName drops a module qualifier: shapes.Shape -> Shape.
func bareTypeName(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		return name[dot+1:]
	}
	return name
}
