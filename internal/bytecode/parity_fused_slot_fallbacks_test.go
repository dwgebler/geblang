package bytecode_test

import "testing"

const fusedSlotFallbacksProgram = `import io;

func modCaptured(int start): string {
    int x = start;
    let read = func(): int { return x; };
    string out = "";
    if (x % 3 == 0) { out = out + "a"; }
    if (x % 3 != 0) { out = out + "b"; }
    if (0 == x % 2) { out = out + "c"; }
    if (x % -4 == 0) { out = out + "d"; }
    return "${out}${read()}";
}

func modBig(int bump): string {
    int x = 9223372036854775807;
    x = x + bump;
    string out = "";
    if (x % 2 == 0) { out = out + "even"; }
    if (x % 2 != 0) { out = out + "odd"; }
    if (x % -7 == 0) { out = out + "seven"; }
    return out;
}

func selfArithCaptured(): int {
    int x = 10;
    int y = 3;
    let read = func(): int { return x + y; };
    x += 5;
    x -= 2;
    x += y;
    x -= y;
    x++;
    x--;
    return read();
}

func selfArithBig(): int {
    int x = 9223372036854775807;
    int y = 9223372036854775807;
    x += 5;
    x -= 2;
    x += y;
    x++;
    x--;
    return x;
}

func compareCaptured(): int {
    int total = 0;
    for (int i = 0; i < 5; i++) {
        let f = func(): int { return i; };
        if (i >= 2) { total += f(); }
        if (i == 3) { total += 100; }
    }
    return total;
}

func compareBig(): string {
    int x = 9223372036854775807;
    x = x + 10;
    int y = 5;
    string out = "";
    if (x > y) { out = out + "gt"; }
    if (y < x) { out = out + "lt"; }
    if (x != y) { out = out + "ne"; }
    if (x == x) { out = out + "eq"; }
    return out;
}

func appendCaptured(): string {
    string s = "a";
    list<int> items = [1];
    let read = func(): string { return "${s}${items.length()}"; };
    s += "b";
    s = s + "c";
    items.push(2);
    return read();
}

func methodCaptured(): int {
    dict<string, int> d = {"a": 1};
    string s = "hello";
    let read = func(): int { return d.get("a") + s.length(); };
    return d.get("a") + s.length() + read();
}

func accumThenCapture(): string {
    string s = "a";
    s += "b";
    s += "c";
    let read = func(): string { return s; };
    s += "d";
    string t = "x";
    let readT = func(): string { return t; };
    t = t + "y";
    string viaExpr = (t += "z");
    return "${read()}|${s}|${readT()}|${viaExpr}";
}

func loopCaptured(): string {
    string out = "";
    list<any> fns = [];
    for (int i = 0; i < 3; i++) {
        string label = "n";
        fns.push(func(): string { return label; });
        label += "x";
        if (i % 2 == 0) { label += "e"; }
        out += label;
    }
    for (f in fns) { out += f(); }
    return out;
}

int g = 9223372036854775807;
g += 1;
g -= 3;
g++;
int h = 4;
let readH = func(): int { return h; };
if (h % 2 == 0) { io.println("global even ${readH()}"); }

io.println(modCaptured(12));
io.println(modCaptured(7));
io.println(modBig(1));
io.println(modBig(2));
io.println(selfArithCaptured());
io.println(selfArithBig());
io.println(compareCaptured());
io.println(compareBig());
io.println(appendCaptured());
io.println(methodCaptured());
io.println(g);
io.println(accumThenCapture());
io.println(loopCaptured());
`

const fusedSlotFallbacksWant = `global even 4
acd12
b7
even
odd
16
18446744073709551617
109
gtltneeq
abc2
12
9223372036854775806
abcd|abcd|xyz|xyz
nxenxnxenxenxnxe
`

func TestParityFusedSlotFallbacks(t *testing.T) {
	ev, vm := parityOutputs(t, fusedSlotFallbacksProgram)
	if ev != vm {
		t.Fatalf("backends diverge\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	if vm != fusedSlotFallbacksWant {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", vm, fusedSlotFallbacksWant)
	}
}
