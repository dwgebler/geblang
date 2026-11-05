package modules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
	yamllib "gopkg.in/yaml.v3"
)

// LockFile records the resolved commit SHA for each git dependency.
type LockFile struct {
	Dependencies map[string]LockEntry `yaml:"dependencies"`
}

// LockEntry is one vendored git dependency in geblang.lock.
type LockEntry struct {
	URL     string `yaml:"url"`
	Version string `yaml:"version,omitempty"`
	Commit  string `yaml:"commit"`
}

// Install fetches all git dependencies declared in manifestPath into
// <manifestRoot>/vendor/, pinning each resolved commit in lockPath.
// Local path dependencies are skipped (they need no fetching).
func Install(manifestPath, lockPath string) error {
	r := &Resolver{Manifests: map[string]*Manifest{}}
	manifest, err := r.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("install: read manifest: %w", err)
	}
	for _, name := range dependencyNames(manifest.Dependencies) {
		dependency := manifest.Dependencies[name]
		if dependency.Workspace {
			if _, err := r.workspaceDependency(manifest, name, dependency); err != nil {
				return fmt.Errorf("install: %w", err)
			}
		}
	}
	vendorDir := filepath.Join(manifest.Root, "vendor")
	lock := readLockFile(lockPath)
	changed := false

	for _, name := range dependencyNames(manifest.Dependencies) {
		dep := manifest.Dependencies[name]
		if dep.Git == "" {
			continue
		}
		destDir := filepath.Join(vendorDir, name)
		if dep.Version != "latest" {
			if entry, ok := lock.Dependencies[name]; ok &&
				entry.URL == dep.Git && entry.Version == dep.Version {
				if _, err := os.Stat(destDir); err == nil {
					short := entry.Commit
					if len(short) > 8 {
						short = short[:8]
					}
					fmt.Printf("  %s: already installed (%s)\n", name, short)
					continue
				}
			}
		}
		resolvedVersion := dep.Version
		if dep.Version == "latest" {
			tag, err := resolveLatestSemverTag(dep.Git)
			if err != nil {
				return fmt.Errorf("install %s: resolve latest: %w", name, err)
			}
			resolvedVersion = tag
			fmt.Printf("  %s: latest resolved to %s\n", name, tag)
		}
		fmt.Printf("  %s: fetching %s", name, dep.Git)
		if resolvedVersion != "" {
			fmt.Printf(" @ %s", resolvedVersion)
		}
		fmt.Println()

		if err := os.RemoveAll(destDir); err != nil {
			return fmt.Errorf("install %s: clean vendor dir: %w", name, err)
		}
		args := []string{"clone", "--depth", "1"}
		if resolvedVersion != "" {
			args = append(args, "--branch", resolvedVersion)
		}
		args = append(args, dep.Git, destDir)
		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install %s: git clone: %w", name, err)
		}
		commit, _ := gitHead(destDir)
		if lock.Dependencies == nil {
			lock.Dependencies = map[string]LockEntry{}
		}
		lock.Dependencies[name] = LockEntry{URL: dep.Git, Version: dep.Version, Commit: commit}
		changed = true
	}

	if changed {
		if err := writeLockFile(lockPath, lock); err != nil {
			return fmt.Errorf("install: write lock file: %w", err)
		}
	}
	return nil
}

// InstallOne adds a git dependency to manifestPath, fetches it, and updates
// the lock file. If name is empty it is derived from the URL's last segment.
func InstallOne(manifestPath, lockPath, gitURL, version, name string) error {
	if name == "" {
		name = nameFromURL(gitURL)
	}
	if err := addDependency(manifestPath, name, gitURL, version); err != nil {
		return fmt.Errorf("install: update manifest: %w", err)
	}
	return Install(manifestPath, lockPath)
}

// normalizeGitURL adds https:// to a scheme-less host/path, leaving explicit schemes and scp-like git@host:path alone.
func normalizeGitURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" || strings.Contains(u, "://") {
		return u
	}
	if colon := strings.IndexByte(u, ':'); colon >= 0 {
		if slash := strings.IndexByte(u, '/'); slash < 0 || colon < slash {
			return u
		}
	}
	return "https://" + u
}

// nameFromURL derives a package name from the last segment of a git URL.
func nameFromURL(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return "package"
	}
	return parts[len(parts)-1]
}

func gitHead(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveLatestSemverTag picks the highest stable semver tag from `git ls-remote --tags`.
func resolveLatestSemverTag(gitURL string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "--tags", "--refs", gitURL).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote --tags %s: %w", gitURL, err)
	}
	tag, err := pickLatestSemverTag(string(out))
	if err != nil {
		return "", fmt.Errorf("%s: %w", gitURL, err)
	}
	return tag, nil
}

// pickLatestSemverTag parses ls-remote output and returns the highest stable
// semver tag. Pre-release tags (-rc, -beta, ...) only win when no stable tag
// exists.
func pickLatestSemverTag(lsRemoteOutput string) (string, error) {
	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(lsRemoteOutput), "\n") {
		_, ref, ok := strings.Cut(line, "refs/tags/")
		if !ok {
			continue
		}
		tag := strings.TrimSpace(ref)
		if !semver.IsValid(canonicalSemver(tag)) {
			continue
		}
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no semver tags found")
	}
	best := tags[0]
	bestStable := semver.Prerelease(canonicalSemver(best)) == ""
	for _, t := range tags[1:] {
		stable := semver.Prerelease(canonicalSemver(t)) == ""
		switch {
		case stable && !bestStable:
			best, bestStable = t, true
		case stable == bestStable:
			if semver.Compare(canonicalSemver(t), canonicalSemver(best)) > 0 {
				best = t
			}
		}
	}
	return best, nil
}

func canonicalSemver(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}

func readLockFile(path string) LockFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return LockFile{Dependencies: map[string]LockEntry{}}
	}
	var lock LockFile
	if err := yamllib.Unmarshal(data, &lock); err != nil {
		return LockFile{Dependencies: map[string]LockEntry{}}
	}
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]LockEntry{}
	}
	return lock
}

func writeLockFile(path string, lock LockFile) error {
	data, err := yamllib.Marshal(lock)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// addDependency updates or inserts a git dependency in manifestPath.
// The manifest is re-marshalled; comments are not preserved.
func addDependency(manifestPath, name, gitURL, version string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var mf manifestFile
	if err := yamllib.Unmarshal(data, &mf); err != nil {
		return err
	}
	if mf.Dependencies == nil {
		mf.Dependencies = map[string]Dependency{}
	}
	mf.Dependencies[name] = Dependency{Git: normalizeGitURL(gitURL), Version: version}
	out, err := yamllib.Marshal(&mf)
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, out, 0o644)
}
