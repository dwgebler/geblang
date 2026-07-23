package native

import (
	"fmt"
	"geblang/internal/runtime"
	"regexp"
	"sync"
	"sync/atomic"
)

func matchDictPut(d runtime.Dict, key string, v runtime.Value) {
	k := runtime.String{Value: key}
	d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: v})
}

func spanListValue(start, end int) runtime.Value {
	if start < 0 {
		return runtime.Null{}
	}
	return &runtime.List{Elements: []runtime.Value{
		runtime.SmallInt{Value: int64(start)},
		runtime.SmallInt{Value: int64(end)},
	}}
}

// reMatchDict builds the match dict (text/span/groups/spans/named/namedSpans) from byte-offset index pairs; spans are rune offsets, end-exclusive.
func reMatchDict(re *regexp.Regexp, text string, idx []int, ri *runtime.RuneInfo) runtime.Dict {
	n := len(idx) / 2
	groupsElems := make([]runtime.Value, n)
	starts := make([]int, n)
	ends := make([]int, n)
	for i := 0; i < n; i++ {
		from, to := idx[2*i], idx[2*i+1]
		if from < 0 {
			groupsElems[i] = runtime.String{Value: ""}
			starts[i], ends[i] = -1, -1
			continue
		}
		groupsElems[i] = runtime.String{Value: text[from:to]}
		starts[i] = ri.RuneIndexOfByte(text, from)
		ends[i] = ri.RuneIndexOfByte(text, to)
	}

	spansElems := make([]runtime.Value, n)
	for i := 0; i < n; i++ {
		spansElems[i] = spanListValue(starts[i], ends[i])
	}

	named := runtime.NewDict()
	namedSpans := runtime.NewDict()
	for i, name := range re.SubexpNames() {
		if name == "" || i >= n {
			continue
		}
		nameKey := runtime.String{Value: name}
		named.PutEntry(DictKey(nameKey), runtime.DictEntry{Key: nameKey, Value: groupsElems[i]})
		namedSpans.PutEntry(DictKey(nameKey), runtime.DictEntry{Key: nameKey, Value: spanListValue(starts[i], ends[i])})
	}

	dict := runtime.NewDictHint(6)
	matchDictPut(dict, "text", groupsElems[0])
	matchDictPut(dict, "span", spanListValue(starts[0], ends[0]))
	matchDictPut(dict, "groups", &runtime.List{Elements: groupsElems})
	matchDictPut(dict, "spans", &runtime.List{Elements: spansElems})
	matchDictPut(dict, "named", named)
	matchDictPut(dict, "namedSpans", namedSpans)
	return dict
}

var (
	regexCache        atomic.Pointer[map[string]*regexp.Regexp]
	regexCacheWriteMu sync.Mutex
	// regexLastHit short-circuits the map (and the per-call string
	// hash) for the dominant pattern-reuse-in-a-loop shape.
	regexLastHit atomic.Pointer[regexCacheEntry]
)

type regexCacheEntry struct {
	pattern string
	re      *regexp.Regexp
}

const regexCacheMaxEntries = 256

func init() {
	empty := map[string]*regexp.Regexp{}
	regexCache.Store(&empty)
}

// CompileCachedRegex exposes the package-internal regex cache to other
// packages that want the same compile-once behaviour.
func CompileCachedRegex(pattern string) (*regexp.Regexp, error) {
	return compileCachedRegex(pattern)
}

