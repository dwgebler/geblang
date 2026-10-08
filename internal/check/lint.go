package check

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
)

type lintImport struct {
	name    string
	line    int
	column  int
	used    bool
	module  string
	message string
}

// lintProgram runs the package's optional lint rules:
// - unused-import warning (when an `import x` alias is never referenced)
// - unreachable-statement warning (statements after return/throw/break/continue)
func lintProgram(file string, program *ast.Program) []Diagnostic {
	diagnostics := []Diagnostic{}
	imports := map[string]*lintImport{}
	for _, stmt := range program.Statements {
		switch imp := stmt.(type) {
		case *ast.ImportStatement:
			name := imp.ModuleName()
			module := strings.Join(imp.Path, ".")
			imports[strings.ToLower(name)] = &lintImport{
				name:    name,
				line:    imp.Token.Line,
				column:  imp.Token.Column,
				module:  module,
				message: fmt.Sprintf("import %s is not used", module),
			}
		case *ast.FromImportStatement:
			module := strings.Join(imp.Path, ".")
			for _, item := range imp.Names {
				if item.Name == nil {
					continue
				}
				local := item.Local()
				imports[strings.ToLower(local)] = &lintImport{
					name:    local,
					line:    item.Name.Token.Line,
					column:  item.Name.Token.Column,
					module:  module,
					message: fmt.Sprintf("from %s import %s: %s is not used", module, item.Name.Value, local),
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *ast.ImportStatement, *ast.FromImportStatement:
			continue
		}
		lintMarkStatementIdentifiers(stmt, imports)
	}
	for _, imp := range imports {
		if !imp.used {
			diagnostics = append(diagnostics, Diagnostic{
				File:     file,
				Line:     imp.line,
				Column:   imp.column,
				Severity: SeverityWarning,
				Rule:     "unused-import",
				Message:  imp.message,
			})
		}
	}
	for _, stmt := range program.Statements {
		diagnostics = append(diagnostics, lintUnreachableStatement(file, stmt)...)
	}
	return diagnostics
}

func lintMarkStatementIdentifiers(stmt ast.Statement, imports map[string]*lintImport) {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		lintMarkStatementIdentifiers(s.Statement, imports)
	case *ast.DeclarationStatement:
		lintMarkTypeRef(s.Type, imports)
		lintMarkExpressionIdentifiers(s.Value, imports)
	case *ast.DestructuringStatement:
		lintMarkExpressionIdentifiers(s.Value, imports)
	case *ast.ExpressionStatement:
		lintMarkExpressionIdentifiers(s.Expression, imports)
	case *ast.ReturnStatement:
		lintMarkExpressionIdentifiers(s.Value, imports)
	case *ast.YieldStatement:
		lintMarkExpressionIdentifiers(s.Value, imports)
	case *ast.SimpleStatement:
		lintMarkExpressionIdentifiers(s.Value, imports)
	case *ast.IfStatement:
		lintMarkExpressionIdentifiers(s.Condition, imports)
		lintMarkBlockIdentifiers(s.Consequence, imports)
		for _, clause := range s.ElseIfs {
			lintMarkExpressionIdentifiers(clause.Condition, imports)
			lintMarkBlockIdentifiers(clause.Body, imports)
		}
		lintMarkBlockIdentifiers(s.Alternative, imports)
	case *ast.WhileStatement:
		lintMarkExpressionIdentifiers(s.Condition, imports)
		lintMarkBlockIdentifiers(s.Body, imports)
	case *ast.ForStatement:
		lintMarkStatementIdentifiers(s.Init, imports)
		lintMarkExpressionIdentifiers(s.Condition, imports)
		lintMarkStatementIdentifiers(s.Update, imports)
		lintMarkExpressionIdentifiers(s.Iterable, imports)
		lintMarkExpressionIdentifiers(s.Step, imports)
		lintMarkBlockIdentifiers(s.Body, imports)
	case *ast.FunctionStatement:
		lintMarkDecorators(s.Decorators, imports)
		for _, param := range s.Parameters {
			lintMarkTypeRef(param.Type, imports)
			lintMarkExpressionIdentifiers(param.Default, imports)
		}
		lintMarkTypeRef(s.ReturnType, imports)
		lintMarkBlockIdentifiers(s.Body, imports)
	case *ast.ClassStatement:
		lintMarkDecorators(s.Decorators, imports)
		lintMarkTypeRef(s.Extends, imports)
		for _, typ := range s.Implements {
			lintMarkTypeRef(typ, imports)
		}
		for _, member := range s.Members {
			lintMarkStatementIdentifiers(member, imports)
		}
	case *ast.TypeAliasStatement:
		lintMarkTypeRef(s.Type, imports)
	case *ast.InterfaceStatement:
		for _, typ := range s.Parents {
			lintMarkTypeRef(typ, imports)
		}
		for _, method := range s.Methods {
			for _, param := range method.Parameters {
				lintMarkTypeRef(param.Type, imports)
				lintMarkExpressionIdentifiers(param.Default, imports)
			}
			lintMarkTypeRef(method.ReturnType, imports)
		}
	case *ast.TryStatement:
		lintMarkBlockIdentifiers(s.Body, imports)
		for _, clause := range s.Catches {
			lintMarkTypeRef(clause.Type, imports)
			lintMarkBlockIdentifiers(clause.Body, imports)
		}
		lintMarkBlockIdentifiers(s.Finally, imports)
	case *ast.EnumStatement:
	case *ast.MatchStatement:
		lintMarkExpressionIdentifiers(s.Expr, imports)
		for _, clause := range s.Cases {
			lintMarkExpressionIdentifiers(clause.Pattern, imports)
			lintMarkExpressionIdentifiers(clause.Guard, imports)
			lintMarkExpressionIdentifiers(clause.Value, imports)
			lintMarkBlockIdentifiers(clause.Body, imports)
		}
	case *ast.InitStatement:
		lintMarkBlockIdentifiers(s.Body, imports)
	}
}

func lintMarkBlockIdentifiers(block *ast.BlockStatement, imports map[string]*lintImport) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		lintMarkStatementIdentifiers(stmt, imports)
	}
}

