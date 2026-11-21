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

func TestDecoratorReferenceArgumentsDiagnoseUnderNative(t *testing.T) {
	for _, arg := range []string{"schema: User", "f: (int n) => n", "list: [User]"} {
		src := "class User {}\n@Meta(" + arg + ")\nclass Svc {\n    @Meta(" + arg + ")\n    func m(): void {}\n}\n@Meta(" + arg + ")\nfunc work(): void {}\n"
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
		found := 0
		for _, d := range diags {
			if d.Severity == transpiler.SeverityError && strings.Contains(d.Message, "type and function arguments are not supported by --native") {
				found++
			}
		}
		if found != 3 {
			t.Fatalf("%s: expected 3 decorator diagnostics, got: %v", arg, diags)
		}
	}
}
