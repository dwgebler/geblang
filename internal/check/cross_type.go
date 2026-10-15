package check

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/typealias"
)

// checkCrossModuleTypes flags a module-qualified type annotation
// `m.Nope` where `m` is a resolved import but `Nope` is not in its
// export set. It walks every type-bearing position and bails (stays
// silent) on any uncertainty: an unknown alias, an unresolvable module,
// Membership uses the full export set plus exported type aliases, so a
// function name in type position is tolerated rather than flagged.
func checkCrossModuleTypes(file string, program *ast.Program, opts Options) []Diagnostic {
	aliases := collectImportAliases(program)
	for name := range collectDeclaredNames(program) {
		delete(aliases, name)
	}
	if len(aliases) == 0 {
		return nil
	}
	cache := opts.ModuleCache
	if cache == nil {
		cache = NewModuleCache()
	}
	c := &crossTypeCollector{aliases: aliases, opts: opts, cache: cache}
	(&astVisitor{typeRef: func(ref *ast.TypeRef) { collectTypeRef(ref, c.visit) }}).program(program)
	return diagsWithFile(c.diags, file)
}

type crossTypeCollector struct {
	aliases map[string]importAlias
	opts    Options
	cache   *ModuleCache
	diags   []Diagnostic
}

func (c *crossTypeCollector) visit(ref *ast.TypeRef) {
	if ref == nil || ref.Name == "" {
		return
	}
	dot := strings.LastIndex(ref.Name, ".")
	if dot <= 0 || dot == len(ref.Name)-1 {
		return
	}
	aliasName := ref.Name[:dot]
	member := ref.Name[dot+1:]
	alias, ok := c.aliases[aliasName]
	if !ok {
		return
	}
	exports, ok := resolveExportSet(alias.canonical, alias.native, c.opts, c.cache)
	if !ok {
		return
	}
	if _, exists := exports[member]; exists {
		return
	}
	if c.exportsTypeAlias(alias, member) {
		return
	}
	c.diags = append(c.diags, Diagnostic{
		Line:   ref.Token.Line,
		Column: ref.Token.Column,
		// An unknown qualified type is uninhabitable at runtime: no value
		// matches it, so every call that exercises the annotation is
		// rejected on both backends. Same epistemic status as the
		// unknown-member (P1) and unknown-method (P2) checks, so error.
		Severity: SeverityError,
		Rule:     "type",
		Message:  fmt.Sprintf("%s has no exported type %s", alias.canonical, member),
	})
}

func (c *crossTypeCollector) exportsTypeAlias(alias importAlias, member string) bool {
	_, ok := c.typeAliasNames(alias)[member]
	return ok
}

func (c *crossTypeCollector) typeAliasNames(alias importAlias) map[string]string {
	if c.opts.Resolver == nil {
		return nil
	}
	path, err := c.opts.Resolver.Resolve(alias.canonical)
	if err != nil {
		return nil
	}
	program, _, err := c.cache.load(path)
	if err != nil || program == nil {
		return nil
	}
	return typealias.ExportedFromProgram(program, alias.canonical)
}

// collectTypeRef recursively visits a TypeRef and its generic arguments,
// list-alias element, and union/intersection operands.
func collectTypeRef(ref *ast.TypeRef, fn func(*ast.TypeRef)) {
	if ref == nil {
		return
	}
	if ref.Operator != "" {
		collectTypeRef(ref.Left, fn)
		collectTypeRef(ref.Right, fn)
		return
	}
	fn(ref)
	for _, arg := range ref.Arguments {
		collectTypeRef(arg, fn)
	}
}
