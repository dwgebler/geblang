package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// valueMatchesType (method form) is the evaluator-aware variant of the
// free function. It walks `errorClassParents` so cross-module
// user-defined error class hierarchies (BadRequestError -> HttpException
// -> RuntimeError, etc.) resolve correctly under `instanceof`. The free
// function is preserved for call sites that don't have an *Evaluator
// in hand and only care about plain class / interface matching.
func (e *Evaluator) valueMatchesType(value runtime.Value, typeName string) bool {
	if dotIdx := strings.Index(typeName, "."); dotIdx >= 0 {
		if ev, ok := value.(runtime.EnumVariant); ok {
			enumTypeName := typeName[:dotIdx]
			variantName := typeName[dotIdx+1:]
			if strings.EqualFold(ev.Enum.Name, enumTypeName) {
				return strings.EqualFold(ev.Variant, variantName)
			}
		}
	}
	stripped := simpleTypeName(typeName)
	if errValue, ok := value.(runtime.Error); ok {
		return e.errorTypeMatches(errValue.Class, stripped)
	}
	return valueMatchesType(value, typeName)
}

// instanceofExact does module-exact matching for the instanceof operator when the RHS resolves to a user class or interface; handled=false falls back to name-based matching.
func (e *Evaluator) instanceofExact(value runtime.Value, typeName string, env *runtime.Environment) (bool, bool) {
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return false, false
	}
	if runtime.IsBuiltinTypeName(typeName) || strings.ContainsAny(typeName, "<|&") {
		return false, false
	}
	resolved, ok, err := e.reflectLookupValue(typeName, env)
	if err != nil || !ok {
		return false, false
	}
	var module, name string
	switch decl := resolved.(type) {
	case *runtime.Class:
		module, name = decl.DeclaringModule(), decl.Name
	case *runtime.Interface:
		module, name = decl.Module, decl.Name
	default:
		return false, false
	}
	matched := runtime.InstanceClassMatchesExact(instance.Class, module, name)
	for _, extra := range instance.ExtraTypeNames {
		if matched {
			break
		}
		matched = strings.EqualFold(simpleTypeName(extra), name)
	}
	return matched, true
}

func valueMatchesType(value runtime.Value, typeName string) bool {
	if typeName == "any" || typeName == "?any" {
		return true
	}
	if arms, ok := splitTopLevelUnion(typeName); ok {
		for _, arm := range arms {
			if valueMatchesType(value, arm) {
				return true
			}
		}
		return false
	}
	if dotIdx := strings.Index(typeName, "."); dotIdx >= 0 {
		if ev, ok := value.(runtime.EnumVariant); ok {
			enumTypeName := typeName[:dotIdx]
			variantName := typeName[dotIdx+1:]
			// `Enum.Variant` is a variant test; a `module.Interface`
			// qualified name (prefix is not this enum) falls through to
			// the interface-conformance check below.
			if strings.EqualFold(ev.Enum.Name, enumTypeName) {
				return strings.EqualFold(ev.Variant, variantName)
			}
		}
	}
	if baseName, args, ok := splitGenericTypeName(typeName); ok {
		if instance, isInstance := value.(*runtime.Instance); isInstance {
			return instanceMatchesParameterizedClass(instance, baseName, args)
		}
		return collectionMatchesGenericType(value, baseName, args)
	}
	typeName = simpleTypeName(typeName)
	if ev, ok := value.(runtime.EnumVariant); ok {
		if strings.EqualFold(ev.Enum.Name, typeName) {
			return true
		}
		for _, iface := range ev.Enum.Implements {
			if strings.EqualFold(iface, typeName) {
				return true
			}
		}
		return false
	}
	if errValue, ok := value.(runtime.Error); ok {
		return errorTypeMatches(errValue.Class, typeName)
	}
	if instance, ok := value.(*runtime.Instance); ok {
		for class := instance.Class; class != nil; class = class.Parent {
			if typeNamesEqual(class.Name, typeName) {
				return true
			}
			if classImplementsInterface(class, typeName) {
				return true
			}
		}
		for _, extra := range instance.ExtraTypeNames {
			if typeNamesEqual(simpleTypeName(extra), typeName) {
				return true
			}
		}
		// Fall through: an instance with an `__invoke` method matches
		// the `callable` family even when its class isn't named callable.
		if isCallableTypeName(typeName) && runtime.IsCallableValue(value) {
			return true
		}
		return false
	}
	// `func` / `callable` / `function` all match any callable runtime value
	// (Function, OverloadedFunction, BytecodeFunction, decorated targets).
	// This keeps `as callable` symmetrical with parameter-type matching
	// and with the VM's cast path, both of which already accept funcs.
	if isCallableTypeName(typeName) && runtime.IsCallableValue(value) {
		return true
	}
	return typeNamesEqual(value.TypeName(), typeName)
}

