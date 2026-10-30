package runtime

import "strings"

func QualifiedTypeName(module, name string) string {
	if module == "" {
		return name
	}
	return module + "." + name
}

// ReifiedTypeName preserves nominal identity when inference stores a type argument.
func ReifiedTypeName(value Value) string {
	switch v := value.(type) {
	case *Instance:
		if v.Class == nil {
			return value.TypeName()
		}
		name := QualifiedTypeName(v.Class.DeclaringModule(), v.Class.Name)
		if len(v.Class.TypeParameters) > 0 {
			args := make([]string, 0, len(v.Class.TypeParameters))
			for _, param := range v.Class.TypeParameters {
				bound, ok := v.TypeBindings[param]
				if !ok {
					return name
				}
				args = append(args, bound)
			}
			name += "<" + strings.Join(args, ",") + ">"
		}
		return name
	case EnumVariant:
		return QualifiedTypeName(v.Enum.Module, v.Enum.Name)
	}
	return value.TypeName()
}
