package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const cmrBaseGb = "module base;\n" +
	"export class Base {\n" +
	"    func describe(): string { return \"from-base\"; }\n" +
	"}\n"

const cmrMainGb = "import base;\n" +
	"import io;\n" +
	"class Sub extends base.Base {}\n" +
	"io.println(Sub().describe());\n"

const cmrExpected = "from-base\n"

// An entry-file class instantiated + invoked reflectively from an imported module must resolve entry-file globals on both backends.
func TestEntryClassReflectDispatchResolvesEntryGlobals(t *testing.T) {
	bin := buildCMBinary(t)
	run := func(args ...string) string {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "di.gb"),
			[]byte("module di;\nimport reflect;\n"+
				"export func makeAndCall(any classRef): string {\n"+
				"    let instance = classRef();\n"+
				"    let handler = reflect.method(instance, \"label\");\n"+
				"    return handler();\n"+
				"}\n"), 0644)
		os.WriteFile(filepath.Join(dir, "main.gb"), []byte(
			"import io;\nimport di;\nimport reflect;\n"+
				"let salutation = \"TAG\";\n"+
				"class Widget { func Widget() {} func label(): string { return salutation; } }\n"+
				"io.println(di.makeAndCall(reflect.class(Widget())));\n"), 0644)
		cmd := exec.Command(bin, append(args, "main.gb")...)
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	vmOut := run("--vm-strict")
	evalOut := run("--disable-vm")
	if vmOut != evalOut {
		t.Fatalf("entry-class globals diverge across backends:\n  vm:   %q\n  eval: %q", vmOut, evalOut)
	}
	if vmOut != "TAG\n" {
		t.Fatalf("expected %q, got %q", "TAG\n", vmOut)
	}
}

// TestCrossModuleCacheHitIdenticalOutput runs main.gb twice from the same dir,
// ensuring the .gbc cache-hit path produces identical output to the cold run.
func TestCrossModuleCacheHitIdenticalOutput(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "base.gb"), []byte(cmrBaseGb), 0644)
	os.WriteFile(filepath.Join(dir, "main.gb"), []byte(cmrMainGb), 0644)

	run := func(label string) string {
		cmd := exec.Command(bin, "main.gb")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: run failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	coldOut := run("cold")
	if coldOut != cmrExpected {
		t.Fatalf("cold run: expected %q, got %q", cmrExpected, coldOut)
	}

	cacheDir := filepath.Join(dir, ".geblang-cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Fatalf("expected .geblang-cache to exist after cold run")
	}

	warmOut := run("warm")
	if warmOut != coldOut {
		t.Fatalf("cache-hit run output differs from cold run:\ncold: %q\nwarm: %q", coldOut, warmOut)
	}
}

func TestPrimitiveMethodCaseSensitivityAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: methodcase\nversion: 0.1.0\nsource: src\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "probe.gb"), []byte(`module methodcase.probe;

export func result(): string {
    any text = "42";
    any values = [];
    string out = "";
    try { text.Length(); out = out + "accepted,"; }
    catch (Error e) { out = out + "rejected,"; }
    try { values.isempty(); out = out + "accepted,"; }
    catch (Error e) { out = out + "rejected,"; }
    try { text.TOINT(); out = out + "accepted"; }
    catch (Error e) { out = out + "rejected"; }
    return out + ":${text.toInt()}";
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module methodcase.main;
import methodcase.probe as probe;
import io;

export func main(list<string> args): void {
    io.println(probe.result());
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	const want = "rejected,rejected,rejected:42\n"
	cold := run("cold VM", entryPath)
	if cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "methodcase")
	run(
		"build",
		"build", "--entry", "methodcase.main", "--out", outBin, pkgDir,
	)
	builtCmd := exec.Command(outBin)
	builtCmd.Dir = pkgDir
	builtOut, err := builtCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	if built := string(builtOut); built != want {
		t.Fatalf("built binary: got %q, want %q", built, want)
	}
}

func TestStringRuneCacheAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: stringcache\nversion: 0.1.0\nsource: src\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module stringcache.main;
import bytes;
import io;
import string;

export func main(list<string> args): void {
    let ascii = "a".repeat(257);
    let pair = string.fromCodePoints([233, 20013]);
    let unicode = pair.repeat(200);
    io.println(ascii.length());
    io.println(ascii.substring(127, 130));
    io.println(unicode.length());
    io.println(unicode[201] == string.fromCodePoint(20013));
    try {
        bytes.toString(bytes.fromHex("61ff62"));
        io.println("accepted");
    } catch (RuntimeError e) {
        io.println(e.getMessage().contains("not valid UTF-8"));
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	const want = "257\naaa\n400\ntrue\ntrue\n"
	if cold := run("cold VM", entryPath); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "stringcache")
	run("build", "build", "--entry", "stringcache.main", "--out", outBin, pkgDir)
	builtCmd := exec.Command(outBin)
	builtCmd.Dir = pkgDir
	builtOut, err := builtCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	if built := string(builtOut); built != want {
		t.Fatalf("built binary: got %q, want %q", built, want)
	}
}

// TestCrossModuleBuildBinaryIdenticalOutput builds a standalone binary from a
// cross-module package and asserts its output matches a direct geblang run.
func TestCrossModuleBuildBinaryIdenticalOutput(t *testing.T) {
	bin := buildCMBinary(t)

	// Package layout: geblang.yaml + src/cmrbuild/base.gb + src/cmrbuild/main.gb
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	os.MkdirAll(srcDir, 0755)

	os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), []byte("name: cmrbuild\nversion: 0.1.0\nsource: src\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "base.gb"), []byte(
		"module cmrbuild.base;\n"+
			"export class Base {\n"+
			"    func describe(): string { return \"from-base\"; }\n"+
			"}\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "main.gb"), []byte(
		"module cmrbuild.main;\n"+
			"import cmrbuild.base as base;\n"+
			"import io;\n"+
			"class Sub extends base.Base {}\n"+
			"export func main(list<string> args): void {\n"+
			"    io.println(Sub().describe());\n"+
			"}\n"), 0644)

	outBin := filepath.Join(pkgDir, "cmrbuild")
	buildOut, err := exec.Command(bin, "build", "--entry", "cmrbuild.main", "--out", outBin, pkgDir).CombinedOutput()
	if err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}

	// Run the built binary.
	builtOut, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}

	// Run geblang directly against the entry source for comparison.
	entryPath := filepath.Join(srcDir, "main.gb")
	cmd := exec.Command(bin, entryPath)
	cmd.Dir = pkgDir
	directOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("direct geblang run failed: %v\n%s", err, directOut)
	}

	if string(builtOut) != string(directOut) {
		t.Fatalf("built binary output differs from direct run:\nbuilt:  %q\ndirect: %q", builtOut, directOut)
	}
	if string(builtOut) != cmrExpected {
		t.Fatalf("expected %q, got %q", cmrExpected, builtOut)
	}
}

// TestCrossModuleInheritedDeserializeAcrossRuntimePaths pins json.parseAs dispatch of a cross-module-inherited static __deserialize__ across cold VM, cached .gbc, and evaluator (own, inherited, override).
func TestCrossModuleInheritedDeserializeAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: dsmod\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "dsbase.gb"), []byte(`module dsmod.dsbase;
export class Base {
    int value;
    func Base(int value) { this.value = value; }
    static func __deserialize__(dict d): Base { return Base(d["value"] * 10); }
    func show(): int { return this.value; }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module dsmod.main;
import dsmod.dsbase as base;
import io;
import json;
class Sub extends base.Base {
    func Sub(int value) { parent(value); }
}
class SubOwn extends base.Base {
    func SubOwn(int value) { parent(value); }
    static func __deserialize__(dict d): SubOwn { return SubOwn(d["value"] + 1); }
}
export func main(list<string> args): void {
    io.println(json.parseAs("{\"value\": 4}", base.Base).show());
    io.println(json.parseAs("{\"value\": 4}", Sub).show());
    io.println(json.parseAs("{\"value\": 4}", SubOwn).show());
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	const want = "40\n40\n5\n"
	if cold := run("cold VM", entryPath); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	// Built binary: the entry module loads as a sub-module, so its class-as-value constants must carry the running module (regression guard for the entry-module class index out of range crash).
	outBin := filepath.Join(pkgDir, "dsmod")
	if buildOut, err := exec.Command(bin, "build", "--entry", "dsmod.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	if built, err := exec.Command(outBin).CombinedOutput(); err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, built)
	} else if string(built) != want {
		t.Fatalf("built binary: got %q, want %q", string(built), want)
	}
}

// Regression guard: json.parseAs of a plain constructor-hydrated entry-module subclass whose parent is in an imported module, across cold VM, cached .gbc, evaluator, and the built binary (where the entry loads as a sub-module).
func TestEntryModuleSubclassCrossModuleParentDeserialize(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"),
		[]byte("name: dsplain\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "shapes.gb"), []byte(`module dsplain.shapes;
export class Base {
    int a;
    func Base(int a) { this.a = a; }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(srcDir, "main.gb")
	if err := os.WriteFile(entryPath, []byte(`module dsplain.main;
import dsplain.shapes as shapes;
import io;
import json;
class Derived extends shapes.Base {
    int b;
    func Derived(int a, int b) { parent(a); this.b = b; }
}
export func main(list<string> args): void {
    let d = json.parseAs("{\"a\": 1, \"b\": 2}", Derived);
    io.println("${d.a}-${d.b}");
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", label, err, out)
		}
		return string(out)
	}

	const want = "1-2\n"
	if cold := run("cold VM", entryPath); cold != want {
		t.Fatalf("cold VM: got %q, want %q", cold, want)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".geblang-cache")); err != nil {
		t.Fatalf("cold VM did not create bytecode cache: %v", err)
	}
	if warm := run("cached VM", entryPath); warm != want {
		t.Fatalf("cached VM: got %q, want %q", warm, want)
	}
	if eval := run("evaluator", "--disable-vm", entryPath); eval != want {
		t.Fatalf("evaluator: got %q, want %q", eval, want)
	}

	outBin := filepath.Join(pkgDir, "dsplain")
	if buildOut, err := exec.Command(bin, "build", "--entry", "dsplain.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	if built, err := exec.Command(outBin).CombinedOutput(); err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, built)
	} else if string(built) != want {
		t.Fatalf("built binary: got %q, want %q", string(built), want)
	}
}

// TestCrossModuleBothBackendsIdenticalOutput runs main.gb on the VM and the
// evaluator and asserts identical stdout (divergence probe).
func TestCrossModuleBothBackendsIdenticalOutput(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "base.gb"), []byte(cmrBaseGb), 0644)
	mainPath := filepath.Join(dir, "main.gb")
	os.WriteFile(mainPath, []byte(cmrMainGb), 0644)

	var outputs [2]string
	modes := [][]string{{mainPath}, {"--disable-vm", mainPath}}
	labels := []string{"VM", "evaluator"}
	for i, mode := range modes {
		out, err := exec.Command(bin, mode...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: run failed: %v\n%s", labels[i], err, out)
		}
		outputs[i] = string(out)
	}

	if outputs[0] != outputs[1] {
		t.Fatalf("backend output differs:\nVM:        %q\nevaluator: %q", outputs[0], outputs[1])
	}
	if outputs[0] != cmrExpected {
		t.Fatalf("expected %q, got %q", cmrExpected, outputs[0])
	}
}

const cmNamedDonorGb = "module donor;\n" +
	"import io;\n" +
	"export func show4(int a, int b, int c, int d): void { io.println(\"${a} ${b} ${c} ${d}\"); }\n"

const cmNamedMainGb = "import donor;\n" +
	"donor.show4(d: 4, a: 1, b: 2, c: 3);\n"

const cmNamedExpected = "1 2 3 4\n"

// A cross-module named function call binds identically across the cold VM, the .gbc cache-hit, and the evaluator.
func TestCrossModuleNamedCallAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(cmNamedDonorGb), 0644)
	os.WriteFile(filepath.Join(dir, "main.gb"), []byte(cmNamedMainGb), 0644)

	run := func(args ...string) string {
		cmd := exec.Command(bin, append(args, "main.gb")...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	cold := run()
	if cold != cmNamedExpected {
		t.Fatalf("cold VM: expected %q, got %q", cmNamedExpected, cold)
	}
	warm := run()
	if warm != cold {
		t.Fatalf(".gbc cache-hit differs from cold run:\ncold: %q\nwarm: %q", cold, warm)
	}
	eval := run("--disable-vm")
	if eval != cold {
		t.Fatalf("evaluator differs from VM:\nvm:   %q\neval: %q", cold, eval)
	}
}

const cmNamedCtorBaseGb = "module base;\n" +
	"export class Base {\n" +
	"    int bx;\n" +
	"    int bz;\n" +
	"    func Base(int bx, int bz = 100) { this.bx = bx; this.bz = bz; }\n" +
	"}\n"

const cmNamedCtorDonorGb = "module donor;\n" +
	"import base;\n" +
	"export class Point {\n" +
	"    int x;\n" +
	"    int y;\n" +
	"    func Point(int x, int y = 10) { this.x = x; this.y = y; }\n" +
	"    func show(): string { return \"Point(${this.x}, ${this.y})\"; }\n" +
	"}\n" +
	"export class Box {\n" +
	"    int a;\n" +
	"    string label;\n" +
	"    int width;\n" +
	"    func Box(int a, string b) { this.a = a; this.label = b; this.width = 0; }\n" +
	"    func Box(int width) { this.a = 0; this.label = \"w\"; this.width = width; }\n" +
	"    func show(): string { return \"Box(${this.a},${this.label},${this.width})\"; }\n" +
	"}\n" +
	"export class Sub extends base.Base {\n" +
	"    int extra;\n" +
	"    func Sub(int bx, int extra) { parent(bx); this.extra = extra; }\n" +
	"    func showsub(): string { return \"Sub(${this.bx},${this.bz},${this.extra})\"; }\n" +
	"}\n"

const cmNamedCtorMainGb = "import donor;\n" +
	"import io;\n" +
	"io.println(donor.Point(y: 2, x: 1).show());\n" +
	"io.println(donor.Point(x: 5).show());\n" +
	"io.println(donor.Box(a: 5, b: \"hi\").show());\n" +
	"io.println(donor.Box(width: 3).show());\n" +
	"io.println(donor.Sub(extra: 9, bx: 7).showsub());\n"

const cmNamedCtorExpected = "Point(1, 2)\nPoint(5, 10)\nBox(5,hi,0)\nBox(0,w,3)\nSub(7,100,9)\n"

// A cross-module named constructor call binds and selects the overload identically across the cold VM, the .gbc cache-hit, and the evaluator.
func TestCrossModuleNamedCtorAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "base.gb"), []byte(cmNamedCtorBaseGb), 0644)
	os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(cmNamedCtorDonorGb), 0644)
	os.WriteFile(filepath.Join(dir, "main.gb"), []byte(cmNamedCtorMainGb), 0644)

	run := func(args ...string) string {
		cmd := exec.Command(bin, append(args, "main.gb")...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	cold := run()
	if cold != cmNamedCtorExpected {
		t.Fatalf("cold VM: expected %q, got %q", cmNamedCtorExpected, cold)
	}
	warm := run()
	if warm != cold {
		t.Fatalf(".gbc cache-hit differs from cold run:\ncold: %q\nwarm: %q", cold, warm)
	}
	eval := run("--disable-vm")
	if eval != cold {
		t.Fatalf("evaluator differs from VM:\nvm:   %q\neval: %q", cold, eval)
	}
}

// A cross-module named constructor call in a built binary matches a direct run.
func TestCrossModuleNamedCtorBuildBinary(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	os.MkdirAll(srcDir, 0755)

	os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), []byte("name: cmnamedctor\nversion: 0.1.0\nsource: src\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "base.gb"), []byte(
		"module cmnamedctor.base;\n"+
			"export class Base {\n"+
			"    int bx;\n"+
			"    int bz;\n"+
			"    func Base(int bx, int bz = 100) { this.bx = bx; this.bz = bz; }\n"+
			"}\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "donor.gb"), []byte(
		"module cmnamedctor.donor;\n"+
			"import cmnamedctor.base as base;\n"+
			"export class Point {\n"+
			"    int x;\n"+
			"    int y;\n"+
			"    func Point(int x, int y = 10) { this.x = x; this.y = y; }\n"+
			"    func show(): string { return \"Point(${this.x}, ${this.y})\"; }\n"+
			"}\n"+
			"export class Sub extends base.Base {\n"+
			"    int extra;\n"+
			"    func Sub(int bx, int extra) { parent(bx); this.extra = extra; }\n"+
			"    func showsub(): string { return \"Sub(${this.bx},${this.bz},${this.extra})\"; }\n"+
			"}\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "main.gb"), []byte(
		"module cmnamedctor.main;\n"+
			"import cmnamedctor.donor as donor;\n"+
			"import io;\n"+
			"export func main(list<string> args): void {\n"+
			"    io.println(donor.Point(y: 2, x: 1).show());\n"+
			"    io.println(donor.Sub(extra: 9, bx: 7).showsub());\n"+
			"}\n"), 0644)

	outBin := filepath.Join(pkgDir, "cmnamedctor")
	if buildOut, err := exec.Command(bin, "build", "--entry", "cmnamedctor.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	builtOut, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	const want = "Point(1, 2)\nSub(7,100,9)\n"
	if string(builtOut) != want {
		t.Fatalf("built binary: expected %q, got %q", want, builtOut)
	}
}

const cmOverloadDonorGb = "module donor;\n" +
	"export func over(int v): string { return \"int:${v}\"; }\n" +
	"export func over(string s): string { return \"str:${s}\"; }\n" +
	"export func over(int a, int b): string { return \"two:${a},${b}\"; }\n"

const cmOverloadMainGb = "import donor;\n" +
	"import io;\n" +
	"from donor import over;\n" +
	"io.println(donor.over(5));\n" +
	"io.println(donor.over(\"x\"));\n" +
	"io.println(donor.over(a: 1, b: 2));\n" +
	"io.println(over(9));\n" +
	"let f = donor.over;\n" +
	"io.println(f(\"val\"));\n"

const cmOverloadExpected = "int:5\nstr:x\ntwo:1,2\nint:9\nstr:val\n"

// A cross-module exported overload set selects the overload identically across the cold VM, the .gbc cache-hit, and the evaluator.
func TestCrossModuleOverloadAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "donor.gb"), []byte(cmOverloadDonorGb), 0644)
	os.WriteFile(filepath.Join(dir, "main.gb"), []byte(cmOverloadMainGb), 0644)

	run := func(args ...string) string {
		cmd := exec.Command(bin, append(args, "main.gb")...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	cold := run()
	if cold != cmOverloadExpected {
		t.Fatalf("cold VM: expected %q, got %q", cmOverloadExpected, cold)
	}
	warm := run()
	if warm != cold {
		t.Fatalf(".gbc cache-hit differs from cold run:\ncold: %q\nwarm: %q", cold, warm)
	}
	eval := run("--disable-vm")
	if eval != cold {
		t.Fatalf("evaluator differs from VM:\nvm:   %q\neval: %q", cold, eval)
	}
}

// A cross-module exported overload set resolves in a built binary matching a direct run.
func TestCrossModuleOverloadBuildBinary(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	os.MkdirAll(srcDir, 0755)

	os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), []byte("name: cmoverload\nversion: 0.1.0\nsource: src\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "donor.gb"), []byte(
		"module cmoverload.donor;\n"+
			"export func over(int v): string { return \"int:${v}\"; }\n"+
			"export func over(string s): string { return \"str:${s}\"; }\n"+
			"export func over(int a, int b): string { return \"two:${a},${b}\"; }\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "main.gb"), []byte(
		"module cmoverload.main;\n"+
			"import cmoverload.donor as donor;\n"+
			"import io;\n"+
			"export func main(list<string> args): void {\n"+
			"    io.println(donor.over(5));\n"+
			"    io.println(donor.over(\"x\"));\n"+
			"    io.println(donor.over(a: 1, b: 2));\n"+
			"    let f = donor.over;\n"+
			"    io.println(f(\"val\"));\n"+
			"}\n"), 0644)

	outBin := filepath.Join(pkgDir, "cmoverload")
	if buildOut, err := exec.Command(bin, "build", "--entry", "cmoverload.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	builtOut, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	const want = "int:5\nstr:x\ntwo:1,2\nstr:val\n"
	if string(builtOut) != want {
		t.Fatalf("built binary: expected %q, got %q", want, builtOut)
	}
}

// A cross-module named function call in a built binary matches a direct run.
func TestCrossModuleNamedCallBuildBinary(t *testing.T) {
	bin := buildCMBinary(t)
	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	os.MkdirAll(srcDir, 0755)

	os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), []byte("name: cmnamed\nversion: 0.1.0\nsource: src\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "donor.gb"), []byte(
		"module cmnamed.donor;\n"+
			"import io;\n"+
			"export func show4(int a, int b, int c, int d): void { io.println(\"${a} ${b} ${c} ${d}\"); }\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "main.gb"), []byte(
		"module cmnamed.main;\n"+
			"import cmnamed.donor as donor;\n"+
			"export func main(list<string> args): void {\n"+
			"    donor.show4(d: 4, a: 1, b: 2, c: 3);\n"+
			"}\n"), 0644)

	outBin := filepath.Join(pkgDir, "cmnamed")
	if buildOut, err := exec.Command(bin, "build", "--entry", "cmnamed.main", "--out", outBin, pkgDir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build failed: %v\n%s", err, buildOut)
	}
	builtOut, err := exec.Command(outBin).CombinedOutput()
	if err != nil {
		t.Fatalf("built binary failed: %v\n%s", err, builtOut)
	}
	if string(builtOut) != cmNamedExpected {
		t.Fatalf("built binary: expected %q, got %q", cmNamedExpected, builtOut)
	}
}
