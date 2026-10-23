package bytecode_test

import "testing"

// Collections typed with an imported interface (list<shapes.Shape>) accept
// values whose class implements it. The VM records an imported interface
// under its qualified name while element tags compare bare names, so push
// used to refuse every implementing class (1.37.7).

func ifaceCollectionsDonor() string {
	return "module shapes;\n" +
		"export interface Shape { func area(): int; }\n" +
		"export class Square implements Shape { func area(): int { return 4; } }\n"
}

func TestParityCrossModuleInterfaceListPush(t *testing.T) {
	ok := t.Run("both backends", func(t *testing.T) {
		runMultiModuleParity(t, map[string]string{"shapes": ifaceCollectionsDonor()},
			`import io;
import shapes as sh;
class Tri implements sh.Shape { func area(): int { return 3; } }
list<sh.Shape> l = [];
l.push(sh.Square());
l.push(Tri());
sh.Shape one = sh.Square();
l.push(one);
dict<string, sh.Shape> d = {};
d["t"] = Tri();
set<sh.Shape> s = {};
s.add(sh.Square());
int total = 0;
for (x in l) {
    total += x.area();
}
io.println(total);
io.println(d["t"].area());
io.println(s.length());
`, "11\n3\n1\n")
	})
	if !ok {
		t.Fatal("collection output diverged across backends")
	}
}

func TestParityCrossModuleInterfaceListRejectsNonImplementer(t *testing.T) {
	ok := t.Run("both backends", func(t *testing.T) {
		runMultiModuleParity(t, map[string]string{"shapes": ifaceCollectionsDonor()},
			`import io;
import shapes;
class Plain { func area(): int { return 1; } }
list<shapes.Shape> l = [];
any p = Plain();
try {
    l.push(p);
} catch (TypeError e) {
    io.println(e.message);
}
`, "cannot push Plain to list<shapes.Shape>\n")
	})
	if !ok {
		t.Fatal("type error diverged across backends")
	}
}
