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
	markUsed := func(name string) {
		if dot := strings.IndexByte(name, '.'); dot >= 0 {
			name = name[:dot]
		}
		if imp, ok := imports[strings.ToLower(name)]; ok {
			imp.used = true
		}
	}
	var markType func(*ast.TypeRef)
	markType = func(ref *ast.TypeRef) {
		if ref == nil {
			return
		}
		markUsed(ref.Name)
		for _, arg := range ref.Arguments {
			markType(arg)
		}
		markType(ref.Left)
		markType(ref.Right)
	}
	var unreachable []Diagnostic
	walker := &astVisitor{
		expr: func(expr ast.Expression) {
			if id, ok := expr.(*ast.Identifier); ok {
				markUsed(id.Value)
			}
		},
		typeRef: markType,
		block: func(block *ast.BlockStatement) {
			unreachable = append(unreachable, lintUnreachableBlock(file, block)...)
		},
	}
	walker.program(program)
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
	return append(diagnostics, unreachable...)
}

func lintUnreachableBlock(file string, block *ast.BlockStatement) []Diagnostic {
	var diagnostics []Diagnostic
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
		terminated = statementTerminates(stmt)
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
	case *ast.WithStatement:
		return s.Token.Line, s.Token.Column
	case *ast.SelectStatement:
		return s.Token.Line, s.Token.Column
	case *ast.DelStatement:
		return s.Token.Line, s.Token.Column
	case *ast.EnumStatement:
		return s.Token.Line, s.Token.Column
	case *ast.InterfaceStatement:
		return s.Token.Line, s.Token.Column
	case *ast.TypeAliasStatement:
		return s.Token.Line, s.Token.Column
	case *ast.ModuleStatement:
		return s.Token.Line, s.Token.Column
	default:
		return 0, 0
	}
}
