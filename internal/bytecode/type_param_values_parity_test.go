package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const typeParamValuesProgram = `import io;
import json;
import yaml;
import reflect;

class User {
    int id = 0;
    string name = "";
    static func label(): string { return "user"; }
}
class Box<T> {
    T v;
    func Box(T v) { this.v = v; }
}
class Wrap<T> { T inner; }
class Repo<T> {
    func load(string body): T { return json.parseAs(body, T); }
    func kind(): string { return "${T}"; }
}
class UserRepo extends Repo<User> {}

func decode<T>(string body): T { return json.parseAs(body, T); }
func make<T>(any v): T { return T(v); }
func blank<T>(): T { return T(); }
func kind<T>(): any { return T; }
func viaYaml<T>(string body): T { return yaml.parseAs(body, T); }
func later<T>(): bool {
    let f = func(): any { return T; };
    return f() == T;
}
func unbound<T>(any x): any { return T; }

func report(callable f): void {
    try { io.println(f()); } catch (TypeError e) { io.println("TypeError: " + e.message); }
}

io.println(decode<User>("{\"id\": 1, \"name\": \"Mara\"}").name);
report(func(): any { return decode<User>("{\"id\": \"x\"}"); });
let b = decode<Box<int>>("{\"v\": 5}");
io.println("${b.v} ${b instanceof Box<int>} ${reflect.typeBindings(b)}");
report(func(): any { return decode<Box<int>>("{\"v\": \"x\"}"); });
io.println(decode<Wrap<User>>("{\"inner\": {\"id\": 2}}").inner instanceof User);
io.println(make<Box<int>>(3).v);
report(func(): any { return make<Box<int>>("x"); });
io.println(blank<User>() instanceof User);
report(func(): any { return blank<int>(); });
io.println("${kind<User>()} ${kind<int>()} ${kind<Box<string>>()} ${typeof(kind<Box<int>>())}");
io.println("${kind<Box<int>>() == kind<Box<int>>()} ${kind<Box<int>>() == kind<Box<string>>()} ${kind<Box<int>>() == Box} ${kind<User>() == User}");
io.println("${reflect.className(kind<Box<int>>())} ${reflect.fields(kind<Box<int>>()).length} ${kind<User>().label()}");
dict<any, int> d = {};
d[kind<Box<int>>()] = 1;
io.println(d[kind<Box<int>>()]);
io.println(viaYaml<User>("id: 7").id);
io.println(later<User>());
report(func(): any { return kind<list<User>>(); });
report(func(): any { return kind<?User>(); });
let viaRef = unbound;
report(func(): any { return viaRef(1); });
io.println(UserRepo().load("{\"id\": 9}").id);
io.println(Repo<User>().kind());
report(func(): any { return Repo().kind(); });
`

func TestTypeParamValuesParity(t *testing.T) {
	ev, vm := parityOutputs(t, typeParamValuesProgram)
	want := "Mara\n" +
		"TypeError: cannot assign string to int field User.id\n" +
		"5 true {\"T\": \"int\"}\n" +
		"TypeError: Box expects int for parameter 'v', got string\n" +
		"true\n3\n" +
		"TypeError: Box expects int for parameter 'v', got string\n" +
		"true\n" +
		"TypeError: type parameter T is bound to int, which cannot be constructed\n" +
		"<class User> int <class Box<string>> Box\n" +
		"true false false true\n" +
		"Box 1 user\n1\n7\ntrue\n" +
		"TypeError: type parameter T is bound to list<User>, which is not a class\n" +
		"TypeError: type parameter T is bound to ?User, which is not a class\n" +
		"TypeError: type parameter T of unbound is not bound; pass a type argument\n" +
		"9\n<class User>\n" +
		"TypeError: type parameter T of Repo.kind is not bound; pass a type argument\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}

func TestTypeParamValuesCrossModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module tplib;
import json;

export class Item { int n = 0; }
export class Box<T> { T v; }
export func decode<T>(string body): T { return json.parseAs(body, T); }
export func make<T>(): T { return T(); }
export class Repo<T> {
    func load(string body): T { return json.parseAs(body, T); }
}
`
	if err := os.WriteFile(filepath.Join(dir, "tplib.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import tplib;

class User { int id = 0; }
class UserRepo extends tplib.Repo<User> {}

io.println(tplib.decode<User>("{\"id\": 3}").id);
io.println(tplib.decode<tplib.Item>("{\"n\": 4}").n);
let b = tplib.decode<tplib.Box<int>>("{\"v\": 5}");
io.println("${b.v} ${b instanceof tplib.Box<int>}");
try { tplib.decode<tplib.Box<int>>("{\"v\": \"x\"}"); } catch (TypeError e) { io.println("TypeError"); }
io.println(tplib.make<User>() instanceof User);
io.println(UserRepo().load("{\"id\": 6}").id);
io.println(tplib.Repo<tplib.Item>().load("{\"n\": 7}").n);
`)
	want := "3\n4\n5 true\nTypeError\ntrue\n6\n7\n"
	if ev != want {
		t.Fatalf("evaluator output = %q, want %q", ev, want)
	}
	if vm != ev {
		t.Fatalf("VM output = %q, evaluator = %q", vm, ev)
	}
}
