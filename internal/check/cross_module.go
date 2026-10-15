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
	collector := &crossModuleCollector{aliases: aliases, opts: opts, cache: cache}
	(&astVisitor{expr: collector.visit}).program(program)
	return diagsWithFile(append(diags, collector.diags...), file)
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

// collectDeclaredNames gathers every name a binding construct introduces anywhere in the program.
func collectDeclaredNames(program *ast.Program) map[string]bool {
	names := map[string]bool{}
	(&astVisitor{binding: func(name string) { names[name] = true }}).program(program)
	return names
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
