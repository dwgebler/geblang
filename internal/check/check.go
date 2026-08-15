// Package check is the shared static-analysis pipeline used by both
// `geblang check` (CLI) and the LSP server. It runs parse, semantic
// analysis, import resolution, bytecode compilation (for the type
// checks the semantic pass misses), and optional lint rules over a
// single Geblang source file.
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/embedfold"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/native"
	"geblang/internal/parser"
	"geblang/internal/semantic"
	"geblang/internal/token"
)

// Severity classifies a diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a single check finding.
type Diagnostic struct {
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Column   int      `json:"column,omitempty"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule,omitempty"`
	Message  string   `json:"message"`
}

// String renders the diagnostic in the geblang-check CLI's traditional
// `file:line:col: severity[rule]: message` format.
func (d Diagnostic) String() string {
	prefix := d.File
	if d.Line > 0 {
		prefix = fmt.Sprintf("%s:%d:%d", prefix, d.Line, d.Column)
	}
	if prefix == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Message)
	}
	if d.Rule != "" {
		return fmt.Sprintf("%s: %s[%s]: %s", prefix, d.Severity, d.Rule, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", prefix, d.Severity, d.Message)
}

// Options controls which checks run.
type Options struct {
	// Lint runs the optional lint rules (unused-import, unreachable).
	Lint bool
	// Resolver, when non-nil, is the module resolver used for import
	// checks. When nil, callers should leave imports unchecked or build
	// a default resolver via NewDefaultResolver.
	Resolver *modules.Resolver
	// CrossModule turns on cross-module symbol checks (foo.bar where
	// bar is not exported by foo). Requires Resolver.
	CrossModule bool
	// NativeSymbols, when non-nil, lists the exported function names
	// for each native module name. Used by cross-module checks. The
	// LSP populates this from internal/lsp/catalog.go.
	NativeSymbols map[string]map[string]struct{}
	// ModuleCache, when non-nil, caches parsed module sources keyed by
	// absolute path so successive cross-module checks don't re-parse.
	ModuleCache *ModuleCache
}

// NewDefaultResolver returns a resolver rooted at the file's directory.
// Mirrors the per-file behaviour of the legacy `checkResolver` helper.
func NewDefaultResolver(file string) *modules.Resolver {
	return modules.NewResolver([]string{filepath.Dir(file)})
}

// CrossModuleAnalysis runs the semantic analyzer (wired for cross-module
// class-method checks when opts.CrossModule and opts.Resolver are set) plus
// the standalone cross-module walkers (P1 unknown members, P5 unknown qualified
// types) over an already-parsed program. It does NOT run bytecode.Compile or
// lint; callers run those separately.
func CrossModuleAnalysis(file string, program *ast.Program, opts Options) []Diagnostic {
	diags := []Diagnostic{}
	analyzer := semantic.New()
	if opts.CrossModule {
		analyzer.EnableMethodChecks()
		if opts.Resolver != nil {
			cache := opts.ModuleCache
			if cache == nil {
				cache = NewModuleCache()
			}
			graph := buildClassGraph(program, opts, cache)
			analyzer.SetClassSurfaceResolver(graph.surface)
			analyzer.SetClassMethodSignatureResolver(graph.methodSignatures)
			analyzer.SetClassFieldTypeResolver(graph.fieldType)
		}
	}
	for _, sd := range analyzer.Analyze(program) {
		severity := SeverityError
		if sd.Severity == semantic.SeverityWarning {
			severity = SeverityWarning
		}
		rule := "semantic"
		if sd.Rule != "" {
			rule = sd.Rule
		}
		diags = append(diags, Diagnostic{
			File: file, Line: sd.Line, Column: sd.Column,
			Severity: severity, Rule: rule, Message: sd.Message,
		})
	}
	if opts.Resolver != nil {
		diags = append(diags, checkReservedNames(file, program, opts)...)
		diags = append(diags, checkImports(file, program, opts)...)
		if opts.CrossModule {
			diags = append(diags, checkCrossModuleSymbols(file, program, opts)...)
			diags = append(diags, checkCrossModuleTypes(file, program, opts)...)
			diags = append(diags, checkInstanceofTypes(file, program, opts)...)
		}
	}
	return diags
}

// Source runs the configured checks against a single source file.
// Returns the parsed program (or nil on parse failure) and the
// accumulated diagnostics.
func Source(file, source string, opts Options) (*ast.Program, []Diagnostic) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		diags := make([]Diagnostic, 0, len(p.Errors()))
		for _, msg := range p.Errors() {
			line, col, text := parseParserError(msg)
			diags = append(diags, Diagnostic{
				File:     file,
				Line:     line,
				Column:   col,
				Severity: SeverityError,
				Rule:     "parse",
				Message:  text,
			})
		}
		return nil, diags
	}
	// A same-module test file is analyzed merged with the module under
	// test so its private references resolve.
	analysisProgram := program
	if merged, ok := SameModuleTestProgram(file, program, opts.Resolver); ok {
		analysisProgram = merged
	}
	diags := []Diagnostic{}
	if _, foldDiags := embedfold.Fold(analysisProgram, file, embedfold.Validate); len(foldDiags) > 0 {
		for _, fd := range foldDiags {
			diags = append(diags, Diagnostic{File: file, Line: fd.Line, Column: fd.Column, Severity: SeverityError, Rule: "embed", Message: fd.Message})
		}
	}
	diags = append(diags, CrossModuleAnalysis(file, analysisProgram, opts)...)
	if _, compileErr := bytecode.CompileWithOptions(analysisProgram, []byte(source), file, bytecode.CompileOptions{NativeSymbols: opts.NativeSymbols}); compileErr != nil {
		if d, ok := compileDiagnostic(file, compileErr); ok {
			diags = append(diags, d)
		}
	}
	diags = suppressRedundantOverloadErrors(diags)
	diags = suppressEmbedFoldCascade(diags)
	if opts.Lint {
		diags = append(diags, lintProgram(file, program)...)
	}
	return program, diags
}

