package bytecode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const asyncGlobalsBody = `int calls = 0;
func bump(): int { calls = calls + 1; return calls; }
class C {
    func hit(): int { calls = calls + 1; return calls; }
    async func ahit(): int { calls = calls + 1; return calls; }
    func gen(): any { calls = calls + 1; yield calls; calls = calls + 1; yield calls; }
}
class D extends C {}
async func af(): int { calls = calls + 1; return calls; }
func g(): any { calls = calls + 1; yield calls; }
export func run(): string {
    list<string> out = [];
    async.await(async.run(func(): int { calls = calls + 1; return calls; }));
    out.push("task ${calls}");
    async.await(async.run(func(): int { return bump(); }));
    out.push("task calls fn ${calls}");
    await af();
    out.push("async fn ${calls}");
    await C().ahit();
    out.push("async method ${calls}");
    await D().ahit();
    out.push("inherited async method ${calls}");
    for (v in g()) { }
    out.push("generator ${calls}");
    let mg = D().gen;
    for (v in mg()) { }
    out.push("generator method value ${calls}");
    let pending = async.run(func(): int { return C().hit(); });
    async.await(pending);
    out.push("task method ${calls}");
    return out.join(",");
}
`

const asyncGlobalsWant = "task 1,task calls fn 2,async fn 3,async method 4,inherited async method 5,generator 6,generator method value 8,task method 9\n"

// Async tasks, async functions/methods and generators write their module-global assignments back when they finish.
func TestParityAsyncAndGeneratorGlobalWriteBack(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, "import io;\nimport async;\n"+
		strings.Replace(asyncGlobalsBody, "export func run", "func run", 1)+"io.println(run());\n")
	if ev != asyncGlobalsWant || vm != asyncGlobalsWant {
		t.Fatalf("entry script: evaluator=%q VM=%q want %q", ev, vm, asyncGlobalsWant)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agw.gb"), []byte("module agw;\nimport async;\n"+asyncGlobalsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm = parityOutputsModulesDir(t, dir, "import io;\nimport agw;\nio.println(agw.run());\nio.println(agw.run());\n")
	wantTwice := asyncGlobalsWant + "task 10,task calls fn 11,async fn 12,async method 13,inherited async method 14,generator 15,generator method value 17,task method 18\n"
	if ev != wantTwice || vm != wantTwice {
		t.Fatalf("named module: evaluator=%q VM=%q want %q", ev, vm, wantTwice)
	}
}

// A task awaited inside an isolated handler keeps its writes to that request.
func TestParityAsyncGlobalWriteBackStaysInIsolatedHandler(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
int calls = 0;
let server = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> {
    async.await(async.run(func(): int { calls = calls + 1; return calls; }));
    return {"status": 200, "body": "${calls}"};
});
let first = http.get("http://${http.serverAddr(server)}/")["body"];
let second = http.get("http://${http.serverAddr(server)}/")["body"];
http.close(server);
io.println("bodies ${first} ${second} global=${calls}");
`)
	want := "bodies 1 1 global=0\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
