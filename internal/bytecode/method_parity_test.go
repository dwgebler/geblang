package bytecode_test

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/native"
	"geblang/internal/parser"
	"testing"
)

// primitiveLiterals constructs a value of each primitive type. NOTE:
// range is the `0..3` literal; `range(0, 3)` is the list-materialising
// builtin, not a range.
var primitiveLiterals = map[string]string{
	"string":  `"abc"`,
	"list":    `[1, 2, 3]`,
	"dict":    `{"a": 1, "b": 2}`,
	"set":     `[1, 2, 3] as set`,
	"bytes":   `"abc" as bytes`,
	"range":   `0..3`,
	"int":     `42`,
	"decimal": `1.5`,
	"float":   `1.5f`,
	"bool":    `true`,
}

// TestPrimitiveMethodsRecognizedOnBothBackends asserts every method in
// native.PrimitiveMethods is recognised by BOTH the evaluator and the
// VM. The two backends dispatch primitive methods through independent
// switches; this is the guard that keeps them from diverging (the class
// of bug where `dir`/a method worked on one backend only).
func TestPrimitiveMethodsRecognizedOnBothBackends(t *testing.T) {
	for typeName, methods := range native.PrimitiveMethods {
		literal, ok := primitiveLiterals[typeName]
		if !ok {
			t.Fatalf("no test literal for primitive type %q", typeName)
		}
		for _, method := range methods {
			src := "let v = " + literal + ";\nv." + method + "();\n"
			if err := runOnEvaluator(src); isUnknownMethodErr(err) {
				t.Errorf("evaluator does not recognise %s.%s: %v", typeName, method, err)
			}
			if err := runOnVM(src); isUnknownMethodErr(err) {
				t.Errorf("VM does not recognise %s.%s: %v", typeName, method, err)
			}
		}
	}
}

func TestPrimitiveMethodNamesAreCaseSensitiveOnBothBackends(t *testing.T) {
	for typeName, methods := range native.PrimitiveMethods {
		literal, ok := primitiveLiterals[typeName]
		if !ok {
			t.Fatalf("no test literal for primitive type %q", typeName)
		}
		names := append([]string(nil), methods...)
		names = append(names, native.PrimitiveConversionMethods...)
		for _, method := range names {
			wrongCase := strings.ToUpper(method[:1]) + method[1:]
			src := "let v = " + literal + ";\nv." + wrongCase + "();\n"
			if err := runOnEvaluator(src); !isUnknownMethodErr(err) {
				t.Errorf("evaluator accepted wrong-case %s.%s: %v", typeName, wrongCase, err)
			}
			if err := runOnVM(src); !isUnknownMethodErr(err) {
				t.Errorf("VM accepted wrong-case %s.%s: %v", typeName, wrongCase, err)
			}
		}
	}
}

// bareBuiltinSnippets is a valid program exercising each bare builtin.
// Every native.BareBuiltins entry must have one (enforced below).
var bareBuiltinSnippets = map[string]string{
	"typeof": "let x = typeof(1);\n",
	"range":  "let x = range(0, 3);\n",
	"zrange": "let x = zrange(0, 3);\n",
	"assert": "assert(true);\n",
	"dir":    "let x = dir(1);\n",
	"dump":   "let x = dump(1);\n",
	"parent": "class A { func A() {} }\nclass B extends A { func B() { parent(); } }\nlet b = B();\n",
}

// TestBareBuiltinsRecognizedOnBothBackends asserts every canonical bare
// builtin runs on BOTH the evaluator and the VM. Catches the class of
// bug where a bare builtin is wired into one backend only (as dump and
// dir were).
func TestBareBuiltinsRecognizedOnBothBackends(t *testing.T) {
	for _, name := range native.BareBuiltins {
		snippet, ok := bareBuiltinSnippets[name]
		if !ok {
			t.Errorf("bare builtin %q has no guard snippet (add one to bareBuiltinSnippets)", name)
			continue
		}
		if err := runOnEvaluator(snippet); err != nil {
			t.Errorf("evaluator: bare builtin %q not recognised: %v", name, err)
		}
		if err := runOnVM(snippet); err != nil {
			t.Errorf("VM: bare builtin %q not recognised: %v", name, err)
		}
	}
}

// bareBuiltinCaseVariants: uppercased name must be rejected (case-sensitive dispatch).
var bareBuiltinCaseVariants = map[string]string{
	"assert": "Assert(true);\n",
	"dir":    "let x = Dir(1);\n",
	"dump":   "Dump(1);\n",
	"parent": "class A { func A() {} }\nclass B extends A { func B() { Parent(); } }\nlet b = B();\n",
	"range":  "let x = Range(1, 2);\n",
	"typeof": "let x = TypeOf(1);\n",
	"zrange": "let x = ZRange(3);\n",
}

func TestBareBuiltinsAreCaseSensitiveOnBothBackends(t *testing.T) {
	for _, name := range native.BareBuiltins {
		snippet, ok := bareBuiltinCaseVariants[name]
		if !ok {
			t.Errorf("bare builtin %q has no case-variant snippet", name)
			continue
		}
		if err := runOnEvaluator(snippet); err == nil {
			t.Errorf("evaluator: case variant of %q unexpectedly accepted", name)
		}
		if err := runOnVM(snippet); err == nil {
			t.Errorf("VM: case variant of %q unexpectedly accepted", name)
		}
	}
}

func TestParentSelectorCaseSensitiveOnBothBackends(t *testing.T) {
	snippet := "class A { func A() {} func greet(): string { return \"a\"; } }\nclass B extends A { func B() {} func greet(): string { return Parent.greet(); } }\nlet b = B();\nlet x = b.greet();\n"
	if err := runOnEvaluator(snippet); err == nil {
		t.Error("evaluator: Parent.greet() unexpectedly accepted")
	}
	if err := runOnVM(snippet); err == nil {
		t.Error("VM: Parent.greet() unexpectedly accepted")
	}
}

func runOnEvaluator(src string) error {
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("parse: %s", strings.Join(p.Errors(), "; "))
	}
	_, err := evaluator.New(io.Discard).Eval(program)
	return err
}

func runOnVM(src string) error {
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("parse: %s", strings.Join(p.Errors(), "; "))
	}
	chunk, err := bytecode.Compile(program, []byte(src), "guard")
	if err != nil {
		return err
	}
	return bytecode.NewVM(chunk, io.Discard).Run()
}

func isUnknownMethodErr(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "unknown method") ||
		strings.Contains(m, "has no method") ||
		strings.Contains(m, "unknown bytecode function") ||
		strings.Contains(m, "no method named")
}

// formatPrimitiveMethodList renders a primitive type's method names the
// way `dir` / `reflect.methods` print them (sorted, JSON-quoted list),
// so golden tests can derive their expected output from the registry
// instead of a frozen literal.
func formatPrimitiveMethodList(typeName string) string {
	names := append([]string(nil), native.PrimitiveMethods[typeName]...)
	sort.Strings(names)
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + n + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
