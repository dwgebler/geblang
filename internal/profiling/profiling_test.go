package profiling

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportListsOwnTotalAndLines(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "run.txt")
	Start(report)
	Record([]string{"<top level>", "main.outer", "main.inner"}, "main.inner:4")
	Record([]string{"<top level>", "main.outer", "main.inner"}, "main.inner:4")
	Record([]string{"<top level>", "main.outer"}, "")
	Finish()
	Finish()
	Active = false
	text, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Geblang profile: 3 samples",
		"       2   66.67%  main.inner",
		"       3  100.00%  main.outer",
		"       2   66.67%  main.inner:4",
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	folded, err := os.ReadFile(filepath.Join(dir, "run.folded"))
	if err != nil {
		t.Fatal(err)
	}
	want := "<top level>;main.outer 1\n<top level>;main.outer;main.inner 2\n"
	if string(folded) != want {
		t.Errorf("folded = %q, want %q", folded, want)
	}
}

func TestFoldedPathReplacesTheExtension(t *testing.T) {
	if got := FoldedPath("/tmp/out.txt"); got != "/tmp/out.folded" {
		t.Errorf("FoldedPath = %q", got)
	}
	if got := FoldedPath("profile"); got != "profile.folded" {
		t.Errorf("FoldedPath = %q", got)
	}
}
