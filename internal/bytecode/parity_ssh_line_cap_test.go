package bytecode_test

import (
	"fmt"
	"testing"
)

// SSH session output is paced by the remote host, so it gets the same line cap as sockets.
func TestParitySSHSessionLineCap(t *testing.T) {
	srv := startSSHTestServer(t)
	defer srv.stop()
	ev, vm := parityOutputsStdlib(t, fmt.Sprintf(`import io;
import ssh;
import async;

let c = ssh.connect("alice@127.0.0.1", {
    "port": %s,
    "password": "secret",
    "insecureSkipHostKey": true,
});
let s = c.spawn("cat");
let writer = async.run(func(): void {
    s.stdin.write("short\n");
    s.stdin.write("x".repeat(1048577) + "\n");
    s.stdin.close();
});
io.println(s.stdout.readLine());
try { s.stdout.readLine(); io.println("long line returned"); } catch (IOError e) { io.println("long line: " + e.message); }
async.await(writer);
c.close();
`, srv.port()))
	want := "short\nlong line: line exceeds 1048576 bytes\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
