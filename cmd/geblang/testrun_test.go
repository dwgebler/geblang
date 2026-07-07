package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const passingTestFile = `import test;
class CalcTest extends test.Test {
    @test
    func adds(): void { this.assertEquals(4, 2 + 2); }
    @test
    func subs(): void { this.assertEquals(0, 2 - 2); }
}
`

// Partial application over two same-arity overloads: a VM parity gap the evaluator resolves at runtime.
const parityGapTestFile = `import test;
func describe(int x): string { return "int:${x}"; }
func describe(string x): string { return "str:${x}"; }
class PartialTest extends test.Test {
    @test
    func resolvesAtApplication(): void {
        let d = describe(_);
        this.assertEquals("int:42", d(42));
    }
}
`

func writeTestCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRunnerRunsOnVMByDefault checks the VM lane runs a passing suite and fails loudly on a parity gap.
func TestRunnerRunsOnVMByDefault(t *testing.T) {
	bin := buildGeblangBinary(t, false)

	t.Run("passing suite green on VM", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{"calc_test.gb": passingTestFile})
		out, err := exec.Command(bin, "test", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("expected success, got %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "tests: total=2 failed=0 passed=2 skipped=0") {
			t.Fatalf("unexpected summary:\n%s", out)
		}
	})

	t.Run("VM parity gap fails loudly and names the file", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{"partial_test.gb": parityGapTestFile})
		out, err := exec.Command(bin, "test", dir).CombinedOutput()
		if err == nil {
			t.Fatalf("expected loud failure, got success:\n%s", out)
		}
		got := string(out)
		for _, want := range []string{"partial_test.gb", "cannot run on the bytecode VM", "@vm-divergence"} {
			if !strings.Contains(got, want) {
				t.Fatalf("loud failure missing %q:\n%s", want, got)
			}
		}
	})
}

// TestRunnerEvaluatorLane checks --runtime=evaluator / --disable-vm run the whole suite on the evaluator.
func TestRunnerEvaluatorLane(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := writeTestCorpus(t, map[string]string{"partial_test.gb": parityGapTestFile})
	for _, flag := range []string{"--runtime=evaluator", "--disable-vm"} {
		out, err := exec.Command(bin, "test", flag, dir).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: expected success, got %v\n%s", flag, err, out)
		}
		if !strings.Contains(string(out), "tests: total=1 failed=0 passed=1 skipped=0") {
			t.Fatalf("%s: unexpected summary:\n%s", flag, out)
		}
		if strings.Contains(string(out), "accepted divergences") {
			t.Fatalf("%s: evaluator lane must not report accepted divergences:\n%s", flag, out)
		}
	}
}

// TestRunnerEscapeHatch checks an annotated + ledgered file runs on the evaluator and is reported.
func TestRunnerEscapeHatch(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := writeTestCorpus(t, map[string]string{
		"partial_test.gb": "# @vm-divergence: overload-partial\n" + parityGapTestFile,
		"KNOWN_DIVERGENCES.md": "| key | file | reason | date |\n" +
			"|-----|------|--------|------|\n" +
			"| overload-partial | partial_test.gb | partial over same-arity overloads is compile-time on the VM | 2026-07-06 |\n",
	})
	out, err := exec.Command(bin, "test", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("expected success via escape hatch, got %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "accepted divergences: 1 file(s) ran on the evaluator") {
		t.Fatalf("missing accepted-divergence report:\n%s", got)
	}
	if !strings.Contains(got, "tests: total=1 failed=0 passed=1 skipped=0") {
		t.Fatalf("unexpected summary:\n%s", got)
	}
}

// TestRunnerCrossValidation checks annotation/ledger mismatches in either direction are hard errors.
func TestRunnerCrossValidation(t *testing.T) {
	bin := buildGeblangBinary(t, false)

	t.Run("annotation without ledger row", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{
			"partial_test.gb": "# @vm-divergence: overload-partial\n" + parityGapTestFile,
		})
		out, err := exec.Command(bin, "test", dir).CombinedOutput()
		if err == nil {
			t.Fatalf("expected error, got success:\n%s", out)
		}
		if !strings.Contains(string(out), "no") || !strings.Contains(string(out), "overload-partial") {
			t.Fatalf("expected annotation-without-ledger error:\n%s", out)
		}
	})

	t.Run("ledger row without annotation", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{
			"calc_test.gb": passingTestFile,
			"KNOWN_DIVERGENCES.md": "| key | file | reason | date |\n" +
				"|-----|------|--------|------|\n" +
				"| stray | calc_test.gb | x | 2026-07-06 |\n",
		})
		out, err := exec.Command(bin, "test", dir).CombinedOutput()
		if err == nil {
			t.Fatalf("expected error, got success:\n%s", out)
		}
		if !strings.Contains(string(out), "no matching @vm-divergence:stray") {
			t.Fatalf("expected ledger-without-annotation error:\n%s", out)
		}
	})
}

