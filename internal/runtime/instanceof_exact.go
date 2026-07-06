package runtime

import "strings"

// Module-exact instanceof: match by (declaring module, name) so a same-named type from another module does not match.

func moduleIdentityEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

// InstanceClassMatchesExact walks the parent chain for a class or implemented-interface declaration matching (module, name).
func InstanceClassMatchesExact(class *Class, module, name string) bool {
	for c := class; c != nil; c = c.Parent {
		if strings.EqualFold(c.Name, name) && moduleIdentityEqual(c.DeclaringModule(), module) {
			return true
		}
		for _, iface := range c.Implements {
			if InterfaceMatchesExact(iface, module, name) {
				return true
			}
		}
	}
	return false
}

// InterfaceMatchesExact reports whether iface or any parent matches (module, name) exactly.
func InterfaceMatchesExact(iface *Interface, module, name string) bool {
	if iface == nil {
		return false
	}
	// The VM may store a qualified Implements name; compare the last segment.
	ifaceName := iface.Name
	if dot := strings.LastIndexByte(ifaceName, '.'); dot >= 0 {
		ifaceName = ifaceName[dot+1:]
	}
	if strings.EqualFold(ifaceName, name) && moduleIdentityEqual(iface.Module, module) {
		return true
	}
	for _, parent := range iface.Parents {
		if InterfaceMatchesExact(parent, module, name) {
			return true
		}
	}
	return false
}
