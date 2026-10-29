package semantic_test

import (
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func TestSourceClassRelations_UsesDefiningModuleAndRejectsUnrelatedClasses(t *testing.T) {
	parse := func(source string) *ast.Program {
		t.Helper()
		p := parser.New(lexer.New(source))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatal(p.Errors())
		}
		return program
	}
	modules := map[string]*ast.Program{
		"base":  parse(`module base; export class Root {} export class Middle extends Root {}`),
		"other": parse(`module other; export class Root {}`),
		"child": parse(`module child; from base import Middle as Parent; export class Child extends Parent {}`),
	}
	root := parse(`import base as b; import other as o; import child as c; class Leaf extends c.Child {}`)
	relations := semantic.SourceClassRelations(root, "", func(name string) (*ast.Program, bool) { p, ok := modules[name]; return p, ok })

	for _, tc := range []struct {
		target, actual string
		assignable     bool
	}{
		{"b.Root", "Leaf", true}, {"b.Middle", "c.Child", true}, {"b.Root", "o.Root", false}, {"o.Root", "Leaf", false}, {"c.Child", "b.Root", false},
	} {
		t.Run(tc.target+"_"+tc.actual, func(t *testing.T) {
			assignable, known := relations(tc.target, tc.actual)
			if !known || assignable != tc.assignable {
				t.Fatalf("got (%v,%v), want (%v,true)", assignable, known, tc.assignable)
			}
		})
	}
}
