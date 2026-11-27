package bytecode_test

import "testing"

// Natively built dicts now cache their key-type check; the cache must follow later key changes.
func TestParityNativeDictKeyTypeCheckFollowsMutation(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import async.channel as ch;

func accepts(dict<string, any> d): string { return "ok"; }

let results = ch.Channel<string>(1);
let server = http.listen("127.0.0.1:0", func(dict request): dict {
    list<string> out = [];
    if (request instanceof dict<string, any>) { out.push("ok"); }
    any loose = request;
    loose[1] = "int key";
    try { out.push(accepts(loose)); } catch (TypeError e) { out.push("TypeError"); }
    loose.remove(1);
    out.push(accepts(loose));
    try { loose[1] = "int key"; } catch (TypeError e) { out.push("guarded"); }
    results.send(out.join(" "));
    return {"status": 200, "body": "ok"};
});
http.get("http://${http.serverAddr(server)}/");
io.println(results.recv());
http.close(server);
`)
	want := "ok TypeError ok guarded\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
