// Package typealias expands exported cross-module type aliases identically for both backends.
package typealias

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/parser"
)

const maxDepth = 16

// Lookup returns the target type source of an alias reference, or false when name is not an alias.
type Lookup func(name string) (string, bool)

// Expand substitutes every alias reference in ref, recursively, leaving other names untouched.
func Expand(ref *ast.TypeRef, lookup Lookup) *ast.TypeRef {
	return expand(ref, lookup, 0)
}

func expand(ref *ast.TypeRef, lookup Lookup, depth int) *ast.TypeRef {
	if ref == nil || depth > maxDepth {
		return ref
	}
	if ref.Operator != "" {
		left := expand(ref.Left, lookup, depth)
		right := expand(ref.Right, lookup, depth)
		if left == ref.Left && right == ref.Right {
			return ref
		}
		out := *ref
		out.Left, out.Right = left, right
		return &out
	}
	if target, ok := lookup(ref.Name); ok {
		if parsed, err := parser.ParseTypeRef(target); err == nil {
			resolved := *expand(parsed, lookup, depth+1)
			resolved.Token = ref.Token
			resolved.Nullable = resolved.Nullable || ref.Nullable
			resolved.ListAlias = resolved.ListAlias || ref.ListAlias
			if len(resolved.Arguments) == 0 && len(ref.Arguments) > 0 && resolved.Operator == "" {
				resolved.Arguments = make([]*ast.TypeRef, len(ref.Arguments))
				for i, arg := range ref.Arguments {
					resolved.Arguments[i] = expand(arg, lookup, depth)
				}
			}
			return &resolved
		}
	}
	var args []*ast.TypeRef
	for i, arg := range ref.Arguments {
		expanded := expand(arg, lookup, depth)
		if expanded != arg && args == nil {
			args = append([]*ast.TypeRef(nil), ref.Arguments...)
		}
		if args != nil {
			args[i] = expanded
		}
	}
	if args == nil {
		return ref
	}
	out := *ref
	out.Arguments = args
	return &out
}

// ExpandString expands aliases inside a type source string; unparsable input is returned unchanged.
func ExpandString(source string, lookup Lookup) string {
	if !strings.ContainsRune(source, '.') {
		if _, ok := lookup(source); !ok {
			return source
		}
	}
	ref, err := parser.ParseTypeRef(source)
	if err != nil {
		return source
	}
	expanded := Expand(ref, lookup)
	if expanded == ref {
		return source
	}
	return expanded.String()
}

type programScope struct {
	canonical   string
	imports     map[string]string
	fromImports map[string]string
	declared    map[string]bool
	local       map[string]*ast.TypeRef
}

func newProgramScope(program *ast.Program, canonical string) *programScope {
	scope := &programScope{canonical: canonical, imports: map[string]string{}, fromImports: map[string]string{}, declared: map[string]bool{}, local: map[string]*ast.TypeRef{}}
	for _, stmt := range program.Statements {
		if exported, ok := stmt.(*ast.ExportStatement); ok {
			stmt = exported.Statement
		}
		switch s := stmt.(type) {
		case *ast.ImportStatement:
			if name := s.ModuleName(); name != "" {
				scope.imports[name] = strings.Join(s.Path, ".")
			}
		case *ast.FromImportStatement:
			for _, item := range s.Names {
				if item.Name != nil {
					scope.fromImports[item.Local()] = strings.Join(s.Path, ".") + "." + item.Name.Value
				}
			}
		case *ast.ClassStatement:
			scope.declared[s.Name.Value] = true
		case *ast.InterfaceStatement:
			scope.declared[s.Name.Value] = true
		case *ast.EnumStatement:
			scope.declared[s.Name.Value] = true
		case *ast.TypeAliasStatement:
			if s.Name != nil && s.Type != nil {
				scope.local[strings.ToLower(s.Name.Value)] = s.Type
			}
		}
	}
	return scope
}

