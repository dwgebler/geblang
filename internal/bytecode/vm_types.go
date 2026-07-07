package bytecode

import (
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (vm *VM) instanceOf(instruction Instruction) error {
	target, err := vm.popString(instruction, "instanceof target must be string")
	if err != nil {
		return err
	}
	value, err := vm.pop()
	if err != nil {
		return vm.callPropagate(instruction, err)
	}
	// Resolve type parameter binding if target is a generic type param name.
	if len(vm.frames) > 0 {
		if bindings := vm.frames[len(vm.frames)-1].typeBindings; bindings != nil {
			if bound, ok := bindings[target]; ok {
				target = bound
			}
		}
	}
	if module, name, isExact := parseExactInstanceofTarget(target); isExact {
		if module == "" {
			module = vm.moduleName
		}
		if instance, ok := value.(*runtime.Instance); ok {
			matched := runtime.InstanceClassMatchesExact(instance.Class, module, name)
			for _, extra := range instance.ExtraTypeNames {
				if matched {
					break
				}
				matched = strings.EqualFold(stripModulePrefix(extra), name)
			}
			vm.push(runtime.Bool{Value: matched})
			return nil
		}
		// Non-instance values keep the legacy name-based match on the bare name.
		target = name
	}
	if arms, ok := vmSplitTopLevelUnion(target); ok {
		for _, arm := range arms {
			vm.push(value)
			vm.push(runtime.String{Value: arm})
			if err := vm.instanceOf(instruction); err != nil {
				return err
			}
			result, perr := vm.pop()
			if perr != nil {
				return vm.runtimeError(instruction, "%s", perr.Error())
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				vm.push(runtime.Bool{Value: true})
				return nil
			}
		}
		vm.push(runtime.Bool{Value: false})
		return nil
	}
	if ev, ok := value.(runtime.EnumVariant); ok {
		if dotIdx := strings.Index(target, "."); dotIdx >= 0 {
			// `Enum.Variant` is a variant test; a `module.Interface`
			// qualified name (prefix is not this enum) falls through to
			// the name/interface check below.
			if enumName := target[:dotIdx]; strings.EqualFold(ev.Enum.Name, enumName) {
				vm.push(runtime.Bool{Value: strings.EqualFold(ev.Variant, target[dotIdx+1:])})
				return nil
			}
		}
		stripped := stripModulePrefix(target)
		if strings.EqualFold(ev.Enum.Name, stripped) {
			vm.push(runtime.Bool{Value: true})
			return nil
		}
		for _, iface := range ev.Enum.Implements {
			if strings.EqualFold(iface, stripped) {
				vm.push(runtime.Bool{Value: true})
				return nil
			}
		}
		vm.push(runtime.Bool{Value: false})
		return nil
	}
	if instance, ok := value.(*runtime.Instance); ok {
		if base, gargs, isGeneric := vmSplitGenericTypeName(stripModulePrefix(target)); isGeneric {
			// Frame-bound type-param names in the args resolve first.
			if len(vm.frames) > 0 {
				if bindings := vm.frames[len(vm.frames)-1].typeBindings; bindings != nil {
					for i, a := range gargs {
						if bound, ok := bindings[a]; ok {
							gargs[i] = bound
						}
					}
				}
			}
			vm.push(runtime.Bool{Value: vm.instanceMatchesParameterizedClass(instance, base, gargs)})
			return nil
		}
		// Try the chunk-local ClassInfo first (cheaper and lets
		// classImplements consult the per-chunk interface table).
		// Fall back to a direct walk of the runtime.Class parent
		// chain so cross-chunk class hierarchies - e.g. a class
		// imported from another module - also resolve correctly.
		stripped := stripModulePrefix(target)
		if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
			if vm.classMatches(classInfo, stripped) || vm.classImplements(classInfo, stripped) {
				vm.push(runtime.Bool{Value: true})
				return nil
			}
		}
		if runtimeClassMatches(instance.Class, stripped) {
			vm.push(runtime.Bool{Value: true})
			return nil
		}
		for _, extra := range instance.ExtraTypeNames {
			if strings.EqualFold(stripModulePrefix(extra), stripped) {
				vm.push(runtime.Bool{Value: true})
				return nil
			}
		}
		vm.push(runtime.Bool{Value: false})
		return nil
	}
	if errValue, ok := value.(runtime.Error); ok {
		// Error-derived class instances are wrapped as runtime.Error
		// rather than *runtime.Instance. Walk the parent chain
		// captured at construction so `instanceof Parent` matches an
		// error subclass even when the parent class was declared in
		// another module.
		stripped := stripModulePrefix(target)
		vm.push(runtime.Bool{Value: vm.errorValueMatches(errValue, stripped)})
		return nil
	}
	// `instanceof list<int>` and friends: split off the generic args
	// and dispatch element-aware matching. Tagged collections compare
	// the recorded element types; untagged collections walk elements.
	if base, args, ok := vmSplitGenericTypeName(target); ok {
		vm.push(runtime.Bool{Value: vmCollectionMatchesGeneric(value, base, args)})
		return nil
	}
	vm.push(runtime.Bool{Value: value.TypeName() == target})
	return nil
}

