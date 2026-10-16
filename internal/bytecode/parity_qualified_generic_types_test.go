package bytecode_test

import "testing"

func TestParityQualifiedGenericTypeArgumentsFromOtherModules(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"boxes.gb": "module boxes;\nexport class Box<T> { list<T> xs = []; }\n",
		"goods.gb": "module goods;\nexport class Order { int n = 1; }\nexport class Refund { int n = 2; }\n",
	}, `import io;
import boxes as b;
import goods as g;
class Holder {
    b.Box<g.Order> orders;
    func Holder() { this.orders = b.Box<g.Order>(); }
    func swap(b.Box<g.Order> next): b.Box<g.Order> { this.orders = next; return next; }
}
let box = b.Box<g.Order>();
io.println(box instanceof b.Box<g.Order>);
io.println(box instanceof b.Box<g.Refund>);
let h = Holder();
io.println(h.swap(box) instanceof b.Box);
b.Box<g.Order> local = box;
try { h.orders = b.Box<g.Refund>(); } catch (TypeError e) { io.println("rejected"); }
`)
	want := "true\nfalse\ntrue\nrejected\n"
	if ev != want || vm != want {
		t.Errorf("qualified generic types:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
