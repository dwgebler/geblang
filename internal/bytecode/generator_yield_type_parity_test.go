package bytecode_test

import "testing"

func TestGeneratorYieldTypeParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Holder<T> {
    func Holder() {}
    func wrong(): generator<T> { list raw = [5]; yield raw[0]; }
}

func words(list raw): generator<string> { for (x in raw) { yield x; } }
func ratios(): generator<float> { yield 2; }
func anything(list raw): generator<any> { for (x in raw) { yield x; } }
func maybe(list raw): iterable<?int> { for (x in raw) { yield x; } }

func show(any source) {
    try {
        list out = [];
        for (x in source) { out.push("${x}:${typeof(x)}"); }
        io.println(out);
    } catch (TypeError e) {
        io.println(e.message);
    }
}

show(words(["a", "b"]));
show(words(["a", 1]));
show(ratios());
show(anything(["a", 1]));
show(maybe([null, 2]));
show(maybe(["x"]));
show(Holder<string>().wrong());
show(Holder<int>().wrong());
func make = func(list raw): generator<int> { for (x in raw) { yield x; } };
show(make(["s"]));
`)
	want := "[\"a:string\", \"b:string\"]\n" +
		"words expects string yield, got int\n" +
		"[\"2:float\"]\n" +
		"[\"a:string\", \"1:int\"]\n" +
		"[\"null:null\", \"2:int\"]\n" +
		"maybe expects ?int yield, got string\n" +
		"Holder.wrong expects T yield, got int\n" +
		"[\"5:int\"]\n" +
		"<closure> expects int yield, got string\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
