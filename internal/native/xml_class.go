package native

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"geblang/internal/runtime"
)

const maxXMLClassDepth = 64

func xmlElementParts(element runtime.Dict) (attrs runtime.Dict, children []runtime.Dict, text string) {
	if entry, ok := element.GetEntry(dictKey("attributes")); ok {
		attrs, _ = entry.Value.(runtime.Dict)
	}
	if entry, ok := element.GetEntry(dictKey("children")); ok {
		if list, isList := entry.Value.(*runtime.List); isList {
			for _, child := range list.Elements {
				if dict, isDict := child.(runtime.Dict); isDict {
					children = append(children, dict)
				}
			}
		}
	}
	if entry, ok := element.GetEntry(dictKey("text")); ok {
		if s, isString := entry.Value.(runtime.String); isString {
			text = s.Value
		}
	}
	return attrs, children, text
}

func xmlElementName(element runtime.Dict) string {
	if entry, ok := element.GetEntry(dictKey("name")); ok {
		if s, isString := entry.Value.(runtime.String); isString {
			return s.Value
		}
	}
	return ""
}

// xmlElementFields maps an element onto class's fields: same-named child elements win over attributes, and text converts to each field's declared type.
func xmlElementFields(ctx ConversionContext, class runtime.Value, element runtime.Dict, depth int) (runtime.Dict, error) {
	if depth > maxXMLClassDepth {
		return runtime.Dict{}, fmt.Errorf("xml.parseAs: nesting exceeds %d levels", maxXMLClassDepth)
	}
	fields, err := ctx.ClassFields(class)
	if err != nil {
		return runtime.Dict{}, err
	}
	attrs, children, _ := xmlElementParts(element)
	byName := map[string][]runtime.Dict{}
	for _, child := range children {
		name := xmlElementName(child)
		byName[name] = append(byName[name], child)
	}
	out := runtime.NewDictHint(len(fields))
	for _, field := range fields {
		matches := byName[field.Name]
		var value runtime.Value
		var err error
		switch {
		case len(matches) > 0:
			value, err = xmlFieldFromElements(ctx, field, matches, depth)
		case attrs.Len() > 0:
			entry, ok := attrs.GetEntry(dictKey(field.Name))
			if !ok {
				continue
			}
			text, _ := entry.Value.(runtime.String)
			value, err = xmlScalar(text.Value, field.Type, field.Name)
		default:
			continue
		}
		if err != nil {
			return runtime.Dict{}, err
		}
		key := runtime.String{Value: field.Name}
		out.PutEntry(DictKey(key), runtime.DictEntry{Key: key, Value: value})
	}
	return out, nil
}

func xmlFieldFromElements(ctx ConversionContext, field ClassFieldInfo, elements []runtime.Dict, depth int) (runtime.Value, error) {
	declared := strings.TrimPrefix(strings.TrimSpace(field.Type), "?")
	if element, ok := xmlListElementType(declared); ok {
		items := make([]runtime.Value, 0, len(elements))
		for _, e := range elements {
			item, err := xmlSingleValue(ctx, element, field.ElementClass, e, field.Name, depth)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return &runtime.List{Elements: items}, nil
	}
	return xmlSingleValue(ctx, field.Type, field.Class, elements[len(elements)-1], field.Name, depth)
}

func xmlListElementType(declared string) (string, bool) {
	if inner, ok := strings.CutSuffix(declared, "[]"); ok {
		return inner, true
	}
	for _, prefix := range []string{"list<", "set<"} {
		if strings.HasPrefix(declared, prefix) && strings.HasSuffix(declared, ">") {
			return declared[len(prefix) : len(declared)-1], true
		}
	}
	return "", false
}

func xmlSingleValue(ctx ConversionContext, declared string, class runtime.Value, element runtime.Dict, field string, depth int) (runtime.Value, error) {
	_, children, text := xmlElementParts(element)
	nullable := strings.HasPrefix(strings.TrimSpace(declared), "?")
	if class != nil {
		if nullable && len(children) == 0 && text == "" {
			return runtime.Null{}, nil
		}
		return xmlElementFields(ctx, class, element, depth+1)
	}
	return xmlScalar(text, declared, field)
}

func xmlScalar(text string, declared string, field string) (runtime.Value, error) {
	t := strings.TrimSpace(declared)
	nullable := strings.HasPrefix(t, "?")
	base := strings.TrimSpace(strings.TrimPrefix(t, "?"))
	if nullable && text == "" && base != "string" {
		return runtime.Null{}, nil
	}
	invalid := func() error {
		return runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("xml.parseAs: field %s expects %s, got %q", field, base, text)}
	}
	switch base {
	case "int":
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			return runtime.SmallInt{Value: v}, nil
		}
		v, err := runtime.NewIntLiteral(text)
		if err != nil {
			return nil, invalid()
		}
		return v, nil
	case "decimal":
		v, err := runtime.NewDecimalLiteral(text)
		if err != nil {
			return nil, invalid()
		}
		return v, nil
	case "float":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, invalid()
		}
		return runtime.Float{Value: v}, nil
	case "bool":
		switch strings.ToLower(text) {
		case "true", "1":
			return runtime.Bool{Value: true}, nil
		case "false", "0":
			return runtime.Bool{Value: false}, nil
		}
		return nil, invalid()
	}
	return runtime.String{Value: text}, nil
}

