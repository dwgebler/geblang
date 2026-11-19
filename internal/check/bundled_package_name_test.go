package check

import (
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/parser"
)

func reservedDiagsFor(t *testing.T, file string, resolver *modules.Resolver) []Diagnostic {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New(string(src)))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	return checkReservedNames(file, program, Options{Resolver: resolver})
}

// A bundle flattens packages under src/ without their manifests; a package module's short name must still read as pkg.name.
func TestManifestLessPackageModuleUsesItsPathName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	stdlib := filepath.Join(root, "stdlib")
	for _, dir := range []string{filepath.Join(src, "webkit"), stdlib} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(stdlib, "i18n.gb"), "module i18n;\n")
	write(filepath.Join(src, "webkit", "i18n.gb"), "module i18n;\n")
	write(filepath.Join(src, "i18n.gb"), "module i18n;\n")
	resolver := &modules.Resolver{ModulePaths: []string{src}, StdlibPaths: []string{stdlib}, Manifests: map[string]*modules.Manifest{}}

	if got := CanonicalModuleName(filepath.Join(src, "webkit", "i18n.gb"), "i18n", resolver); got != "webkit.i18n" {
		t.Fatalf("canonical = %q, want webkit.i18n", got)
	}
	if diags := reservedDiagsFor(t, filepath.Join(src, "webkit", "i18n.gb"), resolver); len(diags) != 0 {
		t.Fatalf("package module flagged as reserved: %v", diags)
	}
	if diags := reservedDiagsFor(t, filepath.Join(src, "i18n.gb"), resolver); len(diags) != 1 {
		t.Fatalf("top-level module named after a stdlib module: diags = %v, want one", diags)
	}
}
