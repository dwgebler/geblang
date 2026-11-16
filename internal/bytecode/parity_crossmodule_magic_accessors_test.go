package bytecode_test

import "testing"

const magicAccessorsDonor = `module magicdonor;

export class Magic {
    dict<string, any> bag;
    int real;

    func Magic() {
        this.bag = {"foo": 7};
        this.real = 3;
    }

    func __get(string name): any { return this.bag[name] ?? "none"; }
    func __set(string name, any value): void { this.bag[name] = "set:${value}"; }
    func hello(): string { return "hi"; }
    func viaThis(): any { return this.inner; }
}

export class Child extends Magic {
    func Child() { parent(); }
}

export class Same {
    dict<string, any> bag;

    func Same() { this.bag = {}; }
    func __get(string name): any { return "donor-get:" + name; }
    func __set(string name, any value): void { this.bag[name] = "donor-set"; }
    func dump(): string { return "${this.bag}"; }
}

export class Registry {
    static int declared = 1;
    static dict<string, any> seen = {};

    static func __getStatic(string name): any { return "static:" + name; }
    static func __setStatic(string name, any value): void { Registry.seen[name] = value; }
    static func recorded(string name): any { return Registry.seen[name] ?? null; }
}

export class SubRegistry extends Registry {}

export func touch(any target): any { return target.dyn; }
export func poke(any target): void { target.dyn = 5; }

export class Dyn {
    dict<string, any> d;

    func Dyn() { this.d = {}; }
    func __call(string name, list<any> args): any { return "call:" + name + ":" + (args.length() as string); }
    func __index(any key): any { return "idx:${key}"; }
    func __setIndex(any key, any value): void { this.d["${key}"] = value; }
    static func __callStatic(string name, list<any> args): any { return "scall:" + name; }
    func __string(): string { return "Dyn!"; }
    func __invoke(int x): int { return x + 1; }
    func known(): string { return "known"; }
}

export class DynKid extends Dyn {
    func DynKid() { parent(); }
}
`

// Magic accessors defined in another chunk were skipped on the VM: __get raised "has no field", __set wrote a raw field, and a same-named local class's accessor could run instead.
func TestParityCrossModuleMagicAccessors(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"magicdonor": magicAccessorsDonor}, `import io;
import magicdonor as donor;

class Same {
    dict<string, any> seen;
    func Same() { this.seen = {}; }
    func __get(string name): any { return "local-get:" + name; }
    func __set(string name, any value): void { this.seen[name] = "local-set"; }
    func dump(): string { return "${this.seen}"; }
}
class LocalChild extends donor.Magic {
    func LocalChild() { parent(); }
}
class LocalRegistry extends donor.Registry {}
class LocalStatic {
    static dict<string, any> seen = {};
    static func __getStatic(string name): any { return "local-static:" + name; }
    static func __setStatic(string name, any value): void { LocalStatic.seen[name] = value; }
}
func show(string label, callable thunk): void {
    try { io.println("${label}: ${thunk()}"); } catch (Error e) { io.println("${label}: ERR"); }
}
let m = donor.Magic();
show("get real", func(): any { return m.real; });
show("get magic", func(): any { return m.foo; });
show("get miss", func(): any { return m.zzz; });
show("set magic", func(): any { m.added = 9; return m.bag["added"]; });
show("set real", func(): any { m.real = 4; return m.real; });
show("method value", func(): any { let h = m.hello; return h(); });
show("inside method", func(): any { return m.viaThis(); });
let c = donor.Child();
show("child get", func(): any { return c.foo; });
show("child set", func(): any { c.x = 1; return c.bag["x"]; });
let lc = LocalChild();
show("local child get", func(): any { return lc.foo; });
show("local child set", func(): any { lc.x = 1; return lc.bag["x"]; });
show("local child declared", func(): any { lc.real = 8; return lc.real; });
let fs = donor.Same();
let ls = Same();
show("foreign same get", func(): any { return fs.q; });
show("foreign same set", func(): any { fs.q = 1; return fs.dump(); });
show("local same get", func(): any { return ls.q; });
show("local same set", func(): any { ls.q = 1; return ls.dump(); });
show("local via donor get", func(): any { return donor.touch(ls); });
show("local via donor set", func(): any { donor.poke(ls); return ls.dump(); });
show("static declared", func(): any { return donor.Registry.declared; });
show("static get", func(): any { return donor.Registry.anything; });
show("static set", func(): any { donor.Registry.dyn = 9; return donor.Registry.recorded("dyn"); });
show("static sub get", func(): any { return donor.SubRegistry.other; });
show("static sub set", func(): any { donor.SubRegistry.viaSub = 4; return donor.Registry.recorded("viaSub"); });
show("static local sub get", func(): any { return LocalRegistry.thing; });
show("static local sub set", func(): any { LocalRegistry.viaLocal = 6; return donor.Registry.recorded("viaLocal"); });
show("static value get", func(): any { let k = donor.Registry; return k.viaValue; });
show("static local value get", func(): any { let k = LocalStatic; return k.viaValue; });
show("static local value set", func(): any { let k = LocalStatic; k.dyn = 2; return LocalStatic.seen["dyn"]; });
class LocalDyn extends donor.Dyn { func LocalDyn() { parent(); } }
for (pair in [["dyn", donor.Dyn()], ["kid", donor.DynKid()], ["local", LocalDyn()]]) {
    string n = pair[0] as string;
    any o = pair[1];
    show(n + " call", func(): any { return o.missing(1, 2); });
    show(n + " known", func(): any { return o.known(); });
    show(n + " index", func(): any { return o["k"]; });
    show(n + " setIndex", func(): any { o["k"] = 3; return o.d["k"]; });
    show(n + " string", func(): any { return "${o}"; });
    show(n + " invoke", func(): any { return o(4); });
}
show("callStatic", func(): any { return donor.Dyn.nothing(1); });
show("callStatic kid", func(): any { return donor.DynKid.nothing(1); });
show("callStatic local", func(): any { return LocalDyn.nothing(1); });
`, `get real: 3
get magic: 7
get miss: none
set magic: set:9
set real: 4
method value: hi
inside method: none
child get: 7
child set: set:1
local child get: 7
local child set: set:1
local child declared: 8
foreign same get: donor-get:q
foreign same set: {"q": "donor-set"}
local same get: local-get:q
local same set: {"q": "local-set"}
local via donor get: local-get:dyn
local via donor set: {"q": "local-set", "dyn": "local-set"}
static declared: 1
static get: static:anything
static set: 9
static sub get: static:other
static sub set: 4
static local sub get: static:thing
static local sub set: 6
static value get: static:viaValue
static local value get: local-static:viaValue
static local value set: 2
dyn call: call:missing:2
dyn known: known
dyn index: idx:k
dyn setIndex: 3
dyn string: Dyn!
dyn invoke: 5
kid call: call:missing:2
kid known: known
kid index: idx:k
kid setIndex: 3
kid string: Dyn!
kid invoke: 5
local call: call:missing:2
local known: known
local index: idx:k
local setIndex: 3
local string: Dyn!
local invoke: 5
callStatic: scall:nothing
callStatic kid: scall:nothing
callStatic local: scall:nothing
`)
}
