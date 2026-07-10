package lsp

import (
	"sort"
	"strings"
	"unicode"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const (
	completionKindModule     = 9
	completionKindFunction   = 3
	completionKindClass      = 7
	completionKindField      = 5
	completionKindEnum       = 13
	completionKindProperty   = 10
	completionKindKeyword    = 14
	completionKindEnumMember = 20
)

func (s *server) completions(params CompletionParams) []CompletionItem {
	source, ok := s.document(params.TextDocument.URI)
	if !ok {
		return []CompletionItem{}
	}
	prefix := sourcePrefix(source, params.Position)
	if mod, ok := moduleMemberContext(prefix); ok {
		return moduleCompletionItems(mod)
	}
	if alias, ok := selectorReceiver(prefix); ok {
		if items, ok := s.userModuleCompletionItems(uriToPath(params.TextDocument.URI), source, alias); ok {
			return items
		}
	}
	enumInfos := fileEnumInfos(source)
	if enumName, ok := enumStaticContext(prefix, enumInfos); ok {
		return enumStaticCompletionItems(enumInfos[enumName])
	}
	if enumName, ok := enumInstanceContext(prefix, source, enumInfos); ok {
		return enumInstanceCompletionItems(enumInfos[enumName])
	}
	// `this.<TAB>` inside a class extending test.Test -> the builtin
	// assertion methods on the Test base class.
	if testThisContext(prefix, source) {
		return testMethodCompletionItems()
	}
	// Try primitive-type method completion: `someDecimal.<TAB>` -> the
	// methods registered for `decimal`. Type is inferred from the most
	// recent `<primitiveType> <name>` declaration in the file (a
	// lightweight lexical scan; explicit type annotations only).
	if typ, ok := primitiveMemberContext(prefix, source); ok {
		return primitiveMethodCompletionItems(typ)
	}
	// Stdlib class method completion: `someReq.<TAB>` where `someReq`
	// is declared as `http.Request someReq;` surfaces the catalogued
	// methods on http.Request.
	if qualified, ok := classMemberContext(prefix, source); ok {
		return classMethodCompletionItems(qualified)
	}
	if importPrefix, ok := importContext(prefix); ok {
		return moduleNameCompletionItems(importPrefix)
	}
	idPrefix := identifierPrefix(prefix)
	items := topLevelCompletionItems(idPrefix)
	items = append(items, userSymbolCompletionItems(source, idPrefix)...)
	return items
}

// testThisContext reports whether the cursor sits on a `this.`
// selector inside a class that extends `test.Test`. The check is
// lexical: any class declaration in the document with `extends
// test.Test` enables the path. False negatives (deeper class
// hierarchies) are acceptable - autocomplete is best-effort.
func testThisContext(prefix, source string) bool {
	trimmed := strings.TrimRightFunc(prefix, isIdentRune)
	if !strings.HasSuffix(trimmed, "this.") {
		return false
	}
	return strings.Contains(source, "extends test.Test")
}

func testMethodCompletionItems() []CompletionItem {
	names := make([]string, 0, len(testBaseMethods))
	for name := range testBaseMethods {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		method := testBaseMethods[name]
		items = append(items, CompletionItem{
			Label:         name,
			Kind:          completionKindFunction,
			Detail:        method.Signature(),
			Documentation: method.Doc,
		})
	}
	return items
}

func (s *server) signatureHelp(params SignatureHelpParams) SignatureHelp {
	source, ok := s.document(params.TextDocument.URI)
	if !ok {
		return SignatureHelp{}
	}
	prefix := sourcePrefix(source, params.Position)
	module, name, argPrefix, ok := callContext(prefix)
	if !ok {
		return SignatureHelp{}
	}
	// First try a class-method lookup: if `module` actually names a
	// typed local variable (`http.Request req; req.header(...)`),
	// resolve via the catalogued class methods. lookupFunction
	// otherwise falls back to module-level functions.
	if fn, ok := lookupClassMethod(source, module, name); ok {
		return SignatureHelp{
			Signatures: []SignatureInformation{{
				Label:         fn.Signature(),
				Documentation: fn.Doc,
				Parameters:    parameterInformation(fn.Params),
			}},
			ActiveSignature: 0,
			ActiveParameter: activeParameter(argPrefix, len(fn.Params)),
		}
	}
	if fn, ok := lookupEnumStaticMethod(source, module, name); ok {
		return SignatureHelp{
			Signatures: []SignatureInformation{{
				Label:         fn.Signature(),
				Documentation: fn.Doc,
				Parameters:    parameterInformation(fn.Params),
			}},
			ActiveSignature: 0,
			ActiveParameter: activeParameter(argPrefix, len(fn.Params)),
		}
	}
	if help, ok := s.userModuleSignatureHelp(uriToPath(params.TextDocument.URI), source, module, name, argPrefix); ok {
		return help
	}
	fn, ok := lookupFunction(module, name)
	if !ok {
		return SignatureHelp{}
	}
	return SignatureHelp{
		Signatures: []SignatureInformation{{
			Label:         fn.Signature(),
			Documentation: fn.Doc,
			Parameters:    parameterInformation(fn.Params),
		}},
		ActiveSignature: 0,
		ActiveParameter: activeParameter(argPrefix, len(fn.Params)),
	}
}

// lookupClassMethod resolves <varName>.<method> via the file's
// typed-variable table and the catalogued class method tables.
// Returns false when varName is not a typed stdlib-class local.
func lookupClassMethod(source, varName, method string) (functionDoc, bool) {
	if varName == "" || method == "" {
		return functionDoc{}, false
	}
	types := fileVarTypes(source)
	typ, ok := types[varName]
	if !ok {
		return functionDoc{}, false
	}
	methods := lookupClassMethods(typ)
	if methods == nil {
		return functionDoc{}, false
	}
	fn, ok := methods[method]
	return fn, ok
}

func lookupEnumStaticMethod(source, enumName, method string) (functionDoc, bool) {
	if enumName == "" || method == "" {
		return functionDoc{}, false
	}
	enums := fileEnumInfos(source)
	info, ok := enums[enumName]
	if !ok {
		return functionDoc{}, false
	}
	return enumStaticFunctionDoc(info, method)
}

func (s *server) document(uri string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.docs[uri]
	return source, ok
}

func sourcePrefix(source string, pos Position) string {
	lines := strings.Split(source, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return ""
	}
	line := lines[pos.Line]
	if pos.Character < 0 {
		pos.Character = 0
	}
	if pos.Character > len(line) {
		pos.Character = len(line)
	}
	return line[:pos.Character]
}

func moduleMemberContext(prefix string) (string, bool) {
	trimmed := strings.TrimRightFunc(prefix, isIdentRune)
	if !strings.HasSuffix(trimmed, ".") {
		return "", false
	}
	beforeDot := strings.TrimSpace(strings.TrimSuffix(trimmed, "."))
	name := trailingIdentifier(beforeDot)
	if name == "" {
		return "", false
	}
	if _, ok := stdlibCatalog[name]; ok {
		return name, true
	}
	return "", false
}

func importContext(prefix string) (string, bool) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "import" {
		return "", true
	}
	if strings.HasPrefix(trimmed, "import ") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "import ")), true
	}
	return "", false
}

