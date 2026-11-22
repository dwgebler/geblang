package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEntryMainFrameLineE2E pins the synthetic call into an exported main to main's own line on every run path.
func TestEntryMainFrameLineE2E(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	cases := []struct {
		name, source, want string
	}{
		{
			name: "no args",
			source: `module app;
import io;


export func main() {
    let n = 0;
    io.println(1 // n);
}
`,
			want: "  at main (line 7)\n  at <top level> (line 5)\n",
		},
		{
			name: "args and int return",
			source: `module app;
import io;

export func main(list<string> args): int {
    let n = 0;
    io.println(1 // n);
    return 0;
}
`,
			want: "  at main (line 6)\n  at <top level> (line 4)\n",
		},
		{
			name: "existing init block",
			source: `module app;
import io;

init {
    io.println("setup");
}

export func main() {
    let n = 0;
    io.println(1 // n);
}
`,
			want: "  at main (line 10)\n  at <top level> (line 8)\n",
		},
		{
			name:   "main on the first line",
			source: "export func main() { let n = 0; let q = 1 // n; }\n",
			want:   "  at main (line 1)\n  at <top level> (line 1)\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte("name: app\nversion: 0.0.1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(dir, "app.gb")
			if err := os.WriteFile(src, []byte(c.source), 0o644); err != nil {
				t.Fatal(err)
			}
			cold := runExpectingExit1(t, bin, src)
			if !strings.HasSuffix(cold, c.want) {
				t.Fatalf("cold run frames:\n%s\nwant suffix:\n%s", cold, c.want)
			}
			if warm := runExpectingExit1(t, bin, src); warm != cold {
				t.Fatalf("cache-hit run differs:\n%s\nvs cold:\n%s", warm, cold)
			}
			if eval := runExpectingExit1(t, bin, "--disable-vm", src); eval != cold {
				t.Fatalf("evaluator run differs:\n%s\nvs vm:\n%s", eval, cold)
			}
			out := filepath.Join(dir, "out", "app")
			if buildOut, err := exec.Command(bin, "build", "--entry", "app", "--out", out, dir).CombinedOutput(); err != nil {
				t.Fatalf("geblang build: %v\n%s", err, buildOut)
			}
			if built := runExpectingExit1(t, out); built != cold {
				t.Fatalf("built binary differs:\n%s\nvs run:\n%s", built, cold)
			}
		})
	}
}
