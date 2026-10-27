package lsp

import "testing"

func TestRenameKeepsAnOverloadGroupTogether(t *testing.T) {
	s, _ := newTestServer()
	uri := "file:///tmp/overloads.gb"
	s.docs[uri] = "func choose(int n): int { return n; }\nfunc choose(string n): string { return n; }\nio.println(choose(1));\nio.println(choose('s'));\n"

	result := s.rename(RenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: 6},
	}, NewName: "selectValue"})

	edits := result.(WorkspaceEdit).Changes[uri]
	if len(edits) != 4 {
		t.Fatalf("incomplete overload rename: %+v", edits)
	}
}
