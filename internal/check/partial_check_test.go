package check

import (
	"path/filepath"
	"testing"

	"geblang/internal/modules"
)

func partialCheckOpts(dir string) Options {
	return Options{Resolver: modules.NewResolver([]string{dir})}
}

func hasError(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

func hasRule(diags []Diagnostic, rule string) bool {
	for _, d := range diags {
		if d.Rule == rule {
			return true
		}
	}
	return false
}

func TestCheckValidPartialNoDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
func add(int a, int b): int { return a + b; }
let f = add(_, 10);
let result = f(1);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if len(diags) != 0 {
		t.Fatalf("valid partial produced diagnostics: %v", diags)
	}
}

func TestCheckPartialAnalyzerVisitsBoundArg(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
func add(int a, int b): int { return a + b; }
let x = 5;
del x;
let f = add(x, _);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if !hasError(diags) {
		t.Fatalf("expected 'use of destroyed binding' error for del'd arg in partial, got %v", diags)
	}
}

func TestCheckHoleWithSpreadIsError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
func log(string a, string b): void {}
let f = log(_, ...rest);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if !hasError(diags) {
		t.Fatalf("expected an error for _ mixed with spread, got %v", diags)
	}
}

// Named dynamic argument to ambiguous overloads: eval resolves at runtime, VM rejects at compile; check reports vm-unsupported, not error.
func TestCheckNamedAmbiguousOverloadIsVMUnsupported(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
func describe(int n): string { return "int"; }
func describe(string n): string { return "str"; }
any v = 42;
let a = describe(n: v);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if hasError(diags) {
		t.Fatalf("ambiguous named overload call must be a warning, not an error: %v", diags)
	}
	if !hasRule(diags, "vm-unsupported") {
		t.Fatalf("expected a vm-unsupported warning for the ambiguous named overload call, got %v", diags)
	}
}

func TestCheckPartialOverAmbiguousOverloadRunsOnVM(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
func describe(int n): string { return "int"; }
func describe(string s): string { return "str"; }
let d = describe(_);
let a = d(42);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if hasError(diags) || hasRule(diags, "vm-unsupported") {
		t.Fatalf("partial over overloads must check clean, got %v", diags)
	}
}

// A module function used as a partial target counts as using its import.
func TestCheckModuleFnAsPartialTargetCountsAsUse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `
import math;
let f = math.max(0, _);
let r = f(5);
`
	_, diags := Source(file, src, partialCheckOpts(dir))
	if hasRule(diags, "unused-import") {
		t.Fatalf("math used as a partial target must not warn unused-import: %v", diags)
	}
}
