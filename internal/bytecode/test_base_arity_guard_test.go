package bytecode_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

func TestTestBaseCatalogNamesAreHandledAtRuntime(t *testing.T) {
	for name := range native.TestBaseMethods {
		if name == "assertThrows" || name == "assertThrowsOf" {
			continue
		}
		if _, handled, _ := runtime.RunTestAssertion(strings.ToLower(name), nil); !handled {
			t.Errorf("TestBaseMethods lists %s but the runtime does not handle it", name)
		}
	}
}

// Every argument count the catalog rejects for a test.Test method must fail with an argument-count error on both backends.
func TestTestBaseCatalogArityMatchesRuntime(t *testing.T) {
	names := make([]string, 0, len(native.TestBaseMethods))
	for name := range native.TestBaseMethods {
		if name != "skip" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("import io;\nimport test;\n\nclass Probe extends test.Test {}\n\nany p = Probe();\n")
	for _, name := range names {
		doc := native.TestBaseMethods[name]
		longest := doc.Params
		for _, shape := range doc.Alternates {
			if len(shape) > len(longest) {
				longest = shape
			}
		}
		for argc := 0; argc <= len(longest)+1; argc++ {
			if fits, _ := native.TestBaseArityFits(name, argc); fits {
				continue
			}
			for _, fill := range arityProbeExtras {
				args := make([]string, argc)
				for i := range args {
					if i < len(longest) {
						args[i] = arityProbeValue(longest[i])
					} else {
						args[i] = fill
					}
				}
				if argc > 0 {
					args[argc-1] = fill
				}
				fmt.Fprintf(&b, "try {\n    p.%s(%s);\n    io.println(\"%s accepts %d\");\n} catch (Error e) {\n    if (!e.message.contains(\"argument\") && !e.message.contains(\"expects (\")) {\n        io.println(\"%s accepts %d: \" + e.message);\n    }\n}\n",
					name, strings.Join(args, ", "), name, argc, name, argc)
			}
		}
	}
	ev, vm := parityOutputs(t, b.String())
	if ev != "" || vm != "" {
		t.Errorf("catalog rejects argument counts the runtime accepts; widen TestBaseMethods.\nevaluator:\n%svm:\n%s", ev, vm)
	}
}
