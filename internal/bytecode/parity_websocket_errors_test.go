package bytecode_test

import "testing"

// WebSocket failures raised RuntimeError or IOError depending on how the connection ended.
func TestParityWebSocketFailuresRaiseIOError(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async.channel as ch;
import web.websocket as ws;
let results = ch.Channel<string>(4);
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    if (request["path"] == "/plain") { return {"status": 200, "body": "not a websocket"}; }
    return ws.upgrade(func(ws.Connection conn): void {
        try { conn.readText(); results.send("no raise"); } catch (Error e) { results.send("server read: ${typeof(e)}"); }
    });
}, {"shareHandler": true, "maxWebSocketMessageBytes": 8});
let addr = http.serverAddr(server);
let c = ws.connect("ws://${addr}/");
c.close();
io.println(results.recv());
try { c.sendText("x"); } catch (Error e) { io.println("send after close: ${typeof(e)}"); }
try { c.readText(); } catch (Error e) { io.println("read after close: ${typeof(e)}"); }
try { ws.connect("ws://${addr}/plain"); } catch (Error e) { io.println("bad handshake: ${typeof(e)}"); }
try { ws.connect("ws://127.0.0.1:1/"); } catch (Error e) { io.println("refused: ${typeof(e)}"); }
let big = ws.connect("ws://${addr}/");
big.sendText("way more than eight bytes");
io.println(results.recv());
try { big.readText(); } catch (Error e) { io.println("client after limit close: ${typeof(e)}"); }
http.close(server);
`)
	want := "server read: IOError\nsend after close: IOError\nread after close: IOError\nbad handshake: IOError\nrefused: IOError\nserver read: IOError\nclient after limit close: IOError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
