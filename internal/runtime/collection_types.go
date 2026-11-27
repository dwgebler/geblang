package runtime

import (
	"strings"
	"sync"
	"sync/atomic"

	"geblang/internal/ast"
	"geblang/internal/parser"
	"geblang/internal/typealias"
)

type CollectionTyping struct {
	declared atomic.Pointer[collectionType]
	fallback atomic.Pointer[collectionType]
}

type collectionType struct {
	ref  *ast.TypeRef
	tags []string
}

var collectionBindingMu sync.Mutex

func NewSet(elements map[string]SetEntry) Set {
	return Set{Elements: elements, Typing: &CollectionTyping{}}
}

func collectionTyping(value Value) *CollectionTyping {
	switch v := value.(type) {
	case *List:
		return &v.typing
	case Dict:
		if v.dictHeader != nil {
			if v.data != nil {
				return &v.data.typing
			}
			return v.typing
		}
	case Set:
		return v.Typing
	}
	return nil
}

func CollectionDeclaredType(value Value) *ast.TypeRef {
	if state := collectionTyping(value); state != nil {
		if declared := state.declared.Load(); declared != nil {
			return declared.ref
		}
	}
	return nil
}

func collectionRef(ref *ast.TypeRef) *ast.TypeRef {
	if ref == nil || ref.Operator != "" {
		return nil
	}
	if ref.ListAlias {
		elem := *ref
		elem.ListAlias, elem.Nullable = false, false
		return &ast.TypeRef{Name: "list", Nullable: ref.Nullable, Arguments: []*ast.TypeRef{&elem}}
	}
	switch strings.ToLower(ref.Name) {
	case "list", "set":
		if len(ref.Arguments) == 1 {
			return ref
		}
	case "dict":
		if len(ref.Arguments) == 2 {
			return ref
		}
	}
	return nil
}

func collectionTypeShape(ref *ast.TypeRef) bool {
	if ref == nil {
		return false
	}
	if ref.Operator != "" {
		return collectionTypeShape(ref.Left) || collectionTypeShape(ref.Right)
	}
	if ref.ListAlias {
		return true
	}
	switch strings.ToLower(ref.Name) {
	case "list", "set":
		return len(ref.Arguments) == 1
	case "dict":
		return len(ref.Arguments) == 2
	}
	return false
}

func typeIdentity(ref *ast.TypeRef) string {
	if strings.EqualFold(ref.Name, "callable") || strings.EqualFold(ref.Name, "function") {
		return "func"
	}
	if ref.CanonicalName != "" {
		return ref.CanonicalName
	}
	if ref.ResolvedModule != "" {
		return QualifiedTypeName(ref.ResolvedModule, bareTypeName(ref.Name))
	}
	return ref.Name
}

