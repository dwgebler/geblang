package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReflectionLocationsAndLocalScopesCachedAndBuilt(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	donor, err := os.ReadFile("../../tests/reflect/location_values_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	source := `module scope_probe;
import io;
import reflect;
import location_values_donor as donor;
class Base { func own(): int { return 10; } }
class Sub extends Base {}
func rebind(bool retire): int {
    let value = 1;
    if (retire) { del value; }
    let value = 2;
    return value;
}
func check(any target, string expectedModule): void {
    let loc = reflect.location(target);
    assert(loc != null);
    assert(loc["module"] == expectedModule);
    assert(loc["line"] > 0);
    assert(loc["column"] > 0);
    assert(donor.inspect(target) == loc);
}
export func main(): void {
    check(func(): int { return 1; }, "scope_probe");
    check(donor.literal(2), "location_values_donor");
    check(donor.Sub().literal(), "location_values_donor");
    check(Sub().own, "scope_probe");
    check(donor.Sub().own, "location_values_donor");
    check(reflect.method(donor.Sub(), "own"), "location_values_donor");
    check(reflect.staticMethod(donor.Base, "staticOwn"), "location_values_donor");
    check(donor.Sub().extra, "location_values_donor");
    let bound = donor.Sub().own;
    assert(bound() == 1);
    let value = 1;
    if (true) { let value = 2; assert(value == 2); }
    assert(value == 1);
    del value;
    let value = 3;
    assert(value == 3);
    assert(rebind(true) == 2);
    bool rejected = false;
    try { rebind(false); } catch (Error e) { rejected = true; }
    assert(rejected);
    let read = func(): int { return value; };
    del value;
    let value = 4;
    assert(read() == 4);
    io.println("locations-and-scopes-ok");
}
`
	for name, contents := range map[string]string{
		"geblang.yaml":             "name: scope_probe_app\nversion: 0.0.0\n",
		"location_values_donor.gb": string(donor),
		"scope_probe.gb":           source,
		"runner.gb":                "import scope_probe; scope_probe.main();\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(executable string, args ...string) {
		t.Helper()
		cmd := exec.Command(executable, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "locations-and-scopes-ok\n" {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, output)
		}
	}

	for _, args := range [][]string{{"run", "runner.gb"}, {"run", "runner.gb"}, {"--disable-vm", "runner.gb"}} {
		run(bin, args...)
	}
	cache, err := filepath.Glob(filepath.Join(dir, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("cache files=%v, error=%v", cache, err)
	}
	executable := filepath.Join(dir, "scope-app")
	build := exec.Command(bin, "build", "--entry", "scope_probe", "--out", executable, ".")
	build.Dir = dir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run(executable)

	bad := filepath.Join(dir, "bad.gb")
	if err := os.WriteFile(bad, []byte("func bad(): void { let x = 1; let x = 2; } bad();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"run", bad}, {"--disable-vm", bad}, {"check", bad}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "already declared") {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
	}
}
