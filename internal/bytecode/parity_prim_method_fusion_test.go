package bytecode_test

import (
	"testing"
)

const primFusionDonor = `module prim_fusion_donor;

let names = ["x", "y"];
let table = {"a": 7};

export func sizes(): int { return names.length() + table.length(); }
export func lookup(string k): any { return table.get(k); }
`

const primFusionMain = `import io;
import prim_fusion_donor as donor;
class Sized { func Sized() {} func length(): int { return 42; } func get(any k): any { return "custom ${k}"; } func contains(any k): bool { return true; } func isEmpty(): bool { return false; } }
let s = "héllo";
let xs = [1, 2, 3];
let d = {"a": 1};
let st = {1, 2};
let b = "abc" as bytes;
let obj = Sized();
let nothing = null;
let key = "a";
io.println(s.length());
io.println(xs.length());
io.println(d.get("a"));
io.println(d.get(key));
io.println(d.get("missing"));
io.println(xs.get(1));
io.println(xs.get(10));
io.println(xs.contains(2));
io.println(st.contains(3));
io.println(b.length());
io.println(s.isEmpty());
io.println(obj.length());
io.println(obj.get("k"));
io.println(obj.contains(1));
func local(): void {
    let ls = "ab";
    let ld = {"x": 5};
    let lo = Sized();
    io.println(ls.length());
    io.println(ld.get("x"));
    io.println(lo.length());
    io.println(lo.get(key));
}
local();
try { io.println(nothing.length()); } catch (Error e) { io.println("err: " + e.message); }
try { io.println(d.get(1, 2)); } catch (Error e) { io.println("err2: " + e.message); }
io.println(xs.get(xs.length() - 1));
io.println(donor.sizes());
io.println(donor.lookup("a"));
io.println(donor.lookup("zz"));
`

const primFusionWant = `5
3
1
1
null
2
null
true
false
3
false
42
custom k
true
2
5
42
custom a
err: unknown method null.length
err2: dict.get expects one argument
3
3
7
null
`

func TestParityPrimitiveMethodSlotReceivers(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"prim_fusion_donor.gb": primFusionDonor}, primFusionMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != primFusionWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
