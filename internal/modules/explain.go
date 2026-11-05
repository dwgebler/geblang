package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"geblang/internal/native"
)

type Candidate struct {
	Path     string
	Origin   string
	Manifest string
	Exists   bool
}

type Resolution struct {
	Module      string
	Path        string
	Native      bool
	Candidates  []Candidate
	Diagnostics []string
}

func (r Resolution) Ambiguous() bool {
	matches := 0
	for _, candidate := range r.Candidates {
		if candidate.Exists {
			matches++
		}
	}
	return matches > 1
}

func (r *Resolver) Explain(canonical string) (Resolution, error) {
	result := Resolution{Module: canonical}
	err := r.walkCandidates(canonical, func(candidate Candidate) bool {
		result.Candidates = append(result.Candidates, candidate)
		if result.Path == "" && candidate.Exists {
			result.Path = candidate.Path
		}
		return false
	})
	if err != nil {
		if result.Path == "" {
			return result, err
		}
		result.Diagnostics = append(result.Diagnostics, err.Error())
	}
	if result.Path != "" {
		return result, nil
	}
	result.Native = native.IsNativeModule(strings.TrimPrefix(canonical, ReservedNamespace+"."))
	if result.Native {
		return result, nil
	}
	return result, fmt.Errorf("cannot resolve module %q", canonical)
}

// walkCandidates preserves lazy first-match resolution for normal execution.
func (r *Resolver) walkCandidates(canonical string, visit func(Candidate) bool) error {
	seen := map[string]bool{}
	try := func(base, relative, origin, manifest string) bool {
		path := filepath.Clean(filepath.Join(base, relative))
		key, err := filepath.Abs(path)
		if err != nil {
			key = path
		}
		if seen[key] {
			return false
		}
		seen[key] = true
		info, err := os.Stat(path)
		return visit(Candidate{Path: path, Origin: origin, Manifest: manifest, Exists: err == nil && !info.IsDir()})
	}
	base := filepath.Join(strings.Split(canonical, ".")...)
	if rest, qualified := strings.CutPrefix(canonical, ReservedNamespace+"."); qualified || r.IsReservedModuleName(canonical) {
		if qualified {
			base = filepath.Join(strings.Split(rest, ".")...)
		}
		if !r.DisableStdlib {
			for _, root := range r.StdlibPaths {
				for _, relative := range []string{base + ".gb", filepath.Join(base, "init.gb")} {
					if try(root, relative, "stdlib", "") {
						return nil
					}
				}
			}
		}
		return nil
	}
	for _, root := range r.searchPaths() {
		for _, relative := range []string{base + ".gb", filepath.Join(base, "init.gb")} {
			if try(root, relative, "search path", "") {
				return nil
			}
		}
	}
	roots, err := r.packageModuleRoots()
	if err != nil {
		return err
	}
	for _, root := range roots {
		for _, relativeBase := range relativeModuleBases(canonical, root.manifest.Name) {
			for _, relative := range []string{relativeBase + ".gb", filepath.Join(relativeBase, "init.gb")} {
				if try(root.path, relative, "package "+root.manifest.Name, root.manifest.Path) {
					return nil
				}
			}
		}
	}
	for _, relative := range []string{base + ".gb", filepath.Join(base, "init.gb")} {
		if try(".", relative, "working directory", "") {
			return nil
		}
	}
	return nil
}
