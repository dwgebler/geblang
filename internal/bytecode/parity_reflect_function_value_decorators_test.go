package bytecode_test

import "testing"

const funcValueDecoratorsProbe = `
func probe(string label, any f): void {
    try {
        io.println("${label}: ${reflect.decorators(f).length()} ${reflect.hasDecorator(f, "Tag")} ${reflect.decorator(f, "Tag")} ${reflect.decorators(f, "Tag").length()}");
    } catch (Error e) {
        io.println("${label}: ERR ${e.message}");
    }
}
`

// Function values reaching reflect through a parameter are closures on the VM; decorator reflection threw "expects reflect target" for all of them.
func TestParityReflectDecoratorsOnFunctionValues(t *testing.T) {
	src := `import io;
import reflect;
` + funcValueDecoratorsProbe + `
@Tag("a")
func tagged(int x): int { return x; }

@Tag("first")
func over(int x): int { return x; }
@Tag("second")
@Extra
func over(string x): string { return x; }

func wrap(callable inner): callable {
    return func(int x): int { return inner(x) as int; };
}

class K {
    @Tag("m")
    func m(): int { return 1; }
    func lit(): callable { return func(): int { return 2; }; }
}
class Sub extends K {}

any viaAny = tagged;
callable viaCallable = tagged;
probe("named", tagged);
probe("viaAny", viaAny);
probe("viaCallable", viaCallable);
probe("literal", func(int x): int { return x; });
probe("capturing", wrap(tagged));
probe("inList", [func(): int { return 1; }][0]);
probe("methodLiteral", K().lit());
probe("inheritedMethodLiteral", Sub().lit());
probe("boundMethod", reflect.method(K(), "m"));
probe("inheritedBoundMethod", reflect.method(Sub(), "m"));
probe("overloaded", over);
io.println(reflect.decorators(over));
probe("int", 5);
probe("null", null);
`
	ev, vm := parityOutputs(t, src)
	if ev != vm {
		t.Fatalf("evaluator/VM divergence\n--- evaluator ---\n%s\n--- vm ---\n%s", ev, vm)
	}
	want := `named: 1 true {"args": ["a"]`
	if len(ev) < len(want) || ev[:len(want)] != want {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}

func TestParityReflectDecoratorsOnCrossModuleFunctionValues(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"fvdonor": `module fvdonor;
@Tag("donor")
export func tagged(int x): int { return x; }
export func plain(int x): int { return x; }
export func literal(): callable { return func(int y): int { return y; }; }
export func capturing(int base): callable { return func(int y): int { return base + y; }; }
`,
	}, `import io;
import reflect;
import fvdonor;
func probe(string label, any f): void {
    io.println("${label}: ${reflect.decorators(f).length()} ${reflect.hasDecorator(f, "Tag")} ${reflect.decorator(f, "Tag") != null}");
}
probe("tagged", fvdonor.tagged);
probe("plain", fvdonor.plain);
probe("literal", fvdonor.literal());
probe("capturing", fvdonor.capturing(2));
`,
		"tagged: 1 true true\nplain: 0 false false\nliteral: 0 false false\ncapturing: 0 false false\n")
}

// An entry-script function value reflected inside another module resolved against that module's function table on the VM.
func TestParityReflectEntryFunctionValuesInOtherModule(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"fvinsp": `module fvinsp;
import reflect;
export func describe(any fn): string {
    list<string> names = [];
    for (p in reflect.parameters(fn)) { names.push((p as dict<string, any>)["name"] as string); }
    return "${reflect.decorators(fn).length()} ${reflect.hasDecorator(fn, "Tag")} ${names} ${reflect.returnType(fn)}";
}
export class Registry {
    list<any> items;
    func Registry() { this.items = []; }
    func add(any fn): void { this.items.push(fn); }
    func describeAll(): list<string> {
        list<string> out = [];
        for (fn in this.items) { out.push(describe(fn)); }
        return out;
    }
}
`,
	}, `import io;
import fvinsp;
@Tag("main")
func mine(string label, int count = 2): string { return label; }
func over(int x): int { return x; }
@Tag("ov")
func over(string y): string { return y; }
int base = 4;
let reg = fvinsp.Registry();
reg.add(mine);
reg.add(func(bool flag, list<int> xs): bool { return flag; });
reg.add(func(decimal amount): int { return base; });
reg.add(over);
for (line in reg.describeAll()) { io.println(line); }
io.println(fvinsp.describe(mine));
`,
		"1 true [\"label\", \"count\"] string\n0 false [\"flag\", \"xs\"] bool\n0 false [\"amount\"] int\n1 true [\"x\"] int\n1 true [\"label\", \"count\"] string\n")
}
