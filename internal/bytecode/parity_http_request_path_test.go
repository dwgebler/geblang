package bytecode_test

import "testing"

// The VM dispatches http.listen through the shared evaluator HTTP server, so this pins both backends to the same canonical path.
func TestParityHTTPRequestPathCanonicalization(t *testing.T) {
	prog := `
import http;
import io;
func makeHandler(): callable {
    return func(dict<string, any> req): dict<string, any> {
        return {"status": 200, "body": req["path"] as string, "headers": {}};
    };
}
let server = http.listen("127.0.0.1:0", makeHandler());
let base = "http://" + http.serverAddr(server);
let a = http.get(base + "//admin/downloads")["body"] as string;
let b = http.get(base + "/admin/../secret")["body"] as string;
http.close(server);
io.println(a + "|" + b);
`
	runParityStateful(t, prog, "/admin/downloads|/secret\n")
}
