package formatter_test

import (
	"strings"
	"testing"

	"geblang/internal/formatter"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// roundtrip verifies that formatting is idempotent: fmt(src) == fmt(fmt(src)).
func roundtrip(t *testing.T, src string) string {
	t.Helper()
	out1, err := formatter.Format([]byte(src))
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	out2, err := formatter.Format(out1)
	if err != nil {
		t.Fatalf("re-format error: %v", err)
	}
	if string(out1) != string(out2) {
		t.Fatalf("formatting not idempotent:\nfirst:\n%s\nsecond:\n%s", out1, out2)
	}
	return string(out1)
}

// parseOK verifies the formatted output is still valid Geblang.
func parseOK(t *testing.T, src string) {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("formatted output has parse errors: %v", errs)
	}
}

func TestFormatDeclaration(t *testing.T) {
	src := `let   x   =   42;`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "let x = 42;") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatMultiReturn(t *testing.T) {
	src := `func pair(): list<int> { return 1, 2; }
let a, b = pair();
a, b = b, a;
let [c, d] = [3, 4];`
	out := roundtrip(t, src)
	parseOK(t, out)
	for _, want := range []string{"return 1, 2;", "let a, b = pair();", "a, b = b, a;", "let [c, d] = [3, 4];"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestFormatFunction(t *testing.T) {
	src := `func add(int a, int b): int { return a + b; }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "func add(") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "return a + b;") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatIf(t *testing.T) {
	src := `if (x == 1) { y = 2; } else { y = 3; }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "} else {") {
		t.Fatalf("expected else on same line as closing brace: %q", out)
	}
}

func TestFormatIfElseIf(t *testing.T) {
	src := `if (x == 1) { y = 1; } else if (x == 2) { y = 2; } else { y = 3; }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "} else if (") {
		t.Fatalf("expected else-if chained: %q", out)
	}
	if !strings.Contains(out, "} else {") || strings.Contains(out, "\n else") {
		t.Fatalf("expected the final else on the else-if's closing line: %q", out)
	}
}

func TestFormatIfElseIfChainIsStable(t *testing.T) {
	src := "if (x == 1) {\n    y = 1;\n} else if (x == 2) {\n    y = 2;\n} else if (x == 3) {\n    y = 3;\n} else {\n    y = 4;\n}\nif (x == 1) {\n    y = 1;\n} else if (x == 2) {\n    y = 2;\n}\ny = 5;\n"
	out := roundtrip(t, src)
	parseOK(t, out)
	if out != src {
		t.Fatalf("else-if chain changed:\n%s", out)
	}
}

func TestFormatExportedTypeAliasHasOneSemicolon(t *testing.T) {
	src := "type A = int;\nexport type B = list<int>;\n"
	out := roundtrip(t, src)
	parseOK(t, out)
	if out != src {
		t.Fatalf("type aliases changed:\n%s", out)
	}
}

func TestFormatWhile(t *testing.T) {
	src := `while (i < 10) { i = i + 1; }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "while (i < 10) {") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatForIn(t *testing.T) {
	src := `for (x in items) { print(x); }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "for (x in items)") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatForCStyle(t *testing.T) {
	src := `for (let i = 0; i < 10; i++) { print(i); }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "for (") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatClass(t *testing.T) {
	src := `class Foo { func bar(): string { return "hi"; } }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "class Foo {") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatStringInterpolation(t *testing.T) {
	src := `let s = "Hello, ${name}!";`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, `"Hello, ${name}!"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatTryCatch(t *testing.T) {
	src := `try { doThing(); } catch (Error e) { log(e); } finally { cleanup(); }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "} catch (") {
		t.Fatalf("expected catch chained: %q", out)
	}
}

func TestFormatEnum(t *testing.T) {
	src := `enum Color { Red, Green, Blue }`
	out := roundtrip(t, src)
	parseOK(t, out)
	if !strings.Contains(out, "enum Color {") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormatBlankLineBetweenTopLevel(t *testing.T) {
	src := `func a() {} func b() {}`
	out := roundtrip(t, src)
	parseOK(t, out)
	// Two top-level functions should be separated by a blank line
	if !strings.Contains(out, "}\n\nfunc") {
		t.Fatalf("expected blank line between top-level functions: %q", out)
	}
}

func TestFormatSyntaxError(t *testing.T) {
	src := `func broken( {`
	_, err := formatter.Format([]byte(src))
	if err == nil {
		t.Fatal("expected error for invalid syntax, got nil")
	}
}

func TestFormatIdempotentComplex(t *testing.T) {
	src := `
func fibonacci(int n): int {
    if (n <= 1) {
        return n;
    }
    return fibonacci(n - 1) + fibonacci(n - 2);
}

class Counter {
    int count = 0;

    func increment() {
        this.count++;
    }

    func get(): int {
        return this.count;
    }
}
`
	roundtrip(t, src)
}
