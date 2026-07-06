package bytecode_test

import "testing"

// classValueDonor is a cross-module base with a value accessor, hydrated via a subclass constructor.
const classValueDonor = `module cvmod;
export class Plain {
    int amount;
    func Plain(int amount) { this.amount = amount; }
    func value(): int { return this.amount; }
}
`

// A plain subclass referenced as a value in json.parseAs, with a cross-module parent, hydrates identically on both backends.
func TestParityCrossModuleClassAsValuePlainDeserialize(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"cvmod": classValueDonor},
		"import io;\nimport json;\nimport cvmod;\nclass Child extends cvmod.Plain {\n    func Child(int amount) { parent(amount); }\n}\nlet d = json.parseAs(\"{\\\"amount\\\": 7}\", Child);\nio.println(d.value());\n",
		"7\n")
}

// A decorated subclass used as a value (the decoratedClasses load-site branch) hydrates identically on both backends.
func TestParityCrossModuleClassAsValueDecoratedDeserialize(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"cvmod": classValueDonor},
		"import io;\nimport json;\nimport cvmod;\nfunc tagClass(any cls): any { return cls; }\n@tagClass\nclass Child extends cvmod.Plain {\n    func Child(int amount) { parent(amount); }\n}\nlet d = json.parseAs(\"{\\\"amount\\\": 9}\", Child);\nio.println(d.value());\n",
		"9\n")
}
