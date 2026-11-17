package bytecode_test

import "testing"

// The VM kept handles opened during a request open after it ended; the evaluator closes them with the request scope.
func TestParityRequestScopedHandles(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import net;
import async;
import async.channel as ch;

let handles = ch.Channel<any>(4);
int requests = 0;
let topLevel = net.listenTcp("127.0.0.1:0");

func stillOpen(any h): string {
    try { net.localAddr(h); return "open"; } catch (Error e) { return "closed"; }
}

let shared = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    requests = requests + 1;
    handles.send(net.listenTcp("127.0.0.1:0"));
    let fromTask = async.await(async.run(func(): any { return net.listenTcp("127.0.0.1:0"); }));
    handles.send(fromTask);
    return {"status": 200, "body": "ok"};
}, {"shareHandler": true});
http.get("http://${http.serverAddr(shared)}/");
http.get("http://${http.serverAddr(shared)}/");
io.println("request handle: " + stillOpen(handles.recv()));
io.println("task handle: " + stillOpen(handles.recv()));
handles.recv();
handles.recv();
io.println("shareHandler global writes persisted: ${requests}");
http.close(shared);

let isolated = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    handles.send(net.listenTcp("127.0.0.1:0"));
    return {"status": 200, "body": "ok"};
});
http.get("http://${http.serverAddr(isolated)}/");
io.println("isolated handler handle: " + stillOpen(handles.recv()));
http.close(isolated);

let tcp = net.serve("127.0.0.1", 0, func(dict<string, any> sock): void {
    handles.send(net.listenTcp("127.0.0.1:0"));
});
let c = net.connectTcp(tcp["localAddr"] as string);
let fromConn = handles.recv();
net.setDeadline(c, 1000);
net.read(c, 1);
net.close(c);
await async.sleep(50);
io.println("net.serve handle: " + stillOpen(fromConn));
net.closeListener(tcp["handle"]);
io.println("top-level handle: " + stillOpen(topLevel));
`)
	want := "request handle: closed\ntask handle: closed\nshareHandler global writes persisted: 2\nisolated handler handle: closed\nnet.serve handle: closed\ntop-level handle: open\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
