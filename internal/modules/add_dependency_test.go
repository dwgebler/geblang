package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "geblang.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readManifest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddDependencyPreservesCommentsAndUnmodelledKeys(t *testing.T) {
	path := writeManifest(t, "# my app\nname: app\nversion: 0.1.0\nsource: src\nassets:\n  sourceDir: assets # bundled\n  entryPoints:\n    - app.ts\n")
	if err := addDependency(path, "gebweb", "github.com/dwgebler/gebweb", "v1.10.0"); err != nil {
		t.Fatal(err)
	}
	want := "# my app\nname: app\nversion: 0.1.0\nsource: src\nassets:\n  sourceDir: assets # bundled\n  entryPoints:\n    - app.ts\ndependencies:\n  gebweb:\n    git: https://github.com/dwgebler/gebweb\n    version: v1.10.0\n"
	if got := readManifest(t, path); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddDependencyReplacesExistingEntryAndKeepsOthers(t *testing.T) {
	path := writeManifest(t, "name: app\ndependencies:\n  local:\n    path: ../local\n  gebweb:\n    git: https://github.com/dwgebler/gebweb\n    version: v1.9.5\n")
	if err := addDependency(path, "gebweb", "https://github.com/dwgebler/gebweb", "v1.10.0"); err != nil {
		t.Fatal(err)
	}
	want := "name: app\ndependencies:\n  local:\n    path: ../local\n  gebweb:\n    git: https://github.com/dwgebler/gebweb\n    version: v1.10.0\n"
	if got := readManifest(t, path); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	r := &Resolver{Manifests: map[string]*Manifest{}}
	m, err := r.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies["gebweb"].Version != "v1.10.0" || m.Dependencies["local"].Path != "../local" {
		t.Fatalf("reloaded dependencies %+v", m.Dependencies)
	}
}

func TestAddDependencyOmitsEmptyVersionAndFillsEmptyBlock(t *testing.T) {
	path := writeManifest(t, "name: app\ndependencies:\n")
	if err := addDependency(path, "lib", "github.com/acme/lib", ""); err != nil {
		t.Fatal(err)
	}
	got := readManifest(t, path)
	want := "name: app\ndependencies:\n  lib:\n    git: https://github.com/acme/lib\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, unwanted := range []string{"workspace", "permissions", "package", "path:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("manifest gained %q:\n%s", unwanted, got)
		}
	}
}

func TestAddDependencyRejectsNonMappingManifest(t *testing.T) {
	path := writeManifest(t, "- not\n- a mapping\n")
	if err := addDependency(path, "lib", "github.com/acme/lib", ""); err == nil {
		t.Fatal("expected an error for a list manifest")
	}
}
