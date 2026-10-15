package check

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/semantic"
)

// checkInstanceofTypes flags a bare type name in an `instanceof` target
// that resolves to nothing: not ambient, not declared in the file, not a
// generic type param, and not a trailing name of any resolved import's
// export set (instanceof matches cross-module types by trailing name).
// Bails silently on any uncertainty - an unresolvable import makes the
// import surface unknowable.
func checkInstanceofTypes(file string, program *ast.Program, opts Options) []Diagnostic {
	cache := opts.ModuleCache
	if cache == nil {
		cache = NewModuleCache()
	}
	importedExports := map[string]struct{}{}
	collector := &crossTypeCollector{aliases: collectImportAliases(program), opts: opts, cache: cache}
	for _, alias := range collector.aliases {
		exports, ok := resolveExportSet(alias.canonical, alias.native, opts, cache)
		if !ok {
			return nil
		}
		for name := range exports {
			importedExports[name] = struct{}{}
		}
		for name := range collector.typeAliasNames(alias) {
			importedExports[name] = struct{}{}
		}
	}
	declared := collectLocalTypeNames(program)
	// Generic names are collected file-wide, an over-approximation of scope.
	generics := map[string]bool{}
	var refs []*ast.TypeRef
	(&astVisitor{
		typeParams: func(params []*ast.TypeParam) {
			for _, param := range params {
				if param != nil && param.Name != nil {
					generics[param.Name.Value] = true
				}
			}
		},
		expr: func(expr ast.Expression) {
			if infix, ok := expr.(*ast.InfixExpression); ok && infix.RightType != nil {
				refs = append(refs, infix.RightType)
			}
		},
	}).program(program)
	var diags []Diagnostic
	var flagLeaf func(ref *ast.TypeRef)
	flagLeaf = func(ref *ast.TypeRef) {
		if ref == nil {
			return
		}
		if ref.Operator != "" {
			flagLeaf(ref.Left)
			flagLeaf(ref.Right)
			return
		}
		for _, arg := range ref.Arguments {
			flagLeaf(arg)
		}
		name := strings.TrimPrefix(ref.Name, "?")
		if name == "" || strings.Contains(name, ".") {
			return
		}
		if semantic.IsAmbientTypeName(name) || declared[name] || generics[name] {
			return
		}
		if _, ok := importedExports[name]; ok {
			return
		}
		diags = append(diags, Diagnostic{
			Line: ref.Token.Line, Column: ref.Token.Column,
			Severity: SeverityError, Rule: "type",
			Message: fmt.Sprintf("unknown type %q in instanceof", name),
		})
	}
	for _, ref := range refs {
		flagLeaf(ref)
	}
	return diagsWithFile(diags, file)
}

// collectLocalTypeNames gathers the file's declared type names (classes,
// interfaces, enums, aliases) plus from-import bound names.
func collectLocalTypeNames(program *ast.Program) map[string]bool {
	names := map[string]bool{}
	var add func(stmt ast.Statement)
	add = func(stmt ast.Statement) {
		switch s := stmt.(type) {
		case *ast.ExportStatement:
			add(s.Statement)
		case *ast.ClassStatement:
			if s.Name != nil {
				names[s.Name.Value] = true
			}
		case *ast.InterfaceStatement:
			if s.Name != nil {
				names[s.Name.Value] = true
			}
		case *ast.EnumStatement:
			if s.Name != nil {
				names[s.Name.Value] = true
			}
		case *ast.TypeAliasStatement:
			if s.Name != nil {
				names[s.Name.Value] = true
			}
		case *ast.FromImportStatement:
			for _, n := range s.Names {
				if n.Alias != nil {
					names[n.Alias.Value] = true
				} else if n.Name != nil {
					names[n.Name.Value] = true
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		add(stmt)
	}
	return names
}
