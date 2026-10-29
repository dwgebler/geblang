package runtime

import (
	"regexp"
	"strings"
)

var typeNamePattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// DeclaringClassBindings threads extends-clause arguments up from the instance to resolve an ancestor's bindings.
func DeclaringClassBindings(instance *Instance, declaring *Class) map[string]string {
	if instance == nil || instance.Class == nil || declaring == nil {
		return nil
	}
	if declaring == instance.Class {
		return instance.TypeBindings
	}
	bindings := ownBindings(instance.Class, instance.TypeBindings)
	for current := instance.Class; current != nil && current != declaring; current = current.Parent {
		parent := current.Parent
		if parent == nil {
			return nil
		}
		next := map[string]string{}
		for i, param := range parent.TypeParameters {
			if i >= len(current.ParentArguments) {
				break
			}
			if bound, ok := substituteTypeParams(strings.TrimSpace(current.ParentArguments[i]), current, bindings); ok {
				next[param] = bound
			}
		}
		bindings = next
	}
	return bindings
}

func ownBindings(class *Class, all map[string]string) map[string]string {
	own := map[string]string{}
	for _, param := range class.TypeParameters {
		if bound, ok := lookupBinding(all, param); ok {
			own[param] = bound
		}
	}
	return own
}

func lookupBinding(bindings map[string]string, name string) (string, bool) {
	if bound, ok := bindings[name]; ok && bound != "" {
		return bound, true
	}
	for key, bound := range bindings {
		if strings.EqualFold(key, name) && bound != "" {
			return bound, true
		}
	}
	return "", false
}

func isTypeParameter(class *Class, name string) bool {
	for _, param := range class.TypeParameters {
		if strings.EqualFold(param, name) {
			return true
		}
	}
	return false
}

// substituteTypeParams rewrites class's type parameters inside arg; ok is false when one is unbound.
func substituteTypeParams(arg string, class *Class, bindings map[string]string) (string, bool) {
	if isPlainIdentifier(arg) {
		if !isTypeParameter(class, arg) {
			return arg, true
		}
		return lookupBinding(bindings, arg)
	}
	ok := true
	out := typeNamePattern.ReplaceAllStringFunc(arg, func(name string) string {
		if !isTypeParameter(class, name) {
			return name
		}
		bound, found := lookupBinding(bindings, name)
		if !found {
			ok = false
			return name
		}
		return bound
	})
	return out, ok
}

// ResolveInheritedTypeBindings rebinds ancestors' type parameters through each extends clause from the instance's own bindings.
func ResolveInheritedTypeBindings(instance *Instance) {
	if instance == nil || instance.Class == nil || instance.Class.Parent == nil {
		return
	}
	owned := map[string]bool{}
	for _, param := range instance.Class.TypeParameters {
		owned[strings.ToLower(param)] = true
	}
	bindings := ownBindings(instance.Class, instance.TypeBindings)
	for current := instance.Class; current.Parent != nil; current = current.Parent {
		parent := current.Parent
		next := map[string]string{}
		for i, param := range parent.TypeParameters {
			if i >= len(current.ParentArguments) {
				break
			}
			bound, ok := substituteTypeParams(strings.TrimSpace(current.ParentArguments[i]), current, bindings)
			if !ok {
				continue
			}
			next[param] = bound
			if !owned[strings.ToLower(param)] {
				if instance.TypeBindings == nil {
					instance.TypeBindings = map[string]string{}
				}
				instance.TypeBindings[param] = bound
				owned[strings.ToLower(param)] = true
			}
		}
		bindings = next
	}
}

func isPlainIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, ch := range s {
		if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (i > 0 && ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}
