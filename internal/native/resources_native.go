package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"geblang/internal/runtime"
)

func registerResourcesNative(r *Registry) {
	r.Register("resources_native", "root", func(args []runtime.Value) (runtime.Value, error) {
		root, err := singleString(args, "resources_native.root")
		if err != nil {
			return nil, err
		}
		resolved, err := resourceRoot(root)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: resolved}, nil
	})
	r.Register("resources_native", "path", func(args []runtime.Value) (runtime.Value, error) {
		resolved, exists, err := resourceLookup(args)
		if err != nil {
			return nil, err
		}
		if !exists {
			name := args[1].(runtime.String).Value
			return nil, fmt.Errorf("resources: %q is not a declared, readable resource", name)
		}
		return runtime.String{Value: resolved}, nil
	})
	r.Register("resources_native", "exists", func(args []runtime.Value) (runtime.Value, error) {
		_, exists, err := resourceLookup(args)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: exists}, nil
	})
}

func resourceLookup(args []runtime.Value) (string, bool, error) {
	if len(args) != 3 {
		return "", false, fmt.Errorf("resources: expected root, name, and mode")
	}
	rootArg, ok := args[0].(runtime.String)
	if !ok {
		return "", false, fmt.Errorf("resources: root must be a string")
	}
	nameArg, ok := args[1].(runtime.String)
	if !ok {
		return "", false, fmt.Errorf("resources: name must be a string")
	}
	modeArg, ok := args[2].(runtime.String)
	if !ok {
		return "", false, fmt.Errorf("resources: mode must be a string")
	}
	name := nameArg.Value
	if err := resourceName(name); err != nil {
		return "", false, err
	}
	root, err := resourceRoot(rootArg.Value)
	if err != nil {
		return "", false, err
	}
	declared, err := resourceDeclared(root, name, modeArg.Value)
	if err != nil {
		return "", false, err
	}
	if !declared {
		return "", false, nil
	}
	candidate := filepath.Join(root, filepath.FromSlash(name))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("resources: %q: %w", name, err)
		}
		for parent := filepath.Dir(candidate); resourceWithin(root, parent); parent = filepath.Dir(parent) {
			resolvedParent, parentErr := filepath.EvalSymlinks(parent)
			if parentErr == nil {
				if !resourceWithin(root, resolvedParent) {
					return "", false, fmt.Errorf("resources: %q escapes the resource root", name)
				}
				break
			}
			if !os.IsNotExist(parentErr) {
				return "", false, fmt.Errorf("resources: %q: %w", name, parentErr)
			}
		}
		return "", false, nil
	}
	if !resourceWithin(root, resolved) {
		return "", false, fmt.Errorf("resources: %q escapes the resource root", name)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resources: %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("resources: %q is not a regular file", name)
	}
	file, err := os.Open(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resources: %q: %w", name, err)
	}
	_ = file.Close()
	return resolved, true, nil
}

func resourceName(name string) error {
	if name == "" || name == "." || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") ||
		path.IsAbs(name) || filepath.IsAbs(name) || path.Clean(name) != name ||
		len(name) >= 2 && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) && name[1] == ':' {
		return fmt.Errorf("resources: invalid resource name %q", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return fmt.Errorf("resources: invalid resource name %q", name)
		}
	}
	return nil
}

func resourceRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("resources: resource root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resources: root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resources: root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("resources: root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("resources: root %q is not a directory", root)
	}
	return resolved, nil
}

func resourceWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resourceDeclared(root, name, mode string) (bool, error) {
	switch mode {
	case "explicit":
		return true, nil
	case "bundle":
		content, err := os.ReadFile(filepath.Join(root, "BUNDLE.json"))
		if err != nil {
			return false, fmt.Errorf("resources: read bundle manifest: %w", err)
		}
		var manifest struct {
			Resources []string
		}
		if err := json.Unmarshal(content, &manifest); err != nil {
			return false, fmt.Errorf("resources: parse bundle manifest: %w", err)
		}
		for _, declared := range manifest.Resources {
			if declared == name {
				return true, nil
			}
		}
		return false, nil
	case "manifest":
		return resourceDeclaredByManifest(root, name)
	default:
		return false, fmt.Errorf("resources: unknown root mode %q", mode)
	}
}

func resourceDeclaredByManifest(root, name string) (bool, error) {
	var content []byte
	var err error
	found := false
	for _, filename := range []string{"geblang.yaml", "geblang.yml", "geblang.json"} {
		content, err = os.ReadFile(filepath.Join(root, filename))
		if err == nil {
			found = true
			break
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("resources: read manifest: %w", err)
		}
	}
	if !found {
		return false, fmt.Errorf("resources: no package manifest in %s", root)
	}
	var manifest struct {
		Resources []string
	}
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return false, fmt.Errorf("resources: parse manifest: %w", err)
	}
	candidate := filepath.Join(root, filepath.FromSlash(name))
	for _, spec := range manifest.Resources {
		if spec == "" || filepath.IsAbs(spec) {
			continue
		}
		pattern := filepath.Join(root, filepath.FromSlash(spec))
		if !resourceWithin(root, pattern) {
			continue
		}
		matches := []string{pattern}
		if _, err := os.Stat(pattern); os.IsNotExist(err) {
			matches, err = filepath.Glob(pattern)
			if err != nil {
				return false, fmt.Errorf("resources: invalid manifest glob %q: %w", spec, err)
			}
		} else if err != nil {
			return false, fmt.Errorf("resources: stat %q: %w", spec, err)
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return false, fmt.Errorf("resources: stat %q: %w", spec, err)
			}
			if info.IsDir() {
				if candidate != match && resourceWithin(match, candidate) {
					return true, nil
				}
			} else if candidate == match {
				return true, nil
			}
		}
	}
	return false, nil
}
