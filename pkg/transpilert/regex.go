package transpilert

import (
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"
)

// Regex bridge over Go's stdlib regexp (RE2), the same engine the interpreter's
// re module and string regex methods use, so results are byte-identical. An
// invalid pattern panics *Error{Class:"RuntimeError"} so the uncaught render and
// exit code match the interpreter's native-error path.

func compileRegex(pattern, label string) *regexp.Regexp {
	re, err := regexp.Compile(pattern)
	if err != nil {
		panic(NewError("RuntimeError", fmt.Sprintf("%s: invalid pattern: %v", label, err)))
	}
	return re
}

// StringMatchesRegex backs the string.matchesRegex method (re.test).
func StringMatchesRegex(text, pattern string) bool {
	return compileRegex(pattern, "re.test").MatchString(text)
}

// StringSplitRegex backs the string.splitRegex method (re.split).
func StringSplitRegex(text, pattern string) []string {
	return compileRegex(pattern, "re.split").Split(text, -1)
}

// StringReplaceRegex backs the string.replaceRegex method (re.replace); the
// replacement honours Go's $1 / ${name} expansion, matching the interpreter.
func StringReplaceRegex(text, pattern, replacement string) string {
	return compileRegex(pattern, "re.replace").ReplaceAllString(text, replacement)
}

// ReReplace backs the re.replace free function: (pattern, replacement, text).
func ReReplace(pattern, replacement, text string) string {
	return compileRegex(pattern, "re.replace").ReplaceAllString(text, replacement)
}

// RePattern wraps a compiled RE2 regex; it backs re.compile(pattern) and its
// chained methods (test/find/findAll/match/matchAll/split/replace).
type RePattern struct{ re *regexp.Regexp }

// ReCompile backs re.compile(pattern).
func ReCompile(pattern string) *RePattern {
	return &RePattern{re: compileRegex(pattern, "re.compile")}
}

func (p *RePattern) Test(text string) bool { return p.re.MatchString(text) }

// Find returns the leftmost match, or nil (Geblang null) when there is none.
// A nullable string lowers to *string in --native, so nil represents null.
func (p *RePattern) Find(text string) *string {
	loc := p.re.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	s := text[loc[0]:loc[1]]
	return &s
}

func (p *RePattern) FindAll(text string) []string {
	matches := p.re.FindAllString(text, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

func (p *RePattern) Split(text string) []string { return p.re.Split(text, -1) }

func (p *RePattern) Replace(replacement, text string) string {
	return p.re.ReplaceAllString(text, replacement)
}

// Match returns the match dict, or nil (Geblang null) when there is no match.
func (p *RePattern) Match(text string) *OrderedDict[string, any] {
	idx := p.re.FindStringSubmatchIndex(text)
	if idx == nil {
		return nil
	}
	return p.matchDict(text, idx, newByteRuneOffsets(text))
}

func (p *RePattern) MatchAll(text string) []*OrderedDict[string, any] {
	all := p.re.FindAllStringSubmatchIndex(text, -1)
	out := make([]*OrderedDict[string, any], 0, len(all))
	if len(all) > 0 {
		bro := newByteRuneOffsets(text)
		for _, idx := range all {
			out = append(out, p.matchDict(text, idx, bro))
		}
	}
	return out
}

// byteRuneOffsets maps byte offsets on rune boundaries to rune indexes; ASCII strings skip the table.
type byteRuneOffsets struct{ offsets []int }

func newByteRuneOffsets(s string) *byteRuneOffsets {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			offsets := make([]int, 0, utf8.RuneCountInString(s)+1)
			for b := range s {
				offsets = append(offsets, b)
			}
			offsets = append(offsets, len(s))
			return &byteRuneOffsets{offsets: offsets}
		}
	}
	return &byteRuneOffsets{}
}

func (o *byteRuneOffsets) runeIndex(b int) int64 {
	if o.offsets == nil {
		return int64(b)
	}
	return int64(sort.Search(len(o.offsets), func(i int) bool { return o.offsets[i] >= b }))
}

// matchDict mirrors the interpreter's match dict (text/span/groups/spans/named/namedSpans); spans are rune offsets, end-exclusive.
func (p *RePattern) matchDict(text string, idx []int, bro *byteRuneOffsets) *OrderedDict[string, any] {
	n := len(idx) / 2
	spanAt := func(i int) any {
		from, to := idx[2*i], idx[2*i+1]
		if from < 0 {
			return nil
		}
		return []int64{bro.runeIndex(from), bro.runeIndex(to)}
	}

	groups := make([]string, n)
	spans := make([]any, n)
	for i := 0; i < n; i++ {
		if idx[2*i] >= 0 {
			groups[i] = text[idx[2*i]:idx[2*i+1]]
		}
		spans[i] = spanAt(i)
	}

	named := NewOrderedDict[string, string]()
	namedSpans := NewOrderedDict[string, any]()
	for i, name := range p.re.SubexpNames() {
		if name == "" || i >= n {
			continue
		}
		named.Set(name, groups[i])
		namedSpans.Set(name, spanAt(i))
	}

	d := NewOrderedDict[string, any]()
	d.Set("text", groups[0])
	d.Set("span", spanAt(0))
	d.Set("groups", groups)
	d.Set("spans", spans)
	d.Set("named", named)
	d.Set("namedSpans", namedSpans)
	return d
}
