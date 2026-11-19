package semantic_test

import (
	"strings"
	"testing"
)

func TestQualifiedParentsNeedAnImport(t *testing.T) {
	rejected := map[string]string{
		"class T extends test.Test {}\n":                            `module "test" is used without an import`,
		"class E implements rag.Embedder {}\n":                      `module "rag" is used without an import`,
		"interface I extends rag.Embedder {}\n":                     `module "rag" is used without an import`,
		"enum Color implements shapes.Named { Red }\n":              `module "shapes" is used without an import`,
		"import async;\nclass C extends async.channel.Channel {}\n": `module "async.channel" is used without an import`,
	}
	for src, want := range rejected {
		found := false
		for _, d := range analyzeWith(t, src, nil) {
			if strings.Contains(d.Message, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want %q", src, want)
		}
	}
	accepted := []string{
		"import test;\nclass T extends test.Test {}\n",
		"import rag as r;\nclass E implements r.Embedder {}\n",
		"import async.channel;\nclass C extends async.channel.Channel {}\n",
		"import async.channel;\nclass C extends channel.Channel {}\n",
	}
	for _, src := range accepted {
		for _, d := range analyzeWith(t, src, nil) {
			if strings.Contains(d.Message, "used without an import") {
				t.Errorf("%q: unexpected %v", src, d)
			}
		}
	}
}