// instanceMatchesParameterizedClass implements `x instanceof Box<string>`
// for user generic classes: the class chain must contain the base, and
// the instance's reified bindings must match the type arguments
// invariantly (positional against the base class's declared params).
// Unbound params and non-generic bases never match.
func instanceMatchesParameterizedClass(instance *runtime.Instance, baseName string, args []string) bool {
	base := simpleTypeName(baseName)
	var declared []string
	found := false
	for class := instance.Class; class != nil; class = class.Parent {
		if typeNamesEqual(class.Name, base) {
			declared = class.TypeParameters
			found = true
			break
		}
	}
	if !found || len(declared) == 0 || len(args) > len(declared) {
		return false
	}
	for i, arg := range args {
		bound, ok := instance.TypeBindings[declared[i]]
		if !ok || !typeNamesEqual(bound, simpleTypeName(strings.TrimSpace(arg))) {
			return false
		}
	}
	return true
}

// splitGenericTypeName splits "list<int>" / "dict<string,int>" / "?list<int>"
// into ("list", ["int"], true) / ("dict", ["string","int"], true) /
// ("list", ["int"], true). Returns (_, _, false) when the input has no
// generic-arg clause.
func splitGenericTypeName(typeName string) (string, []string, bool) {
	if strings.HasPrefix(typeName, "?") {
		typeName = typeName[1:]
	}
	lt := strings.IndexByte(typeName, '<')
	if lt < 0 || !strings.HasSuffix(typeName, ">") {
		return "", nil, false
	}
	base := typeName[:lt]
	inner := typeName[lt+1 : len(typeName)-1]
	// Split top-level commas, ignoring those inside nested generics.
	var args []string
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	if start <= len(inner) {
		args = append(args, strings.TrimSpace(inner[start:]))
	}
	return base, args, true
}

func collectionMatchesGenericType(value runtime.Value, base string, args []string) bool {
	switch v := value.(type) {
	case *runtime.List:
		if base != "list" || len(args) != 1 {
			return false
		}
		if len(v.ElementTypes) >= 1 {
			return typeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0])
		}
		for _, el := range v.Elements {
			if !valueMatchesType(el, args[0]) {
				return false
			}
		}
		return true
	case runtime.Set:
		if base != "set" || len(args) != 1 {
			return false
		}
		if len(v.ElementTypes) >= 1 {
			return typeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0])
		}
		for _, e := range v.Elements {
			if !valueMatchesType(e.Value, args[0]) {
				return false
			}
		}
		return true
	case runtime.Dict:
		if base != "dict" || len(args) != 2 {
			return false
		}
		if len(v.ElementTypes) >= 2 {
			return typeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0]) && typeNameSatisfies(elementTagBase(v.ElementTypes[1]), args[1])
		}
		matches := true
		v.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
			if !valueMatchesType(e.Key, args[0]) || !valueMatchesType(e.Value, args[1]) {
				matches = false
				return false
			}
			return true
		})
		return matches
	}
	return false
}

// splitTopLevelUnion splits a union on depth-0 `|`, preserving `|`
// inside nested generic angle brackets.
func splitTopLevelUnion(typeName string) ([]string, bool) {
	depth := 0
	hasTopLevel := false
	for i := 0; i < len(typeName); i++ {
		switch typeName[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				hasTopLevel = true
			}
		}
	}
	if !hasTopLevel {
		return nil, false
	}
	var arms []string
	depth = 0
	start := 0
	for i := 0; i < len(typeName); i++ {
		switch typeName[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				arms = append(arms, strings.TrimSpace(typeName[start:i]))
				start = i + 1
			}
		}
	}
	arms = append(arms, strings.TrimSpace(typeName[start:]))
	return arms, true
}

// typeNameSatisfies handles `any` and union arms over the existing
// invariance-by-name rule for tagged collections.
// checkDictWriteTags enforces a tagged dict's key and value types on
// any write path.
func checkDictWriteTags(dict runtime.Dict, key, value runtime.Value) error {
	if len(dict.ElementTypes) < 2 {
		return nil
	}
	if !valueSatisfiesElementTag(key, dict.ElementTypes[0]) {
		return thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot use %s key in dict<%s, %s>", key.TypeName(), dict.ElementTypes[0], dict.ElementTypes[1])}}
	}
	if !valueSatisfiesElementTag(value, dict.ElementTypes[1]) {
		return thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot assign %s to dict<%s, %s>", value.TypeName(), dict.ElementTypes[0], dict.ElementTypes[1])}}
	}
	return nil
}