func CollectionTypeAccepts(have, want *ast.TypeRef) bool {
	if have == nil || want == nil {
		return false
	}
	if have == want {
		return true
	}
	if have.Operator == "" && want.Operator == "" && have.Name == want.Name && have.Nullable == want.Nullable && have.ListAlias == want.ListAlias && have.CanonicalName == want.CanonicalName && have.ResolvedModule == want.ResolvedModule && len(have.Arguments) == len(want.Arguments) {
		equal := true
		for i, arg := range have.Arguments {
			if arg != want.Arguments[i] {
				equal = false
				break
			}
		}
		if equal {
			return true
		}
	}
	if want.Name == "any" && want.Operator == "" {
		return true
	}
	if have.Name == "null" && have.Operator == "" {
		return collectionTypeAllowsNull(want)
	}
	if have.Nullable {
		if !collectionTypeAllowsNull(want) {
			return false
		}
		nonnull := *have
		nonnull.Nullable = false
		return CollectionTypeAccepts(&nonnull, want)
	}
	if have.Operator == "|" {
		return CollectionTypeAccepts(have.Left, want) && CollectionTypeAccepts(have.Right, want)
	}
	if want.Operator == "|" {
		return CollectionTypeAccepts(have, want.Left) || CollectionTypeAccepts(have, want.Right)
	}
	if want.Operator == "&" {
		return CollectionTypeAccepts(have, want.Left) && CollectionTypeAccepts(have, want.Right)
	}
	if have.Operator == "&" {
		return CollectionTypeAccepts(have.Left, want) || CollectionTypeAccepts(have.Right, want)
	}
	if have.ListAlias || want.ListAlias {
		return collectionArrayAliasAccepts(have, want)
	}
	if c := collectionRef(have); c != nil {
		have = c
	}
	if c := collectionRef(want); c != nil {
		want = c
	}
	haveName, wantName := typeIdentity(have), typeIdentity(want)
	if !strings.EqualFold(haveName, wantName) {
		for _, parent := range have.Supertypes {
			if CollectionTypeAccepts(parent, want) {
				return true
			}
		}
		return false
	}
	if len(want.Arguments) == 0 {
		return true
	}
	if len(have.Arguments) != len(want.Arguments) {
		return false
	}
	for i, arg := range have.Arguments {
		if collectionRef(have) != nil {
			if !CollectionTypeAccepts(arg, want.Arguments[i]) {
				return false
			}
		} else if !CollectionTypeAccepts(arg, want.Arguments[i]) || !CollectionTypeAccepts(want.Arguments[i], arg) {
			return false
		}
	}
	return true
}

func collectionArrayAliasAccepts(have, want *ast.TypeRef) bool {
	if have.ListAlias {
		elem := *have
		elem.ListAlias, elem.Nullable = false, false
		if want.ListAlias {
			target := *want
			target.ListAlias, target.Nullable = false, false
			return CollectionTypeAccepts(&elem, &target)
		}
		if !strings.EqualFold(want.Name, "list") {
			return false
		}
		return len(want.Arguments) == 0 || (len(want.Arguments) == 1 && CollectionTypeAccepts(&elem, want.Arguments[0]))
	}
	if !strings.EqualFold(have.Name, "list") || len(have.Arguments) != 1 {
		return false
	}
	target := *want
	target.ListAlias, target.Nullable = false, false
	return CollectionTypeAccepts(have.Arguments[0], &target)
}

func collectionTypeAllowsNull(ref *ast.TypeRef) bool {
	if ref == nil {
		return false
	}
	if ref.Nullable || ref.Name == "null" || ref.Name == "any" {
		return true
	}
	if ref.Operator == "|" {
		return collectionTypeAllowsNull(ref.Left) || collectionTypeAllowsNull(ref.Right)
	}
	if ref.Operator == "&" {
		return collectionTypeAllowsNull(ref.Left) && collectionTypeAllowsNull(ref.Right)
	}
	return false
}

type collectionBinding struct {
	state *CollectionTyping
	typ   *ast.TypeRef
}

type collectionCheck struct {
	pending   []collectionBinding
	fallbacks []collectionBinding
	seen      map[*CollectionTyping]*ast.TypeRef
	leaf      func(Value, *ast.TypeRef) bool
	dynamic   bool
}

// CheckCollectionType publishes nested guarantees only after the entire validation succeeds.
func CheckCollectionType(value Value, typ *ast.TypeRef, leaf func(Value, *ast.TypeRef) bool) (bool, bool) {
	return checkCollectionType(value, typ, leaf, true)
}

func MatchCollectionType(value Value, typ *ast.TypeRef, leaf func(Value, *ast.TypeRef) bool) (bool, bool) {
	return checkCollectionType(value, typ, leaf, false)
}

func checkCollectionType(value Value, typ *ast.TypeRef, leaf func(Value, *ast.TypeRef) bool, bind bool) (bool, bool) {
	if !collectionTypeShape(typ) {
		return false, false
	}
	if IsNull(value) {
		return collectionTypeAllowsNull(typ), true
	}
	if declared := CollectionDeclaredType(value); declared != nil {
		if CollectionTypeAccepts(declared, typ) {
			return true, true
		}
		if !collectionTypeIsDynamic(declared) {
			return false, true
		}
	}
	ref := collectionRef(typ)
	if ref == nil {
		ref = typ
	}
	for {
		check := collectionCheck{leaf: leaf}
		if !check.match(value, ref) {
			return false, true
		}
		if !bind {
			return true, true
		}
		if check.dynamic {
			check.retainFallback(value)
			check.pending = nil
		}
		if check.publish() {
			return true, true
		}
	}
}

