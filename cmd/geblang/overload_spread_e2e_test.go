package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const osDonorGb = `module ose.donor;
export func area(int side): string { return "square ${side}"; }
export func area(int w, int h): string { return "rect ${w}x${h}"; }
`

const osMainGb = `module ose.main;
import io;
import ose.donor as donor;
func pick(int a): string { return "one ${a}"; }
func pick(int a, int b): string { return "two ${a} ${b}"; }
class Box {
    func Box(int a) { io.println("box ${a}"); }
    func Box(int a, int b) { io.println("box ${a} ${b}"); }
}
export func main(list<string> args): void {
    defer Box(...{"a": 9, "unknown": 0});
    string typed = pick(...[1, 2]);
    io.println(typed);
    io.println(pick(...{"a": 3}));
    io.println(donor.area(...[4, 5]));
    io.println(donor.area(...{"side": 6}));
}
`

const osExpected = "two 1 2\none 3\nrect 4x5\nsquare 6\nbox 9\n"

// Spreads into overloads and deferred constructions agree on every runtime path, including a built binary.
func TestOverloadSpreadAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, src := range map[string]string{
		filepath.Join(pkgDir, "geblang.yaml"): "name: ose\nversion: 0.1.0\nsource: src\n",
		filepath.Join(srcDir, "donor.gb"):     osDonorGb,
		filepath.Join(srcDir, "main.gb"):      osMainGb,
	} {
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(srcDir, "main.gb")
	for _, args := range [][]string{{entry}, {entry}, {"--disable-vm", entry}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != osExpected {
			t.Fatalf("%v: err=%v got %q want %q", args, err, out, osExpected)
		}
	}
	outBin := filepath.Join(pkgDir, "ose")
	if out, err := exec.Command(bin, "build", "--entry", "ose.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(outBin).CombinedOutput()
	if err != nil || string(out) != osExpected {
		t.Fatalf("built binary: err=%v got %q want %q", err, out, osExpected)
	}
}