// valueSatisfiesElementTag is the element-tag write barrier: name-level
// match first, then the value's class hierarchy (subclass and
// implementer writes into honestly-tagged collections are legal).
// elementTagBase drops a stored tag's nullable marker; instanceof and
// reflect read the base type, leaving nullability to the write barrier.
func elementTagBase(s string) string {
	return strings.TrimPrefix(s, "?")
}

// elementTagName renders a generic argument as a stored element tag,
// preserving a leading ? so the write barrier can accept null.
func elementTagName(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	if ref.Nullable {
		return "?" + ref.Name
	}
	return ref.Name
}

func valueSatisfiesElementTag(value runtime.Value, tag string) bool {
	// A nullable element tag (?T) accepts null; otherwise match the base.
	if strings.HasPrefix(tag, "?") {
		if _, ok := value.(runtime.Null); ok {
			return true
		}
		tag = tag[1:]
	}
	if typeNameSatisfies(value.TypeName(), tag) {
		return true
	}
	if arms, ok := splitTopLevelUnion(tag); ok {
		for _, arm := range arms {
			if valueSatisfiesElementTag(value, arm) {
				return true
			}
		}
		return false
	}
	return runtime.ValueSatisfiesHierarchyLeaf(value, simpleTypeName(strings.TrimSpace(tag)))
}

func typeNameSatisfies(have, want string) bool {
	if want == "any" || want == "?any" {
		return true
	}
	if arms, ok := splitTopLevelUnion(want); ok {
		for _, arm := range arms {
			if typeNameSatisfies(have, arm) {
				return true
			}
		}
		return false
	}
	if isCallableTypeName(want) && isCallableTypeName(have) {
		return true
	}
	return typeNamesEqual(have, want)
}

func (e *Evaluator) checkTypeParamConstraints(fn runtime.Function, callEnv *runtime.Environment) error {
	if len(fn.TypeParamConstraints) == 0 {
		return nil
	}
	for name, constraint := range fn.TypeParamConstraints {
		boundName, ok := callEnv.GetTypeBinding(name)
		if !ok {
			continue
		}
		if err := e.checkConstraintSatisfied(boundName, name, constraint, callEnv); err != nil {
			return err
		}
	}
	return nil
}

func (e *Evaluator) checkClassTypeParamConstraints(class *runtime.Class, bindings map[string]string) error {
	if class == nil || len(class.TypeParamConstraints) == 0 {
		return nil
	}
	env := class.Env
	for name, constraint := range class.TypeParamConstraints {
		boundName, ok := bindings[name]
		if !ok {
			continue
		}
		if err := e.checkConstraintSatisfied(boundName, name, constraint, env); err != nil {
			return err
		}
	}
	return nil
}

func (e *Evaluator) checkConstraintSatisfied(typeName, paramName string, constraint *ast.TypeRef, env *runtime.Environment) error {
	if constraint == nil || e.constraintSatisfied(typeName, constraint, env) {
		return nil
	}
	return fmt.Errorf("type %s does not satisfy constraint %s for type parameter %s", typeName, constraintDisplayString(constraint), paramName)
}

func (e *Evaluator) constraintSatisfied(typeName string, constraint *ast.TypeRef, env *runtime.Environment) bool {
	if constraint == nil {
		return true
	}
	switch constraint.Operator {
	case "|":
		return e.constraintSatisfied(typeName, constraint.Left, env) || e.constraintSatisfied(typeName, constraint.Right, env)
	case "&":
		return e.constraintSatisfied(typeName, constraint.Left, env) && e.constraintSatisfied(typeName, constraint.Right, env)
	}
	leaf := constraint.Name
	// Identity covers primitives and exact class names.
	if typeNamesEqual(typeName, leaf) {
		return true
	}
	boundVal, ok := env.Get(typeName)
	if !ok {
		return false
	}
	boundClass, ok := boundVal.(*runtime.Class)
	if !ok {
		return false
	}
	leafVal, ok := env.Get(leaf)
	if !ok {
		return false
	}
	if _, isIface := leafVal.(*runtime.Interface); isIface {
		return classImplementsInterface(boundClass, leaf)
	}
	if leafClass, isClass := leafVal.(*runtime.Class); isClass {
		for c := boundClass; c != nil; c = c.Parent {
			if typeNamesEqual(c.Name, leafClass.Name) {
				return true
			}
		}
	}
	return false
}