func callContext(prefix string) (module, name, args string, ok bool) {
	open := strings.LastIndex(prefix, "(")
	if open < 0 {
		return "", "", "", false
	}
	callee := strings.TrimSpace(prefix[:open])
	args = prefix[open+1:]
	parts := strings.Split(callee, ".")
	if len(parts) == 1 {
		name = trailingIdentifier(parts[0])
		if name == "" {
			return "", "", "", false
		}
		return "", name, args, true
	}
	name = trailingIdentifier(parts[len(parts)-1])
	module = trailingIdentifier(parts[len(parts)-2])
	if module == "" || name == "" {
		return "", "", "", false
	}
	return module, name, args, true
}

func identifierPrefix(prefix string) string {
	return trailingIdentifier(prefix)
}

func trailingIdentifier(text string) string {
	i := len(text)
	for i > 0 {
		r, size := runeBefore(text[:i])
		if !isIdentRune(r) {
			break
		}
		i -= size
	}
	return text[i:]
}

func runeBefore(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xc0 != 0x80 {
			r := []rune(s[i:])
			if len(r) == 0 {
				return rune(s[i]), 1
			}
			return r[0], len(s) - i
		}
	}
	return 0, 0
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func moduleNameCompletionItems(prefix string) []CompletionItem {
	names := moduleNames()
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			items = append(items, CompletionItem{Label: name, Kind: completionKindModule, Detail: "stdlib module"})
		}
	}
	return items
}

