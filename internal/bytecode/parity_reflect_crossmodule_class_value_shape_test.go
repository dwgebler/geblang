package bytecode_test

import "testing"

const reflectCrossShapeDonor = `module crossshapedonor;
export interface DonorGreeter {
    func greet(): string;
}
/** Donor base doc. */
@Deprecated
export class DonorBase implements DonorGreeter {
    string name;
    func DonorBase(string name) { this.name = name; }
    func greet(): string { return "hi ${this.name}"; }
    static func make(): DonorBase { return DonorBase("made"); }
}
export class DonorMid extends DonorBase {
    func DonorMid(string name) { parent(name); }
    func extra(): string { return "extra"; }
}
`

// (d) a cross-module class value held in a variable: reflect.* must agree with the evaluator for methods/staticMethods/interfaces/parent/decorators/docs/location.
func TestParityReflectClassValueShapeCrossModuleVariable(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"crossshapedonor": reflectCrossShapeDonor,
	}, `import io;
import reflect;
import crossshapedonor;
let c = crossshapedonor.DonorMid;
io.println("methods:${reflect.methods(c)}");
io.println("staticMethods:${reflect.staticMethods(c)}");
io.println("interfaces:${reflect.interfaces(c)}");
io.println("parent:${reflect.parent(c)}");
io.println("className:${reflect.className(c)}");
io.println("decorators:${reflect.decorators(c)}");
io.println("docs:${reflect.docs(c)}");
`,
		"methods:[\"extra\"]\n"+
			"staticMethods:[]\n"+
			"interfaces:[]\n"+
			"parent:DonorBase\n"+
			"className:DonorMid\n"+
			"decorators:[]\n"+
			"docs:null\n")
}

// (d) a cross-module BASE class value (with its own decorators/docs/statics) held in a variable.
func TestParityReflectClassValueShapeCrossModuleVariableBase(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"crossshapedonor": reflectCrossShapeDonor,
	}, `import io;
import reflect;
import crossshapedonor;
let c = crossshapedonor.DonorBase;
io.println("methods:${reflect.methods(c)}");
io.println("staticMethods:${reflect.staticMethods(c)}");
io.println("interfaces:${reflect.interfaces(c)}");
io.println("parent:${reflect.parent(c)}");
io.println("decorators:${reflect.decorators(c)}");
io.println("docs:${reflect.docs(c)}");
io.println("location:${reflect.location(c)}");
`,
		"methods:[\"greet\"]\n"+
			"staticMethods:[\"make\"]\n"+
			"interfaces:[\"DonorGreeter\"]\n"+
			"parent:null\n"+
			"decorators:[{\"args\": [], \"column\": 1, \"line\": 6, \"name\": \"Deprecated\", \"namedArgs\": {}, \"overload\": 0, \"position\": 0, \"target\": \"class\"}]\n"+
			"docs:{\"body\": \"\", \"lines\": [\"Donor base doc.\"], \"summary\": \"Donor base doc.\", \"text\": \"Donor base doc.\"}\n"+
			"location:{\"column\": 8, \"line\": 7, \"module\": \"crossshapedonor\"}\n")
}
