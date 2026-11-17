package semantic_test

import (
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

// A spread's argument count is only known at run time, so a typed context checks only that some overload returns a compatible type.
func TestOverloadedSpreadCallInTypedContext(t *testing.T) {
	const overloads = `func pick(int a): string { return "one"; } func pick(int a, int b): string { return "two"; } list<int> xs = [1, 2];`
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"declaration", overloads + ` string r = pick(...xs);`, true},
		{"return", overloads + ` func f(): string { return pick(...xs); }`, true},
		{"dict spread", overloads + ` string r = pick(...{"a": 1});`, true},
		{"no overload returns the type", overloads + ` int r = pick(...xs);`, false},
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
