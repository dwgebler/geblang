package lower_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestLowerFieldValueTypesLiteralsAndChecksAny(t *testing.T) {
	src := "class L { decimal total; float rate; int count; }\nfunc anyv(any v): any { return v; }\nlet l = L();\nl.total = 0;\nl.rate = -2;\nl.count = anyv(5);\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	for _, want := range []string{`gbDecimalLit("0")`, `float64(-2)`, `transpilert.ExpectFieldIntFast(`, `"L.count"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in:\n%s", want, body)
		}
	}
}

func TestLowerFieldValueDiagnosesAnyIntoNullable(t *testing.T) {
	src := "class L { ?string s; }\nfunc anyv(any v): any { return v; }\nlet l = L();\nl.s = anyv(3);\n"
	_, l := runLowerer(t, src)
	if !strings.Contains(fmt.Sprint(l.Errors()), "nullable field s is not supported by --native") {
		t.Fatalf("expected nullable diagnostic, got %v", l.Errors())
	}
}

func TestLowerConditionalLiteralBranchTakesFieldType(t *testing.T) {
	src := "class L { decimal total; float rate; }\nbool off = false;\ndecimal d = 2.5;\n?decimal n = null;\nlet l = L();\nl.total = off ? d : 3;\nl.rate = off ? 0.5f : 4;\ndecimal x = n ?? 5;\n"
	mod, l := runLowerer(t, src)
	if len(l.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors())
	}
	body := mod.MainBody().String()
	for _, want := range []string{`gbDecimalLit("3")`, `float64(4)`, `gbDecimalLit("5")`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "ExpectFieldDecimal(") {
		t.Fatalf("literal branch fell back to an untyped field check:\n%s", body)
	}
}

func TestLowerConditionalLiteralWithUntypedBranchDiagnoses(t *testing.T) {
	src := "class L { decimal total; }\nfunc anyv(any v): any { return v; }\nbool off = false;\nlet l = L();\nl.total = off ? anyv(1) : 3;\n"
	_, l := runLowerer(t, src)
	if !strings.Contains(fmt.Sprint(l.Errors()), "mixing an integer literal with an untyped branch is not supported by --native") {
		t.Fatalf("expected an untyped-branch diagnostic, got %v", l.Errors())
	}
}
