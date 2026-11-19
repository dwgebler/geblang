package native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"geblang/internal/arityscan"
)

// TestCatalogArityMatchesRuntimeGuards compares each registered native's leading len(args) guard with the catalog, because the VM and analyzer reject calls the catalog does not fit.
func TestCatalogArityMatchesRuntimeGuards(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	var failures []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 3 || !arityscan.IsRegisterCall(call) {
				return true
			}
			module, okM := arityscan.StringLiteral(call.Args[0])
			name, okN := arityscan.StringLiteral(call.Args[1])
			fn, okF := call.Args[2].(*ast.FuncLit)
			if !okM || !okN || !okF {
				return true
			}
			runtimeAccepts, found := arityscan.LeadingGuard(fn.Body)
			if !found {
				return true
			}
			if _, has := nativeDoc(module, name); !has {
				return true
			}
			checked++
			var catalogAccepts []int
			for argc := 0; argc <= arityscan.MaxArity; argc++ {
				if fits, _ := NativeArityFits(module, name, argc); fits {
					catalogAccepts = append(catalogAccepts, argc)
				}
			}
			if !arityscan.SameInts(runtimeAccepts, catalogAccepts) {
				failures = append(failures, module+"."+name+": runtime accepts "+arityscan.Ints(runtimeAccepts)+", catalog accepts "+arityscan.Ints(catalogAccepts))
			}
			return true
		})
	}
	if checked < 100 {
		t.Fatalf("only %d natives had a recognisable arity guard; the scan has stopped matching the sources", checked)
	}
	sort.Strings(failures)
	for _, f := range failures {
		t.Error(f)
	}
}
