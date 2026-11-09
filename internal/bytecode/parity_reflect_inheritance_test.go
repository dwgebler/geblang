package bytecode_test

import "testing"

func writeInheritanceModules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeModule(t, dir, "inhgeo", `module inhgeo;
export class Point {
    int x;
}
export class Tag {
    string label;
}
`)
	writeModule(t, dir, "inhbase", `module inhbase;
import inhgeo as g;
from inhgeo import Tag;
export class Root {
    g.Point origin;
    ?Tag tag;
    decimal score;
}
export class Mid extends Root {
    int level;
    list<g.Point> path;
}
export class Maker {
    func make(): g.Point { return g.Point(); }
    func many(): list<g.Point> { return []; }
    func pair(): ?dict<string, int> { return null; }
}
`)
	writeModule(t, dir, "inhleaf", `module inhleaf;
import inhbase;
export class Leaf extends inhbase.Mid {
    bool active;
}
`)
	return dir
}

func TestParityReflectFieldsInheritedAndResolved(t *testing.T) {
	dir := writeInheritanceModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import inhleaf;
import inhbase;
import inhgeo;
`+describeClassHelper+`
for (f in reflect.fields(inhleaf.Leaf, inherited: true)) {
    let e = f as dict<string, any>;
    io.println((e["name"] as string) + " " + (e["type"] as string) + " " + describe(e["declaringClass"]) + " " + describe(e["class"]) + " " + describe(e["elementClass"]));
}
io.println(reflect.fields(inhleaf.Leaf).length());
io.println(reflect.fields(inhleaf.Leaf(), true).length());
io.println(reflect.fields(inhbase.Mid, inherited: false)[0]["name"]);
let path = reflect.fields(inhbase.Mid)[1] as dict<string, any>;
io.println(path["elementClass"] == inhgeo.Point);
`)
	want := `active bool Leaf@inhleaf null null
level int Mid@inhbase null null
path list<g.Point> Mid@inhbase null Point@inhgeo
origin g.Point Root@inhbase Point@inhgeo null
score decimal Root@inhbase null null
tag ?Tag Root@inhbase Tag@inhgeo null
1
6
level
true
`
	if ev != vm || ev != want {
		t.Fatalf("inherited fields mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectReturnClassAndGenericReturnType(t *testing.T) {
	dir := writeInheritanceModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import inhbase;
`+describeClassHelper+`
class Box<T> {
    T v;
}
class K {
    func a(): list<int> { return []; }
    func c(): ?Box<int> { return null; }
    static func s(): list<string> { return []; }
}
func top(): list<string> { return []; }
let lam = func(): list<int> { return []; };
for (name in ["make", "many", "pair"]) {
    let m = reflect.method(inhbase.Maker, name);
    let r = reflect.returnClass(m);
    io.println(name + " " + (reflect.returnType(m) as string) + " " + describe(r["class"]) + " " + describe(r["elementClass"]));
}
io.println(reflect.returnType(top));
io.println(reflect.returnType(lam));
io.println(reflect.returnType(reflect.method(K, "a")));
io.println(reflect.returnType(reflect.method(K(), "c")));
io.println(reflect.returnType(reflect.staticMethod(K, "s")));
io.println(describe(reflect.returnClass(reflect.method(K, "c"))["class"]));
`)
	want := `make g.Point Point@inhgeo null
many list<g.Point> null Point@inhgeo
pair ?dict<string, int> null null
list<string>
list<int>
list<int>
?Box<int>
list<string>
Box@
`
	if ev != vm || ev != want {
		t.Fatalf("return class mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityParseAsFillsInheritedFields(t *testing.T) {
	dir := writeInheritanceModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import json;
import yaml;
import inhleaf;
import inhgeo;
class Base {
    int x;
}
class Child extends Base {
    int z;
}
class Bag {
    list<inhleaf.Leaf> leaves;
}
let c = json.parseAs("{\"x\": 1, \"z\": 3}", Child);
io.println("${c.x} ${c.z}");
let text = "{\"active\": true, \"level\": 2, \"path\": [{\"x\": 5}], \"origin\": {\"x\": 7}, \"tag\": {\"label\": \"t\"}, \"score\": 1.5}";
let l = json.parseAs(text, inhleaf.Leaf);
io.println("${l.active} ${l.level} ${l.path[0].x} ${l.origin.x} ${l.tag.label} ${l.score} ${l.origin instanceof inhgeo.Point}");
let bag = json.parseAs("{\"leaves\": [" + text + "]}", Bag);
io.println(bag.leaves[0].origin.x);
io.println(yaml.parseAs("level: 4\norigin:\n  x: 9\n", inhleaf.Leaf).origin.x);
try {
    json.parseAs("{\"level\": \"high\"}", inhleaf.Leaf);
} catch (Error e) {
    io.println(e.message);
}
`)
	want := "1 3\ntrue 2 5 7 t 1.5000000000 true\n7\n9\ncannot assign string to int field Leaf.level\n"
	if ev != vm || ev != want {
		t.Fatalf("parseAs inherited mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityHTTPJSONResponseSerializesInstances(t *testing.T) {
	ev, vm := parityOutputsModulesDir(t, t.TempDir(), `import io;
import http;
import json;
class Point {
    int x;
    int _hidden;
}
class Point3 extends Point {
    int z;
}
class Money {
    int cents;
    func __serialize(): any {
        return {"amount": this.cents};
    }
}
let p = Point3();
p.x = 1;
p.z = 3;
let m = Money();
m.cents = 250;
class Label {
    string text;
}
let label = Label();
label.text = "<b>&";
for (v in [p, m, {"items": [p, m]}, [1, "<b>", 2.5], label]) {
    let body = http.jsonResponse(v, 201).body as string;
    io.println(body + " " + ((body == json.stringify(v)) as string));
}
`)
	want := `{"x":1,"z":3} true
{"amount":250} true
{"items":[{"x":1,"z":3},{"amount":250}]} true
[1,"<b>",2.5000000000] true
{"text":"<b>&"} true
`
	if ev != vm || ev != want {
		t.Fatalf("jsonResponse mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectConstructorParameterClasses(t *testing.T) {
	dir := writeInheritanceModules(t)
	writeModule(t, dir, "inhsvc", `module inhsvc;
import inhgeo as g;
from inhgeo import Tag;
class Note {
    string text;
}
export class Svc {
    func Svc(g.Point p, list<Tag> tags, Note note, int n) {}
}
`)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import inhsvc;
import inhgeo;
`+describeClassHelper+`
for (p in reflect.constructors(inhsvc.Svc)[0] as list<any>) {
    let e = p as dict<string, any>;
    io.println((e["name"] as string) + " " + describe(e["class"]) + " " + describe(e["elementClass"]));
}
io.println(reflect.constructors(inhsvc.Svc)[0][0]["class"] == inhgeo.Point);
`)
	want := "p Point@inhgeo null\ntags null Tag@inhgeo\nnote Note@inhsvc null\nn null null\ntrue\n"
	if ev != vm || ev != want {
		t.Fatalf("constructor classes mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
