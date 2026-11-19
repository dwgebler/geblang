package bytecode_test

import "testing"

func TestStringTrimSetAndPartialCaseParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

class Label {
    string text;

    func Label(string text) {
        this.text = text;
    }

    func tidy(): string {
        return this.text.trim("/-").upper(1);
    }
}

func edges(string s): string {
    return s.trimStart("*") + "|" + s.trimEnd("*");
}

io.println("/compare/go/".trim("/"));
io.println(edges("**a**"));
io.println(Label("/-release-notes-/").tidy());
io.println("geblang".upper(-4) + " " + "HELLO".lower(2) + " " + "éa".upper(1));
any v = "xxhixx";
io.println(v.trim("x") + v.upper(99));
try {
    io.println("a".upper("1"));
} catch (Error e) {
    io.println("error");
}
`)
	want := "compare/go\na**|**a\nRelease-notes\ngebLANG heLLO Éa\nhiXXHIXX\nerror\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
