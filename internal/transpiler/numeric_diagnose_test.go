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

func nativeDiagnostics(t *testing.T, src, substr string) []string {
	t.Helper()
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
	var found []string
	for _, d := range diags {
		if d.Severity == transpiler.SeverityError && strings.Contains(d.Message, substr) {
			found = append(found, d.Message)
		}
	}
	return found
}

// An int64-overflowing constant must diagnose, never emit Go that fails to compile.
func TestIntConstantOverflowDiagnosesUnderNative(t *testing.T) {
	for _, expr := range []string{
		"9223372036854775807 + 1",
		"9223372036854775807 * 2",
		"0 - 9223372036854775807 - 2",
		"9223372036854775808",
		"-9223372036854775809",
	} {
		src := "import io;\nio.println(" + expr + ");\n"
		if got := nativeDiagnostics(t, src, "overflows int64"); len(got) != 1 {
			t.Errorf("%s: expected one overflow diagnostic, got %v", expr, got)
		}
	}
}

func TestIntConstantInRangeDoesNotDiagnose(t *testing.T) {
	for _, expr := range []string{
		"-9223372036854775808",
		"(9223372036854775807 + 1) - 1",
		"4611686018427387904 * 2 - 1",
	} {
		src := "import io;\nio.println(" + expr + ");\n"
		if got := nativeDiagnostics(t, src, "overflows int64"); len(got) != 0 {
			t.Errorf("%s: unexpected diagnostics %v", expr, got)
		}
	}
}

// Decimal operands lower to *big.Rat, so Go operators would not compile or would compare pointers.
func TestDecimalOperatorsDiagnoseUnderNative(t *testing.T) {
	for _, op := range []string{"+", "-", "*", "/", "%", "<", ">", "<=", ">=", "==", "!="} {
		src := "import io;\ndecimal a = 1.5;\ndecimal b = 2.5;\nio.println(a " + op + " b);\nio.println(3 " + op + " a);\n"
		if got := nativeDiagnostics(t, src, "on decimal values"); len(got) != 2 {
			t.Errorf("%s: expected two decimal diagnostics, got %v", op, got)
		}
	}
	src := "import io;\ndecimal a = 1.5;\nio.println(-a);\n"
	if got := nativeDiagnostics(t, src, "on decimal values"); len(got) != 1 {
		t.Errorf("unary minus: expected one decimal diagnostic, got %v", got)
	}
}

func TestIntDivisionDiagnosesUnderNative(t *testing.T) {
	src := "import io;\nint a = 1;\nint b = 2;\nio.println(a / b);\nio.println(1 / 2);\n"
	if got := nativeDiagnostics(t, src, "int / int"); len(got) != 2 {
		t.Errorf("expected two int-division diagnostics, got %v", got)
	}
	src = "import io;\nfloat a = 1.0f;\nio.println(a / 2);\nio.println(\"x\" + 1.5);\n"
	if got := nativeDiagnostics(t, src, "the transpiler does not yet support"); len(got) != 0 {
		t.Errorf("float division and string concat must lower, got %v", got)
	}
}

// math aggregates have no native bridge yet; they must diagnose, not emit Go.
func TestMathAggregatesDiagnoseUnderNative(t *testing.T) {
	src := "import io;\nimport math;\nlist<int> xs = [1, 2];\nio.println(math.sum(xs));\nio.println(math.mean(xs));\n"
	if got := nativeDiagnostics(t, src, "no transpiler bridge for math."); len(got) != 2 {
		t.Errorf("expected two bridge diagnostics, got %v", got)
	}
}

func TestStatsDescriptiveDiagnoseUnderNative(t *testing.T) {
	src := "import io;\nimport stats;\nlist<int> xs = [1, 2];\nio.println(stats.variance(xs));\nio.println(stats.describe(xs));\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	_, diags, err := transpiler.Transpile(transpiler.Input{
		Modules: map[string]*ast.Program{"main": prog},
	}, transpiler.Options{EntryModule: "main", IntMode: types.IntModeFast})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	errs := 0
	for _, d := range diags {
		if d.Severity == transpiler.SeverityError {
			errs++
		}
	}
	if errs == 0 {
		t.Fatalf("expected stats calls to diagnose, got %v", diags)
	}
}
