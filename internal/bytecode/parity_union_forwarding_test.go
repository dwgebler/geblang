package bytecode_test

import "testing"

func TestParityUnionArgumentForwarding(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Box {
    int n;
    func Box(Box|int v) {
        this.n = Box.unwrap(v);
    }
    static func unwrap(Box|int v): int {
        if (v instanceof Box) {
            return (v as Box).n;
        }
        return v as int;
    }
    static func relay(Box|int v): int {
        return Box.unwrap(v);
    }
    func plus(Box|int other): Box {
        return Box(this.n + Box.unwrap(other));
    }
    func viaThis(Box|int other): Box {
        return this.plus(other);
    }
    func rebuild(Box|int other): Box {
        return Box(other);
    }
    func __add(Box|int other): Box {
        return this.plus(other);
    }
}

func label(string|int v): string {
    return "${typeof(v)}:${v}";
}
func relay(string|int v): string {
    return label(v);
}
func nullable(?string|int v): string {
    return typeof(v);
}
func widen(string|int v): string {
    return nullable(v);
}
func wide(string|int|bool v): string {
    return typeof(v);
}
func subset(string|int v): string {
    return wide(v);
}
func onlyInt(int v): int {
    return v + 1;
}
func narrowed(string|int v): int {
    if (v instanceof int) {
        return onlyInt(v);
    }
    return -1;
}
func generic(list<int>|string v): string {
    return typeof(v);
}
func genericRelay(list<int>|string v): string {
    return generic(v);
}

io.println(relay(3));
io.println(relay("a"));
io.println(widen(4));
io.println(subset("s"));
io.println(narrowed(41));
io.println(narrowed("x"));
io.println(genericRelay([1]));
io.println(genericRelay("x"));
io.println(Box.relay(5));
io.println(Box.relay(Box(6)));
io.println(Box(1).plus(2).n);
io.println(Box(1).plus(Box(9)).n);
io.println(Box(1).viaThis(3).n);
io.println(Box(1).rebuild(Box(7)).n);
io.println((Box(2) + 3).n);
io.println((Box(2) + Box(4)).n);
let lambda = func(string|int v): string { return label(v); };
io.println(lambda(8));
try {
    onlyInt(relay(1) as any);
} catch (RuntimeError e) {
    io.println(e.message);
}
`)
	want := "int:3\nstring:a\nint\nstring\n42\n-1\nlist\nstring\n5\n6\n3\n10\n4\n7\n5\n6\nint:8\nonlyInt expects int for parameter 'v', got string\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
