package lower_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/transpiler/lower"
	"geblang/internal/transpiler/types"
)

func parse(t *testing.T, src string) *parserResult {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	return &parserResult{prog: prog}
}

type parserResult struct {
	prog interface { /* *ast.Program */
	}
}

func TestLowererImportRegistersKnownStdlibModule(t *testing.T) {
	p := parser.New(lexer.New("import io;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !mod.IsStdlibModule("io") {
		t.Errorf("expected io to be registered as stdlib module")
	}
}

func TestLowererImportSkipsUnknownModule(t *testing.T) {
	p := parser.New(lexer.New("import notamodule;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if mod.IsStdlibModule("notamodule") {
		t.Errorf("non-stdlib module should not be registered")
	}
}

func TestLowererCallEmitsBridgedNativeCall(t *testing.T) {
	src := "import io;\nio.println(\"hi\");\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, `fmt.Println(transpilert.Show("hi"))`) {
		t.Errorf("body missing fmt.Println call:\n%s", body)
	}
}

func TestLowererSysExitWrapsArgInIntCast(t *testing.T) {
	src := "import sys;\nsys.exit(0);\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "os.Exit(int(0))") {
		t.Errorf("expected os.Exit(int(0)), got:\n%s", body)
	}
}

func TestLowererStringLiteralIsQuotedSafely(t *testing.T) {
	src := "import io;\nio.println(\"a \\\"b\\\" c\");\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, `"a \"b\" c"`) {
		t.Errorf("expected escaped quoted string, got:\n%s", body)
	}
}

func TestLowererUnknownTopLevelStatementProducesError(t *testing.T) {
	src := "let x = 1;\ndel x;\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) == 0 {
		t.Errorf("expected an unsupported-statement error for del")
	}
}

func TestLowererImportRegistersAllAddedImports(t *testing.T) {
	src := "import io;\nimport sys;\nio.print(\"x\");\nsys.exit(0);\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	rendered := string(mod.Render())
	for _, imp := range []string{`"fmt"`, `"os"`} {
		if !strings.Contains(rendered, imp) {
			t.Errorf("rendered output missing import %s\n%s", imp, rendered)
		}
	}
}

func TestLowererTypedIntDeclEmitsVarInt64(t *testing.T) {
	src := "int x = 5;\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "var x int64 = 5") {
		t.Errorf("expected var x int64 = 5, got:\n%s", body)
	}
}

func TestLowererLetIntDeclEmitsTypedVar(t *testing.T) {
	src := "let y = 7;\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "var y int64 = 7") {
		t.Errorf("expected var y int64 = 7, got:\n%s", body)
	}
}

func TestLowererLetStringDeclEmitsShortAssign(t *testing.T) {
	src := "let s = \"hi\";\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, `s := "hi"`) {
		t.Errorf("expected s := \"hi\", got:\n%s", body)
	}
}

func TestLowererInfixArithmeticEmitsGoSyntax(t *testing.T) {
	src := "int x = 2 + 3 * 4;\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "2 + (3 * 4)") {
		t.Errorf("expected precedence-preserving parens, got:\n%s", body)
	}
}

func TestLowererInfixOperatorMap(t *testing.T) {
	cases := []struct {
		gbOp string
		want string
	}{
		{"+", "1 + 2"},
		{"-", "1 - 2"},
		{"*", "1 * 2"},
		{"%", "transpilert.ModInt(1, 2)"},
		{"==", "1 == 2"},
		{"!=", "1 != 2"},
		{"<", "1 < 2"},
		{">", "1 > 2"},
		{"<=", "1 <= 2"},
		{">=", "1 >= 2"},
	}
	for _, c := range cases {
		src := "int x = 1 " + c.gbOp + " 2;\n"
		p := parser.New(lexer.New(src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) != 0 {
			t.Fatalf("operator %q parser errors: %v", c.gbOp, errs)
		}

		mod := lower.NewModule("main", true, types.IntModeFast)
		l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
		l.LowerProgram(prog)

		body := mod.MainBody().String()
		if !strings.Contains(body, c.want) {
			t.Errorf("operator %q: expected %q in body, got:\n%s", c.gbOp, c.want, body)
		}
	}
}

func TestLowererFloatDivisionMapsToGoDivision(t *testing.T) {
	p := parser.New(lexer.New("float x = 1.0f / 2.0f;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)
	if body := mod.MainBody().String(); !strings.Contains(body, "1.0 / 2.0") {
		t.Errorf("expected Go float division, got:\n%s", body)
	}
}

func TestLowererIdentifierLowers(t *testing.T) {
	src := "import io;\nint x = 5;\nio.println(x);\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "fmt.Println(transpilert.Show(x))") {
		t.Errorf("expected fmt.Println(transpilert.Show(x)), got:\n%s", body)
	}
}

func TestLowererSysArgsLowersToOsArgs(t *testing.T) {
	src := "import sys;\nlet a = sys.args();\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "a := os.Args[1:]") {
		t.Errorf("expected a := os.Args[1:], got:\n%s", body)
	}
}

func TestLowererListLengthLowersToLen(t *testing.T) {
	src := "import io;\nimport sys;\nlet a = sys.args();\nio.println(a.length());\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "int64(len(a))") {
		t.Errorf("expected int64(len(a)), got:\n%s", body)
	}
}

func TestLowererHOFMethodOnMapResultRoutesThroughTranspilert(t *testing.T) {
	src := "import io;\nlet xs = [1, 2, 3];\nlet d = xs.map(func(int x): int { return x * 2; });\nio.println(d.reduce(func(int a, int b): int { return a + b; }, 0));\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "transpilert.Reduce(d,") {
		t.Errorf("expected transpilert.Reduce(d, ...), got:\n%s", body)
	}
	if strings.Contains(body, "d.reduce(") {
		t.Errorf("did not expect raw d.reduce(...), got:\n%s", body)
	}
}

func TestLowererIfStatementEmitsGoIf(t *testing.T) {
	src := "import io;\nif (1 > 0) { io.println(\"yes\"); }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "if (1 > 0) {") {
		t.Errorf("expected if cond {, got:\n%s", body)
	}
	if !strings.Contains(body, "fmt.Println(transpilert.Show(\"yes\"))") {
		t.Errorf("expected body call inside if, got:\n%s", body)
	}
}

func TestLowererIfElseEmitsBothBranches(t *testing.T) {
	src := "import io;\nif (1 > 0) { io.println(\"a\"); } else { io.println(\"b\"); }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "} else {") {
		t.Errorf("expected else clause, got:\n%s", body)
	}
}

func TestLowererIfElseIfChainEmitsElseIf(t *testing.T) {
	src := "import io;\nif (1 > 0) { io.println(\"a\"); } elseif (2 > 0) { io.println(\"b\"); }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "} else if") {
		t.Errorf("expected else if, got:\n%s", body)
	}
}

func TestLowererIndexExpressionWithLiteralIndex(t *testing.T) {
	src := "import io;\nimport sys;\nlet a = sys.args();\nio.println(a[0]);\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "a[0]") {
		t.Errorf("expected a[0], got:\n%s", body)
	}
	if strings.Contains(body, "int(0)") {
		t.Errorf("literal index should not be wrapped in int(), got:\n%s", body)
	}
}

func TestLowererIndexExpressionWithVariableIndex(t *testing.T) {
	src := "import io;\nimport sys;\nint i = 0;\nlet a = sys.args();\nio.println(a[i]);\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, "a[int(i)]") {
		t.Errorf("expected a[int(i)] for non-literal index, got:\n%s", body)
	}
}

func TestLowererLetDeclInfersListStringFromSysArgs(t *testing.T) {
	src := "import sys;\nlet a = sys.args();\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}

	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)

	// Trigger a length call which only succeeds if scope has the right type.
	src2 := "import io;\nimport sys;\nlet a = sys.args();\nio.println(a.length());\n"
	p2 := parser.New(lexer.New(src2))
	prog2 := p2.ParseProgram()
	if errs := p2.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	mod2 := lower.NewModule("main", true, types.IntModeFast)
	l2 := lower.NewLowerer(mod2, lower.NewNativeBridge(), "test.gb")
	l2.LowerProgram(prog2)
	if !strings.Contains(mod2.MainBody().String(), "int64(len(a))") {
		t.Errorf("type inference flow broken: expected int64(len(a)), got:\n%s", mod2.MainBody().String())
	}
}

func TestLowererCStyleForLoopShape(t *testing.T) {
	src := "import io;\nfor (int i = 0; i < 3; i++) { io.println(i); }\n"
	mod, l := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "for i := int64(0); ") {
		t.Errorf("expected i := int64(0) init, got:\n%s", body)
	}
	if !strings.Contains(body, "i++") {
		t.Errorf("expected i++ update, got:\n%s", body)
	}
	if len(l.Errors()) != 0 {
		t.Errorf("unexpected errors: %v", l.Errors())
	}
}

func TestLowererInclusiveRangeForLoop(t *testing.T) {
	src := "import io;\nfor (i in 1..5 by 2) { io.println(i); }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "i <= int64(5)") {
		t.Errorf("expected inclusive bound i <= int64(5), got:\n%s", body)
	}
	if !strings.Contains(body, "i += int64(2)") {
		t.Errorf("expected step i += int64(2), got:\n%s", body)
	}
}

func TestLowererForInListUsesRange(t *testing.T) {
	src := "import io;\nlet xs = [1, 2, 3];\nfor (n in xs) { io.println(n); }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "for _, n := range xs") {
		t.Errorf("expected for _, n := range xs, got:\n%s", body)
	}
}

func TestLowererForInDictUnreferencedKeyBecomesUnderscore(t *testing.T) {
	src := "import io;\nlet d = {\"a\": 1};\nfor (k, v in d.items()) { io.println(v); }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "for _, __k := range d.Keys()") || !strings.Contains(body, "v, _ := d.Get(__k)") {
		t.Errorf("expected unused key elided with value lookup, got:\n%s", body)
	}
}

func TestLowererForInDictBothUsedKeepsNames(t *testing.T) {
	src := "import io;\nlet d = {\"a\": 1};\nfor (k, v in d.items()) { io.println(k); io.println(v); }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "for _, k := range d.Keys()") || !strings.Contains(body, "v, _ := d.Get(k)") {
		t.Errorf("expected key range with value lookup, got:\n%s", body)
	}
}

func TestLowererListLiteralEmitsTypedComposite(t *testing.T) {
	src := "let xs = [2, 4, 6];\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "[]int64{2, 4, 6}") {
		t.Errorf("expected []int64{2, 4, 6}, got:\n%s", body)
	}
}

func TestLowererDictLiteralEmitsTypedMap(t *testing.T) {
	src := "let d = {\"a\": 2, \"b\": 3};\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, `transpilert.NewOrderedDict[string, int64]()`) || !strings.Contains(body, `__d.Set("a", 2)`) {
		t.Errorf("expected ordered dict construction, got:\n%s", body)
	}
}

func TestLowererInterpolatedStringUsesFmtSprintf(t *testing.T) {
	src := "let i = 1;\nlet s = \"value=${i}\";\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, `fmt.Sprintf("value=%v", transpilert.Show(i))`) {
		t.Errorf("expected fmt.Sprintf call, got:\n%s", body)
	}
}

func TestLowererPostfixIncrementEmitsGoSyntax(t *testing.T) {
	src := "int i = 0;\ni++;\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "i++") {
		t.Errorf("expected i++, got:\n%s", body)
	}
}

func TestLowererAssignmentExpression(t *testing.T) {
	src := "int total = 0;\ntotal = 5;\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "total = 5") {
		t.Errorf("expected total = 5, got:\n%s", body)
	}
}

func TestLowererHomogeneousListLiteralEmitsTypedElem(t *testing.T) {
	mod, l := runLowerer(t, "let xs = [1, 2, 3];\n")
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "[]int64{") {
		t.Errorf("expected []int64 list, got:\n%s", body)
	}
}

func TestLowererMixedListLiteralFallsBackToAny(t *testing.T) {
	mod, l := runLowerer(t, "let xs = [1, \"a\"];\n")
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "[]any{") {
		t.Errorf("expected []any list, got:\n%s", body)
	}
}

func TestLowererDictLiteralInfersKeyValue(t *testing.T) {
	mod, l := runLowerer(t, "let d = {\"k\": 1, \"k2\": 2};\n")
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "transpilert.NewOrderedDict[string, int64]()") {
		t.Errorf("expected ordered dict construction, got:\n%s", body)
	}
}

func runLowerer(t *testing.T, src string) (*lower.Module, *lower.Lowerer) {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	mod := lower.NewModule("main", true, types.IntModeFast)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)
	return mod, l
}

func TestLowererTopLevelFunctionEmitsGoFuncDecl(t *testing.T) {
	src := "func double(int n): int { return n * 2; }\n"
	mod, l := runLowerer(t, src)

	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func double(n int64) int64") {
		t.Errorf("expected func double(n int64) int64, got:\n%s", decls)
	}
	if !strings.Contains(decls, "return (n * 2)") {
		t.Errorf("expected return body, got:\n%s", decls)
	}
}

func TestLowererFunctionWithoutReturnType(t *testing.T) {
	src := "func sayHi() { }\n"
	mod, _ := runLowerer(t, src)

	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func sayHi() {") {
		t.Errorf("expected func sayHi() with no return type, got:\n%s", decls)
	}
}

func TestLowererAsyncFunctionIsRejected(t *testing.T) {
	src := "async func task() { }\n"
	mod, l := runLowerer(t, src)
	_ = mod
	if len(l.Errors()) == 0 {
		t.Errorf("expected async function to be rejected in Phase 1")
	}
}

func TestLowererGenericFunctionAccepted(t *testing.T) {
	src := "func id<T>(T x): T { return x; }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Errorf("expected generic function to be accepted, got errors: %v", l.Errors())
	}
	if !strings.Contains(mod.TopDecls().String(), "func id[T any]") {
		t.Errorf("expected generic emission, got:\n%s", mod.TopDecls().String())
	}
}

func TestLowererWhileLoopEmitsForLoop(t *testing.T) {
	src := "int i = 0;\nwhile (i < 5) { i = i + 1; }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "for ") || !strings.Contains(body, "i < 5") {
		t.Errorf("expected for ... i < 5 ..., got:\n%s", body)
	}
}

func TestLowererReturnWithValue(t *testing.T) {
	src := "func one(): int { return 1; }\n"
	mod, _ := runLowerer(t, src)

	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "return 1") {
		t.Errorf("expected return 1, got:\n%s", decls)
	}
}

func TestLowererBareReturn(t *testing.T) {
	src := "func nothing() { return; }\n"
	mod, _ := runLowerer(t, src)

	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "return\n") {
		t.Errorf("expected bare return, got:\n%s", decls)
	}
}

func TestLowererBreakContinueEmitDirectly(t *testing.T) {
	src := "while (true) { break; continue; }\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "break") || !strings.Contains(body, "continue") {
		t.Errorf("expected break and continue keywords, got:\n%s", body)
	}
}

func TestLowererPrefixNegationAndBang(t *testing.T) {
	src := "let a = -5;\nlet b = !true;\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "-5") {
		t.Errorf("expected -5, got:\n%s", body)
	}
	if !strings.Contains(body, "!true") {
		t.Errorf("expected !true, got:\n%s", body)
	}
}

func TestLowererBoolKeywordLiterals(t *testing.T) {
	src := "let t = true;\nlet f = false;\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "t := true") {
		t.Errorf("expected t := true, got:\n%s", body)
	}
	if !strings.Contains(body, "f := false") {
		t.Errorf("expected f := false, got:\n%s", body)
	}
}

func TestLowererNullKeywordLiteralEmitsAnyTypedVar(t *testing.T) {
	src := "let n = null;\n"
	mod, _ := runLowerer(t, src)

	body := mod.MainBody().String()
	if !strings.Contains(body, "var n any = nil") {
		t.Errorf("expected var n any = nil (Go cannot infer from nil literal), got:\n%s", body)
	}
}

func TestLowererMathAbsWrapsWithFloat64(t *testing.T) {
	src := "import math;\nlet x = math.abs(-5);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "math.Abs(float64(-5))") {
		t.Errorf("expected math.Abs(float64(-5)), got:\n%s", body)
	}
}

func TestLowererMathFloorWrapsResultInInt64(t *testing.T) {
	src := "import math;\nlet x = math.floor(3.7);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `int64(math.Floor(gbDecimalToFloat(gbDecimalLit("3.7"))))`) {
		t.Errorf("expected int64(math.Floor(gbDecimalToFloat(gbDecimalLit(\"3.7\")))), got:\n%s", body)
	}
}

func TestLowererMathPowEmitsBinary(t *testing.T) {
	src := "import math;\nlet x = math.pow(2, 3);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "math.Pow(float64(2), float64(3))") {
		t.Errorf("expected math.Pow(...), got:\n%s", body)
	}
}

func TestLowererMathPiEmitsConstant(t *testing.T) {
	src := "import math;\nlet x = math.pi();\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "math.Pi") {
		t.Errorf("expected math.Pi, got:\n%s", body)
	}
}

func TestLowererMathClampUsesMinMaxNest(t *testing.T) {
	src := "import math;\nlet x = math.clamp(12, 0, 10);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "min(max(12, 0), 10)") {
		t.Errorf("expected min(max(12, 0), 10), got:\n%s", body)
	}
}

func TestLowererMathMinVariadic(t *testing.T) {
	src := "import math;\nlet x = math.min(3, 1, 2);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "min(3, 1, 2)") {
		t.Errorf("expected min(3, 1, 2), got:\n%s", body)
	}
}

func TestLowererIntToStringCastUsesFmtSprintf(t *testing.T) {
	src := "int x = 42;\nlet s = x as string;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `fmt.Sprintf("%v", x)`) {
		t.Errorf("expected fmt.Sprintf cast, got:\n%s", body)
	}
}

func TestLowererFloatToIntCastUsesHelper(t *testing.T) {
	src := "let s = 3.7 as int;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `gbFloatToInt(gbDecimalToFloat(gbDecimalLit("3.7")))`) {
		t.Errorf("expected gbFloatToInt helper, got:\n%s", body)
	}
	if !mod.HasHelper("gbFloatToInt") {
		t.Errorf("expected module to require gbFloatToInt helper")
	}
}

func TestLowererIntToFloatCast(t *testing.T) {
	src := "let s = 5 as float;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "float64(5)") {
		t.Errorf("expected float64(5), got:\n%s", body)
	}
}

func TestLowererStringToIntCastLowersToHelper(t *testing.T) {
	src := `let s = "5" as int;` + "\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected diagnostics for string→int cast: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "transpilert.AsIntFast(") {
		t.Errorf("expected string→int cast to lower to transpilert.AsIntFast, got:\n%s", body)
	}
}

func TestLowererMatchExpressionEmitsIIFE(t *testing.T) {
	src := "let label = match (1) { case 1 => \"a\"; default => \"b\"; };\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "func() string {") {
		t.Errorf("expected typed IIFE wrapping match expr, got:\n%s", body)
	}
	if !strings.Contains(body, "__m := 1") {
		t.Errorf("expected scrutinee binding, got:\n%s", body)
	}
	if !strings.Contains(body, `return "a"`) {
		t.Errorf("expected case body return, got:\n%s", body)
	}
	if !strings.Contains(body, `return "b"`) {
		t.Errorf("expected default body, got:\n%s", body)
	}
}

func TestLowererMatchStatementEmitsIfChain(t *testing.T) {
	src := "import io;\nlet n = 7;\nmatch (n) { case 1: io.println(\"a\"); default: io.println(\"b\"); }\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "__m := n") {
		t.Errorf("expected scrutinee binding, got:\n%s", body)
	}
	if !strings.Contains(body, "if __m == 1 {") {
		t.Errorf("expected if chain, got:\n%s", body)
	}
}

func TestLowererMatchGuardAddedAsAndCondition(t *testing.T) {
	src := "let label = match (10) { case 10 if (true) => \"yes\"; default => \"no\"; };\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "__m == 10 && (true)") {
		t.Errorf("expected guard merged via &&, got:\n%s", body)
	}
}

func TestLowererStringMethodLengthUsesRuneCount(t *testing.T) {
	src := `string s = "hi"; let n = s.length();` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "int64(len([]rune(s)))") {
		t.Errorf("expected rune-count length, got:\n%s", body)
	}
}

func TestLowererStringMethodContainsUsesStringsContains(t *testing.T) {
	src := `string s = "hi"; let b = s.contains("h");` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `strings.Contains(s, "h")`) {
		t.Errorf("expected strings.Contains, got:\n%s", body)
	}
}

func TestLowererStringMethodOnLiteralReceiver(t *testing.T) {
	src := `let n = "  hi  ".trim();` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `strings.TrimSpace("  hi  ")`) {
		t.Errorf("expected strings.TrimSpace on literal, got:\n%s", body)
	}
}

func TestLowererStringMethodSplitTypePropagatesToBinding(t *testing.T) {
	src := `let parts = "a,b,c".split(","); let n = parts.length();` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "int64(len(parts))") {
		t.Errorf("expected length on list type, got:\n%s", body)
	}
}

func TestLowererUserModuleCallUsesPrefix(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	mod.RegisterUserModule("helper", "helper")
	src := `import helper; helper.greet("x");` + "\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "main.gb")
	l.LowerProgram(prog)

	body := mod.MainBody().String()
	if !strings.Contains(body, `helper_greet("x")`) {
		t.Errorf("expected helper_greet call, got:\n%s", body)
	}
}

// A native module also provided as source AST (e.g. profiler) routes an
// external call to its transpiled export, not the native bridge.
func TestLowererSourceModuleWinsOverNativeBridge(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	mod.RegisterSourceModule("profiler")
	mod.RegisterUserModule("profiler", "profiler")
	src := "import profiler; profiler.timer();\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "main.gb")
	l.SetCanonical("main")
	l.LowerProgram(prog)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "profiler_timer()") {
		t.Errorf("expected source-routed profiler_timer() call, got:\n%s", body)
	}
}

// A self-import (`import profiler as native` inside profiler) routes to the
// native bridge, not the source export.
func TestLowererSelfImportRoutesToNativeBridge(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	mod.RegisterSourceModule("profiler")
	src := "import profiler as native; export func snap(): any { return native.snapshot(); }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	l := lower.NewModuleLowerer(mod, lower.NewNativeBridge(), "profiler.gb", "profiler_")
	l.SetCanonical("profiler")
	l.LowerProgram(prog)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "transpilert.ProfilerSnapshot()") {
		t.Errorf("expected self-import to route to native bridge, got:\n%s", decls)
	}
}

func TestLowererNonEntryFunctionUsesPrefixedName(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "export func greet(string name): string { return name; }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewModuleLowerer(mod, lower.NewNativeBridge(), "helper.gb", "helper_")
	l.LowerProgram(prog)

	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func helper_greet(name string) string") {
		t.Errorf("expected prefixed function name, got:\n%s", decls)
	}
}

func TestLowererNonEntryRejectsTopLevelStatement(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "while (true) { }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewModuleLowerer(mod, lower.NewNativeBridge(), "helper.gb", "helper_")
	l.LowerProgram(prog)

	if len(l.Errors()) == 0 {
		t.Errorf("expected executable top-level statement in non-entry module to be rejected")
	}
}

func TestLowererNonEntryModuleVarIsPrefixedPackageVar(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "let netName = \"appnet\";\n" +
		"export func name(): string { return netName; }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewModuleLowerer(mod, lower.NewNativeBridge(), "helper.gb", "helper_")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("module-level let should lower without error, got: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "var helper_netName string = \"appnet\"") {
		t.Errorf("expected prefixed package var, got:\n%s", decls)
	}
	if !strings.Contains(decls, "return helper_netName") {
		t.Errorf("expected same-module reference to use the prefixed name, got:\n%s", decls)
	}
}

func TestLowererEntryHoistsFunctionReferencedConst(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "let greeting = \"hi\";\n" +
		"func shout(): string { return greeting; }\n" +
		"export func main() { }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "main.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) != 0 {
		t.Fatalf("hoistable const should lower without error, got: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "var greeting string = \"hi\"") {
		t.Errorf("expected hoisted package var, got:\n%s", decls)
	}
	if strings.Contains(mod.MainBody().String(), "greeting :=") {
		t.Errorf("hoisted const must not also declare a main() local:\n%s", mod.MainBody().String())
	}
}

func TestLowererEntryHoistDiagnosesNonConstantInitializer(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "let cached = compute();\n" +
		"func compute(): int { return 5; }\n" +
		"func usesIt(): int { return cached; }\n" +
		"export func main() { }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "main.gb")
	l.LowerProgram(prog)

	if len(l.Errors()) == 0 {
		t.Errorf("a function-referenced let with a call initializer should diagnose")
	}
}

func TestLowererExportStatementIsTransparent(t *testing.T) {
	mod := lower.NewModule("main", true, types.IntModeFast)
	src := "export func add(int a, int b): int { return a + b; }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "main.gb")
	l.LowerProgram(prog)

	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func add(a int64, b int64) int64") {
		t.Errorf("expected exported func to lower normally, got:\n%s", decls)
	}
}

func TestLowererClassEmitsStructAndConstructor(t *testing.T) {
	src := "class Point { int x; int y; func Point(int x, int y) { this.x = x; this.y = y; } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()

	if !strings.Contains(decls, "type Point struct {") {
		t.Errorf("expected struct decl, got:\n%s", decls)
	}
	if !strings.Contains(decls, "x int64") || !strings.Contains(decls, "y int64") {
		t.Errorf("expected fields, got:\n%s", decls)
	}
	if !strings.Contains(decls, "func NewPoint(x int64, y int64) *Point {") {
		t.Errorf("expected constructor signature, got:\n%s", decls)
	}
	if !strings.Contains(decls, "this := &Point{}") {
		t.Errorf("expected this allocation, got:\n%s", decls)
	}
}

func TestLowererClassMethodBindsThisReceiver(t *testing.T) {
	src := "class Box { int n; func get(): int { return this.n; } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()

	if !strings.Contains(decls, "func (this *Box) get() int64 {") {
		t.Errorf("expected method with *Box receiver, got:\n%s", decls)
	}
	if !strings.Contains(decls, "return this.n") {
		t.Errorf("expected this.n access, got:\n%s", decls)
	}
}

func TestLowererClassFieldDefaultInitializedInConstructor(t *testing.T) {
	src := "class Counter { int count = 0; func Counter() {} }\nlet c = Counter();\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()

	if !strings.Contains(decls, "this.count = 0") {
		t.Errorf("expected field default assignment, got:\n%s", decls)
	}
}

func TestLowererClassInstantiationRewritesToNewClass(t *testing.T) {
	src := "class Box { int n; func Box(int n) { this.n = n; } }\nlet b = Box(5);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()

	if !strings.Contains(body, "NewBox(5)") {
		t.Errorf("expected NewBox call, got:\n%s", body)
	}
}

func TestLowererTypedClassDeclEmitsPointer(t *testing.T) {
	src := "class Box { int n; func Box(int n) { this.n = n; } }\nBox b = Box(5);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()

	if !strings.Contains(body, "var b *Box = NewBox(5)") {
		t.Errorf("expected var b *Box = NewBox(...), got:\n%s", body)
	}
}

func TestLowererClassWithGenericsAccepted(t *testing.T) {
	src := "class Box<T> { T value; func Box(T v) { this.value = v; } }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Errorf("expected generic class to be accepted, got errors: %v", l.Errors())
	}
	if !strings.Contains(mod.TopDecls().String(), "type Box[T any] struct") {
		t.Errorf("expected generic struct, got:\n%s", mod.TopDecls().String())
	}
}

func TestLowererClassExtendsEmbedsParent(t *testing.T) {
	src := "class Base { string n; func Base(string n) { this.n = n; } }\nclass Child extends Base { func Child(string n) { parent(n); } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "*Base") {
		t.Errorf("expected embedded *Base in Child, got:\n%s", decls)
	}
}

func TestLowererFieldAssignmentEmits(t *testing.T) {
	src := "class Box { int n; func Box(int n) { this.n = n; } }\nlet b = Box(5);\nb.n = 99;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()

	if !strings.Contains(body, "b.n = 99") {
		t.Errorf("expected field assignment, got:\n%s", body)
	}
}

func TestLowererFunctionLiteralEmitsClosure(t *testing.T) {
	src := "let f = func(int n): int { return n * 2; };\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "func(n int64) int64 {") {
		t.Errorf("expected func literal, got:\n%s", body)
	}
}

func TestLowererDeferEmitsGoDefer(t *testing.T) {
	src := "import io;\nfunc f() { defer io.println(\"end\"); io.println(\"start\"); }\nf();\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "defer fmt.Println(transpilert.Show(\"end\"))") {
		t.Errorf("expected defer, got:\n%s", decls)
	}
}

func TestLowererThrowEmitsPanic(t *testing.T) {
	src := "class E extends Error { func E(string m) { parent(m); } }\nfunc f() { throw E(\"x\"); }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, `panic(NewE("x"))`) {
		t.Errorf("expected panic call, got:\n%s", decls)
	}
}

func TestLowererTryCatchRecoversAndMatchesByClass(t *testing.T) {
	src := "import io;\nclass E extends Error { func E(string m) { parent(m); } }\ntry { throw E(\"x\"); } catch (E e) { io.println(e.message); }\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "= recover() }()") {
		t.Errorf("expected deferred recover into the exception var, got:\n%s", body)
	}
	if !strings.Contains(body, `.IsClass("E")`) {
		t.Errorf("expected class-hierarchy catch match, got:\n%s", body)
	}
}

func TestLowererTryFinallyRunsOnAllPaths(t *testing.T) {
	src := "import io;\ntry { io.println(\"a\"); } finally { io.println(\"b\"); }\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	// finally is its own signal closure that overrides any pending transfer,
	// not a Go defer (which would be function-scoped, not try-scoped).
	if !strings.Contains(body, "__fsig1") {
		t.Errorf("expected finally signal closure, got:\n%s", body)
	}
}

func TestLowererInheritanceEmbedsParent(t *testing.T) {
	src := "class Base { string n; func Base(string n) { this.n = n; } func m(): string { return this.n; } }\nclass Child extends Base { func Child(string n) { parent(n); } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	// A hierarchy class embeds the parent impl for reuse and carries a self
	// field; the constructor builds the parent through its interface ctor.
	if !strings.Contains(decls, "type Child_impl struct {\n\t*Base_impl") {
		t.Errorf("expected embedded *Base_impl, got:\n%s", decls)
	}
	if !strings.Contains(decls, "this.Base_impl = NewBase(n).(*Base_impl)") {
		t.Errorf("expected parent constructor invocation, got:\n%s", decls)
	}
}

func TestLowererErrorClassEmitsTranspilertErrorConstructor(t *testing.T) {
	src := "class E extends Error { func E(string m) { parent(m); } }\nfunc f() { throw E(\"x\"); }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func NewE(m string) *transpilert.Error {") {
		t.Errorf("expected error class to lower to a *transpilert.Error constructor, got:\n%s", decls)
	}
	if !strings.Contains(decls, `Class: "E", Message: m`) {
		t.Errorf("expected class+message carried into the error value, got:\n%s", decls)
	}
}

func TestLowererEnumEmitsTypeAndConstants(t *testing.T) {
	src := "enum Color { Red, Green, Blue }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "type Color int") {
		t.Errorf("expected type Color int, got:\n%s", decls)
	}
	for _, c := range []string{"ColorRed", "ColorGreen", "ColorBlue"} {
		if !strings.Contains(decls, c) {
			t.Errorf("expected constant %s in decls", c)
		}
	}
}

func TestLowererEnumSelectorLowersToFlatName(t *testing.T) {
	src := "enum Color { Red, Green, Blue }\nlet c = Color.Green;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "ColorGreen") {
		t.Errorf("expected ColorGreen, got:\n%s", body)
	}
}

func TestLowererEnumStringMethod(t *testing.T) {
	src := "enum Color { Red, Green, Blue }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func (__v Color) String() string {") {
		t.Errorf("expected String() receiver, got:\n%s", decls)
	}
	if !strings.Contains(decls, `return "Color.Red"`) {
		t.Errorf("expected Color.Red string, got:\n%s", decls)
	}
}

func TestLowererTaggedEnumEmitsSealedInterface(t *testing.T) {
	src := "enum Result { Ok(string), Err(string) }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"type Result interface",
		"__gbResult()",
		"type ResultOk struct {",
		"func (ResultOk) __gbResult()",
		"func NewResultOk(v0 string) ResultOk",
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("expected %q in decls, got:\n%s", want, decls)
		}
	}
}

func TestLowererTaggedEnumConstructorCall(t *testing.T) {
	src := "enum Result { Ok(string), Err(string) }\nlet r = Result.Ok(\"x\");\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `NewResultOk("x")`) {
		t.Errorf("expected NewResultOk call, got:\n%s", body)
	}
	if !strings.Contains(body, "var r Result =") {
		t.Errorf("expected interface-typed binding, got:\n%s", body)
	}
}

func TestLowererInstanceOfClassReturnsTypeAssertion(t *testing.T) {
	src := "class Box { int n; func Box(int n) { this.n = n; } }\nlet b = Box(1);\nlet ok = b instanceof Box;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "any(b).(*Box)") {
		t.Errorf("expected any(b).(*Box) assertion, got:\n%s", body)
	}
}

func TestLowererInstanceOfTaggedVariant(t *testing.T) {
	src := "enum Result { Ok(string), Err(string) }\nlet r = Result.Ok(\"x\");\nlet ok = r instanceof Result.Ok;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "any(r).(ResultOk)") {
		t.Errorf("expected variant assertion, got:\n%s", body)
	}
}

func TestLowererStaticMethodEmitsFlatFunction(t *testing.T) {
	src := "class Util { static func double(int n): int { return n * 2; } }\nlet x = Util.double(5);\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	body := mod.MainBody().String()
	if !strings.Contains(decls, "func Util_double(n int64) int64") {
		t.Errorf("expected flat static func, got:\n%s", decls)
	}
	if !strings.Contains(body, "Util_double(5)") {
		t.Errorf("expected static call site rewrite, got:\n%s", body)
	}
}

func TestLowererInterfaceEmitsGoInterface(t *testing.T) {
	src := "interface Printable { func print(): string; }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "type Printable interface {") {
		t.Errorf("expected interface decl, got:\n%s", decls)
	}
	if !strings.Contains(decls, "print_() string") {
		t.Errorf("expected method signature with mangled name, got:\n%s", decls)
	}
}

func TestLowererInterfaceInheritanceEmbeds(t *testing.T) {
	src := "interface P { func p(): string; }\ninterface L extends P { func l(): string; }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "type L interface {\n\tP") {
		t.Errorf("expected embedded interface, got:\n%s", decls)
	}
}

func TestLowererInterfaceTypedDeclaration(t *testing.T) {
	src := "interface P { func p(): string; }\nclass C implements P { func p(): string { return \"x\"; } }\nP v = C();\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "var v P = NewC()") {
		t.Errorf("expected interface-typed var, got:\n%s", body)
	}
}

func TestLowererVariantPatternMatchUsesTypeSwitch(t *testing.T) {
	src := "import io;\nenum R { Ok(string), Err(string) }\nmatch (R.Ok(\"x\")) { case R.Ok(string s): io.println(s); case R.Err(string e): io.println(e); }\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "switch __m := any(") {
		t.Errorf("expected type switch with any() boxing, got:\n%s", body)
	}
	if !strings.Contains(body, "case ROk:") {
		t.Errorf("expected case ROk, got:\n%s", body)
	}
	if !strings.Contains(body, "s := __m.V0") {
		t.Errorf("expected payload binding, got:\n%s", body)
	}
}

func TestLowererMatchExpressionReturnTypeInferredFromCaseBodies(t *testing.T) {
	src := "enum R { Ok(string), Err(string) }\nfunc f(R r): string { return match (r) { case R.Ok(string s) => s; case R.Err(string e) => e; }; }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "return func() string {") {
		t.Errorf("expected match IIFE typed as string, got:\n%s", decls)
	}
}

func TestLowererListPushMutatesInPlace(t *testing.T) {
	src := "let xs = [1, 2]; let ys = xs.push(3);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "*__p = append(*__p, 3)") || !strings.Contains(body, "}(&xs)") {
		t.Errorf("expected in-place push reassigning &xs, got:\n%s", body)
	}
}

func TestLowererListPopMutatesInPlace(t *testing.T) {
	src := "let xs = [1, 2, 3]; let p = xs.pop();\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "*__p = (*__p)[:len(*__p)-1]") || !strings.Contains(body, "}(&xs)") {
		t.Errorf("expected in-place pop reassigning &xs, got:\n%s", body)
	}
}

func TestLowererListContainsUsesSlicesContains(t *testing.T) {
	src := "let xs = [1, 2, 3]; let ok = xs.contains(2);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "slices.Contains(xs, 2)") {
		t.Errorf("expected slices.Contains, got:\n%s", body)
	}
}

func TestLowererDictHasKey(t *testing.T) {
	src := "let d = {\"a\": 1}; let ok = d.hasKey(\"a\");\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `_, __ok := d.Get("a")`) {
		t.Errorf("expected hasKey lookup, got:\n%s", body)
	}
}

func TestLowererDictKeysCallsOrderedKeys(t *testing.T) {
	src := "let d = {\"a\": 1}; let ks = d.keys();\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "d.Keys()") {
		t.Errorf("expected ordered Keys() call, got:\n%s", body)
	}
}

func TestLowererGenericFunctionEmitsTypeParam(t *testing.T) {
	src := "func id<T>(T x): T { return x; }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "func id[T any](x T) T {") {
		t.Errorf("expected generic func, got:\n%s", decls)
	}
}

func TestLowererGenericFunctionWithConstraint(t *testing.T) {
	src := "interface Stringer { func toStr(): string; }\nfunc fmt<T implements Stringer>(T v): string { return v.toStr(); }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "[T Stringer]") {
		t.Errorf("expected constrained type param, got:\n%s", decls)
	}
}

func TestLowererExplicitTypeArgsAtCallSite(t *testing.T) {
	src := "func id<T>(T x): T { return x; }\nlet r = id<int>(5);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "id[int64](5)") {
		t.Errorf("expected explicit type arg, got:\n%s", body)
	}
}

func TestLowererTypeParamInsideListType(t *testing.T) {
	src := "func first<T>(list<T> xs): T { return xs[0]; }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	if !strings.Contains(decls, "xs []T") {
		t.Errorf("expected xs []T (no pointer), got:\n%s", decls)
	}
}

func TestLowererGenericClassEmitsTypeParams(t *testing.T) {
	src := "class Box<T> { T value; func Box(T v) { this.value = v; } func get(): T { return this.value; } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"type Box[T any] struct {",
		"func NewBox[T any](v T) *Box[T] {",
		"this := &Box[T]{}",
		"func (this *Box[T]) get() T {",
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("missing %q in:\n%s", want, decls)
		}
	}
}

func TestLowererGenericInstantiationTypedDecl(t *testing.T) {
	src := "class Box<T> { T value; func Box(T v) { this.value = v; } }\nBox<int> b = Box<int>(42);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "var b *Box[int64] = NewBox[int64](42)") {
		t.Errorf("expected fully-qualified typed decl, got:\n%s", body)
	}
}

func TestLowererMagicAddDispatchesToClassMethod(t *testing.T) {
	src := `class M { int n; func M(int n) { this.n = n; } func __add(M o): M { return M(this.n + o.n); } }
M a = M(1);
M b = M(2);
M c = a + b;
` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "a.__add(b)") {
		t.Errorf("expected magic add dispatch, got:\n%s", body)
	}
}

func TestLowererMagicLtDispatchesToClassMethod(t *testing.T) {
	src := `class M { int n; func M(int n) { this.n = n; } func __lt(M o): bool { return this.n < o.n; } }
M a = M(1);
M b = M(2);
let r = a < b;
` + "\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "a.__lt(b)") {
		t.Errorf("expected magic lt dispatch, got:\n%s", body)
	}
}

func TestLowererClassWithoutMagicMethodKeepsInfix(t *testing.T) {
	src := "let x = 1 + 2;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "(1 + 2)") {
		t.Errorf("expected plain infix for non-class operands, got:\n%s", body)
	}
}

func TestLowererTernaryEmitsTypedIIFE(t *testing.T) {
	src := "let grade = 85 >= 90 ? \"A\" : \"B\";\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "func() string {") {
		t.Errorf("expected typed ternary IIFE, got:\n%s", body)
	}
}

func TestLowererOptionalSelectorEmitsNilCheck(t *testing.T) {
	src := "class B { int n; func B(int n) { this.n = n; } }\nB b = B(5);\nlet v = b?.n;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "__r := b") || !strings.Contains(body, "if __r == nil") {
		t.Errorf("expected ?. IIFE with nil-check, got:\n%s", body)
	}
}

func TestLowererNullCoalesceOnNullableEmitsIIFE(t *testing.T) {
	src := "let x = null;\nlet y = x ?? \"default\";\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "__x := x") || !strings.Contains(body, "if __x != nil") {
		t.Errorf("expected ?? IIFE, got:\n%s", body)
	}
}

func TestLowererNullCoalesceOnIntShortCircuits(t *testing.T) {
	src := "let z = 10;\nlet w = z ?? 99;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if strings.Contains(body, "if __x != nil") {
		t.Errorf("expected ?? on int to short-circuit (no nil-check needed), got:\n%s", body)
	}
}

func TestLowererClassEmitsDecoratorsMethodsFieldsTables(t *testing.T) {
	src := "@svc(\"x\")\nclass G { string n; func G(string n) { this.n = n; } func hi(): string { return this.n; } }\n"
	mod, _ := runLowerer(t, src)
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"var __decorators_G = []*transpilert.OrderedDict[string, any]{",
		`__d.Set("name", "svc")`,
		`__d.Set("args", []any{"x"})`,
		"var __methods_G = []string{",
		`"hi"`,
		"var __fields_G = []string{",
		`"n"`,
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("missing %q in:\n%s", want, decls)
		}
	}
}

func TestLowererReflectClassNameEmitsLiteral(t *testing.T) {
	src := "import reflect;\nclass G {}\nio.println(reflect.className(G));\nimport io;\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `"G"`) {
		t.Errorf("expected class name literal, got:\n%s", body)
	}
}

func TestLowererGeneratorWrapsBodyInIterSeqClosure(t *testing.T) {
	src := "func nats(): generator<int> { let int n = 0; while (true) { yield n; n++; } }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"iter.Seq[int64]",
		"return func(yield func(int64) bool) {",
		"if !yield(n) { return }",
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("missing %q in:\n%s", want, decls)
		}
	}
}

func TestLowererYieldOutsideGeneratorErrors(t *testing.T) {
	src := "func bad(): int { yield 1; return 0; }\n"
	_, l := runLowerer(t, src)
	if len(l.Errors()) == 0 {
		t.Fatal("expected error for yield outside generator")
	}
}

func TestLowererAsyncFunctionWrapsInGbRunTask(t *testing.T) {
	src := "async func work(int x): int { return x; }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"*gbTask[int64]",
		"return gbRunTask(func() int64 {",
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("missing %q in:\n%s", want, decls)
		}
	}
	if !mod.HasHelper("gbTask") {
		t.Error("expected gbTask helper to be required")
	}
}

func TestLowererAwaitEmitsAwaitCall(t *testing.T) {
	src := "import io;\nasync func work(): int { return 1; }\nio.println(await work());\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "work().Await()") {
		t.Errorf("expected work().Await() in main body, got:\n%s", body)
	}
}

// The helper above keeps parse() in scope to avoid unused-import warnings in
// future tests that adopt it; it is not consumed by the current cases.
var _ = parse

func errorsContain(errs []lower.Error, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// --- T0.3 miscompile fixes ---

func TestLowererDecimalLiteralUsesRatHelper(t *testing.T) {
	src := "import io;\ndecimal d = 1.5;\nio.println(d as string);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, `gbDecimalLit("1.5")`) {
		t.Errorf("expected gbDecimalLit(\"1.5\"), got:\n%s", body)
	}
	if !mod.HasHelper("gbDecimalLit") {
		t.Error("expected gbDecimalLit helper to be required")
	}
}

func TestLowererDecimalLiteralStripsUnderscores(t *testing.T) {
	src := "import io;\ndecimal d = 1_000.25;\nio.println(d as string);\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, `gbDecimalLit("1000.25")`) {
		t.Errorf("expected underscore-stripped literal, got:\n%s", body)
	}
}

func TestLowererStringIndexUsesRuneHelper(t *testing.T) {
	src := "import io;\nstring s = \"abc\";\nio.println(s[1]);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "gbStringIndex(s, int64(1))") {
		t.Errorf("expected gbStringIndex(s, int64(1)), got:\n%s", body)
	}
}

func TestLowererRangeBuiltinLowersToHelper(t *testing.T) {
	src := "import io;\nfor (x in range(0, 3)) { io.println(x); }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "gbRange(int64(0), int64(3), 0, true)") {
		t.Errorf("expected gbRange(int64(0), int64(3), 0, true), got:\n%s", body)
	}
}

func TestLowererRangeBuiltinThreeArgs(t *testing.T) {
	src := "import io;\nfor (x in range(10, 0, -2)) { io.println(x); }\n"
	mod, _ := runLowerer(t, src)
	body := mod.MainBody().String()
	if !strings.Contains(body, "gbRange(int64(10), int64(0), int64(-2), false)") {
		t.Errorf("expected gbRange(int64(10), int64(0), int64(-2), false), got:\n%s", body)
	}
}

func TestLowererTypeofBuiltinDiagnoses(t *testing.T) {
	src := "import io;\nio.println(typeof(5));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), `does not yet support the "typeof" builtin`) {
		t.Errorf("expected typeof diagnostic, got: %v", l.Errors())
	}
}

func TestLowererDumpDirAssertDiagnose(t *testing.T) {
	for _, name := range []string{"dump", "dir", "assert"} {
		src := "import io;\n" + name + "(5);\n"
		_, l := runLowerer(t, src)
		if !errorsContain(l.Errors(), `does not yet support the "`+name+`" builtin`) {
			t.Errorf("expected %s diagnostic, got: %v", name, l.Errors())
		}
	}
}

func TestLowererNamedArgsReorderForKnownFunction(t *testing.T) {
	src := "import io;\nfunc f(int a, int b): int { return a - b; }\nio.println(f(b: 1, a: 9));\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "f(9, 1)") {
		t.Errorf("expected reordered f(9, 1), got:\n%s", body)
	}
}

func TestLowererNamedArgsOnNativeCallDiagnoses(t *testing.T) {
	src := "import io;\nio.println(x: 5);\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "named arguments") {
		t.Errorf("expected named-arg diagnostic on native call, got: %v", l.Errors())
	}
}

func TestLowererSpreadArgIntoNonVariadicDiagnoses(t *testing.T) {
	src := "import io;\nfunc f(int a, int b): int { return a; }\nlist<int> xs = [1, 2];\nio.println(f(...xs));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "final argument to a variadic function") {
		t.Errorf("expected spread-into-non-variadic diagnostic, got: %v", l.Errors())
	}
}

func TestLowererSpreadArgIntoVariadicPassesSlice(t *testing.T) {
	src := "import io;\nfunc f(int... xs): int { return 1; }\nlist<int> ys = [1, 2];\nio.println(f(...ys));\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "f(ys...)") {
		t.Errorf("expected spread to lower to f(ys...), got:\n%s", mod.MainBody().String())
	}
}

func TestLowererDefaultArgFilledAtCallSite(t *testing.T) {
	src := "import io;\nfunc g(int a, int b = 9): int { return a + b; }\nio.println(g(1));\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "g(1, 9)") {
		t.Errorf("expected default filled to g(1, 9), got:\n%s", mod.MainBody().String())
	}
}

func TestLowererListEqualityDiagnoses(t *testing.T) {
	src := "import io;\nlist<int> a = [1, 2];\nlist<int> b = [1, 2];\nio.println(a == b);\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "== / != on lists") {
		t.Errorf("expected list-equality diagnostic, got: %v", l.Errors())
	}
}

// --- T1.2 in-place list mutators ---

func TestLowererListPushOnLocalReassignsSlot(t *testing.T) {
	src := "import io;\nlist<int> xs = [1];\nxs.push(2);\nio.println(xs.length());\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "*__p = append(*__p, 2)") || !strings.Contains(body, "}(&xs)") {
		t.Errorf("expected in-place push reassigning &xs, got:\n%s", body)
	}
}

func TestLowererListPushOnParameterDiagnoses(t *testing.T) {
	src := "func add(list<int> xs) {\n  xs.push(1);\n}\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "reached through a parameter") {
		t.Errorf("expected param-receiver diagnostic, got: %v", l.Errors())
	}
}

func TestLowererListPushOnCallResultDiagnoses(t *testing.T) {
	src := "func make(): list<int> { return [1]; }\nmake().push(2);\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "call or complex expression") {
		t.Errorf("expected opaque-receiver diagnostic, got: %v", l.Errors())
	}
}

func TestLowererListReverseInPlace(t *testing.T) {
	src := "import io;\nlist<int> xs = [1, 2, 3];\nxs.reverse();\nio.println(xs[0]);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "slices.Reverse(*__p)") {
		t.Errorf("expected slices.Reverse, got:\n%s", body)
	}
}

func TestLowererListSortWithComparator(t *testing.T) {
	src := "import io;\nlist<int> xs = [3, 1];\nxs.sort(func(int a, int b): int { return a - b; });\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "transpilert.SortInPlaceCmp(&xs") {
		t.Errorf("expected SortInPlaceCmp, got:\n%s", body)
	}
}

func TestLowererListSortBySelector(t *testing.T) {
	src := "import io;\nlist<int> xs = [3, 1];\nxs.sortBy(func(int x): int { return x; });\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "transpilert.SortInPlaceBy(&xs") {
		t.Errorf("expected SortInPlaceBy, got:\n%s", body)
	}
}

// --- T1.5 safe (bigint) int mode ---

func runLowererMode(t *testing.T, src string, mode types.IntMode) (*lower.Module, *lower.Lowerer) {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	mod := lower.NewModule("main", true, mode)
	l := lower.NewLowerer(mod, lower.NewNativeBridge(), "test.gb")
	l.LowerProgram(prog)
	return mod, l
}

func TestLowererSafeIntDeclEmitsTranspilertInt(t *testing.T) {
	src := "int a = 2;\nint b = 3;\nint c = a + b;\n"
	mod, l := runLowererMode(t, src, types.IntModeBigInt)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "var a transpilert.Int = transpilert.FromInt64(2)") {
		t.Errorf("expected transpilert.Int decl, got:\n%s", body)
	}
	if !strings.Contains(body, "transpilert.AddInt(a, b)") {
		t.Errorf("expected AddInt for +, got:\n%s", body)
	}
}

func TestLowererSafeIntArithmeticUsesHelpers(t *testing.T) {
	src := "int a = 5;\nint b = 2;\nint s = a - b;\nint m = a * b;\nint n = -a;\n"
	mod, l := runLowererMode(t, src, types.IntModeBigInt)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	for _, want := range []string{"transpilert.SubInt(a, b)", "transpilert.MulInt(a, b)", "transpilert.NegInt(a)"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q, got:\n%s", want, body)
		}
	}
}

func TestLowererSafeIntDivisionDiagnoses(t *testing.T) {
	src := "int a = 6;\nint b = 2;\nint q = a / b;\n"
	_, l := runLowererMode(t, src, types.IntModeBigInt)
	if !errorsContain(l.Errors(), "safe int mode does not yet support") {
		t.Errorf("expected safe-int division diagnostic, got: %v", l.Errors())
	}
}

func TestLowererFastIntIsDefault(t *testing.T) {
	src := "int a = 2;\nint c = a + a;\n"
	mod, l := runLowererMode(t, src, types.IntModeFast)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if strings.Contains(body, "transpilert.Int") || strings.Contains(body, "AddInt") {
		t.Errorf("fast mode must not emit bignum helpers, got:\n%s", body)
	}
	if !strings.Contains(body, "var a int64 = 2") {
		t.Errorf("expected int64 decl in fast mode, got:\n%s", body)
	}
}

// --- T1.6 nullable types ---

func TestLowererNullableIntDeclEmitsPointer(t *testing.T) {
	src := "import io;\n?int x = null;\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "var x *int64 = (*int64)(nil)") {
		t.Errorf("expected typed-nil pointer decl, got:\n%s", body)
	}
}

func TestLowererNullableValueAssignTakesAddress(t *testing.T) {
	src := "import io;\n?int x = null;\nx = 5;\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "x = gbPtrOf(int64(5))") {
		t.Errorf("expected gbPtrOf address-of on assign, got:\n%s", body)
	}
}

func TestLowererNullCoalesceDerefsNullableValue(t *testing.T) {
	src := "import io;\n?int x = null;\nio.println(x ?? 42);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "return *__x") || !strings.Contains(body, "func() int64") {
		t.Errorf("expected deref of nullable pointer in ??, got:\n%s", body)
	}
}

func TestLowererNullableEqualityLowersToNilCheck(t *testing.T) {
	src := "import io;\n?int x = null;\nio.println(x == null);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "(x == nil)") {
		t.Errorf("expected x == nil, got:\n%s", body)
	}
}

func TestLowererOptionalCallGuardsNil(t *testing.T) {
	src := "class C { func m(): int { return 1; } }\n?C c = null;\nlet r = c?.m();\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "if __r == nil") || !strings.Contains(body, "__r.m()") {
		t.Errorf("expected guarded optional method call, got:\n%s", body)
	}
}

func TestLowererTypeAliasResolvesToTarget(t *testing.T) {
	src := "type Count = int;\nfunc bump(Count n): Count { return n + 1; }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.TopDecls().String(), "func bump(n int64) int64") {
		t.Errorf("expected alias Count to resolve to int64, got:\n%s", mod.TopDecls().String())
	}
}

func TestLowererPipeRewritesToCall(t *testing.T) {
	src := "import io;\nfunc dbl(int n): int { return n * 2; }\nlet x = 5 |> dbl;\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "dbl(5)") {
		t.Errorf("expected pipe to lower to dbl(5), got:\n%s", mod.MainBody().String())
	}
}

func TestLowererListDestructuringIndexesSlice(t *testing.T) {
	src := "import io;\nfunc pair(): list<int> { return [1, 2]; }\nlet [a, b] = pair();\nio.println(a + b);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "a := __dt1[0]") || !strings.Contains(body, "b := __dt1[1]") {
		t.Errorf("expected positional slice indexing, got:\n%s", body)
	}
}

func TestLowererBareMultiAssignBindsEachElement(t *testing.T) {
	src := "import io;\nlet x, y = 1, 2;\nio.println(x + y);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "x := 1") || !strings.Contains(body, "y := 2") {
		t.Errorf("expected per-element bindings, got:\n%s", body)
	}
}

func TestLowererTypedDictDestructuringDiagnosed(t *testing.T) {
	src := "dict<string, int> d = {\"a\": 1};\nlet {a} = d;\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "dict<string, any>") {
		t.Errorf("expected typed-dict destructuring diagnostic, got: %v", l.Errors())
	}
}

func TestLowererListComprehensionBuildsSlice(t *testing.T) {
	src := "import io;\nlet xs = [1, 2, 3];\nlet ys = [x * 2 for x in xs if x > 1];\nio.println(ys.length());\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "= append(") || !strings.Contains(body, "for _, x := range xs") {
		t.Errorf("expected append-in-loop comprehension, got:\n%s", body)
	}
}

func TestLowererDictComprehensionBuildsOrderedDict(t *testing.T) {
	src := "import io;\nlet m = {x: x * x for x in [1, 2]};\nio.println(m[1]);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "NewOrderedDict[int64, int64]") {
		t.Errorf("expected ordered-dict comprehension, got:\n%s", mod.MainBody().String())
	}
}

func TestLowererSetLiteralDiagnosed(t *testing.T) {
	src := "import io;\nlet s = {1, 2, 3};\nio.println(s.length());\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "set literals") {
		t.Errorf("expected set-literal diagnostic, got: %v", l.Errors())
	}
}

func TestLowererAnyTypedNonHofMethodRoutesToCallMethod(t *testing.T) {
	src := "import io;\nany x = \"hi\";\nio.println(x.upper());\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), `transpilert.CallMethod(`) {
		t.Errorf("expected CallMethod dispatch, got:\n%s", mod.MainBody().String())
	}
}

func TestLowererAnyTypedHofConcreteCallbackDiagnosed(t *testing.T) {
	// A concrete-typed callback lowers to a non-assertable func, so it diagnoses.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.map(func(int n): int { return n; }));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "needs an any-typed callback for 'map'") {
		t.Errorf("expected callback-type diagnostic, got: %v", l.Errors())
	}
}

func TestLowererAnyTypedHofAnyCallbackRoutesToCallMethod(t *testing.T) {
	// An any-typed callback lowers to the asserted func(any) any, so it routes.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.map(func(any n): any { return n; }));\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, `transpilert.CallMethod(`) || !strings.Contains(body, `"map"`) {
		t.Errorf("expected CallMethod map dispatch, got:\n%s", body)
	}
}

func TestLowererAnyTypedHofPredicateNonBoolDiagnosed(t *testing.T) {
	// filter needs a bool-returning callback; an any-returning one diagnoses.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.filter(func(any n): any { return n; }));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "needs an any-typed callback for 'filter'") {
		t.Errorf("expected callback-type diagnostic, got: %v", l.Errors())
	}
}

func TestLowererAnyTypedHofByVariantDiagnosed(t *testing.T) {
	// The less-common *By variants stay unsupported on an any receiver.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.takeWhile(func(any n): bool { return true; }));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "higher-order method 'takeWhile' on an any-typed value") {
		t.Errorf("expected *By HOF-on-any diagnostic, got: %v", l.Errors())
	}
}

func TestLowererAnyTypedSortByRoutesToCallMethod(t *testing.T) {
	// The common *By selectors route through the dispatcher with an any callback.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.sortBy(func(any n): any { return n; }));\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, `transpilert.CallMethod(`) || !strings.Contains(body, `"sortBy"`) {
		t.Errorf("expected CallMethod sortBy dispatch, got:\n%s", body)
	}
}

func TestLowererAnyTypedGroupByConcreteCallbackDiagnosed(t *testing.T) {
	// A concrete-typed selector still diagnoses with the cast-first hint.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.groupBy(func(int n): int { return n; }));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "needs an any-typed callback for 'groupBy'") {
		t.Errorf("expected callback-type diagnostic, got: %v", l.Errors())
	}
}

func TestLowererAnyTypedPartitionNonBoolDiagnosed(t *testing.T) {
	// partition needs a bool-returning predicate; an any-returning one diagnoses.
	src := "import io;\nany x = [1, 2, 3];\nio.println(x.partition(func(any n): any { return n; }));\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "needs an any-typed callback for 'partition'") {
		t.Errorf("expected callback-type diagnostic, got: %v", l.Errors())
	}
}

func TestLowererSetComprehensionDiagnosed(t *testing.T) {
	src := "import io;\nlet s = {x for x in [1, 2]};\nio.println(s);\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "set comprehensions") {
		t.Errorf("expected set-comprehension diagnostic, got: %v", l.Errors())
	}
}

func TestLowererDirectCollectionPrintViaShow(t *testing.T) {
	src := "import io;\nlet xs = [1, 2, 3];\nio.println(xs);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "transpilert.Show(xs)") {
		t.Errorf("expected collection print via transpilert.Show, got:\n%s", mod.MainBody().String())
	}
}

func TestLowererWithBlockDefersExit(t *testing.T) {
	src := "import io;\nclass R { func R() {} func __exit() { io.println(\"x\"); } }\nwith (R()) { io.println(\"in\"); }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	if !strings.Contains(body, "defer __dt1.__exit()") {
		t.Errorf("expected deferred __exit, got:\n%s", body)
	}
}

func TestLowererWithControlFlowDiagnosed(t *testing.T) {
	src := "import io;\nclass R { func R() {} }\nfunc f() { with (R()) { return; } }\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "return/break/continue inside a with block") {
		t.Errorf("expected with-control-flow diagnostic, got: %v", l.Errors())
	}
}

func TestLowererFromImportStdlibResolvesNative(t *testing.T) {
	src := "from io import println;\nprintln(42);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), "fmt.Println(transpilert.Show(42))") {
		t.Errorf("expected from-imported println to bridge to fmt.Println, got:\n%s", mod.MainBody().String())
	}
}

func TestLowererDelDiagnosed(t *testing.T) {
	src := "let x = 1;\ndel x;\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "does not yet support del") {
		t.Errorf("expected del diagnostic, got: %v", l.Errors())
	}
}

func TestLowererSelectDiagnosed(t *testing.T) {
	src := "import io;\nimport async.channel as ch;\nlet c = ch.Channel<int>(0);\nselect { case let v = c.recv(): { io.println(v); } default: { io.println(\"x\"); } }\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "does not yet support select") {
		t.Errorf("expected select diagnostic, got: %v", l.Errors())
	}
}

func TestLowererInitBlockInlines(t *testing.T) {
	src := "import io;\ninit { io.println(\"setup\"); }\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	if !strings.Contains(mod.MainBody().String(), `fmt.Println(transpilert.Show("setup"))`) {
		t.Errorf("expected init body inlined, got:\n%s", mod.MainBody().String())
	}
}

func TestLowererRangeAsValueDiagnosed(t *testing.T) {
	src := "import io;\nlet r = 1..5;\nio.println(r);\n"
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), "range used as a value") {
		t.Errorf("expected range-as-value diagnostic, got: %v", l.Errors())
	}
}

func TestLowererUntaggedEnumMethodEmitsGoMethod(t *testing.T) {
	src := `interface Describable { func describe(): string; }
enum Status implements Describable {
    Active, Closed;
    func describe(): string {
        return match (this) {
            case Status.Closed => "closed";
            default => "active";
        };
    }
    func loud(): string { return this.describe() + "!"; }
}
`
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"func (this Status) describe() string",
		"func (this Status) loud() string",
		"if __m == StatusClosed",
		"this.describe()",
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("decls missing %q:\n%s", want, decls)
		}
	}
}

func TestLowererTaggedEnumMethodEmitsInterfaceSigAndImpl(t *testing.T) {
	src := `interface Describable { func describe(): string; }
enum Result implements Describable {
    Ok(int), Err(string);
    func describe(): string {
        return match (this) {
            case Result.Ok(int v) => "ok";
            case Result.Err(string e) => e;
        };
    }
}
`
	mod, l := runLowerer(t, src)
	if errs := l.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected diagnostics: %v", errs)
	}
	decls := mod.TopDecls().String()
	for _, want := range []string{
		"type Result interface {",           // enum interface
		"describe() string",                 // method sig on the interface
		"func Result_describe(this Result)", // shared impl, this is the interface
		"func (__v ResultOk) describe()",    // variant delegate
		"func (__v ResultErr) describe()",   // variant delegate
		"return Result_describe(__v)",       // delegate forwards to shared impl
	} {
		if !strings.Contains(decls, want) {
			t.Errorf("expected %q in decls, got:\n%s", want, decls)
		}
	}
}

func TestLowererEnumMethodNamedStringDiagnoses(t *testing.T) {
	src := `enum E {
    A, B;
    func String(): string { return "x"; }
}
`
	_, l := runLowerer(t, src)
	if !errorsContain(l.Errors(), `enum method named "String"`) {
		t.Errorf("expected String-collision diagnostic, got: %v", l.Errors())
	}
}
