package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const moduleFixtureSource = `module app.foo;

export func bar(int x): string {
    return "${x}";
}

func _hidden(): void {
}

export class Widget {
}
`

// writeModuleFixture creates <root>/app/foo.gb and returns both paths.
func writeModuleFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	modPath := filepath.Join(dir, "foo.gb")
	if err := os.WriteFile(modPath, []byte(moduleFixtureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, modPath
}

func moduleTestServer(t *testing.T, root, mainSource string) (*server, string) {
	t.Helper()
	s, _ := newTestServer()
	s.workspaceRoots = []string{root}
	uri := pathToURI(filepath.Join(root, "main.gb"))
	s.docs[uri] = mainSource
	return s, uri
}

func TestDefinitionResolvesImportedModuleMember(t *testing.T) {
	root, modPath := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 9},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.URI != pathToURI(modPath) {
		t.Fatalf("definition URI: got %q want %q", loc.URI, pathToURI(modPath))
	}
	if loc.Range.Start.Line != 2 || loc.Range.Start.Character != 12 {
		t.Fatalf("definition range: got %+v want line 2 char 12", loc.Range)
	}
}

func TestDefinitionResolvesAliasedImport(t *testing.T) {
	root, modPath := writeModuleFixture(t)
	source := "import app.foo as f;\n\nfunc main(): void {\n    f.bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 7},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.URI != pathToURI(modPath) || loc.Range.Start.Line != 2 {
		t.Fatalf("aliased definition: got %+v", loc)
	}
}

func TestDefinitionResolvesFromImportMember(t *testing.T) {
	root, modPath := writeModuleFixture(t)
	source := "from app.foo import bar;\n\nfunc main(): void {\n    bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 5},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.URI != pathToURI(modPath) || loc.Range.Start.Line != 2 {
		t.Fatalf("from-import definition: got %+v", loc)
	}
}

func TestDefinitionOnModuleAliasJumpsToModuleFile(t *testing.T) {
	root, modPath := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 5},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.URI != pathToURI(modPath) || loc.Range.Start.Line != 0 {
		t.Fatalf("module alias definition: got %+v", loc)
	}
}

func TestDefinitionPrefersLocalSymbolForUnqualifiedName(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc bar(): void {\n}\n\nfunc main(): void {\n    bar();\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 6, Character: 5},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.URI != uri || loc.Range.Start.Line != 2 {
		t.Fatalf("local definition should win: got %+v", loc)
	}
}

func TestDefinitionUsesOpenBufferForModuleFile(t *testing.T) {
	root, modPath := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	// Unsaved edit in the module buffer moves bar down one line.
	s.docs[pathToURI(modPath)] = "module app.foo;\n\n\nexport func bar(int x): string {\n    return \"${x}\";\n}\n"
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 9},
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %T", result)
	}
	if loc.Range.Start.Line != 3 {
		t.Fatalf("expected open-buffer line 3, got %+v", loc.Range)
	}
}

func TestHoverShowsImportedModuleMemberSignature(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.hover(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 9},
	})
	hov, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected HoverResult, got %T", result)
	}
	if !strings.Contains(hov.Contents.Value, "func bar(x: int): string") {
		t.Fatalf("hover missing signature: %q", hov.Contents.Value)
	}
	if !strings.Contains(hov.Contents.Value, "app.foo") {
		t.Fatalf("hover missing defining module: %q", hov.Contents.Value)
	}
}

func TestHoverShowsFromImportedMemberSignature(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "from app.foo import bar;\n\nfunc main(): void {\n    bar(1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.hover(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 5},
	})
	hov, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected HoverResult, got %T", result)
	}
	if !strings.Contains(hov.Contents.Value, "func bar(x: int): string") {
		t.Fatalf("hover missing signature: %q", hov.Contents.Value)
	}
}

func TestHoverKeepsCatalogDocsForNativeModules(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "import math;\n\nfunc main(): void {\n    math.abs(-1);\n}\n"
	s, uri := moduleTestServer(t, root, source)
	result := s.hover(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: 10},
	})
	hov, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected HoverResult, got %T", result)
	}
	if !strings.Contains(hov.Contents.Value, "abs(") {
		t.Fatalf("native hover lost catalog docs: %q", hov.Contents.Value)
	}
}

func TestCompletionListsUserModuleMembers(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.\n}\n"
	s, uri := moduleTestServer(t, root, source)
	items := s.completions(CompletionParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     Position{Line: 3, Character: 8},
		},
	})
	var gotBar, gotWidget, gotHidden bool
	for _, item := range items {
		switch item.Label {
		case "bar":
			gotBar = true
			if !strings.Contains(item.Detail, "func bar(x: int): string") {
				t.Fatalf("bar completion detail: %q", item.Detail)
			}
		case "Widget":
			gotWidget = true
		case "_hidden":
			gotHidden = true
		}
	}
	if !gotBar || !gotWidget {
		t.Fatalf("expected bar and Widget completions, got %+v", items)
	}
	if gotHidden {
		t.Fatalf("_hidden must not be offered (not exported)")
	}
}

func TestSignatureHelpForUserModuleFunction(t *testing.T) {
	root, _ := writeModuleFixture(t)
	source := "import app.foo;\n\nfunc main(): void {\n    foo.bar(\n}\n"
	s, uri := moduleTestServer(t, root, source)
	help := s.signatureHelp(SignatureHelpParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     Position{Line: 3, Character: 12},
		},
	})
	if len(help.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %+v", help)
	}
	sig := help.Signatures[0]
	if !strings.Contains(sig.Label, "func bar(x: int): string") {
		t.Fatalf("signature label: %q", sig.Label)
	}
	if len(sig.Parameters) != 1 || sig.Parameters[0].Label != "x: int" {
		t.Fatalf("signature parameters: %+v", sig.Parameters)
	}
}
