package runtime

import "strings"

// ParameterClassNames returns the class a parameter type names and, for list<T>, set<T> and T[], T's class name.
func ParameterClassNames(typ string) (class string, element string) {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typ), "?"))
	if inner, ok := strings.CutSuffix(t, "[]"); ok {
		return "", classNameOf(inner)
	}
	for _, prefix := range []string{"list<", "set<"} {
		if strings.HasPrefix(t, prefix) && strings.HasSuffix(t, ">") {
			return "", classNameOf(t[len(prefix) : len(t)-1])
		}
	}
	return classNameOf(t), ""
}

var builtinGenericTypes = map[string]bool{"list": true, "set": true, "dict": true}

func classNameOf(typ string) string {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typ), "?"))
	if i := strings.IndexByte(t, '<'); i >= 0 {
		if !strings.HasSuffix(t, ">") {
			return ""
		}
		t = strings.TrimSpace(t[:i])
	}
	if t == "" || builtinGenericTypes[t] || strings.ContainsAny(t, "|&(),<>[] ") {
		return ""
	}
	return t
}

// InheritedFields returns the ancestors' fields that class and nearer ancestors do not redeclare, nearest first.
func InheritedFields(class *Class) []Field {
	if class == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, field := range class.Fields {
		seen[field.Name] = true
	}
	var out []Field
	for c := class.Parent; c != nil; c = c.Parent {
		for _, field := range c.Fields {
			if !seen[field.Name] {
				seen[field.Name] = true
				out = append(out, field)
			}
		}
	}
	return out
}
