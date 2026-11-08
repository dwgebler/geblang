package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/modules"
)

func writeModule(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUndefinedQualifiedTypeFlagsMissingProjectType(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "shapes.gb", "class Circle {}\n")
	mainPath := filepath.Join(dir, "main.gb")
	main := "import shapes;\nfunc area(shapes.Nope c): int { return 0; }\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	if !hasDiag(diags, "type", "shapes has no exported type Nope") {
		t.Fatalf("expected qualified-type diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "has no exported type") && d.Severity != SeverityError {
			t.Fatalf("qualified-type diagnostic must be an error, got severity %v", d.Severity)
		}
	}
}

func TestUndefinedQualifiedTypeAllowsRealType(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "shapes.gb", "export class Circle {}\n")
	mainPath := filepath.Join(dir, "main.gb")
	main := "import shapes;\nfunc area(shapes.Circle c): int { return 0; }\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	for _, d := range diags {
		if d.Rule == "type" && strings.Contains(d.Message, "no exported type") {
			t.Fatalf("real qualified type flagged: %+v", d)
		}
	}
}

func TestUndefinedQualifiedTypeBailsOnUnknownAlias(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.gb")
	// No import for `mystery`; an unresolved alias must stay silent.
	main := "func area(mystery.Nope c): int { return 0; }\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	for _, d := range diags {
		if d.Rule == "type" && strings.Contains(d.Message, "no exported type") {
			t.Fatalf("unknown alias should not be flagged: %+v", d)
		}
	}
}

func TestUndefinedQualifiedTypeInReturnAndGeneric(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "shapes.gb", "class Circle {}\n")
	mainPath := filepath.Join(dir, "main.gb")
	main := "import shapes;\nfunc build(): list<shapes.Nope> { return []; }\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	if !hasDiag(diags, "type", "shapes has no exported type Nope") {
		t.Fatalf("expected qualified-type diagnostic in generic arg, got %+v", diags)
	}
}

func TestUndefinedQualifiedTypeBailsOnNativeWithoutSymbols(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.gb")
	// `binary` is native-only (no stdlib source) and we pass no symbol
	// set, so its export set is unreadable and the check must stay silent.
	main := "import binary;\nfunc f(binary.Nope x): void {}\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	for _, d := range diags {
		if d.Rule == "type" && strings.Contains(d.Message, "no exported type") {
			t.Fatalf("native module without symbol set should bail: %+v", d)
		}
	}
}

func TestUndefinedQualifiedTypeFlagsNativeStdlibSourceType(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.gb")
	// image resolves to a stdlib source module, so a missing type is known.
	main := "import image;\nfunc f(image.Nope x): void {}\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	if !hasDiag(diags, "type", "image has no exported type Nope") {
		t.Fatalf("expected qualified-type diagnostic for stdlib source module, got %+v", diags)
	}
}

// Both runtimes accept a type the module declares without `export`.
func TestUndefinedQualifiedTypeAllowsUnexportedTypes(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "shapes.gb", "class Circle {}\ninterface Round {}\nenum Tint { Red }\nexport class Square {}\n")
	mainPath := filepath.Join(dir, "main.gb")
	main := "import shapes;\nfunc area(shapes.Circle c, shapes.Round r, shapes.Tint t, shapes.Square s, shapes.Oval o): int { return 0; }\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, main, opts)
	for _, name := range []string{"Circle", "Round", "Tint", "Square"} {
		if hasDiag(diags, "type", "shapes has no exported type "+name) {
			t.Errorf("declared type %s flagged: %+v", name, diags)
		}
	}
	if !hasDiag(diags, "type", "shapes has no exported type Oval") {
		t.Errorf("expected the undeclared type to be flagged, got %+v", diags)
	}
}
