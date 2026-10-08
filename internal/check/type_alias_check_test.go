package check

import (
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/modules"
)

func aliasCheckDir(t *testing.T) string {
	dir := t.TempDir()
	writeModule(t, dir, "shapes.gb", "module shapes;\nexport class Shape { func Shape() {} }\n")
	writeModule(t, dir, "facade.gb", "module facade;\nimport shapes;\nfrom shapes import Shape;\nexport type Figure = shapes.Shape;\nexport type Money = decimal;\nexport func make(): Figure { return Shape(); }\n")
	return dir
}

func TestExportedTypeAliasIsPartOfModuleSurface(t *testing.T) {
	dir := aliasCheckDir(t)
	main := "import facade;\nfrom facade import Money;\nfunc area(facade.Figure f): Money { return 1.5; }\n"
	_, diags := Source(filepath.Join(dir, "main.gb"), main, Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true})
	for _, d := range diags {
		if d.Severity == SeverityError || d.Rule == "unused-import" {
			t.Fatalf("exported alias rejected: %+v", d)
		}
	}
}

func TestFromImportedNameIsNotReExported(t *testing.T) {
	dir := aliasCheckDir(t)
	main := "import facade;\nfunc area(facade.Shape s): int { return 0; }\n"
	_, diags := Source(filepath.Join(dir, "main.gb"), main, Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true})
	if !hasDiag(diags, "type", "facade has no exported type Shape") {
		t.Fatalf("expected from-imported name to be rejected, got %+v", diags)
	}
}

func TestStaticAnalysisExpandsExportedAliases(t *testing.T) {
	dir := aliasCheckDir(t)
	main := "import facade;\nfacade.Money m = 1.5;\nfacade.Money bad = \"x\";\n"
	_, diags := Source(filepath.Join(dir, "main.gb"), main, Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true})
	errors := 0
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors++
			if !strings.Contains(d.Message, "string") {
				t.Fatalf("decimal literal rejected for a decimal alias: %+v", d)
			}
		}
	}
	if errors != 1 {
		t.Fatalf("expected exactly the string mismatch, got %+v", diags)
	}
}

func TestAliasOnlyImportIsUsed(t *testing.T) {
	dir := aliasCheckDir(t)
	_, diags := Source(filepath.Join(dir, "facade.gb"), "module facade;\nimport shapes;\nexport type Figure = shapes.Shape;\n", Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true})
	for _, d := range diags {
		if d.Rule == "unused-import" {
			t.Fatalf("import used only by a type alias was reported unused: %+v", d)
		}
	}
}
