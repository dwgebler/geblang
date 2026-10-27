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
	Active.Store(false)
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
	if got := FoldedPath("profile.folded"); got == "profile.folded" {
		t.Fatal("folded output would overwrite the requested report")
	}
	if got := FoldedPath("/tmp/out.txt"); got != "/tmp/out.folded" {
		t.Errorf("FoldedPath = %q", got)
	}
	if got := FoldedPath("profile"); got != "profile.folded" {
		t.Errorf("FoldedPath = %q", got)
	}
}

func TestSamplingCountsEachThreadAtMostOncePerTick(t *testing.T) {
	stop := Collect()
	start := Tick()

	for i := 0; i < 20000; i++ {
		RecordThread(1, Tick(), []string{"first"}, "")
		RecordThread(1, Tick(), []string{"second VM on first thread"}, "")
		RecordThread(2, Tick(), []string{"second thread"}, "")
	}
	end := Tick()
	stacks := stop()

	total := 0
	for _, count := range stacks {
		total += count
	}
	if total < 2 || total > 2*int(end-start+1) {
		t.Fatalf("%d samples across %d ticks and two threads", total, end-start+1)
	}
}

func TestSamplingWhileCollectionsStartAndStop(t *testing.T) {
	ready := make(chan struct{})
	finish := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		close(ready)
		for {
			select {
			case <-finish:
				return
			default:
			}
			if Active.Load() {
				RecordThread(1, Tick(), []string{"worker"}, "")
			}
		}
	}()
	<-ready
	for i := 0; i < 50; i++ {
		collect := Collect()
		collect()
	}
	close(finish)
	<-stopped
}
