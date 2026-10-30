package lsp

import (
	"strings"
	"testing"
)

func TestPatchAPIsAppearInCompletionAndSignatureHelp(t *testing.T) {
	for _, tc := range []struct{ source, name, result string }{
		{"import cli;\ncli.", "promptDecimal", "?decimal"},
		{"import json;\njson.", "write", "int"},
		{"import money;\nmoney.", "sum", "Money"},
		{"string text = \"x\";\ntext.", "byteLength", "int"},
		{"import web.cache as cache;\ncache.", "memoryCacheStore", "MemoryCacheStore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := newTestServer()
			uri := "file:///tmp/patch-apis.gb"
			server.docs[uri] = tc.source
			line := strings.Split(tc.source, "\n")[1]
			items := server.completions(CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: len(line)}}})
			if !hasCompletion(items, tc.name) {
				t.Fatalf("completion missing %s: %+v", tc.name, items)
			}
			call := tc.source + tc.name + "("
			server.docs[uri] = call
			help := server.signatureHelp(SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: len(line) + len(tc.name) + 1}}})
			if len(help.Signatures) == 0 || !strings.Contains(help.Signatures[0].Label, "): "+tc.result) {
				t.Fatalf("signature missing return type: %+v", help)
			}
			if tc.name == "memoryCacheStore" && !strings.Contains(help.Signatures[0].Label, "capacity") {
				t.Fatalf("capacity missing from signature: %+v", help)
			}
		})
	}
}
