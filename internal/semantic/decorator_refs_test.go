package semantic_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func analyzeDecoratorSource(t *testing.T, input string) []semantic.Diagnostic {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return semantic.New().Analyze(program)
}

func TestNestedDecoratorReferenceArgumentsAreRejected(t *testing.T) {
	diagnostics := analyzeDecoratorSource(t, `class User {}
func outer(): void {
    @Meta(schema: User)
    func inner(): void {}
    func withParam(@Route(f: (int n) => n) int x): void {}
    @Meta("literal")
    func fine(): void {}
}
`)
	var refs []semantic.Diagnostic
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "type and function arguments are only allowed") {
			refs = append(refs, d)
		}
	}
	if len(refs) != 2 || refs[0].Line != 3 || refs[1].Line != 5 {
		t.Fatalf("got %v, want diagnostics on lines 3 and 5", refs)
	}
}

func TestModuleLevelDecoratorReferenceArgumentsAreAccepted(t *testing.T) {
	diagnostics := analyzeDecoratorSource(t, `class User {}
@Meta(schema: User, f: (int n) => n)
class Svc {
    @Route(schema: User)
    func handle(@Param(User) string id): void {}
}
@Meta([User])
func top(): void {}
`)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "type and function arguments") {
			t.Fatalf("unexpected diagnostic: %s", d.Message)
		}
	}
}
