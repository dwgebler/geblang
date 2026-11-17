package bytecode_test

import "testing"

// Server WebSockets had no idle, ping or write deadlines, so silent or non-reading clients pinned handlers forever.
func TestParityWebSocketServerTimeouts(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
import time;
import async.channel as ch;
import web.websocket as ws;

let results = ch.Channel<string>(4);
func serverWith(dict<string, any> opts, callable onConn): int {
    opts["shareHandler"] = true;
    return http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
        return ws.upgrade(func(ws.Connection conn): void { onConn(conn); });
    }, opts);
}

# 1. A silent client is dropped after the idle timeout.
let idle = serverWith({"webSocketIdleTimeoutMs": 300, "webSocketPingIntervalMs": 0}, func(ws.Connection conn): void {
    let t0 = time.monotonic();
    try { conn.readText(); results.send("idle: read returned"); }
    catch (TimeoutError e) { results.send("idle: TimeoutError after >=0.25s: ${time.monotonic() - t0 >= 0.25}"); }
});
let silent = ws.connect("ws://${http.serverAddr(idle)}/");
io.println(results.recv());
silent.close();
http.close(idle);

# 2. Pings answered by a reading client keep the connection alive past the idle timeout.
let alive = serverWith({"webSocketIdleTimeoutMs": 300, "webSocketPingIntervalMs": 100}, func(ws.Connection conn): void {
    try { results.send("alive: got " + conn.readText()); }
    catch (Error e) { results.send("alive: ${typeof(e)}"); }
});
let reader = ws.connect("ws://${http.serverAddr(alive)}/");
let pump = async.run(func(): void { try { reader.readText(); } catch (IOError e) { } });
await async.sleep(1000);
reader.sendText("late message");
io.println(results.recv());
reader.close();
async.await(pump);
http.close(alive);

# 3. A client that stops reading makes sends fail at the write timeout.
let stalled = serverWith({"webSocketWriteTimeoutMs": 200}, func(ws.Connection conn): void {
    string chunk = "z".repeat(256 * 1024);
    int sent = 0;
    try {
        while (sent < 4000) { conn.sendText(chunk); sent = sent + 1; }
        results.send("stalled: all sends completed");
    } catch (TimeoutError e) {
        results.send("stalled: TimeoutError");
    }
});
let lazy = ws.connect("ws://${http.serverAddr(stalled)}/");
io.println(results.recv());
lazy.close();
http.close(stalled);

# 4. A failing handler closes with a generic reason.
let failing = serverWith({}, func(ws.Connection conn): void { throw RuntimeError("secret detail"); });
let victim = ws.connect("ws://${http.serverAddr(failing)}/");
try { victim.readText(); } catch (IOError e) { io.println("close reason hides detail: ${!e.message.contains("secret")} ${e.message.contains("internal error")}"); }
http.close(failing);
`)
	want := "idle: TimeoutError after >=0.25s: true\nalive: got late message\nstalled: TimeoutError\nclose reason hides detail: true true\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
