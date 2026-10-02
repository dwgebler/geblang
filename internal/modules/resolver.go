package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	rootembed "geblang"
	"geblang/internal/ast"
	"geblang/internal/ffi"
	"geblang/internal/native"
	"geblang/internal/version"

	yamllib "gopkg.in/yaml.v3"
)

// ReservedNamespace is the top-level namespace reserved for built-in modules
// (`geblang`). Defined canonically in the ast package.
const ReservedNamespace = ast.ReservedModuleNamespace

type Resolver struct {
	ModulePaths   []string
	StdlibPaths   []string
	DisableStdlib bool
	Manifests     map[string]*Manifest

	stdlibNamesCache map[string]bool
}

type Manifest struct {
	Path         string
	Root         string
	Name         string
	Version      string
	Source       string
	Paths        []string
	Resources    []string
	Dependencies map[string]Dependency
	Permissions  ManifestPermissions
}

// ManifestPermissions is the geblang.yaml permissions block.
type ManifestPermissions struct {
	FFI            *ffi.PolicyConfig
	Onnx           bool
	ProcessControl bool
	Browser        bool
}

type Dependency struct {
	Path    string `yaml:"path"`
	Git     string `yaml:"git"`
	Version string `yaml:"version"`
}

func (d *Dependency) UnmarshalYAML(value *yamllib.Node) error {
	switch value.Kind {
	case yamllib.ScalarNode:
		d.Path = value.Value
		return nil
	case yamllib.MappingNode:
		type dependency Dependency
		var parsed dependency
		if err := value.Decode(&parsed); err != nil {
			return err
		}
		*d = Dependency(parsed)
		return nil
	default:
		return fmt.Errorf("dependency must be a path string or mapping")
	}
}

