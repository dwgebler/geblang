package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Workspace struct {
	Members []string `yaml:"members"`
}

// FindWorkspace searches ancestors independently of the member's own manifest.
func (r *Resolver) FindWorkspace(start string) (*Manifest, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		manifest, err := r.manifestIn(current)
		if err != nil {
			return nil, err
		}
		if manifest != nil && len(manifest.Workspace.Members) > 0 {
			return manifest, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, nil
		}
		current = parent
	}
}

func (r *Resolver) manifestIn(dir string) (*Manifest, error) {
	for _, name := range []string{"geblang.yaml", "geblang.yml", "geblang.json"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return r.LoadManifest(path)
		}
	}
	return nil, nil
}

func (r *Resolver) WorkspaceMembers(workspace *Manifest) ([]*Manifest, error) {
	var members []*Manifest
	names := map[string]string{}
	roots := map[string]bool{}
	for _, relative := range workspace.Workspace.Members {
		if relative == "" || filepath.IsAbs(relative) {
			return nil, fmt.Errorf("workspace %s: member must be a non-empty relative directory: %q", workspace.Path, relative)
		}
		root := filepath.Clean(filepath.Join(workspace.Root, relative))
		contained, err := filepath.Rel(workspace.Root, root)
		if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("workspace %s: member %q must be inside the workspace directory", workspace.Path, relative)
		}
		if roots[root] {
			continue
		}
		roots[root] = true
		member, err := r.manifestIn(root)
		if err != nil {
			return nil, fmt.Errorf("workspace member %q: %w", relative, err)
		}
		if member == nil || member.Name == "" {
			return nil, fmt.Errorf("workspace %s: member %q needs its own manifest with a package name", workspace.Path, relative)
		}
		if previous, exists := names[member.Name]; exists {
			return nil, fmt.Errorf("workspace %s: duplicate package name %q in %s and %s", workspace.Path, member.Name, previous, member.Path)
		}
		names[member.Name] = member.Path
		members = append(members, member)
	}
	return members, nil
}

func (r *Resolver) workspaceDependency(owner *Manifest, name string, dependency Dependency) (*Manifest, error) {
	if dependency.Path != "" || dependency.Git != "" || dependency.Version != "" {
		return nil, fmt.Errorf("package %s dependency %s: workspace cannot be combined with path, git or version", manifestName(owner), name)
	}
	workspace, err := r.FindWorkspace(owner.Root)
	if err != nil {
		return nil, err
	}
	if workspace == nil {
		return nil, fmt.Errorf("package %s dependency %s: no ancestor workspace declares this package", manifestName(owner), name)
	}
	members, err := r.WorkspaceMembers(workspace)
	if err != nil {
		return nil, err
	}
	var selected *Manifest
	ownerListed := owner.Path == workspace.Path
	for _, member := range members {
		if member.Path == owner.Path {
			ownerListed = true
		}
		if member.Name == name {
			selected = member
		}
	}
	if !ownerListed {
		return nil, fmt.Errorf("package %s is not a member of workspace %s", manifestName(owner), workspace.Path)
	}
	if selected == nil {
		return nil, fmt.Errorf("package %s dependency %s: no member with this package name in workspace %s", manifestName(owner), name, workspace.Path)
	}
	return selected, nil
}

// InstallWorkspace keeps each package's existing vendor and lockfile boundaries.
func InstallWorkspace(start string) error {
	r := NewResolver([]string{start})
	workspace, err := r.FindWorkspace(start)
	if err != nil {
		return err
	}
	if workspace == nil {
		return fmt.Errorf("install --workspace: no ancestor workspace found")
	}
	members, err := r.WorkspaceMembers(workspace)
	if err != nil {
		return err
	}
	packages := append([]*Manifest{workspace}, members...)
	sort.Slice(packages, func(i, j int) bool { return packages[i].Path < packages[j].Path })
	for _, manifest := range packages {
		for name, dependency := range manifest.Dependencies {
			if dependency.Workspace {
				if _, err := r.workspaceDependency(manifest, name, dependency); err != nil {
					return err
				}
			}
		}
	}
	visited := map[string]bool{}
	for _, manifest := range packages {
		if err := r.installWorkspacePackage(manifest, visited); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) installWorkspacePackage(manifest *Manifest, visited map[string]bool) error {
	if visited[manifest.Path] {
		return nil
	}
	visited[manifest.Path] = true
	if err := Install(manifest.Path, filepath.Join(manifest.Root, "geblang.lock")); err != nil {
		return fmt.Errorf("workspace package %s: %w", manifestName(manifest), err)
	}
	for _, name := range dependencyNames(manifest.Dependencies) {
		dependency := manifest.Dependencies[name]
		var member *Manifest
		var err error
		if dependency.Workspace {
			member, err = r.workspaceDependency(manifest, name, dependency)
		} else {
			root := filepath.Join(manifest.Root, "vendor", name)
			if dependency.Path != "" {
				root = resolveDependencyPath(manifest.Root, dependency.Path)
			}
			member, err = r.manifestIn(root)
		}
		if err != nil {
			return err
		}
		if member != nil {
			if err := r.installWorkspacePackage(member, visited); err != nil {
				return err
			}
		}
	}
	return nil
}
