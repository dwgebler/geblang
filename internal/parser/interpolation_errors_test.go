package parser_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestInterpolationParseErrorsAreReported(t *testing.T) {
	cases := map[string]string{
		`"${1 +}";`:                            `invalid interpolation ${1 +}: expected expression, got EOF`,
		`"${x )}";`:                            `invalid interpolation ${x )}: unexpected ) after expression`,
		`"a${}b";`:                             `invalid interpolation ${}: expected expression, got EOF`,
		`"${func(int x): int { return x; }}";`: `invalid interpolation ${func(int x)}: expected next token to be {, got EOF (a top-level ':' starts a format spec; parenthesize the expression)`,
	}
	for source, want := range cases {
		p := parser.New(lexer.New(source))
		p.ParseProgram()
		if got := strings.Join(p.Errors(), "\n"); !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want %q", source, got, want)
		}
	}
}

func TestValidInterpolationsParseClean(t *testing.T) {
	for _, source := range []string{
		`"${x}|${x:>4}|${x > 1 ? "a" : "b"}|${(x > 1 ? x : 0):03d}";`,
		`"${(func(int x): int { return x; })(4)}";`,
		`"${ {"k": 1}["k"] }|${[1, 2][0]}";`,
	} {
		p := parser.New(lexer.New(source))
		p.ParseProgram()
		if errs := p.Errors(); len(errs) != 0 {
			t.Errorf("%s: unexpected errors %v", source, errs)
		}
	}
}
