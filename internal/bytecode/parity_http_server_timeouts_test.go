package bytecode_test

import "testing"

// Both backends share the evaluator HTTP server; this pins the timeout options and the 408 on a stalled body to one behaviour.
func TestParityHTTPServerReadTimeouts(t *testing.T) {
	prog := `
import http;
import io;
import net;
import sys;
func echo(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "got:" + ((req["body"] as string).length() as string), "headers": {}};
}
func slow(dict<string, any> req): dict<string, any> {
    sys.sleep(300);
    return {"status": 200, "body": "slow-ok", "headers": {}};
}
let server = http.listen("127.0.0.1:0", echo, {"maxConcurrent": 1, "onOverload": "reject", "readBodyTimeoutMs": 200});
let addr = http.serverAddr(server);
let stalled = net.dial("127.0.0.1", addr.split(":")[1] as int);
io.write(stalled["stream"], "POST /in HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\na");
let reply = io.readAll(stalled["stream"]);
io.println(reply.contains("408 Request Timeout"));
io.println(reply.contains("Connection: close"));
io.println(http.request("http://" + addr + "/in").withMethod("POST").withBody("hello").send().text());
http.close(server);

let slowServer = http.listen("127.0.0.1:0", slow, {"readBodyTimeoutMs": 100});
io.println(http.request("http://" + http.serverAddr(slowServer) + "/in").withMethod("POST").withBody("x").send().text());
http.close(slowServer);

for (name in ["readHeaderTimeoutMs", "readBodyTimeoutMs", "idleTimeoutMs"]) {
    dict<string, any> opts = {};
    opts[name] = -1;
    try {
        http.listen("127.0.0.1:0", echo, opts);
        io.println("accepted");
    } catch (Error e) {
        io.println(e.message);
    }
}
`
	runParityStateful(t, prog, "true\ntrue\ngot:5\nslow-ok\n"+
		"http.listen opts.readHeaderTimeoutMs must be a non-negative int\n"+
		"http.listen opts.readBodyTimeoutMs must be a non-negative int\n"+
		"http.listen opts.idleTimeoutMs must be a non-negative int\n")
}
