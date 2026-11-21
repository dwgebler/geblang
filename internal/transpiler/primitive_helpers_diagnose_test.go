package transpiler_test

import (
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/transpiler"
	"geblang/internal/transpiler/types"
)

func nativeErrors(t *testing.T, body string) []string {
	t.Helper()
	src := "import io;\nexport func main() {\n" + body + "\n}"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parse: %s", strings.Join(errs, "; "))
	}
	_, diags, err := transpiler.Transpile(transpiler.Input{
		Modules: map[string]*ast.Program{"main": prog},
	}, transpiler.Options{EntryModule: "main", IntMode: types.IntModeFast})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	var out []string
	for _, d := range diags {
		if d.Severity == transpiler.SeverityError {
			out = append(out, d.Message)
		}
	}
	return out
}

func TestUnloweredPrimitiveHelpersDiagnose(t *testing.T) {
	cases := map[string]string{
		"slugify":   `io.println("Caf\u{e9}".slugify());`,
		"isSubset":  `io.println({1}.isSubsetOf({1, 2}));`,
		"pick":      `io.println({"a": 1}.pick(["a"]));`,
		"mapValues": `io.println({"a": 1}.mapValues((int v) => v + 1));`,
		"isBetween": `io.println(5.isBetween(1, 9));`,
		"isCloseTo": `io.println((1.0f).isCloseTo(1.0f));`,
	}
	for name, body := range cases {
		if errs := nativeErrors(t, body); len(errs) == 0 {
			t.Errorf("%s: expected a transpile diagnostic, got none", name)
		}
	}
}
