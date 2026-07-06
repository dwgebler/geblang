package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParityHofCallbackCaughtThrow pins byte-identical abort/catch behaviour when a native HOF callback throws (the inline path once jumped into the enclosing try and underflowed the VM stack).
func TestParityHofCallbackCaughtThrow(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "Map",
			source: `import io;
func fn(any x): any {
    if (x == 2) { throw RuntimeError("boom"); }
    return x * 2;
}
try {
    let out = [1, 2, 3].map(fn);
    io.println("mapped: ${out}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: boom\ndone\n",
		},
		{
			name: "SortBy",
			source: `import io;
func keyFn(any x): any {
    if (x == 2) { throw RuntimeError("boom-sort"); }
    return x;
}
try {
    let s = [3, 1, 2].sortBy(keyFn);
    io.println("sorted: ${s}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("after");
`,
			want: "caught: boom-sort\nafter\n",
		},
		{
			name: "Reduce",
			source: `import io;
func red(any acc, any x): any {
    if (x == 2) { throw RuntimeError("boom-red"); }
    return acc + x;
}
try {
    let r = [1, 2, 3].reduce(red, 0);
    io.println("reduced: ${r}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("end");
`,
			want: "caught: boom-red\nend\n",
		},
		{
			name: "FilterThenReuse",
			source: `import io;
func keep(any x): bool {
    if (x == 2) { throw RuntimeError("boom-filter"); }
    return x > 0;
}
try {
    let f = [1, 2, 3].filter(keep);
    io.println("filtered: ${f}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
# The stack must be intact enough to run another HOF after the catch.
let b = [4, 5].map(func(any x): any { return x + 1; });
io.println("b: ${b}");
`,
			want: "caught: boom-filter\nb: [5, 6]\n",
		},
		{
			name: "GroupBy",
			source: `import io;
func keyFn(any x): any {
    if (x == 2) { throw RuntimeError("boom-group"); }
    return x % 2;
}
try {
    let g = [1, 2, 3].groupBy(keyFn);
    io.println("grouped: ${g}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: boom-group\ndone\n",
		},
		{
			name: "InnerCatchContinuesIteration",
			source: `import io;
func withInner(any x): any {
    try {
        if (x == 2) { throw RuntimeError("inner"); }
        return x;
    } catch (RuntimeError e) {
        return -1;
    }
}
let m = [1, 2, 3].map(withInner);
io.println("m: ${m}");
`,
			want: "m: [1, -1, 3]\n",
		},
		{
			name: "NestedHofCallbackThrow",
			source: `import io;
func outerCb(any x): any {
    return [1, 2].map(func(any y): any {
        if (x == 2 && y == 2) { throw RuntimeError("nested-boom"); }
        return x * y;
    });
}
try {
    let n = [1, 2, 3].map(outerCb);
    io.println("n: ${n}");
} catch (RuntimeError e) {
    io.println("nested caught: ${e.message}");
}
io.println("done");
`,
			want: "nested caught: nested-boom\ndone\n",
		},
		{
			name: "CollectionsModule",
			source: `import io;
import collections;
func fn(any x): any {
    if (x == 2) { throw RuntimeError("boom-col"); }
    return x;
}
try {
    let out = collections.map([1, 2, 3], fn);
    io.println("mapped: ${out}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: boom-col\ndone\n",
		},
		{
			// A capturing same-module closure runs on a separate VM; the stitch must keep the throw catchable.
			name: "CapturingClosureSeparateVMCaught",
			source: `import io;
let tag = 9;
try {
    let s = [3, 1, 2].sortBy(func(int x): int {
        if (x == 1) { throw RuntimeError("boom cap ${tag}"); }
        return x;
    });
    io.println("${s}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: boom cap 9\ndone\n",
		},
		{
			name: "CallableInstanceSeparateVMCaught",
			source: `import io;
class Keyer {
    func __invoke(int x): int {
        if (x == 1) { throw RuntimeError("boom invoke caught"); }
        return x;
    }
}
try {
    let s = [3, 1, 2].sortBy(Keyer());
    io.println("${s}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: boom invoke caught\ndone\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParity(t, tc.source, tc.want)
		})
	}
}

// TestParityHofCallbackCaughtThrowCrossModule pins that a cross-module HOF callback (foreign fn/closure) throw stays catchable by class after the trace stitch.
func TestParityHofCallbackCaughtThrowCrossModule(t *testing.T) {
	cases := []struct {
		name    string
		modules map[string]string
		main    string
		want    string
	}{
		{
			name: "ForeignFunctionCaughtByClass",
			modules: map[string]string{
				"keymod": `module keymod;
class KeyError extends RuntimeError {
    func KeyError(string m) { parent(m); }
}
export func keyFn(int x): int {
    if (x == 1) { throw KeyError("foreign class boom"); }
    return x;
}
`,
			},
			main: `import io;
import keymod;
class KeyError extends RuntimeError {
    func KeyError(string m) { parent(m); }
}
try {
    let s = [3, 1, 2].sortBy(keymod.keyFn);
    io.println("${s}");
} catch (KeyError e) {
    io.println("caught KeyError: ${e.message}");
} catch (RuntimeError e) {
    io.println("caught RuntimeError: ${e.message}");
}
io.println("done");
`,
			want: "caught KeyError: foreign class boom\ndone\n",
		},
		{
			name: "ForeignClosureCaught",
			modules: map[string]string{
				"clomod": `module clomod;
export func makeKey(): func {
    return func(int x): int {
        if (x == 1) { throw RuntimeError("foreign closure boom"); }
        return x;
    };
}
`,
			},
			main: `import io;
import clomod;
try {
    let kf = clomod.makeKey();
    let s = [3, 1, 2].sortBy(kf);
    io.println("${s}");
} catch (RuntimeError e) {
    io.println("caught: ${e.message}");
}
io.println("done");
`,
			want: "caught: foreign closure boom\ndone\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParityModulesDir(t, moduleDirFor(t, tc.modules), tc.main, tc.want)
		})
	}
}

// TestParityHofReverseCallbackCaught pins the reverse direction under catch: a donor HOF invokes a main callback; the caught error keeps its message AND the structured trace includes the donor's runHof frame on both backends.
func TestParityHofReverseCallbackCaught(t *testing.T) {
	donor := `module donor;

export func runHof(func cb): any {
    let s = [3, 1, 2].sortBy(cb);
    return s;
}
`
	cases := []struct {
		name    string
		modules map[string]string
		main    string
		want    string
	}{
		{
			name:    "MainNamedFnCaughtFrames",
			modules: map[string]string{"donor": donor},
			main: `import io;
import donor;

func keyFn(int x): int {
    if (x == 1) { throw RuntimeError("boom caught rev"); }
    return x;
}

func run(): void {
    try {
        let s = donor.runHof(keyFn);
    } catch (RuntimeError e) {
        io.println("caught: ${e.message}");
        let frames = e.stackTrace().frames();
        for (f in frames) {
            io.println("frame: ${f.function()} line ${f.line()}");
        }
    }
}

run();
`,
			want: "caught: boom caught rev\nframe: keyFn line 5\nframe: runHof line 4\nframe: run line 11\nframe: <top level> line 21\n",
		},
		{
			name:    "MainCapturingClosureCaughtFrames",
			modules: map[string]string{"donor": donor},
			main: `import io;
import donor;

func run(): void {
    let tag = 9;
    try {
        let s = donor.runHof(func(int x): int {
            if (x == 1) { throw RuntimeError("boom cap caught ${tag}"); }
            return x;
        });
    } catch (RuntimeError e) {
        io.println("caught: ${e.message}");
        let frames = e.stackTrace().frames();
        for (f in frames) {
            io.println("frame: ${f.function()} line ${f.line()}");
        }
    }
}

run();
`,
			want: "caught: boom cap caught 9\nframe: <closure> line 8\nframe: runHof line 4\nframe: run line 7\nframe: <top level> line 20\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParityModulesDir(t, moduleDirFor(t, tc.modules), tc.main, tc.want)
		})
	}
}

// moduleDirFor writes modules (name -> source) to a temp dir and returns it.
func moduleDirFor(t *testing.T, modules map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range modules {
		if err := os.WriteFile(filepath.Join(dir, name+".gb"), []byte(body), 0o644); err != nil {
			t.Fatalf("write module %s: %v", name, err)
		}
	}
	return dir
}
