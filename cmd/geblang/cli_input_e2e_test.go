package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInput_PipedReadersMatchAcrossCachedAndBuiltExecution(t *testing.T) {
	bin := buildCMBinary(t)
	project := t.TempDir()
	env := append(os.Environ(), "XDG_CACHE_HOME="+filepath.Join(project, "cache"))
	files := map[string]string{
		"geblang.yaml": "name: inputapp\nversion: 0.0.0\n",
		"reader.gb": `module reader;
import cli;
import io;
import cli.widgets as widgets;
import secureRandom;
export class Reader {
    func next(): ?string { return cli.readLine(); }
}
export class ChildReader extends Reader {}
class ChildProgress extends widgets.ManagedProgress { func ChildProgress() { parent(2,"Child",{"enabled":false}); } }
export func read(): void {
    io.println(Reader().next());
    io.println(ChildReader().next());
    io.println(io.readLine(io.stdin()));
    io.println(cli.promptInt("", {"min":1,"cancel":["q"]}));
    io.println("ff".toIntOrNull(16));
    io.println(widgets.withProgress(1,"work",func(widgets.ManagedProgress p): int { p.advance(); return 7; }));
    widgets.ProgressBar progress = ChildProgress();
    progress.set(99);
    io.println(progress.current);
    io.println(io.stdinReadAll());
    let input = io.stdin();
    assert(io.readLines(input) == []);
    cli.withContext({"stdin":input,"quiet":true},func(): void { assert(cli.readLine() == null); });
    io.println(cli.readLine() == null);
    let session = secureRandom.fromSeed("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f");
    assert(secureRandom.bool(s:session) instanceof bool);
    assert(secureRandom.choice(items:[7],s:session) == 7);
    assert(secureRandom.choice(items:[9]) == 9);
}
`,
		"app.gb": `module app;
import reader;
export func main(): int { reader.read(); return 0; }
`,
		"runner.gb": "import app;\napp.main();\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := "first\nsecond\nthird\n4\n255\n7\n2\nlast\ntrue\n"
	run := func(cmd *exec.Cmd) {
		t.Helper()
		cmd.Dir = project
		cmd.Env = env
		cmd.Stdin = strings.NewReader("first\nsecond\nthird\n4\nlast")
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != want {
			t.Fatalf("%v: %v\noutput %q, want %q", cmd.Args, err, output, want)
		}
	}
	for _, args := range [][]string{{"run", "runner.gb"}, {"run", "runner.gb"}, {"--disable-vm", "runner.gb"}} {
		run(exec.Command(bin, args...))
	}
	cache, err := filepath.Glob(filepath.Join(project, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("expected cached bytecode: %v, files %v", err, cache)
	}
	executable := filepath.Join(project, "input-app")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	build.Env = env
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run(exec.Command(executable))
}
