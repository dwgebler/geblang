package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAnyArgumentsReachOverloadedMethods(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
class C {
    func m(int x): string { return "a"; }
    func m(string x): string { return "b"; }
    static func s(int x): string { return "a"; }
    static func s(string x): string { return "b"; }
}
any v = 1;
let c = C();
let a = c.m(v);
let b = C.s(v);
`
	_, diags := Source(file, src, classCheckOpts(dir))
	if hasError(diags) {
		t.Fatalf("any arguments must not be rejected statically: %v", diags)
	}
}

func TestCheckLiteralMismatchToOverloadedMethodStillErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
class C {
    func m(int x): string { return "a"; }
    func m(string x): string { return "b"; }
}
let c = C();
let a = c.m(true);
`
	_, diags := Source(file, src, classCheckOpts(dir))
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "no matching overload for C.m: got (bool)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a no-matching-overload error for a bool literal, got %v", diags)
	}
}