func lintMarkDecorators(decorators []ast.Decorator, imports map[string]*lintImport) {
	for _, decorator := range decorators {
		if decorator.Name != nil {
			if imp, ok := imports[strings.ToLower(decorator.Name.Value)]; ok {
				imp.used = true
			}
		}
		for _, arg := range decorator.Arguments {
			lintMarkExpressionIdentifiers(arg.Value, imports)
		}
	}
}

func lintMarkTypeRef(typ *ast.TypeRef, imports map[string]*lintImport) {
	if typ == nil {
		return
	}
	name := typ.Name
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		name = name[:dot]
	}
	if imp, ok := imports[strings.ToLower(name)]; ok {
		imp.used = true
	}
	for _, arg := range typ.Arguments {
		lintMarkTypeRef(arg, imports)
	}
	lintMarkTypeRef(typ.Left, imports)
	lintMarkTypeRef(typ.Right, imports)
}

func lintMarkExpressionIdentifiers(expr ast.Expression, imports map[string]*lintImport) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		// `instanceof Mod.Type` is parsed as an Identifier holding the
		// stringified type ("Mod.Type"); strip to the module alias so the
		// import counts as used.
		name := e.Value
		if dot := strings.IndexByte(name, '.'); dot >= 0 {
			name = name[:dot]
		}
		if imp, ok := imports[strings.ToLower(name)]; ok {
			imp.used = true
		}
	case *ast.SpreadExpression:
		lintMarkExpressionIdentifiers(e.Value, imports)
	case *ast.InterpolatedString:
		for _, part := range e.Parts {
			lintMarkExpressionIdentifiers(part, imports)
		}
	case *ast.PrefixExpression:
		lintMarkExpressionIdentifiers(e.Right, imports)
	case *ast.PostfixExpression:
		lintMarkExpressionIdentifiers(e.Left, imports)
	case *ast.InfixExpression:
		lintMarkExpressionIdentifiers(e.Left, imports)
		lintMarkExpressionIdentifiers(e.Right, imports)
	case *ast.AssignmentExpression:
		lintMarkExpressionIdentifiers(e.Left, imports)
		lintMarkExpressionIdentifiers(e.Value, imports)
	case *ast.SelectorExpression:
		lintMarkExpressionIdentifiers(e.Object, imports)
	case *ast.CallExpression:
		lintMarkExpressionIdentifiers(e.Callee, imports)
		for _, arg := range e.Arguments {
			lintMarkExpressionIdentifiers(arg.Value, imports)
		}
	case *ast.IndexExpression:
		lintMarkExpressionIdentifiers(e.Left, imports)
		lintMarkExpressionIdentifiers(e.Index, imports)
	case *ast.ListLiteral:
		for _, element := range e.Elements {
			lintMarkExpressionIdentifiers(element, imports)
		}
	case *ast.DictLiteral:
		for _, pair := range e.Entries {
			lintMarkExpressionIdentifiers(pair.Key, imports)
			lintMarkExpressionIdentifiers(pair.Value, imports)
		}
	case *ast.SetLiteral:
		for _, element := range e.Elements {
			lintMarkExpressionIdentifiers(element, imports)
		}
	case *ast.RangeExpression:
		lintMarkExpressionIdentifiers(e.Start, imports)
		lintMarkExpressionIdentifiers(e.End, imports)
		lintMarkExpressionIdentifiers(e.Step, imports)
	case *ast.FunctionLiteral:
		for _, param := range e.Parameters {
			lintMarkTypeRef(param.Type, imports)
			lintMarkExpressionIdentifiers(param.Default, imports)
		}
		lintMarkTypeRef(e.ReturnType, imports)
		lintMarkBlockIdentifiers(e.Body, imports)
	case *ast.MatchExpression:
		lintMarkExpressionIdentifiers(e.Expr, imports)
		for _, clause := range e.Cases {
			lintMarkExpressionIdentifiers(clause.Pattern, imports)
			lintMarkExpressionIdentifiers(clause.Guard, imports)
			lintMarkExpressionIdentifiers(clause.Value, imports)
			lintMarkBlockIdentifiers(clause.Body, imports)
		}
	case *ast.AwaitExpression:
		lintMarkExpressionIdentifiers(e.Value, imports)
	case *ast.CastExpression:
		lintMarkExpressionIdentifiers(e.Value, imports)
		lintMarkTypeRef(e.Type, imports)
	case *ast.TernaryExpression:
		lintMarkExpressionIdentifiers(e.Condition, imports)
		lintMarkExpressionIdentifiers(e.ThenExpr, imports)
		lintMarkExpressionIdentifiers(e.ElseExpr, imports)
	case *ast.PipeExpression:
		lintMarkExpressionIdentifiers(e.Left, imports)
		lintMarkExpressionIdentifiers(e.Right, imports)
	case *ast.PartialExpression:
		lintMarkExpressionIdentifiers(e.Callee, imports)
		for _, arg := range e.Arguments {
			if !arg.Hole {
				lintMarkExpressionIdentifiers(arg.Value, imports)
			}
		}
	}
}