func (s *programScope) canonicalize(ref *ast.TypeRef, depth int) *ast.TypeRef {
	if ref == nil || depth > maxDepth {
		return ref
	}
	out := *ref
	if ref.Operator != "" {
		out.Left = s.canonicalize(ref.Left, depth)
		out.Right = s.canonicalize(ref.Right, depth)
		return &out
	}
	if dot := strings.LastIndexByte(ref.Name, '.'); dot > 0 {
		if target, ok := s.imports[ref.Name[:dot]]; ok {
			out.Name = target + "." + ref.Name[dot+1:]
		}
	} else if local, ok := s.local[strings.ToLower(ref.Name)]; ok {
		resolved := *s.canonicalize(local, depth+1)
		resolved.Token = ref.Token
		resolved.Nullable = resolved.Nullable || ref.Nullable
		resolved.ListAlias = resolved.ListAlias || ref.ListAlias
		if len(resolved.Arguments) == 0 && len(ref.Arguments) > 0 && resolved.Operator == "" {
			resolved.Arguments = make([]*ast.TypeRef, len(ref.Arguments))
			for i, arg := range ref.Arguments {
				resolved.Arguments[i] = s.canonicalize(arg, depth)
			}
		}
		return &resolved
	} else if s.declared[ref.Name] {
		out.Name = s.canonical + "." + ref.Name
	} else if target, ok := s.fromImports[ref.Name]; ok {
		out.Name = target
	}
	if len(ref.Arguments) > 0 {
		out.Arguments = make([]*ast.TypeRef, len(ref.Arguments))
		for i, arg := range ref.Arguments {
			out.Arguments[i] = s.canonicalize(arg, depth)
		}
	}
	return &out
}

// ExportedFromProgram returns a module's exported aliases with every module qualifier canonical.
func ExportedFromProgram(program *ast.Program, canonical string) map[string]string {
	var scope *programScope
	var aliases map[string]string
	for _, stmt := range program.Statements {
		exported, ok := stmt.(*ast.ExportStatement)
		if !ok {
			continue
		}
		alias, ok := exported.Statement.(*ast.TypeAliasStatement)
		if !ok || alias.Name == nil || alias.Type == nil {
			continue
		}
		if scope == nil {
			scope = newProgramScope(program, canonical)
			aliases = map[string]string{}
		}
		aliases[alias.Name.Value] = scope.canonicalize(alias.Type, 0).String()
	}
	return aliases
}

// SourceLookup reads exported aliases from module sources for static analysis.
type SourceLookup struct {
	resolve func(canonical string) (*ast.Program, bool)
	cache   map[string]map[string]string
}

// NewSourceLookup builds a lookup over modules parsed by resolve.
func NewSourceLookup(resolve func(canonical string) (*ast.Program, bool)) *SourceLookup {
	return &SourceLookup{resolve: resolve, cache: map[string]map[string]string{}}
}

// Target returns the canonical target of canonical.name when it is an exported alias.
func (l *SourceLookup) Target(qualified string) (string, bool) {
	if l == nil {
		return "", false
	}
	dot := strings.LastIndexByte(qualified, '.')
	if dot <= 0 {
		return "", false
	}
	module, name := qualified[:dot], qualified[dot+1:]
	aliases, cached := l.cache[module]
	if !cached {
		if program, ok := l.resolve(module); ok && program != nil {
			aliases = ExportedFromProgram(program, module)
		}
		l.cache[module] = aliases
	}
	target, ok := aliases[name]
	return target, ok
}

// ScopedLookup resolves alias references written in program, using its imports to canonicalize prefixes.
func ScopedLookup(program *ast.Program, canonical string, targets Lookup) Lookup {
	scope := newProgramScope(program, canonical)
	return func(name string) (string, bool) {
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			prefix := name[:dot]
			if target, ok := scope.imports[prefix]; ok {
				prefix = target
			}
			return targets(prefix + "." + name[dot+1:])
		}
		if target, ok := scope.fromImports[name]; ok {
			return targets(target)
		}
		return "", false
	}
}