type CollectionBoundary struct {
	Value Value
	Type  *ast.TypeRef
}

func CheckCollectionBoundaries(boundaries []CollectionBoundary, leaf func(Value, *ast.TypeRef) bool) (bool, int) {
	return checkCollectionBoundaries(boundaries, leaf, false)
}

func checkCollectionBoundaries(boundaries []CollectionBoundary, leaf func(Value, *ast.TypeRef) bool, strict bool) (bool, int) {
	if len(boundaries) == 0 {
		return true, 0
	}
	for {
		combined := collectionCheck{seen: map[*CollectionTyping]*ast.TypeRef{}}
		for i, boundary := range boundaries {
			check := collectionCheck{leaf: leaf}
			if !check.match(boundary.Value, boundary.Type) || (strict && check.dynamic) {
				return false, i
			}
			if check.dynamic {
				check.retainFallback(boundary.Value)
				combined.fallbacks = append(combined.fallbacks, check.fallbacks...)
				continue
			}
			for _, binding := range check.pending {
				if previous, ok := combined.seen[binding.state]; ok {
					if CollectionTypeAccepts(previous, binding.typ) {
						continue
					}
					if !CollectionTypeAccepts(binding.typ, previous) {
						return false, i
					}
					*previous = *binding.typ
					continue
				}
				combined.seen[binding.state] = binding.typ
				combined.pending = append(combined.pending, binding)
			}
		}
		if combined.publish() {
			return true, 0
		}
	}
}

func CollectionDictWriteMatches(collection Dict, key, value Value) (bool, int) {
	declared := CollectionDeclaredType(collection)
	if declared == nil || len(declared.Arguments) != 2 {
		return true, 0
	}
	boundaries := []CollectionBoundary{{key, declared.Arguments[0]}, {value, declared.Arguments[1]}}
	return checkCollectionBoundaries(boundaries, collectionElementMatches, true)
}

func (c *collectionCheck) publish() bool {
	if len(c.pending) == 0 && len(c.fallbacks) == 0 {
		return true
	}
	declarations := make([]*collectionType, len(c.pending))
	for i, binding := range c.pending {
		tags := make([]string, len(binding.typ.Arguments))
		for j, arg := range binding.typ.Arguments {
			tags[j] = collectionArgumentTag(arg)
		}
		declarations[i] = &collectionType{binding.typ, tags}
	}
	fallbacks := make([]*collectionType, len(c.fallbacks))
	for i, binding := range c.fallbacks {
		tags := make([]string, len(binding.typ.Arguments))
		for j, arg := range binding.typ.Arguments {
			tags[j] = arg.Name
			if arg.CanonicalName != "" && arg.Operator == "" && !arg.ListAlias {
				module, _ := SplitCanonicalTypeName(arg.CanonicalName)
				tags[j] = IdentityTag(arg.Name, module)
			}
			if arg.ListAlias {
				tags[j] = "list"
			}
			if arg.Nullable && !strings.HasPrefix(tags[j], "?") {
				tags[j] = "?" + tags[j]
			}
		}
		fallbacks[i] = &collectionType{binding.typ, tags}
	}

	// Serialize publication without holding a global lock during content scans.
	collectionBindingMu.Lock()
	defer collectionBindingMu.Unlock()
	for _, binding := range c.pending {
		if current := binding.state.declared.Load(); current != nil && !CollectionTypeAccepts(current.ref, binding.typ) {
			return false
		}
	}
	for i, binding := range c.pending {
		if binding.state.declared.Load() == nil {
			binding.state.declared.Store(declarations[i])
		}
	}
	for i, binding := range c.fallbacks {
		if binding.state.declared.Load() == nil {
			binding.state.fallback.CompareAndSwap(nil, fallbacks[i])
		}
	}

	return true
}