func lintUnreachableStatement(file string, stmt ast.Statement) []Diagnostic {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		return lintUnreachableStatement(file, s.Statement)
	case *ast.InitStatement:
		return lintUnreachableBlock(file, s.Body)
	case *ast.FunctionStatement:
		return lintUnreachableBlock(file, s.Body)
	case *ast.ClassStatement:
		diagnostics := []Diagnostic{}
		for _, member := range s.Members {
			diagnostics = append(diagnostics, lintUnreachableStatement(file, member)...)
		}
		return diagnostics
	case *ast.IfStatement:
		diagnostics := lintUnreachableBlock(file, s.Consequence)
		for _, clause := range s.ElseIfs {
			diagnostics = append(diagnostics, lintUnreachableBlock(file, clause.Body)...)
		}
		diagnostics = append(diagnostics, lintUnreachableBlock(file, s.Alternative)...)
		return diagnostics
	case *ast.WhileStatement:
		return lintUnreachableBlock(file, s.Body)
	case *ast.ForStatement:
		return lintUnreachableBlock(file, s.Body)
	case *ast.TryStatement:
		diagnostics := lintUnreachableBlock(file, s.Body)
		for _, clause := range s.Catches {
			diagnostics = append(diagnostics, lintUnreachableBlock(file, clause.Body)...)
		}
		diagnostics = append(diagnostics, lintUnreachableBlock(file, s.Finally)...)
		return diagnostics
	case *ast.MatchStatement:
		diagnostics := []Diagnostic{}
		for _, clause := range s.Cases {
			diagnostics = append(diagnostics, lintUnreachableBlock(file, clause.Body)...)
		}
		return diagnostics
	default:
		return nil
	}
}

func lintUnreachableBlock(file string, block *ast.BlockStatement) []Diagnostic {
	if block == nil {
		return nil
	}
	diagnostics := []Diagnostic{}
	terminated := false
	for _, stmt := range block.Statements {
		if terminated {
			line, column := statementPosition(stmt)
			diagnostics = append(diagnostics, Diagnostic{
				File:     file,
				Line:     line,
				Column:   column,
				Severity: SeverityWarning,
				Rule:     "unreachable",
				Message:  "statement is unreachable",
			})
			continue
		}
		diagnostics = append(diagnostics, lintUnreachableStatement(file, stmt)...)
		if statementTerminates(stmt) {
			terminated = true
		}
	}
	return diagnostics
}

func statementTerminates(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStatement:
		return true
	case *ast.SimpleStatement:
		return s.Kind == "break" || s.Kind == "continue" || s.Kind == "throw"
	case *ast.ExportStatement:
		return statementTerminates(s.Statement)
	default:
		return false
	}
}

func statementPosition(stmt ast.Statement) (int, int) {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ImportStatement:
		return s.Token.Line, s.Token.Column
	case *ast.FromImportStatement:
		return s.Token.Line, s.Token.Column
	case *ast.DeclarationStatement:
		return s.Token.Line, s.Token.Column
	case *ast.DestructuringStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ExpressionStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ReturnStatement:
		return s.Token.Line, s.Token.Column
	case *ast.YieldStatement:
		return s.Token.Line, s.Token.Column
	case *ast.SimpleStatement:
		return s.Token.Line, s.Token.Column
	case *ast.IfStatement:
		return s.Token.Line, s.Token.Column
	case *ast.WhileStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ForStatement:
		return s.Token.Line, s.Token.Column
	case *ast.FunctionStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ClassStatement:
		return s.Token.Line, s.Token.Column
	case *ast.TryStatement:
		return s.Token.Line, s.Token.Column
	case *ast.MatchStatement:
		return s.Token.Line, s.Token.Column
	case *ast.InitStatement:
		return s.Token.Line, s.Token.Column
	default:
		return 0, 0
	}
}
