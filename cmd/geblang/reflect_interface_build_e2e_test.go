package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuiltBinaryReflectsInterfaces(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if buildOut, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, buildOut)
	}
	dir := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: ifaceapp\nversion: 1.0.0\n",
		"shapes.gb": `module shapes;

export interface Shape {
    func area(): int;
}
`,
		"app.gb": `module app;
import io;
import reflect;
import shapes;

export func main(): int {
    io.println("${reflect.className(shapes.Shape)} ${reflect.location(shapes.Shape)}");
    return 0;
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "out", "ifaceapp")
	if buildOut, err := exec.Command(bin, "build", "--entry", "app", "--out", out, dir).CombinedOutput(); err != nil {
		t.Fatalf("geblang build: %v\n%s", err, buildOut)
	}
	got, err := exec.Command(out).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if want := `Shape {"column": 8, "line": 3, "module": "shapes"}` + "\n"; string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
