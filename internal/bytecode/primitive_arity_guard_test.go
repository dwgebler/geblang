package bytecode_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"geblang/internal/native"
)

var arityProbeReceivers = map[string][]string{
	"string":  {`"abc,def"`, `"12.5"`, `"7"`, `"true"`, `"a\nb"`},
	"int":     {`7`, `-3`, `0`},
	"float":   {`2.5f`, `-1.25f`},
	"decimal": {`2.5`, `-1.25`},
	"bool":    {`true`, `false`},
	"bytes":   {`"abc" as bytes`, `"" as bytes`},
	"list":    {`[3, 1, 2]`, `["b", "a"]`, `[[1, "a"], [2, "b"]]`, `[1.5, 2.5]`},
	"dict":    {`{"a": 1, "b": 2}`, `{"a": ["b"], "b": []}`},
	"set":     {`[1, 2, 3] as set`},
	"range":   {`0..5`},
}

const arityProbeCallable = `func(any ... xs): any { return xs.length() > 0 ? xs[0] : null; }`

var arityProbeExtras = []string{`1`, `"a"`, `true`, arityProbeCallable, `[1]`}

func arityProbeValue(param string) string {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(param), "..."))
	if len(fields) == 0 {
		return "1"
	}
	typ := strings.TrimPrefix(fields[0], "?")
	switch {
	case typ == "int":
		return "1"
	case typ == "float":
		return "1.5f"
	case typ == "decimal":
		return "1.5"
	case typ == "bool":
		return "true"
	case typ == "string":
		return `"a"`
	case typ == "bytes":
		return `("a" as bytes)`
	case strings.HasPrefix(typ, "callable"), strings.HasPrefix(typ, "func"), strings.HasPrefix(typ, "fn"):
		return arityProbeCallable
	case strings.HasPrefix(typ, "list"):
		return "[1]"
	case strings.HasPrefix(typ, "dict"):
		return `{"a": 1}`
	case strings.HasPrefix(typ, "set"):
		return "([1] as set)"
	}
	return "1"
}

func arityShapes(doc native.FunctionDoc) [][]string {
	return append([][]string{doc.Params}, doc.Alternates...)
}

// Every argument count the catalog rejects for a primitive method must also fail at run time on both backends.
func TestPrimitiveCatalogArityMatchesRuntime(t *testing.T) {
	types := make([]string, 0, len(native.CatalogPrimitiveMethods))
	for typ := range native.CatalogPrimitiveMethods {
		types = append(types, typ)
	}
	sort.Strings(types)
	for _, typ := range types {
		recvs, ok := arityProbeReceivers[typ]
		if !ok {
			t.Fatalf("no probe receiver for primitive type %s", typ)
		}
		methods := native.CatalogPrimitiveMethods[typ]
		names := make([]string, 0, len(methods))
		for name := range methods {
			names = append(names, name)
		}
		sort.Strings(names)
		var b strings.Builder
		b.WriteString("import io;\n\n")
		for _, name := range names {
			doc := methods[name]
			longest := doc.Params
			variadic := false
			for _, shape := range arityShapes(doc) {
				if len(shape) > len(longest) {
					longest = shape
				}
				if len(shape) > 0 && strings.HasPrefix(strings.TrimSpace(shape[len(shape)-1]), "...") {
					variadic = true
				}
			}
			for argc := 0; argc <= len(longest)+1; argc++ {
				if variadic && argc >= len(longest) {
					break
				}
				if fits, _ := native.PrimitiveArityFits(typ, name, argc); fits {
					continue
				}
				candidates := []string{""}
				if argc > 0 {
					candidates = arityProbeExtras
					if argc <= len(longest) {
						candidates = append([]string{arityProbeValue(longest[argc-1])}, candidates...)
					}
				}
				for _, last := range candidates {
					args := make([]string, argc)
					for i := range args {
						switch {
						case i == argc-1:
							args[i] = last
						case i < len(longest):
							args[i] = arityProbeValue(longest[i])
						default:
							args[i] = last
						}
					}
					for _, recv := range recvs {
						fmt.Fprintf(&b, "try {\n    any r = %s;\n    r.%s(%s);\n    io.println(\"%s.%s accepts %d argument(s) on %s\");\n} catch (Error e) {}\n",
							recv, name, strings.Join(args, ", "), typ, name, argc, strings.ReplaceAll(recv, `"`, `'`))
					}
				}
			}
		}
		ev, vm := parityOutputs(t, b.String())
		if ev != "" || vm != "" {
			t.Errorf("%s: catalog rejects argument counts the runtime accepts; widen the catalog entry.\nevaluator:\n%svm:\n%s", typ, ev, vm)
		}
	}
}
