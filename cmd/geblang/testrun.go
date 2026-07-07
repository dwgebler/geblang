package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
	"geblang/internal/check"
	"geblang/internal/evaluator"
	"geblang/internal/modules"
	"geblang/internal/runtime"
)

// ledgerFileName is the per-corpus divergence ledger, found by walking up from the test path.
const ledgerFileName = "KNOWN_DIVERGENCES.md"

type preparedTest struct {
	program *ast.Program
	source  []byte
	dir     string
}

// prepareTestProgram builds the merged runner program; a nil result (no error) means the file declares no test classes.
func prepareTestProgram(path string, tags []string, classFilter string, methodFilters []string, format string) (*preparedTest, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := parseOnly(string(source))
	if err != nil {
		return nil, err
	}
	resolver := modules.NewResolver([]string{filepath.Dir(path)})
	base, sameModule := check.SameModuleTestProgram(path, parsed, resolver)
	if !sameModule {
		if declared := check.DeclaredModuleName(parsed); declared != "" {
			return nil, fmt.Errorf("%s declares module %s but no module source resolves to that name; a same-module test file must sit alongside the module it tests", path, declared)
		}
		base = parsed
	}
	if err := analyzeCrossModule(path, base, resolver); err != nil {
		return nil, err
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
		return nil, nil
	}
	runnerProgram, err := parseOnly(buildTestRunner(classes, tags, methodFilters, format))
	if err != nil {
		return nil, err
	}
	program := &ast.Program{Statements: append(append([]ast.Statement{}, base.Statements...), runnerProgram.Statements...)}
	if err := analyzeCrossModule(path, program, resolver); err != nil {
		return nil, err
	}
	return &preparedTest{program: program, source: source, dir: filepath.Dir(path)}, nil
}

func runTestFileEvaluator(prep *preparedTest, allowFFI []string) (int64, int64, int64, error) {
	var out strings.Builder
	ev := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{prep.dir})
	applyManifestCapabilities(prep.dir)
	if policy, perr := ev.BuildFFIPolicy(prep.dir, allowFFI); perr == nil {
		ev.SetFFIPolicy(policy)
	} else {
		return 0, 0, 0, perr
	}
	result, err := ev.Eval(prep.program)
	if err != nil {
		return 0, 0, 0, err
	}
	if result.Exited && result.ExitCode != 0 {
		return 0, 0, 0, fmt.Errorf("test runner exited with %d", result.ExitCode)
	}
	printTestOutput(out.String())
	return parseTestSummary(out.String())
}

// runTestFileVM runs the runner program on the bytecode VM; a VM compile gap is a loud failure, never a silent evaluator fallback.
func runTestFileVM(prep *preparedTest, path string, allowFFI []string) (int64, int64, int64, error) {
	var out strings.Builder
	basePaths := []string{prep.dir}
	chunk, err := bytecode.CompileWithOptions(prep.program, prep.source, version, bytecode.CompileOptions{NativeSymbols: evaluator.CachedNativeModuleSymbols()})
	if err != nil {
		return 0, 0, 0, vmTestCompileError(err)
	}
	stateful := evaluator.NewWithArgsAndModulePaths(&out, nil, basePaths)
	stateful.AssertionsDisabled = bytecode.AssertionsDisabled
	defer stateful.Cleanup()
	applyManifestCapabilities(prep.dir)
	if policy, perr := stateful.BuildFFIPolicy(prep.dir, allowFFI); perr == nil {
		stateful.SetFFIPolicy(policy)
	} else {
		return 0, 0, 0, perr
	}
	loaderOpts := bcloader.Options{
		Compile: func(canonical, sp string, src []byte, prog *ast.Program, modPaths []string) (bytecode.Chunk, error) {
			resolverPaths := append([]string{filepath.Dir(sp)}, modPaths...)
			// Imported-module warnings go to stderr so the captured test output stays clean.
			an := crossModuleAnalyzer(sp, prog, modules.NewResolver(resolverPaths), os.Stderr, fmt.Sprintf("warning: module %s: ", canonical))
			return loadOrCompileBytecode(sp, src, prog, an)
		},
		LookupBuiltin: func(canonical, alias string) *runtime.Module {
			return stateful.BuiltinModule(canonical, alias)
		},
	}
	loader := bcloader.New(&out, basePaths, stateful, loaderOpts)
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &out, loader)
	defer vm.Cleanup()
	loader.SetMainVM(vm)
	vm.SetModulePaths(basePaths)
	vm.SetStatefulNativeCaller(stateful)
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		var exitErr bytecode.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Code != 0 {
				return 0, 0, 0, fmt.Errorf("test runner exited with %d", exitErr.Code)
			}
		} else {
			return 0, 0, 0, err
		}
	}
	printTestOutput(out.String())
	return parseTestSummary(out.String())
}

