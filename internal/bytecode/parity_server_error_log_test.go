package bytecode_test

import "testing"

// The VM logged a server handler failure as its fully rendered trace wrapped in a second error class.
func TestParityServerHandlerErrorLogLine(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import net;
let cap = io.captureStderr();
let s = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> {
    if (r["path"] == "/throw") { throw ValueError("bad input"); }
    let m = {"a": 1};
    return {"status": 200, "body": "${m.nope()}"};
});
http.get("http://${http.serverAddr(s)}/throw");
http.get("http://${http.serverAddr(s)}/fault");
http.close(s);
let ns = net.serve("127.0.0.1", 0, func(dict<string, any> sock): void { throw RuntimeError("tcp handler failed"); });
let c = net.connectTcp(ns["localAddr"] as string);
net.setDeadline(c, 2000);
net.read(c, 1);
net.close(c);
net.closeListener(ns["handle"]);
let logged = cap.toString();
cap.close();
io.print(logged);
`)
	want := "http.serve: handler error: ValueError: bad input\nhttp.serve: handler error: RuntimeError: unknown method dict.nope\nnet.serve: handler error: RuntimeError: tcp handler failed\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