// constraintDisplayString matches the VM's stored constraint expr with
// the outermost parens stripped, keeping messages byte-identical.
func constraintDisplayString(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	if ref.Operator != "" {
		return constraintRefString(ref.Left) + ref.Operator + constraintRefString(ref.Right)
	}
	return ref.Name
}

func constraintRefString(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	if ref.Operator != "" {
		return "(" + constraintRefString(ref.Left) + ref.Operator + constraintRefString(ref.Right) + ")"
	}
	return ref.Name
}

func typeNameFromExpression(expr ast.Expression) (string, error) {
	switch expr := expr.(type) {
	case *ast.Identifier:
		return expr.Value, nil
	case *ast.SelectorExpression:
		if expr.Name.Value == "type" {
			return expr.Object.String(), nil
		}
		return expr.String(), nil
	}
	return "", fmt.Errorf("expected type name, got %s", expr.String())
}

func castDunderName(target string) string {
	switch strings.ToLower(strings.TrimPrefix(target, "?")) {
	case "string":
		return "__string"
	case "int":
		return "__int"
	case "float":
		return "__float"
	case "bool":
		return "__bool"
	case "decimal":
		return "__decimal"
	case "bytes":
		return "__bytes"
	}
	return ""
}

func checkCastDunderReturn(target string, value runtime.Value) error {
	want := strings.ToLower(strings.TrimPrefix(target, "?"))
	switch want {
	case "string":
		if _, ok := value.(runtime.String); !ok {
			return fmt.Errorf("__string must return string, got %s", value.TypeName())
		}
	case "int":
		switch value.(type) {
		case runtime.SmallInt, runtime.Int:
		default:
			return fmt.Errorf("__int must return int, got %s", value.TypeName())
		}
	case "float":
		if _, ok := value.(runtime.Float); !ok {
			return fmt.Errorf("__float must return float, got %s", value.TypeName())
		}
	case "bool":
		if _, ok := value.(runtime.Bool); !ok {
			return fmt.Errorf("__bool must return bool, got %s", value.TypeName())
		}
	case "decimal":
		if _, ok := value.(runtime.Decimal); !ok {
			return fmt.Errorf("__decimal must return decimal, got %s", value.TypeName())
		}
	case "bytes":
		if _, ok := value.(runtime.Bytes); !ok {
			return fmt.Errorf("__bytes must return bytes, got %s", value.TypeName())
		}
	}
	return nil
}

