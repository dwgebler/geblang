package bytecode_test

import "testing"

func writeParameterClassModules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeModule(t, dir, "shapes", `module shapes;
export class Widget {
    string id;
}
class Hidden {
    int n;
}
export class Box<T> {
    T value;
}
`)
	writeModule(t, dir, "other", `module other;
export class Widget {
    int code;
}
`)
	writeModule(t, dir, "lib", `module lib;
import shapes as model;
import other;
from shapes import Box;
class Local {
    bool ok;
}
export class Ctl {
    func handle(model.Widget w, ?model.Hidden h, list<model.Widget> ws, set<other.Widget> os, model.Widget[] arr, Box<int> b, int n, Local l, dict<string, model.Widget> d): void {}
}
export func top(other.Widget o, string s): void {}
`)
	writeModule(t, dir, "sub", `module sub;
import lib;
import shapes as geo;
export class SubCtl extends lib.Ctl {
    func extra(geo.Widget w): void {}
}
`)
	return dir
}

const describeClassHelper = `func describe(any v): string {
    if (v == null) {
        return "null";
    }
    return reflect.className(v) + "@" + (reflect.location(v)["module"] as string);
}
`

func TestParityReflectParameterClassAcrossModules(t *testing.T) {
	dir := writeParameterClassModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import json;
import reflect;
import lib;
import sub;
import shapes;
import other;
`+describeClassHelper+`
for (p in reflect.parameters(reflect.method(lib.Ctl(), "handle"))) {
    io.println(p["name"] + " " + describe(p["class"]) + " " + describe(p["elementClass"]));
}
io.println(describe(reflect.parameters(reflect.method(sub.SubCtl(), "extra"))[0]["class"]));
io.println(describe(reflect.parameters(reflect.method(reflect.class(reflect.parent(sub.SubCtl)), "handle"))[0]["class"]));
io.println(describe(reflect.parameters(lib.top)[0]["class"]));
let w = reflect.parameters(lib.top)[0]["class"];
io.println(w == other.Widget);
io.println(json.parseAs("{\"code\": 3}", w) instanceof other.Widget);
io.println(reflect.parameters(reflect.method(lib.Ctl, "handle"))[0]["class"] == shapes.Widget);
`)
	want := `w Widget@shapes null
h Hidden@shapes null
ws null Widget@shapes
os null Widget@other
arr null Widget@shapes
b Box@shapes null
n null null
l Local@lib null
d null null
Widget@shapes
Widget@shapes
Widget@other
true
true
true
`
	if ev != vm || ev != want {
		t.Fatalf("parameter class mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectClassCanonicalName(t *testing.T) {
	dir := writeParameterClassModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import lib;
import other as oth;
`+describeClassHelper+`
io.println(describe(reflect.class("shapes.Widget")));
io.println(describe(reflect.class("shapes.Hidden")));
io.println(describe(reflect.class("other.Widget")));
io.println(describe(reflect.class("oth.Widget")));
io.println(describe(reflect.class("nope.Widget")));
let names = ["shapes.Widget", "shapes.Hidden", "other.Widget", "oth.Widget", "nope.Widget"];
for (name in names) {
    io.println(describe(reflect.class(name)));
}
`)
	want := "Widget@shapes\nHidden@shapes\nWidget@other\nWidget@other\nnull\n"
	want += want
	if ev != vm || ev != want {
		t.Fatalf("canonical lookup mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectClassBoundPrefixUsesExports(t *testing.T) {
	dir := writeParameterClassModules(t)
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import reflect;
import shapes;
`+describeClassHelper+`
io.println(describe(reflect.class("shapes.Widget")));
io.println(describe(reflect.class("shapes.Hidden")));
let hidden = "shapes.Hidden";
io.println(describe(reflect.class(hidden)));
`)
	want := "Widget@shapes\nnull\nnull\n"
	if ev != vm || ev != want {
		t.Fatalf("bound prefix mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}

func TestParityReflectParameterClassSingleFile(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import reflect;
class Item {
    string id;
}
class Svc {
    func put(Item item, list<Item> items, ?Item maybe, string s): void {}
}
func run(Item item): void {}
let f = func(Item x): void {};
for (p in reflect.parameters(reflect.method(Svc, "put"))) {
    io.println(p["name"] + " " + ((p["class"] == Item) as string) + " " + ((p["elementClass"] == Item) as string));
}
io.println(reflect.parameters(run)[0]["class"] == Item);
io.println(reflect.parameters(f)[0]["class"] == Item);
`)
	want := "item true false\nitems false true\nmaybe true false\ns false false\ntrue\ntrue\n"
	if ev != vm || ev != want {
		t.Fatalf("single-file mismatch:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
