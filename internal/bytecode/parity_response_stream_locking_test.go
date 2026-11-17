package bytecode_test

import "testing"

// StreamResponse reads held one process-wide lock across the network read, so one stalled stream blocked every other.
func TestParityResponseStreamsReadIndependently(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async;
import async.channel as ch;

let release = ch.Channel<bool>(1);
let s = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> {
    return http.stream(func(int handle): void {
        if (r["path"] == "/stall") {
            http.streamWrite(handle, "first\n");
            http.streamFlush(handle);
            release.recv();
        } else if (r["path"] == "/long") {
            http.streamWrite(handle, "x".repeat(2 * 1024 * 1024));
            http.streamFlush(handle);
        } else {
            for (i in 1..3) { http.streamWrite(handle, "line ${i}\n"); }
        }
        http.streamClose(handle);
    });
}, {"shareHandler": true});
let base = "http://${http.serverAddr(s)}";

let stalled = http.requestStream({"url": base + "/stall"});
io.println("stalled first: " + (stalled.read() as string));
let blocked = async.run(func(): string {
    let next = stalled.read();
    return next == null ? "ended" : "got " + (next as string);
});
await async.sleep(100);
let other = http.requestStream({"url": base + "/fast"});
list<string> lines = [];
let line = other.read();
while (line != null) { lines.push(line as string); line = other.read(); }
io.println("other stream while one is blocked: ${lines}");
stalled.close();
try { io.println("blocked reader after close: " + async.await(blocked)); } catch (Error e) { io.println("blocked reader after close: ${typeof(e)}"); }
release.send(true);

let long = http.requestStream({"url": base + "/long"});
try { long.read(); io.println("long line returned"); } catch (IOError e) { io.println("long line: " + e.message); }
io.println("long stream done: ${long.done()}");
http.close(s);
`)
	want := "stalled first: first\nother stream while one is blocked: [\"line 1\", \"line 2\", \"line 3\"]\nblocked reader after close: IOError\nlong line: line exceeds 1048576 bytes\nlong stream done: true\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
