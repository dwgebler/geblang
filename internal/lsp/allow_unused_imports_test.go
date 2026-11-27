package lsp

import (
	"strings"
	"testing"
)

func TestDiagnosticsHonourAllowUnusedImportsTag(t *testing.T) {
	s, _ := newTestServer()
	tagged := "/** @allowUnusedImports */\nimport json;\nlet x = 1;\n"
	untagged := "import json;\nlet x = 1;\n"

	taggedDiags := s.analyze("file:///tagged.gb", tagged)
	untaggedDiags := s.analyze("file:///untagged.gb", untagged)

	if mentionsUnusedImport(taggedDiags) {
		t.Fatalf("tagged file reported an unused import: %+v", taggedDiags)
	}
	if !mentionsUnusedImport(untaggedDiags) {
		t.Fatalf("untagged file did not report the unused import: %+v", untaggedDiags)
	}
}

func mentionsUnusedImport(diags []Diagnostic) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, "import json is not used") {
			return true
		}
	}
	return false
}
