package lsp

import (
	"geblang/internal/lexer"
	"geblang/internal/token"
)

// prepareRename validates that the cursor is on a renamable identifier.
// Returns the identifier's range or null when the cursor isn't on one.
func (s *server) prepareRename(params TextDocumentPositionParams) any {
	source, ok := s.document(params.TextDocument.URI)
	if !ok {
		return nil
	}
	word := wordAtPosition(source, params.Position.Line, params.Position.Character)
	if word == "" {
		return nil
	}
	if len(identifierOccurrences(params.TextDocument.URI, source, word, params.Position, true)) == 0 {
		return nil
	}
	col := columnOfWord(source, params.Position.Line, params.Position.Character, word)
	if col < 0 {
		return nil
	}
	return Range{
		Start: Position{Line: params.Position.Line, Character: col},
		End:   Position{Line: params.Position.Line, Character: col + utf16Offset(word, len(word))},
	}
}

func (s *server) rename(params RenameParams) any {
	source, ok := s.document(params.TextDocument.URI)
	if !ok {
		return nil
	}
	word := wordAtPosition(source, params.Position.Line, params.Position.Character)
	if word == "" || params.NewName == "" || params.NewName == word {
		return nil
	}
	name := lexer.New(params.NewName)
	if tok := name.NextToken(); tok.Type != token.Ident || tok.Literal != params.NewName || name.NextToken().Type != token.EOF {
		return nil
	}
	locations := identifierOccurrences(params.TextDocument.URI, source, word, params.Position, true)
	if len(locations) == 0 {
		return nil
	}
	edits := make([]TextEdit, 0, len(locations))
	for _, loc := range locations {
		edits = append(edits, TextEdit{Range: loc.Range, NewText: params.NewName})
	}
	return WorkspaceEdit{Changes: map[string][]TextEdit{params.TextDocument.URI: edits}}
}

// columnOfWord finds the starting character index of `word` containing
// the cursor at (line, character). Returns -1 if not found.
func columnOfWord(source string, line, character int, word string) int {
	lineText := splitLine(source, line)
	if lineText == "" {
		return -1
	}
	character = byteOffset(lineText, character)
	start := 0
	for {
		idx := indexFrom(lineText, word, start)
		if idx < 0 {
			return -1
		}
		end := idx + len(word)
		if character >= idx && character <= end && isWordBoundary(lineText, idx, len(word)) {
			return utf16Offset(lineText, idx)
		}
		start = end
	}
}

func splitLine(source string, line int) string {
	idx := 0
	for line > 0 && idx < len(source) {
		nl := indexFrom(source, "\n", idx)
		if nl < 0 {
			return ""
		}
		idx = nl + 1
		line--
	}
	end := indexFrom(source, "\n", idx)
	if end < 0 {
		end = len(source)
	}
	return source[idx:end]
}

func indexFrom(s, sub string, start int) int {
	if start >= len(s) {
		return -1
	}
	idx := indexOf(s[start:], sub)
	if idx < 0 {
		return -1
	}
	return start + idx
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
