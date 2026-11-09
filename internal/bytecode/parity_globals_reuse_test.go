package bytecode_test

import "testing"

func writeGlobalsReuseModules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeModule(t, dir, "reusedonor", `module reusedonor;
let seen = 0;
let items = [1];
export func run(callable cb): any { return cb(); }
export func peek(): int { return seen; }
export func setSeen(int n): void { seen = n; }
export func itemsNow(): list<int> { return items; }
export func swapItems(): void { items = [7, 8]; }
`)
	return dir
}

const globalsReuseProgram = `import io;
import reflect;
import reusedonor as donor;

int factor = 1;
list<string> tags = ["a"];
dict<string, any> registry = {};

func readFactor(): int { return factor; }
func bumpFactor(): int { factor = factor + 10; return factor; }
func readTags(): string { return tags.join(","); }

func register(any cls): any {
    registry[reflect.className(cls)] = factor;
    return cls;
}

class Reader {
    int offset = 100;
    func read(): int { return factor + this.offset; }
}

io.println(donor.run(readFactor));
io.println(donor.run(readFactor));

@register
class Late {
    func value(): int { return factor * 2; }
}

func main(): void {
    io.println(donor.run(readFactor));
    factor = 5;
    io.println(donor.run(readFactor));
    io.println(donor.run(func(): int { return factor + 1; }));
    io.println(donor.run(func(): int { return factor + 1; }));
    let r = Reader();
    io.println(donor.run(r.read));
    factor = 6;
    io.println(donor.run(r.read));
    io.println(donor.run(bumpFactor));
    io.println(factor);
    io.println(donor.run(readFactor));
    io.println(donor.run(readTags));
    tags = ["b", "c"];
    io.println(donor.run(readTags));
    tags.push("d");
    io.println(donor.run(readTags));
    io.println(donor.run(func(): int { return Late().value(); }));
    io.println(registry["Late"]);
    io.println(donor.peek());
    io.println(donor.peek());
    donor.setSeen(4);
    io.println(donor.peek());
    io.println(donor.peek());
    io.println(donor.itemsNow());
    donor.swapItems();
    io.println(donor.itemsNow());
}

main();
`

func TestParityGlobalsVisibleAcrossReusedWorkers(t *testing.T) {
	dir := writeGlobalsReuseModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, globalsReuseProgram)
	want := "1\n1\n1\n5\n6\n6\n105\n106\n16\n16\n16\na\nb,c\nb,c,d\n32\n1\n0\n0\n4\n4\n[1]\n[7, 8]\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != ev {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
}
