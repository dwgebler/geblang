package native

import (
	"fmt"
	"regexp"
	"strings"

	"geblang/internal/runtime"
)

const reSetKind = "ReSet"

// reSet runs every pattern in one combined scan; groups[i] is the capture group that wraps pattern i.
type reSet struct {
	combined *regexp.Regexp
	// any has no capture groups so the common no-match case stays on RE2's fast path.
	any    *regexp.Regexp
	groups []int
}

func compileReSet(patterns []string) (*reSet, error) {
	set := &reSet{groups: make([]int, len(patterns))}
	if len(patterns) == 0 {
		return set, nil
	}
	parts := make([]string, len(patterns))
	plain := make([]string, len(patterns))
	group := 1
	for i, p := range patterns {
		single, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern %d: %v", i, err)
		}
		set.groups[i] = group
		group += 1 + single.NumSubexp()
		parts[i] = "(" + p + ")"
		plain[i] = "(?:" + p + ")"
	}
	combined, err := regexp.Compile(strings.Join(parts, "|"))
	if err != nil {
		return nil, err
	}
	set.combined = combined
	if set.any, err = regexp.Compile(strings.Join(plain, "|")); err != nil {
		return nil, err
	}
	return set, nil
}

// match returns the index of the pattern behind the leftmost match in text, or -1.
func (s *reSet) match(text string) int {
	if s.combined == nil || !s.any.MatchString(text) {
		return -1
	}
	loc := s.combined.FindStringSubmatchIndex(text)
	if loc == nil {
		return -1
	}
	for i, g := range s.groups {
		if loc[2*g] >= 0 {
			return i
		}
	}
	return -1
}

func buildReSetClass() *runtime.Class {
	class := &runtime.Class{Name: "RegexSet", Module: "re", Fields: []runtime.Field{{Name: "patterns"}, {Name: "__handle"}}, Methods: map[string][]runtime.Function{}}
	textMethod := func(name string, result func(set *reSet, text string) runtime.Value) {
		class.Methods[strings.ToLower(name)] = []runtime.Function{{Name: name, Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("re.RegexSet.%s expects a text argument", name)
			}
			text, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("re.RegexSet.%s text must be a string", name)
			}
			payload, ok := patternPayload(this, reSetKind)
			if !ok {
				return nil, fmt.Errorf("re.RegexSet.%s: invalid set handle", name)
			}
			return result(payload.(*reSet), text.Value), nil
		}}}
	}
	textMethod("match", func(set *reSet, text string) runtime.Value { return runtime.NewInt64(int64(set.match(text))) })
	textMethod("test", func(set *reSet, text string) runtime.Value {
		return runtime.Bool{Value: set.any != nil && set.any.MatchString(text)}
	})
	class.Methods["size"] = []runtime.Function{{Name: "size", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		payload, ok := patternPayload(this, reSetKind)
		if !ok {
			return nil, fmt.Errorf("re.RegexSet.size: invalid set handle")
		}
		return runtime.NewInt64(int64(len(payload.(*reSet).groups))), nil
	}}}
	return class
}

var reSetClass = buildReSetClass()

func registerRegexSet(r *Registry) {
	class := reSetClass
	r.Register("re", "set", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("re.set expects a list of patterns")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("re.set expects a list of patterns")
		}
		patterns := make([]string, len(list.Elements))
		for i, element := range list.Elements {
			s, ok := element.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("re.set patterns must be strings")
			}
			patterns[i] = s.Value
		}
		set, err := compileReSet(patterns)
		if err != nil {
			return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("re.set: invalid pattern: %v", err)}
		}
		return &runtime.Instance{Class: class, Fields: map[string]runtime.Value{
			"patterns": &runtime.List{Elements: append([]runtime.Value(nil), list.Elements...)},
			"__handle": runtime.NativeObject{Kind: reSetKind, Payload: set},
		}}, nil
	})
}
