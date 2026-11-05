package modules_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/modules"
)

func writeWorkspaceFile(t *testing.T, root, relative, source string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkspace_ExplicitDependenciesResolveFromMemberAndRoot(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "geblang.yaml", "name: workspace\nworkspace:\n  members: [apps/api, packages/domain, packages/private]\ndependencies:\n  api: {workspace: true}\n")
	writeWorkspaceFile(t, root, "apps/api/geblang.yaml", "name: api\nsource: src\ndependencies:\n  domain: {workspace: true}\n")
	writeWorkspaceFile(t, root, "packages/domain/geblang.yaml", "name: domain\nsource: src\ndependencies:\n  external: {path: ../../external}\n")
	writeWorkspaceFile(t, root, "packages/private/geblang.yaml", "name: private\nsource: src\n")
	writeWorkspaceFile(t, root, "packages/private/src/hidden.gb", "module private.hidden;")
	domainPath := writeWorkspaceFile(t, root, "packages/domain/src/task.gb", "module domain.task;")
	writeWorkspaceFile(t, root, "external/geblang.yaml", "name: external\nsource: lib\n")
	externalPath := writeWorkspaceFile(t, root, "external/lib/tool/init.gb", "module external.tool;")

	for _, start := range []string{root, filepath.Join(root, "apps/api"), filepath.Join(root, "apps/api/src")} {
		t.Run(start, func(t *testing.T) {
			if err := os.MkdirAll(start, 0o755); err != nil {
				t.Fatal(err)
			}
			resolver := modules.NewResolver([]string{start})

			for name, want := range map[string]string{"domain.task": domainPath, "external.tool": externalPath} {
				got, err := resolver.Resolve(name)
				if err != nil || got != want {
					t.Fatalf("resolve %s = %s, %v; want %s", name, got, err, want)
				}
			}
			if _, err := resolver.Resolve("private.hidden"); err == nil {
				t.Fatal("unreferenced workspace member became importable")
			}
		})
	}
}

func TestWorkspace_InvalidDeclarationsReturnContextualErrors(t *testing.T) {
	cases := []struct {
		name, workspace, dependency, member, want string
	}{
		{"missing workspace", "name: root\n", "workspace: true", "name: domain\n", "no ancestor workspace"},
		{"unknown member", "workspace:\n  members: [app]\n", "workspace: true", "name: domain\n", "no member"},
		{"missing manifest", "workspace:\n  members: [app, missing]\n", "workspace: true", "name: domain\n", "needs its own manifest"},
		{"duplicate name", "workspace:\n  members: [app, domain]\n", "workspace: true", "name: app\n", "duplicate package name"},
		{"mixed path", "workspace:\n  members: [app, domain]\n", "workspace: true, path: ../domain", "name: domain\n", "cannot be combined"},
		{"mixed git", "workspace:\n  members: [app, domain]\n", "workspace: true, git: https://example.invalid/domain", "name: domain\n", "cannot be combined"},
		{"mixed version", "workspace:\n  members: [app, domain]\n", "workspace: true, version: 1.0.0", "name: domain\n", "cannot be combined"},
		{"unlisted owner", "workspace:\n  members: [domain]\n", "workspace: true", "name: domain\n", "not a member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspaceFile(t, root, "geblang.yaml", tc.workspace)
			writeWorkspaceFile(t, root, "app/geblang.yaml", "name: app\ndependencies:\n  domain: {"+tc.dependency+"}\n")
			writeWorkspaceFile(t, root, "domain/geblang.yaml", tc.member)
			resolver := modules.NewResolver([]string{filepath.Join(root, "app")})

			_, err := resolver.Resolve("domain.task")

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkspace_CyclesDoNotPreventResolution(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "geblang.yaml", "workspace:\n  members: [app, domain]\n")
	writeWorkspaceFile(t, root, "app/geblang.yaml", "name: app\ndependencies:\n  domain: {workspace: true}\n")
	writeWorkspaceFile(t, root, "domain/geblang.yaml", "name: domain\ndependencies:\n  app: {workspace: true}\n")
	want := writeWorkspaceFile(t, root, "domain/task.gb", "module domain.task;")

	got, err := modules.NewResolver([]string{filepath.Join(root, "app")}).Resolve("domain.task")

	if err != nil || got != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
}

func TestWorkspace_InstallLocalMembersDoesNotCreateLockfiles(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "geblang.yaml", "workspace:\n  members: [app, domain]\n")
	writeWorkspaceFile(t, root, "app/geblang.yaml", "name: app\ndependencies:\n  domain: {workspace: true}\n")
	writeWorkspaceFile(t, root, "domain/geblang.yaml", "name: domain\n")

	err := modules.InstallWorkspace(filepath.Join(root, "app"))

	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".", "app", "domain"} {
		if _, err := os.Stat(filepath.Join(root, dir, "geblang.lock")); !os.IsNotExist(err) {
			t.Fatalf("local-only package acquired a lockfile: %s, %v", dir, err)
		}
	}
}

func TestResolver_AmbiguousDependencySelectionIsStable(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "geblang.yaml", "name: app\ndependencies:\n  alpha: {path: alpha}\n  beta: {path: beta}\n")
	for _, name := range []string{"alpha", "beta"} {
		writeWorkspaceFile(t, root, name+"/geblang.yaml", "name: "+name+"\n")
		writeWorkspaceFile(t, root, name+"/shared/value.gb", "module shared.value;")
	}
	resolver := modules.NewResolver([]string{root})
	selected := map[string]bool{}

	for range 100 {
		path, err := resolver.Resolve("shared.value")
		if err != nil {
			t.Fatal(err)
		}
		selected[path] = true
	}

	if len(selected) != 1 {
		t.Fatalf("the same import selected different providers: %v", selected)
	}
}

func TestWorkspace_InstallsTransitiveGitDependencyIntoOwningPackage(t *testing.T) {
	root := t.TempDir()
	remote := t.TempDir()
	writeWorkspaceFile(t, remote, "geblang.yaml", "name: external_labels\nsource: lib\n")
	writeWorkspaceFile(t, remote, "lib/text.gb", "module external_labels.text;\nexport func value(): string { return \"external\"; }\n")
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"add", "."},
		{"-c", "user.name=Workspace Test", "-c", "user.email=workspace@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Add package"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = remote
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	writeWorkspaceFile(t, root, "geblang.yaml", "workspace:\n  members: [app]\n")
	writeWorkspaceFile(t, root, "app/geblang.yaml", "name: app\ndependencies:\n  shared: {path: ../shared}\n")
	writeWorkspaceFile(t, root, "shared/geblang.yaml", "name: shared\ndependencies:\n  labels: {git: 'file://"+filepath.ToSlash(remote)+"'}\n")

	err := modules.InstallWorkspace(root)

	if err != nil {
		t.Fatal(err)
	}
	resolver := modules.NewResolver([]string{filepath.Join(root, "app")})
	got, err := resolver.Resolve("external_labels.text")
	want := filepath.Join(root, "shared/vendor/labels/lib/text.gb")
	if err != nil || got != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(root, "shared/geblang.lock")); err != nil {
		t.Fatal(err)
	}
	if err := modules.InstallWorkspace(root); err != nil {
		t.Fatal(err)
	}
}
