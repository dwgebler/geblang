package semantic_test

import (
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func analyzeWith(t *testing.T, src string, setup func(*semantic.Analyzer)) []semantic.Diagnostic {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	a := semantic.New()
	if setup != nil {
		setup(a)
	}
	return a.Analyze(program)
}

func TestImportedCallArgumentsAreChecked(t *testing.T) {
	decimalParam := semantic.MethodSignature{Params: []*ast.TypeRef{{Name: "decimal"}}, Required: 1}
	ownType := semantic.MethodSignature{Params: []*ast.TypeRef{{Name: "Widget"}}, Required: 1}
	resolver := func(qualifier, name string) ([]semantic.MethodSignature, bool) {
		switch {
		case qualifier == "m" && name == "f":
			return []semantic.MethodSignature{decimalParam}, true
		case qualifier == "m" && name == "w":
			return []semantic.MethodSignature{ownType}, true
		case qualifier == "" && name == "g":
			return []semantic.MethodSignature{decimalParam}, true
		}
		return nil, false
	}
	src := `import m;
from n import g;
int n = 1;
any d = 1;
m.f(n);
m.f(2);
m.f(2.5);
m.f(d);
m.f(1, 2);
m.w("anything");
g("text");
`
	diags := analyzeWith(t, src, func(a *semantic.Analyzer) { a.SetModuleFunctionSignatureResolver(resolver) })
	var got []string
	for _, d := range diags {
		if strings.Contains(d.Message, "no matching overload") {
			got = append(got, d.Message)
		}
	}
	want := []string{
		"no matching overload for m.f: got (int)",
		"no matching overload for m.f: got (int, int)",
		"no matching overload for g: got (string)",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("diagnostics:\n got %q\nwant %q", got, want)
	}
}

func TestForInLoopVariableShadowsOuterFunction(t *testing.T) {
	src := `func t(int x): int { return x; }
list<string> tags = ["a"];
for (t in tags) {
    string s = t;
}
`
	for _, d := range analyzeWith(t, src, nil) {
		t.Errorf("unexpected diagnostic: %s", d.Message)
	}
}
