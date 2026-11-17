package bytecode_test

import "testing"

// Task evaluators kept private handle registries numbered from 1, so tasks could not close parent handles and their own handles shadowed the parent's.
func TestParityTaskClosesParentWebSocketAndUnblocksReader(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
import async.channel as ch;
import web.websocket as ws;

class Closer {
    ws.Connection conn;
    func Closer(ws.Connection conn) { this.conn = conn; }
    func later(): string {
        await async.sleep(30);
        this.conn.close();
        return "closed";
    }
}

let results = ch.Channel<string>(1);
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(ws.Connection conn): void {
        let closer = Closer(conn);
        let task = async.run(func(): string { return closer.later(); });
        string outcome = "returned";
        try { conn.readText(); } catch (Error e) { outcome = "raised"; }
        results.send("server read ${outcome}; task ${async.await(task)}");
    });
}, {"shareHandler": true});
let c = ws.connect("ws://${http.serverAddr(server)}/");
try { c.readText(); io.println("client read a message"); } catch (Error e) { io.println("client saw close"); }
io.println(results.recv());
http.close(server);
`)
	want := "client saw close\nserver read raised; task closed\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}

func TestParityTaskSendsOnParentWebSocketNotItsOwn(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
import async.channel as ch;
import web.websocket as ws;

let received = ch.Channel<string>(1);
let other = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(ws.Connection conn): void {
        received.send(conn.readText());
    });
}, {"shareHandler": true});
let otherAddr = http.serverAddr(other);
let main = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(ws.Connection conn): void {
        async.await(async.run(func(): void {
            let side = ws.connect("ws://${otherAddr}/");
            conn.sendText("for main");
            side.sendText("for other");
            side.close();
        }));
        conn.close();
    });
}, {"shareHandler": true});
let c = ws.connect("ws://${http.serverAddr(main)}/");
io.println("main got: " + c.readText());
io.println("other got: " + received.recv());
http.close(main);
http.close(other);
`)
	want := "main got: for main\nother got: for other\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}

func TestParityHandlesCrossTaskBoundaryBothWays(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import net;
import async;

let parentOwned = net.listenTcp("127.0.0.1:0");
io.println(async.await(async.run(func(): string {
    try { net.close(parentOwned); return "task closed parent listener"; } catch (Error e) { return "failed: " + e.message; }
})));

let taskOwned = async.await(async.run(func(): any { return net.listenTcp("127.0.0.1:0"); }));
try {
    io.println(net.localAddr(taskOwned) != "");
    net.close(taskOwned);
    io.println("parent closed task listener");
} catch (Error e) {
    io.println("failed: " + e.message);
}
`)
	want := "task closed parent listener\ntrue\nparent closed task listener\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}

func TestParityTraceAndMetricsFromTasksAndRequests(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
import metrics;
import trace;

let span = trace.start("outer");
async.await(async.run(func(): void {
    trace.event(span, "inside-task");
    trace.end(span);
    trace.end(trace.start("task-span"));
    metrics.inc("task");
}));
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    metrics.inc("request");
    trace.end(trace.start("request-span"));
    return {"status": 200, "body": "ok"};
});
http.get("http://${http.serverAddr(server)}/");
http.close(server);
metrics.inc("root");
io.println(metrics.snapshot());
list<string> names = [];
for (s in trace.snapshot()) { names.push((s as dict<string, any>)["name"] as string); }
io.println(names.sorted());
`)
	want := "{\"request\": 1, \"root\": 1, \"task\": 1}\n[\"outer\", \"request-span\", \"task-span\"]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
