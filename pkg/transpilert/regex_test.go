package transpilert

import (
	"reflect"
	"testing"
)

func (d *OrderedDict[K, V]) mustGet(key K) V {
	v, _ := d.Get(key)
	return v
}

func TestStringRegexMethods(t *testing.T) {
	if !StringMatchesRegex("abc123", "[0-9]+") {
		t.Error("matchesRegex: expected true")
	}
	if StringMatchesRegex("abc", "[0-9]+") {
		t.Error("matchesRegex: expected false")
	}
	if got := StringSplitRegex("a1b2c", "[0-9]"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("splitRegex: got %v", got)
	}
	if got := StringReplaceRegex("a b", "(\\w) (\\w)", "$2 $1"); got != "b a" {
		t.Errorf("replaceRegex: got %q", got)
	}
}

func TestReReplaceFreeFn(t *testing.T) {
	if got := ReReplace("\\d+", "#", "a1b22c"); got != "a#b#c" {
		t.Errorf("ReReplace: got %q", got)
	}
}

func TestRePatternTestFindSplit(t *testing.T) {
	p := ReCompile("[0-9]+")
	if !p.Test("a1") || p.Test("abc") {
		t.Error("Test wrong")
	}
	if got := p.Find("foo123bar"); got == nil || *got != "123" {
		t.Errorf("Find: got %v", got)
	}
	if got := p.Find("nodigits"); got != nil {
		t.Errorf("Find no-match should be nil, got %v", *got)
	}
	if got := p.FindAll("a1b22c333"); !reflect.DeepEqual(got, []string{"1", "22", "333"}) {
		t.Errorf("FindAll: got %v", got)
	}
	if got := p.Split("a1b2c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("Split: got %v", got)
	}
}

// Find must return an empty (non-nil) string when the pattern matches empty,
// matching the interpreter's reFindCore (null only when nothing matches).
func TestRePatternFindEmptyMatch(t *testing.T) {
	p := ReCompile("a*")
	got := p.Find("bbb")
	if got == nil || *got != "" {
		t.Errorf("empty match should be \"\", got %v", got)
	}
}

func TestRePatternMatchDict(t *testing.T) {
	p := ReCompile("(?P<first>\\w+)-(?P<second>\\w+)")
	m := p.Match("alpha-beta")
	if m == nil {
		t.Fatal("expected a match")
	}
	want := `{"text": "alpha-beta", "span": [0, 10], "groups": ["alpha-beta", "alpha", "beta"], "spans": [[0, 10], [0, 5], [6, 10]], "named": {"first": "alpha", "second": "beta"}, "namedSpans": {"first": [0, 5], "second": [6, 10]}}`
	if got := Show(m); got != want {
		t.Errorf("Match render: %s", got)
	}
	if p.Match("nodash") != nil {
		t.Error("no-match Match should be nil")
	}
	all := p.MatchAll("a-b c-d")
	if len(all) != 2 {
		t.Fatalf("MatchAll: got %d", len(all))
	}
	if got := Show(all[1].mustGet("span")); got != "[4, 7]" {
		t.Errorf("MatchAll span: %s", got)
	}
}

// Spans are rune offsets: the two-byte o-umlaut must count as one character.
func TestRePatternMatchSpansMultibyte(t *testing.T) {
	p := ReCompile("w\\S+")
	all := p.MatchAll("héllo wörld wörld")
	if len(all) != 2 {
		t.Fatalf("MatchAll: got %d", len(all))
	}
	if got := Show(all[0].mustGet("span")); got != "[6, 11]" {
		t.Errorf("first span: %s", got)
	}
	if got := Show(all[1].mustGet("span")); got != "[12, 17]" {
		t.Errorf("second span: %s", got)
	}
}

// An unmatched optional group keeps "" in groups and null in spans.
func TestRePatternMatchUnmatchedGroupSpans(t *testing.T) {
	p := ReCompile("(a)(b)?")
	m := p.Match("ac")
	if m == nil {
		t.Fatal("expected a match")
	}
	if got := Show(m.mustGet("spans")); got != "[[0, 1], [0, 1], null]" {
		t.Errorf("spans: %s", got)
	}
	if got := Show(m.mustGet("groups")); got != `["a", "a", ""]` {
		t.Errorf("groups: %s", got)
	}
}

func TestInvalidPatternPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on invalid pattern")
		}
		if e, ok := r.(*Error); !ok || e.Class != "RuntimeError" {
			t.Fatalf("expected *Error RuntimeError, got %v", r)
		}
	}()
	ReCompile("(")
}
