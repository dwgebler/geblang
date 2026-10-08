package check

import (
	"fmt"
	"os"
	"sync"
	"time"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// ModuleCache memoises parsed module sources so successive
// cross-module checks don't re-parse the same file on every keystroke.
// Entries are invalidated by mtime; the cache is safe for concurrent
// reads via the embedded sync.Mutex.
type ModuleCache struct {
	mu      sync.Mutex
	entries map[string]moduleCacheEntry
}

type moduleCacheEntry struct {
	mtime   time.Time
	program *ast.Program
	exports map[string]struct{}
}

// NewModuleCache returns an empty cache ready to use.
func NewModuleCache() *ModuleCache {
	return &ModuleCache{entries: map[string]moduleCacheEntry{}}
}

func (c *ModuleCache) load(path string) (*ast.Program, map[string]struct{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	if entry, ok := c.entries[path]; ok && entry.mtime.Equal(info.ModTime()) {
		c.mu.Unlock()
		return entry.program, entry.exports, nil
	}
	c.mu.Unlock()
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return program, collectExports(program), nil
	}
	exports := collectExports(program)
	c.mu.Lock()
	c.entries[path] = moduleCacheEntry{mtime: info.ModTime(), program: program, exports: exports}
	c.mu.Unlock()
	return program, exports, nil
}

// collectExports returns the set of names a module exposes to importers:
// every `export` declaration, every top-level `class` / `interface` /
// `enum`, and every top-level `func` whose name does not begin with `_`.
func collectExports(program *ast.Program) map[string]struct{} {
	exports := map[string]struct{}{}
	if program == nil {
		return exports
	}
	add := func(name string) {
		if name == "" || name[0] == '_' {
			return
		}
		exports[name] = struct{}{}
	}
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *ast.ExportStatement:
			add(exportedName(s.Statement))
		case *ast.FunctionStatement:
			if s.Name != nil {
				add(s.Name.Value)
			}
		case *ast.ClassStatement:
			if s.Name != nil {
				add(s.Name.Value)
			}
		case *ast.InterfaceStatement:
			if s.Name != nil {
				add(s.Name.Value)
			}
		case *ast.EnumStatement:
			if s.Name != nil {
				add(s.Name.Value)
			}
		case *ast.DeclarationStatement:
			if s.Name != nil {
				add(s.Name.Value)
			}
		}
	}
	return exports
}

func exportedName(stmt ast.Statement) string {
	switch s := stmt.(type) {
	case *ast.FunctionStatement:
		if s.Name != nil {
			return s.Name.Value
		}
	case *ast.ClassStatement:
		if s.Name != nil {
			return s.Name.Value
		}
	case *ast.InterfaceStatement:
		if s.Name != nil {
			return s.Name.Value
		}
	case *ast.EnumStatement:
		if s.Name != nil {
			return s.Name.Value
		}
	case *ast.DeclarationStatement:
		if s.Name != nil {
			return s.Name.Value
		}
	}
	return ""
}

// checkCrossModuleSymbols flags `foo.bar()` where `foo` is a resolved
// import but `bar` is not exported by `foo`'s module. Native modules
// look up their export set in opts.NativeSymbols; source modules load
// their AST via opts.Resolver and ModuleCache.
func checkCrossModuleSymbols(file string, program *ast.Program, opts Options) []Diagnostic {
	aliases := collectImportAliases(program)
	// A local binding whose name matches an imported module shadows it
	// (lexical scope wins), so `name.member` resolves to the local, not
	// the module. Drop shadowed aliases to avoid false positives.
	for name := range collectDeclaredNames(program) {
		delete(aliases, name)
	}
	cache := opts.ModuleCache
	if cache == nil {
		cache = NewModuleCache()
	}
	diags := checkFromImportSymbols(program, opts, cache)
	if len(aliases) == 0 {
		return diagsWithFile(diags, file)
	}
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *ast.ImportStatement, *ast.FromImportStatement:
			continue
		}
		diags = append(diags, walkStatementForCrossModule(stmt, aliases, opts, cache)...)
	}
	return diagsWithFile(diags, file)
}

