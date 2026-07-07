package bytecode_test

import "testing"

// dir() on a class value lists fields + lowercased method/static names + consts up the parent chain (returned empty on the VM).
func TestParityDirLocalClassValue(t *testing.T) {
	runParity(t, `import io;
class Base {
    static const VERSION = "1.0";
    int value;
    string label;
    func Base() { this.value = 0; this.label = "b"; }
    func greetUser(): string { return "hi"; }
    static func makeBase(): Base { return Base(); }
}
class Sub extends Base {
    int extra;
    func Sub() { parent(); this.extra = 1; }
    func doBonus(): int { return this.extra; }
    static func buildSub(): Sub { return Sub(); }
}
io.println(dir(Base));
io.println(dir(Sub));
let s = Sub();
io.println(dir(s));
`, "[\"VERSION\", \"greetuser\", \"label\", \"makebase\", \"value\"]\n"+
		"[\"VERSION\", \"buildsub\", \"dobonus\", \"extra\", \"greetuser\", \"label\", \"makebase\", \"value\"]\n"+
		"[\"dobonus\", \"extra\", \"greetuser\", \"label\", \"value\"]\n")
}

// dir() on a class value held in a variable rehydrates the metadata-poor constant.
func TestParityDirClassValueViaVariable(t *testing.T) {
	runParity(t, `import io;
class Box {
    static const TAG = "b";
    int size;
    func Box() { this.size = 0; }
    func fill(): void {}
}
let alias = Box;
io.println(dir(alias));
`, "[\"TAG\", \"fill\", \"size\"]\n")
}
