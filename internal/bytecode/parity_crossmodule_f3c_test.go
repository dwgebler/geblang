package bytecode_test

import "testing"

// reflect.parameters/returnType on a function value crossing a module boundary must keep names, types, hasDefault, and the return type (the bridge wrapper stripped them on the VM).
func TestParityCrossModuleFunctionValueMetadata(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"insp": `module insp;
import reflect;
export func describe(any fn): string {
    let ps = reflect.parameters(fn);
    let out = "count=${ps.length()} ret=${reflect.returnType(fn)}";
    for (p in ps) {
        let d = p as dict<string, any>;
        out = out + " |${d["name"]}:${d["type"]}:${d["hasDefault"]}";
    }
    return out;
}
`,
	}, `import io;
import insp;
func greet(string name, int times = 1): string { return name; }
io.println(insp.describe(greet));
`,
		"count=2 ret=string |name:string:false |times:int:true\n")
}

// A cross-module named call omitting a middle defaulted parameter must engage that default (the VM raised "missing argument" because positional ordering cannot express a middle hole).
func TestParityCrossModuleNamedDefaultBridgedFunction(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"caller": `module caller;
export func invoke(any handler, dict<string, any> named): any { return handler(...named); }
`,
	}, `import io;
import caller;
func q(string q = "dq", string product = "dp", string tail = "dt"): string {
    return "q=${q} product=${product} tail=${tail}";
}
io.println(caller.invoke(q, {}));
io.println(caller.invoke(q, {"product": "x"}));
io.println(caller.invoke(q, {"q": "A", "tail": "T"}));
`,
		"q=dq product=dp tail=dt\nq=dq product=x tail=dt\nq=A product=dp tail=T\n")
}

// Same middle-default binding through a reflect.method callable on a cross-module instance.
func TestParityCrossModuleNamedDefaultReflectMethod(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"caller": `module caller;
import reflect;
export func invoke(any instance, string name, dict<string, any> named): any {
    let m = reflect.method(instance, name);
    return m(...named);
}
`,
	}, `import io;
import caller;
class Ctrl {
    func run(string q = "dq", string product = "dp"): string { return "q=${q} product=${product}"; }
}
let c = Ctrl();
io.println(caller.invoke(c, "run", {"product": "x"}));
io.println(caller.invoke(c, "run", {}));
`,
		"q=dq product=x\nq=dq product=dp\n")
}

// reflect.fields on a cross-module instance must preserve non-scalar decorator arg values (a decimal or list arrived as null on the VM via a lossy AST round-trip).
func TestParityCrossModuleFieldDecoratorArgs(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"insp": `module insp;
import reflect;
export func dump(any instance): string {
    let out = "";
    for (field in reflect.fields(instance)) {
        let f = field as dict<string, any>;
        if (f.contains("decorators")) {
            for (d in f["decorators"] as list<any>) {
                let dd = d as dict<string, any>;
                out = out + "${f["name"]}=${dd["name"]}:${dd["args"]} ";
            }
        }
    }
    return out;
}
`,
	}, `import io;
import insp;
class Model {
    @Assert.greaterThan(18.0)
    ?int value;
    @Assert.choice(["a", "b", "c"])
    ?string other;
}
io.println(insp.dump(Model()));
`,
		"other=Assert.choice:[[\"a\", \"b\", \"c\"]] value=Assert.greaterThan:[18.0000000000] \n")
}

// reflect.fields on a cross-module instance must report declared field types (they came back as "any" on the VM).
func TestParityCrossModuleInstanceFieldTypes(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"insp": `module insp;
import reflect;
export func types(any instance): string {
    let out = "";
    for (field in reflect.fields(instance)) {
        let f = field as dict<string, any>;
        out = out + "${f["name"]}:${f["type"]} ";
    }
    return out;
}
`,
	}, `import io;
import insp;
class Tag { string name; }
class Profile {
    string handle;
    list<Tag> tags;
}
io.println(insp.types(Profile()));
`,
		"handle:string tags:list<Tag> \n")
}

// A method call on a base-typed parameter must dispatch on the runtime class; the compiler devirtualized an exported base class's method and skipped a cross-module override.
func TestParityCrossModuleVirtualDispatchExportedBase(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"base": `module base;
export class Mailable {
    func Mailable() {}
    func subject(): string { return ""; }
}
export func readSubject(Mailable mail): string { return mail.subject(); }
`,
	}, `import io;
import base;
class WelcomeMail extends base.Mailable {
    string name;
    func WelcomeMail(string name) { parent(); this.name = name; }
    func subject(): string { return "Welcome, " + this.name; }
}
io.println(base.readSubject(WelcomeMail("Ada")));
`,
		"Welcome, Ada\n")
}

// reflect.interfaces must return the bare interface name for a cross-module interface (the VM returned the import-qualified name).
func TestParityCrossModuleInterfacesBareName(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"contracts": `module contracts;
export interface Repository<T> {
    func find(string id): ?T;
}
`,
	}, `import io;
import reflect;
import contracts as repo;
class User { string id = ""; }
class UserRepo implements repo.Repository<User> {
    func find(string id): ?User { return null; }
}
io.println("${reflect.interfaces(UserRepo)}");
`,
		"[\"Repository\"]\n")
}

// reflect.class(name) from a module that does not declare the name must resolve the user's entry-chunk class, not a same-named loaded module class.
func TestParityCrossModuleClassByNameEntryPreference(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"donor": `module donor;
export class Profile {
    any _snap;
    dict<string, any> _delta;
}
`,
		"probe": `module probe;
import reflect;
import donor;
export func fields(): string {
    let cls = reflect.class("Profile");
    let out = "";
    for (f in reflect.fields(cls)) {
        let fd = f as dict<string, any>;
        out = out + "${fd["name"]} ";
    }
    return out;
}
`,
	}, `import io;
import probe;
class Tag { string name; }
class Profile {
    string handle;
    list<Tag> tags;
}
io.println(probe.fields());
`,
		"handle tags \n")
}
