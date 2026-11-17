package bytecode_test

import "testing"

// Servers had no default body limit, took a concurrency slot before a slow body arrived, and let a non-reading client pin a handler.
func TestParityHTTPServerBodyLimitSlotOrderAndWriteTimeout(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import net;
import async;

func ok(dict<string, any> r): dict<string, any> {
    if (r["path"] == "/big") { return {"status": 200, "body": "b".repeat(64 * 1024 * 1024)}; }
    return {"status": 200, "body": "small ${r["body"].length()}"};
}

# E: 10 MiB default body limit; 0 lifts it.
let d = http.listen("127.0.0.1:0", ok);
string eleven = "x".repeat(11 * 1024 * 1024);
io.println("default 11MiB: ${http.post("http://${http.serverAddr(d)}/", eleven)["status"]}");
io.println("default 1MiB: ${http.post("http://${http.serverAddr(d)}/", "x".repeat(1024 * 1024))["status"]}");
http.close(d);
let u = http.listen("127.0.0.1:0", ok, {"maxBodyBytes": 0});
io.println("unlimited 11MiB: ${http.post("http://${http.serverAddr(u)}/", eleven)["status"]}");
http.close(u);

# F: a slow upload does not hold the only slot.
let p = http.listen("127.0.0.1:0", ok, {"maxConcurrent": 1, "queueSize": 0, "onOverload": "reject"});
let paddr = http.serverAddr(p);
let slow = net.connectTcp(paddr);
net.write(slow, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nabc" as bytes);
await async.sleep(200);
io.println("during slow upload: ${http.get("http://${paddr}/")["status"]}");
net.close(slow);
http.close(p);

# G: a client that stops reading a big response frees the slot at the write timeout.
let g = http.listen("127.0.0.1:0", ok, {"maxConcurrent": 1, "queueSize": 0, "onOverload": "reject", "writeTimeoutMs": 300});
let gaddr = http.serverAddr(g);
let stuck = net.connectTcp(gaddr);
net.write(stuck, "GET /big HTTP/1.1\r\nHost: x\r\n\r\n" as bytes);
await async.sleep(1500);
io.println("after stalled download: ${http.get("http://${gaddr}/")["status"]}");
let reader = http.get("http://${gaddr}/big");
io.println("full download: ${reader["status"]} ${reader["body"].length()}");
let again = http.get("http://${gaddr}/");
io.println("keep-alive request after: ${again["status"]}");
net.close(stuck);
http.close(g);
`)
	want := "default 11MiB: 413\ndefault 1MiB: 200\nunlimited 11MiB: 200\nduring slow upload: 200\nafter stalled download: 200\nfull download: 200 67108864\nkeep-alive request after: 200\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
