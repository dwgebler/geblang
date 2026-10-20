package check

import (
	"geblang/internal/ast"
	"geblang/internal/semantic"
)

// moduleFunctionResolver returns the analyzer hook that supplies the signatures of functions and constructors declared in the file's imported source modules: by module alias for `alias.name(...)`, or by local name for `from m import name`. Native modules are left to the native catalog checks.
func moduleFunctionResolver(program *ast.Program, opts Options, cache *ModuleCache) func(string, string) ([]semantic.MethodSignature, bool) {
	aliases := collectImportAliases(program)
	type fromName struct{ canonical, member string }
	froms := map[string]fromName{}
	for _, stmt := range program.Statements {
		from, ok := stmt.(*ast.FromImportStatement)
		if !ok {
			continue
		}
		canonical := joinPath(from.Path)
		for _, item := range from.Names {
			if item.Name != nil && item.Local() != "" {
				froms[item.Local()] = fromName{canonical: canonical, member: item.Name.Value}
			}
		}
	}
	return func(qualifier, name string) ([]semantic.MethodSignature, bool) {
		canonical, member := "", name
		if qualifier != "" {
			alias, ok := aliases[qualifier]
			if !ok {
				return nil, false
			}
			canonical = alias.canonical
		} else {
			f, ok := froms[name]
			if !ok {
				return nil, false
			}
			canonical, member = f.canonical, f.member
		}
		if canonical == "" || IsNativeImport(canonical) || opts.Resolver == nil {
			return nil, false
		}
		path, err := opts.Resolver.Resolve(canonical)
		if err != nil {
			return nil, false
		}
		prog, _, err := cache.load(path)
		if err != nil {
			return nil, false
		}
		return semantic.ModuleCallSignatures(prog, member)
	}
}
