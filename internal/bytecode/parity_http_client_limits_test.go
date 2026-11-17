package bytecode_test

import "testing"

// HTTP client calls had no default timeout and buffered any response size.
func TestParityHTTPClientResponseCapAndTimeout(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
let s = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> {
    if (r["path"] == "/huge") { return {"status": 200, "body": "h".repeat(64 * 1024 * 1024 + 1)}; }
    if (r["path"] == "/slow") { await async.sleep(1500); return {"status": 200, "body": "late"}; }
    return {"status": 200, "body": "x".repeat(200)};
});
let base = "http://${http.serverAddr(s)}";
func attempt(string label, callable f): void {
    try { io.println("${label}: ok ${f()}"); } catch (IOError e) { io.println("${label}: ${typeof(e)}" + (e instanceof TimeoutError ? "" : " " + e.message)); }
}
attempt("get huge", func(): any { return (http.get(base + "/huge")["body"] as string).length(); });
attempt("request huge unlimited", func(): any { return (http.requestWithOptions({"url": base + "/huge", "maxResponseBytes": 0})["body"] as string).length(); });
attempt("request capped at 100", func(): any { return (http.requestWithOptions({"url": base + "/", "maxResponseBytes": 100})["body"] as string).length(); });
let c = http.newClient({"maxResponseBytes": 100});
attempt("client capped", func(): any { return (c.get(base + "/")["body"] as string).length(); });
attempt("client per-request override", func(): any { return (c.request({"url": base + "/", "maxResponseBytes": 1000})["body"] as string).length(); });
attempt("request timeout", func(): any { return http.requestWithOptions({"url": base + "/slow", "timeoutMs": 300})["status"]; });
http.close(s);
`)
	want := "get huge: IOError http response body exceeds 67108864 bytes\nrequest huge unlimited: ok 67108865\nrequest capped at 100: IOError http response body exceeds 100 bytes\nclient capped: IOError http response body exceeds 100 bytes\nclient per-request override: ok 200\nrequest timeout: TimeoutError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
