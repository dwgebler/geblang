package bytecode_test

import "testing"

// net.ipSet and the ipBlocklist server option: matching, expiry, and dropping blocked clients at connect time and per request.
func TestParityIPSetAndServerBlocklist(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import net;
import async;

let s = net.ipSet(["10.0.0.0/8", "2001:db8::/32", "192.0.2.7"]);
io.println("contains: ${s.contains("10.1.2.3")} ${s.contains("11.0.0.1")} ${s.contains("2001:db8:5::1")} ${s.contains("::ffff:10.9.9.9")} ${s.contains("192.0.2.7")} ${s.contains("bogus")}");
io.println("size ${s.size()} remove ${s.remove("192.0.2.7")} ${s.remove("192.0.2.7")} size ${s.size()}");
s.add("203.0.113.0/24", 1);
io.println("ttl entry: ${s.contains("203.0.113.9")}");
await async.sleep(1200);
io.println("after expiry: ${s.contains("203.0.113.9")} size ${s.size()}");
try { s.add("not-an-ip"); } catch (ValueError e) { io.println("invalid: ValueError"); }

func status(string url, dict<string, string> headers): string {
    try { return "${http.requestWithOptions({"url": url, "headers": headers, "timeoutMs": 2000})["status"]}"; }
    catch (IOError e) { return "dropped"; }
}
let block = net.ipSet();
let srv = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> { return {"status": 200, "body": "ok"}; }, {"ipBlocklist": block});
let url = "http://${http.serverAddr(srv)}/";
io.println("before block: " + status(url, {}));
block.add("127.0.0.1");
io.println("blocked at connect: " + status(url, {}));
block.remove("127.0.0.1");
io.println("unblocked: " + status(url, {}));
http.close(srv);

let fresh = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> { return {"status": 200, "body": "ok"}; }, {"ipBlocklist": net.ipSet(["127.0.0.0/8"])});
io.println("blocked from the first connection: " + status("http://${http.serverAddr(fresh)}/", {}));
http.close(fresh);

let proxied = http.listen("127.0.0.1:0", func(dict<string, any> r): dict<string, any> { return {"status": 200, "body": "ok"}; }, {"ipBlocklist": block, "trustedProxies": ["127.0.0.1/32"]});
let purl = "http://${http.serverAddr(proxied)}/";
block.add("198.51.100.0/24");
io.println("proxied blocked client: " + status(purl, {"X-Forwarded-For": "198.51.100.20"}));
io.println("proxied other client: " + status(purl, {"X-Forwarded-For": "198.51.101.20"}));
http.close(proxied);

let tcpBlock = net.ipSet(["127.0.0.1"]);
let tsrv = net.serve("127.0.0.1", 0, func(dict<string, any> sock): void { net.write(sock["handle"], "hi" as bytes); }, {"ipBlocklist": tcpBlock});
let c = net.connectTcp(tsrv["localAddr"] as string);
net.setDeadline(c, 1000);
try { io.println("net.serve blocked read bytes: ${net.read(c, 4).length()}"); } catch (IOError e) { io.println("net.serve blocked: IOError"); }
net.close(c);
net.closeListener(tsrv["handle"]);
`)
	want := "contains: true false true true true false\nsize 3 remove true false size 2\nttl entry: true\nafter expiry: false size 2\ninvalid: ValueError\nbefore block: 200\nblocked at connect: dropped\nunblocked: 200\nblocked from the first connection: dropped\nproxied blocked client: dropped\nproxied other client: 200\nnet.serve blocked read bytes: 0\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