func moduleCompletionItems(module string) []CompletionItem {
	mod, ok := stdlibCatalog[module]
	if !ok {
		return []CompletionItem{}
	}
	names := make([]string, 0, len(mod.Functions)+len(mod.Classes))
	for name := range mod.Functions {
		names = append(names, name)
	}
	for name := range mod.Classes {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		if fn, ok := mod.Functions[name]; ok {
			items = append(items, CompletionItem{
				Label:         name,
				Kind:          completionKindFunction,
				Detail:        fn.Signature(),
				Documentation: fn.Doc,
			})
			continue
		}
		items = append(items, CompletionItem{
			Label:  name,
			Kind:   completionKindClass,
			Detail: mod.Classes[name],
		})
	}
	return items
}

// primitiveMemberContext reports whether the cursor is on a selector
// `name.<cursor>` where `name` is a variable of a known primitive
// type (string, int, float, decimal, bool, bytes, list, dict, set,
// range). On a hit it returns the primitive-type keyword so the
// caller can look up its method table. Misses fall through to the
// next completion path.
//
// Type inference is intentionally simple: a lexical scan over the
// document collects the most recent `<primitive> <name>` declaration
// before the cursor. Inferred `let x = ...` and complex type
// expressions (`?string`, `list<int>`) are out of scope - autocomplete
// is best-effort.
func primitiveMemberContext(prefix, source string) (string, bool) {
	trimmed := strings.TrimRightFunc(prefix, isIdentRune)
	if !strings.HasSuffix(trimmed, ".") {
		return "", false
	}
	beforeDot := strings.TrimSpace(strings.TrimSuffix(trimmed, "."))
	name := trailingIdentifier(beforeDot)
	if name == "" {
		return "", false
	}
	types := fileVarTypes(source)
	typ, ok := types[name]
	if !ok {
		return "", false
	}
	if _, isPrimitive := primitiveTypeNames[typ]; !isPrimitive {
		return "", false
	}
	return typ, true
}

// classMemberContext mirrors primitiveMemberContext for qualified
// stdlib classes. Returns the fully-qualified class name (e.g.
// "http.Request") when the cursor is on `<var>.<TAB>` and var was
// declared as a stdlib class type.
func classMemberContext(prefix, source string) (string, bool) {
	trimmed := strings.TrimRightFunc(prefix, isIdentRune)
	if !strings.HasSuffix(trimmed, ".") {
		return "", false
	}
	beforeDot := strings.TrimSpace(strings.TrimSuffix(trimmed, "."))
	name := trailingIdentifier(beforeDot)
	if name == "" {
		return "", false
	}
	types := fileVarTypes(source)
	typ, ok := types[name]
	if !ok {
		return "", false
	}
	if _, _, isQualified := splitQualifiedClass(typ); !isQualified {
		return "", false
	}
	if lookupClassMethods(typ) == nil {
		return "", false
	}
	return typ, true
}

// classMethodCompletionItems returns the LSP CompletionItems for
// every method on the given qualified stdlib class.
func classMethodCompletionItems(qualified string) []CompletionItem {
	methods := lookupClassMethods(qualified)
	if methods == nil {
		return []CompletionItem{}
	}
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		method := methods[name]
		items = append(items, CompletionItem{
			Label:         name,
			Kind:          completionKindFunction,
			Detail:        method.Signature(),
			Documentation: method.Doc,
		})
	}
	return items
}

type lspEnumInfo struct {
	name        string
	backed      bool
	backingType string
	variants    []string
	methods     []string
}

func fileEnumInfos(source string) map[string]lspEnumInfo {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	out := map[string]lspEnumInfo{}
	if program == nil {
		return out
	}
	for _, stmt := range program.Statements {
		collectEnumInfo(stmt, out)
	}
	return out
}

func collectEnumInfo(stmt ast.Statement, out map[string]lspEnumInfo) {
	if exported, ok := stmt.(*ast.ExportStatement); ok && exported.Statement != nil {
		collectEnumInfo(exported.Statement, out)
		return
	}
	enumStmt, ok := stmt.(*ast.EnumStatement)
	if !ok || enumStmt.Name == nil {
		return
	}
	info := lspEnumInfo{name: enumStmt.Name.Value}
	if enumStmt.BackingType != nil {
		info.backed = true
		info.backingType = enumStmt.BackingType.Name
	}
	for _, variant := range enumStmt.Variants {
		if variant.Name != nil {
			info.variants = append(info.variants, variant.Name.Value)
		}
	}
	for _, method := range enumStmt.Methods {
		if method.Name != nil {
			info.methods = append(info.methods, method.Name.Value)
		}
	}
	sort.Strings(info.variants)
	sort.Strings(info.methods)
	out[info.name] = info
}

