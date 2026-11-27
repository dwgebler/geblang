package native

import (
	"math/big"
	"strconv"
	"strings"

	argbinding "geblang/internal/binding"
	"geblang/internal/runtime"
)

type DefaultKind int

const (
	DefaultNone DefaultKind = iota
	DefaultInt
	DefaultDecimal
	DefaultFloat
	DefaultString
	DefaultBool
	DefaultNull
	DefaultList
	DefaultDict
	DefaultSet
)

// DefaultLiteral is a catalog parameter default that can be materialized as a value; Text holds the literal source for numbers and the decoded text for strings.
type DefaultLiteral struct {
	Kind DefaultKind
	Text string
}

// ArgSource is one argument to pass a native positionally: a call-site argument index, or a default filling a skipped parameter.
type ArgSource struct {
	Index   int
	Default DefaultLiteral
}

func (s ArgSource) IsDefault() bool {
	return s.Index == argbinding.DefaultSlot
}

func (d DefaultLiteral) Value() runtime.Value {
	switch d.Kind {
	case DefaultInt:
		n, _ := new(big.Int).SetString(d.Text, 10)
		return bigIntValue(n)
	case DefaultDecimal:
		r, _ := new(big.Rat).SetString(d.Text)
		return runtime.Decimal{Value: r}
	case DefaultFloat:
		f, _ := strconv.ParseFloat(d.Text, 64)
		return runtime.Float{Value: f}
	case DefaultString:
		return runtime.String{Value: d.Text}
	case DefaultBool:
		return runtime.Bool{Value: d.Text == "true"}
	case DefaultList:
		return &runtime.List{}
	case DefaultDict:
		return runtime.NewDict()
	case DefaultSet:
		return runtime.NewSet(map[string]runtime.SetEntry{})
	}
	return runtime.Null{}
}

// catalogDefault parses the literal default of a catalog parameter such as "float relTol = 1e-9"; computed defaults like "end = length" are not literals.
func catalogDefault(param string) (DefaultLiteral, bool) {
	decl, text, ok := strings.Cut(param, " = ")
	if !ok {
		return DefaultLiteral{}, false
	}
	text = strings.TrimSpace(text)
	fields := strings.Fields(decl)
	typ := ""
	if len(fields) > 1 {
		typ = strings.Join(fields[:len(fields)-1], " ")
	}
	switch text {
	case "null":
		return DefaultLiteral{Kind: DefaultNull}, true
	case "true", "false":
		return DefaultLiteral{Kind: DefaultBool, Text: text}, true
	case "[]":
		return DefaultLiteral{Kind: DefaultList}, true
	case "{}":
		if typ == "set" || strings.HasPrefix(typ, "set<") {
			return DefaultLiteral{Kind: DefaultSet}, true
		}
		return DefaultLiteral{Kind: DefaultDict}, true
	}
	if strings.HasPrefix(text, "\"") {
		s, err := strconv.Unquote(text)
		if err != nil {
			return DefaultLiteral{}, false
		}
		return DefaultLiteral{Kind: DefaultString, Text: s}, true
	}
	if _, ok := new(big.Rat).SetString(text); !ok || strings.ContainsAny(text, "/xX") {
		return DefaultLiteral{}, false
	}
	integral := !strings.ContainsAny(text, ".eE")
	switch {
	case typ == "float":
		return DefaultLiteral{Kind: DefaultFloat, Text: text}, true
	case typ == "decimal" || !integral:
		return DefaultLiteral{Kind: DefaultDecimal, Text: text}, true
	}
	return DefaultLiteral{Kind: DefaultInt, Text: text}, true
}

