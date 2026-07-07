package bytecode_test

import "testing"

const dirParentBase = `module basemod;
export interface Greeter {
    func greet(): string;
}
export class BaseCtl implements Greeter {
    static const KIND = "base";
    func BaseCtl() {}
    func greet(): string { return "hi"; }
    func shared(): string { return "s"; }
    static func mkBase(): BaseCtl { return BaseCtl(); }
}
`

const dirParentMid = `module midmod;
import basemod as base;
export interface Named {
    func name(): string;
}
export class MyCtl extends base.BaseCtl implements Named {
    func MyCtl() { parent(); }
    func extra(): string { return "e"; }
    func name(): string { return "n"; }
}
`

// reflect.parent returns the bare parent name so a parent-chain walk crosses module boundaries (the VM emitted a qualified name and stopped).
func TestParityCrossModuleReflectParentWalk(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"basemod": dirParentBase,
		"midmod":  dirParentMid,
	}, `import io;
import reflect;
import midmod;
func collect(any cls): list<string> {
    let acc = [];
    let cur = cls;
    let done = false;
    while (!done) {
        for (i in reflect.interfaces(cur)) { acc.push(i as string); }
        let p = reflect.parent(cur);
        if (p == null) { done = true; }
        else {
            let nxt = reflect.class(p as string);
            if (nxt == null) { done = true; } else { cur = nxt; }
        }
    }
    return acc;
}
let cls = reflect.class(midmod.MyCtl());
io.println("parent=${reflect.parent(cls)}");
io.println("ifaces=${collect(cls)}");
io.println("dir=${dir(cls)}");
`, "parent=BaseCtl\n"+
		"ifaces=[\"Named\", \"Greeter\"]\n"+
		"dir=[\"KIND\", \"extra\", \"greet\", \"mkbase\", \"name\", \"shared\"]\n")
}

// dir on a cross-module class value reached directly (module.Class) includes the foreign parent's inherited members.
func TestParityCrossModuleDirDirectClassValue(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"basemod": dirParentBase,
		"midmod":  dirParentMid,
	}, `import io;
import midmod;
io.println(dir(midmod.MyCtl));
`, "[\"KIND\", \"extra\", \"greet\", \"mkbase\", \"name\", \"shared\"]\n")
}