func castValue(value runtime.Value, target string) (runtime.Value, error) {
	if valueMatchesType(value, target) {
		return value, nil
	}
	/* Nullable targets accept null directly; non-null values fall
	 * through to the underlying type's cast logic. */
	if strings.HasPrefix(target, "?") {
		if _, isNull := value.(runtime.Null); isNull {
			return runtime.Null{}, nil
		}
		return castValue(value, target[1:])
	}
	switch target {
	case "string":
		/* `bytes as string` decodes UTF-8 (errors on invalid bytes)
		 * rather than producing the hex form `value.Inspect()` returns
		 * for bytes. Other types still use `Inspect()` as the canonical
		 * string representation. */
		if v, ok := value.(runtime.Bytes); ok {
			if !utf8.Valid(v.Value) {
				return nil, fmt.Errorf("bytes value is not valid UTF-8")
			}
			return runtime.String{Value: string(v.Value)}, nil
		}
		return runtime.String{Value: value.Inspect()}, nil
	case "int":
		switch v := value.(type) {
		case runtime.String:
			return runtime.NewIntLiteral(v.Value)
		case runtime.Decimal:
			/* Truncate toward zero: matches the C/Java/Go integer-
			 * cast convention. Use big.Int division of num/den so
			 * arbitrary-precision decimals round correctly. */
			num := new(big.Int).Set(v.Value.Num())
			den := v.Value.Denom()
			q := new(big.Int).Quo(num, den)
			return runtime.Int{Value: q}, nil
		case runtime.Float:
			return runtime.NewInt64(int64(math.Trunc(v.Value))), nil
		case runtime.Bool:
			if v.Value {
				return runtime.SmallInt{Value: 1}, nil
			}
			return runtime.SmallInt{Value: 0}, nil
		}
	case "decimal":
		switch v := value.(type) {
		case runtime.SmallInt:
			return native.SmallIntToDecimal(v), nil
		case runtime.Int:
			return intToDecimal(v), nil
		case runtime.Float:
			return runtime.NewDecimalLiteral(strconv.FormatFloat(v.Value, 'g', -1, 64))
		case runtime.String:
			return runtime.NewDecimalLiteral(v.Value)
		}
	case "float":
		switch v := value.(type) {
		case runtime.SmallInt:
			return runtime.Float{Value: float64(v.Value)}, nil
		case runtime.Int:
			f, _ := new(big.Rat).SetInt(v.Value).Float64()
			return runtime.Float{Value: f}, nil
		case runtime.Decimal:
			f, _ := v.Value.Float64()
			return runtime.Float{Value: f}, nil
		case runtime.String:
			f, err := strconv.ParseFloat(v.Value, 64)
			if err != nil {
				return nil, err
			}
			return runtime.Float{Value: f}, nil
		}
	case "bool":
		switch v := value.(type) {
		case runtime.Bool:
			return v, nil
		case runtime.SmallInt:
			return runtime.Bool{Value: v.Value != 0}, nil
		case runtime.Int:
			return runtime.Bool{Value: v.Value.Sign() != 0}, nil
		case runtime.Float:
			return runtime.Bool{Value: v.Value != 0}, nil
		case runtime.Decimal:
			return runtime.Bool{Value: v.Value.Sign() != 0}, nil
		case runtime.String:
			switch v.Value {
			case "true":
				return runtime.Bool{Value: true}, nil
			case "false":
				return runtime.Bool{Value: false}, nil
			}
		case runtime.Null:
			return runtime.Bool{Value: false}, nil
		}
	case "bytes":
		/* `string as bytes` encodes UTF-8. Go strings are already UTF-8,
		 * so we copy out the underlying byte sequence. The inverse
		 * (`bytes as string`) is handled below. */
		if v, ok := value.(runtime.String); ok {
			b := make([]byte, len(v.Value))
			copy(b, v.Value)
			return runtime.Bytes{Value: b}, nil
		}
	case "list":
		/* `set as list` materializes in iteration order (the underlying
		 * map's range order; not insertion order - sets are unordered
		 * by design). To get a deterministic order, sort the result. */
		if v, ok := value.(runtime.Set); ok {
			out := make([]runtime.Value, 0, len(v.Elements))
			for _, entry := range v.Elements {
				out = append(out, entry.Value)
			}
			return &runtime.List{Elements: out}, nil
		}
	case "set":
		/* `list as set` de-duplicates. First occurrence wins; later
		 * duplicates are dropped. */
		if v, ok := value.(*runtime.List); ok {
			elements := make(map[string]runtime.SetEntry, len(v.Elements))
			for _, elem := range v.Elements {
				k := dictKey(elem)
				if _, seen := elements[k]; seen {
					continue
				}
				elements[k] = runtime.SetEntry{Value: elem}
			}
			return runtime.Set{Elements: elements}, nil
		}
	}
	return nil, fmt.Errorf("cannot cast %s to %s", value.TypeName(), target)
}

func primitiveConversionTarget(name string) (string, bool) {
	switch name {
	case "toInt":
		return "int", true
	case "toDecimal":
		return "decimal", true
	case "toFloat":
		return "float", true
	case "toBool":
		return "bool", true
	}
	return "", false
}

