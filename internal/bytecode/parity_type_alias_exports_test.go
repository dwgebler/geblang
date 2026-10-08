package bytecode_test

import "testing"

func typeAliasModules(t *testing.T) string {
	return moduleDirFor(t, map[string]string{
		"aliascontract": `module aliascontract;
export interface Client { func chat(): string; }
export class Base { func Base() {} func hello(): string { return "base"; } }
`,
		"aliasprov": `module aliasprov;
import aliascontract;
export class Prov implements aliascontract.Client { func Prov() {} func chat(): string { return "prov"; } }
`,
		"aliasfacade": `module aliasfacade;
import aliascontract;
import aliasprov;
export type Client = aliascontract.Client;
export type Bot = aliascontract.Client;
export type Money = decimal;
export type Ids = list<int>;
export type Root = aliascontract.Base;
export type Rows = list;
type Page = int;
export func client(): Client { return aliasprov.Prov(); }
`,
		"aliasmid": `module aliasmid;
import aliasfacade;
export type Chained = aliasfacade.Bot;
`,
	})
}

func TestParityExportedTypeAliases(t *testing.T) {
	runParityModulesDir(t, typeAliasModules(t), `import io;
import aliasfacade;
import aliasmid;
from aliasfacade import Bot, Money;
class Page { func Page() {} }
class Mine implements aliasfacade.Client { func Mine() {} func chat(): string { return "mine"; } }
class Robo implements Bot { func Robo() {} func chat(): string { return "robo"; } }
class Kid extends aliasfacade.Root { func Kid() { parent(); } }
func use(aliasfacade.Client c): string { return c.chat(); }
func useBot(aliasfacade.Bot c): string { return c.chat(); }
func useChain(aliasmid.Chained c): string { return c.chat(); }
func cost(aliasfacade.Money m): string { return "${m}"; }
func ids(list raw): aliasfacade.Ids { return raw; }
func rows(list raw): aliasfacade.Rows<string> { return raw; }
func page(Page p): string { return "page"; }
func run(string label, func f): void {
    try { io.println(label + " " + f()); } catch (TypeError e) { io.println(label + " ! " + e.message); }
}
run("client", func(): any { return use(aliasfacade.client()); });
run("mine", func(): any { return use(Mine()); });
run("bot", func(): any { return useBot(Robo()); });
run("chain", func(): any { return useChain(Mine()); });
run("money", func(): any { return cost(1.5); });
run("money-bad", func(): any { list xs = ["x"]; return cost(xs[0]); });
run("ids-bad", func(): any { return ids(["a"]); });
run("rows-bad", func(): any { return rows([1]); });
run("kid", func(): any { return Kid().hello(); });
run("inst", func(): any { return "${Mine() instanceof aliasfacade.Client} ${Robo() instanceof aliasfacade.Bot} ${Kid() instanceof aliasfacade.Root} ${5 instanceof aliasfacade.Client}"; });
run("cast", func(): any { any m = Mine(); aliasfacade.Client c = m as aliasfacade.Client; return c.chat(); });
run("money-local", func(): any { Money m = 2; return "${m}"; });
run("private-alias", func(): any { return page(Page()); });
`, "client prov\n"+
		"mine mine\n"+
		"bot robo\n"+
		"chain mine\n"+
		"money 1.5000000000\n"+
		"money-bad ! cost expects decimal for parameter 'm', got string\n"+
		"ids-bad ! ids expects list<int> return, got list<string>\n"+
		"rows-bad ! rows expects list<string> return, got list<int>\n"+
		"kid base\n"+
		"inst true true true false\n"+
		"cast mine\n"+
		"money-local 2.0000000000\n"+
		"private-alias page\n")
}

func TestParityLocalTypeAliasesInInstanceofAndCasts(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
type Ints = list<int>;
type Num = int;
class Dog { func Dog() {} }
type Pup = Dog;
type Rows = list;
any xs = [1];
io.println(xs instanceof Ints);
io.println(5 instanceof Num);
io.println(Dog() instanceof Pup);
any d = Dog();
Pup p = d as Pup;
io.println(p);
try { Rows<string> r = [1]; io.println(r); } catch (TypeError e) { io.println(e.message); }
`)
	want := "true\ntrue\ntrue\n<Dog>\ntype error: cannot assign list to list<string> (element at index 0 is int)\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityNullableBoundTypeParameter(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Box<T> {
    T v;
    func Box(T v) { this.v = v; }
    func put(?T x): string { return "ok"; }
    func none(): ?T { return null; }
}
Box<int> b = Box(1);
io.println(b.put(null));
io.println(b.none());
try { any s = "x"; b.put(s); } catch (TypeError e) { io.println(e.message); }
`)
	want := "ok\nnull\nBox.put expects ?T for parameter 'x', got string\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}

func TestParityUnresolvedTypeBindingsOmitted(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import reflect;
class Pair<T, E> {
    ?T left;
    ?E right;
    func Pair(?T left, ?E right) { this.left = left; this.right = right; }
}
func rightOnly<T, E>(E value): Pair<T, E> { return Pair(null, value); }
func leftOnly<T, E>(T value): Pair<T, E> { return Pair(value, null); }
io.println(reflect.typeBindings(rightOnly("bad")));
io.println(reflect.typeBindings(leftOnly(1)));
io.println(reflect.typeBindings(Pair("x", null)));
io.println(reflect.typeBindings(Pair(null, null)));
`)
	want := "{\"E\": \"string\"}\n{\"T\": \"int\"}\n{\"T\": \"string\"}\n{}\n"
	if ev != want {
		t.Fatalf("evaluator output:\n%s\nwant:\n%s", ev, want)
	}
	if vm != want {
		t.Fatalf("vm output:\n%s\nwant:\n%s", vm, want)
	}
}
