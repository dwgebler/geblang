package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

// 9.22: a spread argument not in last position must not drop trailing args.
func TestParitySpreadNotLastFunction(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
let rest = [10, 20];
show(1, ...rest, 4);
show(...rest, 3, 4);
`, "1 10 20 4\n10 20 3 4\n")
}

func TestParityMultipleSpreadsFunction(t *testing.T) {
	runParity(t, `import io;
func s5(int a, int b, int c, int d, int e): void { io.println("${a} ${b} ${c} ${d} ${e}"); }
s5(1, ...[10, 20], ...[30, 40]);
s5(...[10, 20], ...[30, 40], 5);
`, "1 10 20 30 40\n10 20 30 40 5\n")
}

// 9.23: a named argument before a spread binds by name, not positionally.
func TestParityNamedBeforeSpreadFunction(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c): void { io.println("${a} ${b} ${c}"); }
let rest = [10, 20];
show(c: 1, ...rest);
show(...rest, c: 1);
`, "10 20 1\n10 20 1\n")
}

func TestParityNamedBeforeSpreadMissingArgCaught(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
try {
    show(d: 4, ...[10, 20]);
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "show missing argument c\n")
}

func TestParityNamedSpreadDuplicateParamCaught(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
try {
    show(d: 4, ...[10, 20], a: 1);
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "show parameter a passed more than once\n")
}

func TestParityDictSpreadWithPositionalsAround(t *testing.T) {
	runParity(t, `import io;
func s5(int a, int b, int c, int d, int e): void { io.println("${a} ${b} ${c} ${d} ${e}"); }
s5(1, 2, 3, ...{"e": 50, "d": 40});
s5(1, 2, 3, ...{"d": 40, "e": 50, "junk": 9});
`, "1 2 3 40 50\n1 2 3 40 50\n")
}

func TestParitySpreadNotLastVariadic(t *testing.T) {
	runParity(t, `import io;
func lead(int a, int ...rest): void { io.println("${a}:${rest}"); }
lead(1, ...[2, 3], 4);
lead(...[5], 6, 7);
`, "1:[2, 3, 4]\n5:[6, 7]\n")
}

func TestParitySpreadNotLastMethod(t *testing.T) {
	runParity(t, `import io;
class Box {
    func m4(int a, int b, int c, int d): string { return "${a} ${b} ${c} ${d}"; }
}
let bx = Box();
io.println(bx.m4(1, ...[10, 20], 4));
io.println(bx.m4(d: 4, ...[10, 20, 30]));
io.println(bx.m4(c: 3, d: 4, ...[10, 20]));
`, "1 10 20 4\n10 20 30 4\n10 20 3 4\n")
}

func TestParitySpreadNotLastStaticMethod(t *testing.T) {
	runParity(t, `import io;
class Box {
    static func s4(int a, int b, int c, int d): string { return "${a} ${b} ${c} ${d}"; }
}
io.println(Box.s4(1, ...[10, 20], 4));
io.println(Box.s4(d: 4, ...[10, 20, 30]));
`, "1 10 20 4\n10 20 30 4\n")
}

func TestParitySpreadNotLastConstructor(t *testing.T) {
	runParity(t, `import io;
class Pt {
    int x; int y; int z;
    func Pt(int x, int y, int z) { this.x = x; this.y = y; this.z = z; }
}
let p = Pt(1, ...[10], 3);
io.println("${p.x} ${p.y} ${p.z}");
let q = Pt(z: 3, ...[10, 20]);
io.println("${q.x} ${q.y} ${q.z}");
`, "1 10 3\n10 20 3\n")
}

func TestParitySpreadNotLastCallableValue(t *testing.T) {
	runParity(t, `import io;
let f = func(int a, int b, int c): string { return "${a} ${b} ${c}"; };
io.println(f(1, ...[2], 3));
io.println(f(c: 9, ...[7, 8]));
class H { func adder(int a, int b, int c): int { return a + b + c; } }
let h = H();
io.println((h.adder)(1, ...[2], 3));
`, "1 2 3\n7 8 9\n6\n")
}

func TestParitySpreadNotLastNative(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.max(1, ...[5], 9));
io.println(math.max(...[5], 1, 9));
`, "9\n9\n")
}

// The evaluator rejects named args mixed with spread on natives; the VM must agree.
func TestParityNamedSpreadNativeRejected(t *testing.T) {
	runParity(t, `import io;
import math;
try {
    io.println(math.max(c: 1, ...[2, 3]));
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "named arguments are only supported for Geblang functions and methods\n")
}

func TestParityDeferSpreadNotLast(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
func work(): void {
    let rest = [10, 20];
    defer show(1, ...rest, 4);
    io.println("body");
}
work();
`, "body\n1 10 20 4\n")
}

func TestParityDeferNamedBeforeSpread(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c): void { io.println("${a} ${b} ${c}"); }
class Box {
    func m3(int a, int b, int c): void { io.println("m ${a} ${b} ${c}"); }
}
func work(): void {
    defer show(c: 1, ...[10, 20]);
    let bx = Box();
    defer bx.m3(c: 9, ...[7, 8]);
    let f = func(int a, int b, int c): void { io.println("f ${a} ${b} ${c}"); };
    defer f(c: 6, ...[4, 5]);
    io.println("body");
}
work();
`, "body\nf 4 5 6\nm 7 8 9\n10 20 1\n")
}

// Spread-not-last binds identically when the callee lives in another module.
func TestParitySpreadNotLastCrossModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(`module donor;
export func join4(int a, int b, int c, int d): string { return "${a}|${b}|${c}|${d}"; }
export class Joiner {
    int x; int y; int z;
    func Joiner(int x, int y, int z) { this.x = x; this.y = y; this.z = z; }
    func dump(): string { return "${this.x}|${this.y}|${this.z}"; }
    func m3(int a, int b, int c): string { return "${a}|${b}|${c}"; }
    static func s3(int a, int b, int c): string { return "${a}|${b}|${c}"; }
}
`), 0o644); err != nil {
		t.Fatalf("write donor: %v", err)
	}
	runParityModulesDir(t, dir, `import io;
import donor;
io.println(donor.join4(1, ...[10, 20], 4));
let j = donor.Joiner(1, ...[10], 3);
io.println(j.dump());
io.println(j.m3(1, ...[10, 20]));
io.println(j.m3(c: 9, ...[10, 20]));
io.println(donor.Joiner.s3(1, ...[10, 20]));
`, "1|10|20|4\n1|10|3\n1|10|20\n10|20|9\n1|10|20\n")
}

// Frozen-reference defer semantics: spread-list mutation visible at fire time, reassignment not.
func TestParityDeferSpreadTrailingFrozenReference(t *testing.T) {
	runParity(t, `import io;
func show(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
func mutated(): void {
    let rest = [10, 20];
    defer show(1, ...rest, 4);
    rest[0] = 77;
}
func reassigned(): void {
    let rest = [10, 20];
    defer show(1, ...rest, 4);
    rest = [88, 99];
}
mutated();
reassigned();
`, "1 77 20 4\n1 10 20 4\n")
}
