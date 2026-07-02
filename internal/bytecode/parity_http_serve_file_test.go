package bytecode_test

import "testing"

// The __serveFile marker routes through the shared evaluator writer, so the VM serves byte-identically to the evaluator.
func TestParityHTTPServeFile(t *testing.T) {
	prog := `
import http;
import io;
import web;
func makeHandler(): callable {
    return func(dict<string, any> req): dict<string, any> {
        return {"status": 200, "headers": {}, "__serveFile": web.serveFileMarker({"path": "TMPFILE"})};
    };
}
let server = http.listen("127.0.0.1:0", makeHandler());
let base = "http://" + http.serverAddr(server);
let r = http.get(base + "/file.txt");
http.close(server);
io.println((r["status"] as int as string) + "|" + (r["body"] as string));
`
	runParityStatefulWithFile(t, prog, "parity file body", "200|parity file body\n")
}
