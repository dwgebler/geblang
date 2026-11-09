package native

import (
	"fmt"
	"strings"

	"regexp"

	"github.com/dlclark/regexp2"

	"geblang/internal/runtime"
)

// A Pattern instance holds its compiled regex as a NativeObject payload, so it is freed with the instance.
const (
	rePatternKind   = "RePattern"
	pcrePatternKind = "PcrePattern"
)

func patternPayload(this *runtime.Instance, kind string) (any, bool) {
	handle, ok := this.Fields["__handle"].(runtime.NativeObject)
	if !ok || handle.Kind != kind {
		return nil, false
	}
	return handle.Payload, handle.Payload != nil
}

func buildRePatternClass() *runtime.Class {
	class := &runtime.Class{
		Name:    "Pattern",
		Module:  "re",
		Fields:  []runtime.Field{{Name: "source"}, {Name: "__handle"}},
		Methods: map[string][]runtime.Function{},
	}
	method := func(name string, core func(re *reCompiled, text string) runtime.Value) {
		class.Methods[strings.ToLower(name)] = []runtime.Function{{
			Name: name,
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("re.Pattern.%s expects a text argument", name)
				}
				text, ok := args[0].(runtime.String)
				if !ok {
					return nil, fmt.Errorf("re.Pattern.%s text must be a string", name)
				}
				re, ok := lookupReHandle(this)
				if !ok {
					return nil, fmt.Errorf("re.Pattern.%s: invalid pattern handle", name)
				}
				return core(re, text.Value), nil
			},
		}}
	}
	method("test", func(re *reCompiled, t string) runtime.Value { return reTestCore(re.r, t) })
	method("find", func(re *reCompiled, t string) runtime.Value { return reFindCore(re.r, t) })
	method("findAll", func(re *reCompiled, t string) runtime.Value { return reFindAllCore(re.r, t) })
	method("match", func(re *reCompiled, t string) runtime.Value { return reMatchCore(re.r, t) })
	method("matchAll", func(re *reCompiled, t string) runtime.Value { return reMatchAllCore(re.r, t) })
	method("split", func(re *reCompiled, t string) runtime.Value { return reSplitCore(re.r, t) })
	// replace takes (replacement, text).
	class.Methods["replace"] = []runtime.Function{{
		Name: "replace",
		Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("re.Pattern.replace expects (replacement, text)")
			}
			repl, ok1 := args[0].(runtime.String)
			text, ok2 := args[1].(runtime.String)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("re.Pattern.replace arguments must be strings")
			}
			re, ok := lookupReHandle(this)
			if !ok {
				return nil, fmt.Errorf("re.Pattern.replace: invalid pattern handle")
			}
			return reReplaceCore(re.r, repl.Value, text.Value), nil
		},
	}}
	return class
}

func buildPcrePatternClass() *runtime.Class {
	class := &runtime.Class{
		Name:    "Pattern",
		Module:  "pcre",
		Fields:  []runtime.Field{{Name: "source"}, {Name: "flags"}, {Name: "__handle"}},
		Methods: map[string][]runtime.Function{},
	}
	method := func(name string, core func(re *regexp2.Regexp, text string) (runtime.Value, error)) {
		class.Methods[strings.ToLower(name)] = []runtime.Function{{
			Name: name,
			Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("pcre.Pattern.%s expects a text argument", name)
				}
				text, ok := args[0].(runtime.String)
				if !ok {
					return nil, fmt.Errorf("pcre.Pattern.%s text must be a string", name)
				}
				re, ok := lookupPcreHandle(this)
				if !ok {
					return nil, fmt.Errorf("pcre.Pattern.%s: invalid pattern handle", name)
				}
				return core(re, text.Value)
			},
		}}
	}
	method("test", pcreTestCore)
	method("find", pcreFindCore)
	method("findAll", pcreFindAllCore)
	method("match", pcreMatchCore)
	method("matchAll", pcreMatchAllCore)
	method("split", pcreSplitCore)
	class.Methods["replace"] = []runtime.Function{{
		Name: "replace",
		Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("pcre.Pattern.replace expects (replacement, text)")
			}
			repl, ok1 := args[0].(runtime.String)
			text, ok2 := args[1].(runtime.String)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("pcre.Pattern.replace arguments must be strings")
			}
			re, ok := lookupPcreHandle(this)
			if !ok {
				return nil, fmt.Errorf("pcre.Pattern.replace: invalid pattern handle")
			}
			return pcreReplaceCore(re, repl.Value, text.Value)
		},
	}}
	return class
}

type reCompiled struct{ r *regexp.Regexp }

func lookupReHandle(this *runtime.Instance) (*reCompiled, bool) {
	v, ok := patternPayload(this, rePatternKind)
	if !ok {
		return nil, false
	}
	re, ok := v.(*reCompiled)
	return re, ok
}

func lookupPcreHandle(this *runtime.Instance) (*regexp2.Regexp, bool) {
	v, ok := patternPayload(this, pcrePatternKind)
	if !ok {
		return nil, false
	}
	re, ok := v.(*regexp2.Regexp)
	return re, ok
}

func registerRegexCompile(r *Registry) {
	reClass := buildRePatternClass()
	pcreClass := buildPcrePatternClass()

	r.Register("re", "compile", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("re.compile expects a pattern string")
		}
		pattern, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("re.compile pattern must be a string")
		}
		compiled, err := compileCachedRegex(pattern.Value)
		if err != nil {
			return nil, fmt.Errorf("re.compile: invalid pattern: %v", err)
		}
		return &runtime.Instance{
			Class: reClass,
			Fields: map[string]runtime.Value{
				"source":   pattern,
				"__handle": runtime.NativeObject{Kind: rePatternKind, Payload: &reCompiled{r: compiled}},
			},
		}, nil
	})

	r.Register("pcre", "compile", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("pcre.compile expects a pattern string and optional flags")
		}
		pattern, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("pcre.compile pattern must be a string")
		}
		flags := runtime.String{Value: ""}
		if len(args) == 2 {
			f, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("pcre.compile flags must be a string")
			}
			flags = f
		}
		compiled, err := compileCachedPCRE(pattern.Value, flags.Value)
		if err != nil {
			return nil, fmt.Errorf("pcre.compile: %v", err)
		}
		return &runtime.Instance{
			Class: pcreClass,
			Fields: map[string]runtime.Value{
				"source":   pattern,
				"flags":    flags,
				"__handle": runtime.NativeObject{Kind: pcrePatternKind, Payload: compiled},
			},
		}, nil
	})
}
