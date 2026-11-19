package bytecode_test

import "testing"

func TestGeneratorRuntimeFaultMessageParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import collections;

class Box {
    func Box() {}
    func each(int x): generator<decimal> { yield 10 / x; }
}
func named(int x): generator<decimal> { yield 1; yield 10 / x; }
let closure = func(int x): generator<decimal> { yield 10 / x; };
func relay(int x): generator<decimal> { for (v in named(x)) { yield v; } }

func show(string label, func body) {
    try { body(); io.println(label + ": ok"); } catch (Error e) { io.println(label + ": [" + e.class + "] " + e.message); }
}
show("forIn", () => { for (v in named(0)) {} });
show("method", () => { for (v in Box().each(0)) {} });
show("closure", () => { for (v in closure(0)) {} });
show("relay", () => { for (v in relay(0)) {} });
show("next", () => { let g = named(0); g.next(); g.next(); });
show("done", () => { let g = closure(0); g.done(); });
show("lazyMap", () => { for (v in collections.lazyMap(named(0), (any v) => v)) {} });
show("comprehension", () => { any l = [v for v in named(0)]; });
`)
	want := "forIn: [RuntimeError] decimal division by zero\n" +
		"method: [RuntimeError] decimal division by zero\n" +
		"closure: [RuntimeError] decimal division by zero\n" +
		"relay: [RuntimeError] decimal division by zero\n" +
		"next: [RuntimeError] decimal division by zero\n" +
		"done: [RuntimeError] decimal division by zero\n" +
		"lazyMap: [RuntimeError] decimal division by zero\n" +
		"comprehension: [RuntimeError] decimal division by zero\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
