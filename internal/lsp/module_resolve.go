package lsp

import (
	"os"
	"sort"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// fileImports is a document's import surface: module aliases plus from-imported names.
type fileImports struct {
	aliases map[string]string     // local alias -> canonical module name
	from    map[string]fromImport // local name -> defining module + original name
}

type fromImport struct {
	canonical string
	name      string
}

func parseFileImports(source string) fileImports {
	out := fileImports{aliases: map[string]string{}, from: map[string]fromImport{}}
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if program == nil {
		return out
	}
	for _, stmt := range program.Statements {
		switch imp := stmt.(type) {
		case *ast.ImportStatement:
			alias := imp.ModuleName()
			canonical := strings.Join(imp.Path, ".")
			if alias != "" && canonical != "" {
				out.aliases[alias] = canonical
			}
		case *ast.FromImportStatement:
			canonical := strings.Join(imp.Path, ".")
			if canonical == "" {
				continue
			}
			for _, item := range imp.Names {
				local := item.Local()
				if local == "" || item.Name == nil {
					continue
				}
				out.from[local] = fromImport{canonical: canonical, name: item.Name.Value}
			}
		}
	}
	return out
}

// selectorQualifier returns the identifier left of the `.` preceding the word at (line, char), or "".
func selectorQualifier(source string, line, char int) string {
	lines := strings.Split(source, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	text := lines[line]
	if char < 0 || char > len(text) {
		return ""
	}
	start := char
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	if start == 0 || text[start-1] != '.' {
		return ""
	}
	end := start - 1
	qStart := end
	for qStart > 0 && isIdentByte(text[qStart-1]) {
		qStart--
	}
	return text[qStart:end]
}

// moduleSymbolHit is a symbol resolved into an imported project module.
type moduleSymbolHit struct {
	canonical string
	path      string
	source    string
	sym       userSymbol
}

// moduleSymbol resolves `qualifier.word` (or a from-imported bare word) to its defining project-module symbol.
func (s *server) moduleSymbol(file, source, qualifier, word string) (moduleSymbolHit, bool) {
	imports := parseFileImports(source)
	canonical, target := "", word
	if qualifier != "" {
		c, ok := imports.aliases[qualifier]
		if !ok {
			return moduleSymbolHit{}, false
		}
		canonical = c
	} else {
		fi, ok := imports.from[word]
		if !ok {
			return moduleSymbolHit{}, false
		}
		canonical, target = fi.canonical, fi.name
	}
	path, modSource, ok := s.resolveModuleSource(file, canonical)
	if !ok {
		return moduleSymbolHit{}, false
	}
	for _, sym := range extractSymbols(modSource) {
		if sym.name == target {
			return moduleSymbolHit{canonical: canonical, path: path, source: modSource, sym: sym}, true
		}
	}
	return moduleSymbolHit{}, false
}

// resolveModuleSource maps a canonical module name to its file and text, preferring an open editor buffer.
func (s *server) resolveModuleSource(file, canonical string) (string, string, bool) {
	resolver := s.resolverForFile(file)
	if resolver == nil {
		return "", "", false
	}
	path, err := resolver.Resolve(canonical)
	if err != nil {
		return "", "", false
	}
	if source, ok := s.document(pathToURI(path)); ok {
		return path, source, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	return path, string(data), true
}

// moduleAliasTarget resolves a bare identifier naming an imported project module to its file.
func (s *server) moduleAliasTarget(file, source, word string) (path, canonical string, ok bool) {
	imports := parseFileImports(source)
	canonical, found := imports.aliases[word]
	if !found {
		return "", "", false
	}
	path, _, ok = s.resolveModuleSource(file, canonical)
	if !ok {
		return "", "", false
	}
	return path, canonical, true
}

func moduleSymbolLocation(hit moduleSymbolHit) Location {
	lines := strings.Split(hit.source, "\n")
	return Location{URI: pathToURI(hit.path), Range: nameRange(hit.sym.line, hit.sym.name, lines)}
}

func moduleSymbolHover(hit moduleSymbolHit) string {
	return "```geblang\n" + hit.sym.detail + "\n```\n\nDefined in `" + hit.canonical + "`"
}

// moduleHover renders import-resolved hover; empty string defers to the legacy hover path.
func (s *server) moduleHover(file, source string, line, char int) string {
	word := wordAtPosition(source, line, char)
	if word == "" {
		return ""
	}
	qualifier := selectorQualifier(source, line, char)
	if qualifier != "" {
		if mod, ok := stdlibCatalog[qualifier]; ok {
			if fn, ok := mod.Functions[word]; ok {
				return "```geblang\n" + fn.Signature() + "\n```\n\n" + fn.Doc
			}
			if doc, ok := mod.Classes[word]; ok {
				return "```geblang\n" + word + "\n```\n\n" + doc
			}
		}
		if hit, ok := s.moduleSymbol(file, source, qualifier, word); ok {
			return moduleSymbolHover(hit)
		}
		return ""
	}
	if findDefinition(source, word) >= 0 {
		return "" // local symbol; the legacy path renders it
	}
	if hit, ok := s.moduleSymbol(file, source, "", word); ok {
		return moduleSymbolHover(hit)
	}
	if _, canonical, ok := s.moduleAliasTarget(file, source, word); ok {
		return "**" + canonical + "** - project module"
	}
	return ""
}

// userModuleCompletionItems lists an imported project module's visible members for `alias.` completion.
func (s *server) userModuleCompletionItems(file, source, alias string) ([]CompletionItem, bool) {
	imports := parseFileImports(source)
	canonical, ok := imports.aliases[alias]
	if !ok {
		return nil, false
	}
	_, modSource, ok := s.resolveModuleSource(file, canonical)
	if !ok {
		return nil, false
	}
	items := []CompletionItem{}
	for _, sym := range extractSymbols(modSource) {
		if strings.HasPrefix(sym.name, "_") {
			continue
		}
		kind := completionKindFunction
		switch sym.kind {
		case symbolKindClass, symbolKindInterface:
			kind = completionKindClass
		case symbolKindEnum:
			kind = completionKindEnum
		case symbolKindVariable, symbolKindConstant:
			kind = 6 // variable kind in LSP CompletionItemKind
		}
		items = append(items, CompletionItem{Label: sym.name, Kind: kind, Detail: sym.detail})
	}
	if len(items) == 0 {
		return nil, false
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items, true
}

// userModuleSignatureHelp resolves signature help for calls to imported project-module functions.
func (s *server) userModuleSignatureHelp(file, source, module, name, argPrefix string) (SignatureHelp, bool) {
	hit, ok := s.moduleSymbol(file, source, module, name)
	if !ok || hit.sym.kind != symbolKindFunction {
		return SignatureHelp{}, false
	}
	return SignatureHelp{
		Signatures: []SignatureInformation{{
			Label:      hit.sym.detail,
			Parameters: parameterInformation(hit.sym.params),
		}},
		ActiveSignature: 0,
		ActiveParameter: activeParameter(argPrefix, len(hit.sym.params)),
	}, true
}
