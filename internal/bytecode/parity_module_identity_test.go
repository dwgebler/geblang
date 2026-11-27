package bytecode_test

import "testing"

func TestModuleExactTypeIdentityParity(t *testing.T) {
	modules := map[string]string{
		"ida.gb":      `module ida; export interface Named { func name(): string; } export interface Child extends Named { func age(): int; } export class User { string id = "a"; }`,
		"idb.gb":      `module idb; export interface Named { func name(): string; } export class User { string id = "b"; } export func takeNamed(Named n): string { return n.name(); } export func names(): list<Named> { list<Named> xs = []; return xs; }`,
		"idfacade.gb": `module idfacade; import ida as leaf; export type Named = leaf.Named;`,
	}
	ev, vm := parityModuleOutputs(t, modules, `import io;
import ida as a;
import idb as b;
import idfacade as facade;
from idb import User;

enum Color implements a.Child {
    Red;
    func name(): string { return "red"; }
    func age(): int { return 1; }
}

class Box implements a.Named {
    func name(): string { return "box"; }
}

func takeA(a.Named n): string { return n.name(); }
func takeFacade(facade.Named n): string { return n.name(); }
func takeUser(User u): string { return u.id; }

func attempt(string label, callable fn): void {
    try {
        fn();
        io.println(label + " accepted");
    } catch (TypeError e) {
        io.println(label + " rejected: " + e.message);
    }
}

any red = Color.Red;
any box = Box();
any alphaUser = a.User();
list<b.Named> theirs = [];
dict<string, User> users = {};
attempt("enum param", func(): void { takeA(red); });
attempt("enum facade", func(): void { takeFacade(red); });
attempt("enum foreign", func(): void { b.takeNamed(red); });
attempt("class foreign", func(): void { b.takeNamed(box); });
attempt("user param", func(): void { takeUser(alphaUser); });
attempt("push", func(): void { theirs.push(box); });
attempt("foreign list", func(): void { b.names().push(red); });
attempt("dict", func(): void { users["k"] = alphaUser; });
io.println([red instanceof a.Named, red instanceof a.Child, red instanceof b.Named, box instanceof b.Named]);
`)

	want := "enum param accepted\n" +
		"enum facade accepted\n" +
		"enum foreign rejected: takeNamed expects Named for parameter 'n', got Color\n" +
		"class foreign rejected: takeNamed expects Named for parameter 'n', got Box\n" +
		"user param rejected: takeUser expects idb.User for parameter 'u', got ida.User\n" +
		"push rejected: cannot push Box to list<b.Named>\n" +
		"foreign list rejected: cannot push Color to list<Named>\n" +
		"dict rejected: cannot assign ida.User to dict<string, idb.User>\n" +
		"[true, true, false, false]\n"
	if ev != want || vm != want {
		t.Fatalf("module identity mismatch\nevaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestConstructibleTypeAliasParity(t *testing.T) {
	modules := map[string]string{
		"leafw.gb":   `module leafw; export class Widget { string label; func Widget(string label = "plain") { this.label = label; } static func named(string l): Widget { return Widget(l); } } export enum Shade { Light; Dark; }`,
		"facadew.gb": `module facadew; import leafw as leaf; export type Widget = leaf.Widget; export type Shade = leaf.Shade;`,
	}
	ev, vm := parityModuleOutputs(t, modules, `import io;
import facadew as facade;
import leafw as leaf;
from facadew import Widget;

class Fancy extends facade.Widget {
    func Fancy() { parent("fancy"); }
}

func describe(leaf.Widget w): string { return w.label; }

let made = facade.Widget("made");
io.println([made.label, facade.Widget().label, Widget("from").label, describe(Fancy())]);
io.println([made instanceof leaf.Widget, Fancy() instanceof facade.Widget]);
io.println([facade.Widget.named("static").label, facade.Shade.Dark == leaf.Shade.Dark]);
`)

	want := "[\"made\", \"plain\", \"from\", \"fancy\"]\n[true, true]\n[\"static\", true]\n"
	if ev != want || vm != want {
		t.Fatalf("constructible alias mismatch\nevaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}
