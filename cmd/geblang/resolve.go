package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"geblang/internal/modules"
)

func runResolve(args []string) {
	os.Exit(resolveCommand(args, os.Stdout, os.Stderr))
}

func resolveCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	explain := flags.Bool("explain", false, "show candidate paths and manifests")
	strict := flags.Bool("strict", false, "fail when multiple files provide the module")
	from := flags.String("from", ".", "resolve from this file or directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: geblang resolve [--explain] [--strict] [--from path] <module>")
		return 2
	}
	start, err := filepath.Abs(*from)
	if err != nil {
		fmt.Fprintln(stderr, "resolve:", err)
		return 1
	}
	if info, statErr := os.Stat(start); statErr != nil {
		fmt.Fprintln(stderr, "resolve:", statErr)
		return 1
	} else if !info.IsDir() {
		start = filepath.Dir(start)
	}
	resolver := modules.NewResolver([]string{start})
	result, resolveErr := resolver.Explain(flags.Arg(0))
	if *explain {
		if manifest, err := resolver.FindManifest(start); err == nil && manifest != nil {
			fmt.Fprintln(stdout, "manifest:", manifest.Path)
		}
		for _, candidate := range result.Candidates {
			status := "missing"
			if candidate.Exists {
				status = "shadowed"
				if candidate.Path == result.Path {
					status = "selected"
				}
			}
			fmt.Fprintf(stdout, "%s: %s (%s)\n", status, candidate.Path, candidate.Origin)
			if candidate.Manifest != "" {
				fmt.Fprintln(stdout, "  manifest:", candidate.Manifest)
			}
		}
	}
	if resolveErr != nil {
		fmt.Fprintln(stderr, "resolve:", resolveErr)
		return 1
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintln(stderr, "warning: remaining candidates could not be inspected:", diagnostic)
	}
	if result.Native {
		fmt.Fprintln(stdout, "builtin:", result.Module)
	} else {
		fmt.Fprintln(stdout, result.Path)
	}
	if result.Ambiguous() {
		fmt.Fprintf(stderr, "warning: module %q has multiple providers; the first matching file is selected\n", result.Module)
		if *strict {
			return 1
		}
	}
	return 0
}
