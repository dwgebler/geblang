package evaluator

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// literalFieldValue types an integer literal by its target field, as declarations do.
func literalFieldValue(instance *runtime.Instance, name string, value runtime.Value) runtime.Value {
	switch value.(type) {
	case runtime.Int, runtime.SmallInt:
	default:
		return value
	}
	if checkFieldWrite(instance, name, value) == nil {
		return value
	}
	for _, target := range []string{"float", "decimal"} {
		if converted, err := castValue(value, target); err == nil && checkFieldWrite(instance, name, converted) == nil {
			return converted
		}
	}
	return value
}

// fieldCheck is the resolved declared type of one field on one class.
type fieldCheck struct {
	declaring *runtime.Class
	declared  *ast.TypeRef
	params    map[string]bool
	kind      string
	tags      []string
}

func resolveFieldCheck(class *runtime.Class, name string) *fieldCheck {
	if class.FieldChecks != nil {
		if cached, ok := class.FieldChecks.Load(name); ok {
			return cached.(*fieldCheck)
		}
	}
	check := &fieldCheck{}
	for c := class; c != nil; c = c.Parent {
		found := false
		for _, field := range c.Fields {
			if field.Name != name {
				continue
			}
			found = true
			declared := field.CheckType
			if declared == nil {
				declared = field.Type
			}
			if declared != nil && !(declared.Operator == "" && declared.Name == "any" && !declared.Nullable) {
				check.declaring = c
				check.declared = declared
				if len(c.TypeParameters) > 0 && typeRefUsesTypeParameter(declared, lowerNameSet(c.TypeParameters)) {
					check.params = lowerNameSet(c.TypeParameters)
				}
				check.kind, check.tags = collectionTagsFor(declared)
			}
			break
		}
		if found {
			break
		}
	}
	if class.FieldChecks == nil {
		return check
	}
	actual, _ := class.FieldChecks.LoadOrStore(name, check)
	return actual.(*fieldCheck)
}

// checkFieldWrite enforces a declared field type on a runtime write; untyped and undeclared fields pass.
func checkFieldWrite(instance *runtime.Instance, name string, value runtime.Value) error {
	if instance == nil || instance.Class == nil {
		return nil
	}
	check := resolveFieldCheck(instance.Class, name)
	declared := check.declared
	if declared == nil || primitiveFieldMatch(declared, value) || collectionTagsMatch(check.kind, check.tags, value) {
		return nil
	}
	var bindings map[string]string
	if check.params != nil {
		bindings = runtime.DeclaringClassBindings(instance, check.declaring)
	}
	if matchValueToTypeRefWith(check.params, bindings, value, declared) {
		return nil
	}
	return fieldMismatchError(value, declared, instance.Class.Name, name)
}

func fieldMismatchError(value runtime.Value, declared *ast.TypeRef, className, name string) error {
	suffix := collectionMismatchSuffix(value, declared)
	got := descriptiveTypeName(value)
	if suffix != "" {
		got = value.TypeName()
	}
	return runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("cannot assign %s to %s field %s.%s%s", got, declared.String(), className, name, suffix)}
}

// checkStaticWrite enforces a typed static field's declared type.
func checkStaticWrite(class *runtime.Class, name string, value runtime.Value) error {
	declared := class.StaticTypes[name]
	if declared == nil || matchValueToTypeRef(nil, value, declared) {
		return nil
	}
	return fieldMismatchError(value, declared, class.Name, name)
}

// literalStaticValue types an integer literal by its target static field.
func literalStaticValue(class *runtime.Class, name string, value runtime.Value) runtime.Value {
	switch value.(type) {
	case runtime.Int, runtime.SmallInt:
	default:
		return value
	}
	if checkStaticWrite(class, name, value) == nil {
		return value
	}
	for _, target := range []string{"float", "decimal"} {
		if converted, err := castValue(value, target); err == nil && checkStaticWrite(class, name, converted) == nil {
			return converted
		}
	}
	return value
}

// primitiveFieldMatch accepts the plain primitive matches without the general matcher; false means "check fully".
func primitiveFieldMatch(declared *ast.TypeRef, value runtime.Value) bool {
	if declared.Operator != "" || len(declared.Arguments) > 0 || declared.ListAlias {
		return false
	}
	switch declared.Name {
	case "int":
		switch value.(type) {
		case runtime.Int, runtime.SmallInt:
			return true
		}
	case "string":
		_, ok := value.(runtime.String)
		return ok
	case "bool":
		_, ok := value.(runtime.Bool)
		return ok
	case "float":
		_, ok := value.(runtime.Float)
		return ok
	case "decimal":
		_, ok := value.(runtime.Decimal)
		return ok
	}
	return false
}

func collectionTagsFor(declared *ast.TypeRef) (string, []string) {
	if declared.Operator != "" || declared.ListAlias {
		return "", nil
	}
	kind := strings.ToLower(declared.Name)
	if kind != "list" && kind != "set" && kind != "dict" {
		return "", nil
	}
	var tags []string
	for _, arg := range declared.Arguments {
		tags = append(tags, elementTagName(arg))
	}
	return kind, tags
}

// collectionTagsMatch accepts a collection whose element-type tag already equals the declared one; tags are write-barrier enforced.
func collectionTagsMatch(kind string, want []string, value runtime.Value) bool {
	if len(want) == 0 {
		return false
	}
	var tags []string
	switch v := value.(type) {
	case *runtime.List:
		if kind != "list" {
			return false
		}
		tags = v.ElementTypes
	case runtime.Set:
		if kind != "set" {
			return false
		}
		tags = v.ElementTypes
	case runtime.Dict:
		if kind != "dict" {
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

func lowerNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = true
	}
	return set
}
