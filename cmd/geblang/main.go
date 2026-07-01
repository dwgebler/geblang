package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"geblang/internal/ast"
	"geblang/internal/bcloader"
	"geblang/internal/bundle"
	"geblang/internal/bytecode"
	"geblang/internal/check"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/memwatch"
	"geblang/internal/modules"
	"geblang/internal/native"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	geblangver "geblang/internal/version"
)

const version = geblangver.Geblang
const bannerString = "Geblang Version %s, ©2026 David Gebler.\n==========================================\n"

type executionMode int

const (
	executionAuto executionMode = iota
	executionEvaluatorOnly
	executionVMStrict
)

func main() {
	// GEBLANG_PPROF=addr starts Go's pprof HTTP endpoint for engine
	// profiling (dev tool; no effect when unset).
	if addr := os.Getenv("GEBLANG_PPROF"); addr != "" {
		// Mutex/block sampling so /debug/pprof/mutex and /block attribute contention.
		goruntime.SetMutexProfileFraction(5)
		goruntime.SetBlockProfileRate(1000000)
		go func() {
			if err := http.ListenAndServe(addr, nil); err != nil {
				fmt.Fprintf(os.Stderr, "geblang: pprof listener: %v\n", err)
			}
		}()
	}
	if b, err := bundle.OpenFromExecutable(); err == nil && b != nil {
		os.Exit(runBundled(b))
	}

	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Fprintf(os.Stdout, bannerString, version)
		topic := ""
		if len(os.Args) > 2 {
			topic = os.Args[2]
		}
		if !printHelp(os.Stdout, topic) {
			fmt.Fprintf(os.Stderr, "unknown help topic %s\n", topic)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Fprintf(os.Stdout, "geblang %s\n", version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "licenses" {
		noPager := false
		for _, a := range os.Args[2:] {
			if a == "--no-pager" {
				noPager = true
			}
		}
		writePaged(os.Stdout, licenseText, noPager)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "completion" {
		runCompletion(os.Args[2:])
		return
	}
	if len(os.Args) > 2 && isHelpArg(os.Args[2]) {
		topic := os.Args[1]
		if topic == "--module" || topic == "-m" {
			topic = "module"
		}
		if printHelp(os.Stdout, topic) {
			return
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "run" {
		// Explicit alias for the default script path (never re-read as a subcommand).
		runScriptOrModule(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "test" {
		runTests(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "cache" {
		runCache(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "check" {
		runCheck(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runInit(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctor(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "doc" {
		runDoc(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "install" {
		runInstall(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "fmt" {
		runFmt(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "dap" {
		runDap(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "lsp" {
		runLsp(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "build" {
		runBuild(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bind" {
		runBind(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "-m" || os.Args[1] == "--module") {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: geblang -m <module> [args...]")
			os.Exit(2)
		}
		exitCode, err := runModule(os.Args[2], os.Args[3:], executionAuto, false, nil, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}
	if len(os.Args) == 1 {
		os.Exit(runREPL(os.Stdin, os.Stdout, os.Stderr, replConfig{}))
	}
	if len(os.Args) > 1 && os.Args[1] == "repl" {
		mode := executionEvaluatorOnly
		traceExec := false
		args := os.Args[2:]
		for len(args) > 0 {
			switch args[0] {
			case "--disable-vm":
				mode = executionEvaluatorOnly
				args = args[1:]
			case "--vm-strict", "--vm":
				mode = executionVMStrict
				args = args[1:]
			case "--trace-exec":
				traceExec = true
				args = args[1:]
			default:
				fmt.Fprintf(os.Stderr, "unknown repl option %s\n", args[0])
				os.Exit(2)
			}
		}
		os.Exit(runREPL(os.Stdin, os.Stdout, os.Stderr, replConfig{mode: mode, traceExec: traceExec}))
	}

	runScriptOrModule(os.Args[1:])
}

func runScriptOrModule(args []string) {
	mode := executionAuto
	traceExec := false
	moduleName := ""
	var allowFFI []string
	for len(args) > 0 {
		switch args[0] {
		case "--disable-vm":
			mode = executionEvaluatorOnly
			args = args[1:]
		case "--vm-strict", "--vm":
			mode = executionVMStrict
			args = args[1:]
		case "--trace-exec":
			traceExec = true
			args = args[1:]
		case "--allow-ffi":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: --allow-ffi <path-or-glob>")
				os.Exit(2)
			}
			allowFFI = append(allowFFI, args[1])
			args = args[2:]
		case "--no-assert":
			bytecode.AssertionsDisabled = true
			args = args[1:]
		case "--allow-process-control":
			native.SetProcessControlEnabled(true)
			args = args[1:]
		case "--allow-onnx":
			native.SetOnnxEnabled(true)
			args = args[1:]
		case "--allow-browser":
			native.SetBrowserEnabled(true)
			args = args[1:]
		case "-m", "--module":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: geblang -m <module> [args...]")
				os.Exit(2)
			}
			moduleName = args[1]
			args = args[2:]
			goto doneFlags
		default:
			goto doneFlags
		}
	}
doneFlags:
	if moduleName != "" {
		exitCode, err := runModule(moduleName, args, mode, traceExec, allowFFI, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}

	if len(args) < 1 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	source, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", args[0], err)
		os.Exit(1)
	}

	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, msg := range p.Errors() {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(1)
	}
	// A directly-run file that declares `export func main` auto-invokes it.
	// Mark the source so the transformed bytecode caches under a distinct key
	// from the same file's untransformed compilation when imported as a module.
	if appendMainInvocation(program) {
		source = append(source, []byte("\n/*__geb_automain__*/\n")...)
	}
	// Cross-module analysis runs inside runScript: gated on the .gbc
	// cache miss for the VM, every invocation for the evaluator.
	exitCode, err := runScript(args[0], args[1:], source, program, mode, allowFFI, os.Stdout, traceWriter(traceExec, os.Stderr))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: geblang <command> [arguments...]")
	fmt.Fprintln(writer, "       geblang <script.gb> [args...]    run a script directly (shorthand)")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Running code:")
	fmt.Fprintln(writer, "  geblang <script.gb> [args...]      run a script with the bytecode VM (fallback to evaluator on unsupported constructs)")
	fmt.Fprintln(writer, "  geblang run <script.gb> [args...]  explicit alias for the bare script form")
	fmt.Fprintln(writer, "  geblang --disable-vm <script.gb>   run a script with the tree-walking evaluator")
	fmt.Fprintln(writer, "  geblang --vm-strict <script.gb>    run a script with the VM only, failing instead of falling back")
	fmt.Fprintln(writer, "  geblang --trace-exec <script.gb>   print which engine handled the script")
	fmt.Fprintln(writer, "  geblang --no-assert <script.gb>    elide assert(...) calls (arguments are not evaluated)")
	fmt.Fprintln(writer, "  geblang --allow-ffi <path-or-glob> <script.gb>  enable FFI access to matching libraries (repeatable)")
	fmt.Fprintln(writer, "  geblang --allow-process-control <script.gb>  enable privileged process ops (setuid/setgid, kill/signal by pid)")
	fmt.Fprintln(writer, "  geblang --allow-onnx <script.gb>   enable local ONNX model inference (loads a native shared library)")
	fmt.Fprintln(writer, "  geblang --allow-browser <script.gb>  enable headless-browser automation (launches a browser subprocess)")
	fmt.Fprintln(writer, "  geblang -m <module> [args...]      run the named module's exported main()")
	fmt.Fprintln(writer, "  geblang repl                       start the interactive REPL")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Project workflow:")
	fmt.Fprintln(writer, "  geblang init [--name n] [--source d]  scaffold a geblang.yaml manifest in the current directory")
	fmt.Fprintln(writer, "  geblang install [git-url[@ver]]    install a package or all manifest dependencies")
	fmt.Fprintln(writer, "  geblang build --entry m --out p [--no-assert] [--native]  bundle the project into a single self-contained binary")
	fmt.Fprintln(writer, "  geblang fmt <file.gb> [...]        format files in place (use --stdin for piped input)")
	fmt.Fprintln(writer, "  geblang check [--strict] <path>    parse + lint without executing")
	fmt.Fprintln(writer, "  geblang test [--tag n] [-v] <p>    discover and run *_test.gb files")
	fmt.Fprintln(writer, "  geblang doc [--format m|json] <p>  extract API documentation from doc comments")
	fmt.Fprintln(writer, "  geblang bind <manifest.yaml>       generate a Geblang module wrapping a C-ABI shared library (FFI)")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "IDE / tooling integration:")
	fmt.Fprintln(writer, "  geblang lsp                        start the Language Server Protocol server (stdio)")
	fmt.Fprintln(writer, "  geblang dap                        start the Debug Adapter Protocol server (stdio)")
	fmt.Fprintln(writer, "  geblang doctor [--json]            inspect the local install for common setup issues")
	fmt.Fprintln(writer, "  geblang cache clean                purge the bytecode cache")
	fmt.Fprintln(writer, "  geblang cache stats [--json]       report bytecode cache size and entries")
	fmt.Fprintln(writer, "  geblang completion bash            print a bash completion script (source <(geblang completion bash))")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Help:")
	fmt.Fprintln(writer, "  geblang help [topic]               show detailed help for a command (topic == repl, run, build, ...)")
	fmt.Fprintln(writer, "  geblang --version                  print the Geblang version and exit")
	fmt.Fprintln(writer, "  geblang licenses [--no-pager]      print third-party attribution notices (paged on a terminal)")
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-help" || arg == "-h" || arg == "help"
}

func printHelp(writer io.Writer, topic string) bool {
	switch strings.ToLower(topic) {
	case "":
		printUsage(writer)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Run geblang with no arguments to start the REPL.")
		fmt.Fprintln(writer, "Use `geblang help <topic>` or `geblang <command> --help` for command details.")
		fmt.Fprintln(writer, "Topics: repl, run, module, build, install, fmt, lsp, dap, test, check, init, doctor, doc, cache, completion")
		return true
	case "repl":
		fmt.Fprintln(writer, "usage: geblang repl [--disable-vm|--vm-strict|--trace-exec]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Starts an interactive Geblang session. REPL commands include:")
		fmt.Fprintln(writer, "  :help, :quit, :reset, :load <file>, :vars, :imports, :stdlib, :modules, :members <module>, :mode, :history")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang repl")
		fmt.Fprintln(writer, "  geblang repl --vm-strict")
		return true
	case "run":
		fmt.Fprintln(writer, "usage: geblang [run] [--disable-vm|--vm-strict|--trace-exec] [--allow-ffi <path-or-glob>] <script.gb> [args...]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Runs a script. VM execution is attempted by default and falls back to the evaluator when needed.")
		fmt.Fprintln(writer, "The `run` verb is optional; `geblang app.gb` and `geblang run app.gb` are equivalent.")
		fmt.Fprintln(writer, "`--allow-ffi <path-or-glob>` grants FFI access to matching libraries and may be repeated.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang app.gb")
		fmt.Fprintln(writer, "  geblang run app.gb")
		fmt.Fprintln(writer, "  geblang run --allow-ffi 'libm.so.*' app.gb")
		fmt.Fprintln(writer, "  geblang --vm-strict app.gb --port 8080")
		fmt.Fprintln(writer, "  geblang --trace-exec app.gb")
		return true
	case "build":
		fmt.Fprintln(writer, "usage: geblang build --entry module.name --out <path> [--native] [--runtime <path>] [--resource <path>] [--allow-ffi <glob>] [--allow-onnx] [--allow-process-control] [--allow-browser] [--docker [--docker-port N] [--force]] [<package-dir>]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Produces a self-contained executable from a Geblang package.")
		fmt.Fprintln(writer, "The output binary bundles reachable source files, precompiled bytecode,")
		fmt.Fprintln(writer, "source stdlib modules, and imported vendored package dependencies.")
		fmt.Fprintln(writer, "Running the output binary requires no separate geblang installation.")
		fmt.Fprintln(writer, "The entry module must `export func main()` or `export func main(list<string> args)`,")
		fmt.Fprintln(writer, "optionally returning : int used as the process exit code. A missing or")
		fmt.Fprintln(writer, "incompatible main is a build error and produces no binary.")
		fmt.Fprintln(writer, "--entry  canonical module name whose exported main is the entry point (required)")
		fmt.Fprintln(writer, "--out    output file path (required)")
		fmt.Fprintln(writer, "--native compile to a standalone native binary via the Go transpiler (experimental);")
		fmt.Fprintln(writer, "         calls the entry module's exported main, same as the bundled build")
		fmt.Fprintln(writer, "--runtime <p> embed the bundle into the runtime binary at p instead of this one")
		fmt.Fprintln(writer, "         (for cross-platform builds; p must be the same geblang version - see scripts/cross-build.sh)")
		fmt.Fprintln(writer, "--docker        also write a Dockerfile next to the binary")
		fmt.Fprintln(writer, "--docker-port N add EXPOSE N to the generated Dockerfile")
		fmt.Fprintln(writer, "--force         overwrite an existing generated Dockerfile")
		fmt.Fprintln(writer, "--no-assert     strip assert() calls from the bundled program")
		fmt.Fprintln(writer, "--resource <p>  embed the file or directory at p into the binary as a resource (repeat for multiple)")
		fmt.Fprintln(writer, "--allow-ffi <glob>       bake FFI access to libraries matching glob into the binary (capability)")
		fmt.Fprintln(writer, "--allow-onnx             bake local ONNX inference into the binary (capability)")
		fmt.Fprintln(writer, "--allow-process-control  bake process-control access into the binary (capability)")
		fmt.Fprintln(writer, "--allow-browser          bake headless-browser access into the binary (capability)")
		fmt.Fprintln(writer, "<package-dir>  package root directory containing geblang.yaml (default: .)")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang build --entry app.main --out build/app")
		fmt.Fprintln(writer, "  geblang build --entry app.main --out build/app ./packages/app")
		fmt.Fprintln(writer, "  geblang build --entry app.main --out build/app --native")
		return true
	case "install":
		fmt.Fprintln(writer, "usage: geblang install")
		fmt.Fprintln(writer, "       geblang install <git-url>[@version] [<name>]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Without arguments, fetches all git dependencies declared in geblang.yaml")
		fmt.Fprintln(writer, "into vendor/ and pins their commit SHAs in geblang.lock.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "With a git URL, adds the dependency to geblang.yaml and fetches it.")
		fmt.Fprintln(writer, "The package name is derived from the URL's last path segment unless")
		fmt.Fprintln(writer, "an explicit <name> is provided.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "geblang.lock should be committed to source control for reproducible builds.")
		fmt.Fprintln(writer, "vendor/ may be gitignored and regenerated with geblang install.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang install")
		fmt.Fprintln(writer, "  geblang install https://github.com/acme/tools.git")
		fmt.Fprintln(writer, "  geblang install https://github.com/acme/tools.git@v1.2.0 acme.tools")
		return true
	case "fmt":
		fmt.Fprintln(writer, "usage: geblang fmt <file.gb> [...]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Formats Geblang source files in place using canonical style:")
		fmt.Fprintln(writer, "  4-space indentation, braces on the same line, blank lines between top-level declarations.")
		fmt.Fprintln(writer, "Files with syntax errors are reported and left unchanged.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang fmt src/main.gb")
		fmt.Fprintln(writer, "  geblang fmt src/*.gb")
		return true
	case "lsp":
		fmt.Fprintln(writer, "usage: geblang lsp")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Starts a Language Server Protocol (LSP) server on stdin/stdout.")
		fmt.Fprintln(writer, "The server publishes parse and semantic diagnostics on open/change.")
		fmt.Fprintln(writer, "Intended to be launched by an IDE extension (e.g. vscode-geblang).")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang lsp")
		return true
	case "dap":
		fmt.Fprintln(writer, "usage: geblang dap")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Starts a Debug Adapter Protocol (DAP) server on stdin/stdout.")
		fmt.Fprintln(writer, "Supports breakpoints, step over/into/out, and variable inspection.")
		fmt.Fprintln(writer, "Intended to be launched by an IDE debug configuration (e.g. vscode-geblang).")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang dap")
		return true
	case "module", "-m":
		fmt.Fprintln(writer, "usage: geblang [--disable-vm|--vm-strict|--trace-exec] [--allow-ffi <path-or-glob>] -m <module> [args...]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Resolves a module and invokes its exported main(args) function without a wrapper script.")
		fmt.Fprintln(writer, "VM execution is attempted by default and falls back to the evaluator when needed.")
		fmt.Fprintln(writer, "`--allow-ffi <path-or-glob>` grants FFI access in addition to permissions from geblang.yaml.")
		fmt.Fprintln(writer, "Execution options must appear before `-m`; later arguments are passed to the module.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang -m http.server 8080")
		fmt.Fprintln(writer, "  geblang --allow-ffi 'libsqlite3*' -m app.cli")
		fmt.Fprintln(writer, "  geblang --vm-strict -m app.main --verbose")
		return true
	case "test":
		fmt.Fprintln(writer, "usage: geblang test [--tag name] [--class ClassName] [--method methodName]")
		fmt.Fprintln(writer, "                    [--verbose|-v|--format <summary|verbose|teamcity>]")
		fmt.Fprintln(writer, "                    [--allow-ffi <glob>] [--allow-process-control] [--allow-onnx] [--allow-browser] <file-or-dir>")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Runs Geblang test files. Directory paths discover *_test.gb files recursively.")
		fmt.Fprintln(writer, "--tag name runs only tests decorated with the given tag. Repeat to include multiple tags.")
		fmt.Fprintln(writer, "--class ClassName restricts the run to that test class.")
		fmt.Fprintln(writer, "--method methodName restricts the run to that method (repeat for multiple methods).")
		fmt.Fprintln(writer, "--verbose / -v prints each test class and method with PASS/FAIL status (testdox-style).")
		fmt.Fprintln(writer, "--format teamcity emits ##teamcity[...] service messages for JetBrains IDE test runners.")
		fmt.Fprintln(writer, "--allow-ffi <glob> / --allow-process-control / --allow-onnx / --allow-browser grant the matching capability to the run (default-deny).")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang test tests/user_test.gb")
		fmt.Fprintln(writer, "  geblang test tests/")
		fmt.Fprintln(writer, "  geblang test --tag integration tests/")
		fmt.Fprintln(writer, "  geblang test --verbose tests/")
		fmt.Fprintln(writer, "  geblang test --format teamcity tests/")
		fmt.Fprintln(writer, "  geblang test --class UserTest --method login tests/user_test.gb")
		return true
	case "check":
		fmt.Fprintln(writer, "usage: geblang check [--json] [--no-lint] [--strict] <file-or-dir>")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Parses, semantically checks, and lints Geblang files without executing them.")
		fmt.Fprintln(writer, "--json writes a structured report for CI and editor tooling.")
		fmt.Fprintln(writer, "--no-lint disables lint diagnostics. --strict exits non-zero on warnings.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang check src/")
		fmt.Fprintln(writer, "  geblang check --strict docs/examples/")
		fmt.Fprintln(writer, "  geblang check --json src/main.gb")
		return true
	case "init":
		fmt.Fprintln(writer, "usage: geblang init [--name name] [--source dir] [--force]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Creates a minimal geblang.yaml package manifest in the current directory.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang init --name acme.api")
		fmt.Fprintln(writer, "  geblang init --name acme.api --source src")
		return true
	case "completion":
		fmt.Fprintln(writer, "usage: geblang completion bash")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Prints a shell completion script. Enable it for the current shell:")
		fmt.Fprintln(writer, "  source <(geblang completion bash)")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Or install it permanently by adding that line to ~/.bashrc.")
		fmt.Fprintln(writer, "Completes subcommands at the first position and filenames after.")
		return true
	case "doctor":
		fmt.Fprintln(writer, "usage: geblang doctor [--json]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Reports local Geblang tooling, manifest, and bytecode cache state.")
		fmt.Fprintln(writer, "--json writes a structured report for CI and editor tooling.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang doctor")
		fmt.Fprintln(writer, "  geblang doctor --json")
		return true
	case "doc":
		fmt.Fprintln(writer, "usage: geblang doc [--format markdown|json] [--out file] <file-or-dir>")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Generates source API documentation from Geblang declarations and docblocks.")
		fmt.Fprintln(writer, "When a file uses export, only exported declarations are included.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang doc src/")
		fmt.Fprintln(writer, "  geblang doc --format json src/")
		fmt.Fprintln(writer, "  geblang doc --out build/api.md src/")
		return true
	case "bind":
		fmt.Fprintln(writer, "usage: geblang bind [--out file] <manifest.yaml>")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Generates a Geblang module wrapping a C-ABI shared library according to the")
		fmt.Fprintln(writer, "manifest. With no --out, prints the generated source to stdout.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Manifest sections: module, library, doc, constants, structs, functions.")
		fmt.Fprintln(writer, "Types: VOID, INT8..INT64, UINT8..UINT64, FLOAT, DOUBLE, PTR, CSTRING, BYTES.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang bind bindings/sqlite.yaml --out src/sqlite.gb")
		fmt.Fprintln(writer, "  geblang bind bindings/libm.yaml > src/libm.gb")
		return true
	case "cache":
		fmt.Fprintln(writer, "usage: geblang cache clean")
		fmt.Fprintln(writer, "       geblang cache stats [--json]")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Inspects or removes cached bytecode chunks under .geblang-cache/.")
		fmt.Fprintln(writer, "--json writes structured cache stats.")
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "Examples:")
		fmt.Fprintln(writer, "  geblang cache stats")
		fmt.Fprintln(writer, "  geblang cache stats --json")
		fmt.Fprintln(writer, "  geblang cache clean")
		return true
	default:
		return false
	}
}

func runDoctor(args []string) {
	config, err := parseDoctorArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "usage: geblang doctor [--json]")
		os.Exit(2)
	}
	report, err := collectDoctorReport(exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if config.JSON {
		if err := writeDoctorJSON(os.Stdout, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	writeDoctorReport(os.Stdout, report)
}

type doctorConfig struct {
	JSON bool
}

type doctorReport struct {
	GeblangVersion   string              `json:"geblangVersion"`
	WorkingDirectory string              `json:"workingDirectory"`
	GoPath           string              `json:"goPath,omitempty"`
	GoFound          bool                `json:"goFound"`
	Manifest         *doctorManifest     `json:"manifest,omitempty"`
	ManifestError    string              `json:"manifestError,omitempty"`
	Cache            doctorCacheSnapshot `json:"cache"`
	CacheError       string              `json:"cacheError,omitempty"`
	FFI              doctorFFI           `json:"ffi"`
}

type doctorFFI struct {
	Enabled bool             `json:"enabled"`
	Source  string           `json:"source,omitempty"` // "manifest" or "none"
	Entries []doctorFFIEntry `json:"entries,omitempty"`
}

type doctorFFIEntry struct {
	Path string `json:"path,omitempty"`
	Glob string `json:"glob,omitempty"`
}

type doctorManifest struct {
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

type doctorCacheSnapshot struct {
	Root  string `json:"root"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

func parseDoctorArgs(args []string) (doctorConfig, error) {
	config := doctorConfig{}
	for _, arg := range args {
		switch arg {
		case "--json":
			config.JSON = true
		default:
			return config, fmt.Errorf("unknown doctor option %s", arg)
		}
	}
	return config, nil
}

// readDoctorFFI parses just the permissions.ffi block from the
// project manifest. Lives here rather than in internal/ffi to
// keep the modules package free of cross-cutting deps on the FFI
// runtime; the doctor only needs the user-visible rules, not the
// runtime Policy struct.
func readDoctorFFI(manifestPath string) doctorFFI {
	out := doctorFFI{Source: "none"}
	if manifestPath == "" {
		return out
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return out
	}
	var parsed struct {
		Permissions struct {
			FFI *struct {
				Enabled   bool `yaml:"enabled"`
				Libraries []struct {
					Path string `yaml:"path"`
					Glob string `yaml:"glob"`
				} `yaml:"libraries"`
			} `yaml:"ffi"`
		} `yaml:"permissions"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return out
	}
	if parsed.Permissions.FFI == nil {
		return out
	}
	out.Source = "manifest"
	out.Enabled = parsed.Permissions.FFI.Enabled
	for _, lib := range parsed.Permissions.FFI.Libraries {
		out.Entries = append(out.Entries, doctorFFIEntry{Path: lib.Path, Glob: lib.Glob})
	}
	return out
}

func collectDoctorReport(lookPath func(string) (string, error)) (doctorReport, error) {
	wd, err := os.Getwd()
	if err != nil {
		return doctorReport{}, err
	}
	report := doctorReport{GeblangVersion: version, WorkingDirectory: wd}
	if path, err := lookPath("go"); err == nil {
		report.GoFound = true
		report.GoPath = path
	}
	resolver := modules.NewResolver([]string{wd})
	manifest, err := resolver.FindManifest(wd)
	if err != nil {
		report.ManifestError = err.Error()
	} else if manifest != nil {
		report.Manifest = &doctorManifest{
			Path:    manifest.Path,
			Name:    manifest.Name,
			Version: manifest.Version,
			Source:  manifest.Source,
		}
		report.FFI = readDoctorFFI(manifest.Path)
	}
	stats, err := bytecodeCacheStats()
	if err != nil {
		report.Cache = doctorCacheSnapshot{Root: bytecodeCacheRoot()}
		report.CacheError = err.Error()
	} else {
		report.Cache = doctorCacheSnapshot{Root: stats.Root, Files: stats.Files, Bytes: stats.Bytes}
	}
	return report, nil
}

func writeDoctorReport(writer io.Writer, report doctorReport) {
	fmt.Fprintf(writer, "geblang: %s\n", report.GeblangVersion)
	fmt.Fprintf(writer, "working directory: %s\n", report.WorkingDirectory)
	if report.GoFound {
		fmt.Fprintf(writer, "go: %s\n", report.GoPath)
	} else {
		fmt.Fprintln(writer, "go: not found")
	}
	if report.ManifestError != "" {
		fmt.Fprintf(writer, "manifest: error: %v\n", report.ManifestError)
	} else if report.Manifest == nil {
		fmt.Fprintln(writer, "manifest: not found")
	} else {
		fmt.Fprintf(writer, "manifest: %s\n", report.Manifest.Path)
		fmt.Fprintf(writer, "package: %s %s\n", valueOrDash(report.Manifest.Name), valueOrDash(report.Manifest.Version))
		fmt.Fprintf(writer, "source: %s\n", valueOrDash(report.Manifest.Source))
	}
	if report.CacheError != "" {
		fmt.Fprintf(writer, "cache: error: %v\n", report.CacheError)
	} else {
		fmt.Fprintf(writer, "cache: %s files=%d bytes=%d\n", report.Cache.Root, report.Cache.Files, report.Cache.Bytes)
	}
	writeDoctorFFI(writer, report.FFI)
}

func writeDoctorFFI(writer io.Writer, ffi doctorFFI) {
	switch ffi.Source {
	case "manifest":
		if !ffi.Enabled {
			fmt.Fprintln(writer, "ffi: disabled by manifest (permissions.ffi.enabled is false)")
			return
		}
		if len(ffi.Entries) == 0 {
			fmt.Fprintln(writer, "ffi: enabled, allow-list empty (no libraries can load)")
			return
		}
		fmt.Fprintf(writer, "ffi: enabled, %d allow-list rule(s)\n", len(ffi.Entries))
		for _, entry := range ffi.Entries {
			if entry.Path != "" {
				fmt.Fprintf(writer, "  path %s\n", entry.Path)
			} else if entry.Glob != "" {
				fmt.Fprintf(writer, "  glob %s\n", entry.Glob)
			}
		}
	default:
		fmt.Fprintln(writer, "ffi: not configured (pass --allow-ffi or add a permissions.ffi block)")
	}
}

func writeDoctorJSON(writer io.Writer, report doctorReport) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

type initConfig struct {
	name   string
	source string
	force  bool
}

func runInit(args []string) {
	config, err := parseInitArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "usage: geblang init [--name name] [--source dir] [--force]")
		os.Exit(2)
	}
	path, err := initPackage(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("created %s\n", path)
}

func parseInitArgs(args []string) (initConfig, error) {
	config := initConfig{source: "src"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 >= len(args) {
				return config, fmt.Errorf("geblang init --name expects a value")
			}
			config.name = args[i+1]
			i++
		case "--source":
			if i+1 >= len(args) {
				return config, fmt.Errorf("geblang init --source expects a directory")
			}
			config.source = args[i+1]
			i++
		case "--force":
			config.force = true
		default:
			return config, fmt.Errorf("unknown init option %s", args[i])
		}
	}
	return config, nil
}

func initPackage(config initConfig) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if config.name == "" {
		config.name = packageNameFromDir(wd)
	}
	if config.name == "" {
		return "", fmt.Errorf("package name is required")
	}
	if config.source == "" {
		config.source = "src"
	}
	manifestPath := filepath.Join(wd, "geblang.yaml")
	if _, err := os.Stat(manifestPath); err == nil && !config.force {
		return "", fmt.Errorf("%s already exists; use --force to overwrite", manifestPath)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(wd, config.source), 0o755); err != nil {
		return "", err
	}
	manifest := fmt.Sprintf("name: %s\nversion: 0.1.0\nsource: %s\npaths: []\ndependencies: {}\n", config.name, config.source)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return "", err
	}
	return manifestPath, nil
}

func packageNameFromDir(path string) string {
	name := strings.ToLower(filepath.Base(path))
	var b strings.Builder
	lastWasDot := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastWasDot = false
			continue
		}
		if !lastWasDot && b.Len() > 0 {
			b.WriteByte('.')
			lastWasDot = true
		}
	}
	return strings.Trim(b.String(), ".")
}

func runCheck(args []string) {
	config, err := parseCheckArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "usage: geblang check [--json] [--no-lint] [--strict] <file-or-dir>")
		os.Exit(2)
	}
	result, err := checkGeblangPath(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if config.JSON {
		if err := writeCheckJSON(os.Stdout, result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintln(os.Stderr, diagnostic.String())
		}
		if len(result.Diagnostics) == 0 {
			fmt.Printf("checked %d file(s)\n", result.Checked)
		}
	}
	if result.HasErrors() || (config.Strict && result.HasWarnings()) {
		os.Exit(1)
	}
}

type checkConfig struct {
	Path   string
	JSON   bool
	Lint   bool
	Strict bool

	// nativeSymbols and moduleCache back the cross-module symbol check;
	// built once per run in checkGeblangPath and shared across files.
	nativeSymbols map[string]map[string]struct{}
	moduleCache   *check.ModuleCache
}

type checkResult struct {
	Checked     int               `json:"checked"`
	Diagnostics []checkDiagnostic `json:"diagnostics"`
}

type checkDiagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Severity string `json:"severity"`
	Rule     string `json:"rule,omitempty"`
	Message  string `json:"message"`
}

func (d checkDiagnostic) String() string {
	prefix := d.File
	if d.Line > 0 {
		prefix = fmt.Sprintf("%s:%d:%d", prefix, d.Line, d.Column)
	}
	if prefix == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Message)
	}
	if d.Rule != "" {
		return fmt.Sprintf("%s: %s[%s]: %s", prefix, d.Severity, d.Rule, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", prefix, d.Severity, d.Message)
}

func (r checkResult) HasErrors() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func (r checkResult) HasWarnings() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == "warning" {
			return true
		}
	}
	return false
}

func parseCheckArgs(args []string) (checkConfig, error) {
	config := checkConfig{Lint: true}
	paths := []string{}
	for _, arg := range args {
		switch arg {
		case "--json":
			config.JSON = true
		case "--no-lint":
			config.Lint = false
		case "--strict":
			config.Strict = true
		default:
			paths = append(paths, arg)
		}
	}
	if len(paths) != 1 {
		return config, fmt.Errorf("geblang check expects one file or directory")
	}
	config.Path = paths[0]
	return config, nil
}

func writeCheckJSON(writer io.Writer, result checkResult) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func checkGeblangPath(config checkConfig) (checkResult, error) {
	files, err := discoverGeblangFiles(config.Path)
	if err != nil {
		return checkResult{}, err
	}
	if len(files) == 0 {
		return checkResult{}, fmt.Errorf("no Geblang files found")
	}
	config.nativeSymbols = evaluator.NativeModuleSymbols()
	config.moduleCache = check.NewModuleCache()
	result := checkResult{Checked: len(files), Diagnostics: []checkDiagnostic{}}
	programs := map[string]*ast.Program{}
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, checkDiagnostic{File: file, Severity: "error", Rule: "read", Message: err.Error()})
			continue
		}
		program, diagnostics := checkGeblangSource(file, string(source), config)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		if program != nil {
			programs[file] = program
		}
	}
	result.Diagnostics = append(result.Diagnostics, checkModuleDeclarations(config, programs)...)
	return result, nil
}

func checkGeblangSource(file, source string, config checkConfig) (*ast.Program, []checkDiagnostic) {
	opts := check.Options{
		Lint:          config.Lint,
		Resolver:      checkResolver(config, file),
		CrossModule:   true,
		NativeSymbols: config.nativeSymbols,
		ModuleCache:   config.moduleCache,
	}
	program, raw := check.Source(file, source, opts)
	diagnostics := make([]checkDiagnostic, 0, len(raw))
	for _, d := range raw {
		diagnostics = append(diagnostics, checkDiagnostic{
			File:     d.File,
			Line:     d.Line,
			Column:   d.Column,
			Severity: string(d.Severity),
			Rule:     d.Rule,
			Message:  d.Message,
		})
	}
	return program, diagnostics
}

// isBytecodeParityError is the cmd/-package alias for
// bytecode.IsParityError. Kept as a thin wrapper so existing callers
// in this file don't need touching while the helper lives in the
// bytecode package, where it can also be reused by the DAP server's
// pre-flight check.
func isBytecodeParityError(err error) bool {
	return bytecode.IsParityError(err)
}

type moduleDeclaration struct {
	name   string
	file   string
	line   int
	column int
}

func checkModuleDeclarations(config checkConfig, programs map[string]*ast.Program) []checkDiagnostic {
	diagnostics := []checkDiagnostic{}
	declared := map[string][]moduleDeclaration{}
	files := make([]string, 0, len(programs))
	for file := range programs {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		program := programs[file]
		modules := []moduleDeclaration{}
		for index, stmt := range program.Statements {
			moduleStmt, ok := stmt.(*ast.ModuleStatement)
			if !ok {
				continue
			}
			name := strings.Join(moduleStmt.Path, ".")
			decl := moduleDeclaration{name: name, file: file, line: moduleStmt.Token.Line, column: moduleStmt.Token.Column}
			modules = append(modules, decl)
			if index != 0 {
				diagnostics = append(diagnostics, checkDiagnostic{
					File:     file,
					Line:     moduleStmt.Token.Line,
					Column:   moduleStmt.Token.Column,
					Severity: "error",
					Rule:     "module",
					Message:  "module declaration must be the first statement",
				})
			}
		}
		if len(modules) > 1 {
			for _, decl := range modules[1:] {
				diagnostics = append(diagnostics, checkDiagnostic{
					File:     decl.file,
					Line:     decl.line,
					Column:   decl.column,
					Severity: "error",
					Rule:     "module",
					Message:  "file contains more than one module declaration",
				})
			}
		}
		if len(modules) == 0 {
			continue
		}
		decl := modules[0]
		resolved, err := checkResolver(config, file).Resolve(decl.name)
		if err != nil {
			declared[decl.name] = append(declared[decl.name], decl)
			diagnostics = append(diagnostics, checkDiagnostic{
				File:     decl.file,
				Line:     decl.line,
				Column:   decl.column,
				Severity: "error",
				Rule:     "module",
				Message:  fmt.Sprintf("cannot resolve declared module %s", decl.name),
			})
			continue
		}
		if !sameFilePath(resolved, file) {
			if strings.HasSuffix(file, "_test.gb") {
				// Same-module test file: tests module X from inside X
				// (check.SameModuleTestProgram), not a duplicate.
				continue
			}
			diagnostics = append(diagnostics, checkDiagnostic{
				File:     decl.file,
				Line:     decl.line,
				Column:   decl.column,
				Severity: "error",
				Rule:     "module",
				Message:  fmt.Sprintf("declared module %s resolves to %s", decl.name, resolved),
			})
		}
		declared[decl.name] = append(declared[decl.name], decl)
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		decls := declared[name]
		if len(decls) < 2 {
			continue
		}
		for _, decl := range decls {
			diagnostics = append(diagnostics, checkDiagnostic{
				File:     decl.file,
				Line:     decl.line,
				Column:   decl.column,
				Severity: "error",
				Rule:     "duplicate-module",
				Message:  fmt.Sprintf("module %s is declared in multiple checked files", name),
			})
		}
	}
	return diagnostics
}

func checkResolver(config checkConfig, file string) *modules.Resolver {
	// Root at the importing file's own directory (plus stdlib/manifest
	// paths added by NewResolver), matching how the runtime resolves
	// imports. Rooting at the check-target directory would let dotted
	// imports (async.http) resolve to sibling files under it instead of
	// the bundled stdlib module.
	return modules.NewResolver([]string{filepath.Dir(file)})
}

func sameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func discoverGeblangFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	files := []string{}
	err = filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".geblang-cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".gb") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// applyManifestCapabilities enables onnx / process-control when the project's geblang.yaml permissions block grants them (dev-time parity with the FFI policy).
func applyManifestCapabilities(dir string) {
	m, err := modules.NewResolver([]string{dir}).FindManifest(dir)
	if err != nil || m == nil {
		return
	}
	if m.Permissions.Onnx {
		native.SetOnnxEnabled(true)
	}
	if m.Permissions.ProcessControl {
		native.SetProcessControlEnabled(true)
	}
	if m.Permissions.Browser {
		native.SetBrowserEnabled(true)
	}
}

func runScript(sourcePath string, scriptArgs []string, source []byte, program *ast.Program, mode executionMode, allowFFI []string, stdout io.Writer, trace io.Writer) (int, error) {
	applyManifestCapabilities(filepath.Dir(sourcePath))
	memwatch.Start()
	// Keep advisory analysis warnings off the program's stdout (stderr instead).
	analyze := crossModuleAnalyzer(sourcePath, program, modules.NewResolver([]string{filepath.Dir(sourcePath)}), os.Stderr, "warning: ")
	// On the VM path the cross-module analysis runs inside the compile
	// (cache-miss) step; track whether it ran so the eval fallback does
	// not re-run it.
	analyzedDuringCompile := false
	if mode != executionEvaluatorOnly {
		chunk, err := loadOrCompileBytecode(sourcePath, source, program, analyze)
		analyzedDuringCompile = true
		if err == nil {
			traceExecution(trace, "vm", "")
			basePaths := []string{filepath.Dir(sourcePath)}
			stateful := evaluator.NewWithArgsAndModulePaths(stdout, scriptArgs, basePaths)
			stateful.AssertionsDisabled = bytecode.AssertionsDisabled
			defer stateful.Cleanup()
			if policy, perr := stateful.BuildFFIPolicy(filepath.Dir(sourcePath), allowFFI); perr == nil {
				stateful.SetFFIPolicy(policy)
			} else {
				return 1, perr
			}
			loaderOpts := bcloader.Options{
				Compile: func(canonical, sp string, src []byte, prog *ast.Program, modPaths []string) (bytecode.Chunk, error) {
					resolverPaths := append([]string{filepath.Dir(sp)}, modPaths...)
					an := crossModuleAnalyzer(sp, prog, modules.NewResolver(resolverPaths), stdout, fmt.Sprintf("warning: module %s: ", canonical))
					return loadOrCompileBytecode(sp, src, prog, an)
				},
				LookupBuiltin: func(canonical, alias string) *runtime.Module {
					return stateful.BuiltinModule(canonical, alias)
				},
			}
			loader := bcloader.New(stdout, basePaths, stateful, loaderOpts)
			loader.SetMainChunk(chunk)
			vm := bytecode.NewVMWithModuleLoader(chunk, stdout, loader)
			defer vm.Cleanup()
			loader.SetMainVM(vm)
			vm.SetModulePaths(basePaths)
			vm.SetStatefulNativeCaller(stateful)
			stateful.SetMethodDispatcher(vm)
			if err := vm.Run(); err != nil {
				var exitErr bytecode.ExitError
				if errors.As(err, &exitErr) {
					return exitErr.Code, nil
				}
				return 1, err
			}
			return 0, nil
		}
		if mode == executionVMStrict {
			return 1, err
		}
		// A cross-module analysis error is a hard error on both backends;
		// never fall back to the evaluator on it.
		var aerr analysisError
		if errors.As(err, &aerr) {
			return 1, err
		}
		// Bytecode-compile errors that are NOT parity gaps are real
		// static-analysis errors (type mismatches, no-matching-overload,
		// undeclared identifiers). Abort instead of falling back to the
		// evaluator: running half the script before crashing on a
		// known-bad call is the worst possible failure mode.
		if !isBytecodeParityError(err) {
			return 1, err
		}
		traceExecution(trace, "evaluator", err.Error())
	} else {
		traceExecution(trace, "evaluator", "--disable-vm")
	}
	// The evaluator has no .gbc cache, so the analysis must run every
	// invocation to reject uncalled cross-module errors runtime alone
	// misses. Skip only when the VM compile step already analyzed.
	if !analyzedDuringCompile {
		if err := analyze(); err != nil {
			return 1, err
		}
	}
	return runEvaluator(sourcePath, scriptArgs, program, allowFFI, stdout)
}

func runModule(moduleName string, moduleArgs []string, mode executionMode, traceExec bool, allowFFI []string, stdout io.Writer, stderr io.Writer) (int, error) {
	if moduleName == "" {
		return 2, fmt.Errorf("module name is required")
	}
	alias := "__geb_module"
	source := []byte(fmt.Sprintf(`import sys;
import %s as %s;

let __geb_result = %s.main(sys.args());
if (__geb_result != null) {
    sys.exit(__geb_result as int);
}
`, moduleName, alias, alias))
	wd, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	sourcePath := filepath.Join(wd, "__geblang_module__.gb")
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		return 1, err
	}
	return runScript(sourcePath, moduleArgs, source, program, mode, allowFFI, stdout, traceWriter(traceExec, stderr))
}

func traceWriter(enabled bool, writer io.Writer) io.Writer {
	if !enabled {
		return nil
	}
	return writer
}

func traceExecution(writer io.Writer, engine string, reason string) {
	if writer == nil {
		return
	}
	if reason == "" {
		fmt.Fprintf(writer, "geblang: execution=%s\n", engine)
		return
	}
	fmt.Fprintf(writer, "geblang: execution=%s reason=%s\n", engine, reason)
}

func runEvaluator(sourcePath string, scriptArgs []string, program *ast.Program, allowFFI []string, stdout io.Writer) (int, error) {
	e := evaluator.NewWithArgsAndModulePaths(stdout, scriptArgs, []string{filepath.Dir(sourcePath)})
	e.AssertionsDisabled = bytecode.AssertionsDisabled
	if policy, err := e.BuildFFIPolicy(filepath.Dir(sourcePath), allowFFI); err == nil {
		e.SetFFIPolicy(policy)
	} else {
		return 1, err
	}
	defer e.Cleanup()
	result, err := e.Eval(program)
	if err != nil {
		return 1, err
	}
	if result.Exited {
		return result.ExitCode, nil
	}
	return 0, nil
}

func runCache(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: geblang cache clean")
		fmt.Fprintln(os.Stderr, "       geblang cache stats [--json]")
		os.Exit(2)
	}
	switch args[0] {
	case "clean":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: geblang cache clean")
			os.Exit(2)
		}
		if err := cleanBytecodeCache(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("bytecode cache cleaned")
	case "stats":
		config, err := parseCacheStatsArgs(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, "usage: geblang cache stats [--json]")
			os.Exit(2)
		}
		stats, err := bytecodeCacheStats()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if config.JSON {
			if err := writeCacheStatsJSON(os.Stdout, stats); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		fmt.Printf("cache root: %s\n", stats.Root)
		fmt.Printf("files: %d\n", stats.Files)
		fmt.Printf("bytes: %d\n", stats.Bytes)
	default:
		fmt.Fprintln(os.Stderr, "usage: geblang cache clean")
		fmt.Fprintln(os.Stderr, "       geblang cache stats [--json]")
		os.Exit(2)
	}
}

func cleanBytecodeCache() error {
	return os.RemoveAll(bytecodeCacheRoot())
}

type cacheStats struct {
	Root  string `json:"root"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

type cacheStatsConfig struct {
	JSON bool
}

func parseCacheStatsArgs(args []string) (cacheStatsConfig, error) {
	config := cacheStatsConfig{}
	for _, arg := range args {
		switch arg {
		case "--json":
			config.JSON = true
		default:
			return config, fmt.Errorf("unknown cache stats option %s", arg)
		}
	}
	return config, nil
}

func writeCacheStatsJSON(writer io.Writer, stats cacheStats) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(stats)
}

func bytecodeCacheStats() (cacheStats, error) {
	root := bytecodeCacheRoot()
	stats := cacheStats{Root: root}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return stats, nil
	} else if err != nil {
		return stats, err
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stats.Files++
		stats.Bytes += info.Size()
		return nil
	})
	return stats, err
}

func runTests(args []string) {
	tags := []string{}
	classFilter := ""
	methodFilters := []string{}
	paths := []string{}
	format := "summary"
	allowFFI := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tag":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "geblang test --tag expects a name")
				os.Exit(2)
			}
			tags = append(tags, args[i+1])
			i++
		case "--class":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "geblang test --class expects a class name")
				os.Exit(2)
			}
			classFilter = args[i+1]
			i++
		case "--method":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "geblang test --method expects a method name")
				os.Exit(2)
			}
			methodFilters = append(methodFilters, args[i+1])
			i++
		case "--verbose", "-v":
			format = "verbose"
		case "--format":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "geblang test --format expects one of summary, verbose, teamcity")
				os.Exit(2)
			}
			format = args[i+1]
			i++
		case "--allow-ffi":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "geblang test --allow-ffi expects a path or glob")
				os.Exit(2)
			}
			allowFFI = append(allowFFI, args[i+1])
			i++
		case "--allow-process-control":
			native.SetProcessControlEnabled(true)
		case "--allow-onnx":
			native.SetOnnxEnabled(true)
		case "--allow-browser":
			native.SetBrowserEnabled(true)
		default:
			paths = append(paths, args[i])
		}
	}
	switch format {
	case "summary", "verbose", "teamcity":
	default:
		fmt.Fprintf(os.Stderr, "geblang test --format must be one of summary, verbose, teamcity (got %q)\n", format)
		os.Exit(2)
	}
	if len(paths) != 1 {
		fmt.Fprintln(os.Stderr, "usage: geblang test [--tag name] [--verbose|--format <summary|verbose|teamcity>] <file-or-dir>")
		os.Exit(2)
	}
	files, err := discoverTestFiles(paths[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no test files found")
		os.Exit(1)
	}
	total := int64(0)
	failed := int64(0)
	skipped := int64(0)
	for _, file := range files {
		fileTotal, fileFailed, fileSkipped, err := runTestFile(file, tags, classFilter, methodFilters, format, allowFFI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			os.Exit(1)
		}
		total += fileTotal
		failed += fileFailed
		skipped += fileSkipped
	}
	passed := total - failed - skipped
	if format == "teamcity" {
		fmt.Printf("##teamcity[message text='tests: total=%d failed=%d passed=%d skipped=%d' status='NORMAL']\n", total, failed, passed, skipped)
	} else {
		fmt.Printf("tests: total=%d failed=%d passed=%d skipped=%d\n", total, failed, passed, skipped)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func discoverTestFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	files := []string{}
	err = filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.gb") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func runTestFile(path string, tags []string, classFilter string, methodFilters []string, format string, allowFFI []string) (int64, int64, int64, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	parsed, err := parseOnly(string(source))
	if err != nil {
		return 0, 0, 0, err
	}
	resolver := modules.NewResolver([]string{filepath.Dir(path)})
	// A test file declaring `module X;` merges with X's source so its
	// tests reach X's private members (check.SameModuleTestProgram).
	base, sameModule := check.SameModuleTestProgram(path, parsed, resolver)
	if !sameModule {
		if declared := check.DeclaredModuleName(parsed); declared != "" {
			return 0, 0, 0, fmt.Errorf("%s declares module %s but no module source resolves to that name; a same-module test file must sit alongside the module it tests", path, declared)
		}
		base = parsed
	}
	if err := analyzeCrossModule(path, base, resolver); err != nil {
		return 0, 0, 0, err
	}
	classes := testClasses(parsed)
	if classFilter != "" {
		filtered := classes[:0]
		for _, name := range classes {
			if name == classFilter {
				filtered = append(filtered, name)
			}
		}
		classes = filtered
	}
	if len(classes) == 0 {
		return 0, 0, 0, nil
	}
	runnerProgram, err := parseOnly(buildTestRunner(classes, tags, methodFilters, format))
	if err != nil {
		return 0, 0, 0, err
	}
	program := &ast.Program{Statements: append(append([]ast.Statement{}, base.Statements...), runnerProgram.Statements...)}
	if err := analyzeCrossModule(path, program, resolver); err != nil {
		return 0, 0, 0, err
	}
	var out strings.Builder
	// Pass the script's directory as a module path so the resolver
	// can walk up to find a project's geblang.yaml when the test
	// lives inside a non-stdlib package (e.g. gebweb/tests/).
	ev := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{filepath.Dir(path)})
	applyManifestCapabilities(filepath.Dir(path))
	if policy, perr := ev.BuildFFIPolicy(filepath.Dir(path), allowFFI); perr == nil {
		ev.SetFFIPolicy(policy)
	} else {
		return 0, 0, 0, perr
	}
	result, err := ev.Eval(program)
	if err != nil {
		return 0, 0, 0, err
	}
	if result.Exited && result.ExitCode != 0 {
		return 0, 0, 0, fmt.Errorf("test runner exited with %d", result.ExitCode)
	}
	printTestOutput(out.String())
	return parseTestSummary(out.String())
}

// analyzeCrossModule runs the cross-module static checks (unknown members,
// methods, and qualified types) in the compile path, returning the first
// error-severity diagnostic as a Go error. Warnings are ignored (the compile
// path is not --strict). resolver must resolve imports as geblang check does.
func analyzeCrossModule(file string, program *ast.Program, resolver *modules.Resolver) error {
	opts := check.Options{
		Resolver:      resolver,
		CrossModule:   true,
		NativeSymbols: evaluator.NativeModuleSymbols(),
		ModuleCache:   check.NewModuleCache(),
	}
	for _, d := range check.CrossModuleAnalysis(file, program, opts) {
		if d.Severity == check.SeverityError {
			return fmt.Errorf("%s:%d:%d: error[%s]: %s", file, d.Line, d.Column, d.Rule, d.Message)
		}
	}
	return nil
}

func parseAndAnalyze(file, source string) (*ast.Program, error) {
	program, err := parseOnly(source)
	if err != nil {
		return nil, err
	}
	if err := analyzeCrossModule(file, program, modules.NewResolver([]string{filepath.Dir(file)})); err != nil {
		return nil, err
	}
	return program, nil
}

func parseOnly(source string) (*ast.Program, error) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(p.Errors(), "\n"))
	}
	return program, nil
}

func testClasses(program *ast.Program) []string {
	classes := []string{}
	for _, stmt := range program.Statements {
		class, ok := stmt.(*ast.ClassStatement)
		if !ok || class.Extends == nil {
			continue
		}
		// The runner targets classes that derive (directly) from the
		// Test framework class. Source typically writes `extends test.Test`
		// after `import test`, which the parser stores as the fully-
		// qualified name "test.Test"; accept the bare form too for
		// callers who alias the import.
		switch class.Extends.Name {
		case "Test", "test.Test":
			classes = append(classes, class.Name.Value)
		}
	}
	return classes
}

func buildTestRunner(classes []string, tags []string, methodFilters []string, format string) string {
	var b strings.Builder
	b.WriteString("import io;\nimport sys;\nimport test;\n")
	b.WriteString("let __geb_total = 0;\nlet __geb_failed = 0;\nlet __geb_skipped = 0;\n")
	if format == "teamcity" {
		// Helper that escapes a string per the TeamCity service-message
		// spec: | -> ||, ' -> |', [ -> |[, ] -> |], \n -> |n, \r -> |r.
		// IDEs reject unescaped messages, so this must run on every
		// name / message / details value we splice into the output.
		b.WriteString("func __geb_tc_escape(any value): string {\n")
		b.WriteString("    string s = value as string;\n")
		b.WriteString("    string out = \"\";\n")
		b.WriteString("    for (let i = 0; i < s.length(); i = i + 1) {\n")
		b.WriteString("        string c = s.substring(i, i + 1);\n")
		b.WriteString("        if (c == \"|\") { out = out + \"||\"; }\n")
		b.WriteString("        else if (c == \"'\") { out = out + \"|'\"; }\n")
		b.WriteString("        else if (c == \"[\") { out = out + \"|[\"; }\n")
		b.WriteString("        else if (c == \"]\") { out = out + \"|]\"; }\n")
		b.WriteString("        else if (c == \"\\n\") { out = out + \"|n\"; }\n")
		b.WriteString("        else if (c == \"\\r\") { out = out + \"|r\"; }\n")
		b.WriteString("        else { out = out + c; }\n")
		b.WriteString("    }\n")
		b.WriteString("    return out;\n")
		b.WriteString("}\n")
	}
	tagList := "[]"
	if len(tags) > 0 {
		quoted := make([]string, 0, len(tags))
		for _, tag := range tags {
			quoted = append(quoted, strconvQuote(tag))
		}
		tagList = "[" + strings.Join(quoted, ", ") + "]"
	}
	methodList := "[]"
	if len(methodFilters) > 0 {
		quoted := make([]string, 0, len(methodFilters))
		for _, name := range methodFilters {
			quoted = append(quoted, strconvQuote(name))
		}
		methodList = "[" + strings.Join(quoted, ", ") + "]"
	}
	for i, className := range classes {
		resultName := fmt.Sprintf("__geb_result_%d", i)
		fmt.Fprintf(&b, "let %s = test.run(%s, {\"tags\": %s, \"methods\": %s});\n", resultName, className, tagList, methodList)
		fmt.Fprintf(&b, "__geb_total = __geb_total + %s[\"total\"];\n", resultName)
		fmt.Fprintf(&b, "__geb_failed = __geb_failed + %s[\"failed\"];\n", resultName)
		fmt.Fprintf(&b, "__geb_skipped = __geb_skipped + (%s[\"skipped\"] ?? 0);\n", resultName)
		switch format {
		case "teamcity":
			classTC := strconvQuote(className)
			fmt.Fprintf(&b, "io.println(\"##teamcity[testSuiteStarted name='\" + __geb_tc_escape(%s) + \"' locationHint='geblang_test://\" + __geb_tc_escape(%s) + \"']\");\n", classTC, classTC)
			fmt.Fprintf(&b, "for (__geb_case in %s[\"tests\"]) {\n", resultName)
			b.WriteString("    string __geb_tc_name = __geb_tc_escape(__geb_case[\"name\"]);\n")
			fmt.Fprintf(&b, "    string __geb_tc_loc = \"geblang_test://\" + __geb_tc_escape(%s) + \"/\" + __geb_tc_name;\n", classTC)
			b.WriteString("    io.println(\"##teamcity[testStarted name='\" + __geb_tc_name + \"' locationHint='\" + __geb_tc_loc + \"' captureStandardOutput='true']\");\n")
			b.WriteString("    if (__geb_case[\"skipped\"] ?? false) {\n")
			b.WriteString("        io.println(\"##teamcity[testIgnored name='\" + __geb_tc_name + \"' message='\" + __geb_tc_escape(__geb_case[\"message\"]) + \"']\");\n")
			b.WriteString("    } else if (!__geb_case[\"passed\"]) {\n")
			b.WriteString("        string __geb_tc_msg = __geb_tc_escape(__geb_case[\"message\"]);\n")
			b.WriteString("        io.println(\"##teamcity[testFailed name='\" + __geb_tc_name + \"' message='\" + __geb_tc_msg + \"']\");\n")
			b.WriteString("    }\n")
			b.WriteString("    io.println(\"##teamcity[testFinished name='\" + __geb_tc_name + \"']\");\n")
			b.WriteString("}\n")
			fmt.Fprintf(&b, "io.println(\"##teamcity[testSuiteFinished name='\" + __geb_tc_escape(%s) + \"']\");\n", classTC)
		case "verbose":
			fmt.Fprintf(&b, "io.println(%q);\n", className)
			fmt.Fprintf(&b, "for (__geb_case in %s[\"tests\"]) {\n", resultName)
			b.WriteString("  if (__geb_case[\"skipped\"] ?? false) {\n")
			b.WriteString("    io.println(\"  SKIP \" + __geb_case[\"name\"] + \": \" + __geb_case[\"message\"]);\n")
			b.WriteString("  } else if (__geb_case[\"passed\"]) {\n")
			b.WriteString("    io.println(\"  PASS \" + __geb_case[\"name\"]);\n")
			b.WriteString("  } else {\n")
			b.WriteString("    io.println(\"  FAIL \" + __geb_case[\"name\"] + \": \" + __geb_case[\"message\"]);\n")
			b.WriteString("  }\n")
			b.WriteString("}\n")
		default:
			fmt.Fprintf(&b, "for (__geb_failure in %s[\"failures\"]) { io.println(\"FAIL %s: \" + __geb_failure); }\n", resultName, className)
		}
	}
	b.WriteString("io.println(\"__GEB_TEST_SUMMARY__ \" + (__geb_total as string) + \" \" + (__geb_failed as string) + \" \" + (__geb_skipped as string));\n")
	return b.String()
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func parseTestSummary(output string) (int64, int64, int64, error) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "__GEB_TEST_SUMMARY__ ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 4 {
			return 0, 0, 0, fmt.Errorf("invalid test summary")
		}
		var total, failed, skipped int64
		if _, err := fmt.Sscan(parts[1], &total); err != nil {
			return 0, 0, 0, err
		}
		if _, err := fmt.Sscan(parts[2], &failed); err != nil {
			return 0, 0, 0, err
		}
		if _, err := fmt.Sscan(parts[3], &skipped); err != nil {
			return 0, 0, 0, err
		}
		return total, failed, skipped, nil
	}
	return 0, 0, 0, fmt.Errorf("missing test summary")
}

func printTestOutput(output string) {
	for _, line := range strings.SplitAfter(output, "\n") {
		if strings.HasPrefix(strings.TrimSuffix(line, "\n"), "__GEB_TEST_SUMMARY__ ") {
			continue
		}
		fmt.Print(line)
	}
}

// crossModuleAnalyzer returns a closure that runs the cross-module static
// analysis for the given file, prints warnings to stdout, and returns the
// first error-severity diagnostic. It is invoked only on a cache miss, so a
// .gbc hit provably implies a prior clean analysis.
// analysisError tags a cross-module analysis failure so the run path treats
// it as a hard error, not a VM parity gap (the message can contain words like
// "export" that the parity heuristic matches).
type analysisError struct{ msg string }

func (e analysisError) Error() string { return e.msg }

func crossModuleAnalyzer(sourcePath string, program *ast.Program, resolver *modules.Resolver, stdout io.Writer, warnPrefix string) func() error {
	return func() error {
		opts := check.Options{Resolver: resolver, CrossModule: true, NativeSymbols: evaluator.NativeModuleSymbols(), ModuleCache: check.NewModuleCache()}
		var firstError error
		for _, d := range check.CrossModuleAnalysis(sourcePath, program, opts) {
			if d.Severity == check.SeverityWarning {
				fmt.Fprintf(stdout, "%s%s\n", warnPrefix, d.Message)
				continue
			}
			if firstError == nil {
				firstError = analysisError{msg: d.Message}
			}
		}
		return firstError
	}
}

func loadOrCompileBytecode(sourcePath string, source []byte, astProgram *ast.Program, analyze func() error) (bytecode.Chunk, error) {
	cacheDir := bytecodeCacheDir()
	cachePath := bytecode.CachePath(cacheDir, sourcePath, source, version)
	// --no-assert mutates the compiled chunk in a way the cache key
	// doesn't capture; skip the cache on both read and write so the
	// next normal run doesn't pick up an asserts-elided chunk.
	if !bytecode.AssertionsDisabled {
		if data, err := os.ReadFile(cachePath); err == nil {
			chunk, err := bytecode.Decode(data)
			if err == nil && chunk.Compiler == version && chunk.SourceHash == bytecode.SourceHash(source) {
				return chunk, nil
			}
		}
	}
	// Cache miss: analyze before compiling so a cached chunk always
	// reflects a clean prior analysis; abort without caching on error.
	if analyze != nil {
		if err := analyze(); err != nil {
			return bytecode.Chunk{}, err
		}
	}
	chunk, err := bytecode.CompileWithOptions(astProgram, source, version, bytecode.CompileOptions{NativeSymbols: evaluator.CachedNativeModuleSymbols()})
	if err != nil {
		return bytecode.Chunk{}, err
	}
	if bytecode.AssertionsDisabled {
		return chunk, nil
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		return bytecode.Chunk{}, err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		_ = os.WriteFile(cachePath, encoded, 0o644)
	}
	return chunk, nil
}

func bytecodeCacheRoot() string {
	return ".geblang-cache"
}

func bytecodeCacheDir() string {
	return filepath.Join(bytecodeCacheRoot(), version)
}