// instanceMatchesParameterizedClass: class chain contains base AND
// reified bindings match the args invariantly; unbound never matches.
func (vm *VM) instanceMatchesParameterizedClass(instance *runtime.Instance, base string, args []string) bool {
	chainMatches := false
	if classInfo, ok := vm.classInfo(instance.Class.Name); ok && vm.classMatches(classInfo, base) {
		chainMatches = true
	}
	if !chainMatches && runtimeClassMatches(instance.Class, base) {
		chainMatches = true
	}
	if !chainMatches {
		return false
	}
	var declared []string
	if classInfo, ok := vm.classInfo(base); ok {
		declared = classInfo.TypeParameters
	} else {
		for class := instance.Class; class != nil; class = class.Parent {
			if strings.EqualFold(class.Name, base) {
				declared = class.TypeParameters
				break
			}
		}
	}
	if len(declared) == 0 || len(args) > len(declared) {
		return false
	}
	for i, arg := range args {
		bound, ok := instance.TypeBindings[declared[i]]
		if !ok || !strings.EqualFold(bound, arg) {
			return false
		}
	}
	return true
}

func vmSplitGenericTypeName(typeName string) (string, []string, bool) {
	if strings.HasPrefix(typeName, "?") {
		typeName = typeName[1:]
	}
	lt := strings.IndexByte(typeName, '<')
	if lt < 0 || !strings.HasSuffix(typeName, ">") {
		return "", nil, false
	}
	base := typeName[:lt]
	inner := typeName[lt+1 : len(typeName)-1]
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

func vmCollectionMatchesGeneric(value runtime.Value, base string, args []string) bool {
	switch v := value.(type) {
	case *runtime.List:
		if base != "list" || len(args) != 1 {
			return false
		}
		if len(v.ElementTypes) >= 1 {
			return vmTypeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0])
		}
		for _, el := range v.Elements {
			if !vmValueMatchesSimpleType(el, args[0]) {
				return false
			}
		}
		return true
	case runtime.Set:
		if base != "set" || len(args) != 1 {
			return false
		}
		if len(v.ElementTypes) >= 1 {
			return vmTypeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0])
		}
		for _, e := range v.Elements {
			if !vmValueMatchesSimpleType(e.Value, args[0]) {
				return false
			}
		}
		return true
	case runtime.Dict:
		if base != "dict" || len(args) != 2 {
			return false
		}
		if len(v.ElementTypes) >= 2 {
			return vmTypeNameSatisfies(elementTagBase(v.ElementTypes[0]), args[0]) && vmTypeNameSatisfies(elementTagBase(v.ElementTypes[1]), args[1])
		}
		matches := true
		v.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
			if !vmValueMatchesSimpleType(e.Key, args[0]) || !vmValueMatchesSimpleType(e.Value, args[1]) {
				matches = false
				return false
			}
			return true
		})
		return matches
	}
	return false
}

func vmValueMatchesSimpleType(value runtime.Value, target string) bool {
	if target == "any" || target == "?any" {
		return true
	}
	if arms, ok := vmSplitTopLevelUnion(target); ok {
		for _, arm := range arms {
			if vmValueMatchesSimpleType(value, arm) {
				return true
			}
		}
		return false
	}
	if base, args, ok := vmSplitGenericTypeName(target); ok {
		return vmCollectionMatchesGeneric(value, base, args)
	}
	switch value.(type) {
	case runtime.SmallInt, runtime.Int:
		return target == "int"
	case runtime.Float:
		return target == "float"
	case runtime.Decimal:
		return target == "decimal"
	case runtime.String:
		return target == "string"
	case runtime.Bool:
		return target == "bool"
	case runtime.Bytes:
		return target == "bytes"
	}
	return strings.EqualFold(value.TypeName(), target)
}

