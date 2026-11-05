package modules_test

import (
	"path/filepath"
	"testing"

	"geblang/internal/modules"
)

func TestExplain_PreservesSelectionAndReportsCompetingProviders(t *testing.T) {
	root := t.TempDir()
	first := writeWorkspaceFile(t, root, "one/shared/value.gb", "module shared.value;")
	writeWorkspaceFile(t, root, "two/shared/value/init.gb", "module shared.value;")
	resolver := modules.NewResolver([]string{filepath.Join(root, "one"), filepath.Join(root, "two")})
	resolver.DisableStdlib = true

	selected, err := resolver.Resolve("shared.value")
	if err != nil {
		t.Fatal(err)
	}
	report, err := resolver.Explain("shared.value")

	if err != nil || report.Path != selected || selected != first || !report.Ambiguous() {
		t.Fatalf("selection %s, report %+v, error %v", selected, report, err)
	}
}

func TestExplain_DuplicateSearchRootsDoNotCreateAmbiguity(t *testing.T) {
	root := t.TempDir()
	want := writeWorkspaceFile(t, root, "shared/value.gb", "module shared.value;")
	resolver := modules.NewResolver([]string{root, filepath.Join(root, ".")})

	report, err := resolver.Explain("shared.value")

	if err != nil || report.Path != want || report.Ambiguous() {
		t.Fatalf("report %+v, error %v", report, err)
	}
}

func TestExplain_BuiltinsIgnoreLocalProviders(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "io.gb", "module io;")
	resolver := modules.NewResolver([]string{root})
	for _, name := range []string{"io", "geblang.io"} {
		report, err := resolver.Explain(name)

		if err != nil || !report.Native || report.Ambiguous() {
			t.Fatalf("%s: report %+v, error %v", name, report, err)
		}
		for _, candidate := range report.Candidates {
			if candidate.Path == filepath.Join(root, "io.gb") {
				t.Fatal("reserved built-in considered a local provider")
			}
		}
	}
}

func TestExplain_MissingModuleIncludesSearchedPaths(t *testing.T) {
	root := t.TempDir()
	resolver := modules.NewResolver([]string{root})
	resolver.DisableStdlib = true

	report, err := resolver.Explain("missing.module")

	if err == nil || len(report.Candidates) == 0 || report.Path != "" {
		t.Fatalf("report %+v, error %v", report, err)
	}
}

func TestExplain_KeepsDirectSelectionWhenLaterPackageInspectionFails(t *testing.T) {
	root := t.TempDir()
	want := writeWorkspaceFile(t, root, "app/main.gb", "module app.main;")
	writeWorkspaceFile(t, root, "geblang.yaml", "name: app\ndependencies:\n  missing: {workspace: true}\n")
	resolver := modules.NewResolver([]string{root})

	selected, err := resolver.Resolve("app.main")
	if err != nil {
		t.Fatal(err)
	}
	report, err := resolver.Explain("app.main")

	if err != nil || selected != want || report.Path != selected || len(report.Diagnostics) != 1 {
		t.Fatalf("selection %s, report %+v, error %v", selected, report, err)
	}
}
