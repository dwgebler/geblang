package bytecode_test

import (
	"testing"
)

// TestParityDecoratedMethodValue pins a bound value of a decorated instance / static method (9.39): the decorator wrapper runs with the receiver forwarded in every dispatch context.
func TestParityDecoratedMethodValue(t *testing.T) {
	preamble := `import io;
func logged(any next): any {
    return func(int x): int { io.println("W"); return next(x); };
}
class Svc {
    int base;
    func Svc(int base) { this.base = base; }
    @logged
    func handle(int x): int { return x + this.base; }
    @logged
    static func shandle(int x): int { return x * 3; }
}
func take(any f): int { return f(5); }
let s = Svc(100);
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "Call", body: `let f = s.handle; io.println(f(5));`, want: "W\n105\n"},
		{name: "Store", body: `let xs = [s.handle]; io.println(xs[0](7));`, want: "W\n107\n"},
		{name: "Return", body: `func getf(): any { return s.handle; } let f = getf(); io.println(f(9));`, want: "W\n109\n"},
		{name: "Pass", body: `io.println(take(s.handle));`, want: "W\n105\n"},
		{name: "ReceiverMutation", body: `let f = s.handle; io.println(f(1)); s.base = 200; io.println(f(1));`, want: "W\n101\nW\n201\n"},
		{name: "StaticValue", body: `let g = Svc.shandle; io.println(g(4));`, want: "W\n12\n"},
		{name: "HofCallback", body: `let xs = [3, 1, 2]; io.println("${xs.sortBy(s.handle)}");`, want: "W\nW\nW\n[1, 2, 3]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParity(t, preamble+tc.body+"\n", tc.want)
		})
	}
}

// TestParityDecoratedMethodValueOverloadPin pins that a decorated method value freezes the first overload at bind time, like a plain one.
func TestParityDecoratedMethodValueOverloadPin(t *testing.T) {
	src := `import io;
class Svc {
    func m(int x): string { return "int:${x}"; }
    func m(string s): string { return "str:${s}"; }
}
let f = Svc().m;
io.println(f(7));
`
	runParity(t, src, "int:7\n")
}

// TestParityDecoratedMethodValueThrow pins a throwing decorated method / static value: the real class surfaces and the trace follows the decorated-frame rules.
func TestParityDecoratedMethodValueThrow(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "BoundInstanceValue",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    func handle(int x): int { throw ValueError("boom in method"); }
}
let s = Svc();
let f = s.handle;
io.println(f(5));
`,
			want: `uncaught ValueError: boom in method
  at handle (line 5)
  at Svc.handle (line 2)
  at <top level> (line 9)`,
		},
		{
			name: "StaticValue",
			src: `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    static func handle(int x): int { throw ValueError("boom in static"); }
}
let g = Svc.handle;
io.println(g(5));
`,
			want: `uncaught ValueError: boom in static
  at handle (line 5)
  at Svc.handle (line 2)
  at <top level> (line 8)`,
		},
	}
	for _, tc := range cases {
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

// TestParityDecoratedMethodValueCaught pins catchability + class preservation for a throwing decorated method / static value.
func TestParityDecoratedMethodValueCaught(t *testing.T) {
	preamble := `import io;
func trace(any next): any { return func(int x): int { return next(x); }; }
class Svc {
    @trace
    func handle(int x): int { throw ValueError("inst boom"); }
    @trace
    static func shandle(int x): int { throw ValueError("static boom"); }
}
let s = Svc();
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "BoundInstanceValue",
			body: `let f = s.handle;
try { f(5); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: inst boom\n",
		},
		{
			name: "StaticValue",
			body: `let g = Svc.shandle;
try { g(5); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`,
			want: "ValueError: static boom\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParity(t, preamble+tc.body+"\n", tc.want)
		})
	}
}

// TestParityDecoratedMethodValueCrossModule pins a bound value of a decorated method whose class lives in another module: the decorator runs on the home worker with the receiver bound.
func TestParityDecoratedMethodValueCrossModule(t *testing.T) {
	donor := `module svcdonor;
import io;
func logged(any next): any {
    return func(int x): int { io.println("W"); return next(x); };
}
export class Svc {
    int base;
    func Svc(int base) { this.base = base; }
    @logged
    func handle(int x): int { return x + this.base; }
    @logged
    func kaboom(int x): int { throw ValueError("xm boom"); }
}
`
	t.Run("Value", func(t *testing.T) {
		dir := t.TempDir()
		writeModule(t, dir, "svcdonor", donor)
		runParityModulesDir(t, dir, `import io;
import svcdonor;
let s = svcdonor.Svc(100);
let f = s.handle;
io.println(f(5));
`, "W\n105\n")
	})
	t.Run("Throw", func(t *testing.T) {
		main := `import io;
import svcdonor;
let s = svcdonor.Svc(100);
let f = s.kaboom;
io.println(f(5));
`
		want := `uncaught ValueError: xm boom
  at kaboom (line 12)
  at Svc.kaboom (line 4)
  at <top level> (line 5)`
		evGot, vmGot := uncaughtOnBothBackendsModulesDir(t, map[string]string{"svcdonor": donor}, main)
		if evGot != vmGot {
			t.Fatalf("backend divergence:\n--- evaluator ---\n%s\n--- vm ---\n%s", evGot, vmGot)
		}
		if evGot != want {
			t.Fatalf("canonical mismatch:\n--- got ---\n%s\n--- want ---\n%s", evGot, want)
		}
	})
}

// TestParityDecoratedClassConstructorTrace pins a class-constructor decorator's uncaught throw (9.40): the wrapper frame stays <closure> and the <top level> line survives on the VM.
func TestParityDecoratedClassConstructorTrace(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "WithConstructor",
			src: `import io;
func guard(any cls): any { return func(any... a): any { throw ValueError("blocked"); }; }
@guard
class Box {
    int v;
    func Box(int v) { this.v = v; }
}
let b = Box(5);
io.println(b);
`,
			want: `uncaught ValueError: blocked
  at <closure> (line 2)
  at <top level> (line 8)`,
		},
		{
			name: "NoConstructor",
			src: `import io;
func guard(any cls): any { return func(any... a): any { throw ValueError("blocked"); }; }
@guard
class Box {
    int v;
}
let b = Box();
io.println(b);
`,
			want: `uncaught ValueError: blocked
  at <closure> (line 2)
  at <top level> (line 7)`,
		},
	}
	for _, tc := range cases {
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

// TestParityDecoratedClassConstructorCaught pins catchability for a class-decorator throw with and without a constructor.
func TestParityDecoratedClassConstructorCaught(t *testing.T) {
	preamble := `import io;
func guard(any cls): any { return func(any... a): any { throw ValueError("blocked"); }; }
@guard
class Box {
    int v;
    func Box(int v) { this.v = v; }
}
@guard
class Bare {
    int v;
}
`
	cases := []struct {
		name string
		body string
	}{
		{name: "WithConstructor", body: `try { let b = Box(5); io.println(b); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`},
		{name: "NoConstructor", body: `try { let b = Bare(); io.println(b); } catch (ValueError e) { io.println("${e.class}: ${e.message}"); }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParity(t, preamble+tc.body+"\n", "ValueError: blocked\n")
		})
	}
}
