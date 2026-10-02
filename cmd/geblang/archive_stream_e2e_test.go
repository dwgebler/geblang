package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestArchiveStreamSourceCacheAndBuiltBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build geblang: %v\n%s", err, output)
	}
	project := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: app\nversion: 0.0.0\n")
	write("app.gb", `module app;
import archive;
import bytes;
import io;
import path;
export func main(): int {
    let root = io.tempDir("archive-built-*");
    defer io.remove(root);
    let location = path.join(root, "data.zip");
    let writer = archive.create(location, "zip");
    writer.addFile("data.bin", bytes.fromHex("00ff01"));
    writer.close();
    let extracted = archive.extract(location, path.join(root, "output"));
    io.println(extracted.length());
    io.println(bytes.toHex(io.readBytes(path.join(root, "output", "data.bin"))));
    return 0;
}
`)
	write("runner.gb", "import app;\napp.main();\n")
	away := t.TempDir()
	cacheEnv := append(os.Environ(), "XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "cache"))
	for run := 0; run < 2; run++ {
		cmd := exec.Command(bin, "run", filepath.Join(project, "runner.gb"))
		cmd.Dir = away
		cmd.Env = cacheEnv
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "1\n00ff01\n" {
			t.Fatalf("source run %d: %v, output %q", run+1, err, output)
		}
	}
	executable := filepath.Join(project, "app-bin")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	build.Env = cacheEnv
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build app: %v\n%s", err, output)
	}
	launch := exec.Command(executable)
	launch.Dir = away
	launch.Env = cacheEnv
	output, err := launch.CombinedOutput()
	if err != nil || string(output) != "1\n00ff01\n" {
		t.Fatalf("built app: %v, output %q", err, output)
	}
}