// checkFromImportSymbols flags each `from M import N` whose N is not
// exported by M. Uses opts.NativeSymbols for native modules and the
// module cache for project modules.
func checkFromImportSymbols(program *ast.Program, opts Options, cache *ModuleCache) []Diagnostic {
	diags := []Diagnostic{}
	for _, stmt := range program.Statements {
		from, ok := stmt.(*ast.FromImportStatement)
		if !ok {
			continue
		}
		canonical := joinPath(from.Path)
		if canonical == "" {
			continue
		}
		native := IsNativeImport(canonical)
		exports, lookup := resolveExportSet(canonical, native, opts, cache)
		if !lookup {
			continue
		}
		var typeAliases map[string]string
		if !native {
			typeAliases = (&crossTypeCollector{opts: opts, cache: cache}).typeAliasNames(importAlias{canonical: canonical})
		}
		for _, item := range from.Names {
			if item.Name == nil {
				continue
			}
			if _, exists := exports[item.Name.Value]; exists {
				continue
			}
			if _, isAlias := typeAliases[item.Name.Value]; isAlias {
				continue
			}
			diags = append(diags, Diagnostic{
				Line:     item.Name.Token.Line,
				Column:   item.Name.Token.Column,
				Severity: SeverityError,
				Rule:     "import",
				Message:  fmt.Sprintf("%s has no exported member %s", canonical, item.Name.Value),
			})
		}
	}
	return diags
}

func resolveExportSet(canonical string, native bool, opts Options, cache *ModuleCache) (map[string]struct{}, bool) {
	var nativeExports map[string]struct{}
	if native && opts.NativeSymbols != nil {
		if symbols, ok := opts.NativeSymbols[canonical]; ok && len(symbols) > 0 {
			nativeExports = symbols
		}
	}
	if opts.Resolver != nil {
		if path, err := opts.Resolver.Resolve(canonical); err == nil {
			if _, exports, err := cache.load(path); err == nil {
				// Dual-name module: stdlib wraps a native of the same name.
				// Surface both sets so callers can reach either.
				if nativeExports != nil {
					merged := make(map[string]struct{}, len(exports)+len(nativeExports))
					for k := range nativeExports {
						merged[k] = struct{}{}
					}
					for k := range exports {
						merged[k] = struct{}{}
					}
					return merged, true
				}
				return exports, true
			}
		}
	}
	if nativeExports != nil {
		return nativeExports, true
	}
	return nil, false
}

func diagsWithFile(diags []Diagnostic, file string) []Diagnostic {
	for i := range diags {
		if diags[i].File == "" {
			diags[i].File = file
		}
	}
	return diags
}

type importAlias struct {
	canonical string
	native    bool
}

func collectImportAliases(program *ast.Program) map[string]importAlias {
	out := map[string]importAlias{}
	for _, stmt := range program.Statements {
		if imp, ok := stmt.(*ast.ImportStatement); ok {
			alias := imp.ModuleName()
			canonical := joinPath(imp.Path)
			out[alias] = importAlias{canonical: canonical, native: IsNativeImport(canonical)}
		}
	}
	return out
}

// collectDeclaredNames gathers every name introduced by a local
// declaration, parameter, loop variable, destructuring target, or catch
// binding anywhere in the program. Used to detect module-name shadowing.
func collectDeclaredNames(program *ast.Program) map[string]bool {
	names := map[string]bool{}
	for _, stmt := range program.Statements {
		collectNamesStmt(stmt, names)
	}
	return names
}

func addName(id *ast.Identifier, names map[string]bool) {
	if id != nil && id.Value != "" {
		names[id.Value] = true
	}
}

func addParams(params []ast.Parameter, names map[string]bool) {
	for _, p := range params {
		addName(p.Name, names)
		collectNamesExpr(p.Default, names)
	}
}