// snapshotEnvTypeBindings walks the environment chain and copies the
// accumulated type-parameter bindings into a fresh map. Used when a
// FunctionLiteral or identifier-as-callable creates a function value
// whose enclosing scope has active generic bindings.
func snapshotEnvTypeBindings(env *runtime.Environment) map[string]string {
	if env == nil {
		return nil
	}
	out := map[string]string{}
	for _, name := range env.TypeBindingNames() {
		if v, ok := env.GetTypeBinding(name); ok {
			out[name] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// matchValueToTypeRefWith is matchValueToTypeRef extended with an
// inheritedBindings map. When a type-parameter name in the spec is not
// declared by the function being entered but IS bound by the caller's
// outer generic frame, the binding's concrete type is substituted and
// re-checked.
func matchValueToTypeRefWith(typeParams map[string]bool, inherited map[string]string, value runtime.Value, typ *ast.TypeRef) bool {
	if typ == nil {
		return true
	}
	if len(inherited) > 0 && typ.Operator == "" && len(typ.Arguments) == 0 {
		if bound, ok := inherited[typ.Name]; ok && bound != "" {
			// Substitute the concrete type and re-check via the
			// existing path.
			substituted := &ast.TypeRef{Token: typ.Token, Name: bound, Nullable: typ.Nullable}
			return matchValueToTypeRef(typeParams, value, substituted)
		}
	}
	return matchValueToTypeRef(typeParams, value, typ)
}

func functionReturnMatchesExpected(fn runtime.Function, expected *ast.TypeRef) bool {
	if expected == nil || expected.Operator != "" || expected.Name == "any" {
		return true
	}
	if fn.ReturnType == nil {
		return expected.Name == "void" || expected.Nullable
	}
	if typeRefUsesFunctionTypeParameter(fn, fn.ReturnType) {
		return true
	}
	return typeRefAssignable(expected, fn.ReturnType)
}

func typeRefAssignable(target, actual *ast.TypeRef) bool {
	if target == nil || target.Operator != "" || target.Name == "any" {
		return true
	}
	if actual == nil || actual.Operator != "" {
		return false
	}
	if !target.Nullable && actual.Nullable {
		return false
	}
	targetName := target.Name
	actualName := actual.Name
	if target.ListAlias {
		targetName = "list"
	}
	if actual.ListAlias {
		actualName = "list"
	}
	if isCallableTypeName(targetName) && isCallableTypeName(actualName) {
		return true
	}
	if !typeNamesEqual(targetName, actualName) {
		return false
	}
	if len(target.Arguments) > 0 && len(target.Arguments) == len(actual.Arguments) {
		for i, tArg := range target.Arguments {
			if !typeRefAssignable(tArg, actual.Arguments[i]) {
				return false
			}
		}
	}
	return true
}

func valueMatchesTypeRef(value runtime.Value, typ *ast.TypeRef) bool {
	if typ == nil || typ.Operator != "" || typ.Name == "any" {
		return true
	}
	if _, ok := value.(runtime.Null); ok {
		return typ.Nullable
	}
	typeName := simpleTypeName(typ.Name)
	if typ.ListAlias || typeName == "list" {
		return value.TypeName() == "list"
	}
	if typeName == "set" {
		return value.TypeName() == "set"
	}
	if typeName == "dict" {
		return value.TypeName() == "dict"
	}
	if typeName == "Task" {
		if _, ok := value.(*runtime.Task); ok {
			return true
		}
		/* Not the runtime async Task. Fall through so a user-defined
		 * class named `Task` still matches its own instance. The
		 * built-in runtime Task is reachable only through `async`-
		 * returning callsites, so masking a user class with this
		 * name would otherwise reject every dispatch. */
	}
	if isCallableTypeName(typeName) {
		return runtime.IsCallableValue(value)
	}
	if isGeneratorTypeName(typeName) {
		_, ok := value.(*runtime.Generator)
		return ok
	}
	if typeNamesEqual(value.TypeName(), typ.Name) {
		if instance, ok := value.(*runtime.Instance); ok && !instanceMatchesTypeArgs(instance, typ) {
			return false
		}
		return true
	}
	// Error-derived classes are wrapped as runtime.Error rather than
	// *runtime.Instance. Walk the captured parent chain so a parameter
	// typed `HttpException` accepts a `BadRequestError` value.
	if errValue, ok := value.(runtime.Error); ok {
		target := simpleTypeName(typ.Name)
		for _, ancestor := range errValue.Parents {
			if typeNamesEqual(ancestor, target) {
				return true
			}
		}
		return false
	}
	if ev, ok := value.(runtime.EnumVariant); ok {
		target := simpleTypeName(typ.Name)
		for _, iface := range ev.Enum.Implements {
			if typeNamesEqual(iface, target) {
				return true
			}
		}
		return false
	}
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return false
	}
	for class := instance.Class.Parent; class != nil; class = class.Parent {
		if typeNamesEqual(class.Name, typ.Name) {
			if !instanceMatchesTypeArgs(instance, typ) {
				return false
			}
			return true
		}
	}
	return classImplementsInterface(instance.Class, simpleTypeName(typ.Name))
}

// instanceMatchesTypeArgs enforces invariance on the type arguments of a
// reified generic class instance. When the parameter type carries explicit
// arguments (e.g. `Box<Base>`) the instance's `TypeBindings` must match
// each argument exactly - a `Box<Sub>` is NOT a `Box<Base>` even though
// `Sub extends Base`, because mutating methods on the parameter could
// otherwise insert a sibling `Base` subtype that violates the original
// container's declared element type.
//
// When the parameter type carries no arguments, or the instance has no
// recorded bindings (raw polymorphic construction), the check passes -
// invariance only fires when both sides explicitly carry type arguments.
func instanceMatchesTypeArgs(instance *runtime.Instance, typ *ast.TypeRef) bool {
	if instance == nil || typ == nil || len(typ.Arguments) == 0 {
		return true
	}
	if instance.Class == nil || len(instance.Class.TypeParameters) == 0 {
		return true
	}
	if len(instance.TypeBindings) == 0 {
		return true
	}
	for i, arg := range typ.Arguments {
		if i >= len(instance.Class.TypeParameters) {
			break
		}
		if arg == nil || arg.Operator != "" || arg.Name == "" {
			continue
		}
		paramName := instance.Class.TypeParameters[i]
		bound, ok := instance.TypeBindings[paramName]
		if !ok || bound == "" {
			continue
		}
		if !typeNamesEqual(bound, arg.Name) {
			return false
		}
	}
	return true
}

func simpleTypeName(name string) string {
	if _, suffix, ok := strings.Cut(name, "."); ok {
		return suffix
	}
	return name
}

func typeNamesEqual(left, right string) bool {
	return strings.EqualFold(simpleTypeName(left), simpleTypeName(right))
}

func isCallableTypeName(name string) bool {
	return strings.EqualFold(name, "func") || strings.EqualFold(name, "callable") || strings.EqualFold(name, "function")
}

func isGeneratorTypeName(name string) bool {
	return strings.EqualFold(name, "generator") || strings.EqualFold(name, "iterable")
}

// descriptiveTypeName returns a type name including element types where detectable,
// e.g. "list<string>" instead of "list", "dict<string,int>" instead of "dict".
// For reified user-defined generic class instances it also unspools the
// recorded TypeBindings - "Container<Sub>" rather than the bare "Container" -
// so error messages about invariant-parameter mismatches surface the
// caller's actual binding rather than just the class name.
func descriptiveTypeName(value runtime.Value) string {
	switch v := value.(type) {
	case *runtime.List:
		if len(v.Elements) > 0 {
			return "list<" + v.Elements[0].TypeName() + ">"
		}
		return "list"
	case runtime.Set:
		for _, entry := range v.Elements {
			return "set<" + entry.Value.TypeName() + ">"
		}
		return "set"
	case runtime.Dict:
		result := "dict"
		v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			result = "dict<" + entry.Key.TypeName() + "," + entry.Value.TypeName() + ">"
			return false
		})
		return result
	case *runtime.Instance:
		if v == nil || v.Class == nil || len(v.Class.TypeParameters) == 0 || len(v.TypeBindings) == 0 {
			return value.TypeName()
		}
		parts := make([]string, 0, len(v.Class.TypeParameters))
		for _, p := range v.Class.TypeParameters {
			if bound, ok := v.TypeBindings[p]; ok && bound != "" {
				parts = append(parts, bound)
			}
		}
		if len(parts) == 0 {
			return value.TypeName()
		}
		return v.Class.Name + "<" + strings.Join(parts, ", ") + ">"
	}
	return value.TypeName()
}

