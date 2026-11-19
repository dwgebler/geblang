package bytecode_test

import "testing"

func TestNullableElementInstanceofParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

list<?int> a = [1, null];
list<int> b = [1, 2];
set<?string> c = {"x"};
dict<string, ?int> d = {"k": null};
any va = a;
any vb = b;
any vc = c;
any vd = d;
any raw = [1, null];
any ints = [1, 2];
any rawDict = {"k": null};
io.println("${va instanceof list<int>} ${va instanceof list<?int>} ${va instanceof list<any>}");
io.println("${vb instanceof list<int>} ${vb instanceof list<?int>}");
io.println("${vc instanceof set<string>} ${vc instanceof set<?string>}");
io.println("${vd instanceof dict<string, int>} ${vd instanceof dict<string, ?int>}");
io.println("${raw instanceof list<int>} ${raw instanceof list<?int>} ${ints instanceof list<?int>}");
io.println("${rawDict instanceof dict<string, ?int>} ${rawDict instanceof dict<string, int>}");
`)
	want := "false true true\ntrue true\nfalse true\nfalse true\nfalse true true\ntrue false\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
