package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionMarksExportedTypeAliasAsClass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "facade.gb"), []byte("module facade;\n\nimport leaf;\n\nexport type Widget = leaf.Widget;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.gb")
	src := "import facade;\n\nlet w = facade."
	if err := os.WriteFile(mainPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + mainPath
	s := &server{docs: map[string]string{uri: src}}
	items := s.completions(CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 2, Character: 15},
	}})
	item, ok := findCompletion(items, "Widget")
	if !ok {
		t.Fatalf("expected Widget completion, got %#v", items)
	}
	if item.Kind != completionKindClass {
		t.Fatalf("Widget completion kind: got %d, want %d", item.Kind, completionKindClass)
	}
}