// orderNativeArgs maps a binding result onto positional native arguments: a trailing omitted optional is dropped so the native applies its own default, and a skipped one before a supplied argument is filled from its catalog literal.
func orderNativeArgs(sig argbinding.Signature, params []string, result argbinding.Result) ([]ArgSource, error) {
	variadicIndex := -1
	if sig.Variadic && len(sig.ParamNames) > 0 {
		variadicIndex = len(sig.ParamNames) - 1
	}
	out := make([]ArgSource, 0, len(result.Slots)+len(result.TailArgs))
	var pending []int
	flush := func() error {
		for _, i := range pending {
			lit, ok := catalogDefault(params[i])
			if !ok {
				return argbinding.MissingArgumentError(sig, i)
			}
			out = append(out, ArgSource{Index: argbinding.DefaultSlot, Default: lit})
		}
		pending = pending[:0]
		return nil
	}
	for i, slot := range result.Slots {
		if i == variadicIndex {
			continue
		}
		if slot == argbinding.DefaultSlot {
			pending = append(pending, i)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		out = append(out, ArgSource{Index: slot})
	}
	if variadicIndex >= 0 && result.Slots[variadicIndex] != argbinding.DefaultSlot {
		if err := flush(); err != nil {
			return nil, err
		}
		out = append(out, ArgSource{Index: result.Slots[variadicIndex]})
	}
	for _, idx := range result.TailArgs {
		out = append(out, ArgSource{Index: idx})
	}
	return out, nil
}

// bindDoc selects the first of doc's parameter lists that binds args, falling back to the first whose parameter names cover every named argument so errors name the closest overload.
func bindDoc(doc FunctionDoc, owner, name string, args []argbinding.Arg) ([]ArgSource, error) {
	var firstErr error
	fallback := -1
	lists := append([][]string{doc.Params}, doc.Alternates...)
	for li, params := range lists {
		sig := nativeSignature(name, params)
		sig.Owner = owner
		sources, err := bindParams(sig, params, args)
		if err == nil {
			return sources, nil
		}
		if fallback < 0 && namesCovered(sig, args) {
			fallback, firstErr = li, err
		}
		if li == 0 && firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func bindParams(sig argbinding.Signature, params []string, args []argbinding.Arg) ([]ArgSource, error) {
	result, err := argbinding.Order(sig, args)
	if err != nil {
		return nil, err
	}
	return orderNativeArgs(sig, params, result)
}

func namesCovered(sig argbinding.Signature, args []argbinding.Arg) bool {
	for _, arg := range args {
		if arg.Name == "" {
			continue
		}
		found := false
		for _, name := range sig.ParamNames {
			if strings.EqualFold(name, arg.Name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// BindNativeCall binds named and positional arguments to a native module function; ok is false when the function has no catalog signature.
func BindNativeCall(module, function string, args []argbinding.Arg) (sources []ArgSource, ok bool, err error) {
	doc, found := nativeDoc(module, function)
	if !found {
		return nil, false, nil
	}
	sources, err = bindDoc(doc, "", function, args)
	return sources, true, err
}

// BindPrimitiveCall binds named and positional arguments to a primitive method by its catalog signature; ok is false when typeName has no such catalog method.
func BindPrimitiveCall(typeName, method string, args []argbinding.Arg) (sources []ArgSource, ok bool, err error) {
	doc, found := CatalogPrimitiveMethods[typeName][method]
	if !found {
		return nil, false, nil
	}
	sources, err = bindDoc(doc, typeName, method, args)
	return sources, true, err
}

// PrimitiveCatalogType is the catalog key for a primitive receiver value, or "" when the value has no primitive method catalog.
func PrimitiveCatalogType(v runtime.Value) string {
	switch v.(type) {
	case runtime.String:
		return "string"
	case runtime.SmallInt, runtime.Int:
		return "int"
	case runtime.Float:
		return "float"
	case runtime.Decimal:
		return "decimal"
	case runtime.Bool:
		return "bool"
	case runtime.Bytes:
		return "bytes"
	case *runtime.List:
		return "list"
	case runtime.Dict:
		return "dict"
	case runtime.Set:
		return "set"
	case runtime.Range:
		return "range"
	}
	return ""
}

// ArgNames wraps call-site argument names for binding; an empty name is positional.
func ArgNames(names []string, argc int) []argbinding.Arg {
	out := make([]argbinding.Arg, argc)
	for i := 0; i < argc && i < len(names); i++ {
		out[i].Name = names[i]
	}
	return out
}

// HasNames reports whether any call-site argument is named.
func HasNames(names []string) bool {
	for _, n := range names {
		if n != "" {
			return true
		}
	}
	return false
}

// ApplyArgSources builds the positional argument list, materializing a fresh value for each filled default.
func ApplyArgSources(sources []ArgSource, args []runtime.Value) []runtime.Value {
	out := make([]runtime.Value, len(sources))
	for i, s := range sources {
		if s.IsDefault() {
			out[i] = s.Default.Value()
		} else {
			out[i] = args[s.Index]
		}
	}
	return out
}

// WithoutNullComparator treats an explicit null comparator as the documented default, natural order.
func WithoutNullComparator(args []runtime.Value) []runtime.Value {
	if len(args) == 1 {
		if _, isNull := args[0].(runtime.Null); isNull {
			return nil
		}
	}
	return args
}

// nativeMethodOwner returns the Go-backed class that defines method for class, walking parents.
func nativeMethodOwner(class *runtime.Class, method string) *runtime.Class {
	key := strings.ToLower(method)
	for current := class; current != nil; current = current.Parent {
		overloads, ok := current.Methods[key]
		if !ok {
			continue
		}
		if current.Module == "" || len(overloads) == 0 || overloads[0].Native == nil {
			return nil
		}
		return current
	}
	return nil
}

func nativeClassMethodDoc(module, class, method string) (FunctionDoc, bool) {
	methods := StdlibCatalog[module].ClassMethods[class]
	if doc, ok := methods[method]; ok {
		return doc, true
	}
	for name, doc := range methods {
		if strings.EqualFold(name, method) {
			return doc, true
		}
	}
	return FunctionDoc{}, false
}

// BindReceiverCall binds a method call's arguments by catalog signature for a primitive receiver or an instance of a Go-backed class; ok is false when the receiver's method has no catalog signature.
func BindReceiverCall(receiver runtime.Value, method string, args []argbinding.Arg) (sources []ArgSource, ok bool, err error) {
	if typeName := PrimitiveCatalogType(receiver); typeName != "" {
		return BindPrimitiveCall(typeName, method, args)
	}
	instance, isInstance := receiver.(*runtime.Instance)
	if !isInstance || instance.Class == nil {
		return nil, false, nil
	}
	owner := nativeMethodOwner(instance.Class, method)
	if owner == nil {
		return nil, false, nil
	}
	doc, found := nativeClassMethodDoc(owner.Module, owner.Name, method)
	if !found {
		return nil, false, nil
	}
	sources, err = bindDoc(doc, owner.Name, method, args)
	return sources, true, err
}
