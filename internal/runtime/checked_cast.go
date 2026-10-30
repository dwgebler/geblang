package runtime

import (
	"geblang/internal/ast"
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
