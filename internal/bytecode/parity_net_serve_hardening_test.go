package bytecode_test

import "testing"

// net.serve left connections open after the handler returned or threw, and socket line reads had no length limit.
func TestParityNetServeClosesConnectionsAndCapsLines(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import net;
import sockets;
import async.channel as ch;

let srv = net.serve("127.0.0.1", 0, func(dict<string, any> sock): void {
    let first = net.read(sock["handle"], 8) as string;
    if (first == "throw") { throw RuntimeError("bad input"); }
    net.write(sock["handle"], "ok" as bytes);
});
let addr = srv["localAddr"] as string;
for (msg in ["hello", "throw"]) {
    let c = net.connectTcp(addr);
    net.write(c, msg as bytes);
    net.setDeadline(c, 2000);
    let reply = net.read(c, 8) as string;
    let after = net.read(c, 8);
    io.println("${msg}: reply=[${reply}] then ${after.length()} bytes (closed)");
    net.close(c);
}
net.closeListener(srv["handle"]);

let lines = ch.Channel<string>(3);
let ls = sockets.serve("127.0.0.1", 0, func(any conn): void {
    try {
        lines.send("first: " + conn.readLine());
        lines.send("at limit: " + (conn.readLine().length() as string));
        conn.readLine();
        lines.send("long line returned");
    } catch (IOError e) {
        lines.send("long line: " + e.message);
    }
});
let port = ls.localAddr().split(":")[1] as int;
let c = sockets.dial("127.0.0.1", port);
c.writeln("hello");
c.write("y".repeat(1048576) + "\r\n");
c.writeln("x".repeat(1048577));
io.println(lines.recv());
io.println(lines.recv());
io.println(lines.recv());
c.close();
ls.close();
`)
	want := "hello: reply=[ok] then 0 bytes (closed)\nthrow: reply=[] then 0 bytes (closed)\nfirst: hello\nat limit: 1048576\nlong line: line exceeds 1048576 bytes\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