func collectNamesStmt(stmt ast.Statement, names map[string]bool) {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		collectNamesStmt(s.Statement, names)
	case *ast.DeclarationStatement:
		addName(s.Name, names)
		collectNamesExpr(s.Value, names)
	case *ast.DestructuringStatement:
		for _, n := range s.Names {
			addName(n, names)
		}
		collectNamesExpr(s.Value, names)
	case *ast.ExpressionStatement:
		collectNamesExpr(s.Expression, names)
	case *ast.ReturnStatement:
		collectNamesExpr(s.Value, names)
	case *ast.YieldStatement:
		collectNamesExpr(s.Value, names)
	case *ast.SimpleStatement:
		collectNamesExpr(s.Value, names)
	case *ast.IfStatement:
		collectNamesExpr(s.Condition, names)
		collectNamesBlock(s.Consequence, names)
		for _, clause := range s.ElseIfs {
			collectNamesExpr(clause.Condition, names)
			collectNamesBlock(clause.Body, names)
		}
		collectNamesBlock(s.Alternative, names)
	case *ast.WhileStatement:
		collectNamesBlock(s.Body, names)
	case *ast.ForStatement:
		for _, n := range s.VarNames {
			addName(n, names)
		}
		collectNamesStmt(s.Init, names)
		collectNamesBlock(s.Body, names)
	case *ast.FunctionStatement:
		addParams(s.Parameters, names)
		collectNamesBlock(s.Body, names)
	case *ast.ClassStatement:
		for _, member := range s.Members {
			collectNamesStmt(member, names)
		}
	case *ast.TryStatement:
		collectNamesBlock(s.Body, names)
		for _, clause := range s.Catches {
			addName(clause.Name, names)
			collectNamesBlock(clause.Body, names)
		}
		collectNamesBlock(s.Finally, names)
	case *ast.MatchStatement:
		for _, clause := range s.Cases {
			collectNamesBlock(clause.Body, names)
		}
	case *ast.InitStatement:
		collectNamesBlock(s.Body, names)
	}
}

func collectNamesBlock(block *ast.BlockStatement, names map[string]bool) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		collectNamesStmt(stmt, names)
	}
}

// collectNamesExpr only recurses into expressions that can introduce
// new bindings (function literals); other expression shapes are skipped.
func collectNamesExpr(expr ast.Expression, names map[string]bool) {
	if fn, ok := expr.(*ast.FunctionLiteral); ok {
		addParams(fn.Parameters, names)
		collectNamesBlock(fn.Body, names)
	}
}

func joinPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	out := path[0]
	for _, p := range path[1:] {
		out += "." + p
	}
	return out
}

func walkStatementForCrossModule(stmt ast.Statement, aliases map[string]importAlias, opts Options, cache *ModuleCache) []Diagnostic {
	collector := &crossModuleCollector{aliases: aliases, opts: opts, cache: cache}
	walkStmt(stmt, collector)
	return collector.diags
}

type crossModuleCollector struct {
	aliases map[string]importAlias
	opts    Options
	cache   *ModuleCache
	diags   []Diagnostic
}

func (c *crossModuleCollector) visit(expr ast.Expression) {
	sel, ok := expr.(*ast.SelectorExpression)
	if !ok {
		return
	}
	ident, ok := sel.Object.(*ast.Identifier)
	if !ok {
		return
	}
	alias, ok := c.aliases[ident.Value]
	if !ok {
		return
	}
	if sel.Name == nil {
		return
	}
	name := sel.Name.Value
	if name == "" {
		return
	}
	if c.symbolExists(alias, name) {
		return
	}
	c.diags = append(c.diags, Diagnostic{
		Line:     sel.Name.Token.Line,
		Column:   sel.Name.Token.Column,
		Severity: SeverityError,
		Rule:     "import",
		Message:  fmt.Sprintf("%s has no exported member %s", alias.canonical, name),
	})
}

func (c *crossModuleCollector) symbolExists(alias importAlias, name string) bool {
	exports, ok := resolveExportSet(alias.canonical, alias.native, c.opts, c.cache)
	if !ok {
		return true
	}
	_, exists := exports[name]
	return exists
}

