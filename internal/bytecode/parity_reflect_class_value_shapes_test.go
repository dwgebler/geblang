package bytecode_test

// Probes reflect.* across class-value shapes (direct identifier, variable, function return, function argument); see reflectClassMetadata's canonicalClassValue rehydration.

import (
	"strings"
	"testing"
)

const reflectShapesClasses = `interface Greeter {
    func greet(): string;
}

/** Base class doc. */
@Deprecated
class Base implements Greeter {
    string name;
    func Base(string name) { this.name = name; }
    func greet(): string { return "hi"; }
    static func make(): Base { return Base("made"); }
}

/** Mid class doc. */
class Mid extends Base {
    int extraField;
    func Mid(string name, int extraField) {
        parent(name);
        this.extraField = extraField;
    }
    func extra(): string { return "extra"; }
    static func makeMid(): Mid { return Mid("mid", 1); }
}

func identity(any c): any { return c; }
func takesClass(any k): any { return k; }
`

const reflectShapesProbeTemplate = `let extraKey = "extra";
let makeMidKey = "makeMid";
io.println("methods:${reflect.methods(__C__)}");
io.println("staticMethods:${reflect.staticMethods(__C__)}");
io.println("interfaces:${reflect.interfaces(__C__)}");
io.println("parent:${reflect.parent(__C__)}");
io.println("className:${reflect.className(__C__)}");
io.println("fields:${reflect.fields(__C__)}");
io.println("docs:${reflect.docs(__C__)}");
io.println("location:${reflect.location(__C__)}");
io.println("decorators:${reflect.decorators(__C__)}");
io.println("methodFound:${reflect.method(__C__, extraKey) != null}");
io.println("staticMethodFound:${reflect.staticMethod(__C__, makeMidKey) != null}");
`

func reflectShapesProbeFor(receiver string) string {
	return strings.ReplaceAll(reflectShapesProbeTemplate, "__C__", receiver)
}

const reflectShapesWant = "methods:[\"extra\"]\n" +
	"staticMethods:[\"makeMid\"]\n" +
	"interfaces:[]\n" +
	"parent:Base\n" +
	"className:Mid\n" +
	"fields:[{\"decorators\": [], \"doc\": null, \"hasDefault\": false, \"name\": \"extraField\", \"nullable\": false, \"type\": \"int\"}]\n" +
	"docs:{\"body\": \"\", \"lines\": [\"Mid class doc.\"], \"summary\": \"Mid class doc.\", \"text\": \"Mid class doc.\"}\n" +
	"location:{\"column\": 1, \"line\": 17, \"module\": \"\"}\n" +
	"decorators:[]\n" +
	"methodFound:true\n" +
	"staticMethodFound:true\n"

// (a) a class identifier passed directly at the reflect call site - the historically-working shape.
func TestParityReflectClassValueShapeDirectIdentifier(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+reflectShapesClasses+reflectShapesProbeFor("Mid"), reflectShapesWant)
}

// (b) a class value held in a variable - the finding's exact repro shape.
func TestParityReflectClassValueShapeVariable(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+reflectShapesClasses+`
let c = Mid;
`+reflectShapesProbeFor("c"), reflectShapesWant)
}

// (c) a class value returned from a function.
func TestParityReflectClassValueShapeFunctionReturn(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+reflectShapesClasses+`
let c = identity(Mid);
`+reflectShapesProbeFor("c"), reflectShapesWant)
}

// (e) a class value passed as a function argument.
func TestParityReflectClassValueShapeFunctionArgument(t *testing.T) {
	runParity(t, "import io;\nimport reflect;\n"+reflectShapesClasses+`
func report(any c) {
`+reflectShapesProbeFor("c")+`}
report(takesClass(Mid));
`, reflectShapesWant)
}

// Two references to the same class through separate variables both rehydrate independently (no shared-state leak between call sites).
func TestParityReflectClassValueShapeTwoVariablesIndependent(t *testing.T) {
	runParity(t, `import io;
import reflect;
class Widget {
    func Widget() {}
    func spin(): void {}
}
let a = Widget;
let b = Widget;
io.println(reflect.methods(a));
io.println(reflect.methods(b));
`, "[\"spin\"]\n[\"spin\"]\n")
}
