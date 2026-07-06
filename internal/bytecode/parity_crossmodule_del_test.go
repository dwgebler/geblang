package bytecode_test

import "testing"

// delTypeDonor exports a plain no-destructor class; the 9.3 analyzer fix is guarded in analyzer_test.go and this pins the runtime del path.
const delTypeDonor = `module resmod;
export class Resource {
    string name;
    func Resource(string name) { this.name = name; }
}
`

// del of a binding whose inferred type is a cross-module class retires the binding and continues on both backends.
func TestParityDelCrossModuleTypedBinding(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"resmod": delTypeDonor},
		"import io;\nimport resmod;\nlet r = resmod.Resource(\"A\");\nio.println(\"before\");\ndel r;\nio.println(\"after\");\n",
		"before\nafter\n")
}

// del of a from-imported cross-module class type.
func TestParityDelCrossModuleFromImportType(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"resmod": delTypeDonor},
		"import io;\nfrom resmod import Resource;\nlet r = Resource(\"A\");\nio.println(\"before\");\ndel r;\nio.println(\"after\");\n",
		"before\nafter\n")
}
