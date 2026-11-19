package embedfold_test

import (
	"strings"
	"testing"

	"geblang/internal/embedfold"
)

func TestDeclaringEmbedIsRejected(t *testing.T) {
	dir := t.TempDir()
	rejected := []string{
		"func embed(int x): int { return x; }",
		"let embed = 1;",
		"func f(int embed): int { return 1; }",
		"for (embed in [1]) {}",
		"let [embed, b] = [1, 2];",
		"from json import parse as embed;",
		"class C { func C() {} func m(int embed): void {} }",
	}
	for _, src := range rejected {
		_, _, diags := foldSource(t, dir, src, embedfold.Validate)
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "embed is reserved for the compile-time embed builtin") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want a reserved-name diagnostic, got %v", src, diags)
		}
	}
}

func TestEmbedMembersAreAllowed(t *testing.T) {
	dir := t.TempDir()
	src := `interface Embedder { func embed(string t): int; }
class E implements Embedder {
    int embed = 0;
    func E() {}
    func embed(string t): int { return t.length(); }
}
let n = E().embed("abc");
`
	_, _, diags := foldSource(t, dir, src, embedfold.Validate)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
