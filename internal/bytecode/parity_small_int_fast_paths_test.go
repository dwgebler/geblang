package bytecode_test

import "testing"

const smallIntFastPathsProgram = `import io;

class Counter {
    int n;
    func Counter(int n) { this.n = n; }
    func value(): int { return this.n; }
    func text(): string { return "n${this.n}"; }
}

func top(): int { return 9223372036854775807; }
func castToParam<int>(any v): any { return v as int; }

let one = Counter(1);
let c = Counter(40);
io.println(c.value() + one.value() + c.value());
io.println(top() + one.value());
io.println((0 - top()) + (0 - one.value()) + (0 - one.value()));
io.println(top() + top());
io.println(typeof(top() + one.value()));
let t = top();
let o = one.value();
io.println(t + o);
io.println((0 - t) + (0 - o) + (0 - o));
io.println(t + "hello".length());
io.println(c.text() + c.text());
io.println(typeof(c.value() + 0.5f));
io.println(typeof(c.value() + 0.5));
int total = 0;
for (int i = 0; i < 4; i++) { total = total + i + c.value(); }
io.println(total);

io.println("".length());
io.println("héllo".length());
io.println("a".repeat(256).length());
io.println("a".repeat(257).length());
io.println("é".repeat(300).length());
string long = "aé".repeat(200);
io.println(long.length() + long.length());

io.println(c.value() as int);
io.println(typeof(c.value() as int));
io.println((c.value() == 40) as bool);
io.println(typeof(1.5f as float));
io.println(typeof(c.value() as string));
io.println(typeof(castToParam<string>(5)));
io.println(typeof(castToParam<int>(5)));

let d = {"x": 1, "y": 2, "z": 3};
d["y"] = 20;
d["w"] = 4;
d.remove("x");
io.println(d);
let dup = {"k": 1, "j": 2, "k": 3};
io.println(dup);
io.println({"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8, "i": 9}.keys());
`

const smallIntFastPathsWant = `81
9223372036854775808
-9223372036854775809
18446744073709551614
int
9223372036854775808
-9223372036854775809
9223372036854775812
n40n40
float
decimal
166
0
5
256
257
300
800
40
int
true
float
string
string
int
{"y": 20, "z": 3, "w": 4}
{"k": 3, "j": 2}
["a", "b", "c", "d", "e", "f", "g", "h", "i"]
`

func TestParitySmallIntFastPaths(t *testing.T) {
	ev, vm := parityOutputs(t, smallIntFastPathsProgram)
	if ev != vm {
		t.Fatalf("backends diverge\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	if vm != smallIntFastPathsWant {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", vm, smallIntFastPathsWant)
	}
}
