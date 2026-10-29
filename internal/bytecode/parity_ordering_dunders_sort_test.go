package bytecode_test

import "testing"

func TestParityOrderingDundersDriveSortingAndRanking(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"dunder_fixture.gb": "module dunder_fixture;\nexport class Money {\n    int p;\n    func Money(int p) { this.p = p; }\n    func __lt(Money o): bool { return this.p < o.p; }\n    func __eq(?Money o): bool { return o != null && this.p == o.p; }\n    func __string(): string { return \"${this.p}\"; }\n}\n",
	}, `import io;
import collections;
import dunder_fixture;
class Tip extends dunder_fixture.Money { func Tip(int p) { parent(p); } }
class Weight {
    int g;
    func Weight(int g) { this.g = g; }
    func __gt(Weight o): bool { return this.g > o.g; }
    func __string(): string { return "${this.g}g"; }
}
class Plain {}
func show(list<any> xs): string { return "${[x as string for x in xs]}"; }
let ms = [dunder_fixture.Money(5), dunder_fixture.Money(1), dunder_fixture.Money(3)];
io.println(show(ms.sorted()));
io.println(show(collections.sort(ms)));
io.println(show([Tip(9), Tip(2)].sorted()));
io.println(show([Weight(7), Weight(3)].sorted()));
io.println(show(ms.topK(2)));
let sortedMs = ms.sorted();
io.println("${sortedMs.binarySearch(dunder_fixture.Money(3))} ${sortedMs.lowerBound(dunder_fixture.Money(2))}");
try { [Plain(), Plain()].sorted(); } catch (TypeError e) { io.println(e.message); }
`)
	want := "[\"1\", \"3\", \"5\"]\n[\"1\", \"3\", \"5\"]\n[\"2\", \"9\"]\n[\"3g\", \"7g\"]\n[\"5\", \"3\"]\n1 1\ncannot compare Plain and Plain\n"
	if ev != want || vm != want {
		t.Errorf("ordering dunders in sorting:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