func valueMatchesFunctionTypeRef(fn runtime.Function, value runtime.Value, typ *ast.TypeRef) bool {
	return matchValueToTypeRef(functionTypeParameterSetOrNil(fn), value, typ)
}

// matchValueToTypeRef is the recursive implementation of collection element type checking.
// typeParams is the pre-computed generic type parameter set (nil for non-generic contexts); a nil
// map is safe - Go map lookups on nil maps return the zero value.
func matchValueToTypeRef(typeParams map[string]bool, value runtime.Value, typ *ast.TypeRef) bool {
	if typ == nil || typ.Name == "any" {
		return true
	}
	if typ.Operator == "|" {
		return matchValueToTypeRef(typeParams, value, typ.Left) || matchValueToTypeRef(typeParams, value, typ.Right)
	}
	if typ.Operator == "&" {
		return matchValueToTypeRef(typeParams, value, typ.Left) && matchValueToTypeRef(typeParams, value, typ.Right)
	}
	if typ.Operator != "" {
		return true
	}
	// Generic type parameter accepts any value incl null at runtime, matching the VM.
	if typeParams[strings.ToLower(typ.Name)] {
		return true
	}
	// Null matches only a nullable type; also avoids null-deref in the element checks below.
	if _, isNull := value.(runtime.Null); isNull {
		return typ.Nullable
	}
	typeName := simpleTypeName(typ.Name)
	if typ.ListAlias || typeName == "list" {
		if value.TypeName() != "list" {
			return false
		}
		var elemType *ast.TypeRef
		if len(typ.Arguments) > 0 {
			elemType = typ.Arguments[0]
		} else if typ.ListAlias && typ.Name != "" && !strings.EqualFold(typ.Name, "list") {
			// T[] syntax: element type is the Name field (e.g. int[] → element type is int)
			elemType = &ast.TypeRef{Token: typ.Token, Name: typ.Name}
		}
		if elemType != nil && !typeParams[strings.ToLower(elemType.Name)] {
			lst := value.(*runtime.List)
			for _, elem := range lst.Elements {
				if !matchValueToTypeRef(typeParams, elem, elemType) {
					return false
				}
			}
		}
		return true
	}
	if typeName == "set" {
		if value.TypeName() != "set" {
			return false
		}
		if len(typ.Arguments) > 0 && !typeParams[strings.ToLower(typ.Arguments[0].Name)] {
			s := value.(runtime.Set)
			for _, entry := range s.Elements {
				if !matchValueToTypeRef(typeParams, entry.Value, typ.Arguments[0]) {
					return false
				}
			}
		}
		return true
	}
	if typeName == "dict" {
		if value.TypeName() != "dict" {
			return false
		}
		if len(typ.Arguments) >= 2 {
			keyIsTP := typeParams[strings.ToLower(typ.Arguments[0].Name)]
			valIsTP := typeParams[strings.ToLower(typ.Arguments[1].Name)]
			d := value.(runtime.Dict)
			ok := true
			d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				if !keyIsTP && !matchValueToTypeRef(typeParams, entry.Key, typ.Arguments[0]) {
					ok = false
					return false
				}
				if !valIsTP && !matchValueToTypeRef(typeParams, entry.Value, typ.Arguments[1]) {
					ok = false
					return false
				}
				return true
			})
			if !ok {
				return false
			}
		}
		return true
	}
	return valueMatchesTypeRef(value, typ)
}

