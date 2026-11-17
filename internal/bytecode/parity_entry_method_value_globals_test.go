package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const entryMethodValueDispatch = `module disp;
import reflect;
export func viaCallable(callable f): any {
    return f();
}
export func viaReflect(any obj, string name): any {
    let h = reflect.method(obj, name);
    return h();
}
export func reflected(any obj, string name): callable {
    return reflect.method(obj, name);
}
`

const entryMethodValueLib = `module mvlib;
export int libCalls = 0;
export class Counter {
    func hit(): int { libCalls = libCalls + 1; return libCalls; }
}
export func libCount(): int { return libCalls; }
`

// Bound method values from the entry script run on a loader worker; their global writes must reach the main VM.
func TestParityEntryMethodValueGlobalWrites(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{"disp.gb": entryMethodValueDispatch, "mvlib.gb": entryMethodValueLib} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import disp;
import mvlib;
int calls = 0;
class C {
    func hit(): int { calls = calls + 1; return calls; }
}
class D extends C {
    func own(): int { calls = calls + 10; return calls; }
}
class LocalCounter extends mvlib.Counter {}
let c = C();
let m = c.hit;
m();
io.println("same-module value: ${calls}");
m();
io.println("second call: ${calls}");
let d = D();
let inherited = d.hit;
let own = d.own;
inherited();
own();
io.println("subclass values: ${calls}");
io.println("cross-module call: ${disp.viaCallable(c.hit)} global=${calls}");
io.println("cross-module reflect: ${disp.viaReflect(c, "hit")} global=${calls}");
let h = disp.reflected(c, "hit");
io.println("foreign handle here: ${h()} global=${calls}");
io.println("foreign handle there: ${disp.viaCallable(h)} global=${calls}");
let lc = LocalCounter();
let lm = lc.hit;
lm();
lm();
io.println("imported class method value: ${mvlib.libCount()}");
`)
	want := "same-module value: 1\nsecond call: 2\nsubclass values: 13\ncross-module call: 14 global=14\ncross-module reflect: 15 global=15\nforeign handle here: 16 global=16\nforeign handle there: 17 global=17\nimported class method value: 2\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}

// A shareHandler handler's writes persist; an isolated handler's stay per request.
func TestParityEntryMethodValueGlobalWritesInHandlers(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
int calls = 0;
class C {
    func hit(): int { calls = calls + 1; return calls; }
}
let c = C();
let h = c.hit;
let shared = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> { return {"status": 200, "body": "${h()}"}; }, {"shareHandler": true});
let r1 = http.get("http://${http.serverAddr(shared)}/");
http.close(shared);
io.println("shared handler body=${r1["body"]} global=${calls}");
let isolated = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> { return {"status": 200, "body": "${h()}"}; });
let r2 = http.get("http://${http.serverAddr(isolated)}/");
http.close(isolated);
io.println("isolated handler body=${r2["body"]} global=${calls}");
`)
	want := "shared handler body=1 global=1\nisolated handler body=2 global=1\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}
