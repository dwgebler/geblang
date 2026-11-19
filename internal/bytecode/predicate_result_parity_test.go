package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPredicateNonBoolResultIsTypeErrorParity(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import collections;

func asIs(any x): any {
    return x;
}

class Rule {
    func keep(int x): any {
        return x > 1 ? "yes" : 0;
    }
}

func attempt(string label, callable run): void {
    try {
        io.println(label + ": " + "${run()}");
    } catch (TypeError e) {
        io.println(label + ": " + e.message);
    }
}

let xs = [1, 2, 3];
let rule = Rule();
attempt("filter", () => xs.filter(asIs));
attempt("find", () => xs.find(asIs));
attempt("findLast", () => xs.findLast(asIs));
attempt("any", () => xs.any(asIs));
attempt("all", () => xs.all(asIs));
attempt("count", () => xs.count(asIs));
attempt("partition", () => xs.partition(asIs));
attempt("takeWhile", () => xs.takeWhile(asIs));
attempt("dropWhile", () => xs.dropWhile(asIs));
attempt("indexBy", () => xs.indexBy(asIs));
attempt("list.search", () => xs.search(asIs));
attempt("method", () => xs.filter(rule.keep));
attempt("string.search", () => "ab".search(asIs));
attempt("dict.search", () => ({"a": 1}).search(asIs));
attempt("collections.filter", () => collections.filter(xs, asIs));
attempt("lazyFilter", () => [x for x in collections.lazyFilter(xs, asIs)]);
attempt("bool ok", () => xs.filter((int x) => x > 1));
attempt("empty list", () => [].all(asIs));
`)
	want := `filter: list.filter predicate must return bool, got int
find: list.find predicate must return bool, got int
findLast: list.findLast predicate must return bool, got int
any: list.any predicate must return bool, got int
all: list.all predicate must return bool, got int
count: list.count predicate must return bool, got int
partition: list.partition predicate must return bool, got int
takeWhile: list.takeWhile predicate must return bool, got int
dropWhile: list.dropWhile predicate must return bool, got int
indexBy: list.indexBy predicate must return bool, got int
list.search: list.search predicate must return bool, got int
method: list.filter predicate must return bool, got int
string.search: string.search predicate must return bool, got string
dict.search: dict.search predicate must return bool, got int
collections.filter: list.filter predicate must return bool, got int
lazyFilter: collections.lazyFilter predicate must return bool, got int
bool ok: [2, 3]
empty list: true
`
	if ev != want || vm != want {
		t.Fatalf("evaluator:\n%s\nvm:\n%s\nwant:\n%s", ev, vm, want)
	}
}

func TestPredicateNonBoolResultCrossModuleParity(t *testing.T) {
	dir := t.TempDir()
	module := `module rules;

export func loose(int x): any {
    return x;
}

export class Checker {
    func strict(int x): bool {
        return x > 1;
    }

    func sloppy(int x): any {
        return x > 1 ? 1 : 0;
    }
}

export func evens(list<int> xs, callable keep): list<int> {
    return xs.filter(keep);
}
`
	if err := os.WriteFile(filepath.Join(dir, "rules.gb"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import rules;

let c = rules.Checker();
io.println(rules.evens([1, 2, 3], c.strict));
for (keep in [rules.loose, c.sloppy]) {
    try {
        io.println(rules.evens([1, 2, 3], keep));
    } catch (TypeError e) {
        io.println(e.message);
    }
}
`)
	want := "[2, 3]\nlist.filter predicate must return bool, got int\nlist.filter predicate must return bool, got int\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
