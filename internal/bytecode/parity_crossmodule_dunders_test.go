package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const dunderDonorGb = `module vecdonor;
export class V {
    int n;
    list<int> items = [];
    func V(int n) { this.n = n; this.items = [n, n + 1]; }
    func __add(V o): V { return V(this.n + o.n); }
    func __mul(int k): V { return V(this.n * k); }
    func __lt(V o): bool { return this.n < o.n; }
    func __eq(V o): bool { return this.n == o.n; }
    func __neg(): V { return V(-this.n); }
    func __not(): bool { return this.n == 0; }
    func __bitand(V o): V { return V(this.n & o.n); }
    func __bitnot(): V { return V(~this.n); }
    func __index(int i): int { return this.items[i]; }
    func __setIndex(int i, int v): void { this.items[i] = v; }
    func __contains(int x): bool { return this.items.contains(x); }
    func __string(): string { return "V(${this.n})"; }
    func __int(): int { return this.n; }
    func __bool(): bool { return this.n != 0; }
}
`

const dunderMain = `import io;
import vecdonor as m;
class W extends m.V {
    func W(int n) { parent(n); }
}
func report(any a, any b): void {
    io.println("${a + b} ${a * 3} ${-a} ${!a} ${a & b} ${~a}");
    io.println("${a < b} ${a > b} ${a <= b} ${a >= b} ${b <= a} ${b >= a}");
    io.println("${a == m.V(2)} ${a != b} ${[a] == [m.V(2)]}");
    a[0] = 7;
    io.println("${a[0]} ${a[1]} ${3 in a} ${9 in a}");
    io.println("${a} ${a as int} ${a as bool}");
}
report(m.V(2), m.V(5));
report(W(2), W(5));
`

const dunderWant = "V(7) V(6) V(-2) false V(0) V(-3)\n" +
	"true false true false false true\n" +
	"true true true\n" +
	"7 3 true false\n" +
	"V(2) 2 true\n"

func TestParityCrossModuleOperatorDunders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vecdonor.gb"), []byte(dunderDonorGb), 0o644); err != nil {
		t.Fatalf("write donor: %v", err)
	}
	runParityModulesDir(t, dir, dunderMain, dunderWant+dunderWant)
}
