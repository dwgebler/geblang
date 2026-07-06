package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Module-exact instanceof holds identically across evaluator, VM cold, and VM warm (.gbc cache-hit): identity is computed at runtime and survives serialization.
func TestInstanceofModuleExactAcrossRuntimePaths(t *testing.T) {
	bin := buildCMBinary(t)
	donor := "export class Config { func Config() {} }\nexport func make(): any { return Config(); }\n"
	main := `import io;
import moda;
import modb;
let x = modb.make();
io.println(x instanceof modb.Config);
io.println(x instanceof moda.Config);
class Sub extends moda.Config {}
let s = Sub();
io.println(s instanceof moda.Config);
io.println(s instanceof modb.Config);
`
	const want = "true\nfalse\ntrue\nfalse\n"

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "moda.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modb.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.gb"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(label string, args ...string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: run failed: %v\n%s", label, err, out)
		}
		if string(out) != want {
			t.Fatalf("%s: got %q, want %q", label, string(out), want)
		}
	}

	run("vm-cold", "main.gb")
	run("vm-warm", "main.gb")
	run("evaluator", "--disable-vm", "main.gb")
}
