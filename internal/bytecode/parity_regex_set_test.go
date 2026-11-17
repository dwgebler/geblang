package bytecode_test

import "testing"

func TestParityRegexSet(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import re;
let s = re.set(["(?i)\\bunion\\s+select\\b", "(a)(b)c", "x|y", "(?i)<script\\b", "zzz"]);
io.println("size ${s.size()}");
for (t in ["1 UNION SELECT 2", "abc", "y", "<SCRIPT>", "nothing", "abc then x", "x then abc", ""]) {
    io.println("[${t}] -> ${s.match(t)} ${s.test(t)}");
}
io.println("flags stay scoped: ${re.set(["(?i)abc", "XYZ"]).match("xyz")}");
io.println("empty set: ${re.set([]).match("anything")}");
io.println("tie goes to lower index: ${re.set(["ab", "a"]).match("ab")}");
try { re.set(["ok", "("]); } catch (ValueError e) { io.println("invalid: ValueError"); }
`)
	want := "size 5\n[1 UNION SELECT 2] -> 0 true\n[abc] -> 1 true\n[y] -> 2 true\n[<SCRIPT>] -> 3 true\n[nothing] -> -1 false\n[abc then x] -> 1 true\n[x then abc] -> 2 true\n[] -> -1 false\nflags stay scoped: -1\nempty set: -1\ntie goes to lower index: 0\ninvalid: ValueError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