// walkStmt drives expression visits across statement shapes; mirrors
// the recursion the lint pass uses, kept narrow to selector expressions.
func walkStmt(stmt ast.Statement, c *crossModuleCollector) {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		walkStmt(s.Statement, c)
	case *ast.DeclarationStatement:
		walkExpr(s.Value, c)
	case *ast.DestructuringStatement:
		walkExpr(s.Value, c)
	case *ast.ExpressionStatement:
		walkExpr(s.Expression, c)
	case *ast.ReturnStatement:
		walkExpr(s.Value, c)
	case *ast.YieldStatement:
		walkExpr(s.Value, c)
	case *ast.SimpleStatement:
		walkExpr(s.Value, c)
	case *ast.IfStatement:
		walkExpr(s.Condition, c)
		walkBlock(s.Consequence, c)
		for _, clause := range s.ElseIfs {
			walkExpr(clause.Condition, c)
			walkBlock(clause.Body, c)
		}
		walkBlock(s.Alternative, c)
	case *ast.WhileStatement:
		walkExpr(s.Condition, c)
		walkBlock(s.Body, c)
	case *ast.ForStatement:
		walkStmt(s.Init, c)
		walkExpr(s.Condition, c)
		walkStmt(s.Update, c)
		walkExpr(s.Iterable, c)
		walkBlock(s.Body, c)
	case *ast.FunctionStatement:
		walkBlock(s.Body, c)
	case *ast.ClassStatement:
		for _, member := range s.Members {
			walkStmt(member, c)
		}
	case *ast.TryStatement:
		walkBlock(s.Body, c)
		for _, clause := range s.Catches {
			walkBlock(clause.Body, c)
		}
		walkBlock(s.Finally, c)
	case *ast.MatchStatement:
		walkExpr(s.Expr, c)
		for _, clause := range s.Cases {
			walkExpr(clause.Guard, c)
			walkExpr(clause.Value, c)
			walkBlock(clause.Body, c)
		}
	case *ast.InitStatement:
		walkBlock(s.Body, c)
	}
}

func walkBlock(block *ast.BlockStatement, c *crossModuleCollector) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		walkStmt(stmt, c)
	}
}

func walkExpr(expr ast.Expression, c *crossModuleCollector) {
	if expr == nil {
		return
	}
	c.visit(expr)
	switch e := expr.(type) {
	case *ast.SpreadExpression:
		walkExpr(e.Value, c)
	case *ast.InterpolatedString:
		for _, part := range e.Parts {
			walkExpr(part, c)
		}
	case *ast.PrefixExpression:
		walkExpr(e.Right, c)
	case *ast.PostfixExpression:
		walkExpr(e.Left, c)
	case *ast.InfixExpression:
		walkExpr(e.Left, c)
		walkExpr(e.Right, c)
	case *ast.AssignmentExpression:
		walkExpr(e.Left, c)
		walkExpr(e.Value, c)
	case *ast.SelectorExpression:
		walkExpr(e.Object, c)
	case *ast.CallExpression:
		walkExpr(e.Callee, c)
		for _, arg := range e.Arguments {
			walkExpr(arg.Value, c)
		}
	case *ast.IndexExpression:
		walkExpr(e.Left, c)
		walkExpr(e.Index, c)
	case *ast.ListLiteral:
		for _, element := range e.Elements {
			walkExpr(element, c)
		}
	case *ast.DictLiteral:
		for _, pair := range e.Entries {
			walkExpr(pair.Key, c)
			walkExpr(pair.Value, c)
		}
	case *ast.SetLiteral:
		for _, element := range e.Elements {
			walkExpr(element, c)
		}
	case *ast.RangeExpression:
		walkExpr(e.Start, c)
		walkExpr(e.End, c)
		walkExpr(e.Step, c)
	case *ast.FunctionLiteral:
		walkBlock(e.Body, c)
	case *ast.MatchExpression:
		walkExpr(e.Expr, c)
		for _, clause := range e.Cases {
			walkExpr(clause.Guard, c)
			walkExpr(clause.Value, c)
			walkBlock(clause.Body, c)
		}
	case *ast.AwaitExpression:
		walkExpr(e.Value, c)
	case *ast.CastExpression:
		walkExpr(e.Value, c)
	case *ast.TernaryExpression:
		walkExpr(e.Condition, c)
		walkExpr(e.ThenExpr, c)
		walkExpr(e.ElseExpr, c)
	}
}
