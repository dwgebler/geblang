package bytecode_test

import "testing"

// reflect.class(instance) resolves to the instance's declaring-module class value, module-exactly, on both backends: it equals that module's own class value and not a same-named class from another module (finding 9.8).
func TestParityReflectClassModuleExact(t *testing.T) {
	donor := "export class Config { func Config() {} }\nexport func make(): any { return Config(); }\n"
	runMultiModuleParity(t, map[string]string{"moda": donor, "modb": donor},
		`import io;
import reflect;
import moda;
import modb;
let x = modb.make();
io.println(reflect.class(x) == modb.Config);
io.println(reflect.class(x) == moda.Config);
let y = moda.make();
io.println(reflect.class(y) == moda.Config);
io.println(reflect.class(y) == modb.Config);
`, "true\nfalse\ntrue\nfalse\n")
}