// vmSplitTopLevelUnion splits a union on depth-0 `|`, preserving `|`
// inside nested generic angle brackets.
func vmSplitTopLevelUnion(typeName string) ([]string, bool) {
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

// vmTypeNameSatisfies handles `any` and union arms over the existing
// case-insensitive invariance rule for tagged collections.
// vmValueSatisfiesElementTag is the element-tag write barrier:
// name-level match first, then the value's class hierarchy (subclass
// and implementer writes into honestly-tagged collections are legal).
// elementSpecTag renders a generic-arg spec as a stored element tag,
// preserving a leading ? so the write barrier can accept null.
func elementSpecTag(a vmTypeSpec) string {
	if a.nullable {
		return "?" + a.base
	}
	return a.base
}

// elementTagBase drops a stored tag's nullable marker; instanceof and
// reflect read the base type, leaving nullability to the write barrier.
func elementTagBase(s string) string {
	return strings.TrimPrefix(s, "?")
}

func vmValueSatisfiesElementTag(value runtime.Value, tag string) bool {
	// A nullable element tag (?T) accepts null; otherwise match the base.
	if strings.HasPrefix(tag, "?") {
		if _, ok := value.(runtime.Null); ok {
			return true
		}
		tag = tag[1:]
	}
	if vmTypeNameSatisfies(value.TypeName(), tag) {
		return true
	}
	if arms, ok := vmSplitTopLevelUnion(tag); ok {
		for _, arm := range arms {
			if vmValueSatisfiesElementTag(value, arm) {
				return true
			}
		}
		return false
	}
	return runtime.ValueSatisfiesHierarchyLeaf(value, stripModulePrefix(strings.TrimSpace(strings.TrimPrefix(tag, "?"))))
}

// vmCheckDictWriteTags enforces a tagged dict's key and value types on
// any write path.
func vmCheckDictWriteTags(dict runtime.Dict, key, value runtime.Value) error {
	if len(dict.ElementTypes) < 2 {
		return nil
	}
	if !vmValueSatisfiesElementTag(key, dict.ElementTypes[0]) {
		return vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot use %s key in dict<%s, %s>", key.TypeName(), dict.ElementTypes[0], dict.ElementTypes[1])}
	}
	if !vmValueSatisfiesElementTag(value, dict.ElementTypes[1]) {
		return vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot assign %s to dict<%s, %s>", value.TypeName(), dict.ElementTypes[0], dict.ElementTypes[1])}
	}
	return nil
}

func vmTypeNameSatisfies(have, want string) bool {
	if want == "any" || want == "?any" {
		return true
	}
	if arms, ok := vmSplitTopLevelUnion(want); ok {
		for _, arm := range arms {
			if vmTypeNameSatisfies(have, arm) {
				return true
			}
		}
		return false
	}
	if vmCallableTypeName(want) && vmCallableTypeName(have) {
		return true
	}
	return strings.EqualFold(have, want)
}

func vmCallableTypeName(name string) bool {
	return strings.EqualFold(name, "func") || strings.EqualFold(name, "callable") || strings.EqualFold(name, "function")
}

func (vm *VM) cast(instruction Instruction, ip int) (int, error) {
	target, err := vm.popString(instruction, "cast target must be string")
	if err != nil {
		return 0, err
	}
	value, err := vm.pop()
	if err != nil {
		return 0, vm.callPropagate(instruction, err)
	}
	// Class / interface / parent-chain widening cast: an Error or
	// Instance is assignable to any ancestor in its chain, with the
	// module prefix on the target name stripped (so `e as errors.X`
	// matches `e` whose class extends X declared in any module).
	stripped := stripModulePrefix(target)
	if errValue, ok := value.(runtime.Error); ok {
		if vm.errorValueMatches(errValue, stripped) {
			vm.push(value)
			return ip, nil
		}
	}
	if instance, ok := value.(*runtime.Instance); ok {
		if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
			if vm.classMatches(classInfo, stripped) || vm.classImplements(classInfo, stripped) {
				vm.push(value)
				return ip, nil
			}
		}
		if runtimeClassMatches(instance.Class, stripped) {
			vm.push(value)
			return ip, nil
		}
		if dunder := castDunderName(target); dunder != "" {
			if result, handled, err := vm.invokeInstanceMethod(instance, dunder, nil); err != nil {
				return 0, vm.callPropagate(instruction, err)
			} else if handled {
				if err := checkCastDunderReturn(target, result); err != nil {
					return vm.throwTyped(instruction, ip, "RuntimeError", err.Error())
				}
				vm.push(result)
				return ip, nil
			}
		}
	}
	// A callable-typed cast accepts any invocable value as-is (mirrors the evaluator); reflect targets carry their bound callable here.
	if vmCallableTypeName(stripped) && runtime.IsCallableValue(value) {
		vm.push(value)
		return ip, nil
	}
	cast, err := castValue(value, target)
	if err != nil {
		// Cast failures are user-catchable as a thrown RuntimeError, matching the evaluator.
		return vm.throwTyped(instruction, ip, "RuntimeError", err.Error())
	}
	vm.push(cast)
	return ip, nil
}

