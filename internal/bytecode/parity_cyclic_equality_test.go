package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const cyclicEqualityProgram = `import io;

class Node {
    string name;
    ?Node next = null;
    func Node(string name) { this.name = name; }
}

class Always {
    func __eq(any other): bool { return false; }
}

func ring(string a, string b): Node {
    let first = Node(a);
    first.next = Node(b);
    first.next.next = first;
    return first;
}

let self = Node("a");
self.next = self;
list<any> l = [1];
l.push(l);
list<any> m = [1];
m.push(m);
dict<string, any> d = {"k": 1};
d["me"] = d;
set<any> s = [1, 2] as set<any>;
let w = Always();
io.println([self == self, ring("x", "y") == ring("x", "y"), ring("x", "y") == ring("x", "z")]);
io.println([l == l, l == m, [l].contains(m), [l].indexOf(m)]);
io.println([d == d, s == s, w == w, w != w]);
`

func TestCyclicStructuralEqualityParity(t *testing.T) {
	ev, vm := parityOutputs(t, cyclicEqualityProgram)

	want := "[true, true, false]\n[true, true, true, 0]\n[true, true, false, true]\n"
	if ev != want || vm != want {
		t.Fatalf("cyclic equality mismatch\nevaluator: %q\nvm:        %q\nwant:      %q", ev, vm, want)
	}
}

func TestCyclicEqualityAcrossModulesParity(t *testing.T) {
	dir := t.TempDir()
	module := `module links;

export class Link {
    string label;
    ?Link next = null;
    func Link(string label) { this.label = label; }
}
`
	if err := os.WriteFile(filepath.Join(dir, "links.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import links;

class TaggedLink extends links.Link {
    string tag;
    func TaggedLink(string label, string tag) { parent(label); this.tag = tag; }
}

func loop(string tag): TaggedLink {
    let t = TaggedLink("a", tag);
    t.next = TaggedLink("b", tag);
    t.next.next = t;
    return t;
}

let base = links.Link("solo");
base.next = base;
io.println([base == base, loop("x") == loop("x"), loop("x") == loop("y")]);
`)

	want := "[true, true, false]\n"
	if ev != want || vm != want {
		t.Fatalf("cross-module cyclic equality mismatch\nevaluator: %q\nvm:        %q\nwant:      %q", ev, vm, want)
	}
}