func TestScanVMDivergence(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	none := write("none.gb", "import test;\n")
	if key, err := scanVMDivergence(none); err != nil || key != "" {
		t.Fatalf("no annotation: got (%q, %v)", key, err)
	}
	hash := write("hash.gb", "# @vm-divergence: my-key\nimport test;\n")
	if key, err := scanVMDivergence(hash); err != nil || key != "my-key" {
		t.Fatalf("hash comment: got (%q, %v)", key, err)
	}
	block := write("block.gb", "/* @vm-divergence some.key */\nimport test;\n")
	if key, err := scanVMDivergence(block); err != nil || key != "some.key" {
		t.Fatalf("block comment: got (%q, %v)", key, err)
	}
	conflict := write("conflict.gb", "# @vm-divergence: a\n# @vm-divergence: b\n")
	if _, err := scanVMDivergence(conflict); err == nil {
		t.Fatal("conflicting keys must error")
	}
}

func TestParseDivergenceLedger(t *testing.T) {
	data := "# Header\n\n" +
		"| key | file | reason | date |\n" +
		"|-----|------|--------|------|\n" +
		"| k1 | a_test.gb | reason one | 2026-07-06 |\n" +
		"| k2 | sub/b_test.gb | reason two | 2026-07-06 |\n"
	ledger, err := parseDivergenceLedger("/corpus/KNOWN_DIVERGENCES.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ledger.entries))
	}
	if ledger.entries[0].key != "k1" || ledger.entries[0].file != filepath.Clean("/corpus/a_test.gb") {
		t.Fatalf("entry 0 wrong: %+v", ledger.entries[0])
	}
	if ledger.entries[1].file != filepath.Clean("/corpus/sub/b_test.gb") {
		t.Fatalf("entry 1 file wrong: %+v", ledger.entries[1])
	}
	if _, err := parseDivergenceLedger("/c/L.md", []byte("| | x_test.gb | r | d |\n")); err == nil {
		t.Fatal("row with empty key must error")
	}
}

func TestValidateDivergences(t *testing.T) {
	dir := t.TempDir()
	annotated := filepath.Join(dir, "a_test.gb")
	if err := os.WriteFile(annotated, []byte("# @vm-divergence: k1\nimport test;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "b_test.gb")
	if err := os.WriteFile(plain, []byte("import test;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(dir, ledgerFileName)
	annAbs := filepath.Clean(annotated)

	okLedger := &divergenceLedger{path: ledgerPath, dir: dir, entries: []divergenceEntry{{key: "k1", file: annAbs, rawFile: "a_test.gb"}}}
	if err := validateDivergences([]string{annotated, plain}, map[string]string{annotated: "k1"}, okLedger); err != nil {
		t.Fatalf("valid pairing should pass: %v", err)
	}

	empty := &divergenceLedger{}
	if err := validateDivergences([]string{annotated}, map[string]string{annotated: "k1"}, empty); err == nil {
		t.Fatal("annotation with no ledger must error")
	}

	strayLedger := &divergenceLedger{path: ledgerPath, dir: dir, entries: []divergenceEntry{{key: "k1", file: filepath.Clean(plain), rawFile: "b_test.gb"}}}
	if err := validateDivergences([]string{plain}, map[string]string{plain: ""}, strayLedger); err == nil {
		t.Fatal("ledger row without a matching annotation must error")
	}

	ghostLedger := &divergenceLedger{path: ledgerPath, dir: dir, entries: []divergenceEntry{{key: "k1", file: filepath.Join(dir, "ghost_test.gb"), rawFile: "ghost_test.gb"}}}
	if err := validateDivergences(nil, map[string]string{}, ghostLedger); err == nil {
		t.Fatal("ledger row for a missing file must error")
	}
}

func TestVMTestCompileErrorHint(t *testing.T) {
	src := "func describe(int x): string { return \"i\"; }\n" +
		"func describe(string x): string { return \"s\"; }\n" +
		"let d = describe(_);\n"
	program := parser.New(lexer.New(src)).ParseProgram()
	_, err := bytecode.Compile(program, []byte(src), "test")
	if err == nil {
		t.Fatal("expected a compile error for the partial-over-overloads snippet")
	}
	if !bytecode.IsParityError(err) {
		t.Fatalf("expected a parity error, got: %v", err)
	}
	hinted := vmTestCompileError(err)
	for _, want := range []string{"cannot run on the bytecode VM", "@vm-divergence", ledgerFileName} {
		if !strings.Contains(hinted.Error(), want) {
			t.Fatalf("parity-gap hint missing %q: %v", want, hinted)
		}
	}

	plain := errors.New("some genuine static error")
	if got := vmTestCompileError(plain); got.Error() != plain.Error() {
		t.Fatalf("non-parity error must pass through unchanged, got: %v", got)
	}
}
