package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/profiling"
)

const profileDonorGb = `module donor;
export func work(int n): int {
    int s = 0;
    for (i in 0..<n) {
        s = (s + i * 7) % 1000003;
    }
    return s;
}
`

const profileMainGb = `import io;
import donor;
func inner(int n): int {
    int s = 0;
    for (i in 0..<n) {
        s = (s + i % 7) % 1000003;
    }
    return s;
}
func outer(int rounds): int {
    int t = 0;
    for (k in 0..<rounds) {
        t = (t + inner(20000) + donor.work(20000)) % 1000003;
    }
    return t;
}
io.println(outer(ROUNDS) >= 0);
`

// profiledStacks runs fn with sampling on and returns the folded stacks it recorded.
func profiledStacks(t *testing.T, fn func()) map[string]int {
	t.Helper()
	stop := profiling.Collect()
	fn()
	return stop()
}

// hasStack reports whether a recorded stack ends with the given frames.
func hasStack(stacks map[string]int, suffix string) bool {
	for key := range stacks {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// Sampling names the same Geblang call stacks on both backends: functions in
// the main script, a function in another module, and loops in each.
func TestParityProfileStacks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(profileDonorGb), 0o644); err != nil {
		t.Fatal(err)
	}
	// The evaluator is much slower, so it runs fewer rounds for a similar time.
	evalSource := strings.Replace(profileMainGb, "ROUNDS", "8", 1)
	vmSource := strings.Replace(profileMainGb, "ROUNDS", "200", 1)

	evStacks := profiledStacks(t, func() {
		p := parser.New(lexer.New(evalSource))
		program := p.ParseProgram()
		var out bytes.Buffer
		if _, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir}).Eval(program); err != nil {
			t.Fatalf("evaluator error: %v", err)
		}
	})
	vmStacks := profiledStacks(t, func() {
		p := parser.New(lexer.New(vmSource))
		program := p.ParseProgram()
		aliases := bcloader.SourceTypeAliasLookup(program, "", []string{dir})
		chunk, err := bytecode.CompileWithOptions(program, []byte(vmSource), "parity", bytecode.CompileOptions{TypeAliasLookup: aliases})
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}
		var out bytes.Buffer
		stateful := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir})
		loader := newHarnessLoader(&out, stateful)
		loader.SetModulePaths([]string{dir})
		loader.SetMainChunk(chunk)
		vm := bytecode.NewVMWithModuleLoader(chunk, &out, loader)
		loader.SetMainVM(vm)
		vm.SetModulePaths([]string{dir})
		vm.SetStatefulNativeCaller(stateful)
		stateful.SetMethodDispatcher(vm)
		if err := vm.Run(); err != nil {
			t.Fatalf("vm error: %v", err)
		}
	})
	for name, stacks := range map[string]map[string]int{"evaluator": evStacks, "vm": vmStacks} {
		for _, want := range []string{"<top level>;main.outer;main.inner", "<top level>;main.outer;donor.work"} {
			if !hasStack(stacks, want) {
				t.Errorf("%s profile has no stack %q; got %v", name, want, stacks)
			}
		}
	}
}

const profileTasksGb = `import async;
import io;
import donor;
func spin(int rounds): int {
    int t = 0;
    for (k in 0..<rounds) {
        t = (t + donor.work(5000)) % 1000003;
        let f = func(int n): int { return donor.work(n); };
        t = (t + f(2000)) % 1000003;
    }
    return t;
}
list<any> tasks = [];
for (w in 0..<6) {
    tasks.push(async.run(func(): int { return spin(40); }));
}
int sum = 0;
for (task in tasks) {
    sum = (sum + (async.await(task) as int)) % 1000003;
}
io.println(sum >= 0);
`

// Sampling async tasks that call across modules records their stacks (and,
// under -race, never reads a VM running on another goroutine).
func TestProfileAsyncTasks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(profileDonorGb), 0o644); err != nil {
		t.Fatal(err)
	}
	stacks := profiledStacks(t, func() {
		p := parser.New(lexer.New(profileTasksGb))
		program := p.ParseProgram()
		aliases := bcloader.SourceTypeAliasLookup(program, "", []string{dir})
		chunk, err := bytecode.CompileWithOptions(program, []byte(profileTasksGb), "parity", bytecode.CompileOptions{TypeAliasLookup: aliases})
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}
		var out bytes.Buffer
		stateful := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir})
		loader := newHarnessLoader(&out, stateful)
		loader.SetModulePaths([]string{dir})
		loader.SetMainChunk(chunk)
		vm := bytecode.NewVMWithModuleLoader(chunk, &out, loader)
		loader.SetMainVM(vm)
		vm.SetModulePaths([]string{dir})
		vm.SetStatefulNativeCaller(stateful)
		stateful.SetMethodDispatcher(vm)
		if err := vm.Run(); err != nil {
			t.Fatalf("vm error: %v", err)
		}
		if out.String() != "true\n" {
			t.Fatalf("output %q", out.String())
		}
	})
	if !hasStack(stacks, "main.spin;donor.work") {
		t.Errorf("no task stack through donor.work; got %v", stacks)
	}
}
