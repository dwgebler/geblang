package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/bundle"
	"geblang/internal/check"
	"geblang/internal/embedfold"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/parser"
	"geblang/internal/transpiler"
	"geblang/pkg/transpilert"
)

// runBuildNative transpiles the entry program to Go and compiles it to a
// self-contained native binary. The transpilert runtime is vendored into a
// temp module (replace geblang => ./geblangrt) so the build is offline and
// needs no geblang repo or published module. Any unsupported construct fails
// loudly and produces no binary, pointing at plain `geblang build`.
func runBuildNative(entry, outPath, absPkgDir string, entrySig entryMainSig) {
	resolver := modules.NewResolver([]string{absPkgDir})

	entryPath, err := resolver.Resolve(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "geblang build --native: cannot resolve entry module %q: %v\n", entry, err)
		os.Exit(1)
	}

	allModules, err := bundle.WalkImports(entry, entryPath, resolver, check.IsNativeImport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "geblang build --native: %v\n", err)
		os.Exit(1)
	}

	modulesAst := make(map[string]*ast.Program, len(allModules))
	sources := make(map[string]string, len(allModules))
	for canonical, absPath := range allModules {
		src, err := os.ReadFile(absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "geblang build --native: read %s: %v\n", absPath, err)
			os.Exit(1)
		}
		p := parser.New(lexer.New(string(src)))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) != 0 {
			fmt.Fprintf(os.Stderr, "geblang build --native: parse %s: %s\n", absPath, strings.Join(errs, "; "))
			os.Exit(1)
		}
		if _, diags := embedfold.Fold(prog, absPath, embedfold.Inline); len(diags) > 0 {
			d := diags[0]
			fmt.Fprintf(os.Stderr, "geblang build --native: %s:%d:%d: %s\n", absPath, d.Line, d.Column, d.Message)
			os.Exit(1)
		}
		modulesAst[canonical] = prog
		sources[canonical] = absPath
	}

	out, diags, err := transpiler.Transpile(transpiler.Input{
		Modules: modulesAst,
		Sources: sources,
	}, transpiler.Options{
		EntryModule:         entry,
		EntryMainWantsArgs:  entrySig.WantsArgs,
		EntryMainReturnsInt: entrySig.ReturnsInt,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "geblang build --native: transpile: %v\n", err)
		os.Exit(1)
	}

	if failNativeDiagnostics(diags) {
		os.Exit(1)
	}

	absOut, err := filepath.Abs(outPath)
	if err != nil {
		absOut = outPath
	}
	if err := buildNativeBinary(out, absOut, entry); err != nil {
		fmt.Fprintf(os.Stderr, "geblang build --native: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("geblang build --native: wrote %s\n", absOut)
}

// failNativeDiagnostics prints every error-severity diagnostic and the VM
// fallback hint, returning true if the build must abort.
func failNativeDiagnostics(diags []transpiler.Diagnostic) bool {
	var errs []transpiler.Diagnostic
	for _, d := range diags {
		if d.Severity == transpiler.SeverityError {
			errs = append(errs, d)
		}
	}
	if len(errs) == 0 {
		return false
	}
	fmt.Fprintln(os.Stderr, "geblang build --native: this program uses constructs the native compiler does not support:")
	for _, d := range errs {
		fmt.Fprintf(os.Stderr, "  %s:%d:%d: %s\n", d.File, d.Line, d.Column, d.Message)
		if d.Hint != "" {
			fmt.Fprintf(os.Stderr, "    hint: %s\n", d.Hint)
		}
	}
	fmt.Fprintln(os.Stderr, "geblang build --native cannot compile this program; use 'geblang build' for the bundled VM binary")
	return true
}

// buildNativeBinary materializes the temp build module and runs `go build`
// offline, writing the binary at absOut.
func buildNativeBinary(out transpiler.Output, absOut, entry string) error {
	work, err := os.MkdirTemp("", "geblang-native-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	keep := os.Getenv("GEBLANG_NATIVE_KEEP") != ""
	if !keep {
		defer os.RemoveAll(work)
	}

	for relPath, contents := range out.Files {
		full := filepath.Join(work, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(full, contents, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
	}

	rtDir := filepath.Join(work, "geblangrt", "pkg", "transpilert")
	if err := vendorRuntime(rtDir); err != nil {
		return err
	}
	// The vendored runtime keeps the import path geblang/pkg/transpilert.
	if err := os.WriteFile(filepath.Join(work, "geblangrt", "go.mod"),
		[]byte("module geblang\n\ngo "+goModVersion()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write runtime go.mod: %w", err)
	}

	appName := sanitizeModuleName(entry)
	appMod := "module " + appName + "\n\ngo " + goModVersion() +
		"\n\nrequire geblang v0.0.0\n\nreplace geblang => ./geblangrt\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(appMod), 0o644); err != nil {
		return fmt.Errorf("write app go.mod: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	build := exec.Command("go", "build", "-buildvcs=false", "-o", absOut, "./main")
	build.Dir = work
	// Pin the toolchain to this binary's own Go version so the offline build
	// (GOPROXY=off) never tries to fetch a newer toolchain than is installed.
	build.Env = append(os.Environ(),
		"GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN="+runtime.Version())
	if msg, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, msg)
	}
	return nil
}

// vendorRuntime writes the embedded transpilert source into dir, skipping test
// files (the only non-stdlib code transpiled output depends on).
func vendorRuntime(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir runtime: %w", err)
	}
	entries, err := transpilert.RuntimeSources.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded runtime: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := fs.ReadFile(transpilert.RuntimeSources, name)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return fmt.Errorf("write runtime %s: %w", name, err)
		}
	}
	return nil
}

// goModVersion returns the major.minor go directive for the generated modules.
// It uses the toolchain that built this binary (which compiled the embedded
// runtime), so the vendored go.mod never declares a floor below what the
// runtime's language features require.
func goModVersion() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(v, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// sanitizeModuleName turns an entry module name into a valid Go module path.
func sanitizeModuleName(entry string) string {
	name := strings.ReplaceAll(entry, ".", "_")
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		name = "app"
	}
	return name
}
