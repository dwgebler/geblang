package semantic_test

import (
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func TestExplicitGenericCallUsesBoundSignatureInTypedContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"declaration", `func id<T>(T value): T { return value; } int n = id<int>(17);`, true},
		{"callback", `func id<T>(T value): T { return value; } let f = func(): int { return id<int>(17); };`, true},
		{"collection", `func id<T>(T value): T { return value; } func f(): list<int> { return id<list<int>>([1]); }`, true},
		{"nullable", `func id<T>(T value): ?T { return value; } func f(): ?int { return id<int>(17); }`, true},
		{"wrong return", `func id<T>(T value): T { return value; } func f(): int { return id<string>("x"); }`, false},
		{"wrong argument", `func id<T>(T value): T { return value; } int n = id<int>("x");`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			diagnostics := semantic.New().Analyze(program)

			if (len(diagnostics) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, diagnostics)
			}
		})
	}
}