func xmlElementDict(name string, text string, children []runtime.Value) runtime.Dict {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey("name"):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: name}},
		dictKey("attributes"): {Key: runtime.String{Value: "attributes"}, Value: runtime.DictFromEntries(map[string]runtime.DictEntry{}, nil)},
		dictKey("children"):   {Key: runtime.String{Value: "children"}, Value: &runtime.List{Elements: children}},
		dictKey("text"):       {Key: runtime.String{Value: "text"}, Value: runtime.String{Value: text}},
	}, nil)
}

func xmlInstanceElement(ctx ConversionContext, instance *runtime.Instance, name string, depth int) (runtime.Dict, error) {
	if depth > maxXMLClassDepth {
		return runtime.Dict{}, fmt.Errorf("xml.stringify: nesting exceeds %d levels", maxXMLClassDepth)
	}
	if value, called, err := invokeInstanceSerialize(ctx, instance); err != nil {
		return runtime.Dict{}, err
	} else if called {
		return xmlValueElement(ctx, name, value, depth)
	}
	fields := instance.SnapshotFields()
	names := make([]string, 0, len(fields))
	for fieldName := range fields {
		if !strings.HasPrefix(fieldName, "_") {
			names = append(names, fieldName)
		}
	}
	sort.Strings(names)
	var children []runtime.Value
	for _, fieldName := range names {
		elements, err := xmlFieldElements(ctx, fieldName, fields[fieldName], depth)
		if err != nil {
			return runtime.Dict{}, err
		}
		children = append(children, elements...)
	}
	return xmlElementDict(name, "", children), nil
}

func xmlFieldElements(ctx ConversionContext, name string, value runtime.Value, depth int) ([]runtime.Value, error) {
	var items []runtime.Value
	switch v := value.(type) {
	case runtime.Null:
		return nil, nil
	case *runtime.List:
		items = v.Elements
	case runtime.Set:
		keys := make([]string, 0, len(v.Elements))
		for k := range v.Elements {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			items = append(items, v.Elements[k].Value)
		}
	default:
		element, err := xmlValueElement(ctx, name, value, depth)
		if err != nil {
			return nil, err
		}
		return []runtime.Value{element}, nil
	}
	out := make([]runtime.Value, 0, len(items))
	for _, item := range items {
		element, err := xmlValueElement(ctx, name, item, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, element)
	}
	return out, nil
}

func xmlValueElement(ctx ConversionContext, name string, value runtime.Value, depth int) (runtime.Dict, error) {
	switch v := value.(type) {
	case *runtime.Instance:
		return xmlInstanceElement(ctx, v, name, depth+1)
	case runtime.Dict:
		var children []runtime.Value
		for _, key := range v.OrderedKeys() {
			entry, _ := v.GetEntry(key)
			keyName, ok := entry.Key.(runtime.String)
			if !ok {
				return runtime.Dict{}, fmt.Errorf("xml.stringify: dict keys must be strings")
			}
			elements, err := xmlFieldElements(ctx, keyName.Value, entry.Value, depth+1)
			if err != nil {
				return runtime.Dict{}, err
			}
			children = append(children, elements...)
		}
		return xmlElementDict(name, "", children), nil
	case runtime.String:
		return xmlElementDict(name, v.Value, nil), nil
	case runtime.Bool, runtime.SmallInt, runtime.Int, runtime.Decimal, runtime.Float:
		return xmlElementDict(name, value.Inspect(), nil), nil
	case runtime.Null:
		return xmlElementDict(name, "", nil), nil
	}
	return runtime.Dict{}, fmt.Errorf("xml.stringify does not support %s", value.TypeName())
}
