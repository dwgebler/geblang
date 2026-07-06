package bytecode_test

import (
	"testing"
)

// TestParityHofCallbackTraceFrames pins byte-identical traces when a native-invoked callback throws: the caller frame's line is the HOF call site, not line 0 (VM) or a stale callback line (evaluator).
func TestParityHofCallbackTraceFrames(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "SortByKeyFn",
			source: `import io;
func keyFn(int x): int {
    if (x == 1) { throw RuntimeError("boom in keyFn"); }
    return x;
}
func outer(): void {
    let s = [3, 1, 2].sortBy(keyFn);
    io.println("${s}");
}
outer();
`,
			want: `uncaught RuntimeError: boom in keyFn
  at keyFn (line 3)
  at outer (line 7)
  at <top level> (line 10)`,
		},
		{
			name: "MapCallback",
			source: `import io;
func mapper(int x): int {
    if (x == 2) { throw RuntimeError("boom in mapper"); }
    return x * 2;
}
func outer(): void {
    let m = [1, 2, 3].map(mapper);
    io.println("${m}");
}
outer();
`,
			want: `uncaught RuntimeError: boom in mapper
  at mapper (line 3)
  at outer (line 7)
  at <top level> (line 10)`,
		},
		{
			name: "FilterCallback",
			source: `import io;
func keep(int x): bool {
    if (x == 2) { throw RuntimeError("boom in keep"); }
    return x > 0;
}
func outer(): void {
    let f = [1, 2, 3].filter(keep);
    io.println("${f}");
}
outer();
`,
			want: `uncaught RuntimeError: boom in keep
  at keep (line 3)
  at outer (line 7)
  at <top level> (line 10)`,
		},
		{
			name: "ReduceCallback",
			source: `import io;
func combine(int acc, int x): int {
    if (x == 2) { throw RuntimeError("boom in reduce"); }
    return acc + x;
}
func outer(): void {
    let r = [1, 2, 3].reduce(combine, 0);
    io.println("${r}");
}
outer();
`,
			want: `uncaught RuntimeError: boom in reduce
  at combine (line 3)
  at outer (line 7)
  at <top level> (line 10)`,
		},
		{
			// A HOF inside a HOF callback: each caller frame's line is its own HOF call site.
			name: "NestedHof",
			source: `import io;
func inner(int x): int {
    throw RuntimeError("boom nested");
}
func mid(int x): int {
    let a = [10, 20].map(inner);
    return x;
}
func outer(): void {
    let m = [1, 2, 3].map(mid);
    io.println("${m}");
}
outer();
`,
			want: `uncaught RuntimeError: boom nested
  at inner (line 3)
  at mid (line 6)
  at outer (line 10)
  at <top level> (line 13)`,
		},
		{
			// Module-form HOF (collections.map) carries the native call site to the callback frame like the method form.
			name: "ModuleFormMap",
			source: `import io;
import collections;
func kf(int x): int {
    if (x == 1) { throw RuntimeError("boom module"); }
    return x;
}
func outer(): void {
    let s = collections.map([3, 1, 2], kf);
    io.println("${s}");
}
outer();
`,
			want: `uncaught RuntimeError: boom module
  at kf (line 4)
  at outer (line 8)
  at <top level> (line 11)`,
		},
		{
			// A no-upvalue closure takes the same inline path as a named callback.
			name: "NoUpvalueClosure",
			source: `import io;
func outer(): void {
    let m = [1, 2, 3].map(func(int x): int {
        if (x == 2) { throw RuntimeError("boom closure"); }
        return x;
    });
    io.println("${m}");
}
outer();
`,
			want: `uncaught RuntimeError: boom closure
  at <closure> (line 4)
  at outer (line 3)
  at <top level> (line 9)`,
		},
		{
			// A capturing same-module closure runs the callback on a separate wrapper VM; the host caller chain must be stitched back on.
			name: "CapturingClosureSeparateVM",
			source: `import io;
func level1(): void {
    let tag = 7;
    let s = [3, 1, 2].sortBy(func(int x): int {
        if (x == 1) { throw RuntimeError("boom capturing ${tag}"); }
        return x;
    });
    io.println("${s}");
}
func level2(): void {
    level1();
}
func outer(): void {
    level2();
}
outer();
`,
			want: `uncaught RuntimeError: boom capturing 7
  at <closure> (line 5)
  at level1 (line 4)
  at level2 (line 11)
  at outer (line 14)
  at <top level> (line 16)`,
		},
		{
			// A callable instance (__invoke) runs the callback on a separate wrapper VM.
			name: "CallableInstanceSeparateVM",
			source: `import io;
class Keyer {
    func __invoke(int x): int {
        if (x == 1) { throw RuntimeError("boom invoke"); }
        return x;
    }
}
func level1(): void {
    let k = Keyer();
    let s = [3, 1, 2].sortBy(k);
    io.println("${s}");
}
func level2(): void {
    level1();
}
func outer(): void {
    level2();
}
outer();
`,
			want: `uncaught RuntimeError: boom invoke
  at Keyer.__invoke (line 4)
  at level1 (line 10)
  at level2 (line 14)
  at outer (line 17)
  at <top level> (line 19)`,
		},
		{
			// A HOF inside a separate-VM callback: the baseline composes so both callback frames and the full host chain survive.
			name: "NestedHofSeparateVM",
			source: `import io;
func level1(): void {
    let tag = 3;
    let s = [3, 1, 2].sortBy(func(int x): int {
        let inner = [10, 20].sortBy(func(int y): int {
            if (tag == 3) { throw RuntimeError("boom nested"); }
            return y;
        });
        return x;
    });
    io.println("${s}");
}
func level2(): void {
    level1();
}
func outer(): void {
    level2();
}
outer();
`,
			want: `uncaught RuntimeError: boom nested
  at <closure> (line 6)
  at <closure> (line 5)
  at level1 (line 4)
  at level2 (line 14)
  at outer (line 17)
  at <top level> (line 19)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evGot, vmGot := uncaughtOnBothBackends(t, tc.source)
			if evGot != vmGot {
				t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
			}
			if evGot != tc.want {
				t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, tc.want)
			}
		})
	}
}

// TestParityHofCallbackTraceCrossModule pins the two cross-module separate-VM shapes (foreign function, foreign closure) whose callbacks run on a donor worker VM; the host caller chain must be stitched back on.
func TestParityHofCallbackTraceCrossModule(t *testing.T) {
	cases := []struct {
		name    string
		modules map[string]string
		main    string
		want    string
	}{
		{
			name: "ForeignFunction",
			modules: map[string]string{
				"keymod": `module keymod;
export func keyFn(int x): int {
    if (x == 1) { throw RuntimeError("boom foreign fn"); }
    return x;
}
`,
			},
			main: `import io;
import keymod;
func level1(): void {
    let s = [3, 1, 2].sortBy(keymod.keyFn);
    io.println("${s}");
}
func level2(): void {
    level1();
}
func outer(): void {
    level2();
}
outer();
`,
			want: `uncaught RuntimeError: boom foreign fn
  at keyFn (line 3)
  at level1 (line 4)
  at level2 (line 8)
  at outer (line 11)
  at <top level> (line 13)`,
		},
		{
			name: "ForeignClosure",
			modules: map[string]string{
				"clomod": `module clomod;
export func makeKey(): func {
    let bump = 5;
    return func(int x): int {
        if (x == 1) { throw RuntimeError("boom foreign closure"); }
        return x + bump;
    };
}
`,
			},
			main: `import io;
import clomod;
func level1(): void {
    let kf = clomod.makeKey();
    let s = [3, 1, 2].sortBy(kf);
    io.println("${s}");
}
func level2(): void {
    level1();
}
func outer(): void {
    level2();
}
outer();
`,
			want: `uncaught RuntimeError: boom foreign closure
  at <closure> (line 5)
  at level1 (line 5)
  at level2 (line 9)
  at outer (line 12)
  at <top level> (line 14)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, tc.modules, tc.main)
			if evGot != vmGot {
				t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
			}
			if evGot != tc.want {
				t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, tc.want)
			}
		})
	}
}

