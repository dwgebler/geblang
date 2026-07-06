package bytecode_test

import "testing"

// iterDonor exports a __next/__done range iterator and an __iter-based wrapper.
const iterDonor = `module iterdonor;
export class Range {
    int cur;
    int stop;
    func Range(int start, int stop) { this.cur = start; this.stop = stop; }
    func __next(): int { let v = this.cur; this.cur = this.cur + 1; return v; }
    func __done(): bool { return this.cur >= this.stop; }
}
export class Wrapper {
    Range r;
    func Wrapper(int n) { this.r = Range(0, n); }
    func __iter(): Range { return this.r; }
}
`

// A local subclass of a cross-module __next/__done iterator is iterable on both backends; the VM previously errored "not iterable" because the hook lookup missed the cross-module parent.
func TestParityIterCrossModuleInheritedNextDone(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nclass MyRange extends iterdonor.Range {\n    func MyRange(int n) { parent(0, n); }\n}\nfor (x in MyRange(3)) { io.println(x); }\n",
		"0\n1\n2\n")
}

// A two-level local subclass still inherits the cross-module iterator dunders.
func TestParityIterCrossModuleInheritedTwoLevel(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nclass A extends iterdonor.Range {\n    func A(int n) { parent(0, n); }\n}\nclass B extends A {\n    func B(int n) { parent(n); }\n}\nfor (x in B(4)) { io.println(x); }\n",
		"0\n1\n2\n3\n")
}

// A local subclass that inherits a cross-module __iter() iterates through the returned iterator.
func TestParityIterCrossModuleInheritedIter(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nclass MyWrap extends iterdonor.Wrapper {\n    func MyWrap(int n) { parent(n); }\n}\nfor (x in MyWrap(3)) { io.println(x); }\n",
		"0\n1\n2\n")
}

// Directly iterating a foreign __next/__done class (no local subclass) works on both backends.
func TestParityIterForeignDirectNextDone(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nfor (x in iterdonor.Range(0, 3)) { io.println(x); }\n",
		"0\n1\n2\n")
}

// Directly iterating a foreign __iter class works on both backends.
func TestParityIterForeignDirectIter(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nfor (x in iterdonor.Wrapper(3)) { io.println(x); }\n",
		"0\n1\n2\n")
}

// A local subclass that OVERRIDES the cross-module __next runs its own body (the local method still wins).
func TestParityIterCrossModuleOverrideNext(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"iterdonor": iterDonor},
		"import io;\nimport iterdonor;\nclass Doubler extends iterdonor.Range {\n    func Doubler(int n) { parent(0, n); }\n    func __next(): int { let v = this.cur; this.cur = this.cur + 1; return v * 10; }\n}\nfor (x in Doubler(3)) { io.println(x); }\n",
		"0\n10\n20\n")
}
