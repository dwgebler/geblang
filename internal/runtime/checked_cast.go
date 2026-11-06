package runtime

import (
	"fmt"
	"geblang/internal/ast"
	"strconv"
	"strings"
)

// MatchesCheckedCast keeps strict nominal casts separate from legacy parameter matching.
func MatchesCheckedCast(value Value, typ *ast.TypeRef, match func(Value, *ast.TypeRef) bool) bool {
	if typ == nil {
		return false
	}
	if !castHasQualifiedType(typ) {
		return match(value, typ)
	}
	if typ.Operator == "|" {
		return MatchesCheckedCast(value, typ.Left, match) || MatchesCheckedCast(value, typ.Right, match)
	}
	if typ.Operator == "&" {
		return MatchesCheckedCast(value, typ.Left, match) && MatchesCheckedCast(value, typ.Right, match)
	}
	if _, null := value.(Null); null {
		return match(value, typ)
	}
	name := strings.ToLower(typ.Name)
	switch v := value.(type) {
	case *List:
		if typ.ListAlias && len(typ.Arguments) == 0 {
			element := *typ
			element.Nullable, element.ListAlias = false, false
			for _, item := range v.Elements {
				if !MatchesCheckedCast(item, &element, match) {
					return false
				}
			}
			return true
		}
		if name == "list" && len(typ.Arguments) == 1 {
			for _, item := range v.Elements {
				if !MatchesCheckedCast(item, typ.Arguments[0], match) {
					return false
				}
			}
			return true
		}
	case Set:
		if name == "set" && len(typ.Arguments) == 1 {
			for _, item := range v.Elements {
				if !MatchesCheckedCast(item.Value, typ.Arguments[0], match) {
					return false
				}
			}
			return true
		}
	case Dict:
		if name == "dict" && len(typ.Arguments) == 2 {
			valid := true
			v.ForEachEntry(func(_ string, item DictEntry) bool {
				valid = MatchesCheckedCast(item.Key, typ.Arguments[0], match) && MatchesCheckedCast(item.Value, typ.Arguments[1], match)
				return valid
			})
			return valid
		}
	case *Instance:
		if dot := strings.LastIndexByte(typ.Name, '.'); dot > 0 && !InstanceClassMatchesExact(v.Class, typ.Name[:dot], typ.Name[dot+1:]) {
			return false
		}
	}
	return match(value, typ)
}

func castHasQualifiedType(typ *ast.TypeRef) bool {
	if typ == nil {
		return false
	}
	if strings.Contains(typ.Name, ".") || castHasQualifiedType(typ.Left) || castHasQualifiedType(typ.Right) {
		return true
	}
	for _, arg := range typ.Arguments {
		if castHasQualifiedType(arg) {
			return true
		}
	}
	return false
}

// CastMismatchDetail explains why a checked collection cast failed, naming
// the first offending part by its path: `: key 0 is int, not string`,
// `: ["stats"][2] is string, not int` or `: member 1.5 is decimal, not int`.
// It returns "" when no element is to blame (the container itself has the
// wrong type). Call it only after MatchesCheckedCast reported false: it walks
// the value again and is never on a successful cast's path.
func CastMismatchDetail(value Value, typ *ast.TypeRef, match func(Value, *ast.TypeRef) bool) string {
	m, ok := findCastMismatch(value, typ, match, "")
	if !ok {
		return ""
	}
	subject := m.path
	if m.role != "" {
		subject = strings.TrimSpace(m.path + " " + m.role + " " + castShowValue(m.actual))
	}
	return fmt.Sprintf(": %s is %s, not %s", subject, m.actual.TypeName(), m.want)
}

type castMismatch struct {
	path   string // where in the value: `["stats"][2]`; "" for the top level
	role   string // "key" or "member" when the culprit is a dict key or set member
	actual Value
	want   string
}

// findCastMismatch finds the first element of `value` that fails its element
// type in `typ`, descending into nested collections.
func findCastMismatch(value Value, typ *ast.TypeRef, match func(Value, *ast.TypeRef) bool, path string) (castMismatch, bool) {
	if typ == nil || typ.Operator != "" {
		return castMismatch{}, false
	}
	// A failing element is searched for a deeper culprit; failing that (it is
	// a scalar, or the wrong kind of container), the element itself is it.
	element := func(item Value, elementType *ast.TypeRef, at string) (castMismatch, bool) {
		if MatchesCheckedCast(item, elementType, match) {
			return castMismatch{}, false
		}
		if m, ok := findCastMismatch(item, elementType, match, at); ok {
			return m, true
		}
		return castMismatch{path: at, actual: item, want: elementType.String()}, true
	}
	name := strings.ToLower(typ.Name)
	switch v := value.(type) {
	case *List:
		var elementType *ast.TypeRef
		if typ.ListAlias && len(typ.Arguments) == 0 {
			e := *typ
			e.Nullable, e.ListAlias = false, false
			elementType = &e
		} else if name == "list" && len(typ.Arguments) == 1 {
			elementType = typ.Arguments[0]
		}
		if elementType == nil {
			return castMismatch{}, false
		}
		for i, item := range v.Elements {
			if m, ok := element(item, elementType, path+"["+strconv.Itoa(i)+"]"); ok {
				return m, true
			}
		}
	case Set:
		if name == "set" && len(typ.Arguments) == 1 {
			// Set order differs between the runtimes, so name the failing
			// member that sorts first by its rendering.
			var culprit Value
			for _, item := range v.Elements {
				if !MatchesCheckedCast(item.Value, typ.Arguments[0], match) {
					if culprit == nil || castShowValue(item.Value) < castShowValue(culprit) {
						culprit = item.Value
					}
				}
			}
			if culprit != nil {
				return castMismatch{path: path, role: "member", actual: culprit, want: typ.Arguments[0].String()}, true
			}
		}
	case Dict:
		if name == "dict" && len(typ.Arguments) == 2 {
			var found castMismatch
			var ok bool
			v.ForEachEntry(func(_ string, entry DictEntry) bool {
				if !MatchesCheckedCast(entry.Key, typ.Arguments[0], match) {
					found, ok = castMismatch{path: path, role: "key", actual: entry.Key, want: typ.Arguments[0].String()}, true
					return false
				}
				found, ok = element(entry.Value, typ.Arguments[1], path+"["+castShowValue(entry.Key)+"]")
				return !ok
			})
			return found, ok
		}
	}
	return castMismatch{}, false
}

// castShowValue renders a key or member as source would write it: strings
// quoted, everything else as inspected.
func castShowValue(v Value) string {
	if s, ok := v.(String); ok {
		return strconv.Quote(s.Value)
	}
	return v.Inspect()
}