func (vm *VM) selectRuntimeFunction(instruction Instruction, name string, indices []int64, args []runtime.Value, paramOffset int) (int64, error) {
	return vm.selectRuntimeFunctionWith(instruction, name, indices, args, paramOffset, nil)
}

// selectRuntimeFunctionWith breaks overload-selection ties with explicit
// bindings; single-candidate mismatches stay with construct-site validation.
func (vm *VM) selectRuntimeFunctionWith(instruction Instruction, name string, indices []int64, args []runtime.Value, paramOffset int, inherited map[string]string) (int64, error) {
	/* Fast path: most classes declare a single overload per method.
	 * Skip the slice allocation + post-loop "ambiguous" check and
	 * just verify arity + types directly on the lone candidate. */
	if len(indices) == 1 {
		index := indices[0]
		if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
			return 0, vm.runtimeError(instruction, "method index out of range")
		}
		function := vm.curMod.Chunk.Functions[index]
		min, max, variadic := bytecodeFunctionArityRange(function, paramOffset)
		if len(args) < min || (!variadic && len(args) > max) {
			return 0, vm.runtimeError(instruction, "no matching overload for %s", name)
		}
		if !vm.runtimeArgumentsMatch(function, args, paramOffset) {
			return 0, vm.runtimeError(instruction, "no matching overload for %s", name)
		}
		return index, nil
	}
	matches := []int64{}
	for _, index := range indices {
		if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
			return 0, vm.runtimeError(instruction, "method index out of range")
		}
		function := vm.curMod.Chunk.Functions[index]
		min, max, variadic := bytecodeFunctionArityRange(function, paramOffset)
		if len(args) < min || (!variadic && len(args) > max) {
			continue
		}
		if !vm.runtimeArgumentsMatch(function, args, paramOffset) {
			continue
		}
		matches = append(matches, index)
	}
	if len(matches) > 1 && len(inherited) > 0 {
		kept := matches[:0]
		for _, index := range matches {
			if vm.runtimeArgumentsMatchWith(vm.curMod.Chunk.Functions[index], args, paramOffset, inherited) {
				kept = append(kept, index)
			}
		}
		if len(kept) > 0 {
			matches = kept
		}
	}
	if len(matches) == 0 {
		return 0, vm.runtimeError(instruction, "no matching overload for %s", name)
	}
	if len(matches) > 1 {
		return 0, vm.runtimeError(instruction, "ambiguous overload for %s", name)
	}
	return matches[0], nil
}

func (vm *VM) selectRuntimeNamedFunction(instruction Instruction, name string, indices []int64, args []runtime.Value, names []string, paramOffset int) (int64, []runtime.Value, error) {
	matches := []int64{}
	orderedMatches := [][]runtime.Value{}
	for _, index := range indices {
		if index < 0 || int(index) >= len(vm.curMod.Chunk.Functions) {
			return 0, nil, vm.runtimeError(instruction, "method index out of range")
		}
		ordered, err := vm.orderRuntimeArguments(instruction, vm.curMod.Chunk.Functions[index], args, names, paramOffset)
		if err != nil {
			continue
		}
		if !vm.runtimeArgumentsMatch(vm.curMod.Chunk.Functions[index], ordered, paramOffset) {
			continue
		}
		matches = append(matches, index)
		orderedMatches = append(orderedMatches, ordered)
	}
	if len(matches) == 0 {
		return 0, nil, vm.runtimeError(instruction, "no matching overload for %s", name)
	}
	if len(matches) > 1 {
		return 0, nil, vm.runtimeError(instruction, "ambiguous overload for %s", name)
	}
	return matches[0], orderedMatches[0], nil
}