// vmTestCompileError points a VM parity gap at the escape hatch; any other compile error passes through unchanged.
func vmTestCompileError(err error) error {
	if !isBytecodeParityError(err) {
		return err
	}
	return fmt.Errorf("cannot run on the bytecode VM: %v\n"+
		"  geblang test runs on the VM by default. If this is a known, accepted divergence,\n"+
		"  add a '# @vm-divergence: <key>' comment to the file and a matching %s row;\n"+
		"  otherwise the VM gap must be fixed. Pass --runtime=evaluator to run the whole suite on the evaluator", err, ledgerFileName)
}

// vmDivergenceRe matches the file-level escape-hatch marker, e.g. `# @vm-divergence: some-key`.
var vmDivergenceRe = regexp.MustCompile(`@vm-divergence\s*:?\s*([A-Za-z0-9._-]+)`)

// scanVMDivergence returns the file's accepted-divergence key, or "" when absent; a file declares at most one.
func scanVMDivergence(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := ""
	for _, line := range strings.Split(string(data), "\n") {
		m := vmDivergenceRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if key != "" && key != m[1] {
			return "", fmt.Errorf("%s: multiple @vm-divergence keys (%s, %s); use one key per file", path, key, m[1])
		}
		key = m[1]
	}
	return key, nil
}

type divergenceEntry struct {
	key     string
	file    string
	rawFile string
	reason  string
	date    string
}

type divergenceLedger struct {
	path    string // absolute path to the ledger, "" when none was found
	dir     string
	entries []divergenceEntry
}

// loadDivergenceLedger finds and parses the nearest ledger walking up from the test path; a missing ledger yields an empty one.
func loadDivergenceLedger(testPath string) (*divergenceLedger, error) {
	start := testPath
	if info, err := os.Stat(testPath); err == nil && !info.IsDir() {
		start = filepath.Dir(testPath)
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for dir := abs; ; {
		candidate := filepath.Join(dir, ledgerFileName)
		if data, err := os.ReadFile(candidate); err == nil {
			return parseDivergenceLedger(candidate, data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return &divergenceLedger{}, nil
		}
		dir = parent
	}
}

func parseDivergenceLedger(path string, data []byte) (*divergenceLedger, error) {
	l := &divergenceLedger{path: path, dir: filepath.Dir(path)}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitLedgerRow(trimmed)
		if len(cells) != 4 {
			continue
		}
		if strings.EqualFold(cells[0], "key") && strings.EqualFold(cells[1], "file") {
			continue
		}
		if isLedgerSeparator(cells) {
			continue
		}
		if cells[0] == "" || cells[1] == "" {
			return nil, fmt.Errorf("%s: divergence row missing key or file: %q", path, line)
		}
		abs := cells[1]
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(l.dir, abs)
		}
		l.entries = append(l.entries, divergenceEntry{
			key:     cells[0],
			file:    filepath.Clean(abs),
			rawFile: cells[1],
			reason:  cells[2],
			date:    cells[3],
		})
	}
	return l, nil
}

func splitLedgerRow(line string) []string {
	parts := strings.Split(line, "|")
	if len(parts) >= 2 {
		parts = parts[1 : len(parts)-1]
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func isLedgerSeparator(cells []string) bool {
	for _, c := range cells {
		trimmed := strings.Trim(c, ": ")
		if trimmed == "" || strings.Trim(trimmed, "-") != "" {
			return false
		}
	}
	return true
}

// validateDivergences requires every annotation to have a ledger row and every ledger row to have a matching annotation.
func validateDivergences(files []string, annotations map[string]string, ledger *divergenceLedger) error {
	ledgerByFile := map[string]map[string]bool{}
	for _, e := range ledger.entries {
		if ledgerByFile[e.file] == nil {
			ledgerByFile[e.file] = map[string]bool{}
		}
		ledgerByFile[e.file][e.key] = true
	}
	for _, file := range files {
		key := annotations[file]
		if key == "" {
			continue
		}
		abs, err := filepath.Abs(file)
		if err != nil {
			return err
		}
		abs = filepath.Clean(abs)
		if ledger.path == "" {
			return fmt.Errorf("%s: @vm-divergence:%s annotation has no %s ledger; create one next to the tests with a matching row", file, key, ledgerFileName)
		}
		if !ledgerByFile[abs][key] {
			return fmt.Errorf("%s: @vm-divergence:%s annotation has no matching row in %s", file, key, ledger.path)
		}
	}
	for _, e := range ledger.entries {
		if _, err := os.Stat(e.file); err != nil {
			return fmt.Errorf("%s: lists %s (key %s) but the file does not exist", ledger.path, e.rawFile, e.key)
		}
		got, err := scanVMDivergence(e.file)
		if err != nil {
			return err
		}
		if got != e.key {
			return fmt.Errorf("%s: lists %s (key %s) but the file has no matching @vm-divergence:%s annotation", ledger.path, e.rawFile, e.key, e.key)
		}
	}
	return nil
}