// collectionMismatchSuffix returns a detail string like " (element at index 1 is string)"
// describing the first element that fails the type check. Returns "" when no mismatch is found
// or the type has no element-type arguments to check against.
func collectionMismatchSuffix(value runtime.Value, typ *ast.TypeRef) string {
	if typ == nil {
		return ""
	}
	switch v := value.(type) {
	case *runtime.List:
		var elemType *ast.TypeRef
		if len(typ.Arguments) > 0 {
			elemType = typ.Arguments[0]
		} else if typ.ListAlias && typ.Name != "" && !strings.EqualFold(typ.Name, "list") {
			elemType = &ast.TypeRef{Token: typ.Token, Name: typ.Name}
		}
		if elemType == nil {
			return ""
		}
		for i, elem := range v.Elements {
			if !matchValueToTypeRef(nil, elem, elemType) {
				return fmt.Sprintf(" (element at index %d is %s)", i, elem.TypeName())
			}
		}
	case runtime.Set:
		if len(typ.Arguments) == 0 {
			return ""
		}
		for _, entry := range v.Elements {
			if !matchValueToTypeRef(nil, entry.Value, typ.Arguments[0]) {
				return fmt.Sprintf(" (element %s is %s)", entry.Value.Inspect(), entry.Value.TypeName())
			}
		}
	case runtime.Dict:
		if len(typ.Arguments) < 2 {
			return ""
		}
		msg := ""
		v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			if !matchValueToTypeRef(nil, entry.Value, typ.Arguments[1]) {
				msg = fmt.Sprintf(" (value for key %s is %s)", entry.Key.Inspect(), entry.Value.TypeName())
				return false
			}
			return true
		})
		if msg != "" {
			return msg
		}
	}
	return ""
}

func typeRefUsesFunctionTypeParameter(fn runtime.Function, typ *ast.TypeRef) bool {
	if typ == nil {
		return false
	}
	params := functionTypeParameterSetOrNil(fn)
	if params == nil {
		return false
	}
	return typeRefUsesTypeParameter(typ, params)
}

func functionTypeParameterSet(fn runtime.Function) map[string]bool {
	params := map[string]bool{}
	for _, name := range fn.TypeParameters {
		params[strings.ToLower(name)] = true
	}
	return params
}

func functionTypeParameterSetOrNil(fn runtime.Function) map[string]bool {
	if len(fn.TypeParameters) == 0 {
		return nil
	}
	return functionTypeParameterSet(fn)
}

func typeRefUsesTypeParameter(typ *ast.TypeRef, params map[string]bool) bool {
	if typ == nil {
		return false
	}
	if params[strings.ToLower(typ.Name)] {
		return true
	}
	for _, arg := range typ.Arguments {
		if typeRefUsesTypeParameter(arg, params) {
			return true
		}
	}
	return typeRefUsesTypeParameter(typ.Left, params) || typeRefUsesTypeParameter(typ.Right, params)
}