func (vm *VM) runtimeArgumentsMatch(function FunctionInfo, args []runtime.Value, paramOffset int) bool {
	return vm.runtimeArgumentsMatchWith(function, args, paramOffset, nil)
}

func (vm *VM) runtimeArgumentsMatchWith(function FunctionInfo, args []runtime.Value, paramOffset int, inherited map[string]string) bool {
	if len(function.ParamTypes) == 0 {
		return true
	}
	typeParams := function.typeParamSet
	for i, arg := range args {
		paramIndex := i + paramOffset
		if paramIndex >= len(function.ParamTypes) {
			if !function.Variadic || len(function.ParamTypes) == 0 {
				return false
			}
			// Extra variadic args match the variadic param's element type.
			paramIndex = len(function.ParamTypes) - 1
		}
		var spec vmTypeSpec
		if paramIndex < len(function.paramTypeSpecs) && function.paramTypeSpecs[paramIndex].raw != "" {
			spec = function.paramTypeSpecs[paramIndex]
		} else {
			spec = vm.typeSpec(function.ParamTypes[paramIndex])
		}
		if !vm.matchValueToTypeSpecWith(typeParams, inherited, arg, spec) {
			return false
		}
	}
	return true
}

// descriptiveRuntimeTypeName returns a type name that includes element type info where detectable,
// e.g. "list<string>" instead of "list". For reified user-defined generic
// class instances it also unspools the recorded TypeBindings -
// "Container<Sub>" rather than the bare "Container" - so error messages
// about invariant-parameter mismatches surface the caller's actual
// binding rather than just the class name.
func (vm *VM) descriptiveRuntimeTypeName(value runtime.Value) string {
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

// parseTypeStr splits a generic type string like "list<dict<string,int>>" into
// base="list" and inner="dict<string,int>". Returns (s, "", false) for non-generic types.
func parseTypeStr(s string) (base, inner string, hasInner bool) {
	lt := strings.IndexByte(s, '<')
	if lt < 0 {
		return s, "", false
	}
	depth := 0
	for i := lt; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return s[:lt], s[lt+1 : i], true
			}
		}
	}
	return s, "", false
}

