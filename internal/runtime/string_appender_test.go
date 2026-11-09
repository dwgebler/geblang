package runtime

import (
	goruntime "runtime"
	"strings"
	"testing"
)

func TestStringAppenderKeepsEarlierResults(t *testing.T) {
	var a StringAppender
	base := strings.Repeat("x", 300)
	s1 := a.Concat(base, "a")
	s2 := a.Concat(s1, "b")
	branch := a.Concat(s1, "c")
	s3 := a.Concat(s2, strings.Repeat("y", 1000))
	if s1 != base+"a" || s2 != base+"ab" || branch != base+"ac" || s3 != base+"ab"+strings.Repeat("y", 1000) {
		t.Fatalf("appender changed an earlier result: %q %q %q", s1[300:], s2[300:], branch[300:])
	}
	if base != strings.Repeat("x", 300) {
		t.Fatalf("appender changed its input")
	}
}

func TestStringAppenderLoopMatchesConcatenation(t *testing.T) {
	var a StringAppender
	got, want := "", ""
	other := ""
	for i := 0; i < 5000; i++ {
		piece := string(rune('a' + i%26))
		got = a.Concat(got, piece)
		other = a.Concat(other, "-"+piece)
		want += piece
	}
	if got != want || len(other) != 10000 {
		t.Fatalf("interleaved appends diverged: len %d vs %d, other %d", len(got), len(want), len(other))
	}
	a.Reset()
	if a.Concat(got, "!") != want+"!" {
		t.Fatalf("concat after reset diverged")
	}
}

func TestStringAppenderDoesNotRetainDroppedStrings(t *testing.T) {
	var a StringAppender
	big := a.Concat(strings.Repeat("x", 1<<20), "y")
	if len(big) != 1<<20+1 {
		t.Fatalf("unexpected length %d", len(big))
	}
	big = ""
	_ = big
	gcTwice()
	for i := range a.slots {
		if a.slots[i].data.Value() != nil {
			t.Fatalf("slot %d still holds a dropped buffer", i)
		}
	}
}

func gcTwice() {
	goruntime.GC()
	goruntime.GC()
}
