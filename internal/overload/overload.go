// Package overload is the canonical positional overload selector shared by both backends, so an overloaded function invoked as a value (a callback) picks the same overload on the evaluator and the bytecode VM.
package overload

import (
	"fmt"
	"strings"

	"geblang/internal/binding"
	"geblang/internal/runtime"
)

// Select returns the single overload whose signature binds the positional args and whose parameter types accept them; errors mirror the language's direct-call rules (no match / ambiguous).
func Select(name string, overloads []runtime.Function, args []runtime.Value) (runtime.Function, error) {
	_, fn, err := SelectWithIndex(name, overloads, args)
	return fn, err
}

// SelectWithIndex is Select but also returns the winner's position in overloads, so a caller can map it back to the home-chunk function index (needed to route an async winner through the worker on its own snapshot rather than a vm-capturing wrapper).
func SelectWithIndex(name string, overloads []runtime.Function, args []runtime.Value) (int, runtime.Function, error) {
	matchedIdx := -1
	count := 0
	for i, fn := range overloads {
		if bindsPositional(fn, len(args)) && argsMatchParamTypes(fn, args) {
			count++
			matchedIdx = i
		}
	}
	switch count {
	case 0:
		return -1, runtime.Function{}, fmt.Errorf("no matching overload for %s", binding.DisplayName(name))
	case 1:
		return matchedIdx, overloads[matchedIdx], nil
	default:
		return -1, runtime.Function{}, fmt.Errorf("ambiguous overload for %s", binding.DisplayName(name))
	}
}

func bindsPositional(fn runtime.Function, argc int) bool {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		return true
	}
	sig := binding.Signature{FuncName: fn.Name, ParamNames: make([]string, len(fn.Parameters)), HasDefault: make([]bool, len(fn.Parameters))}
	for i, p := range fn.Parameters {
		if p.Name != nil {
			sig.ParamNames[i] = p.Name.Value
		}
		sig.HasDefault[i] = p.Default != nil
	}
	if len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic {
		sig.Variadic = true
		sig.HasDefault[len(sig.HasDefault)-1] = true
	}
	bargs := make([]binding.Arg, argc)
	_, err := binding.Order(sig, bargs)
	return err == nil
}

func argsMatchParamTypes(fn runtime.Function, args []runtime.Value) bool {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		return true
	}
	typeParams := map[string]bool{}
	for _, tp := range fn.TypeParameters {
		typeParams[strings.ToLower(tp)] = true
	}
	variadic := len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	for i, arg := range args {
		if arg == nil {
			continue
		}
		paramIdx := i
		if i >= len(fn.Parameters) {
			if !variadic {
				return false
			}
			paramIdx = len(fn.Parameters) - 1
		}
		typeStr := ""
		if fn.Parameters[paramIdx].Type != nil {
			typeStr = fn.Parameters[paramIdx].Type.String()
		}
		if !valueMatchesTypeString(typeParams, arg, typeStr) {
			return false
		}
	}
	return true
}

// valueMatchesTypeString accepts arg against a parameter's source-string type (any, nullability, unions, generic params, base name with generic args stripped) - focused on overload disambiguation, not full assignability.
func valueMatchesTypeString(typeParams map[string]bool, arg runtime.Value, typeStr string) bool {
	typeStr = strings.TrimSpace(typeStr)
	if typeStr == "" || typeStr == "any" {
		return true
	}
	if parts := splitTopLevelUnion(typeStr); len(parts) > 1 {
		for _, p := range parts {
			if valueMatchesTypeString(typeParams, arg, p) {
				return true
			}
		}
		return false
	}
	nullable := strings.HasPrefix(typeStr, "?")
	if nullable {
		typeStr = strings.TrimSpace(typeStr[1:])
	}
	if _, isNull := arg.(runtime.Null); isNull {
		return nullable || typeStr == "" || typeStr == "any"
	}
	if typeParams[strings.ToLower(typeStr)] {
		return true
	}
	if cbase, genArgs, isGeneric := splitGenericArgs(typeStr); isGeneric {
		if handled, ok := collectionMatches(typeParams, arg, cbase, genArgs); handled {
			return ok
		}
	}
	base := typeStr
	if i := strings.IndexByte(base, '<'); i >= 0 {
		base = base[:i]
	}
	base = strings.ToLower(strings.TrimSpace(base))
	// Strip a module qualifier (animals.Animal -> animal) so a cross-module parameter type matches the bare runtime class name, as the direct-call selectors do.
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		base = base[i+1:]
	}
	if base == "" || base == "any" {
		return true
	}
	if strings.EqualFold(arg.TypeName(), base) {
		return true
	}
	// Match the direct-call selectors: a subclass, interface implementer, error parent, or enum implementer also accepts the base type.
	return runtime.ValueSatisfiesHierarchyLeaf(arg, base)
}

// splitGenericArgs splits "list<int>" into ("list", ["int"]) and "dict<string, int>" into ("dict", ["string","int"]); ok is false with no generic clause.
func splitGenericArgs(typeStr string) (string, []string, bool) {
	lt := strings.IndexByte(typeStr, '<')
	if lt < 0 || !strings.HasSuffix(typeStr, ">") {
		return "", nil, false
	}
	base := strings.ToLower(strings.TrimSpace(typeStr[:lt]))
	inner := typeStr[lt+1 : len(typeStr)-1]
	var args []string
	depth, start := 0, 0
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
	args = append(args, strings.TrimSpace(inner[start:]))
	return base, args, true
}

// collectionMatches mirrors the evaluator's callback matcher for generic collections: it walks the actual elements (covariant), the same way the evaluator does at a callback boundary where declared element tags are not consulted. handled is false when arg is not the collection kind the base names.
func collectionMatches(typeParams map[string]bool, arg runtime.Value, base string, genArgs []string) (handled bool, ok bool) {
	switch v := arg.(type) {
	case *runtime.List:
		if base != "list" || len(genArgs) != 1 {
			return true, false
		}
		for _, el := range v.Elements {
			if !valueMatchesTypeString(typeParams, el, genArgs[0]) {
				return true, false
			}
		}
		return true, true
	case runtime.Set:
		if base != "set" || len(genArgs) != 1 {
			return true, false
		}
		for _, e := range v.Elements {
			if !valueMatchesTypeString(typeParams, e.Value, genArgs[0]) {
				return true, false
			}
		}
		return true, true
	case runtime.Dict:
		if base != "dict" || len(genArgs) != 2 {
			return true, false
		}
		matched := true
		v.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
			if !valueMatchesTypeString(typeParams, e.Key, genArgs[0]) || !valueMatchesTypeString(typeParams, e.Value, genArgs[1]) {
				matched = false
				return false
			}
			return true
		})
		return true, matched
	case *runtime.Instance:
		// A user-generic instance (Box<Dog>) matches a Box<X> parameter only when its type bindings equal X (invariant, like the evaluator); a function type parameter in X position is a wildcard.
		if v.Class == nil || bareTypeName(v.Class.Name) != bareTypeName(base) {
			return true, false
		}
		tps := v.Class.TypeParameters
		if len(tps) != len(genArgs) {
			return true, false
		}
		for i, tp := range tps {
			want := strings.TrimSpace(genArgs[i])
			if typeParams[strings.ToLower(want)] {
				continue
			}
			bound, ok := v.TypeBindings[tp]
			if !ok || bareTypeName(bound) != bareTypeName(want) {
				return true, false
			}
		}
		return true, true
	}
	return false, false
}

// bareTypeName lowercases a type name and strips any module qualifier so cross-module names compare by their bare name.
func bareTypeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func splitTopLevelUnion(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case '|':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}
