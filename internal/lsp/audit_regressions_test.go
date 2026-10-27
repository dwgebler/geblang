package lsp

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRenameOnlyEditsTheSelectedBinding(t *testing.T) {
	s, _ := newTestServer()
	uri := "file:///tmp/rename.gb"
	s.docs[uri] = "int value = 1;\nfunc other(): void { int value = 2; io.println(value); }\nio.println(\"value\"); /* value */\nio.println(value);\n"

	result := s.rename(RenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: 5},
	}, NewName: "renamed"})

	edits := result.(WorkspaceEdit).Changes[uri]
	if len(edits) != 2 || edits[0].Range.Start.Line != 0 || edits[1].Range.Start.Line != 3 {
		t.Fatalf("rename edits unrelated text: %+v", edits)
	}
	if got := s.prepareRename(TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: 13}}); got != nil {
		t.Fatalf("rename in string should be unavailable: %+v", got)
	}
}

func TestRenameDoesNotBindAnUnresolvedMemberToALocalVariable(t *testing.T) {
	s, _ := newTestServer()
	uri := "file:///tmp/member.gb"
	line := "io.println(box.value);"
	s.docs[uri] = "int value = 1;\nlet box = {\"value\": 2};\n" + line + "\n"

	result := s.rename(RenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: strings.Index(line, "value")},
	}, NewName: "renamed"})

	if result != nil {
		t.Fatalf("unresolved member renamed an unrelated variable: %+v", result)
	}
}

func TestRenameRejectsRefactorsWhoseRequiredEditsAreNotResolved(t *testing.T) {
	for _, source := range []string{
		"class Box { func Box() {} }\nlet b = Box();\n",
		"func f(int value): int { return value; }\nio.println(f(value: 1));\n",
		"int value = 1;\nfunc f(): int { int value = value + 1; return value; }\n",
	} {
		s, _ := newTestServer()
		uri := "file:///tmp/incomplete-refactor.gb"
		s.docs[uri] = source
		name := "Box"
		if strings.Contains(source, "value") {
			name = "value"
		}
		params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: strings.Index(source, name)}}

		if got := s.prepareRename(params); got != nil {
			t.Fatalf("unsafe rename offered for %s: %+v", name, got)
		}
		if got := s.rename(RenameParams{TextDocumentPositionParams: params, NewName: "renamed"}); got != nil {
			t.Fatalf("incomplete edits for %s: %+v", name, got)
		}
	}
}

func TestNavigationAndRenameUseUTF16Positions(t *testing.T) {
	s, _ := newTestServer()
	uri := "file:///tmp/unicode.gb"
	line := `io.println("` + "\u00e9\U0001f600\u00e9" + `"); io.println(value);`
	s.docs[uri] = "int value = 1;\n" + line + "\n"
	column := utf16Offset(line, strings.Index(line, "value"))
	params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: column + 2}}

	definition := s.definition(params)
	result := s.rename(RenameParams{TextDocumentPositionParams: params, NewName: "count"})

	if location, ok := definition.(Location); !ok || location.Range.Start != (Position{Line: 0, Character: 4}) {
		t.Fatalf("definition = %#v", definition)
	}
	edits := result.(WorkspaceEdit).Changes[uri]
	if len(edits) != 2 || edits[1].Range.Start.Character != column {
		t.Fatalf("rename has incorrect UTF-16 range: %+v", edits)
	}
}

func TestRenameIncludesInterpolationExpressionsWithoutChangingLiteralText(t *testing.T) {
	s, _ := newTestServer()
	uri := "file:///tmp/interpolation.gb"
	line := `io.println("value ${value} ${value:>5} ${true ? value : 0}");`
	s.docs[uri] = "int value = 1;\n" + line + "\n"

	result := s.rename(RenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: 5},
	}, NewName: "count"})

	edits := result.(WorkspaceEdit).Changes[uri]
	if len(edits) != 4 {
		t.Fatalf("expected declaration and three interpolation references: %+v", edits)
	}
	for _, edit := range edits[1:] {
		if edit.Range.Start.Character <= strings.Index(line, "value") {
			t.Fatalf("renamed literal text: %+v", edit)
		}
		if line[edit.Range.Start.Character:edit.Range.End.Character] != "value" {
			t.Fatalf("misplaced interpolation edit: %+v", edit)
		}
	}
}

func TestExitNotificationEndsServerWithInputStillOpen(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	done := make(chan error, 1)
	go func() { done <- Serve(r, io.Discard) }()
	body := `{"jsonrpc":"2.0","method":"exit"}`

	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("exit waited for stdin to close")
	}
}