// SameModuleTestProgram merges a `*_test.gb` file that declares
// `module X;` with the module source X resolves to, so the test's
// references to X's private members analyze and execute as if declared
// inside the module (the Go same-package test convention). Module
// declarations are stripped from the merged program so it runs as a
// script. Returns ok=false when the convention does not apply.
func SameModuleTestProgram(file string, program *ast.Program, resolver *modules.Resolver) (*ast.Program, bool) {
	if resolver == nil || !strings.HasSuffix(file, "_test.gb") {
		return nil, false
	}
	canonical := DeclaredModuleName(program)
	if canonical == "" {
		return nil, false
	}
	modulePath, err := resolver.Resolve(canonical)
	if err != nil {
		return nil, false
	}
	absModule, _ := filepath.Abs(modulePath)
	absTest, _ := filepath.Abs(file)
	if absModule == absTest {
		return nil, false
	}
	moduleSource, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, false
	}
	mp := parser.New(lexer.New(string(moduleSource)))
	moduleProgram := mp.ParseProgram()
	if len(mp.Errors()) > 0 || DeclaredModuleName(moduleProgram) != canonical {
		return nil, false
	}
	merged := make([]ast.Statement, 0, len(moduleProgram.Statements)+len(program.Statements))
	merged = append(merged, withoutModuleDeclarations(moduleProgram.Statements)...)
	merged = append(merged, withoutModuleDeclarations(program.Statements)...)
	return &ast.Program{Statements: merged}, true
}

// DeclaredModuleName returns the program's `module X;` canonical name,
// or "" when the program is a plain script.
func DeclaredModuleName(program *ast.Program) string {
	for _, stmt := range program.Statements {
		if decl, ok := stmt.(*ast.ModuleStatement); ok {
			return strings.Join(decl.Path, ".")
		}
	}
	return ""
}

