package bytecode_test

// Reflection and error surfaces keep a parameter's original case (9.44); the lowercased matching key must not leak out.

import (
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const paramCaseClasses = `class Widget {
    int extraField;
    func Widget(int extraField, string labelText = "x") {
        this.extraField = extraField;
    }
    func doThing(int firstArg, decimal secondArg): void {}
    static func makeIt(int seedValue): Widget { return Widget(seedValue); }
}

func topFn(int outerName, bool flagOn): void {}

func nameOf(any p): string { return (p as dict<string, any>)["name"] as string; }
`

const paramCaseProbe = `for (ctor in reflect.constructors(__W__)) {
    for (p in ctor as list<any>) { io.println("ctor:${nameOf(p)}"); }
}
for (p in reflect.parameters(reflect.method(__W__, "doThing"))) { io.println("method:${nameOf(p)}"); }
for (p in reflect.parameters(topFn)) { io.println("fn:${nameOf(p)}"); }
for (p in reflect.parameters(reflect.staticMethod(__W__, "makeIt"))) { io.println("static:${nameOf(p)}"); }
`

const paramCaseWant = "ctor:extraField\nctor:labelText\nmethod:firstArg\nmethod:secondArg\nfn:outerName\nfn:flagOn\nstatic:seedValue\n"

// Direct class identifier at the reflect call site.
func TestParityParamNameCaseDirect(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+paramCaseClasses+strings.ReplaceAll(paramCaseProbe, "__W__", "Widget"), paramCaseWant)
}

// Class value reached through a variable (the finding's repro shape).
func TestParityParamNameCaseVariable(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+paramCaseClasses+"let c = Widget;\n"+strings.ReplaceAll(paramCaseProbe, "__W__", "c"), paramCaseWant)
}

// A free function reaching reflect.parameters through an `any` binding (compiled to a closure value, not a direct reference).
func TestParityParamNameCaseFunctionValueThroughAny(t *testing.T) {
	runParity(t, `import io;
import reflect;
func topFn(int outerName, bool flagOn): void {}
func namesOf(any fn): void {
    for (p in reflect.parameters(fn)) { io.println((p as dict<string, any>)["name"] as string); }
}
namesOf(topFn);
`, "outerName\nflagOn\n")
}

// A type mismatch names the parameter in its original case on both backends.
func TestParityParamNameCaseTypeMismatchError(t *testing.T) {
	runParity(t, `import io;
func greet(string firstName): void { io.println(firstName); }
try { greet(5 as any); } catch (Error e) { io.println(e.message); }
`, "greet expects string for parameter 'firstName', got int\n")
}

// Cross-module reflection over a foreign class keeps declared param case.
func TestParityParamNameCaseCrossModule(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"paramdonor": `module paramdonor;
export class Gizmo {
    int coreValue;
    func Gizmo(int coreValue, string tagName = "t") { this.coreValue = coreValue; }
    static func assemble(int seedValue): Gizmo { return Gizmo(seedValue); }
}
`,
	}, `import io;
import reflect;
import paramdonor as pd;
func nameOf(any p): string { return (p as dict<string, any>)["name"] as string; }
for (ctor in reflect.constructors(pd.Gizmo)) {
    for (p in ctor as list<any>) { io.println("ctor:${nameOf(p)}"); }
}
for (p in reflect.parameters(reflect.staticMethod(pd.Gizmo, "assemble"))) { io.println("static:${nameOf(p)}"); }
`, "ctor:coreValue\nctor:tagName\nstatic:seedValue\n")
}

// The .gbc round-trip preserves original-case display names while keeping the lowercased matching key.
func TestParamDisplayNamesSurviveEncodeDecode(t *testing.T) {
	source := `class Widget {
    func Widget(int extraField, string labelText = "x") {}
    static func makeIt(int seedValue): void {}
}
func topFn(int outerName): void {}
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "paramcase")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	want := map[string]string{"extrafield": "extraField", "labeltext": "labelText", "seedvalue": "seedValue", "outername": "outerName"}
	seen := map[string]bool{}
	for _, fn := range decoded.Functions {
		for i, lower := range fn.ParamNames {
			display, ok := want[lower]
			if !ok {
				continue
			}
			if i >= len(fn.ParamDisplayNames) {
				t.Fatalf("function %q missing display name for param %d", fn.Name, i)
			}
			if fn.ParamDisplayNames[i] != display {
				t.Errorf("param %q: display name = %q, want %q", lower, fn.ParamDisplayNames[i], display)
			}
			if lower != strings.ToLower(display) {
				t.Errorf("matching key %q is not lowercased", lower)
			}
			seen[lower] = true
		}
	}
	for lower := range want {
		if !seen[lower] {
			t.Errorf("param %q not found in decoded chunk", lower)
		}
	}
}
