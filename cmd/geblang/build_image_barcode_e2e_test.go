package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Fresh compile, .gbc cache hit, evaluator and bundled binary must all print the same output.
func TestImageDrawingAndBarcodesOnCachedAndBuiltPaths(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}
	stdlib, err := filepath.Abs(filepath.Join("..", "..", "stdlib"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: imgapp\nsource: src\n")
	write("src/badges.gb", `module badges;
import image;

export class Badge extends image.Image {
    func Badge(int size) {
        parent(image.blank(size, size, "white")._handle);
        this.rect(0, 0, size, size, {"color": "black"});
    }

    func badge(string color): Badge {
        this.circle(this.width() / 2, this.height() / 2, 3, {"fill": color});
        return this;
    }
}
`)
	write("src/main.gb", `module main;
import io;
import image;
import barcode;
import badges;

class Gold extends badges.Badge {
    func Gold() { parent(16); }
    func gold(): Gold {
        this.badge("orange").text("G", 1, 1, {"size": 8, "font": "bold"});
        return this;
    }
}

export func main(list<string> args): int {
    let g = Gold().gold();
    io.println(g.getPixel(0, 8) + " " + g.getPixel(8, 8) + " " + g.getPixel(13, 13));
    let qr = barcode.qr("https://example.com", {"scale": 2, "level": "H"});
    let sheet = image.blank(qr.width() + 20, qr.height() + 20, "white").paste(qr, 10, 10).paste(g, 0, 0, {"opacity": 0.5});
    io.println("${sheet.width()}x${sheet.height()} ${sheet.encode("png").length()} ${sheet.getPixel(18, 18)}");
    io.println(image.measureText("cached", {"font": "mono", "size": 12}));
    for (format in barcode.formats()) {
        if (format == "ean13") {
            io.println(barcode.encode("590123412345", {"format": format}).width());
        }
    }
    try {
        barcode.encode("x", {"format": "ean8"});
    } catch (RuntimeError e) {
        io.println(e.message);
    }
    try {
        g.fill("nope");
    } catch (RuntimeError e) {
        io.println(e.message);
    }
    return 0;
}
`)

	env := append(os.Environ(), "GEBLANG_STDLIB="+stdlib)
	run := func(label string, cmd *exec.Cmd) string {
		t.Helper()
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", label, err, out)
		}
		return string(out)
	}
	script := func() *exec.Cmd {
		cmd := exec.Command(bin, "run", "src/main.gb")
		cmd.Dir = dir
		return cmd
	}
	fresh := run("fresh run", script())
	cached := run("cached run", script())
	evalCmd := exec.Command(bin, "run", "--disable-vm", "src/main.gb")
	evalCmd.Dir = dir
	evaluated := run("evaluator run", evalCmd)

	out := filepath.Join(dir, "out", "app")
	build := exec.Command(bin, "build", "--entry", "main", "--out", out, ".")
	build.Dir = dir
	run("geblang build", build)
	builtCmd := exec.Command(out)
	builtCmd.Dir = t.TempDir()
	built := run("built binary", builtCmd)

	want := "#000000ff #ffa500ff #ffffffff\n"
	if len(fresh) < len(want) || fresh[:len(want)] != want {
		t.Fatalf("unexpected output:\n%s", fresh)
	}
	for label, got := range map[string]string{"cached": cached, "evaluator": evaluated, "built": built} {
		if got != fresh {
			t.Errorf("%s output differs from the fresh VM run:\n fresh: %q\n %s: %q", label, fresh, label, got)
		}
	}
}