// TestParityHofReverseCallbackTrace pins the reverse direction: a DONOR module's HOF (runHof runs sortBy) invokes a MAIN callback bridged back to the host vm; the donor's own runHof frame must survive on both backends.
func TestParityHofReverseCallbackTrace(t *testing.T) {
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
			name:    "MainNamedFn",
			modules: map[string]string{"donor": donor},
			main: `import io;
import donor;

func keyFn(int x): int {
    if (x == 1) { throw RuntimeError("boom from main keyFn"); }
    return x;
}

func level2(): void {
    let s = donor.runHof(keyFn);
    io.println("${s}");
}

func outer(): void {
    level2();
}

outer();
`,
			want: `uncaught RuntimeError: boom from main keyFn
  at keyFn (line 5)
  at runHof (line 4)
  at level2 (line 10)
  at outer (line 15)
  at <top level> (line 18)`,
		},
		{
			name:    "MainCapturingClosure",
			modules: map[string]string{"donor": donor},
			main: `import io;
import donor;

func level2(): void {
    let tag = 9;
    let s = donor.runHof(func(int x): int {
        if (x == 1) { throw RuntimeError("boom capturing ${tag}"); }
        return x;
    });
    io.println("${s}");
}

func outer(): void {
    level2();
}

outer();
`,
			want: `uncaught RuntimeError: boom capturing 9
  at <closure> (line 7)
  at runHof (line 4)
  at level2 (line 6)
  at outer (line 14)
  at <top level> (line 17)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, tc.modules, tc.main)
			if evGot != vmGot {
				t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
			}
			if evGot != tc.want {
				t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, tc.want)
			}
		})
	}
}
