package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExtCallForwardsNamedArgs: ext.call and ext.callWithOptions bind their own parameters by name and send any other named argument as a kwarg, on both backends.
func TestExtCallForwardsNamedArgs(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	extension, err := filepath.Abs("../../examples/extensions/python/extension.py")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "geblang")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build geblang: %v\n%s", err, output)
	}
	project := t.TempDir()
	manifest := "name: extkw\nversion: 0.0.0\nsource: .\nextensions:\n  python_example:\n    command: [\"python3\", \"" + extension + "\"]\n    startup_timeout_ms: 10000\n"
	if err := os.WriteFile(filepath.Join(project, "geblang.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `import ext;
import io;
let conn = ext.load("python_example");
io.println(ext.call(conn, "greet", name: "Kw"));
io.println(ext.callWithOptions(conn, "greet", {"timeoutMs": 5000}, name: "Opt"));
io.println(ext.call(conn, "add", 2, 3) as string);
io.println(ext.call(function: "greet", name: "Both", conn: conn));
io.println(ext.callWithOptions(conn, "greet", name: "Mixed", options: {"timeoutMs": 5000}));
ext.close(conn);
`
	if err := os.WriteFile(filepath.Join(project, "main.gb"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"run", "main.gb"}, {"--disable-vm", "main.gb"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "cache"))
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "hello Kw\nhello Opt\n5\nhello Both\nhello Mixed\n" {
			t.Fatalf("%v: %v, output %q", args, err, output)
		}
	}
}
