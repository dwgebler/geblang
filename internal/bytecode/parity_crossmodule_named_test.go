package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

// 9.27: named args to a cross-module free function bind against the callee's home-module parameters on both backends.
const namedDonorGb = `module donor;
import io;
export func show4(int a, int b, int c, int d): void { io.println("${a} ${b} ${c} ${d}"); }
export func show3(int a, int b, int c): void { io.println("${a} ${b} ${c}"); }
export func withDefault(int a, int b, int c = 99): void { io.println("${a} ${b} ${c}"); }
`

func writeNamedDonor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(namedDonorGb), 0o644); err != nil {
		t.Fatalf("write donor: %v", err)
	}
	return dir
}

func TestParityCrossModuleNamedPlain(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import donor;
donor.show4(d: 4, a: 1, b: 2, c: 3);
`, "1 2 3 4\n")
}

func TestParityCrossModuleNamedPositionalMix(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import donor;
donor.show4(1, 2, d: 4, c: 3);
`, "1 2 3 4\n")
}

func TestParityCrossModuleNamedDefaultOmitted(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import donor;
donor.withDefault(b: 2, a: 1);
`, "1 2 99\n")
}

func TestParityCrossModuleNamedSpread(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import donor;
let rest = [10, 20];
donor.show3(c: 1, ...rest);
donor.show3(...rest, c: 1);
`, "10 20 1\n10 20 1\n")
}

func TestParityCrossModuleNamedDictSpread(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import donor;
donor.show4(1, 2, ...{"d": 40, "c": 30});
donor.show4(a: 1, ...{"d": 40, "c": 30, "b": 20});
donor.show4(1, 2, ...{"d": 40, "c": 30, "junk": 9});
`, "1 2 30 40\n1 20 30 40\n1 2 30 40\n")
}

func TestParityCrossModuleNamedFromImport(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `from donor import show4;
show4(d: 4, a: 1, b: 2, c: 3);
`, "1 2 3 4\n")
}

func TestParityCrossModuleNamedInDefer(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import io;
import donor;
func run(): void {
    defer donor.show4(d: 4, a: 1, b: 2, c: 3);
    io.println("body");
}
run();
`, "body\n1 2 3 4\n")
}

func TestParityCrossModuleNamedUnknownParamCaught(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import io;
import donor;
try {
    donor.show4(z: 4, a: 1, b: 2, c: 3);
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "show4 has no parameter z\n")
}

func TestParityCrossModuleNamedDuplicateParamCaught(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import io;
import donor;
try {
    donor.show4(1, a: 1, b: 2, c: 3);
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "show4 parameter a passed more than once\n")
}

func TestParityCrossModuleNamedMissingRequiredCaught(t *testing.T) {
	dir := writeNamedDonor(t)
	runParityModulesDir(t, dir, `import io;
import donor;
try {
    donor.show4(a: 1, b: 2);
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "show4 missing argument c\n")
}

// 9.29: named args to a cross-module class constructor select the overload and bind against the constructor's home-module parameters on both backends.
const namedCtorBaseGb = `module base;
export class Base {
    int bx;
    int bz;
    func Base(int bx, int bz = 100) { this.bx = bx; this.bz = bz; }
    func describe(): string { return "Base(${this.bx}, ${this.bz})"; }
}
`

const namedCtorDonorGb = `module helpers;
import base;
export class Point {
    int x;
    int y;
    func Point(int x, int y = 10) { this.x = x; this.y = y; }
    func show(): string { return "Point(${this.x}, ${this.y})"; }
}
export class Box {
    int a;
    string label;
    int width;
    func Box(int a, string b) { this.a = a; this.label = b; this.width = 0; }
    func Box(int width) { this.a = 0; this.label = "w"; this.width = width; }
    func show(): string { return "Box(${this.a}, ${this.label}, ${this.width})"; }
}
export class Sub extends base.Base {
    int extra;
    func Sub(int bx, int extra) { parent(bx); this.extra = extra; }
    func showsub(): string { return "Sub(${this.bx}, ${this.bz}, ${this.extra})"; }
}
export class Zero extends base.Base {
    func showzero(): string { return "Zero(${this.bx}, ${this.bz})"; }
}
`

func writeNamedCtorDonor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(namedCtorBaseGb), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helpers.gb"), []byte(namedCtorDonorGb), 0o644); err != nil {
		t.Fatalf("write helpers: %v", err)
	}
	return dir
}

func TestParityCrossModuleNamedCtorPlain(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Point(x: 1, y: 2).show());
`, "Point(1, 2)\n")
}

func TestParityCrossModuleNamedCtorOutOfOrder(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Point(y: 2, x: 1).show());
`, "Point(1, 2)\n")
}

func TestParityCrossModuleNamedCtorPositionalMix(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Point(1, y: 2).show());
`, "Point(1, 2)\n")
}

func TestParityCrossModuleNamedCtorDefaultOmitted(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Point(x: 5).show());
`, "Point(5, 10)\n")
}

func TestParityCrossModuleNamedCtorFromImport(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
from helpers import Point;
io.println(Point(y: 2, x: 1).show());
`, "Point(1, 2)\n")
}

func TestParityCrossModuleNamedCtorAliasedImport(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers as h;
io.println(h.Point(y: 2, x: 1).show());
`, "Point(1, 2)\n")
}

func TestParityCrossModuleNamedCtorOverloadSelection(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Box(a: 5, b: "hi").show());
io.println(helpers.Box(b: "hi", a: 5).show());
io.println(helpers.Box(width: 3).show());
`, "Box(5, hi, 0)\nBox(5, hi, 0)\nBox(0, w, 3)\n")
}

func TestParityCrossModuleNamedCtorForeignParent(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Sub(bx: 7, extra: 9).showsub());
io.println(helpers.Sub(extra: 9, bx: 7).showsub());
`, "Sub(7, 100, 9)\nSub(7, 100, 9)\n")
}

func TestParityCrossModuleNamedCtorInDefer(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
func run(): void {
    defer io.println(helpers.Point(y: 2, x: 1).show());
    io.println("body");
}
run();
`, "body\nPoint(1, 2)\n")
}

func TestParityCrossModuleNamedCtorDictSpread(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
io.println(helpers.Point(...{"x": 1, "y": 2}).show());
io.println(helpers.Point(x: 1, ...{"y": 2}).show());
io.println(helpers.Point(...{"x": 1, "y": 2, "junk": 9}).show());
`, "Point(1, 2)\nPoint(1, 2)\nPoint(1, 2)\n")
}

func TestParityCrossModuleNamedCtorUnknownParamCaught(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
try {
    io.println(helpers.Point(z: 9).show());
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "no matching overload for Point\n")
}

func TestParityCrossModuleNamedCtorDuplicateParamCaught(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
try {
    io.println(helpers.Point(x: 1, x: 2).show());
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "no matching overload for Point\n")
}

func TestParityCrossModuleNamedCtorMissingRequiredCaught(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
try {
    io.println(helpers.Point(y: 2).show());
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "no matching overload for Point\n")
}

func TestParityCrossModuleNamedCtorTypeMismatchCaught(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
try {
    io.println(helpers.Point(x: "no", y: 2).show());
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "no matching overload for Point\n")
}

func TestParityCrossModuleNamedCtorNoConstructorCaught(t *testing.T) {
	dir := writeNamedCtorDonor(t)
	runParityModulesDir(t, dir, `import io;
import helpers;
try {
    io.println(helpers.Zero(bx: 7).showzero());
} catch (RuntimeError e) {
    io.println(e.getMessage());
}
`, "Zero constructor expects no arguments\n")
}
