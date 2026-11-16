package semantic_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func TestDestructuringRebindsDeletedNames(t *testing.T) {
	for name, source := range map[string]string{
		"list":                         `func f(): int { let value = 1; del value; let value, other = [2, 3]; return value; }`,
		"dict":                         `func f(): int { let value = 1; del value; let {value} = {"value": 2}; return value; }`,
		"assignment_rejects_destroyed": `func f(): void { let value = 1; del value; value, other = [2, 3]; }`,
	} {
		t.Run(name, func(t *testing.T) {
			p := parser.New(lexer.New(source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			diagnostics := semantic.New().Analyze(program)

			if name == "assignment_rejects_destroyed" {
				if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "destroyed binding") {
					t.Fatalf("diagnostics=%v", diagnostics)
				}
			} else if len(diagnostics) != 0 {
				t.Fatalf("diagnostics=%v", diagnostics)
			}
		})
	}
}
