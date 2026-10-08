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
	generics := map[string]bool{}
	var refs []*ast.TypeRef
	walkInstanceofRefs(program.Statements, generics, &refs)
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

// walkInstanceofRefs gathers every instanceof target TypeRef and every
// generic type-param name (file-wide, an over-approximation of scope).
func walkInstanceofRefs(stmts []ast.Statement, generics map[string]bool, refs *[]*ast.TypeRef) {
	for _, stmt := range stmts {
		walkInstanceofStmt(stmt, generics, refs)
	}
}

func addGenericNames(params []*ast.TypeParam, generics map[string]bool) {
	for _, g := range params {
		if g != nil && g.Name != nil {
			generics[g.Name.Value] = true
		}
	}
}

func walkInstanceofStmt(stmt ast.Statement, generics map[string]bool, refs *[]*ast.TypeRef) {
	walkBlock := func(b *ast.BlockStatement) {
		if b != nil {
			walkInstanceofRefs(b.Statements, generics, refs)
		}
	}
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		walkInstanceofStmt(s.Statement, generics, refs)
	case *ast.DeclarationStatement:
		walkInstanceofExpr(s.Value, generics, refs)
	case *ast.DestructuringStatement:
		walkInstanceofExpr(s.Value, generics, refs)
	case *ast.FunctionStatement:
		addGenericNames(s.Generics, generics)
		for _, p := range s.Parameters {
			walkInstanceofExpr(p.Default, generics, refs)
		}
		walkBlock(s.Body)
	case *ast.ClassStatement:
		addGenericNames(s.Generics, generics)
		for _, member := range s.Members {
			walkInstanceofStmt(member, generics, refs)
		}
		if s.Destructor != nil {
			walkBlock(s.Destructor.Body)
		}
	case *ast.InterfaceStatement:
		addGenericNames(s.Generics, generics)
		for _, sig := range s.Methods {
			addGenericNames(sig.Generics, generics)
		}
		for _, def := range s.Defaults {
			walkInstanceofStmt(def, generics, refs)
		}
	case *ast.InitStatement:
		walkBlock(s.Body)
	case *ast.ExpressionStatement:
		walkInstanceofExpr(s.Expression, generics, refs)
	case *ast.ReturnStatement:
		walkInstanceofExpr(s.Value, generics, refs)
	case *ast.YieldStatement:
		walkInstanceofExpr(s.Value, generics, refs)
	case *ast.SimpleStatement:
		walkInstanceofExpr(s.Value, generics, refs)
	case *ast.IfStatement:
		walkInstanceofExpr(s.Condition, generics, refs)
		walkBlock(s.Consequence)
		for _, ei := range s.ElseIfs {
			walkInstanceofExpr(ei.Condition, generics, refs)
			walkBlock(ei.Body)
		}
		walkBlock(s.Alternative)
	case *ast.WhileStatement:
		walkInstanceofExpr(s.Condition, generics, refs)
		walkBlock(s.Body)
	case *ast.ForStatement:
		if s.Init != nil {
			walkInstanceofStmt(s.Init, generics, refs)
		}
		walkInstanceofExpr(s.Condition, generics, refs)
		if s.Update != nil {
			walkInstanceofStmt(s.Update, generics, refs)
		}
		walkInstanceofExpr(s.Iterable, generics, refs)
		walkBlock(s.Body)
	case *ast.WithStatement:
		walkInstanceofExpr(s.Value, generics, refs)
		walkBlock(s.Body)
	case *ast.TryStatement:
		walkBlock(s.Body)
		for _, c := range s.Catches {
			walkBlock(c.Body)
		}
		walkBlock(s.Finally)
	case *ast.MatchStatement:
		walkInstanceofExpr(s.Expr, generics, refs)
		for _, c := range s.Cases {
			walkInstanceofExpr(c.Guard, generics, refs)
			walkBlock(c.Body)
		}
	case *ast.SelectStatement:
		for _, c := range s.Cases {
			walkBlock(c.Body)
		}
		walkBlock(s.Default)
	case *ast.BlockStatement:
		walkInstanceofRefs(s.Statements, generics, refs)
	}
}

func walkInstanceofExpr(expr ast.Expression, generics map[string]bool, refs *[]*ast.TypeRef) {
	switch e := expr.(type) {
	case nil:
		return
	case *ast.InfixExpression:
		walkInstanceofExpr(e.Left, generics, refs)
		walkInstanceofExpr(e.Right, generics, refs)
		if e.RightType != nil {
			*refs = append(*refs, e.RightType)
		}
	case *ast.PrefixExpression:
		walkInstanceofExpr(e.Right, generics, refs)
	case *ast.PostfixExpression:
		walkInstanceofExpr(e.Left, generics, refs)
	case *ast.AssignmentExpression:
		walkInstanceofExpr(e.Left, generics, refs)
		walkInstanceofExpr(e.Value, generics, refs)
	case *ast.CastExpression:
		walkInstanceofExpr(e.Value, generics, refs)
	case *ast.SelectorExpression:
		walkInstanceofExpr(e.Object, generics, refs)
	case *ast.IndexExpression:
		walkInstanceofExpr(e.Left, generics, refs)
		walkInstanceofExpr(e.Index, generics, refs)
	case *ast.CallExpression:
		walkInstanceofExpr(e.Callee, generics, refs)
		for _, arg := range e.Arguments {
			walkInstanceofExpr(arg.Value, generics, refs)
		}
	case *ast.FunctionLiteral:
		for _, p := range e.Parameters {
			walkInstanceofExpr(p.Default, generics, refs)
		}
		if e.Body != nil {
			walkInstanceofRefs(e.Body.Statements, generics, refs)
		}
	case *ast.SpreadExpression:
		walkInstanceofExpr(e.Value, generics, refs)
	case *ast.InterpolatedString:
		for _, part := range e.Parts {
			walkInstanceofExpr(part, generics, refs)
		}
	case *ast.TernaryExpression:
		walkInstanceofExpr(e.Condition, generics, refs)
		walkInstanceofExpr(e.ThenExpr, generics, refs)
		walkInstanceofExpr(e.ElseExpr, generics, refs)
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			walkInstanceofExpr(el, generics, refs)
		}
	case *ast.SetLiteral:
		for _, el := range e.Elements {
			walkInstanceofExpr(el, generics, refs)
		}
	case *ast.DictLiteral:
		for _, entry := range e.Entries {
			walkInstanceofExpr(entry.Key, generics, refs)
			walkInstanceofExpr(entry.Value, generics, refs)
		}
	case *ast.MatchExpression:
		walkInstanceofExpr(e.Expr, generics, refs)
		for _, c := range e.Cases {
			walkInstanceofExpr(c.Guard, generics, refs)
			walkInstanceofExpr(c.Value, generics, refs)
		}
	}
}
