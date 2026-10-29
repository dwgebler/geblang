package semantic

import (
	"strings"

	"geblang/internal/ast"
)

// SourceClassRelations follows lexical imports without evaluating module code.
func SourceClassRelations(program *ast.Program, canonical string, load func(string) (*ast.Program, bool)) func(string, string) (bool, bool) {
	programs := map[string]*ast.Program{canonical: program}
	models := map[string]ModuleModel{canonical: ExtractModel(program)}
	get := func(module string) *ast.Program {
		if p, present := programs[module]; present {
			return p
		}
		p, ok := load(module)
		if !ok {
			p = nil
		}
		programs[module] = p
		if p != nil {
			models[module] = ExtractModel(p)
		}
		return p
	}
	type classRef struct {
		module, name string
		decl         ClassDecl
	}
	var resolve func(string, string, map[string]bool) (classRef, bool)
	resolve = func(module, name string, seen map[string]bool) (classRef, bool) {
		identity := module + ":" + name
		if seen[identity] {
			return classRef{}, false
		}
		seen[identity] = true
		p := get(module)
		if p == nil {
			return classRef{}, false
		}
		if decl, ok := models[module].Classes[name]; ok {
			return classRef{module, name, decl}, true
		}
		for _, stmt := range p.Statements {
			switch stmt := stmt.(type) {
			case *ast.ImportStatement:
				if suffix, ok := strings.CutPrefix(name, stmt.ModuleName()+"."); ok {
					return resolve(strings.Join(stmt.Path, "."), suffix, seen)
				}
			case *ast.FromImportStatement:
				for _, item := range stmt.Names {
					if item.Local() == name && item.Name != nil {
						return resolve(strings.Join(stmt.Path, "."), item.Name.Value, seen)
					}
				}
			}
		}
		return classRef{}, false
	}
	return func(target, actual string) (bool, bool) {
		wanted, ok := resolve(canonical, target, map[string]bool{})
		if !ok {
			return false, false
		}
		current, ok := resolve(canonical, actual, map[string]bool{})
		if !ok {
			return false, false
		}
		seen := map[string]bool{}
		for {
			id := current.module + ":" + current.name
			if seen[id] {
				return false, false
			}
			seen[id] = true
			if current.module == wanted.module && current.name == wanted.name {
				return true, true
			}
			if current.decl.Parent == "" {
				return false, true
			}
			current, ok = resolve(current.module, current.decl.Parent, map[string]bool{})
			if !ok {
				return false, false
			}
		}
	}
}
