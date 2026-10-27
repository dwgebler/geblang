package lsp

import (
	"reflect"
	"unicode"
	"unicode/utf8"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func (s *server) references(params ReferenceParams) any {
	source, ok := s.document(params.TextDocument.URI)
	if !ok {
		return []Location{}
	}
	word := wordAtPosition(source, params.Position.Line, params.Position.Character)
	if word == "" {
		return []Location{}
	}
	return identifierOccurrences(params.TextDocument.URI, source, word, params.Position, false)
}

func identifierOccurrences(uri, source, word string, position Position, forRename bool) []Location {
	out := []Location{}
	p := parser.New(lexer.New(source))
	c := &bindingCollector{}
	c.walk(reflect.ValueOf(p.ParseProgram()), bindingContext{scope: span{endLine: int(^uint(0) >> 1)}})
	column := runeColumn(splitLine(source, position.Line), position.Character)
	qualifier := selectorQualifier(source, position.Line, position.Character)
	if qualifier != "" && qualifier != "this" {
		return out
	}
	target := resolveLocalBinding(c.bindings, position.Line, column, word, qualifier)
	if target == nil {
		return out
	}
	if forRename && (target.member || target.renameBlocked || c.ambiguous[word]) {
		return out
	}
	selected := false
	for _, tok := range referenceTokens(source) {
		if tok.Literal != word {
			continue
		}
		line := tok.Line - 1
		text := splitLine(source, line)
		col := tokenColumn(text, tok.Column-1)
		if line == position.Line && column >= tok.Column-1 && column <= tok.Column-1+utf8.RuneCountInString(word) {
			selected = true
		}
		q := selectorQualifier(source, line, col)
		if q != "" && q != "this" {
			continue
		}
		binding := resolveLocalBinding(c.bindings, line, tok.Column-1, word, q)
		if binding != nil && (*binding == *target || binding.function && target.function && binding.name == target.name && binding.scope == target.scope && binding.member == target.member) {
			out = append(out, Location{URI: uri, Range: Range{
				Start: Position{Line: line, Character: col},
				End:   Position{Line: line, Character: col + utf16Offset(word, len(word))},
			}})
		}
	}
	if !selected {
		return []Location{}
	}
	return out
}

func isWordBoundary(line string, start, length int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:start])
		if isIdentChar(r) {
			return false
		}
	}
	end := start + length
	if end < len(line) {
		r, _ := utf8.DecodeRuneInString(line[end:])
		if isIdentChar(r) {
			return false
		}
	}
	return true
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
