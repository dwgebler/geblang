package transpiler

import (
	"errors"
	"go/format"
	"sort"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/desugar"
	"geblang/internal/semantic"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/lower"
	"geblang/internal/transpiler/types"
)

type Input struct {
	Modules map[string]*ast.Program
	Sources map[string]string
}

type Output struct {
	Files map[string][]byte
}

func Transpile(input Input, opts Options) (Output, []Diagnostic, error) {
	out := Output{Files: map[string][]byte{}}
	if input.Modules == nil {
		return out, nil, nil
	}
	if opts.PackageName == "" {
		opts.PackageName = "geblang_transpiled"
	}

	bridge := lower.NewNativeBridge()
	mod := lower.NewModule("main", true, opts.IntMode)
	mod.SetEntryMainSignature(opts.EntryMainWantsArgs, opts.EntryMainReturnsInt)

	canonicals := make([]string, 0, len(input.Modules))
	for c := range input.Modules {
		canonicals = append(canonicals, c)
	}
	sort.Strings(canonicals)

	for _, canonical := range canonicals {
		// Every provided module is source AST; mark it so its calls route to the
		// transpiled export even when the name is also a native module.
		mod.RegisterSourceModule(canonical)
		if canonical == opts.EntryModule {
			continue
		}
		bindingName := lastSegment(canonical)
		mod.RegisterUserModule(bindingName, canonical)
	}

	var diags []Diagnostic
	for _, canonical := range canonicals {
		if err := desugar.Arrows(input.Modules[canonical]); err != nil {
			d := Diagnostic{Severity: SeverityError, File: canonical, Message: err.Error()}
			var positioned *desugar.PositionError
			if errors.As(err, &positioned) {
				d.Line, d.Column, d.Message = positioned.Line, positioned.Column, positioned.Message
			}
			diags = append(diags, d)
		}
	}
	if len(diags) > 0 {
		return out, diags, nil
	}

	// Register every module's classes before lowering any so cross-module
	// inheritance (parent in another module) resolves its full member set.
	prereg := lower.NewLowerer(mod, bridge, "")
	for _, canonical := range canonicals {
		prereg.PreregisterClasses(input.Modules[canonical])
	}
	// Pre-pass cross-module function return types so an entry lowered first can
	// infer the result of a `module.fn()` call into a chained method.
	for _, canonical := range canonicals {
		if canonical == opts.EntryModule {
			continue
		}
		prefix, ok := mod.UserModulePrefix(lastSegment(canonical))
		if !ok {
			prefix = userPrefixFor(canonical)
		}
		prereg.PreregisterModuleReturns(input.Modules[canonical], prefix)
	}

	entryProg, hasEntry := input.Modules[opts.EntryModule]
	if hasEntry {
		l := lower.NewLowerer(mod, bridge, input.Sources[opts.EntryModule])
		l.SetCanonical(opts.EntryModule)
		l.SetExprTypes(resolveExprTypes(entryProg))
		l.LowerProgram(entryProg)
		diags = appendLowerErrors(diags, l.Errors())
	}

	for _, canonical := range canonicals {
		if canonical == opts.EntryModule {
			continue
		}
		prefix, ok := mod.UserModulePrefix(lastSegment(canonical))
		if !ok {
			prefix = userPrefixFor(canonical)
		}
		l := lower.NewModuleLowerer(mod, bridge, input.Sources[canonical], prefix)
		l.SetCanonical(canonical)
		l.SetExprTypes(resolveExprTypes(input.Modules[canonical]))
		l.LowerProgram(input.Modules[canonical])
		diags = appendLowerErrors(diags, l.Errors())
	}

	raw := mod.Render()
	formatted, ferr := format.Source(raw)
	if ferr != nil {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			File:     input.Sources[opts.EntryModule],
			Message:  "gofmt failed: " + ferr.Error(),
			Hint:     "emitted Go was not syntactically valid; this is a transpiler bug",
		})
		formatted = raw
	}
	out.Files["main/main.go"] = formatted

	return out, diags, nil
}

func appendLowerErrors(diags []Diagnostic, errs []lower.Error) []Diagnostic {
	for _, e := range errs {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			File:     e.File,
			Line:     e.Line,
			Column:   e.Column,
			Message:  e.Message,
			Hint:     e.Hint,
		})
	}
	return diags
}

func lastSegment(canonical string) string {
	if i := strings.LastIndexByte(canonical, '.'); i >= 0 {
		return canonical[i+1:]
	}
	return canonical
}

func userPrefixFor(canonical string) string {
	return emit.MangleIdent(strings.ReplaceAll(canonical, ".", "_")) + "_"
}

// resolveExprTypes runs the semantic recording pass and converts its
// best-effort types into the transpiler's *types.Type, keyed by node.
func resolveExprTypes(prog *ast.Program) map[ast.Expression]*types.Type {
	if prog == nil {
		return nil
	}
	resolved := semantic.ResolveExpressionTypes(prog)
	if len(resolved) == 0 {
		return nil
	}
	out := make(map[ast.Expression]*types.Type, len(resolved))
	for expr, et := range resolved {
		if t := exprTypeToTransp(et); t != nil {
			out[expr] = t
		}
	}
	return out
}

// exprTypeToTransp rebuilds a minimal *ast.TypeRef and maps it via FromAST.
// Unions/intersections (Name has '|' or '&') and unknowns yield nil.
func exprTypeToTransp(et semantic.ExprType) *types.Type {
	ref := exprTypeToRef(et)
	if ref == nil {
		return nil
	}
	return types.FromAST(ref)
}

func exprTypeToRef(et semantic.ExprType) *ast.TypeRef {
	if !et.Known || et.Name == "" || strings.ContainsAny(et.Name, "|&") {
		return nil
	}
	ref := &ast.TypeRef{Name: et.Name, Nullable: et.Nullable}
	for _, arg := range et.Args {
		argRef := exprTypeToRef(arg)
		if argRef == nil {
			argRef = &ast.TypeRef{Name: "any"}
		}
		ref.Arguments = append(ref.Arguments, argRef)
	}
	return ref
}
