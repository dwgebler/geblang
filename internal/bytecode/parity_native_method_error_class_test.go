package bytecode_test

import "testing"

// The VM raised RuntimeError for network failures from native methods, where the evaluator raised IOError / TimeoutError.
func TestParityNativeMethodNetworkErrorClasses(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
let slow = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> {
    await async.sleep(1000);
    return {"status": 200, "body": "late"};
});
let c = http.newClient({"timeoutMs": 200});
func classOf(callable f): string {
    try { f(); return "returned"; } catch (Error e) { return "${typeof(e)}"; }
}
io.println("client refused: " + classOf(func(): void { c.get("http://127.0.0.1:1/"); }));
io.println("builder refused: " + classOf(func(): void { http.request("http://127.0.0.1:1/").send(); }));
io.println("client timeout: " + classOf(func(): void { c.get("http://${http.serverAddr(slow)}/"); }));
io.println("timeout is an IOError: " + classOf(func(): void {
    try { c.get("http://${http.serverAddr(slow)}/"); } catch (IOError e) { throw ValueError("caught as IOError"); }
}));
http.close(slow);
`)
	want := "client refused: IOError\nbuilder refused: IOError\nclient timeout: TimeoutError\ntimeout is an IOError: ValueError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
