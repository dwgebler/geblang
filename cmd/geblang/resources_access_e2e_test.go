package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceAccessSourceCacheAndBuiltRemap(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build geblang: %v\n%s", err, output)
	}
	project := t.TempDir()
	write := func(name, content string) {
		location := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(location), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(location, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: app\nversion: 0.0.0\nresources:\n  - assets\n")
	write("assets/message.txt", "resource-ok")
	if err := os.WriteFile(filepath.Join(project, "assets", "binary.dat"), []byte{0, 255}, 0o644); err != nil {
		t.Fatal(err)
	}
	write("assets/remap.txt", "remapped")
	write("private.txt", "not declared")
	write("app.gb", `module app;
import bytes;
import io;
import resources;
export func main(): int {
    io.println(resources.readText("assets/message.txt"));
    io.println(bytes.toHex(resources.readBytes("assets/binary.dat")));
    io.println(resources.exists("extra/remapped.txt"));
    io.println(resources.exists("private.txt"));
    return 0;
}
`)
	write("runner.gb", `import app;
app.main();
`)
	away := t.TempDir()
	cacheEnv := append(os.Environ(), "XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "cache"))
	for run := 0; run < 2; run++ {
		cmd := exec.Command(bin, "run", filepath.Join(project, "runner.gb"))
		cmd.Dir = away
		cmd.Env = cacheEnv
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "resource-ok\n00ff\nfalse\nfalse\n" {
			t.Fatalf("source run %d: %v, output %q", run+1, err, output)
		}
	}
	executable := filepath.Join(project, "app-bin")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable,
		"--resource", "assets/remap.txt=extra/remapped.txt", ".")
	build.Dir = project
	build.Env = cacheEnv
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build app: %v\n%s", err, output)
	}
	launch := exec.Command(executable)
	launch.Dir = away
	launch.Env = cacheEnv
	output, err := launch.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "resource-ok\n00ff\ntrue\nfalse" {
		t.Fatalf("built app: %v, output %q", err, output)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "assets", "escape.txt")); err != nil {
		t.Fatal(err)
	}
	unsafeBuild := exec.Command(bin, "build", "--entry", "app", "--out", filepath.Join(project, "unsafe-bin"), ".")
	unsafeBuild.Dir = project
	unsafeBuild.Env = cacheEnv
	if output, err := unsafeBuild.CombinedOutput(); err == nil || !strings.Contains(string(output), "escapes the project directory") {
		t.Fatalf("symlink escape build: %v, output %q", err, output)
	}
}
