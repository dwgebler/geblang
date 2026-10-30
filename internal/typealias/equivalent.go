package typealias

import (
	"sort"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/parser"
)

// EquivalentNames compares complete reified shapes, preserving nominal modules.
func EquivalentNames(left, right string) bool {
	if strings.EqualFold(left, right) {
		return true
	}
	if !strings.ContainsAny(left+right, "<|&?[") {
		return false
	}
	l, le := parser.ParseTypeRef(left)
	r, re := parser.ParseTypeRef(right)
	return le == nil && re == nil && canonicalTypeShape(l) == canonicalTypeShape(r)
}

func leafTypeName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func canonicalTypeShape(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	prefix := ""
	if ref.Nullable {
		prefix = "?"
	}
	if ref.ListAlias {
		element := *ref
		element.ListAlias = false
		element.Nullable = false
		return prefix + "list<" + canonicalTypeShape(&element) + ">"
	}
	if ref.Operator != "" {
		parts := []string{}
		var collect func(*ast.TypeRef)
		collect = func(node *ast.TypeRef) {
			if node.Operator == ref.Operator && !node.Nullable {
				collect(node.Left)
				collect(node.Right)
				return
			}
			parts = append(parts, canonicalTypeShape(node))
		}
		collect(ref.Left)
		collect(ref.Right)
		sort.Strings(parts)
		parts = compactShapes(parts)
		if len(parts) == 1 {
			return prefix + parts[0]
		}
		return prefix + "(" + strings.Join(parts, ref.Operator) + ")"
	}
	result := prefix + strings.ToLower(ref.Name)
	if len(ref.Arguments) > 0 {
		args := make([]string, len(ref.Arguments))
		for i, arg := range ref.Arguments {
			args[i] = canonicalTypeShape(arg)
		}
		result += "<" + strings.Join(args, ",") + ">"
	}
	return result
}

func compactShapes(parts []string) []string {
	out := parts[:0]
	for _, part := range parts {
		if len(out) == 0 || out[len(out)-1] != part {
			out = append(out, part)
		}
	}
	return out
}
