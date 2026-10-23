package check

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/semantic"
)

// classGraph holds the class/interface declarations visible to a file
// (its own plus those of its direct, non-native imports), used to
// resolve the full member set of a class across modules for the
// unknown-method check.
type classGraph struct {
	classes    map[string]semantic.ClassDecl
	interfaces map[string]semantic.InterfaceDecl
	// ambiguous names are declared in more than one source; resolution
	// bails on them to avoid picking the wrong declaration.
	ambiguous map[string]bool
}

// buildClassGraph extracts the class model of the file under check plus
// each directly-imported source module. Native modules are skipped (no
// user classes to resolve here).
func buildClassGraph(program *ast.Program, opts Options, cache *ModuleCache) *classGraph {
	g := &classGraph{
		classes:    map[string]semantic.ClassDecl{},
		interfaces: map[string]semantic.InterfaceDecl{},
		ambiguous:  map[string]bool{},
	}
	g.merge(semantic.ExtractModel(program))
	if opts.Resolver == nil {
		return g
	}
	for _, canonical := range importedModulePaths(program) {
		if IsNativeImport(canonical) {
			continue
		}
		path, err := opts.Resolver.Resolve(canonical)
		if err != nil {
			continue
		}
		prog, _, err := cache.load(path)
		if err != nil {
			continue
		}
		g.merge(semantic.ExtractModel(prog))
	}
	return g
}

// importedModulePaths returns the canonical paths of every module the
// program imports, via either `import m` or `from m import ...`.
func importedModulePaths(program *ast.Program) []string {
	var paths []string
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *ast.ImportStatement:
			if p := joinPath(s.Path); p != "" {
				paths = append(paths, p)
			}
		case *ast.FromImportStatement:
			if p := joinPath(s.Path); p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

func (g *classGraph) merge(model semantic.ModuleModel) {
	for name, decl := range model.Classes {
		if _, seen := g.classes[name]; seen {
			g.ambiguous[name] = true
			continue
		}
		g.classes[name] = decl
	}
	for name, decl := range model.Interfaces {
		if _, seen := g.interfaces[name]; seen {
			g.ambiguous[name] = true
			continue
		}
		g.interfaces[name] = decl
	}
}

// simpleName strips a module qualifier so "base.Base" -> "Base"; graph keys are simple names.
func simpleName(name string) string {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[dot+1:]
	}
	return name
}

// surface returns the full member set (methods + fields, including
// inherited and interface-provided members) for a class, plus whether
// the class resolved cleanly. ok is false on any uncertainty (unknown
// name, ambiguity, a `__call` or decorated class anywhere in the chain,
// an unresolvable parent or interface, or a cycle) so callers stay
// silent rather than risk a false positive.
func (g *classGraph) surface(className string) (map[string]bool, bool) {
	members := map[string]bool{}
	seen := map[string]bool{}
	for name := className; name != ""; {
		if g.ambiguous[name] || seen[name] {
			return nil, false
		}
		seen[name] = true
		decl, ok := g.classes[name]
		if !ok || decl.HasCall || decl.Decorated {
			return nil, false
		}
		for m := range decl.Methods {
			members[m] = true
		}
		// Fields are members too, marked false: calling one with method
		// syntax (obj.field(...)) fails at runtime on both backends.
		for f := range decl.Fields {
			if _, ok := members[f]; !ok {
				members[f] = false
			}
		}
		for _, iface := range decl.Implements {
			if !g.addInterfaceMembers(simpleName(iface), members, map[string]bool{}) {
				return nil, false
			}
		}
		name = simpleName(decl.Parent)
	}
	return members, true
}

// methodSignatures returns the overload signatures for a method, found on the
// class or the nearest ancestor that declares it, plus that declaring class's
// generic parameter names (for the analyzer's type-parameter-aware checks).
// Returns ok=false on any uncertainty (ambiguous/cyclic/__call/decorated class,
// or the method resolves only via an interface) so callers stay silent.
func (g *classGraph) methodSignatures(className, methodLower string) ([]semantic.MethodSignature, []string, bool) {
	seen := map[string]bool{}
	for name := className; name != ""; {
		if g.ambiguous[name] || seen[name] {
			return nil, nil, false
		}
		seen[name] = true
		decl, ok := g.classes[name]
		if !ok || decl.HasCall || decl.Decorated {
			return nil, nil, false
		}
		if sigs, found := decl.MethodSigs[methodLower]; found && len(sigs) > 0 {
			return sigs, decl.TypeParams, true
		}
		name = simpleName(decl.Parent)
	}
	return nil, nil, false
}

// typeRefNamesAny reports whether ref mentions any of names (a class's type parameters).
func typeRefNamesAny(ref *ast.TypeRef, names []string) bool {
	if ref == nil || len(names) == 0 {
		return false
	}
	for _, name := range names {
		if strings.EqualFold(ref.Name, name) {
			return true
		}
	}
	for _, arg := range ref.Arguments {
		if typeRefNamesAny(arg, names) {
			return true
		}
	}
	return typeRefNamesAny(ref.Left, names) || typeRefNamesAny(ref.Right, names)
}

// fieldType returns the declared type of a field, walking ancestors; ok=false on any uncertainty.
func (g *classGraph) fieldType(className, fieldLower string) (*ast.TypeRef, bool) {
	seen := map[string]bool{}
	for name := className; name != ""; {
		if g.ambiguous[name] || seen[name] {
			return nil, false
		}
		seen[name] = true
		decl, ok := g.classes[name]
		if !ok || decl.HasCall || decl.Decorated {
			return nil, false
		}
		if ref, found := decl.FieldTypes[fieldLower]; found {
			if ref == nil || typeRefNamesAny(ref, decl.TypeParams) {
				return nil, false
			}
			return ref, true
		}
		name = simpleName(decl.Parent)
	}
	return nil, false
}

func (g *classGraph) addInterfaceMembers(name string, members, seen map[string]bool) bool {
	if g.ambiguous[name] {
		return false
	}
	if seen[name] {
		return true
	}
	seen[name] = true
	decl, ok := g.interfaces[name]
	if !ok {
		return false // unresolvable interface may carry default methods
	}
	for m := range decl.Methods {
		members[m] = true
	}
	for f := range decl.Fields {
		members[f] = true
	}
	for _, parent := range decl.Parents {
		if !g.addInterfaceMembers(simpleName(parent), members, seen) {
			return false
		}
	}
	return true
}
