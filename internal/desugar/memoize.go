package desugar

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
)

// Memoize rewrites each @memoize top-level function into a cached wrapper over a
// renamed impl, keyed by the argument tuple. Idempotent. Methods/async/
// generators/void are rejected.
func Memoize(program *ast.Program) error {
	if err := rejectMemoizedMethods(program); err != nil {
		return err
	}
	var caches []ast.Statement
	var impls []ast.Statement
	for _, stmt := range program.Statements {
		fn := topLevelFunc(stmt)
		if fn == nil || !hasDecorator(fn.Decorators, "memoize") {
			continue
		}
		if err := validateMemoizable(fn); err != nil {
			return err
		}
		cache, impl, err := rewriteMemoized(fn)
		if err != nil {
			return err
		}
		caches = append(caches, cache)
		impls = append(impls, impl)
	}
	if len(caches) == 0 {
		return nil
	}
	insertAfterModule(program, caches)
	program.Statements = append(program.Statements, impls...)
	return nil
}

func topLevelFunc(stmt ast.Statement) *ast.FunctionStatement {
	if fn, ok := unwrapExport(stmt).(*ast.FunctionStatement); ok {
		return fn
	}
	return nil
}

func unwrapExport(stmt ast.Statement) ast.Statement {
	if export, ok := stmt.(*ast.ExportStatement); ok {
		return export.Statement
	}
	return stmt
}

func hasDecorator(decorators []ast.Decorator, name string) bool {
	for _, d := range decorators {
		if d.Name != nil && d.Name.Value == name {
			return true
		}
	}
	return false
}

func rejectMemoizedMethods(program *ast.Program) error {
	for _, stmt := range program.Statements {
		class := classFromStatement(stmt)
		if class == nil {
			continue
		}
		for _, member := range class.Members {
			if fn, ok := member.(*ast.FunctionStatement); ok && hasDecorator(fn.Decorators, "memoize") {
				return fmt.Errorf("@memoize is only supported on top-level functions, not methods (%s.%s)", class.Name.Value, fnName(fn))
			}
		}
	}
	return nil
}

func validateMemoizable(fn *ast.FunctionStatement) error {
	name := fnName(fn)
	if fn.Async {
		return fmt.Errorf("@memoize cannot be applied to async function %s", name)
	}
	if isGeneratorFunc(fn) {
		return fmt.Errorf("@memoize cannot be applied to generator function %s", name)
	}
	if fn.ReturnType == nil || strings.TrimSpace(fn.ReturnType.String()) == "void" {
		return fmt.Errorf("@memoize requires a non-void return type on %s", name)
	}
	for _, p := range fn.Parameters {
		if p.Name == nil {
			return fmt.Errorf("@memoize function %s has an unnamed parameter", name)
		}
	}
	return nil
}

// rewriteMemoized turns fn into the caching wrapper and returns the cache decl and renamed impl.
func rewriteMemoized(fn *ast.FunctionStatement) (ast.Statement, ast.Statement, error) {
	name := fn.Name.Value
	implName := "__memo_impl_" + name
	cacheName := "__memo_cache_" + name

	keyArgs := make([]string, len(fn.Parameters))
	callArgs := make([]string, len(fn.Parameters))
	for i, p := range fn.Parameters {
		keyArgs[i] = p.Name.Value
		if p.Variadic {
			callArgs[i] = "..." + p.Name.Value
		} else {
			callArgs[i] = p.Name.Value
		}
	}

	wrapperSrc := "func __memo_wrapper() {\n" +
		"\tlet __memo_k = [" + strings.Join(keyArgs, ", ") + "];\n" +
		"\tif (" + cacheName + ".contains(__memo_k)) { return " + cacheName + "[__memo_k]; }\n" +
		"\tlet __memo_v = " + implName + "(" + strings.Join(callArgs, ", ") + ");\n" +
		"\t" + cacheName + "[__memo_k] = __memo_v;\n" +
		"\treturn __memo_v;\n" +
		"}\n"
	body, err := parseFunctionBody(wrapperSrc)
	if err != nil {
		return nil, nil, fmt.Errorf("@memoize %s: %w", name, err)
	}

	impl := &ast.FunctionStatement{
		Token:      fn.Token,
		Name:       &ast.Identifier{Value: implName},
		Generics:   fn.Generics,
		Parameters: fn.Parameters,
		ReturnType: fn.ReturnType,
		Body:       fn.Body,
	}

	fn.Decorators = withoutDecorator(fn.Decorators, "memoize")
	fn.Body = body

	cacheSrc := "let " + cacheName + " = {};\n"
	cacheStmts, err := parseMembers(cacheSrc)
	if err != nil || len(cacheStmts) != 1 {
		return nil, nil, fmt.Errorf("@memoize %s: cache declaration failed", name)
	}
	return cacheStmts[0], impl, nil
}

func insertAfterModule(program *ast.Program, stmts []ast.Statement) {
	at := 0
	if len(program.Statements) > 0 {
		if _, ok := program.Statements[0].(*ast.ModuleStatement); ok {
			at = 1
		}
	}
	program.Statements = append(program.Statements[:at], append(stmts, program.Statements[at:]...)...)
}

func withoutDecorator(decorators []ast.Decorator, name string) []ast.Decorator {
	out := decorators[:0:0]
	for _, d := range decorators {
		if d.Name != nil && d.Name.Value == name {
			continue
		}
		out = append(out, d)
	}
	return out
}

func fnName(fn *ast.FunctionStatement) string {
	if fn.Name != nil {
		return fn.Name.Value
	}
	return "<anonymous>"
}

func isGeneratorFunc(fn *ast.FunctionStatement) bool {
	if fn.ReturnType != nil && strings.HasPrefix(strings.TrimSpace(fn.ReturnType.String()), "generator") {
		return true
	}
	return ast.BlockContainsYield(fn.Body)
}

func parseFunctionBody(src string) (*ast.BlockStatement, error) {
	stmts, err := parseMembers(src)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, fmt.Errorf("expected one function, got %d statements", len(stmts))
	}
	fn, ok := stmts[0].(*ast.FunctionStatement)
	if !ok || fn.Body == nil {
		return nil, fmt.Errorf("expected a function body")
	}
	return fn.Body, nil
}
