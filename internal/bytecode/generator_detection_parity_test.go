package bytecode_test

import "testing"

func TestGeneratorBlockFormsAndEarlyReturnParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import async.channel as ch;

class Res {
    func Res() {}
    func __enter(): Res { io.println("enter"); return this; }
    func __exit(): void { io.println("exit"); }
}

func insideWith(): generator<int> {
    with (r = Res()) { yield 1; yield 2; }
}
func insideSelect(): generator<string> {
    let c = ch.Channel<string>(1);
    c.send("ready");
    select {
        case let v = c.recv(): { yield v; }
        default: { yield "default"; }
    }
}
func upTo(int limit): generator<int> {
    for (n in [1, 2, 3, 4]) {
        if (n > limit) { return; }
        yield n;
    }
}
func stopInWith(): generator<int> {
    with (r = Res()) { yield 9; return; }
}

let g = insideWith();
io.println("created");
io.println([v for v in g]);
io.println([v for v in insideSelect()]);
io.println([v for v in upTo(2)]);
io.println([v for v in stopInWith()]);
`)
	want := "created\nenter\nexit\n[1, 2]\n[\"ready\"]\n[1, 2]\nenter\nexit\n[9]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
