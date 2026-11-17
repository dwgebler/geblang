package bytecode_test

import "testing"

// readAll and readLines on a socket buffered everything until the peer closed, so a peer that never closed could exhaust memory.
func TestParityNetworkReadAllCap(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import net;
import sockets;
import async.channel as ch;

let release = ch.Channel<bool>(4);
let srv = net.serve("127.0.0.1", 0, func(dict<string, any> sock): void {
    let mode = net.read(sock["handle"], 8) as string;
    if (mode == "big") {
        net.write(sock["handle"], "b".repeat(64 * 1024 * 1024 + 1) as bytes);
        release.recv();
    } else if (mode == "lines") {
        for (let int i = 0; i < 30; i++) { net.write(sock["handle"], ("l".repeat(100) + "\n") as bytes); }
    } else {
        net.write(sock["handle"], "small data" as bytes);
    }
});
let port = (srv["localAddr"] as string).split(":")[1] as int;
func dial(string mode): any {
    let c = sockets.dial("127.0.0.1", port);
    c.write(mode);
    return c;
}
func attempt(string label, callable f): void {
    try { io.println("${label}: ${f()}"); } catch (IOError e) { io.println("${label}: IOError ${e.message}"); }
}
let big = dial("big");
attempt("never-closing peer", func(): any { return big.readAll().length(); });
release.send(true);
big.close();
attempt("explicit cap", func(): any { return dial("small").readAll(5).length(); });
attempt("no cap", func(): any { return dial("small").readAll(0); });
attempt("default small", func(): any { return dial("small").readAll(); });
attempt("readLines cap", func(): any { return io.readLines(dial("lines").stream.handle, 1000).length(); });
attempt("readLines fits", func(): any { return io.readLines(dial("lines").stream.handle).length(); });
string path = io.tempDir("rac_") + "/f.txt";
io.writeText(path, "file contents");
let f = io.open(path, "r");
attempt("file unlimited", func(): any { return io.readAll(f); });
net.closeListener(srv["handle"]);
`)
	want := "never-closing peer: IOError stream data exceeds 67108864 bytes\nexplicit cap: IOError stream data exceeds 5 bytes\nno cap: small data\ndefault small: small data\nreadLines cap: IOError stream data exceeds 1000 bytes\nreadLines fits: 30\nfile unlimited: file contents\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
