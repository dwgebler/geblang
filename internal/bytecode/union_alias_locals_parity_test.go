package bytecode_test

import "testing"

func TestUnionAliasLocalsParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;

type IntOrString = int | string;
type MaybeNum = ?int | float;

func raw(any x): any { return x; }
func echo(IntOrString v): IntOrString {
    IntOrString local = v;
    return local;
}

IntOrString x = 5;
io.println(x);
x = "b";
io.println(x);
MaybeNum m = null;
io.println(m);
m = 2;
io.println(typeof(m));
io.println(echo("e"));
for (IntOrString item in [1, "two"]) { io.println(item); }
try { x = raw(1.5); } catch (TypeError e) { io.println(e.message); }
try { IntOrString y = raw(true); } catch (TypeError e) { io.println(e.message); }
try { MaybeNum z = raw("s"); } catch (TypeError e) { io.println(e.message); }
`)
	want := "5\nb\nnull\nint\ne\n1\ntwo\n" +
		"type error: cannot assign decimal to int | string\n" +
		"type error: cannot assign bool to int | string\n" +
		"type error: cannot assign string to ?int | float\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
