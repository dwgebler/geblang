package bytecode_test

import "testing"

func TestIterableParametersAcceptCollectionsParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import streams;

class Bag {
    func Bag() {}
    func __iter(): any { return [1, 2]; }
}
class Steps {
    int n = 0;
    func Steps() {}
    func __next(): any { this.n += 1; return this.n; }
    func __done(): bool { return this.n >= 3; }
}
func raw(any x): any { return x; }
func gen(): generator<int> { yield 1; }
func count(iterable<int> xs): int {
    int n = 0;
    for (x in xs) { n += 1; }
    return n;
}
func chars(iterable<string> xs): int {
    int n = 0;
    for (x in xs) { n += 1; }
    return n;
}
func asIter(): iterable<int> { return {5, 6}; }

func show(string label, func body) {
    try { io.println(label + ": " + (body() as string)); } catch (TypeError e) { io.println(label + ": " + e.message); }
}
show("generator", () => count(gen()));
show("list", () => count([1, 2, 3]));
show("set", () => count({1, 2}));
show("range", () => count(1..4));
show("dict", () => chars(raw({"a": 1})));
show("string", () => chars("abc"));
show("bag", () => count(Bag()));
show("steps", () => count(Steps()));
show("stream", () => count(streams.of([1, 2])));
show("return", () => [v for v in asIter()].length());
show("wrongElems", () => count(raw(["a"])));
show("rangeAsString", () => chars(raw(1..2)));
show("number", () => count(raw(5)));
show("null", () => count(raw(null)));
`)
	want := "generator: 1\nlist: 3\nset: 2\nrange: 4\n" +
		"dict: chars expects iterable<string> for parameter 'xs', got dict<string,int>\n" +
		"string: 3\nbag: 2\nsteps: 3\nstream: 2\nreturn: 2\n" +
		"wrongElems: count expects iterable<int> for parameter 'xs', got list (element at index 0 is string)\n" +
		"rangeAsString: chars expects iterable<string> for parameter 'xs', got range\n" +
		"number: count expects iterable<int> for parameter 'xs', got int\n" +
		"null: count expects iterable<int> for parameter 'xs', got null\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
