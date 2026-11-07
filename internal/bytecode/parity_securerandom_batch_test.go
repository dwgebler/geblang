package bytecode_test

import "testing"

// randomInts is pinned: rounds audited with it must replay to the same
// values in every later release.
func TestParitySecureRandomInts(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import secureRandom;

let seed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
let s = secureRandom.fromSeed(seed, "Alice");
io.println(secureRandom.randomInts(s, 12, 1, 6));
io.println(secureRandom.randomInts(s, 3, -9223372036854775807 - 1, 9223372036854775807));
io.println(secureRandom.randomInts(s, 0, 1, 6));
let log = secureRandom.auditLog(s);
io.println(log.length());
io.println(log[0]);
io.println(secureRandom.replay(seed, "Alice", 0, "randomInts", [12, 1, 6]));
`)
	if ev != vm {
		t.Fatalf("evaluator and VM differ:\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	want := "[2, 4, 5, 1, 3, 1, 6, 6, 3, 4, 3, 5]\n" +
		"[7627682152251664404, -8160946475758419981, 891777474334510541]\n" +
		"[]\n3\n" +
		"{\"nonce\": 0, \"method\": \"randomInts\", \"args\": [12, 1, 6], \"output\": [2, 4, 5, 1, 3, 1, 6, 6, 3, 4, 3, 5]}\n" +
		"[2, 4, 5, 1, 3, 1, 6, 6, 3, 4, 3, 5]\n"
	if ev != want {
		t.Fatalf("randomInts output changed:\n%s", ev)
	}
}
