package bytecode_test

import "testing"

const constraintsDonorGb = `module inventory;
export interface Entity {
    string id;
}
export class Repo<T implements Entity> {
    dict<string, T> items = {};
    func save(T e): void { this.items[e.id] = e; }
    func find(string id): ?T { return this.items.get(id); }
}
export func firstId<T implements Entity>(list<T> xs): string { return xs[0].id; }
`

func TestParityGenericConstraintsAcceptTypesFromOtherModules(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"inventory.gb": constraintsDonorGb}, `import io;
import inventory;
class Thing implements inventory.Entity { func Thing(string id) { this.id = id; } }
class SubThing extends Thing { func SubThing(string id) { parent(id); } }
class Plain { string id = "p"; }
let repo = inventory.Repo<SubThing>();
repo.save(SubThing("s1"));
io.println(repo.find("s1")?.id);
io.println(inventory.firstId([Thing("a")]));
try { inventory.Repo<Plain>(); } catch (TypeError e) { io.println(e.message); }
try { inventory.firstId([Plain()]); } catch (TypeError e) { io.println(e.message); }
`)
	want := "s1\na\n" +
		"type Plain does not satisfy constraint Entity for type parameter T\n" +
		"type Plain does not satisfy constraint Entity for type parameter T\n"
	if ev != want || vm != want {
		t.Errorf("cross-module constraints:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityClassConstraintsCheckedAtConstruction(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
interface E { func k(): int; }
class Ok implements E { func k(): int { return 1; } }
class Plain {}
class NoCtor<T implements E> { list<T> xs = []; }
class WithCtor<T implements E> {
    T item;
    func WithCtor(T item) { io.println("ctor ran"); this.item = item; }
}
class Holder<T implements E> { func Holder() { io.println("holder ctor"); } }
class PlainHolder extends Holder<Plain> { func PlainHolder() { parent(); } }
func attempt(callable f): void {
    try { f(); io.println("accepted"); } catch (TypeError e) { io.println("rejected"); }
}
attempt(func(): void { NoCtor<Ok>(); });
attempt(func(): void { NoCtor<Plain>(); });
attempt(func(): void { WithCtor(Plain()); });
attempt(func(): void { WithCtor<Plain>(Plain()); });
attempt(func(): void { PlainHolder(); });
`)
	want := "accepted\nrejected\nrejected\nrejected\nrejected\n"
	if ev != want || vm != want {
		t.Errorf("construction-time constraints:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
