package bytecode_test

import "testing"

const intStringCastProgram = `import io;

func viaGeneric<T>(T v): string { return v as string; }

class Labeller {
    int base;
    func Labeller(int base) { this.base = base; }
    func label(int n): string { return "k" + ((this.base + n) as string); }
}

int top = 9223372036854775807;
int bottom = 0 - top - 1;
io.println(0 as string);
io.println((0 - 7) as string);
io.println(255 as string);
io.println(top as string);
io.println(bottom as string);
io.println((top + 1) as string);
io.println((bottom - 1) as string);
string out = "";
for (int i = 8; i < 12; i++) { out = out + "k" + (i as string) + ";"; }
io.println(out);
io.println(viaGeneric<int>(42));
io.println(Labeller(10).label(5));
io.println(typeof(12 as string));
io.println(true as string);
io.println(1.5f as string);
`

const intStringCastWant = `0
-7
255
9223372036854775807
-9223372036854775808
9223372036854775808
-9223372036854775809
k8;k9;k10;k11;
42
k15
string
true
1.5
`

func TestParityIntStringCast(t *testing.T) {
	ev, vm := parityOutputs(t, intStringCastProgram)
	if ev != vm {
		t.Fatalf("backends diverge\nevaluator:\n%s\nvm:\n%s", ev, vm)
	}
	if vm != intStringCastWant {
		t.Fatalf("unexpected output\ngot:\n%s\nwant:\n%s", vm, intStringCastWant)
	}
}
