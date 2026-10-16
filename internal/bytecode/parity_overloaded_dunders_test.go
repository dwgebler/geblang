package bytecode_test

import "testing"

func TestParityOverloadedDundersDispatchByArgumentType(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class V {
    int n;
    func V(int n) { this.n = n; }
    func __mul(int k): string { return "int"; }
    func __mul(decimal k): string { return "decimal"; }
    func __mul(V o): string { return "V"; }
    func __lt(int k): bool { return this.n < k; }
    func __lt(V o): bool { return this.n < o.n; }
    func __eq(int k): bool { return this.n == k; }
    func __eq(V o): bool { return this.n == o.n; }
    func __index(int i): string { return "i${i}"; }
    func __index(string k): string { return "k${k}"; }
    func __contains(int x): bool { return x == this.n; }
    func __contains(string s): bool { return s == "${this.n}"; }
}
class W extends V { func W(int n) { parent(n); } }
for (v in [V(2), W(2)]) {
    io.println("${v * 2} ${v * 1.5} ${v * V(3)}");
    io.println("${v < 3} ${v < V(1)} ${v > V(1)}");
    io.println("${v == 2} ${v == V(2)} ${v != V(5)}");
    io.println("${v[1]} ${v["a"]} ${2 in v} ${"2" in v}");
}
`)
	block := "int decimal V\ntrue false true\ntrue true true\ni1 ka true true\n"
	want := block + block
	if ev != want || vm != want {
		t.Errorf("overloaded dunders:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
