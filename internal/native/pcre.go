package native

// pcre is the PHP-compatible regex module, backed by the
// dlclark/regexp2 engine (a port of .NET's regex). It coexists
// with the built-in `re` module:
//
//   - `re` runs RE2 (`regexp`): linear time, no backreferences,
//     no lookarounds. Use for performance-critical paths and any
//     input that may be user-controlled (no catastrophic
//     backtracking).
//   - `pcre` runs a PCRE-style engine with lookahead /
//     lookbehind, backreferences, atomic groups, possessive
//     quantifiers, and named captures via `(?P<name>...)` /
//     `(?<name>...)`. Use when porting PHP code or when the
//     pattern needs PCRE-only features.
//
// The surface mirrors `re.*`: test, find, findAll, match,
// matchAll, replace, split, quote. Each function takes the
// pattern + text + an optional flags string (subset of PHP's
// modifier letters):
//
//	i  case-insensitive
//	m  multiline (^ and $ match line boundaries)
//	s  dotall (. matches newlines)
//	x  extended (ignore whitespace, allow # comments)
//	U  ungreedy (swap greedy/lazy for default quantifiers)

import (
	"fmt"
	"strings"
	"sync"

	"geblang/internal/runtime"

	"github.com/dlclark/regexp2"
)

// pcreCache memoises compiled patterns keyed by pattern+flags so
// tight loops calling pcre.test / pcre.match with the same pattern
// don't recompile.
var (
	pcreCacheMu sync.Mutex
	pcreCache   = map[string]*regexp2.Regexp{}
)

const pcreCacheMaxEntries = 256

func compileCachedPCRE(pattern, flags string) (*regexp2.Regexp, error) {
	opts, err := parsePCREFlags(flags)
	if err != nil {
		return nil, err
	}
	key := pattern + "\x00" + flags
	pcreCacheMu.Lock()
	re, ok := pcreCache[key]
	pcreCacheMu.Unlock()
	if ok {
		return re, nil
	}
	re, err = regexp2.Compile(translatePCREPattern(pattern), opts)
	if err != nil {
		return nil, err
	}
	pcreCacheMu.Lock()
	if len(pcreCache) >= pcreCacheMaxEntries {
		for k := range pcreCache {
			delete(pcreCache, k)
			break
		}
	}
	pcreCache[key] = re
	pcreCacheMu.Unlock()
	return re, nil
}

// parsePCREFlags maps the PHP-style modifier letters to
// regexp2.RegexOptions. Unknown letters are an error so typos
// don't get silently dropped.
func parsePCREFlags(flags string) (regexp2.RegexOptions, error) {
	var opts regexp2.RegexOptions
	for _, c := range flags {
		switch c {
		case 'i':
			opts |= regexp2.IgnoreCase
		case 'm':
			opts |= regexp2.Multiline
		case 's':
			opts |= regexp2.Singleline
		case 'x':
			opts |= regexp2.IgnorePatternWhitespace
		case 'U':
			// PHP's "U" inverts greediness. regexp2 doesn't have
			// a direct equivalent; flagging until we either map
			// it via inline (?U) or document the gap.
			return 0, fmt.Errorf("flag 'U' (ungreedy) is not supported by the regexp2 backend; use lazy quantifiers (*?, +?) explicitly")
		default:
			return 0, fmt.Errorf("unknown pcre flag %q (supported: imsx)", string(c))
		}
	}
	return opts, nil
}

// pcreArgs pulls (pattern, text, flags) out of the argument list.
// flags is optional.
func pcreArgs(args []runtime.Value, label string, minN, maxN int) (string, string, string, error) {
	if len(args) < minN || len(args) > maxN {
		return "", "", "", fmt.Errorf("%s expects %d-%d arguments (pattern, text[, flags]), got %d", label, minN, maxN, len(args))
	}
	pattern, ok1 := args[0].(runtime.String)
	text, ok2 := args[1].(runtime.String)
	if !ok1 || !ok2 {
		return "", "", "", fmt.Errorf("%s: pattern and text must be strings", label)
	}
	flags := ""
	if len(args) >= 3 {
		f, ok := args[2].(runtime.String)
		if !ok {
			return "", "", "", fmt.Errorf("%s: flags must be a string", label)
		}
		flags = f.Value
	}
	return pattern.Value, text.Value, flags, nil
}