func enumStaticContext(prefix string, enums map[string]lspEnumInfo) (string, bool) {
	name, ok := selectorReceiver(prefix)
	if !ok {
		return "", false
	}
	if _, exists := enums[name]; exists {
		return name, true
	}
	return "", false
}

func enumInstanceContext(prefix, source string, enums map[string]lspEnumInfo) (string, bool) {
	name, ok := selectorReceiver(prefix)
	if !ok {
		return "", false
	}
	types := fileEnumVarTypes(source, enums)
	enumName, ok := types[name]
	if !ok {
		return "", false
	}
	if _, exists := enums[enumName]; !exists {
		return "", false
	}
	return enumName, true
}

func selectorReceiver(prefix string) (string, bool) {
	trimmed := strings.TrimRightFunc(prefix, isIdentRune)
	if !strings.HasSuffix(trimmed, ".") {
		return "", false
	}
	beforeDot := strings.TrimSpace(strings.TrimSuffix(trimmed, "."))
	name := trailingIdentifier(beforeDot)
	return name, name != ""
}

func enumStaticCompletionItems(info lspEnumInfo) []CompletionItem {
	items := make([]CompletionItem, 0, len(info.variants)+4)
	for _, variant := range info.variants {
		items = append(items, CompletionItem{
			Label:  variant,
			Kind:   completionKindEnumMember,
			Detail: info.name + "." + variant,
		})
	}
	names := []string{"fromName", "values"}
	if info.backed {
		names = append(names, "from", "tryFrom")
	}
	sort.Strings(names)
	staticMethods := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		fn, ok := enumStaticFunctionDoc(info, name)
		if !ok {
			continue
		}
		staticMethods = append(staticMethods, CompletionItem{
			Label:         name,
			Kind:          completionKindFunction,
			Detail:        fn.Signature(),
			Documentation: fn.Doc,
		})
	}
	return append(items, staticMethods...)
}

func enumStaticFunctionDoc(info lspEnumInfo, name string) (functionDoc, bool) {
	named := func(doc functionDoc) functionDoc {
		doc.Name = name
		return doc
	}
	switch name {
	case "fromName":
		return named(fn([]string{"string name"}, "?"+info.name, "Resolves a nullary variant by exact variant name, or returns null.")), true
	case "values":
		return named(fn([]string{}, "list<"+info.name+">", "Returns the enum's nullary variants in declaration order.")), true
	case "from":
		if !info.backed {
			return functionDoc{}, false
		}
		return named(fn([]string{info.backingType + " value"}, info.name, "Returns the variant with this backing value, or throws when absent.")), true
	case "tryFrom":
		if !info.backed {
			return functionDoc{}, false
		}
		return named(fn([]string{info.backingType + " value"}, "?"+info.name, "Returns the variant with this backing value, or null when absent.")), true
	}
	return functionDoc{}, false
}