// splitTopLevelTypeOp scans for the supplied operator byte ('|' or
// '&') at depth zero in a type-string and splits on every
// occurrence. Returns ok=true and the trimmed branch list when the
// operator was found at the top level; ok=false leaves the input
// untouched. Generic argument lists (`<...>`) are skipped so that
// `dict<int, string>` doesn't tokenise its inner comma or any
// nested generic operators.
func splitTopLevelTypeOp(s string, op byte) ([]string, bool) {
	depth := 0
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case op:
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if len(parts) == 0 {
		return nil, false
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts, true
}

// splitTypeArgs splits a comma-separated type argument list respecting nested angle brackets.
// e.g. "string,dict<string,int>" → ["string", "dict<string,int>"]
func splitTypeArgs(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(parts, strings.TrimSpace(s[start:]))
}

// matchValueToTypeStr is the internal recursive implementation of VM type checking.
// typeParams is the pre-computed set of generic type parameter names (nil for non-generic contexts;
// a nil map is safe - Go map lookups on nil maps return the zero value).
func (vm *VM) matchValueToTypeStr(typeParams map[string]bool, value runtime.Value, typ string) bool {
	spec := vm.typeSpec(typ)
	return vm.matchValueToTypeSpec(typeParams, value, spec)
}

// matchValueToTypeStrWith is matchValueToTypeStr extended with an
// inheritedBindings map. When a type-parameter name in the spec is not
// declared by the function being entered but IS bound by the caller's
// outer generic frame, the binding's concrete type is substituted and
// re-checked. Used by the closure call path so that lambdas and
// named-generic-function references resolve T correctly.
func (vm *VM) matchValueToTypeStrWith(typeParams map[string]bool, inherited map[string]string, value runtime.Value, typ string) bool {
	spec := vm.typeSpec(typ)
	return vm.matchValueToTypeSpecWith(typeParams, inherited, value, spec)
}

func (vm *VM) matchValueToTypeSpecWith(typeParams map[string]bool, inherited map[string]string, value runtime.Value, spec vmTypeSpec) bool {
	// Concrete bindings win over the bare-type-param accept, matching the
	// evaluator: an explicit call-site binding (Box<string>(...)) constrains
	// the function's own T rather than leaving it inference-open.
	if len(inherited) > 0 {
		if bound, ok := inherited[spec.base]; ok && bound != "" {
			return vm.matchValueToTypeSpec(typeParams, value, vm.typeSpec(bound))
		}
		if bound, ok := inherited[spec.baseLower]; ok && bound != "" {
			return vm.matchValueToTypeSpec(typeParams, value, vm.typeSpec(bound))
		}
	}
	if typeParams[spec.baseLower] {
		return true
	}
	return vm.matchValueToTypeSpec(typeParams, value, spec)
}

func (vm *VM) typeSpec(typ string) vmTypeSpec {
	typ = strings.TrimSpace(typ)
	if vm.typeSpecCache == nil {
		vm.typeSpecCache = map[string]vmTypeSpec{}
	}
	if spec, ok := vm.typeSpecCache[typ]; ok {
		return spec
	}
	spec := parseVMTypeSpec(typ)
	vm.typeSpecCache[typ] = spec
	return spec
}

func parseVMTypeSpec(typ string) vmTypeSpec {
	raw := strings.TrimSpace(typ)
	// Top-level `|` or `&` (outside angle brackets) builds a
	// union / intersection spec whose args are the branches.
	if branches, op := splitTopLevelTypeOp(raw, '|'); op {
		spec := vmTypeSpec{raw: raw, kind: vmTypeUnion}
		for _, b := range branches {
			spec.args = append(spec.args, parseVMTypeSpec(b))
		}
		// A union is "nullable" when any branch is the explicit
		// null type or a ?T sigil. This lets the early-return
		// for VMKindNull in matchValueToTypeSpec stay accurate.
		for _, arg := range spec.args {
			if arg.nullable || arg.baseLower == "null" {
				spec.nullable = true
				break
			}
		}
		return spec
	}
	if branches, op := splitTopLevelTypeOp(raw, '&'); op {
		spec := vmTypeSpec{raw: raw, kind: vmTypeIntersection}
		for _, b := range branches {
			spec.args = append(spec.args, parseVMTypeSpec(b))
		}
		return spec
	}
	baseTyp, innerTyp, hasInner := parseTypeStr(raw)
	base := strings.TrimSpace(baseTyp)
	nullable := strings.HasPrefix(base, "?")
	/* Strip the leading `?` from `base` so callers comparing it to a
	 * value's TypeName / class name don't have to handle the nullable
	 * sigil themselves. The `nullable` flag carries that bit. */
	base = strings.TrimPrefix(base, "?")
	baseLower := strings.ToLower(base)
	spec := vmTypeSpec{
		raw:       raw,
		base:      base,
		baseLower: baseLower,
		nullable:  nullable,
		kind:      vmTypeKindForBase(baseLower),
	}
	if hasInner {
		for _, arg := range splitTypeArgs(innerTyp) {
			if arg != "" {
				spec.args = append(spec.args, parseVMTypeSpec(arg))
			}
		}
	}
	return spec
}

func vmTypeKindForBase(baseLower string) vmTypeKind {
	switch normalizeCallableTypeName(baseLower) {
	case "", "any":
		return vmTypeAny
	case "int":
		return vmTypeInt
	case "string":
		return vmTypeString
	case "bool":
		return vmTypeBool
	case "float":
		return vmTypeFloat
	case "decimal":
		return vmTypeDecimal
	case "list":
		return vmTypeList
	case "set":
		return vmTypeSet
	case "dict":
		return vmTypeDict
	case "func":
		return vmTypeCallable
	case "generator", "iterable":
		return vmTypeGenerator
	default:
		return vmTypeOther
	}
}

func (vm *VM) matchValueToTypeSpec(typeParams map[string]bool, value runtime.Value, spec vmTypeSpec) bool {
	if typeParams[spec.baseLower] {
		return true
	}
	if spec.kind == vmTypeAny {
		return true
	}
	if spec.kind == vmTypeUnion {
		if _, isNull := value.(runtime.Null); isNull && spec.nullable {
			return true
		}
		for _, branch := range spec.args {
			if vm.matchValueToTypeSpec(typeParams, value, branch) {
				return true
			}
		}
		return false
	}
	if spec.kind == vmTypeIntersection {
		for _, branch := range spec.args {
			if !vm.matchValueToTypeSpec(typeParams, value, branch) {
				return false
			}
		}
		return true
	}
	// Null is assignable to any nullable type, regardless of element
	// parameterisation. The element walk below would otherwise type-assert
	// the null as a List/Set/Dict and panic.
	if _, isNull := value.(runtime.Null); isNull {
		return spec.nullable
	}
	if !vm.runtimeValueMatchesTypeSpec(value, spec) {
		return false
	}
	if len(spec.args) == 0 {
		return true
	}
	switch spec.baseLower {
	case "list":
		elemSpec := spec.args[0]
		if typeParams[elemSpec.baseLower] {
			break
		}
		lst := value.(*runtime.List)
		for _, elem := range lst.Elements {
			if !vm.matchValueToTypeSpec(typeParams, elem, elemSpec) {
				return false
			}
		}
	case "set":
		elemSpec := spec.args[0]
		if typeParams[elemSpec.baseLower] {
			break
		}
		s := value.(runtime.Set)
		for _, entry := range s.Elements {
			if !vm.matchValueToTypeSpec(typeParams, entry.Value, elemSpec) {
				return false
			}
		}
	case "dict":
		if len(spec.args) == 2 {
			keySpec := spec.args[0]
			valSpec := spec.args[1]
			d := value.(runtime.Dict)
			ok := true
			d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				if !typeParams[keySpec.baseLower] && !vm.matchValueToTypeSpec(typeParams, entry.Key, keySpec) {
					ok = false
					return false
				}
				if !typeParams[valSpec.baseLower] && !vm.matchValueToTypeSpec(typeParams, entry.Value, valSpec) {
					ok = false
					return false
				}
				return true
			})
			if !ok {
				return false
			}
		}
	default:
		// Reified user-defined generic class instance: enforce invariance
		// on the bound type parameters. A typed parameter declared as
		// `Box<Base>` must NOT accept a `Box<Sub>` value, because
		// mutating methods on the parameter could otherwise insert a
		// sibling `Base` subtype that violates the original container's
		// declared element type (the same unsoundness that motivates
		// invariance in Kotlin/Java).
		if instance, ok := value.(*runtime.Instance); ok && instance.Class != nil &&
			len(instance.Class.TypeParameters) > 0 && len(instance.TypeBindings) > 0 {
			for i, argSpec := range spec.args {
				if i >= len(instance.Class.TypeParameters) {
					break
				}
				if typeParams[argSpec.baseLower] || argSpec.baseLower == "" || argSpec.kind == vmTypeAny {
					continue
				}
				paramName := instance.Class.TypeParameters[i]
				bound, ok := instance.TypeBindings[paramName]
				if !ok || bound == "" {
					continue
				}
				if !strings.EqualFold(bound, argSpec.base) {
					return false
				}
			}
		}
	}
	return true
}