// pcreMatchToDict builds the match dict (text/span/groups/spans/named/namedSpans); regexp2 indexes are already rune offsets.
func pcreMatchToDict(m *regexp2.Match) runtime.Dict {
	groups := m.Groups()
	n := len(groups)
	groupsElems := make([]runtime.Value, n)
	starts := make([]int, n)
	ends := make([]int, n)
	for i := range groups {
		g := &groups[i]
		groupsElems[i] = runtime.String{Value: g.String()}
		if len(g.Captures) == 0 {
			starts[i], ends[i] = -1, -1
			continue
		}
		starts[i], ends[i] = g.Index, g.Index+g.Length
	}

	spansElems := make([]runtime.Value, n)
	for i := 0; i < n; i++ {
		spansElems[i] = spanListValue(starts[i], ends[i])
	}

	named := runtime.NewDict()
	namedSpans := runtime.NewDict()
	for i := range groups {
		if i == 0 {
			continue
		}
		name := groups[i].Name
		if name == "" || name == fmt.Sprintf("%d", i) {
			continue
		}
		nameKey := runtime.String{Value: name}
		named.PutEntry(DictKey(nameKey), runtime.DictEntry{Key: nameKey, Value: groupsElems[i]})
		namedSpans.PutEntry(DictKey(nameKey), runtime.DictEntry{Key: nameKey, Value: spanListValue(starts[i], ends[i])})
	}

	dict := runtime.NewDictHint(6)
	matchDictPut(dict, "text", runtime.String{Value: m.String()})
	matchDictPut(dict, "span", spanListValue(starts[0], ends[0]))
	matchDictPut(dict, "groups", &runtime.List{Elements: groupsElems})
	matchDictPut(dict, "spans", &runtime.List{Elements: spansElems})
	matchDictPut(dict, "named", named)
	matchDictPut(dict, "namedSpans", namedSpans)
	return dict
}

// translatePCREPattern rewrites PHP/Python named-group syntax to
// the .NET form regexp2 expects, so patterns copied verbatim from
// PHP code work without manual edits:
//
//	(?P<name>...)  -> (?<name>...)
//	(?P=name)      -> \k<name>
//
// Other PCRE syntax (lookarounds, possessive quantifiers,
// backrefs by number, named captures via the .NET `(?<n>...)`
// form) is passed through unchanged.
func translatePCREPattern(p string) string {
	if !strings.Contains(p, "(?P") {
		return p
	}
	var b strings.Builder
	b.Grow(len(p))
	for i := 0; i < len(p); i++ {
		if i+2 < len(p) && p[i] == '(' && p[i+1] == '?' && p[i+2] == 'P' {
			if i+3 < len(p) && p[i+3] == '<' {
				b.WriteString("(?<")
				i += 3
				continue
			}
			if i+3 < len(p) && p[i+3] == '=' {
				end := strings.IndexByte(p[i+4:], ')')
				if end >= 0 {
					b.WriteString(`\k<`)
					b.WriteString(p[i+4 : i+4+end])
					b.WriteByte('>')
					i = i + 4 + end
					continue
				}
			}
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

// pcreSplit emulates the missing regexp2.Regexp.Split by walking
// the matches and slicing the input at each match's runes range.
// Empty input still yields a single empty string, matching
// strings.Split semantics.
func pcreSplit(re *regexp2.Regexp, text string) ([]string, error) {
	runes := []rune(text)
	var out []string
	last := 0
	m, err := re.FindRunesMatch(runes)
	if err != nil {
		return nil, err
	}
	for m != nil {
		idx := m.Index
		out = append(out, string(runes[last:idx]))
		last = idx + m.Length
		m, err = re.FindNextMatch(m)
		if err != nil {
			return nil, err
		}
	}
	out = append(out, string(runes[last:]))
	return out, nil
}

// pcreQuoteSpecial escapes regex metacharacters in a literal
// string. Matches PHP preg_quote semantics (no second-arg
// delimiter list since we don't use PHP-style pattern delimiters).
func pcreQuoteSpecial(s string) string {
	specials := `.\+*?[^]$(){}=!<>|:-#`
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	for _, c := range s {
		if strings.ContainsRune(specials, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func registerPCRE(r *Registry) {
	pcreFn := func(name string, body func(re *regexp2.Regexp, text string) (runtime.Value, error)) {
		r.Register("pcre", name, func(args []runtime.Value) (runtime.Value, error) {
			pattern, text, flags, err := pcreArgs(args, "pcre."+name, 2, 3)
			if err != nil {
				return nil, err
			}
			re, err := compileCachedPCRE(pattern, flags)
			if err != nil {
				return nil, fmt.Errorf("pcre.%s: %v", name, err)
			}
			return body(re, text)
		})
	}
	pcreFn("test", pcreTestCore)

	pcreFn("find", pcreFindCore)
	pcreFn("findAll", pcreFindAllCore)
	pcreFn("match", pcreMatchCore)
	pcreFn("matchAll", pcreMatchAllCore)

	r.Register("pcre", "replace", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 3 || len(args) > 4 {
			return nil, fmt.Errorf("pcre.replace expects (pattern, replacement, text[, flags])")
		}
		pattern, ok1 := args[0].(runtime.String)
		repl, ok2 := args[1].(runtime.String)
		text, ok3 := args[2].(runtime.String)
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("pcre.replace: pattern, replacement, text must be strings")
		}
		flags := ""
		if len(args) == 4 {
			f, ok := args[3].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("pcre.replace: flags must be a string")
			}
			flags = f.Value
		}
		re, err := compileCachedPCRE(pattern.Value, flags)
		if err != nil {
			return nil, fmt.Errorf("pcre.replace: %v", err)
		}
		return pcreReplaceCore(re, repl.Value, text.Value)
	})

	pcreFn("split", pcreSplitCore)

	r.Register("pcre", "quote", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("pcre.quote expects (text)")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("pcre.quote: argument must be a string")
		}
		return runtime.String{Value: pcreQuoteSpecial(text.Value)}, nil
	})
}

// pcre operation cores, shared by the module functions and the
// compiled Pattern handle. Each takes an already-compiled regexp2.
func pcreTestCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	ok, err := re.MatchString(text)
	if err != nil {
		return nil, fmt.Errorf("pcre.test: %v", err)
	}
	return runtime.Bool{Value: ok}, nil
}

func pcreFindCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	m, err := re.FindStringMatch(text)
	if err != nil {
		return nil, fmt.Errorf("pcre.find: %v", err)
	}
	if m == nil {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: m.String()}, nil
}

func pcreFindAllCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	var out []runtime.Value
	m, err := re.FindStringMatch(text)
	if err != nil {
		return nil, fmt.Errorf("pcre.findAll: %v", err)
	}
	for m != nil {
		out = append(out, runtime.String{Value: m.String()})
		m, err = re.FindNextMatch(m)
		if err != nil {
			return nil, fmt.Errorf("pcre.findAll: %v", err)
		}
	}
	return &runtime.List{Elements: out}, nil
}

func pcreMatchCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	m, err := re.FindStringMatch(text)
	if err != nil {
		return nil, fmt.Errorf("pcre.match: %v", err)
	}
	if m == nil {
		return runtime.Null{}, nil
	}
	return pcreMatchToDict(m), nil
}

func pcreMatchAllCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	var out []runtime.Value
	m, err := re.FindStringMatch(text)
	if err != nil {
		return nil, fmt.Errorf("pcre.matchAll: %v", err)
	}
	for m != nil {
		out = append(out, pcreMatchToDict(m))
		m, err = re.FindNextMatch(m)
		if err != nil {
			return nil, fmt.Errorf("pcre.matchAll: %v", err)
		}
	}
	return &runtime.List{Elements: out}, nil
}

func pcreReplaceCore(re *regexp2.Regexp, repl, text string) (runtime.Value, error) {
	result, err := re.Replace(text, repl, -1, -1)
	if err != nil {
		return nil, fmt.Errorf("pcre.replace: %v", err)
	}
	return runtime.String{Value: result}, nil
}

func pcreSplitCore(re *regexp2.Regexp, text string) (runtime.Value, error) {
	parts, err := pcreSplit(re, text)
	if err != nil {
		return nil, fmt.Errorf("pcre.split: %v", err)
	}
	elements := make([]runtime.Value, len(parts))
	for i, p := range parts {
		elements[i] = runtime.String{Value: p}
	}
	return &runtime.List{Elements: elements}, nil
}