func withoutModuleDeclarations(statements []ast.Statement) []ast.Statement {
	out := make([]ast.Statement, 0, len(statements))
	for _, stmt := range statements {
		if _, ok := stmt.(*ast.ModuleStatement); ok {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

// checkReservedNames flags a user module that declares a reserved built-in
// name (native or stdlib, or the geblang namespace) and a `geblang.X` import
// whose target is not a built-in. Keyed on the declared module name, not the
// filename, so a namespaced module (e.g. `module gebweb.errors` in errors.gb)
// is fine. stdlib-path files are exempt: they legitimately wrap native modules.
func checkReservedNames(file string, program *ast.Program, opts Options) []Diagnostic {
	if opts.Resolver == nil || fileUnderStdlib(file, opts.Resolver) {
		return nil
	}
	var diags []Diagnostic
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *ast.ModuleStatement:
			name := strings.Join(s.Path, ".")
			if opts.Resolver.IsReservedModuleName(name) {
				diags = append(diags, Diagnostic{
					File: file, Line: s.Token.Line, Column: s.Token.Column,
					Severity: SeverityError, Rule: "module",
					Message: "module " + name + " shadows a reserved built-in module name; rename it (built-in module names and the geblang namespace are reserved)",
				})
			}
		case *ast.ImportStatement:
			if s.ForceBuiltin && !opts.Resolver.IsReservedModuleName(strings.Join(s.Path, ".")) {
				diags = append(diags, reservedImportDiag(file, s.Token, strings.Join(s.Path, ".")))
			}
		case *ast.FromImportStatement:
			if s.ForceBuiltin && !opts.Resolver.IsReservedModuleName(strings.Join(s.Path, ".")) {
				diags = append(diags, reservedImportDiag(file, s.Token, strings.Join(s.Path, ".")))
			}
		}
	}
	return diags
}

func reservedImportDiag(file string, tok token.Token, name string) Diagnostic {
	return Diagnostic{
		File: file, Line: tok.Line, Column: tok.Column,
		Severity: SeverityError, Rule: "import",
		Message: "geblang." + name + " is not a built-in module; the geblang namespace is reserved for built-ins",
	}
}

func fileUnderStdlib(file string, r *modules.Resolver) bool {
	abs, err := filepath.Abs(file)
	if err != nil {
		return false
	}
	for _, base := range r.StdlibPaths {
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(baseAbs, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// compileDiagnostic maps a bytecode compile error onto the static-
// analysis contract. A VM capability gap (IsParityError) is valid code
// the tree-walking evaluator runs but the bytecode VM cannot build yet,
// so it surfaces as a vm-unsupported warning rather than vanishing;
// every other compile error is a genuine static error both backends
// must reject. Returns false only for a nil error.
func compileDiagnostic(file string, compileErr error) (Diagnostic, bool) {
	if compileErr == nil {
		return Diagnostic{}, false
	}
	if bytecode.IsParityError(compileErr) {
		return Diagnostic{
			File:     file,
			Severity: SeverityWarning,
			Rule:     "vm-unsupported",
			Message:  compileErr.Error(),
		}, true
	}
	return Diagnostic{
		File:     file,
		Severity: SeverityError,
		Rule:     "type",
		Message:  compileErr.Error(),
	}, true
}

// suppressRedundantOverloadErrors drops a function's overload-mismatch error
// when an unknown-type error for that same function already explains it.
func suppressRedundantOverloadErrors(diags []Diagnostic) []Diagnostic {
	unknownParamFns := map[string]bool{}
	for _, d := range diags {
		if name, ok := unknownTypeFunctionName(d.Message); ok {
			unknownParamFns[name] = true
		}
	}
	if len(unknownParamFns) == 0 {
		return diags
	}
	out := diags[:0]
	for _, d := range diags {
		if name, ok := overloadErrorFunctionName(d.Message); ok && unknownParamFns[name] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// suppressEmbedFoldCascade drops the derivative unknown-function/unknown-bytecode-function diagnostics a failing embed() triggers.
func suppressEmbedFoldCascade(diags []Diagnostic) []Diagnostic {
	embedLines := map[string]bool{}
	for _, d := range diags {
		if d.Rule == "embed" {
			embedLines[embedLineKey(d.File, d.Line)] = true
		}
	}
	if len(embedLines) == 0 {
		return diags
	}
	out := diags[:0]
	for _, d := range diags {
		if d.Rule == "semantic" && d.Message == `unknown function "embed"` && embedLines[embedLineKey(d.File, d.Line)] {
			continue
		}
		if d.Rule == "type" && strings.HasSuffix(d.Message, "unknown bytecode function embed") {
			if line, ok := locatedLine(d.Message); ok && embedLines[embedLineKey(d.File, line)] {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

func embedLineKey(file string, line int) string {
	return file + "\x00" + strconv.Itoa(line)
}

// locatedLine extracts N from a "line N:M: ..." prefixed compiler message.
func locatedLine(msg string) (int, bool) {
	if !strings.HasPrefix(msg, "line ") {
		return 0, false
	}
	rest := msg[len("line "):]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:idx])
	if err != nil {
		return 0, false
	}
	return n, true
}

// stripLinePrefix removes a leading "line N:M: " that the bytecode compiler
// prepends; analyzer messages carry no such prefix.
func stripLinePrefix(msg string) string {
	if !strings.HasPrefix(msg, "line ") {
		return msg
	}
	if i := strings.Index(msg, ": "); i >= 0 {
		return msg[i+len(": "):]
	}
	return msg
}

// unknownTypeFunctionName extracts F from `unknown type "X" in parameter p of
// function F` (the analyzer's class-method context is also "function").
func unknownTypeFunctionName(msg string) (string, bool) {
	if !strings.HasPrefix(msg, "unknown type ") {
		return "", false
	}
	idx := strings.Index(msg, " of function ")
	if idx < 0 {
		return "", false
	}
	return strings.TrimSpace(msg[idx+len(" of function "):]), true
}

// overloadErrorFunctionName extracts F from the two overload-mismatch forms:
// the generic `no matching overload for F[...]` and the detailed `F expects
// <type> for parameter ...`.
func overloadErrorFunctionName(msg string) (string, bool) {
	msg = stripLinePrefix(msg)
	const prefix = "no matching overload for "
	if strings.HasPrefix(msg, prefix) {
		rest := msg[len(prefix):]
		for _, sep := range []string{":", " with ", " returning "} {
			if i := strings.Index(rest, sep); i >= 0 {
				rest = rest[:i]
			}
		}
		return strings.TrimSpace(rest), true
	}
	if i := strings.Index(msg, " expects "); i >= 0 && strings.Contains(msg, " for parameter '") {
		return strings.TrimSpace(msg[:i]), true
	}
	return "", false
}

// parseParserError splits "line:col: message" into its parts. Parser
// errors that don't match this shape fall back to (0, 0, msg).
func parseParserError(msg string) (line, col int, text string) {
	parts := strings.SplitN(msg, ": ", 2)
	if len(parts) != 2 {
		return 0, 0, msg
	}
	pos := strings.SplitN(parts[0], ":", 2)
	if len(pos) != 2 {
		return 0, 0, msg
	}
	if _, err := fmt.Sscanf(pos[0], "%d", &line); err != nil {
		return 0, 0, msg
	}
	if _, err := fmt.Sscanf(pos[1], "%d", &col); err != nil {
		return 0, 0, msg
	}
	return line, col, parts[1]
}

func checkImports(file string, program *ast.Program, opts Options) []Diagnostic {
	resolver := opts.Resolver
	isNative := func(canonical string) bool {
		if IsNativeImport(canonical) {
			return true
		}
		// Engine-native modules (e.g. secureRandom, the ffi/ssh/proc
		// bridges) are not all listed in NativeModuleNames; trust the
		// engine-derived symbol set when present.
		_, ok := opts.NativeSymbols[canonical]
		return ok
	}
	diags := []Diagnostic{}
	for _, stmt := range program.Statements {
		switch imp := stmt.(type) {
		case *ast.ImportStatement:
			canonical := strings.Join(imp.Path, ".")
			if canonical == "" || isNative(canonical) {
				continue
			}
			if _, err := resolver.Resolve(canonical); err != nil {
				diags = append(diags, Diagnostic{
					File:     file,
					Line:     imp.Token.Line,
					Column:   imp.Token.Column,
					Severity: SeverityError,
					Rule:     "import",
					Message:  fmt.Sprintf("cannot resolve import %s", canonical),
				})
			}
		case *ast.FromImportStatement:
			canonical := strings.Join(imp.Path, ".")
			if canonical == "" || isNative(canonical) {
				continue
			}
			if _, err := resolver.Resolve(canonical); err != nil {
				diags = append(diags, Diagnostic{
					File:     file,
					Line:     imp.Token.Line,
					Column:   imp.Token.Column,
					Severity: SeverityError,
					Rule:     "import",
					Message:  fmt.Sprintf("cannot resolve import %s", canonical),
				})
			}
		}
	}
	return diags
}

// IsNativeImport reports whether the canonical module name is shipped
// as a native (Go-implemented) module. Thin pass-through to
// internal/native.IsNativeModule so the list has a single source of
// truth.
func IsNativeImport(canonical string) bool {
	return native.IsNativeModule(canonical)
}

// NativeImportModules returns the set of canonical names of native
// modules. Exposed so the LSP code-action quick-fix can rank candidates.
func NativeImportModules() []string {
	return native.NativeModuleList()
}

// SameFilePath reports whether two paths resolve to the same file on
// disk after absolute-path normalisation.
func SameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
