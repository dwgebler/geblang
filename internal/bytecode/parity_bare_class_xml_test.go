package bytecode_test

import "testing"

func writeBareNameModules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, m := range []string{"bnzeta", "bnalpha", "bnmid"} {
		writeModule(t, dir, m, "module "+m+";\nexport class Widget {\n    string tag;\n}\nexport class Base {\n    int level;\n}\n")
	}
	writeModule(t, dir, "bnpicker", `module bnpicker;
import reflect;
from bnzeta import Widget;
export func literal(): any {
    return reflect.class("Widget");
}
export func dynamic(string name): any {
    return reflect.class(name);
}
`)
	writeModule(t, dir, "bnkids", `module bnkids;
import bnmid as m;
import bnzeta;
export class Kid extends m.Base {
    string name;
}
export class GrandKid extends Kid {}
export class Holder {
    bnzeta.Widget item;
    list<m.Widget> items;
}
export class Oops extends Error {}
`)
	return dir
}

const ownerHelper = `func owner(any cls): string {
    return cls == null ? "null" : reflect.className(cls) + "@" + (reflect.location(cls)["module"] as string);
}
`

func TestParityBareClassNameOrder(t *testing.T) {
	dir := writeBareNameModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import bnzeta;
import bnmid;
import bnalpha;
import bnpicker;
`+ownerHelper+`
let n = "Widget";
io.println(owner(reflect.class("Widget")));
io.println(owner(reflect.class(n)));
io.println(owner(bnpicker.literal()));
io.println(owner(bnpicker.dynamic(n)));
`)
	want := "Widget@bnalpha\nWidget@bnalpha\nWidget@bnzeta\nWidget@bnzeta\n"
	if ev != vm || ev != want {
		t.Fatalf("bare name mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectParentClassAndNestedResolvedHydration(t *testing.T) {
	dir := writeBareNameModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import json;
import reflect;
import bnalpha;
import bnmid;
import bnzeta;
import bnkids;
`+ownerHelper+`
class Root {}
class Leaf extends Root {}
io.println(owner(reflect.parentClass(bnkids.Kid)));
io.println(owner(reflect.parentClass(bnkids.GrandKid)));
io.println(owner(reflect.parentClass(Leaf())));
io.println(owner(reflect.parentClass(Root)));
io.println(owner(reflect.parentClass(bnkids.Oops)));
let h = json.parseAs("{\"item\": {\"tag\": \"z\"}, \"items\": [{\"tag\": \"m\"}]}", bnkids.Holder);
io.println(h.item instanceof bnzeta.Widget);
io.println(h.items[0] instanceof bnmid.Widget);
io.println(h.item instanceof bnalpha.Widget);
`)
	want := "Base@bnmid\nKid@bnkids\nRoot@\nnull\nnull\ntrue\ntrue\nfalse\n"
	if ev != vm || ev != want {
		t.Fatalf("parentClass/hydration mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityXMLClassMapping(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "xgeo", `module xgeo;
export class Address {
    string city;
    int zip;
}
`)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import xml;
import xgeo as g;
class Base {
    string id;
}
class Person extends Base {
    string name;
    int age;
    bool active;
    decimal score;
    ?string nick;
    ?int rank;
    g.Address home;
    list<string> tags;
    list<g.Address> places;
    int _secret;
}
class Money {
    int cents;
    func __serialize(): any {
        return {"amount": this.cents, "currency": "GBP"};
    }
}
let text = "<person id=\"p1\" age=\"99\"><name>Ada &amp; co</name><age>36</age><active>1</active><score>1.5</score><rank></rank><home><city>Leeds</city><zip>12</zip></home><tags>x</tags><tags>y</tags><places><city>York</city><zip>1</zip></places><unknown>z</unknown></person>";
let p = xml.parseAs(text, Person) as Person;
io.println("${p.id} ${p.name} ${p.age} ${p.active} ${p.score} ${p.nick} ${p.rank}");
io.println("${p.home.city} ${p.home.zip} ${p.home instanceof g.Address} ${p.tags} ${p.places[0].city}");
let out = xml.stringify(p);
io.println(out);
let back = xml.parseAs(out, Person) as Person;
io.println("${back.id} ${back.name} ${back.places.length()} ${back.score}");
let m = Money();
m.cents = 250;
io.println(xml.stringify(m));
io.println(xml.stringify({"name": "plain", "attributes": {"a": "1"}, "children": [], "text": "ok"}));
try {
    xml.parseAs("<person><age>old</age></person>", Person);
} catch (TypeError e) {
    io.println(e.message);
}
`)
	want := `p1 Ada & co 36 true 1.5000000000 null null
Leeds 12 true ["x", "y"] York
<Person><active>true</active><age>36</age><home><city>Leeds</city><zip>12</zip></home><id>p1</id><name>Ada &amp; co</name><places><city>York</city><zip>1</zip></places><score>1.5000000000</score><tags>x</tags><tags>y</tags></Person>
p1 Ada & co 1 1.5000000000
<Money><amount>250</amount><currency>GBP</currency></Money>
<plain a="1">ok</plain>
xml.parseAs: field age expects int, got "old"
`
	if ev != vm || ev != want {
		t.Fatalf("xml mapping mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