func (c *collectionCheck) retainFallback(value Value) {
	state := collectionTyping(value)
	if state == nil || state.declared.Load() != nil {
		return
	}
	if ref := c.seen[state]; ref != nil {
		c.fallbacks = append(c.fallbacks, collectionBinding{state, ref})
	}
}

func CollectionExtendMatches(collection Value, values []Value) (bool, int) {
	declared := CollectionDeclaredType(collection)
	if declared == nil || len(declared.Arguments) != 1 {
		return true, 0
	}
	for {
		check := collectionCheck{leaf: collectionElementMatches}
		for i, value := range values {
			if !check.match(value, declared.Arguments[0]) || check.dynamic {
				return false, i
			}
		}
		if check.publish() {
			return true, 0
		}
	}
}

func (c *collectionCheck) match(value Value, typ *ast.TypeRef) bool {
	if matched, handled := collectionPrimitiveMatches(value, typ); handled {
		return matched
	}
	if typ.Operator == "|" {
		previous := collectionCheck{leaf: c.leaf, dynamic: c.dynamic}
		if len(c.pending) > 0 {
			previous.seen = map[*CollectionTyping]*ast.TypeRef{}
			for _, binding := range c.pending {
				ref := *binding.typ
				previous.pending = append(previous.pending, collectionBinding{binding.state, &ref})
				previous.seen[binding.state] = &ref
			}
		}
		if c.match(value, typ.Left) {
			return true
		}
		*c = previous
		return c.match(value, typ.Right)
	}
	if typ.Operator == "&" {
		return c.match(value, typ.Left) && c.match(value, typ.Right)
	}
	if IsNull(value) {
		return typ.Nullable || typ.Name == "any" || typ.Name == "null"
	}
	ref := collectionRef(typ)
	if ref == nil {
		if typ.Name == "iterable" || typ.Name == "generator" {
			c.dynamic = true
			return c.leaf(value, typ)
		}
		if len(typ.Arguments) > 0 {
			if _, nominal := value.(*Instance); !nominal {
				if _, task := value.(*Task); task {
					return c.leaf(value, typ)
				}
				c.dynamic = true
			}
		}
		if typ.CanonicalName != "" || strings.Contains(typ.Name, ".") {
			return collectionElementMatches(value, typ)
		}
		return c.leaf(value, typ)
	}
	if !strings.EqualFold(value.TypeName(), ref.Name) {
		return false
	}
	state := collectionTyping(value)
	if state != nil {
		if declared := state.declared.Load(); declared != nil {
			if CollectionTypeAccepts(declared.ref, ref) {
				return true
			}
			if !collectionTypeIsDynamic(declared.ref) {
				return false
			}
			c.dynamic = true
		}
		if pending, ok := c.seen[state]; ok {
			if CollectionTypeAccepts(pending, ref) {
				return true
			}
			if !CollectionTypeAccepts(ref, pending) {
				return false
			}
			*pending = *ref
			pending.Nullable = false
		} else {
			concrete := *ref
			concrete.Nullable = false
			if c.seen == nil {
				c.seen = map[*CollectionTyping]*ast.TypeRef{}
			}
			c.seen[state] = &concrete
			c.pending = append(c.pending, collectionBinding{state, &concrete})
		}
	}
	switch v := value.(type) {
	case *List:
		for _, item := range v.Elements {
			if !c.match(item, ref.Arguments[0]) {
				return false
			}
		}
	case Set:
		for _, item := range v.Elements {
			if !c.match(item.Value, ref.Arguments[0]) {
				return false
			}
		}
	case Dict:
		matched := true
		v.ForEachEntry(func(_ string, entry DictEntry) bool {
			matched = c.match(entry.Key, ref.Arguments[0]) && c.match(entry.Value, ref.Arguments[1])
			return matched
		})
		return matched
	}
	return true
}

