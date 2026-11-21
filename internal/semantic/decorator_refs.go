package semantic

import "geblang/internal/ast"

// Type and function decorator arguments resolve in module scope, so they cannot see enclosing locals.
func (a *Analyzer) checkNestedDecoratorReferences(stmt ast.Statement, fn *ast.FunctionStatement) {
	if fn == nil {
		return
	}
	if s, ok := stmt.(*ast.FunctionStatement); ok {
		a.rejectDecoratorReferences(s.Decorators)
		for _, p := range s.Parameters {
			a.rejectDecoratorReferences(p.Decorators)
		}
	}
}

func (a *Analyzer) rejectDecoratorReferences(decorators []ast.Decorator) {
	for _, d := range decorators {
		for _, arg := range d.Arguments {
			if ast.DecoratorHasReference(arg.Value) {
				a.errorAt(d.Token, "decorator @%s: type and function arguments are only allowed on module-level declarations and their members", d.Name.Value)
				break
			}
		}
	}
}
