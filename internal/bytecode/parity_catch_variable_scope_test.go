package bytecode_test

import "testing"

// The VM compiled a catch body in the catch variable's own scope, so redeclaring that variable in the body was rejected as a duplicate.
func TestParityCatchBodyMayRedeclareCatchVariable(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Resource { func __enter(): Resource { return this; } func __exit(): void {} }
class Base {
    int seen;
    func Base() {
        try { throw ValueError("ctor"); } catch (Error a) { let a = 4; this.seen = a; }
    }
    func run(): int {
        int t = 0;
        try { throw ValueError("x"); } catch (Error a) { let a = 2; t += a; let g = func(): int { return a; }; t += g(); }
        try { throw ValueError("y"); } catch (ValueError a) { int a = 5; t += a; } catch (Error a) { t = -1; } finally { let a = 1; t += a; }
        try { try { throw ValueError("z"); } catch (Error a) { let a = 3; throw RuntimeError("again"); } } catch (Error a) { t += 10; }
        with (o = Resource()) { let o = 100; t += o; }
        return t;
    }
}
class Sub extends Base { func Sub() { parent(); } }
func message(): string {
    try { throw ValueError("kept"); } catch (Error a) { string text = a.message; let a = "shadow"; return text + ":" + a; }
    return "";
}
io.println(Base().run());
io.println(Sub().run());
io.println(Sub().seen);
io.println(message());
`)
	want := "120\n120\n4\nkept:shadow\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
