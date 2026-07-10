package lsp

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const (
	symbolKindFunction  = 12
	symbolKindClass     = 5
	symbolKindInterface = 11
	symbolKindEnum      = 10
	symbolKindVariable  = 13
	symbolKindConstant  = 14
)

// DocumentSymbol is an LSP DocumentSymbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// userSymbol is a lightweight extracted symbol used for completions/hover/definition.
type userSymbol struct {
	name   string
	kind   int
	line   int // 1-based
	detail string
	params []string // function parameter labels, nil for non-functions
}

// extractSymbols parses source and returns top-level user-defined symbols.
func extractSymbols(source string) []userSymbol {
	p := parser.New(lexer.New(source))
	prog := p.ParseProgram()
	var syms []userSymbol
	for _, stmt := range prog.Statements {
		syms = append(syms, symbolsFromStatement(stmt)...)
	}
	return syms
}

func symbolsFromStatement(stmt ast.Statement) []userSymbol {
	switch s := stmt.(type) {
	case *ast.FunctionStatement:
		if s.Name == nil {
			return nil
		}
		return []userSymbol{{name: s.Name.Value, kind: symbolKindFunction, line: s.Token.Line, detail: buildFuncDetail(s), params: funcParamLabels(s)}}

	case *ast.ClassStatement:
		if s.Name == nil {
			return nil
		}
		return []userSymbol{{name: s.Name.Value, kind: symbolKindClass, line: s.Token.Line, detail: "class " + s.Name.Value}}

	case *ast.InterfaceStatement:
		if s.Name == nil {
			return nil
		}
		return []userSymbol{{name: s.Name.Value, kind: symbolKindInterface, line: s.Token.Line, detail: "interface " + s.Name.Value}}

	case *ast.EnumStatement:
		if s.Name == nil {
			return nil
		}
		return []userSymbol{{name: s.Name.Value, kind: symbolKindEnum, line: s.Token.Line, detail: "enum " + s.Name.Value}}

	case *ast.DeclarationStatement:
		if s.Name == nil {
			return nil
		}
		kind := symbolKindVariable
		if s.Kind == "const" {
			kind = symbolKindConstant
		}
		return []userSymbol{{name: s.Name.Value, kind: kind, line: s.Token.Line, detail: s.Kind + " " + s.Name.Value}}

	case *ast.ExportStatement:
		if s.Statement != nil {
			return symbolsFromStatement(s.Statement)
		}
	}
	return nil
}

func buildFuncDetail(s *ast.FunctionStatement) string {
	var sb strings.Builder
	if s.Async {
		sb.WriteString("async ")
	}
	sb.WriteString("func ")
	sb.WriteString(s.Name.Value)
	sb.WriteByte('(')
	sb.WriteString(strings.Join(funcParamLabels(s), ", "))
	sb.WriteByte(')')
	if s.ReturnType != nil {
		sb.WriteString(": ")
		sb.WriteString(s.ReturnType.String())
	}
	return sb.String()
}

func funcParamLabels(s *ast.FunctionStatement) []string {
	labels := make([]string, 0, len(s.Parameters))
	for _, p := range s.Parameters {
		var sb strings.Builder
		if p.Variadic {
			sb.WriteString("...")
		}
		if p.Name != nil {
			sb.WriteString(p.Name.Value)
		}
		if p.Type != nil {
			sb.WriteString(": ")
			sb.WriteString(p.Type.String())
		}
		labels = append(labels, sb.String())
	}
	return labels
}

// documentSymbols converts user symbols to LSP DocumentSymbol format.
func documentSymbols(source string) []DocumentSymbol {
	syms := extractSymbols(source)
	lines := strings.Split(source, "\n")
	out := make([]DocumentSymbol, 0, len(syms))
	for _, sym := range syms {
		r := lineRange(sym.line, lines)
		sel := nameRange(sym.line, sym.name, lines)
		out = append(out, DocumentSymbol{
			Name:           sym.name,
			Kind:           sym.kind,
			Range:          r,
			SelectionRange: sel,
		})
	}
	return out
}

// lineRange returns the full-line range for line n (1-based).
func lineRange(line int, lines []string) Range {
	if line < 1 || line > len(lines) {
		return Range{}
	}
	l := line - 1
	return Range{
		Start: Position{Line: l, Character: 0},
		End:   Position{Line: l, Character: len(lines[l])},
	}
}

// nameRange returns the range covering the symbol name on its line.
func nameRange(line int, name string, lines []string) Range {
	if line < 1 || line > len(lines) {
		return Range{}
	}
	l := line - 1
	text := lines[l]
	col := strings.Index(text, name)
	if col < 0 {
		col = 0
	}
	return Range{
		Start: Position{Line: l, Character: col},
		End:   Position{Line: l, Character: col + len(name)},
	}
}

// wordAtPosition returns the identifier word at the given position.
func wordAtPosition(source string, line, char int) string {
	lines := strings.Split(source, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	text := lines[line]
	if char < 0 || char > len(text) {
		return ""
	}
	// Expand left
	start := char
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	// Expand right
	end := char
	for end < len(text) && isIdentByte(text[end]) {
		end++
	}
	return text[start:end]
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// findDefinition returns the line (0-based) of the definition of name in source, or -1.
func findDefinition(source, name string) int {
	syms := extractSymbols(source)
	for _, sym := range syms {
		if sym.name == name {
			return sym.line - 1 // convert to 0-based
		}
	}
	return -1
}

// hoverContent returns a markdown hover string for the word at the given position.
func hoverContent(source string, line, char int) string {
	word := wordAtPosition(source, line, char)
	if word == "" {
		return ""
	}

	// Check user-defined symbols first
	syms := extractSymbols(source)
	for _, sym := range syms {
		if sym.name == word {
			return "```geblang\n" + sym.detail + "\n```"
		}
	}

	// Check stdlib catalog
	for modName, mod := range stdlibCatalog {
		if modName == word {
			return "**" + word + "** - stdlib module"
		}
		if fn, ok := mod.Functions[word]; ok {
			return "```geblang\n" + fn.Signature() + "\n```\n\n" + fn.Doc
		}
		if doc, ok := mod.Classes[word]; ok {
			return "```geblang\n" + word + "\n```\n\n" + doc
		}
	}

	if fn, ok := globalBuiltinDoc(word); ok {
		fn.Name = word
		return "```geblang\n" + fn.Signature() + "\n```\n\n" + fn.Doc
	}

	return ""
}
