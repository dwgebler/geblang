package parser_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestParserRejectsEOFBeforeClosingBodyBrace(t *testing.T) {
	for _, source := range []string{
		`class Broken {`,
		`class Broken { int value;`,
		`class Broken { func value(): int { return 1; }`,
		`class Broken { func value(): int { return 1;`,
		`interface Broken {`,
		`interface Broken { func value(): int;`,
		`enum Broken {`,
		`enum Broken { One, Two`,
		`enum Broken { One; func value(): int { return 1; }`,
		`func broken(): void {`,
		`if (true) {`,
		`select {`,
		`match (1) {`,
		`match (1) { case 1 => 2;`,
	} {
		t.Run(source, func(t *testing.T) {
			p := parser.New(lexer.New(source))

			p.ParseProgram()

			if !strings.Contains(strings.Join(p.Errors(), "\n"), "expected } before end of input") {
				t.Fatalf("missing closing-brace error: %v", p.Errors())
			}
		})
	}
}
