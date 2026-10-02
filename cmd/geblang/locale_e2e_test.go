package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocaleSourceCacheAndBuiltBinary(t *testing.T) {
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
import bytes;
import datetime;
import i18n;
import io;
import locale;
export func main(): int {
    io.println(locale.formatNumber(12345.67, "de"));
    io.println(bytes.toHex(bytes.fromString(
        locale.formatCurrency(1234.5, "EUR", "fr"))));
    io.println(locale.formatDate(datetime.Instant("2024-07-04T23:30:00Z"),
        "en-GB", "full", "Europe/London"));
    let translator = i18n.catalog({
        "en": {"hello": "Hello {name}"},
        "fr": {"hello": "Bonjour {name}"}
    }, "fr-CA");
    io.println(translator.text("hello", {"name": "Ada"}));
    return 0;
}
`)
	write("runner.gb", "import app;\napp.main();\n")
	want := "12.345,67\n31c2a03233342c3530c2a0e282ac\nFriday, 5 July 2024\nBonjour Ada\n"
	away := t.TempDir()
	cacheEnv := append(os.Environ(), "XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "cache"))
	for run := 0; run < 2; run++ {
		cmd := exec.Command(bin, "run", filepath.Join(project, "runner.gb"))
		cmd.Dir = away
		cmd.Env = cacheEnv
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != want {
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
	if err != nil || string(output) != want {
		t.Fatalf("built app: %v, output %q", err, output)
	}
}
