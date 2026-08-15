package transpiler_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/embedfold"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/transpiler"
)

// TestThreeWayParity runs every program in tests/transpile/ three ways - the
// bytecode VM, the tree-walking evaluator, and the transpiled native binary -
// and asserts byte-identical stdout plus matching exit code across all three.
// A corpus program is transpile-safe by construction: if --native diagnoses,
// fails go build, or diverges, the test fails (the net is working).
func TestThreeWayParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping three-way native parity in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("three-way harness assumes POSIX")
	}

	repoRoot := repoRootFromTest(t)
	geblangBin := findGeblangBinary(t, repoRoot)
	corpusDir := filepath.Join(repoRoot, "tests", "transpile")

	progs, err := filepath.Glob(filepath.Join(corpusDir, "*.gb"))
	if err != nil {
		t.Fatalf("glob corpus: %v", err)
	}
	if len(progs) == 0 {
		t.Skip("no programs in tests/transpile")
	}
	sort.Strings(progs)

	// One shared go-build cache (set once for the package) keeps the per-program
	// `go build` cost down; programs run in parallel subtests.
	for _, prog := range progs {
		prog := prog
		name := strings.TrimSuffix(filepath.Base(prog), ".gb")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runThreeWay(t, repoRoot, geblangBin, prog)
		})
	}
}

func runThreeWay(t *testing.T, repoRoot, geblangBin, progPath string) {
	t.Helper()

	progArgs := readArgsSidecar(t, progPath)

	vmOut, vmCode := runScript(t, repoRoot, geblangBin, progPath, "--vm-strict", progArgs)
	evalOut, evalCode := runScript(t, repoRoot, geblangBin, progPath, "--disable-vm", progArgs)

	if vmCode != evalCode {
		t.Fatalf("VM vs evaluator exit code differ: vm=%d eval=%d", vmCode, evalCode)
	}
	if string(vmOut) != string(evalOut) {
		t.Fatalf("VM vs evaluator stdout differ\n--- vm ---\n%q\n--- eval ---\n%q", vmOut, evalOut)
	}

	natOut, natCode := buildAndRunNative(t, repoRoot, progPath, progArgs)

	if vmCode != natCode {
		t.Fatalf("VM vs native exit code differ: vm=%d native=%d\nnative output: %q", vmCode, natCode, natOut)
	}
	if string(vmOut) != string(natOut) {
		t.Fatalf("VM vs native stdout differ\n--- vm ---\n%q\n--- native ---\n%q", vmOut, natOut)
	}
}

// readArgsSidecar returns the program arguments from an optional
// "<prog>.args" file (one arg per non-empty line), so a fixture can exercise
// argument-dependent paths (e.g. sys.args + toInt) across all three backends.
func readArgsSidecar(t *testing.T, progPath string) []string {
	t.Helper()
	data, err := os.ReadFile(progPath + ".args")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// runScript runs a single-file program with the given backend flag and returns
// stdout plus the process exit code.
func runScript(t *testing.T, dir, bin, progPath, flag string, progArgs []string) ([]byte, int) {
	t.Helper()
	cmdArgs := append([]string{flag, progPath}, progArgs...)
	cmd := exec.Command(bin, cmdArgs...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := exitCode(err)
	if err != nil && code == -1 {
		t.Fatalf("run %s %s: %v\nstderr: %s", flag, progPath, err, stderr.String())
	}
	return stdout.Bytes(), code
}

// buildAndRunNative transpiles the program, builds it offline against the live
// geblang checkout (replace geblang => repoRoot, no network), and runs it.
func buildAndRunNative(t *testing.T, repoRoot, progPath string, progArgs []string) ([]byte, int) {
	t.Helper()
	src, err := os.ReadFile(progPath)
	if err != nil {
		t.Fatalf("read %s: %v", progPath, err)
	}
	p := parser.New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parse %s: %s", progPath, strings.Join(errs, "; "))
	}
	if _, diags := embedfold.Fold(prog, progPath, embedfold.Inline); len(diags) > 0 {
		t.Fatalf("fold %s: %s", progPath, diags[0].Message)
	}
	module := strings.TrimSuffix(filepath.Base(progPath), ".gb")

	out, diags, err := transpiler.Transpile(transpiler.Input{
		Modules: map[string]*ast.Program{module: prog},
	}, transpiler.Options{EntryModule: module})
	if err != nil {
		t.Fatalf("transpile %s: %v", progPath, err)
	}
	for _, d := range diags {
		if d.Severity == transpiler.SeverityError {
			t.Fatalf("transpile-safe program %s diagnosed (not transpile-safe): %s", progPath, d)
		}
	}

	work := t.TempDir()
	writeOutputTree(t, work, out)
	writeGoMod(t, work, repoRoot)

	gotOut, code, err := goBuildAndRun(t, work, progArgs)
	if err != nil {
		t.Fatalf("native build %s: %v\noutput: %s", progPath, err, gotOut)
	}
	return gotOut, code
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
