package evaluator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"geblang/internal/arityscan"
	"geblang/internal/native"
)

// TestEvaluatorBuiltinArityMatchesCatalog compares the leading len(args) guard of each evaluator-implemented module function with the catalog the VM and analyzer enforce.
func TestEvaluatorBuiltinArityMatchesCatalog(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	bodies := map[string]*ast.BlockStmt{}
	var table *ast.File
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if file == "builtin_modules.go" {
			table = parsed
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				bodies[fn.Name.Name] = fn.Body
			}
		}
	}
	if table == nil {
		t.Fatal("builtin_modules.go not found")
	}
	checked := 0
	var failures []string
	ast.Inspect(table, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		module, ok := arityscan.StringLiteral(kv.Key)
		if !ok {
			return true
		}
		members, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range members.Elts {
			member, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			name, ok := arityscan.StringLiteral(member.Key)
			if !ok {
				continue
			}
			target := ""
			switch v := member.Value.(type) {
			case *ast.Ident:
				target = v.Name
			case *ast.SelectorExpr:
				target = v.Sel.Name
			}
			body := bodies[target]
			if body == nil {
				continue
			}
			runtimeAccepts, found := arityscan.LeadingGuard(body)
			if !found {
				continue
			}
			var catalogAccepts []int
			known := false
			for argc := 0; argc <= arityscan.MaxArity; argc++ {
				fits, ok := native.NativeArityFits(module, name, argc)
				known = known || ok
				if fits {
					catalogAccepts = append(catalogAccepts, argc)
				}
			}
			if !known {
				continue
			}
			checked++
			if !arityscan.SameInts(runtimeAccepts, catalogAccepts) {
				failures = append(failures, module+"."+name+": runtime accepts "+arityscan.Ints(runtimeAccepts)+", catalog accepts "+arityscan.Ints(catalogAccepts))
			}
		}
		return true
	})
	if checked < 20 {
		t.Fatalf("only %d evaluator builtins had a recognisable arity guard; the scan has stopped matching the sources", checked)
	}
	sort.Strings(failures)
	for _, f := range failures {
		t.Error(f)
	}
}