func enumInstanceCompletionItems(info lspEnumInfo) []CompletionItem {
	items := []CompletionItem{
		{Label: "fields", Kind: completionKindProperty, Detail: "fields: list<any>", Documentation: "Associated payload values for this enum variant."},
		{Label: "variant", Kind: completionKindProperty, Detail: "variant: string", Documentation: "Variant name for this enum value."},
	}
	if info.backed {
		items = append(items, CompletionItem{
			Label:         "value",
			Kind:          completionKindField,
			Detail:        "value: " + info.backingType,
			Documentation: "Backing scalar value for this enum variant.",
		})
	}
	for _, method := range info.methods {
		items = append(items, CompletionItem{
			Label:  method,
			Kind:   completionKindFunction,
			Detail: info.name + "." + method + "(...)",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

func fileEnumVarTypes(source string, enums map[string]lspEnumInfo) map[string]string {
	out := map[string]string{}
	lines := strings.Split(source, "\n")
	for _, line := range lines {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"let ", "const ", "static "} {
			if strings.HasPrefix(line, prefix) {
				line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		typ := parts[0]
		if _, ok := enums[typ]; !ok {
			continue
		}
		name := parts[1]
		for _, sep := range []string{"=", ";", ","} {
			if idx := strings.Index(name, sep); idx >= 0 {
				name = name[:idx]
			}
		}
		name = strings.TrimSpace(name)
		if name == "" || !isIdentName(name) {
			continue
		}
		out[name] = typ
	}
	return out
}

// primitiveMethodCompletionItems returns the LSP CompletionItems for
// every method registered on the given primitive type. Sorted for
// determinism so the editor always shows the same order.
func primitiveMethodCompletionItems(typ string) []CompletionItem {
	methods, ok := primitiveMethods[typ]
	if !ok {
		return []CompletionItem{}
	}
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		method := methods[name]
		items = append(items, CompletionItem{
			Label:         name,
			Kind:          completionKindFunction,
			Detail:        method.Signature(),
			Documentation: method.Doc,
		})
	}
	return items
}

// fileVarTypes walks the source for `<primitiveType> <name>`-shape
// declarations and records the declared type per variable name. The
// scan is line-oriented and rough but correct for the common case:
// the user wrote `decimal d = 3.14;` and later types `d.<TAB>`.
// Re-declarations overwrite earlier ones (last wins, mirroring lexical
// scoping in the broad sense).
func fileVarTypes(source string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(source, "\n")
	for _, line := range lines {
		// Strip line comments.
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Accept a leading `let ` / `const ` / `static ` prefix so
		// `let decimal d = ...` is recognised. Strip it for the type
		// match below.
		for _, prefix := range []string{"let ", "const ", "static "} {
			if strings.HasPrefix(line, prefix) {
				line = strings.TrimPrefix(line, prefix)
				line = strings.TrimSpace(line)
			}
		}
		// Look for `TYPE NAME` where TYPE is a primitive keyword or a
		// `module.ClassName` qualified stdlib type, and NAME is a
		// simple identifier. The line must continue with `=` or `;`
		// (declaration) - we don't want to misread function parameter
		// lists or expressions.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		typ := parts[0]
		_, isPrimitive := primitiveTypeNames[typ]
		_, _, isQualifiedClass := splitQualifiedClass(typ)
		if !isPrimitive && !isQualifiedClass {
			continue
		}
		name := parts[1]
		// Trim the trailing `;` or `,` or `=...` if present.
		for _, sep := range []string{"=", ";", ","} {
			if idx := strings.Index(name, sep); idx >= 0 {
				name = name[:idx]
			}
		}
		name = strings.TrimSpace(name)
		if name == "" || !isIdentName(name) {
			continue
		}
		out[name] = typ
	}
	return out
}

// isIdentName reports whether s is a plain identifier (letters,
// digits, underscores, not starting with a digit).
func isIdentName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && (r >= '0' && r <= '9') {
			return false
		}
		if !isIdentRune(r) {
			return false
		}
	}
	return true
}

func userSymbolCompletionItems(source, prefix string) []CompletionItem {
	syms := extractSymbols(source)
	items := make([]CompletionItem, 0, len(syms))
	seen := map[string]bool{}
	for _, sym := range syms {
		if prefix != "" && !strings.HasPrefix(sym.name, prefix) {
			continue
		}
		if seen[sym.name] {
			continue
		}
		seen[sym.name] = true
		kind := completionKindFunction
		switch sym.kind {
		case symbolKindClass:
			kind = completionKindClass
		case symbolKindEnum:
			kind = completionKindEnum
		case symbolKindVariable, symbolKindConstant:
			kind = 6 // variable kind in LSP CompletionItemKind
		}
		items = append(items, CompletionItem{
			Label:  sym.name,
			Kind:   kind,
			Detail: sym.detail,
		})
	}
	return items
}

func topLevelCompletionItems(prefix string) []CompletionItem {
	items := moduleNameCompletionItems(prefix)
	for _, kw := range []string{"import", "export", "module", "init", "func", "class", "interface", "enum", "let", "var", "const", "type", "if", "else", "for", "in", "while", "with", "del", "return", "async", "await", "yield", "try", "catch", "throw"} {
		if prefix == "" || strings.HasPrefix(kw, prefix) {
			items = append(items, CompletionItem{Label: kw, Kind: completionKindKeyword, Detail: "keyword"})
		}
	}
	builtinNames := make([]string, 0, len(globalBuiltins))
	for name := range globalBuiltins {
		builtinNames = append(builtinNames, name)
	}
	sort.Strings(builtinNames)
	for _, name := range builtinNames {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		f := globalBuiltins[name]
		f.Name = name
		items = append(items, CompletionItem{
			Label:         name,
			Kind:          completionKindFunction,
			Detail:        f.Signature(),
			Documentation: f.Doc,
		})
	}
	if strings.HasPrefix(prefix, "_") || prefix == "" {
		for _, m := range magicMethods {
			if prefix == "" || strings.HasPrefix(m.name, prefix) {
				items = append(items, CompletionItem{Label: m.name, Kind: completionKindFunction, Detail: m.detail})
			}
		}
	}
	return items
}

// magicMethods enumerates the dunder methods recognised by the
// runtime. Surfaced as completions when the cursor sits on a name
// starting with `_` so editors can scaffold the right signature.
// Names match the recognised forms in internal/bytecode/vm.go and
// internal/evaluator/evaluator.go: most operators use a single
// trailing underscore segment (`__eq`, `__add`, ...). The four
// hooks `__enter__` / `__exit__` / `__serialize__` / `__deserialize__`
// keep the trailing `__` pair for historical reasons.
var magicMethods = []struct {
	name   string
	detail string
}{
	// Dynamic dispatch.
	{"__invoke", "callable-object dispatch (instance(...) and func-typed)"},
	{"__call", "fallback for unknown instance method names"},
	{"__callStatic", "fallback for unknown static method names"},
	{"__get", "fallback for unknown field reads"},
	{"__set", "fallback for unknown field writes"},
	{"__getStatic", "fallback for unknown static-value reads"},
	{"__setStatic", "fallback for unknown static-value writes"},
	{"__parentMsg", "explicit fallthrough to the parent class chain"},
	// Iteration protocol (1.0.6).
	{"__iter", "iterator-protocol: returns the iterator for `for in`"},
	{"__done", "iterator-protocol: true when iteration finished"},
	{"__next", "iterator-protocol: next value (called when not done)"},
	// Context managers.
	{"__enter", "with-block entry hook (return value bound to `with (n = ...)`)"},
	{"__exit", "with-block exit hook (always runs on any block exit)"},
	// Serialisation.
	{"__serialize", "json/yaml/toml.stringify override (returns plain value)"},
	{"__deserialize", "json/yaml/toml.parse factory (static method)"},
	// Type coercion.
	{"__string", "implicit-string conversion (str / interpolation / `+`)"},
	{"__int", "implicit-int conversion (cast-as / arithmetic)"},
	{"__float", "implicit-float conversion"},
	{"__decimal", "implicit-decimal conversion"},
	{"__bool", "implicit-bool conversion (truthiness / `if`)"},
	{"__bytes", "implicit-bytes conversion"},
	// Comparison.
	{"__eq", "equality operator (==)"},
	{"__lt", "less-than operator (<)"},
	{"__lte", "less-than-or-equal operator (<=)"},
	{"__gt", "greater-than operator (>)"},
	{"__gte", "greater-than-or-equal operator (>=)"},
	// Arithmetic operators.
	{"__add", "addition operator (+)"},
	{"__sub", "subtraction operator (-)"},
	{"__mul", "multiplication operator (*)"},
	{"__div", "division operator (/)"},
	{"__intdiv", "integer-division operator (//)"},
	{"__mod", "modulo operator (%)"},
	{"__pow", "exponentiation operator (**)"},
	{"__neg", "unary negation (-)"},
	{"__not", "logical-not operator (!)"},
	// Bitwise operators.
	{"__bitand", "bitwise-AND operator (&)"},
	{"__bitor", "bitwise-OR operator (|)"},
	{"__bitxor", "bitwise-XOR operator (^)"},
	{"__bitnot", "bitwise-NOT operator (~)"},
	{"__lshift", "left-shift operator (<<)"},
	{"__rshift", "right-shift operator (>>)"},
}

func lookupFunction(module, name string) (functionDoc, bool) {
	if module != "" {
		mod, ok := stdlibCatalog[module]
		if !ok {
			return functionDoc{}, false
		}
		fn, ok := mod.Functions[name]
		return fn, ok
	}
	for _, mod := range stdlibCatalog {
		if fn, ok := mod.Functions[name]; ok {
			return fn, true
		}
	}
	return functionDoc{}, false
}

func parameterInformation(params []string) []ParameterInformation {
	out := make([]ParameterInformation, len(params))
	for i, p := range params {
		out[i] = ParameterInformation{Label: p}
	}
	return out
}

func activeParameter(args string, max int) int {
	if max == 0 {
		return 0
	}
	depth := 0
	active := 0
	inString := rune(0)
	escaped := false
	for _, r := range args {
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == inString {
				inString = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			inString = r
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				active++
			}
		}
	}
	if active >= max {
		return max - 1
	}
	return active
}
