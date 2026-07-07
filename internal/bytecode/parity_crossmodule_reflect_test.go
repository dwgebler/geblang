package bytecode_test

import "testing"

// Donor modules: a foreign parent, and a decorated subclass in another module (reflect.methods on the cross-module class value returned empty on the VM in 1.32.0).
const reflectCtlBase = `module ctlbase;
export class BaseCtl {
    func BaseCtl() {}
    func shared(): string { return "s"; }
}
`

const reflectRoutes = `module routes;
export func Get(string path): dict<string, any> { return {"__route": true, "path": path}; }
`

const reflectCtl = `module ctl;
import ctlbase;
import routes;
export class Home extends ctlbase.BaseCtl {
    func Home() { parent(); }
    @routes.Get("/")
    func index(): string { return "i"; }
    @routes.Get("/about")
    func about(): string { return "a"; }
    static func mk(): Home { return Home(); }
}
`

// reflect.methods/staticMethods/constructors + the decorator route-walk on a class from a CROSS-MODULE instance must be byte-identical (empty on the VM in 1.32.0).
func TestParityCrossModuleReflectClassOfInstance(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"ctlbase": reflectCtlBase,
		"routes":  reflectRoutes,
		"ctl":     reflectCtl,
	}, `import io;
import reflect;
import ctl;
let c = ctl.Home();
let cls = reflect.class(c);
io.println("methods=${reflect.methods(cls).sorted()}");
io.println("statics=${reflect.staticMethods(cls).sorted()}");
io.println("ctors=${reflect.constructors(cls).length()}");
io.println("methodNull=${reflect.method(c, "index") == null}");
int routes = 0;
for (name in reflect.methods(cls)) {
    let h = reflect.method(c, name as string);
    for (d in reflect.decorators(h)) {
        let dm = d as dict<string, any>;
        if ((dm["name"] as string).endsWith("Get")) { routes = routes + 1; }
    }
}
io.println("routes=${routes}");
`,
		"methods=[\"about\", \"index\"]\nstatics=[\"mk\"]\nctors=1\nmethodNull=false\nroutes=2\n")
}

// A method from a cross-module instance is invocable; a parentless cross-module class reflects its own methods.
func TestParityCrossModuleReflectMethodInvocable(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"ctlbase": reflectCtlBase,
		"routes":  reflectRoutes,
		"ctl":     reflectCtl,
	}, `import io;
import reflect;
import ctl;
import ctlbase;
let c = ctl.Home();
let m = reflect.method(c, "index");
io.println("call=${m()}");
let b = ctlbase.BaseCtl();
io.println("baseMethods=${reflect.methods(reflect.class(b)).sorted()}");
`,
		"call=i\nbaseMethods=[\"shared\"]\n")
}

// reflect.class resolved BY NAME also carries the full method metadata cross-module.
func TestParityCrossModuleReflectClassByName(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"ctlbase": reflectCtlBase,
		"routes":  reflectRoutes,
		"ctl":     reflectCtl,
	}, `import io;
import reflect;
import ctl;
let cls = reflect.class("Home");
io.println("methods=${reflect.methods(cls).sorted()}");
io.println("statics=${reflect.staticMethods(cls).sorted()}");
`,
		"methods=[\"about\", \"index\"]\nstatics=[\"mk\"]\n")
}