func collectionTypeIsDynamic(ref *ast.TypeRef) bool {
	if ref == nil {
		return false
	}
	if ref.Name == "any" && ref.Operator == "" {
		return true
	}
	for _, arg := range ref.Arguments {
		if collectionTypeIsDynamic(arg) {
			return true
		}
	}
	return collectionTypeIsDynamic(ref.Left) || collectionTypeIsDynamic(ref.Right)
}

func CollectionWriteMatches(collection Value, argument int, value Value) bool {
	declared := CollectionDeclaredType(collection)
	if declared == nil || argument >= len(declared.Arguments) {
		return true
	}
	return collectionWriteElementMatches(value, declared.Arguments[argument])
}

func collectionWriteElementMatches(value Value, typ *ast.TypeRef) bool {
	if matched, handled := collectionPrimitiveMatches(value, typ); handled {
		return matched
	}
	if !collectionTypeShape(typ) {
		return collectionElementMatches(value, typ)
	}
	matched, _ := checkCollectionBoundaries([]CollectionBoundary{{value, typ}}, collectionElementMatches, true)
	return matched
}

func collectionElementMatches(value Value, typ *ast.TypeRef) bool {
	if matched, handled := collectionPrimitiveMatches(value, typ); handled {
		return matched
	}
	if matched, handled := CheckCollectionType(value, typ, collectionElementMatches); handled {
		return matched
	}
	if typ.Operator == "|" {
		return collectionElementMatches(value, typ.Left) || collectionElementMatches(value, typ.Right)
	}
	if typ.Operator == "&" {
		return collectionElementMatches(value, typ.Left) && collectionElementMatches(value, typ.Right)
	}
	if typ.Name == "any" {
		return true
	}
	if IsNull(value) {
		return typ.Nullable || typ.Name == "null"
	}
	name := typeIdentity(typ)
	if name == "func" || name == "callable" || name == "function" {
		return IsCallableValue(value)
	}
	if instance, ok := value.(*Instance); ok {
		if typ.CanonicalName != "" || strings.Contains(name, ".") {
			module, bare := SplitCanonicalTypeName(name)
			if !ValueIsNominalType(instance, module, bare) {
				return false
			}
		} else if !ValueSatisfiesHierarchyLeaf(value, name) {
			return false
		}
		if len(typ.Arguments) > 0 {
			for class := instance.Class; class != nil; class = class.Parent {
				if !strings.EqualFold(class.Name, bareTypeName(name)) {
					continue
				}
				if typ.CanonicalName != "" || strings.Contains(name, ".") {
					module, _ := SplitCanonicalTypeName(name)
					if !moduleIdentityEqual(class.DeclaringModule(), module) {
						continue
					}
				}
				if len(class.TypeParameters) != len(typ.Arguments) {
					return false
				}
				bindings := DeclaringClassBindings(instance, class)
				for i, param := range class.TypeParameters {
					bound, err := parser.ParseTypeRef(bindings[param])
					if err != nil || !CollectionTypeAccepts(bound, typ.Arguments[i]) || !CollectionTypeAccepts(typ.Arguments[i], bound) {
						return false
					}
				}
				return true
			}
			return false
		}
		return true
	}
	if enum, ok := value.(EnumVariant); ok && enum.Enum != nil {
		if typ.CanonicalName != "" || strings.Contains(name, ".") {
			module, bare := SplitCanonicalTypeName(name)
			return EnumMatchesExact(enum.Enum, module, bare)
		}
		return strings.EqualFold(enum.Enum.Name, name) || EnumImplements(enum.Enum, name)
	}
	if strings.EqualFold(value.TypeName(), name) {
		return true
	}

	if err, ok := value.(Error); ok {
		return ErrorValueIsA(err, bareTypeName(name), strings.EqualFold)
	}
	return ValueSatisfiesHierarchyLeaf(value, name)
}

