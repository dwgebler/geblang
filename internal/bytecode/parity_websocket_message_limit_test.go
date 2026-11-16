package bytecode_test

import "testing"

// Both backends serve WebSockets through the shared evaluator server; this pins the message limit, its error and its option validation to one behaviour.
func TestParityWebSocketMessageLimit(t *testing.T) {
	prog := `
import http;
import io;
import store;
import sys;
import websocket;
const outcome = store.Store();
func echo(dict<string, any> req): dict<string, any> {
    return websocket.upgrade(func(any conn): void {
        try {
            while (true) {
                string msg = websocket.readText(conn);
                websocket.sendText(conn, "ok:" + (msg.length() as string));
            }
        } catch (Error e) {
            outcome.set("error", e.message);
        }
    });
}
let server = http.listen("127.0.0.1:0", echo, {"maxWebSocketMessageBytes": 32});
let conn = websocket.connect("ws://" + http.serverAddr(server) + "/");
websocket.sendText(conn, "x".repeat(32));
io.println(websocket.readText(conn));
websocket.sendText(conn, "x".repeat(33));
try {
    websocket.readText(conn);
    io.println("still open");
} catch (Error e) {
    io.println("client closed");
}
for (i in 0..<100) {
    if (outcome.get("error") != null) { break; }
    sys.sleep(20);
}
io.println(outcome.get("error"));
http.close(server);
try {
    http.listen("127.0.0.1:0", echo, {"maxWebSocketMessageBytes": -1});
} catch (Error e) {
    io.println(e.message);
}
`
	want := "ok:32\nclient closed\n" +
		"websocket.readText: message exceeds the server's maxWebSocketMessageBytes limit; the connection was closed\n" +
		"http.listen opts.maxWebSocketMessageBytes must be a non-negative int\n"
	ev, vm := parityOutputsStdlib(t, prog)
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q\nVM=%q\nwant %q", ev, vm, want)
	}
}
