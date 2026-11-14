package bytecode_test

import (
	"bytes"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const fusedLocalConstProgram = `import io;

func fib(int n): int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

func compare(int n): string {
    string out = "";
    if (n < 3) { out = out + "lt"; }
    if (n <= 3) { out = out + "le"; }
    if (n > 3) { out = out + "gt"; }
    if (n >= 3) { out = out + "ge"; }
    if (n == 3) { out = out + "eq"; }
    if (n != 3) { out = out + "ne"; }
    return out;
}

func arith(int n): list<int> {
    return [n + 1, n - 1, n + 0, n - 0, (n + 2) * (n - 2), n - 5 + n];
}

func edges(): list<string> {
    int top = 9223372036854775807;
    int bottom = 0 - top;
    bottom = bottom - 1;
    int past = top;
    past = past + 5;
    list<string> out = [];
    out.push("${top + 1}");
    out.push("${top - 1}");
    out.push("${bottom - 1}");
    out.push("${bottom + 1}");
    out.push("${past - 5}");
    out.push("${past + 9223372036854775807}");
    out.push("${typeof(top + 1)}");
    out.push("${typeof(past - 6)}");
    if (past > 9223372036854775807) { out.push("past-gt"); }
    if (past == 7) { out.push("wrong"); }
    if (bottom < 0) { out.push("bottom-lt"); }
    if (past != 3) { out.push("past-ne"); }
    return out;
}

func captured(int start): string {
    int x = start;
    let bump = func(): void { x = x + 10; };
    let read = func(): int { return x - 1; };
    string out = "${x + 1}";
    bump();
    if (x > 10) { out = out + "|big"; }
    while (x < 40) { x = x + 7; }
    return "${out}|${x - 2}|${read()}";
}

func loops(int limit): int {
    int total = 0;
    for (int i = 0; i < 5; i++) {
        total = total + (i + 1);
    }
    int j = limit;
    while (j > 0) {
        total = total + (j - 1);
        j = j - 1;
    }
    return total;
}

func pick(int n): string {
    return n >= 10 ? "wide${n - 10}" : "narrow${n + 10}";
}

class Counter {
    int base;
    func Counter(int seed) {
        int local = seed;
        this.base = local + 100;
    }
    func step(int amount): int {
        if (amount > 1) {
            return this.base + (amount - 1);
        }
        return this.base + (amount + 1);
    }
    static func twice(int v): int { return (v + 1) + (v - 1); }
}

class Sub extends Counter {
    func Sub(int seed) { parent(seed - 1); }
    func step(int amount): int {
        if (amount == 0) { return 0 - 1; }
        return parent.step(amount + 1);
    }
}

func countdown(int from): generator<int> {
    int n = from;
    while (n > 0) {
        yield n - 1;
        n = n - 1;
    }
}

func optional(int n, int extra = 0): int {
    if (extra != 0) { return n + extra; }
    return n - 3;
}

io.println(fib(15));
io.println("${compare(2)} ${compare(3)} ${compare(4)}");
io.println(arith(7));
io.println(arith(-7));
io.println(edges());
io.println(captured(3));
io.println(loops(4));
io.println("${pick(12)} ${pick(2)}");
io.println(Counter(1).step(5));
io.println(Counter(1).step(1));
io.println(Sub(1).step(3));
io.println(Sub(1).step(0));
io.println(Counter.twice(9));
io.println([v for v in countdown(4)]);
io.println("${optional(10)} ${optional(10, 4)} ${optional(extra: 2, n: 1)}");
int up = 4;
up += fib(up + 1);
io.println(up);
io.println([1, 2, 3].map(func(int v): int { return v - 1; }));
io.println("abc".repeat(fib(3) + 0));
`

const fusedLocalConstWant = `610
ltlene legeeq gtgene
[8, 6, 7, 7, 45, 9]
[-6, -8, -7, -7, 45, -19]
["9223372036854775808", "9223372036854775806", "-9223372036854775809", "-9223372036854775807", "9223372036854775807", "18446744073709551619", "int", "int", "past-gt", "bottom-lt", "past-ne"]
4|big|39|40
21
wide2 narrow12
105
103
103
-1
18
[3, 2, 1, 0]
7 14 3
9
[0, 1, 2]
abcabc
`

func TestParityFusedLocalConst(t *testing.T) {
	ev, vm := parityOutputs(t, fusedLocalConstProgram)
	if ev != vm {
		t.Fatalf("backends diverge\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	if vm != fusedLocalConstWant {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", vm, fusedLocalConstWant)
	}
}

func compileFusedLocalConst(t *testing.T) bytecode.Chunk {
	t.Helper()
	p := parser.New(lexer.New(fusedLocalConstProgram))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(program, []byte(fusedLocalConstProgram), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return chunk
}

func TestFusedLocalConstOpcodesAreEmitted(t *testing.T) {
	chunk := compileFusedLocalConst(t)
	seen := map[bytecode.Op]int{}
	for _, instruction := range chunk.Instructions {
		seen[instruction.Op]++
	}
	for _, op := range []bytecode.Op{bytecode.OpLocalAddIntConst, bytecode.OpLocalSubIntConst, bytecode.OpJumpIfLocalCmpIntConst} {
		if seen[op] == 0 {
			t.Fatalf("opcode %d was not emitted", op)
		}
	}
}

func TestFusedLocalConstSurvivesChunkRoundTrip(t *testing.T) {
	encoded, err := bytecode.Encode(compileFusedLocalConst(t))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if out.String() != fusedLocalConstWant {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", out.String(), fusedLocalConstWant)
	}
}

func TestParityFusedLocalConstCrossModule(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"steps.gb": `module steps;

export func fib(int n): int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

export class Walker {
    int at;
    func Walker(int start) { this.at = start + 1; }
    func advance(int amount): int {
        if (amount >= 2) {
            this.at = this.at + (amount - 1);
        }
        return this.at;
    }
}
`}, `import io;
import steps;

class Runner extends steps.Walker {
    func Runner(int start) { parent(start - 1); }
    func sprint(int amount): int {
        if (amount != 0) {
            return this.advance(amount + 2);
        }
        return this.at;
    }
}

io.println(steps.fib(12));
let r = Runner(5);
io.println(r.advance(1));
io.println(r.advance(4));
io.println(r.sprint(3));
io.println(r.sprint(0));
let viaValue = steps.fib;
io.println(viaValue(10));
`)
	if ev != vm {
		t.Fatalf("backends diverge\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	want := "144\n5\n8\n12\n12\n55\n"
	if vm != want {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", vm, want)
	}
}
