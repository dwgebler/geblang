package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"geblang/internal/modules"
)

func runInstall(args []string) {
	if len(args) == 1 && args[0] == "--workspace" {
		wd, err := os.Getwd()
		if err == nil {
			err = modules.InstallWorkspace(wd)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "install:", err)
			os.Exit(1)
		}
		return
	}
	// Find the manifest for the current working directory.
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "install: cannot determine working directory:", err)
		os.Exit(1)
	}
	r := modules.NewResolver(nil)
	manifest, err := r.FindManifest(wd)
	if err != nil || manifest == nil {
		fmt.Fprintln(os.Stderr, "install: no geblang.yaml found in current directory or ancestors")
		os.Exit(1)
	}
	manifestPath := manifest.Path
	lockPath := filepath.Join(manifest.Root, "geblang.lock")

	if len(args) == 0 {
		// Install all git dependencies declared in geblang.yaml.
		fmt.Println("Installing dependencies...")
		if err := modules.Install(manifestPath, lockPath); err != nil {
			fmt.Fprintln(os.Stderr, "install:", err)
			os.Exit(1)
		}
		fmt.Println("Done.")
		return
	}

	// geblang install <git-url>[@version] [<name>]
	rawArg := args[0]
	gitURL, version, _ := strings.Cut(rawArg, "@")
	name := ""
	if len(args) > 1 {
		name = args[1]
	}

	if !strings.Contains(gitURL, "/") {
		fmt.Fprintf(os.Stderr, "install: %q does not look like a git URL\n", gitURL)
		fmt.Fprintln(os.Stderr, "usage: geblang install <git-url>[@version] [<name>]")
		os.Exit(2)
	}

	fmt.Printf("Adding dependency %s...\n", gitURL)
	if err := modules.InstallOne(manifestPath, lockPath, gitURL, version, name); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	fmt.Println("Done.")
}
