package formatter

import (
	"reflect"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func parseForTest(t *testing.T, src string) reflect.Value {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse %q: %v", src, errs)
	}
	return reflect.ValueOf(prog)
}

func TestASTEquivalentIgnoresLayoutOnly(t *testing.T) {
	a := parseForTest(t, "func f(int a): int { if (a > 1) { return a; } return 0; }")
	b := parseForTest(t, "func f(int a): int {\n    if ((a > 1)) {\n        return a;\n    }\n\n    return 0;\n}")
	if !astEquivalent(a, b) {
		t.Fatal("layout and redundant parens must not count as a difference")
	}
}

func TestASTEquivalentDetectsNodesLossyStringFormsHide(t *testing.T) {
	cases := [][2]string{
		{"interface I { string id; func a(): int; }", "interface I { func a(): int; }"},
		{"interface I { func a(): int { return 1; } }", "interface I { }"},
		{"class C { @trim string s; }", "class C { string s; }"},
		{"class C { func ~C() {} }", "class C { }"},
		{"enum S: string { A = \"a\" }", "enum S { A }"},
		{"try { a(); } catch (ValueError e) { b(); }", "try { a(); }"},
	}
	for _, c := range cases {
		if astEquivalent(parseForTest(t, c[0]), parseForTest(t, c[1])) {
			t.Errorf("expected a difference between %q and %q", c[0], c[1])
		}
	}
}

func TestVerifyRoundTripRefusesOutputThatDropsANode(t *testing.T) {
	p := parser.New(lexer.New("interface I { string id; }"))
	prog := p.ParseProgram()
	if err := verifyRoundTrip(prog, "interface I {}\n"); err == nil {
		t.Fatal("round-trip check accepted output that dropped an interface property")
	}
}
