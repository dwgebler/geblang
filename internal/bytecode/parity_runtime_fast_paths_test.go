package bytecode_test

import "testing"

func TestParityRuntimeFastPaths(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, t.TempDir(), `import io;
import re;
import reflect;
func upper(string v): string {
    return v.upper();
}
class Tagged {
    @upper
    string code;
}
class TaggedChild extends Tagged {
    string extra;
}
let base = "x".repeat(300);
let one = base + "1";
let two = one + "2";
let branch = one + "3";
io.println("${one.length()} ${two.substring(299, 302)} ${branch.substring(299, 302)} ${base.length()}");
string s = "";
for (int i = 0; i < 3000; i++) {
    s = s + (i as string);
}
io.println(s.length());
let d = {};
for (int i = 0; i < 60; i++) {
    d["k" + (i as string)] = i;
}
for (int i = 0; i < 60; i++) {
    if (i % 3 != 0) {
        d.delete("k" + (i as string));
    }
}
d["k1"] = 100;
io.println(d.keys());
let row = {"a": 1, "b": 2, "c": 3};
io.println(row);
let fields = reflect.fields(TaggedChild, inherited: true);
(fields[0] as dict<string, any>)["name"] = "changed";
io.println((reflect.fields(TaggedChild, inherited: true)[0] as dict<string, any>)["name"]);
let child = TaggedChild();
child.code = "abc";
io.println(child.code);
int hits = 0;
for (int i = 0; i < 500; i++) {
    if (re.compile("[0-9]+").test("a" + (i as string))) {
        hits = hits + 1;
    }
}
io.println(hits);
io.println([1, 2, 3].length() + "abc".length() + ({"a": 1}.get("a") as int));
`)
	want := `301 x12 x13 300
10890
["k0", "k3", "k6", "k9", "k12", "k15", "k18", "k21", "k24", "k27", "k30", "k33", "k36", "k39", "k42", "k45", "k48", "k51", "k54", "k57", "k1"]
{"a": 1, "b": 2, "c": 3}
extra
ABC
500
7
`
	if ev != vm || ev != want {
		t.Fatalf("fast path mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
