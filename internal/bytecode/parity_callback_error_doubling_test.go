package bytecode_test

import (
	"testing"
)

// TestParityCallbackErrorCaught pins catchability + class preservation when an uncaught throw crosses a native-callback / decorated-call boundary (finding 9.12: the VM stringified it into a fresh RuntimeError).
func TestParityCallbackErrorCaught(t *testing.T) {
	preamble := `import io;
func trace(any next): any {
    return func(int x): int { return next(x); };
}
func guard(any cls): any {
    return func(int x): any { throw ValueError("ctor boom"); };
}
class Svc {
    @trace
    func inst(int x): int { throw ValueError("inst boom"); }
    @trace
    static func stat(int x): int { throw ValueError("stat boom"); }
}
@guard
class Box {
    int v;
    func Box(int x) { this.v = x; }
}
@trace
func fn(int x): int { throw ValueError("fn boom"); }
let s = Svc();
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "DictSearch",
			body: `try { {"a": 1, "b": 2}.search(func(int v): bool { throw ValueError("dict boom"); }); }
catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: dict boom\n",
		},
		{
			name: "StringSearch",
			body: `try { "hi".search(func(string c): bool { throw ValueError("str boom"); }); }
catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: str boom\n",
		},
		{
			name: "DecoratedInstancePositional",
			body: `try { s.inst(1); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: inst boom\n",
		},
		{
			name: "DecoratedInstanceNamed",
			body: `try { s.inst(x: 1); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: inst boom\n",
		},
		{
			name: "DecoratedStaticDirect",
			body: `try { Svc.stat(1); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: stat boom\n",
		},
		{
			name: "DecoratedStaticAsValue",
			body: `let c = Svc;
try { c.stat(1); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: stat boom\n",
		},
		{
			name: "DecoratedFunction",
			body: `try { fn(1); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: fn boom\n",
		},
		{
			name: "DecoratedClassConstructor",
			body: `try { let b = Box(5); io.println(b); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: ctor boom\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParity(t, preamble+tc.body+"\n", tc.want)
		})
	}
}

// TestUncaughtCallbackErrorNotDoubled guards single-render + class-preservation for an uncaught callback/decorated throw; decorated forms only pin no-doubling because their frame stitching diverges (pre-existing 9.13/9.14).
func TestUncaughtCallbackErrorNotDoubled(t *testing.T) {
	t.Run("DictSearchByteIdentical", func(t *testing.T) {
		src := `import io;
let d = {"a": 1, "b": 2, "c": 3};
io.println(d.search(func(int v): bool { throw ValueError("boom in callback"); }));
`
		want := `uncaught ValueError: boom in callback
  at <closure> (line 3)
  at <top level> (line 3)`
		evGot, vmGot := uncaughtOnBothBackends(t, src)
		if evGot != vmGot {
			t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
		}
		if evGot != want {
			t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, want)
		}
	})
	t.Run("StringSearchByteIdentical", func(t *testing.T) {
		src := `import io;
let s = "hello";
io.println(s.search(func(string ch): bool { throw ValueError("boom in callback"); }));
`
		want := `uncaught ValueError: boom in callback
  at <closure> (line 3)
  at <top level> (line 3)`
		evGot, vmGot := uncaughtOnBothBackends(t, src)
		if evGot != vmGot {
			t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
		}
		if evGot != want {
			t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, want)
		}
	})

	// Uncaught decorated forms are byte-identical across backends (9.37): wrapper frame named after the decorated function, top-level line retained.
	decorated := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "DecoratedFunction",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
@trace
func doit(int x): int { throw ValueError("boom in func"); }
io.println(doit(5));
`,
			want: `uncaught ValueError: boom in func
  at doit (line 4)
  at doit (line 2)
  at <top level> (line 5)`,
		},
		{
			name: "DecoratedInstanceMethod",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    func handle(int x): int { throw ValueError("boom in method"); }
}
io.println(Svc().handle(5));
`,
			want: `uncaught ValueError: boom in method
  at handle (line 5)
  at Svc.handle (line 2)
  at <top level> (line 7)`,
		},
		{
			name: "DecoratedInstanceMethodNamed",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    func handle(int x): int { throw ValueError("boom in method"); }
}
io.println(Svc().handle(x: 5));
`,
			want: `uncaught ValueError: boom in method
  at handle (line 5)
  at Svc.handle (line 2)
  at <top level> (line 7)`,
		},
		{
			name: "DecoratedStaticMethod",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    static func handle(int x): int { throw ValueError("boom in static"); }
}
io.println(Svc.handle(5));
`,
			want: `uncaught ValueError: boom in static
  at handle (line 5)
  at Svc.handle (line 2)
  at <top level> (line 7)`,
		},
	}
	for _, tc := range decorated {
		t.Run(tc.name, func(t *testing.T) {
			evGot, vmGot := uncaughtOnBothBackends(t, tc.src)
			if evGot != vmGot {
				t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
			}
			if evGot != tc.want {
				t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, tc.want)
			}
		})
	}
}