func compileCachedRegex(pattern string) (*regexp.Regexp, error) {
	if last := regexLastHit.Load(); last != nil && last.pattern == pattern {
		return last.re, nil
	}
	current := *regexCache.Load()
	if re, ok := current[pattern]; ok {
		regexLastHit.Store(&regexCacheEntry{pattern: pattern, re: re})
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCacheWriteMu.Lock()
	defer regexCacheWriteMu.Unlock()
	current = *regexCache.Load()
	if existing, ok := current[pattern]; ok {
		return existing, nil
	}
	next := make(map[string]*regexp.Regexp, len(current)+1)
	if len(current) < regexCacheMaxEntries {
		for k, v := range current {
			next[k] = v
		}
	}
	next[pattern] = re
	regexCache.Store(&next)
	regexLastHit.Store(&regexCacheEntry{pattern: pattern, re: re})
	return re, nil
}

func registerRe(r *Registry) {
	twoStrings := func(args []runtime.Value, label string) (string, string, error) {
		if len(args) != 2 {
			return "", "", fmt.Errorf("%s expects two string arguments", label)
		}
		pattern, ok1 := args[0].(runtime.String)
		text, ok2 := args[1].(runtime.String)
		if !ok1 || !ok2 {
			return "", "", fmt.Errorf("%s arguments must be strings", label)
		}
		return pattern.Value, text.Value, nil
	}

	reFn := func(name string, body func(re *regexp.Regexp, text string) runtime.Value) {
		r.Register("re", name, func(args []runtime.Value) (runtime.Value, error) {
			pattern, text, err := twoStrings(args, "re."+name)
			if err != nil {
				return nil, err
			}
			re, err := compileCachedRegex(pattern)
			if err != nil {
				return nil, fmt.Errorf("re.%s: invalid pattern: %v", name, err)
			}
			return body(re, text), nil
		})
	}
	reFn("test", reTestCore)
	reFn("find", reFindCore)
	reFn("findAll", reFindAllCore)
	reFn("match", reMatchCore)
	reFn("matchAll", reMatchAllCore)
	reFn("split", reSplitCore)
	r.Register("re", "replace", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("re.replace expects (pattern, replacement, text)")
		}
		pattern, ok1 := args[0].(runtime.String)
		repl, ok2 := args[1].(runtime.String)
		text, ok3 := args[2].(runtime.String)
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("re.replace arguments must be strings")
		}
		re, err := compileCachedRegex(pattern.Value)
		if err != nil {
			return nil, fmt.Errorf("re.replace: invalid pattern: %v", err)
		}
		return reReplaceCore(re, repl.Value, text.Value), nil
	})
}

// re operation cores, shared by the module functions and the compiled
// Pattern handle. Each takes an already-compiled regex.
func reTestCore(re *regexp.Regexp, text string) runtime.Value {
	return runtime.Bool{Value: re.MatchString(text)}
}

func reFindCore(re *regexp.Regexp, text string) runtime.Value {
	match := re.FindString(text)
	if match == "" && !re.MatchString(text) {
		return runtime.Null{}
	}
	return runtime.String{Value: match}
}

func reFindAllCore(re *regexp.Regexp, text string) runtime.Value {
	matches := re.FindAllString(text, -1)
	elements := make([]runtime.Value, len(matches))
	for i, m := range matches {
		elements[i] = runtime.String{Value: m}
	}
	return &runtime.List{Elements: elements}
}

func reMatchCore(re *regexp.Regexp, text string) runtime.Value {
	idx := re.FindStringSubmatchIndex(text)
	if idx == nil {
		return runtime.Null{}
	}
	return reMatchDict(re, text, idx, runtime.StringRuneInfo(text))
}

func reMatchAllCore(re *regexp.Regexp, text string) runtime.Value {
	all := re.FindAllStringSubmatchIndex(text, -1)
	elements := make([]runtime.Value, 0, len(all))
	if len(all) > 0 {
		ri := runtime.StringRuneInfo(text)
		for _, idx := range all {
			elements = append(elements, reMatchDict(re, text, idx, ri))
		}
	}
	return &runtime.List{Elements: elements}
}

func reReplaceCore(re *regexp.Regexp, repl, text string) runtime.Value {
	return runtime.String{Value: re.ReplaceAllString(text, repl)}
}

func reSplitCore(re *regexp.Regexp, text string) runtime.Value {
	parts := re.Split(text, -1)
	elements := make([]runtime.Value, len(parts))
	for i, p := range parts {
		elements[i] = runtime.String{Value: p}
	}
	return &runtime.List{Elements: elements}
}