// collectionMismatchSuffixStr returns a detail string like " (element at index 1 is string)"
// describing the first element that violates the type constraint, or "" when there is no mismatch.
func (vm *VM) collectionMismatchSuffixStr(value runtime.Value, typ string) string {
	spec := vm.typeSpec(typ)
	if len(spec.args) == 0 {
		return ""
	}
	switch v := value.(type) {
	case *runtime.List:
		elemSpec := spec.args[0]
		for i, elem := range v.Elements {
			if !vm.matchValueToTypeSpec(nil, elem, elemSpec) {
				return fmt.Sprintf(" (element at index %d is %s)", i, elem.TypeName())
			}
		}
	case runtime.Set:
		elemSpec := spec.args[0]
		for _, entry := range v.Elements {
			if !vm.matchValueToTypeSpec(nil, entry.Value, elemSpec) {
				return fmt.Sprintf(" (element %s is %s)", entry.Value.Inspect(), entry.Value.TypeName())
			}
		}
	case runtime.Dict:
		if len(spec.args) == 2 {
			valSpec := spec.args[1]
			msg := ""
			v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				if !vm.matchValueToTypeSpec(nil, entry.Value, valSpec) {
					msg = fmt.Sprintf(" (value for key %s is %s)", entry.Key.Inspect(), entry.Value.TypeName())
					return false
				}
				return true
			})
			if msg != "" {
				return msg
			}
		}
	}
	return ""
}

func (vm *VM) runtimeValueMatchesFunctionType(function FunctionInfo, value runtime.Value, typ string) bool {
	typeParams := function.typeParamSet
	return vm.matchValueToTypeStr(typeParams, value, typ)
}

