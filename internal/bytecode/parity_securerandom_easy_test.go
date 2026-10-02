package bytecode_test

import "testing"

func TestParitySecureRandomIntAndBytes(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import secureRandom;

let seed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
let s = secureRandom.fromSeed(seed, "Alice");
io.println([secureRandom.randomInt(s, 1, 6) for i in 1..12]);
io.println(secureRandom.randomInt(s, -3, 3));
io.println(secureRandom.randomInt(s, 0, 0));
io.println(secureRandom.randomInt(s, -9223372036854775807 - 1, 9223372036854775807));
io.println(secureRandom.randomBytes(s, 6));
io.println(secureRandom.bytes(secureRandom.fromSeed(seed, "Alice"), 6));
io.println(secureRandom.randomBytes(secureRandom.fromSeed(seed, "Alice"), 6));
let entries = secureRandom.auditLog(s);
io.println(entries.length());
io.println(entries[0]);
io.println(entries[15]);
io.println(secureRandom.replay(seed, "Alice", 0, "randomInt", [1, 6]));
io.println(secureRandom.replay(seed, "Alice", 12, "randomInt", [-3, 3]));
io.println(secureRandom.replay(seed, "Alice", 15, "bytes", [6]));

let ok = true;
for (i in 1..200) {
    let n = secureRandom.randomInt(1, 6);
    ok = ok && n >= 1 && n <= 6;
}
io.println(ok);
io.println(secureRandom.randomInt(7, 7));
io.println(secureRandom.randomInt(2 ** 70, 2 ** 70));
io.println(secureRandom.randomInt(min: -2, max: -2));
io.println(secureRandom.randomInt(...[9, 9]));
io.println(secureRandom.randomBytes(16).length());
io.println(secureRandom.randomBytes(0).length());
io.println(typeof(secureRandom.randomBytes(4)));
io.println(secureRandom.randomBytes(16) != secureRandom.randomBytes(16));

func report(func f): void {
    try {
        f();
    } catch (RuntimeError e) {
        io.println(e.message);
    }
}
report(func(): void { secureRandom.randomInt(5, 1); });
report(func(): void { secureRandom.randomInt(s, 5, 1); });
report(func(): void { secureRandom.randomInt(1); });
report(func(): void { secureRandom.randomInt("a", 2); });
report(func(): void { secureRandom.randomInt(s, 1, 2 ** 70); });
report(func(): void { secureRandom.randomBytes(-1); });
secureRandom.reveal(s);
report(func(): void { secureRandom.randomInt(s, 1, 6); });
report(func(): void { secureRandom.randomBytes(s, 2); });
`)
	want := "[5, 5, 4, 3, 4, 5, 2, 6, 3, 5, 5, 2]\n-3\n0\n7082999434584174822\na7623d54994a\nc6cc652295f5\nc6cc652295f5\n16\n" +
		"{\"nonce\": 0, \"method\": \"randomInt\", \"args\": [1, 6], \"output\": 5}\n" +
		"{\"nonce\": 15, \"method\": \"bytes\", \"args\": [6], \"output\": a7623d54994a}\n" +
		"5\n-3\na7623d54994a\ntrue\n7\n1180591620717411303424\n-2\n9\n16\n0\nbytes\ntrue\n" +
		"secureRandom.randomInt min must be <= max\n" +
		"secureRandom.randomInt min must be <= max\n" +
		"secureRandom.randomInt expects min and max\n" +
		"secureRandom.randomInt min must be int\n" +
		"secureRandom.randomInt: min and max must be 64-bit ints in a session draw\n" +
		"secureRandom.randomBytes byte count out of range\n" +
		"secureRandom.randomInt: session has been revealed; no further draws allowed\n" +
		"secureRandom.bytes: session has been revealed; no further draws allowed\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
