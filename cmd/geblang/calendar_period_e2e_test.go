package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalendarPeriodCachedAndBuiltBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build geblang: %v\n%s", err, output)
	}
	project := t.TempDir()
	cacheHome := filepath.Join(project, "cache")
	cacheEnv := append(os.Environ(), "XDG_CACHE_HOME="+cacheHome)
	files := map[string]string{
		"geblang.yaml": "name: app\nversion: 0.0.0\n",
		"app.gb": `module app;
import datetime;
import datetime.period as period;
import io;
export func main(): int {
    let next = period.Period(0, 1).addTo(datetime.Instant(2024, 1, 31));
    io.println(next.formatRFC3339());
    return 0;
}
`,
		"runner.gb": `import app;
app.main();
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for run := 0; run < 2; run++ {
		cmd := exec.Command(bin, "run", "runner.gb")
		cmd.Dir = project
		cmd.Env = cacheEnv
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "2024-02-29T00:00:00Z\n" {
			t.Fatalf("source run %d: %v, output %q", run+1, err, output)
		}
	}
	cache, err := filepath.Glob(filepath.Join(project, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("expected cached bytecode: %v, files %v", err, cache)
	}
	executable := filepath.Join(project, "calendar-app")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	build.Env = cacheEnv
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build app: %v\n%s", err, output)
	}
	launch := exec.Command(executable)
	launch.Dir = t.TempDir()
	launch.Env = cacheEnv
	output, err := launch.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "2024-02-29T00:00:00Z" {
		t.Fatalf("built app: %v, output %q", err, output)
	}
}