func (vm *VM) runtimeValueMatchesTypeSpec(value runtime.Value, spec vmTypeSpec) bool {
	if spec.kind == vmTypeAny {
		return true
	}
	if _, ok := value.(runtime.Null); ok {
		return spec.nullable
	}
	switch spec.kind {
	case vmTypeInt:
		switch value.(type) {
		case runtime.SmallInt, runtime.Int:
			return true
		}
		return false
	case vmTypeString:
		_, ok := value.(runtime.String)
		return ok
	case vmTypeBool:
		_, ok := value.(runtime.Bool)
		return ok
	case vmTypeFloat:
		_, ok := value.(runtime.Float)
		return ok
	case vmTypeDecimal:
		_, ok := value.(runtime.Decimal)
		return ok
	case vmTypeList:
		_, ok := value.(*runtime.List)
		return ok
	case vmTypeSet:
		_, ok := value.(runtime.Set)
		return ok
	case vmTypeDict:
		_, ok := value.(runtime.Dict)
		return ok
	case vmTypeCallable:
		return runtime.IsCallableValue(value)
	case vmTypeGenerator:
		_, ok := value.(*runtime.Generator)
		return ok
	}
	if value.TypeName() == spec.base {
		return true
	}
	stripped := stripModulePrefix(spec.base)
	// Error-derived values: walk the captured parent chain so a
	// parameter typed `HttpException` accepts a `BadRequestError`.
	if errValue, ok := value.(runtime.Error); ok {
		if strings.EqualFold(errValue.Class, stripped) {
			return true
		}
		for _, ancestor := range errValue.Parents {
			if strings.EqualFold(ancestor, stripped) {
				return true
			}
		}
		return false
	}
	if ev, ok := value.(runtime.EnumVariant); ok {
		for _, iface := range ev.Enum.Implements {
			if strings.EqualFold(iface, stripped) {
				return true
			}
		}
		return false
	}
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return false
	}
	if classInfo, ok := vm.classInfo(instance.Class.Name); ok {
		if vm.classMatches(classInfo, stripped) || vm.classImplements(classInfo, stripped) {
			return true
		}
	}
	// Fall back to the cross-chunk runtime.Class chain (set up at
	// instance construction) so parameters typed with an imported
	// class still match.
	return runtimeClassMatches(instance.Class, stripped)
}

// castDunderName returns the dunder method name a class can define
// to control its `as TARGET` conversion. Empty string means no
// dunder is recognised for that target.
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

// checkCastDunderReturn validates that a cast dunder produced a
// value compatible with the target type.
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
	// A native value's TypeName is unqualified, so a module-qualified target (streams.IOStream) matches on its stripped name, mirroring the evaluator.
	if value.TypeName() == target || value.TypeName() == stripModulePrefix(target) {
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
	case "any":
		// Widening to the top type is a no-op; the value keeps its dynamic
		// type. The evaluator's valueMatchesType allows this at the top of cast.
		return value, nil
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
		case runtime.SmallInt:
			return v, nil
		case runtime.Int:
			return v, nil
		case runtime.String:
			value, err := runtime.NewIntLiteral(v.Value)
			if err != nil {
				return nil, err
			}
			if value.Value.IsInt64() {
				return runtime.SmallInt{Value: value.Value.Int64()}, nil
			}
			return value, nil
		case runtime.Decimal:
			/* Truncate toward zero: big.Int.Quo handles arbitrary
			 * precision correctly. */
			num := new(big.Int).Set(v.Value.Num())
			den := v.Value.Denom()
			q := new(big.Int).Quo(num, den)
			if q.IsInt64() {
				return runtime.SmallInt{Value: q.Int64()}, nil
			}
			return runtime.Int{Value: q}, nil
		case runtime.Float:
			return runtime.SmallInt{Value: int64(math.Trunc(v.Value))}, nil
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
			return native.IntToDecimal(v), nil
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
		 * (`bytes as string`) is handled in the "string" case above. */
		if v, ok := value.(runtime.String); ok {
			b := make([]byte, len(v.Value))
			copy(b, v.Value)
			return runtime.Bytes{Value: b}, nil
		}
	case "list":
		/* `set as list` materializes; the underlying map's range order
		 * means the resulting list ordering is unspecified (sets are
		 * unordered by design). */
		if v, ok := value.(runtime.Set); ok {
			out := make([]runtime.Value, 0, len(v.Elements))
			for _, entry := range v.Elements {
				out = append(out, entry.Value)
			}
			return &runtime.List{Elements: out}, nil
		}
	case "set":
		/* `list as set` de-duplicates. First occurrence wins. */
		if v, ok := value.(*runtime.List); ok {
			elements := make(map[string]runtime.SetEntry, len(v.Elements))
			for _, elem := range v.Elements {
				k := native.DictKey(elem)
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