func collectionPrimitiveMatches(value Value, typ *ast.TypeRef) (bool, bool) {
	if typ.Operator != "" || typ.ListAlias || len(typ.Arguments) > 0 {
		return false, false
	}
	if typ.Name == "any" {
		return true, true
	}
	if IsNull(value) {
		return typ.Nullable || typ.Name == "null", true
	}
	switch typ.Name {
	case "int":
		switch value.(type) {
		case Int, SmallInt:
			return true, true
		}
		return false, true
	case "string":
		_, ok := value.(String)
		return ok, true
	case "bool":
		_, ok := value.(Bool)
		return ok, true
	case "float":
		_, ok := value.(Float)
		return ok, true
	case "decimal":
		_, ok := value.(Decimal)
		return ok, true
	case "bytes":
		_, ok := value.(Bytes)
		return ok, true
	case "func", "callable", "function":
		return IsCallableValue(value), true
	case "null":
		return false, true
	}
	return false, false
}

func CopyCollectionType(source, target Value) {
	if state := collectionTyping(target); state != nil {
		if original := collectionTyping(source); original != nil {
			state.fallback.Store(original.fallback.Load())
			state.declared.Store(original.declared.Load())
		}
	}
}

func PreserveCollectionType(source, target Value) Value {
	CopyCollectionType(source, target)
	return target
}

func CollectionElementTypes(value Value) []string {
	if state := collectionTyping(value); state != nil {
		if declared := state.declared.Load(); declared != nil {
			return declared.tags
		}
		if fallback := state.fallback.Load(); fallback != nil {
			return fallback.tags
		}
	}
	switch v := value.(type) {
	case *List:
		return v.ElementTypes
	case Dict:
		return v.ElementTypes
	case Set:
		return v.ElementTypes
	}
	return nil
}

func HasCollectionType(value Value) bool {
	return CollectionDeclaredType(value) != nil
}

func NominalSupertypes(value Value, args []*ast.TypeRef) []*ast.TypeRef {
	var names []*ast.TypeRef
	add := func(name string) {
		if ref, err := parser.ParseTypeRef(name); err == nil {
			names = append(names, ref)
		}
	}
	seen := map[*Interface]bool{}
	var iface func(*Interface)
	iface = func(v *Interface) {
		if v == nil || seen[v] {
			return
		}
		seen[v] = true
		add(QualifiedTypeName(v.Module, bareTypeName(v.Name)))
		for _, parent := range v.Parents {
			iface(parent)
		}
	}
	switch v := value.(type) {
	case *Class:
		instance := &Instance{Class: v, TypeBindings: map[string]string{}}
		for i, param := range v.TypeParameters {
			if i < len(args) {
				instance.TypeBindings[param] = typealias.Qualified(args[i]).String()
			}
		}
		for class := v; class != nil; class = class.Parent {
			name := QualifiedTypeName(class.DeclaringModule(), class.Name)
			add(name)
			if len(class.TypeParameters) > 0 {
				bindings := DeclaringClassBindings(instance, class)
				parts := make([]string, 0, len(class.TypeParameters))
				for _, param := range class.TypeParameters {
					if bound := bindings[param]; bound != "" {
						parts = append(parts, bound)
					}
				}
				if len(parts) == len(class.TypeParameters) {
					add(name + "<" + strings.Join(parts, ",") + ">")
				}
			}
			for _, implemented := range class.Implements {
				iface(implemented)
			}
		}
	case *Interface:
		iface(v)
	case *EnumDef:
		for _, implemented := range v.Interfaces() {
			iface(implemented)
		}
		if v.InterfacesBound() {
			break
		}
		for _, implemented := range v.Implements {
			if !strings.Contains(implemented, ".") {
				implemented = QualifiedTypeName(v.Module, implemented)
			}
			add(implemented)
		}
	}
	return names
}

func collectionArgumentTag(arg *ast.TypeRef) string {
	if arg.CanonicalName != "" && arg.Operator == "" && !arg.ListAlias && len(arg.Arguments) == 0 {
		module, _ := SplitCanonicalTypeName(arg.CanonicalName)
		tag := IdentityTag(arg.Name, module)
		if arg.Nullable {
			tag = "?" + tag
		}
		return tag
	}
	return typealias.Qualified(arg).String()
}