type manifestFile struct {
	Name         string                `yaml:"name"`
	Version      string                `yaml:"version"`
	Source       string                `yaml:"source"`
	Paths        []string              `yaml:"paths"`
	ModulePaths  []string              `yaml:"modulePaths"`
	Resources    []string              `yaml:"resources"`
	Dependencies map[string]Dependency `yaml:"dependencies"`
	Package      struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"package"`
	Permissions struct {
		FFI            *ffi.PolicyConfig `yaml:"ffi"`
		Onnx           bool              `yaml:"onnx"`
		ProcessControl bool              `yaml:"processControl"`
		Browser        bool              `yaml:"browser"`
	} `yaml:"permissions"`
}

type moduleRoot struct {
	path     string
	manifest *Manifest
}

func NewResolver(modulePaths []string) *Resolver {
	return &Resolver{ModulePaths: append([]string(nil), modulePaths...), StdlibPaths: DefaultStdlibPaths(), Manifests: map[string]*Manifest{}}
}

// topComponent returns the first dotted component of a canonical module name.
func topComponent(canonical string) string {
	if i := strings.IndexByte(canonical, '.'); i >= 0 {
		return canonical[:i]
	}
	return canonical
}

// stdlibModuleNames is the set of top-level module names shipped on the stdlib
// path (cached). A top-level `.gb` file or directory there is a module name.
func (r *Resolver) stdlibModuleNames() map[string]bool {
	if r.stdlibNamesCache != nil {
		return r.stdlibNamesCache
	}
	names := map[string]bool{}
	for _, base := range r.StdlibPaths {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				names[name] = true
			} else if strings.HasSuffix(name, ".gb") {
				names[strings.TrimSuffix(name, ".gb")] = true
			}
		}
	}
	r.stdlibNamesCache = names
	return names
}

// IsReservedModuleName reports whether the top-level component of canonical
// names a built-in module (native or stdlib) or the reserved geblang namespace.
// A user/program/package module may not use such a name.
func (r *Resolver) IsReservedModuleName(canonical string) bool {
	top := topComponent(canonical)
	if top == ReservedNamespace {
		return true
	}
	if _, ok := native.NativeModuleNames[top]; ok {
		return true
	}
	return r.stdlibModuleNames()[top]
}

// resolveStdlibOnly resolves canonical strictly against the stdlib path,
// ignoring user/program/package paths. Used for reserved built-in names and the
// geblang. prefix so user files can never shadow a built-in. A not-found result
// (e.g. a native-only module with no stdlib source) lets the caller fall back
// to the native built-in.
func (r *Resolver) resolveStdlibOnly(canonical string) (string, error) {
	if r.DisableStdlib {
		return "", fmt.Errorf("cannot resolve module %q", canonical)
	}
	baseRelative := filepath.Join(strings.Split(canonical, ".")...)
	candidates := []string{baseRelative + ".gb", filepath.Join(baseRelative, "init.gb")}
	for _, base := range r.StdlibPaths {
		for _, relative := range candidates {
			candidate := filepath.Clean(filepath.Join(base, relative))
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("cannot resolve module %q", canonical)
}

func (r *Resolver) Resolve(canonical string) (string, error) {
	// `geblang.X` is the canonical built-in path: resolve X strictly against the
	// stdlib, never user/package files.
	if rest, ok := strings.CutPrefix(canonical, ReservedNamespace+"."); ok {
		return r.resolveStdlibOnly(rest)
	}
	// A reserved built-in name resolves only to the built-in; user/package files
	// may not shadow it (parity with the VM, which always treats these natively).
	if r.IsReservedModuleName(canonical) {
		return r.resolveStdlibOnly(canonical)
	}
	baseRelative := filepath.Join(strings.Split(canonical, ".")...)
	candidates := []string{baseRelative + ".gb", filepath.Join(baseRelative, "init.gb")}
	seen := map[string]bool{}
	for _, base := range r.searchPaths() {
		if base == "" {
			base = "."
		}
		for _, relative := range candidates {
			candidate := filepath.Clean(filepath.Join(base, relative))
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	packageRoots, err := r.packageModuleRoots()
	if err != nil {
		return "", err
	}
	for _, root := range packageRoots {
		for _, relativeBase := range relativeModuleBases(canonical, root.manifest.Name) {
			for _, relative := range []string{relativeBase + ".gb", filepath.Join(relativeBase, "init.gb")} {
				candidate := filepath.Clean(filepath.Join(root.path, relative))
				if seen[candidate] {
					continue
				}
				seen[candidate] = true
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate, nil
				}
			}
		}
	}
	for _, relative := range candidates {
		if info, err := os.Stat(relative); err == nil && !info.IsDir() {
			return relative, nil
		}
	}
	return "", fmt.Errorf("cannot resolve module %q", canonical)
}

func (r *Resolver) searchPaths() []string {
	paths := append([]string(nil), r.ModulePaths...)
	if !r.DisableStdlib {
		paths = append(paths, r.StdlibPaths...)
	}
	if env := os.Getenv("GEBLANG_PATH"); env != "" {
		paths = append(paths, filepath.SplitList(env)...)
	}
	return cleanExistingSearchPaths(paths)
}

func DefaultStdlibPaths() []string {
	disk := cleanExistingSearchPaths(gatherStdlibDiskPaths())
	if len(disk) > 0 {
		return disk
	}
	// No stdlib on disk (e.g. a bare global install): fall back to the copy embedded in the binary.
	if dir := ensureEmbeddedStdlib(); dir != "" {
		return []string{dir}
	}
	return disk
}

func gatherStdlibDiskPaths() []string {
	paths := []string{}
	if env := os.Getenv("GEBLANG_STDLIB"); env != "" {
		paths = append(paths, filepath.SplitList(env)...)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(exeDir, "stdlib"),
			filepath.Join(exeDir, "..", "share", "geblang", "stdlib"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, ancestorStdlibPaths(wd)...)
	}
	return paths
}

// ensureEmbeddedStdlib extracts the embedded stdlib to a per-version cache dir once and returns its path (empty on failure).
func ensureEmbeddedStdlib() string {
	cache, err := os.UserCacheDir()
	if err != nil || cache == "" {
		cache = os.TempDir()
	}
	root := filepath.Join(cache, "geblang")
	hash, err := embeddedStdlibHash(rootembed.StdlibFS)
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, "stdlib-"+version.Geblang+"-"+hash[:16])
	if hasManifest(dir) {
		return dir
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return ""
	}
	tmp, err := os.MkdirTemp(root, "stdlib-tmp-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	if err := extractEmbeddedStdlib(tmp); err != nil {
		return ""
	}
	if err := os.Rename(tmp, dir); err != nil {
		if hasManifest(dir) {
			return dir
		}
		return ""
	}
	return dir
}

func embeddedStdlibHash(source fs.FS) (string, error) {
	digest := sha256.New()
	err := fs.WalkDir(source, "stdlib", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(source, p)
		if err != nil {
			return err
		}
		_, _ = digest.Write([]byte(p))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(content)
		_, _ = digest.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func extractEmbeddedStdlib(dst string) error {
	return fs.WalkDir(rootembed.StdlibFS, "stdlib", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "stdlib"), "/")
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := rootembed.StdlibFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func ancestorStdlibPaths(start string) []string {
	paths := []string{}
	current, err := filepath.Abs(start)
	if err != nil {
		current = filepath.Clean(start)
	}
	for {
		candidate := filepath.Join(current, "stdlib")
		if hasManifest(candidate) {
			paths = append(paths, candidate)
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return paths
}

func cleanExistingSearchPaths(paths []string) []string {
	cleaned := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" {
			path = "."
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		if path != "." {
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				continue
			}
		}
		seen[path] = true
		cleaned = append(cleaned, path)
	}
	return cleaned
}

func hasManifest(dir string) bool {
	for _, name := range []string{"geblang.yaml", "geblang.yml", "geblang.json"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func (r *Resolver) packageModuleRoots() ([]moduleRoot, error) {
	roots := []moduleRoot{}
	seenRoots := map[string]bool{}
	seenManifests := map[string]bool{}
	for _, base := range r.searchPaths() {
		if base == "" {
			base = "."
		}
		manifest, err := r.FindManifest(base)
		if err != nil {
			return nil, err
		}
		if manifest == nil {
			continue
		}
		if err := r.collectPackageModuleRoots(manifest, seenManifests, seenRoots, &roots); err != nil {
			return nil, err
		}
	}
	return roots, nil
}

func (r *Resolver) collectPackageModuleRoots(manifest *Manifest, seenManifests map[string]bool, seenRoots map[string]bool, roots *[]moduleRoot) error {
	if manifest == nil {
		return nil
	}
	if seenManifests[manifest.Path] {
		return nil
	}
	seenManifests[manifest.Path] = true
	for _, rootPath := range manifest.ModuleRoots() {
		if seenRoots[rootPath] {
			continue
		}
		seenRoots[rootPath] = true
		*roots = append(*roots, moduleRoot{path: rootPath, manifest: manifest})
	}
	for name, dependency := range manifest.Dependencies {
		var dependencyRoot string
		if dependency.Git != "" && dependency.Path == "" {
			// Git dependency: resolved from vendor/<name> adjacent to this manifest.
			vendorPath := filepath.Join(manifest.Root, "vendor", name)
			if _, err := os.Stat(vendorPath); err != nil {
				// Not yet installed; skip (user must run geblang install).
				continue
			}
			dependencyRoot = vendorPath
		} else if dependency.Path != "" {
			dependencyRoot = resolveDependencyPath(manifest.Root, dependency.Path)
		} else {
			return fmt.Errorf("package %s dependency %s has no path or git URL", manifestName(manifest), name)
		}
		dependencyManifest, err := r.FindManifest(dependencyRoot)
		if err != nil {
			return err
		}
		if dependencyManifest == nil {
			dependencyManifest = &Manifest{
				Path:         filepath.Clean(filepath.Join(dependencyRoot, "geblang.yaml")),
				Root:         dependencyRoot,
				Name:         name,
				Dependencies: map[string]Dependency{},
			}
		}
		if err := r.collectPackageModuleRoots(dependencyManifest, seenManifests, seenRoots, roots); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) FindManifest(start string) (*Manifest, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		current = filepath.Clean(start)
	}
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		for _, name := range []string{"geblang.yaml", "geblang.yml", "geblang.json"} {
			path := filepath.Join(current, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return r.LoadManifest(path)
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, nil
		}
		current = parent
	}
}

func (r *Resolver) LoadManifest(path string) (*Manifest, error) {
	path = filepath.Clean(path)
	if r.Manifests == nil {
		r.Manifests = map[string]*Manifest{}
	}
	if manifest, ok := r.Manifests[path]; ok {
		return manifest, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed manifestFile
	if err := yamllib.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse package manifest %s: %w", path, err)
	}
	name := parsed.Name
	if name == "" {
		name = parsed.Package.Name
	}
	version := parsed.Version
	if version == "" {
		version = parsed.Package.Version
	}
	paths := append([]string(nil), parsed.Paths...)
	paths = append(paths, parsed.ModulePaths...)
	manifest := &Manifest{
		Path:         path,
		Root:         filepath.Dir(path),
		Name:         name,
		Version:      version,
		Source:       parsed.Source,
		Paths:        paths,
		Resources:    append([]string(nil), parsed.Resources...),
		Dependencies: parsed.Dependencies,
		Permissions: ManifestPermissions{
			FFI:            parsed.Permissions.FFI,
			Onnx:           parsed.Permissions.Onnx,
			ProcessControl: parsed.Permissions.ProcessControl,
			Browser:        parsed.Permissions.Browser,
		},
	}
	if manifest.Dependencies == nil {
		manifest.Dependencies = map[string]Dependency{}
	}
	for name, dep := range manifest.Dependencies {
		if dep.Git != "" {
			dep.Git = normalizeGitURL(dep.Git)
			manifest.Dependencies[name] = dep
		}
	}
	r.Manifests[path] = manifest
	return manifest, nil
}

// resolveDependencyPath expands ~ and $VAR, then uses an absolute path as-is or joins it to root.
func resolveDependencyPath(root, p string) string {
	p = expandManifestPath(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(root, p))
}

func expandManifestPath(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	} else if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

func (m *Manifest) ModuleRoots() []string {
	roots := []string{}
	if m.Source != "" {
		roots = append(roots, filepath.Clean(filepath.Join(m.Root, m.Source)))
	} else {
		roots = append(roots, m.Root)
	}
	for _, path := range m.Paths {
		if path == "" {
			continue
		}
		roots = append(roots, filepath.Clean(filepath.Join(m.Root, path)))
	}
	return roots
}

func manifestName(manifest *Manifest) string {
	if manifest.Name != "" {
		return manifest.Name
	}
	return manifest.Root
}

func relativeModuleBases(canonical, packageName string) []string {
	bases := []string{filepath.Join(strings.Split(canonical, ".")...)}
	if packageName == "" {
		return bases
	}
	if canonical == packageName {
		return append(bases, "init")
	}
	prefix := packageName + "."
	if strings.HasPrefix(canonical, prefix) {
		stripped := strings.TrimPrefix(canonical, prefix)
		bases = append(bases, filepath.Join(strings.Split(stripped, ".")...))
	}
	return bases
}
