package native

import (
	"strings"

	"geblang/internal/runtime"
)

// NestedKind classifies how a parseAs field type recurses into nested
// class instances.
type NestedKind int

const (
	NestedNone NestedKind = iota
	NestedSingle
	NestedList
	NestedDict
)

// FieldTypeInfo pairs a class field name with its declared type string
// (the ast.TypeRef.String() form, identical on both backends).
type FieldTypeInfo struct {
	Name string
	Type string
	// Class is the nested class resolved in the declaring module, when known; it wins over a name lookup.
	Class runtime.Value
}

// ParseNestedFieldType extracts the recursion shape and the candidate
// inner class name from a declared field-type string. It returns
// NestedNone for primitives, `any`, unions, sets, and anything outside
// a single / list / dict of one type. The caller decides whether the
// returned name actually resolves to a user class.
func ParseNestedFieldType(fieldType string) (NestedKind, string) {
	t := stripOptionalType(fieldType)
	if t == "" {
		return NestedNone, ""
	}
	if strings.HasSuffix(t, "[]") {
		return NestedList, simpleClassName(stripOptionalType(t[:len(t)-2]))
	}
	if inner, ok := genericArgs(t, "list"); ok && len(inner) == 1 {
		return NestedList, simpleClassName(stripOptionalType(inner[0]))
	}
	if inner, ok := genericArgs(t, "dict"); ok && len(inner) == 2 {
		return NestedDict, simpleClassName(stripOptionalType(inner[1]))
	}
	// Only a bare identifier is a candidate class; compound types are not.
	if strings.ContainsAny(t, "<>[]|, ") {
		return NestedNone, ""
	}
	name := simpleClassName(t)
	if builtinTypeNames[name] {
		return NestedNone, ""
	}
	return NestedSingle, name
}

// builtinTypeNames are lowercase engine type names; a single field of one
// of these never recurses, so the deserializer skips a class lookup. User
// classes are case-sensitive, so a class named `Set` is unaffected.
var builtinTypeNames = map[string]bool{
	"string": true, "int": true, "smallint": true, "float": true,
	"decimal": true, "bool": true, "bytes": true, "any": true,
	"void": true, "null": true, "never": true, "callable": true,
	"function": true, "set": true, "list": true, "dict": true, "range": true,
}

func stripOptionalType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "?")
	return strings.TrimSpace(t)
}

// simpleClassName drops a module qualifier (`addr.Address` -> `Address`);
// the class registries on both backends key by the simple name.
func simpleClassName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// genericArgs returns the top-level type arguments of `name<...>`.
func genericArgs(t, name string) ([]string, bool) {
	prefix := name + "<"
	if !strings.HasPrefix(t, prefix) || !strings.HasSuffix(t, ">") {
		return nil, false
	}
	return splitTopLevelCommas(t[len(prefix) : len(t)-1]), true
}

func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '[':
			depth++
		case '>', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// HydrateNestedClassFields returns `value` with each entry whose field
// type names a user class (or a list / dict of one) recursively
// deserialized into that class. Primitive fields, nulls, and shape
// mismatches are left untouched, and the original dict is returned
// unchanged when nothing recurses. resolveClass reports whether a name
// is a user class (yielding the class value to deserialize into);
// deserialize performs the nested parseAs.
func HydrateNestedClassFields(
	fields []FieldTypeInfo,
	value runtime.Dict,
	resolveClass func(string) (runtime.Value, bool),
	deserialize func(class runtime.Value, val runtime.Value) (runtime.Value, error),
) (runtime.Dict, error) {
	type plan struct {
		kind  NestedKind
		class runtime.Value
	}
	plans := map[string]plan{}
	for _, f := range fields {
		kind, inner := ParseNestedFieldType(f.Type)
		if kind == NestedNone {
			continue
		}
		class, ok := f.Class, f.Class != nil
		if !ok {
			class, ok = resolveClass(inner)
		}
		if !ok {
			continue
		}
		plans[DictKey(runtime.String{Value: f.Name})] = plan{kind, class}
	}
	if len(plans) == 0 {
		return value, nil
	}
	out := runtime.NewDictHint(value.Len())
	for _, key := range value.OrderedKeys() {
		entry, _ := value.GetEntry(key)
		p, planned := plans[key]
		if !planned {
			out.PutEntry(key, entry)
			continue
		}
		newVal, err := hydrateNestedValue(p.kind, p.class, entry.Value, deserialize)
		if err != nil {
			return runtime.Dict{}, err
		}
		out.PutEntry(key, runtime.DictEntry{Key: entry.Key, Value: newVal})
	}
	return out, nil
}

func hydrateNestedValue(
	kind NestedKind,
	class runtime.Value,
	val runtime.Value,
	deserialize func(runtime.Value, runtime.Value) (runtime.Value, error),
) (runtime.Value, error) {
	switch kind {
	case NestedSingle:
		if d, ok := val.(runtime.Dict); ok {
			return deserialize(class, d)
		}
	case NestedList:
		if lst, ok := val.(*runtime.List); ok {
			elems := make([]runtime.Value, len(lst.Elements))
			for i, el := range lst.Elements {
				if d, ok := el.(runtime.Dict); ok {
					inst, err := deserialize(class, d)
					if err != nil {
						return nil, err
					}
					elems[i] = inst
				} else {
					elems[i] = el
				}
			}
			return &runtime.List{Elements: elems, ElementTypes: lst.ElementTypes}, nil
		}
	case NestedDict:
		if dv, ok := val.(runtime.Dict); ok {
			out := runtime.NewDictHint(dv.Len())
			for _, k := range dv.OrderedKeys() {
				e, _ := dv.GetEntry(k)
				if d, ok := e.Value.(runtime.Dict); ok {
					inst, err := deserialize(class, d)
					if err != nil {
						return nil, err
					}
					out.PutEntry(k, runtime.DictEntry{Key: e.Key, Value: inst})
				} else {
					out.PutEntry(k, e)
				}
			}
			return out, nil
		}
	}
	return val, nil
}
