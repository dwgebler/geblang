package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityListOperations(t *testing.T) {
	runParity(t, `import io;
list items = [10, 20, 30, 40];
io.println(items[0]);
io.println(items.length());
io.println(items.isEmpty());
io.println(items[1..<3]);
io.println(items.get(2));
`, "10\n4\nfalse\n[20, 30]\n30\n")
}

func TestParityDictOperations(t *testing.T) {
	runParity(t, `import io;
dict d = {"a": 1};
io.println(d["a"]);
io.println(d.length());
io.println(d.isEmpty());
io.println(d.keys().length());
d["b"] = 2;
io.println(d.length());
`, "1\n1\nfalse\n1\n2\n")
}

func TestParityForInList(t *testing.T) {
	runParity(t, `import io;
list nums = [10, 20, 30];
int sum = 0;
for (int n in nums) {
    sum = sum + n;
}
io.println(sum);
`, "60\n")
}

func TestParityForInRange(t *testing.T) {
	runParity(t, `import io;
int sum = 0;
for (int i in 1..5) {
    sum = sum + i;
}
io.println(sum);
`, "15\n")
}

func TestParityDictDelete(t *testing.T) {
	runParity(t, `import io;
let d = {"a": 1, "b": 2, "c": 3};
d.delete("b");
io.println(d.length() as string);
io.println(d.hasKey("b") as string);
io.println(d.hasKey("a") as string);
`, "2\nfalse\ntrue\n")
}

func TestParityListHigherOrder(t *testing.T) {
	runParity(t, `import io;
let nums = [1, 2, 3, 4, 5];
let doubled = nums.map(func(int x): int { return x * 2; });
io.println(doubled.get(0) as string);
let evens = nums.filter(func(int x): bool { return x % 2 == 0; });
io.println(evens.length() as string);
let sum = nums.reduce(func(int acc, int x): int { return acc + x; }, 0);
io.println(sum as string);
let found = nums.find(func(int x): bool { return x > 3; });
io.println(found as string);
io.println(nums.any(func(int x): bool { return x > 4; }) as string);
io.println(nums.all(func(int x): bool { return x > 0; }) as string);
io.println(nums.count(func(int x): bool { return x % 2 == 0; }) as string);
let sorted = [3,1,2].sorted();
io.println(sorted.get(0) as string);
let nested = [[1,2],[3,4]].flatten();
io.println(nested.length() as string);
let uniq = [1,2,2,3,1].unique();
io.println(uniq.length() as string);
let zipped = [1,2,3].zip([4,5,6]);
io.println(zipped.length() as string);
`, "2\n2\n15\n4\ntrue\ntrue\n2\n1\n4\n3\n3\n")
}

func TestParityListMethods(t *testing.T) {
	runParity(t, `import io;
let a = [1, 2, 3];
io.println(a.first() as string);
io.println(a.last() as string);
io.println(a.indexOf(2) as string);
io.println(a.contains(3) as string);
let b = a.push(4);
io.println(b.length() as string);
let c = b.pop();
io.println(c.length() as string);
let d = a.insert(1, 99);
io.println(d.get(1) as string);
let e = a.removeAt(1);
io.println(e.length() as string);
let f = a.concat([4, 5]);
io.println(f.length() as string);
`, "1\n3\n1\ntrue\n4\n3\n99\n3\n5\n")
}

func TestParityCollectionsModule(t *testing.T) {
	runParity(t, `import io;
import collections;
let a = [1, 2, 3, 4, 5];
io.println(collections.length(a) as string);
io.println(collections.isEmpty(a) as string);
io.println(collections.isEmpty([]) as string);
io.println(collections.contains(a, 3) as string);
io.println(collections.contains(a, 9) as string);
let rev = collections.reverse(a);
io.println(rev.get(0) as string);
let sorted = collections.sort([3,1,2]);
io.println(sorted.get(0) as string);
io.println(collections.join(["a","b","c"], ","));
let doubled = collections.map(a, func(int x): int { return x * 2; });
io.println(doubled.get(0) as string);
let evens = collections.filter(a, func(int x): bool { return x % 2 == 0; });
io.println(evens.length() as string);
let sum = collections.reduce(a, func(int acc, int x): int { return acc + x; }, 0);
io.println(sum as string);
let found = collections.find(a, func(int x): bool { return x > 3; });
io.println(found as string);
io.println(collections.any(a, func(int x): bool { return x > 4; }) as string);
io.println(collections.all(a, func(int x): bool { return x > 0; }) as string);
let flat = collections.flatten([[1,2],[3,4]]);
io.println(flat.length() as string);
let uniq = collections.unique([1,2,2,3,1]);
io.println(uniq.length() as string);
let zipped = collections.zip([1,2], [3,4]);
io.println(zipped.length() as string);
let ns = collections.sorted([3,1,2]);
io.println(ns.get(0) as string);
	`, "5\nfalse\ntrue\ntrue\nfalse\n5\n1\na,b,c\n2\n2\n15\n4\ntrue\ntrue\n4\n3\n2\n1\n")
}

func TestParityCollectionsLazyHelpers(t *testing.T) {
	runParity(t, `import io;
import collections;
let stream = collections.lazyMap(collections.range(1, 10), func(int x): int {
    return x * 2;
});
let evens = collections.lazyFilter(stream, func(int x): bool {
    return x % 4 == 0;
});
for (n in collections.take(evens, 3)) {
    io.println(n as string);
}
for (n in collections.range(5, 0, -2)) {
    io.println(n as string);
}
for (n in collections.take([7, 8, 9], 2)) {
    io.println(n as string);
}
	`, "4\n8\n12\n5\n3\n1\n7\n8\n")
}

func TestParitySetLiteralAndMethods(t *testing.T) {
	runParity(t, `import io;
import collections;
let a = {1, 2, 2, 3};
io.println(typeof(a));
io.println(a.length() as string);
io.println(a.isEmpty() as string);
io.println(a.contains(2) as string);
io.println(a.contains(9) as string);
io.println(collections.length(a) as string);
io.println(collections.contains(a, 3) as string);
let b = a.add(4);
io.println(b.length() as string);
let c = b.remove(2);
io.println(c.contains(2) as string);
io.println(c.toList().length() as string);
let u = {1, 2}.union({2, 3});
io.println(u.length() as string);
let i = {1, 2}.intersection({2, 3});
io.println(i.contains(2) as string);
io.println(i.contains(1) as string);
let d = {1, 2, 3}.difference({2});
io.println(d.contains(2) as string);
io.println(d.length() as string);
io.println({1, 2} == {2, 1});
`, "set\n3\nfalse\ntrue\nfalse\n3\ntrue\n4\nfalse\n3\n3\ntrue\nfalse\nfalse\n2\ntrue\n")
}

func TestParitySpreadList(t *testing.T) {
	runParity(t, `import io;
let a = [1, 2, 3];
let b = [4, 5, 6];
let c = [...a, ...b];
io.println(c.length() as string);
io.println(c[0] as string);
io.println(c[5] as string);
let d = [0, ...a, 4];
io.println(d.length() as string);
io.println(d[0] as string);
io.println(d[4] as string);
`, "6\n1\n6\n5\n0\n4\n")
}

func TestParitySpreadCall(t *testing.T) {
	runParity(t, `import io;
func add(int a, int b, int c): int {
    return a + b + c;
}
let args = [10, 20, 30];
io.println(add(...args) as string);
io.println(add(...[1, 2, 3]) as string);
`, "60\n6\n")
}

// Spread (...list) into native variadics; the VM previously dropped it.
func TestParitySpreadIntoNativeVariadic(t *testing.T) {
	runParity(t, `import io;
import math;
import binary;
let nums = [3, 7, 2];
io.println(math.max(...nums));
io.println(math.min(...nums));
io.println(math.max(10, ...nums));
io.println(math.max(...[5]));
let floats = [1.5f, 2.5f, 3.5f];
let blob = binary.pack("<3f", ...floats);
io.println(blob.length());
let back = binary.unpack("<3f", blob);
io.println("${back[0]}-${back[2]}");
`, "7\n2\n10\n5\n12\n1.5-3.5\n")
}

func TestParityForInIteratesDictSetString(t *testing.T) {
	runParity(t, `import io;
let d = {"z": 1, "a": 2};
d["m"] = 3;
for (pair in d) { io.println("${pair}"); }
for (k, v in d) { io.println("${k}=${v}"); }
let s = {3, 1, 2};
for (x in s) { io.println(x); }
let text = "héllo";
for (c in text) { io.println(c); }
`, "[\"z\", 1]\n[\"a\", 2]\n[\"m\", 3]\nz=1\na=2\nm=3\n1\n2\n3\nh\né\nl\nl\no\n")
}

func TestParityComprehensionsIterateDictSetString(t *testing.T) {
	runParity(t, `import io;
let d = {"b": 2, "a": 1};
io.println("${[p for p in d]}");
io.println("${[k + "-" + (v as string) for k, v in d]}");
io.println("${[x * 2 for x in {3, 1, 2}]}");
let text = "ab";
io.println("${[c for c in text]}");
io.println("${ {c for c in text} }");
`, "[[\"b\", 2], [\"a\", 1]]\n[\"b-2\", \"a-1\"]\n[2, 4, 6]\n[\"a\", \"b\"]\nset{\"a\", \"b\"}\n")
}

func TestParityListMutatorsInPlace(t *testing.T) {
	runParity(t, `import io;
let a = [3, 1, 2];
let b = a.push(4);
io.println("${a} ${b}");
b.prepend(0);
io.println("${a}");
a.insert(2, 9);
io.println("${a}");
a.removeAt(2);
a.remove(0);
io.println("${a}");
a.pop();
io.println("${a}");
a.reverse();
io.println("${a}");
io.println("${a.sort()}");
io.println("${a}");
let c = [5, 6].push(7).push(8);
io.println("${c}");
let orig = [3, 1];
let copySorted = orig.sorted();
let copyRev = orig.reversed();
io.println("${orig} ${copySorted} ${copyRev}");
`, `[3, 1, 2, 4] [3, 1, 2, 4]
[0, 3, 1, 2, 4]
[0, 3, 9, 1, 2, 4]
[3, 1, 2, 4]
[3, 1, 2]
[2, 1, 3]
[1, 2, 3]
[1, 2, 3]
[5, 6, 7, 8]
[3, 1] [1, 3] [1, 3]
`)
}

func TestParityListMutatorGuards(t *testing.T) {
	runParity(t, `import io;
import freeze;
let f = freeze.shallow([1, 2]);
try { f.push(3); } catch (ImmutableError e) { io.println("frozen: ${e.message}"); }
try { f.sort(); } catch (ImmutableError e) { io.println("frozen2: ${e.message}"); }
list<int> typed = [1, 2];
any bad = "x";
try { typed.push(bad); } catch (TypeError e) { io.println("typed: ${e.message}"); }
let s = {1, 2};
let s2 = s.add(3);
io.println("${s} ${s2}");
s.remove(1);
io.println("${s}");
`, `frozen: cannot modify frozen list
frozen2: cannot modify frozen list
typed: cannot push string to list<int>
set{1, 2, 3} set{1, 2, 3}
set{2, 3}
`)
}

// opts.maxBodyBytes caps the request body at the server: an oversize
// body answers 413 without invoking the handler; bodies at the limit
// pass through.
func TestParityHTTPListenMaxBodyBytes(t *testing.T) {
	runParityWithStdlib(t, `
import io;
import http;
let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return {"status": 200, "body": "got:" + ((request["body"] as string).length() as string)};
}, {"maxBodyBytes": 16});
let base = "http://" + http.serverAddr(server);
let ok = http.request(base + "/in").withMethod("POST").withBody("x".repeat(16)).send();
io.println(ok.status());
io.println(ok.text());
let big = http.request(base + "/in").withMethod("POST").withBody("x".repeat(17)).send();
io.println(big.status());
http.close(server);
`, "200\ngot:16\n413\n")
}

func TestParityCollectionsChunk(t *testing.T) {
	runParity(t, `import io;
import collections;
let chunks = collections.chunk([1, 2, 3, 4, 5], 2);
io.println(chunks.length());
io.println(chunks[0][0]);
io.println(chunks[0][1]);
io.println(chunks[1][0]);
io.println(chunks[1][1]);
io.println(chunks[2][0]);
`, "3\n1\n2\n3\n4\n5\n")
}

func TestParityCollectionsPartition(t *testing.T) {
	runParity(t, `import io;
import collections;
let parts = collections.partition([1, 2, 3, 4, 5], func(int x): bool { return x % 2 == 0; });
io.println(parts[0]);
io.println(parts[1]);
`, "[2, 4]\n[1, 3, 5]\n")
}

func TestParityCollectionsGroupBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let groups = collections.groupBy(["a", "bb", "c", "dd"], func(string s): int { return s.length(); });
io.println(groups[1].length());
io.println(groups[2].length());
`, "2\n2\n")
}

func TestParityDictContains(t *testing.T) {
	runParity(t, `import io;
let d = {"a": 1, "b": 2};
io.println(d.contains("a"));
io.println(d.contains("c"));
io.println(d.hasKey("b"));
io.println(d.hasKey("z"));
`, "true\nfalse\ntrue\nfalse\n")
}

func TestParityDictKeysInsertionOrder(t *testing.T) {
	runParity(t, `import io;
dict<string, int> d = {};
d["zeta"] = 1;
d["mu"] = 2;
d["alpha"] = 3;
io.println(d.keys());
io.println(d.values());
let lit = {"first": 1, "second": 2, "third": 3};
io.println(lit.keys());
`, "[\"zeta\", \"mu\", \"alpha\"]\n[1, 2, 3]\n[\"first\", \"second\", \"third\"]\n")
}

// TestParityCovariantCollectionArgument pins the covariant collection contract
// on both backends: a list<Dog> flows into a list<Animal> parameter and an
// element-mismatched call (list<int> into list<string>) raises an identical
// runtime error on each backend.
func TestParityCovariantCollectionArgument(t *testing.T) {
	runParity(t, `import io;

class Animal { func name(): string { return "animal"; } }
class Dog extends Animal { func name(): string { return "dog"; } }

func countAnimals(list<Animal> xs): int { return xs.length(); }
func countStrings(list<string> xs): int { return xs.length(); }

let list<Dog> dogs = [Dog(), Dog()];
io.println(countAnimals(dogs));

let list<int> ints = [1, 2, 3];
try {
    countStrings(ints);
} catch (TypeError e) {
    io.println("rejected");
}
`, "2\nrejected\n")
}

func TestParityCollectionsFindLast(t *testing.T) {
	runParity(t, `import io;
import collections;
let x = collections.findLast([1, 3, 5, 2, 4], func(int n): bool { return n % 2 == 1; });
io.println(x);
let none = collections.findLast([2, 4, 6], func(int n): bool { return n % 2 == 1; });
io.println(none);
`, "5\nnull\n")
}

func TestParityCollectionsContainsBy(t *testing.T) {
	runParity(t, `import io;
import collections;
io.println(collections.containsBy(["alice", "bob", "carol"], "bob", func(string s): string { return s; }));
io.println(collections.containsBy(["alice", "bob", "carol"], "dave", func(string s): string { return s; }));
`, "true\nfalse\n")
}

func TestParityCollectionsIndexBy(t *testing.T) {
	runParity(t, `import io;
import collections;
io.println(collections.indexBy([10, 20, 30, 40], func(int n): bool { return n > 25; }));
io.println(collections.indexBy([1, 2, 3], func(int n): bool { return n > 10; }));
`, "2\n-1\n")
}

func TestParityCollectionsBinarySearch(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [1, 3, 5, 7, 9, 11];
io.println(collections.binarySearch(xs, 7));
io.println(collections.binarySearch(xs, 1));
io.println(collections.binarySearch(xs, 11));
io.println(collections.binarySearch(xs, 4));
`, "3\n0\n5\n-1\n")
}

func TestParityCollectionsLowerBound(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [1, 3, 5, 7, 9];
io.println(collections.lowerBound(xs, 5));
io.println(collections.lowerBound(xs, 4));
io.println(collections.lowerBound(xs, 0));
io.println(collections.lowerBound(xs, 10));
`, "2\n2\n0\n5\n")
}

func TestParityCollectionsUpperBound(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [1, 3, 5, 7, 9];
io.println(collections.upperBound(xs, 5));
io.println(collections.upperBound(xs, 4));
io.println(collections.upperBound(xs, 0));
io.println(collections.upperBound(xs, 10));
`, "3\n2\n0\n5\n")
}

func TestParityCollectionsMinBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 1, 4, 1, 5];
io.println(collections.minBy(xs, func(int n): int { return n; }));
io.println(collections.minBy([], func(int n): int { return n; }));
`, "1\nnull\n")
}

func TestParityCollectionsMaxBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 1, 4, 1, 5];
io.println(collections.maxBy(xs, func(int n): int { return n; }));
io.println(collections.maxBy([], func(int n): int { return n; }));
`, "5\nnull\n")
}

func TestParityCollectionsSortBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 1, 4, 1, 5];
let s = collections.sortBy(xs, func(int n): int { return n; });
io.println(s[0]);
io.println(s[4]);
io.println(collections.sortBy(xs, func(int n): int { return -n; })[0]);
let desc = collections.sortBy(xs, func(int n): int { return n; }, true);
io.println(desc[0]);
io.println(desc[4]);
let asc = collections.sortBy(xs, func(int n): int { return n; }, false);
io.println(asc[0]);
`, "1\n5\n5\n5\n1\n1\n")
}

func TestParityCollectionsTopBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 1, 4, 1, 5, 9, 2, 6];
let top3 = collections.topBy(xs, func(int n): int { return n; }, 3);
io.println(top3[0]);
io.println(top3[1]);
io.println(top3[2]);
io.println(top3.length());
`, "9\n6\n5\n3\n")
}

func TestParityCollectionsSumBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 7, 10];
io.println(collections.sumBy(xs, func(int n): int { return n; }));
io.println(collections.sumBy([], func(int n): int { return n; }));
`, "20\n0\n")
}

func TestParityCollectionsAverageBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [1, 2, 3];
io.println(collections.averageBy(xs, func(int n): int { return n; }));
io.println(collections.averageBy([], func(int n): int { return n; }));
`, "2\nnull\n")
}

func TestParityCollectionsTopKBottomK(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [3, 1, 4, 1, 5, 9, 2, 6];
let top = collections.topK(xs, 3);
let bottom = collections.bottomK(xs, 2);
io.println(top);
io.println(bottom);
io.println(collections.topK(xs, -1).length());
`, "[9, 6, 5]\n[1, 1]\n0\n")
}

func TestParityCollectionsFrequenciesAndMode(t *testing.T) {
	runParity(t, `import io;
import collections;
let counts = collections.frequencies(["a", "b", "a", "c", "b", "a"]);
io.println(counts["a"]);
io.println(counts["b"]);
io.println(counts["missing"]);
io.println(collections.mode(["a", "b", "a", "c"]));
io.println(collections.mode([]));
`, "3\n2\nnull\na\nnull\n")
}

func TestParityCollectionsDifferenceIntersection(t *testing.T) {
	runParity(t, `import io;
import collections;
let a = [1, 2, 3, 4, 5];
let b = [2, 4, 6];
io.println(collections.difference(a, b));
io.println(collections.intersection(a, b));
io.println(collections.difference([], b));
io.println(collections.intersection([], b));
`, "[1, 3, 5]\n[2, 4]\n[]\n[]\n")
}

func TestParityCollectionsDifferenceByIntersectionBy(t *testing.T) {
	runParity(t, `import io;
import collections;
let words = ["apple", "banana", "cherry", "avocado"];
let exclude = ["a", "c"];
let diff = collections.differenceBy(words, exclude, func(string s): string { return s[0]; });
let inter = collections.intersectionBy(words, exclude, func(string s): string { return s[0]; });
io.println(diff);
io.println(inter);
`, "[\"banana\"]\n[\"apple\", \"cherry\", \"avocado\"]\n")
}

func TestParityCollectionsZipWith(t *testing.T) {
	runParity(t, `import io;
import collections;
let a = [1, 2, 3];
let b = [10, 20, 30];
let sums = collections.zipWith(a, b, func(int x, int y): int { return x + y; });
io.println(sums);
let shorter = collections.zipWith([1, 2], [10, 20, 30], func(int x, int y): int { return x + y; });
io.println(shorter);
`, "[11, 22, 33]\n[11, 22]\n")
}

func TestParityCollectionsBFS(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
let r = collections.bfs(g, "a");
io.println(r);
`, "[\"a\", \"b\", \"c\", \"d\"]\n")
}

func TestParityCollectionsBFSChain(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b"], "b": ["c"], "c": []};
io.println(collections.bfs(g, "a"));
io.println(collections.bfs(g, "c"));
`, "[\"a\", \"b\", \"c\"]\n[\"c\"]\n")
}

func TestParityCollectionsDFS(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
let r = collections.dfs(g, "a");
io.println(r);
`, "[\"a\", \"b\", \"d\", \"c\"]\n")
}

func TestParityCollectionsDFSChain(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b"], "b": ["c"], "c": []};
io.println(collections.dfs(g, "a"));
`, "[\"a\", \"b\", \"c\"]\n")
}

func TestParityCollectionsTopologicalSort(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
let r = collections.topologicalSort(g);
io.println(r);
`, "[\"a\", \"b\", \"c\", \"d\"]\n")
}

func TestParityCollectionsTopologicalSortChain(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b"], "b": ["c"], "c": []};
io.println(collections.topologicalSort(g));
`, "[\"a\", \"b\", \"c\"]\n")
}

func TestParityCollectionsShortestPath(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
io.println(collections.shortestPath(g, "a", "d"));
io.println(collections.shortestPath(g, "a", "a"));
`, "[\"a\", \"b\", \"d\"]\n[\"a\"]\n")
}

func TestParityCollectionsShortestPathUnreachable(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b"], "b": [], "c": ["d"], "d": []};
io.println(collections.shortestPath(g, "a", "c"));
io.println(collections.shortestPath(g, "d", "a"));
`, "null\nnull\n")
}

func TestParityCollectionsShortestPathChain(t *testing.T) {
	runParity(t, `import io;
import collections;
let g = {"a": ["b"], "b": ["c"], "c": []};
io.println(collections.shortestPath(g, "a", "c"));
`, "[\"a\", \"b\", \"c\"]\n")
}

func TestParityCompoundAssignIndex(t *testing.T) {
	runParity(t, `import io;
let arr = [10, 20, 30];
arr[1] += 5;
io.println(arr[1] as string);
arr[0] *= 3;
io.println(arr[0] as string);
`, "25\n30\n")
}

func TestParityRangeLength(t *testing.T) {
	runParity(t, `import io;
let r = 0..9;
io.println(r.length() as string);
`, "10\n")
}

func TestParityRangeLengthByStep(t *testing.T) {
	runParity(t, `import io;
let r = 0..10 by 2;
io.println(r.length() as string);
`, "6\n")
}

func TestParityRangeLengthExclusive(t *testing.T) {
	runParity(t, `import io;
let r = 0..<10;
io.println(r.length() as string);
`, "10\n")
}

func TestParityZRange(t *testing.T) {
	runParity(t, `import io;
io.println("${zrange(0, 5)}");
io.println("${zrange(5)}");
io.println("${zrange(2, 8, 2)}");
io.println("${zrange(5, 0)}");
`, "[0, 1, 2, 3, 4]\n[0, 1, 2, 3, 4]\n[2, 4, 6]\n[5, 4, 3, 2, 1]\n")
}

func TestParityRangeIsEmpty(t *testing.T) {
	runParity(t, `import io;
let r = 5..3;
io.println(r.isEmpty() as string);
`, "true\n")
}

func TestParityRangeContains(t *testing.T) {
	runParity(t, `import io;
let r = 0..10 by 2;
io.println(r.contains(4) as string);
io.println(r.contains(3) as string);
io.println(r.contains(10) as string);
`, "true\nfalse\ntrue\n")
}

func TestParityRangeFirst(t *testing.T) {
	runParity(t, `import io;
let r = 5..20 by 5;
io.println(r.first() as string);
`, "5\n")
}

func TestParityRangeLast(t *testing.T) {
	runParity(t, `import io;
let r = 0..10 by 3;
io.println(r.last() as string);
`, "9\n")
}

func TestParityRangeToList(t *testing.T) {
	runParity(t, `import io;
let r = 1..5;
let list = r.toList();
for (n in list) {
    io.print(n as string + " ");
}
io.println("");
`, "1 2 3 4 5 \n")
}

func TestParityRangeInspect(t *testing.T) {
	runParity(t, `import io;
let r = 0..10 by 2;
io.println(r as string);
`, "0..10 by 2\n")
}

func TestParityCollectionElementTypeEnforcement(t *testing.T) {
	runParity(t, `
import io;
func sumInts(list<int> items): int {
    let total = 0;
    for (item in items) { total = total + item; }
    return total;
}
io.println(sumInts([1, 2, 3]));
`, "6\n")

	runErrorParity(t, `
func sumInts(list<int> items): int {
    return 0;
}
sumInts(["a", "b"]);
`, "expects list<int>")

	runErrorParity(t, `
func takeStrings(list<string> items): void {}
takeStrings([1, 2, 3]);
`, "expects list<string>")

	runErrorParity(t, `
func takeDictStringInt(dict<string, int> d): void {}
takeDictStringInt({"a": "not-an-int"});
`, "expects dict<string")

	runErrorParity(t, `
func takeSetInt(set<int> s): void {}
takeSetInt({"a", "b"});
`, "expects set<int>")

	runParity(t, `
import io;
func nested(list<dict<string, int>> rows): int {
    return rows[0]["count"];
}
io.println(nested([{"count": 7}]));
`, "7\n")

	runErrorParity(t, `
func nested(list<dict<string, int>> rows): void {}
nested([{"count": "bad"}]);
`, "expects list<dict<string")
}

// TestParityListInPlaceAppend covers the new in-place list growth
// methods (append, extend, clear) that mutate the receiver and
// participate in reference semantics.
func TestParityListInPlaceAppend(t *testing.T) {
	// append mutates in place and is observable through aliases.
	runParity(t, `import io;
let xs = [1, 2];
let ys = xs;
xs.append(3);
io.println(ys);
`, "[1, 2, 3]\n")

	// extend appends every element of another list.
	runParity(t, `import io;
let xs = [1, 2];
xs.extend([3, 4, 5]);
io.println(xs);
`, "[1, 2, 3, 4, 5]\n")

	// clear empties the list in place.
	runParity(t, `import io;
let xs = [1, 2, 3];
xs.clear();
io.println(xs);
io.println(xs.length());
`, "[]\n0\n")

	// dict.clear empties the dict in place.
	runParity(t, `import io;
let d = {"a": 1, "b": 2};
d.clear();
io.println(d);
io.println(d.length());
`, "{}\n0\n")

	// append on a frozen list raises ImmutableError.
	runErrorParity(t, `import freeze;
let xs = freeze.shallow([1, 2]);
xs.append(3);
`, "ImmutableError")

	// extend on a frozen list raises ImmutableError.
	runErrorParity(t, `import freeze;
let xs = freeze.shallow([1, 2]);
xs.extend([3]);
`, "ImmutableError")

	// clear on a frozen list raises ImmutableError.
	runErrorParity(t, `import freeze;
let xs = freeze.shallow([1, 2]);
xs.clear();
`, "ImmutableError")

	// clear on a frozen dict raises ImmutableError.
	runErrorParity(t, `import freeze;
let d = freeze.shallow({"a": 1});
d.clear();
`, "ImmutableError")

	// Typed-list append rejects wrong element type at runtime when
	// the typed list flows through an any-channel.
	runParity(t, `import io;
func bad(any container, any item): void {
    try {
        (container as list<int>).append(item);
    } catch (TypeError e) {
        io.println(e.message);
    }
}
list<int> xs = [1, 2];
bad(xs, "oops");
io.println(xs);
`, "cannot append string to list<int>\n[1, 2]\n")

	// Typed-list extend rejects wrong element type.
	runParity(t, `import io;
func bad(any container, any items): void {
    try {
        (container as list<int>).extend(items as list<any>);
    } catch (TypeError e) {
        io.println(e.message);
    }
}
list<int> xs = [1, 2];
bad(xs, [3, "oops"]);
io.println(xs);
`, "cannot extend list<int> with string at index 1\n[1, 2]\n")

	// append returns null (in-place methods do not return the receiver).
	runParity(t, `import io;
let xs = [1, 2];
let r = xs.append(3);
io.println(r);
`, "null\n")

	// extend rejects non-list argument.
	runErrorParity(t, `let xs = [1, 2]; xs.extend(99);`, "list.extend expects a list argument")
}

func TestParityListCopyMethods(t *testing.T) {
	// reverse mutates in place and returns the receiver (1.16.0).
	runParity(t, `import io;
let xs = [1, 2, 3];
io.println(xs.reverse());
io.println(xs);
`, "[3, 2, 1]\n[3, 2, 1]\n")

	// reversed is the copy variant; original unchanged.
	runParity(t, `import io;
let xs = [1, 2, 3];
io.println(xs.reversed());
io.println(xs);
`, "[3, 2, 1]\n[1, 2, 3]\n")

	// reverse on empty.
	runParity(t, `import io; io.println(([] as list<int>).reverse());`, "[]\n")

	// reverse chains after sorted (sorted copies, so the source survives).
	runParity(t, `import io;
let src = [3, 1, 4, 1, 5];
io.println(src.sorted().reverse());
io.println(src);
`, "[5, 4, 3, 1, 1]\n[3, 1, 4, 1, 5]\n")

	// prepend mutates in place and returns the receiver.
	runParity(t, `import io;
let xs = [2, 3];
io.println(xs.prepend(1));
io.println(xs);
`, "[1, 2, 3]\n[1, 2, 3]\n")

	// unshift is an alias of prepend.
	runParity(t, `import io;
io.println([2, 3].unshift(1));
`, "[1, 2, 3]\n")

	// remove drops the first matching element in place.
	runParity(t, `import io;
let xs = [3, 1, 4, 1, 5];
io.println(xs.remove(1));
io.println(xs);
`, "[3, 4, 1, 5]\n[3, 4, 1, 5]\n")

	// remove with no match leaves the list unchanged.
	runParity(t, `import io;
io.println([1, 2, 3].remove(99));
`, "[1, 2, 3]\n")
}

func TestParityLiteralSpread(t *testing.T) {
	// List literal spread (already worked before L3; regression guard).
	runParity(t, `import io;
let xs = [1, 2, 3];
io.println([0, ...xs, 4]);
`, "[0, 1, 2, 3, 4]\n")

	// Dict literal spread - probe via content (length + key lookups) to
	// sidestep a pre-existing parity bug in dict iteration order display.
	runParity(t, `import io;
let d1 = {"a": 1, "b": 2};
let d2 = {"x": 0, ...d1, "y": 4};
io.println(d2.length());
io.println(d2["x"]);
io.println(d2["a"]);
io.println(d2["b"]);
io.println(d2["y"]);
`, "4\n0\n1\n2\n4\n")

	// Last-write-wins on key collision.
	runParity(t, `import io;
let d1 = {"a": 1, "b": 2};
let d3 = {...d1, "b": 99};
io.println(d3.length());
io.println(d3["a"]);
io.println(d3["b"]);
`, "2\n1\n99\n")

	// Multiple spread sources, later wins.
	runParity(t, `import io;
let a = {"x": 1, "y": 2};
let b = {"y": 99, "z": 3};
let m = {...a, ...b};
io.println(m.length());
io.println(m["x"]);
io.println(m["y"]);
io.println(m["z"]);
`, "3\n1\n99\n3\n")

	// Set literal spread.
	runParity(t, `import io;
let s1 = {1, 2, 3};
let s2 = {0, ...s1, 4};
io.println(s2.length());
io.println(s2.contains(0));
io.println(s2.contains(2));
io.println(s2.contains(4));
`, "5\ntrue\ntrue\ntrue\n")

	// Set spread from list source.
	runParity(t, `import io;
let xs = [10, 20];
let s = {...xs, 30};
io.println(s.length());
io.println(s.contains(10));
io.println(s.contains(30));
`, "3\ntrue\ntrue\n")

}

func TestParityPipeOperator(t *testing.T) {
	runParity(t, `import io;
func double(int x): int { return x * 2; }
io.println(5 |> double());
`, "10\n")

	runParity(t, `import io;
func double(int x): int { return x * 2; }
io.println(5 |> double);
`, "10\n")

	runParity(t, `import io;
func add(int a, int b): int { return a + b; }
io.println(5 |> add(3));
`, "8\n")

	runParity(t, `import io;
func double(int x): int { return x * 2; }
func add(int a, int b): int { return a + b; }
io.println(5 |> double() |> add(1));
`, "11\n")

	runParity(t, `import io;
func double(int x): int { return x * 2; }
func add(int a, int b): int { return a + b; }
io.println(10 |> add(5) |> double);
`, "30\n")

	runParity(t, `import io;
class S { static func tag(string s, string suffix): string { return s + ":" + suffix; } }
io.println("hi" |> S.tag("end"));
`, "hi:end\n")
}

func TestParityComprehensions(t *testing.T) {
	runParity(t, `import io;
io.println([x * 2 for x in [1, 2, 3, 4, 5]]);
`, "[2, 4, 6, 8, 10]\n")

	runParity(t, `import io;
io.println([x for x in [1, 2, 3, 4, 5] if x % 2 == 0]);
`, "[2, 4]\n")

	runParity(t, `import io;
io.println([x for x in [1, 2, 3, 4, 5] if x > 1 if x < 5]);
`, "[2, 3, 4]\n")

	runParity(t, `import io;
io.println([x * y for x in [1, 2, 3] for y in [10, 20, 30]]);
`, "[10, 20, 30, 20, 40, 60, 30, 60, 90]\n")

	runParity(t, `import io;
io.println({x * x for x in [1, 2, 3, 2, 1]});
`, "set{1, 4, 9}\n")

	runParity(t, `import io;
io.println({x: x * x for x in [1, 2, 3]});
`, "{1: 1, 2: 4, 3: 9}\n")

	runParity(t, `import io;
io.println([x * 2 for int x in [1, 2, 3]]);
`, "[2, 4, 6]\n")

	runParity(t, `import io;
let mul = 100;
io.println([x * mul for x in [1, 2, 3]]);
`, "[100, 200, 300]\n")

	runParity(t, `import io;
io.println([x * y for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10]);
`, "[40, 60]\n")

	runParity(t, `import io;
io.println([[y for y in range(0, x)] for x in range(0, 4)]);
`, "[[0], [0, 1], [0, 1, 2], [0, 1, 2, 3], [0, 1, 2, 3, 4]]\n")

	runParity(t, `import io;
io.println([x for x in []]);
`, "[]\n")
}

func TestParityDictSpreadIgnoresExtras(t *testing.T) {
	runParity(t, `import io;
func greet(string name, int age, bool active = true): string {
    return name + "/" + (age as string) + "/" + (active as string);
}
let d = {"name": "bob", "age": 43, "extra": 9.9, "active": false};
io.println(greet(...d));
`, "bob/43/false\n")

	runParity(t, `import io;
func greet(string name, int age, bool active = true): string {
    return name + "/" + (age as string) + "/" + (active as string);
}
let d = {"name": "alice", "age": 30, "ignored": 1};
io.println(greet(...d));
`, "alice/30/true\n")

	runParity(t, `import io;
func greet(string name, int age, bool active = true): string {
    return name + "/" + (age as string) + "/" + (active as string);
}
let d = {"age": 60, "active": false, "junk": "x"};
io.println(greet("frank", ...d));
`, "frank/60/false\n")
}

func TestParityDictAliases(t *testing.T) {
	// entries is an alias of items.
	runParity(t, `import io;
io.println({"a": 1, "b": 2}.entries());
`, "[[\"a\", 1], [\"b\", 2]]\n")

	// insert is an alias of set; both mutate in place and return null.
	runParity(t, `import io;
let d = {"a": 1};
let r = d.insert("b", 2);
io.println(r);
io.println(d);
`, "null\n{\"a\": 1, \"b\": 2}\n")

	// remove is an alias of delete on dicts.
	runParity(t, `import io;
let d = {"a": 1, "b": 2};
d.remove("a");
io.println(d);
`, "{\"b\": 2}\n")
}

// TestParitySliceStep covers the J1 Python-style xs[a:b:step] syntax
// including negative step for reversed iteration.
func TestParitySliceStep(t *testing.T) {
	runParity(t, `import io;
let xs = [10, 20, 30, 40, 50];
io.println(xs[::-1]);
io.println(xs[::2]);
io.println(xs[1:4]);
io.println("hello"[::-1]);
`, "[50, 40, 30, 20, 10]\n[10, 30, 50]\n[20, 30, 40]\nolleh\n")
}

// TestParitySpreadOnCallable covers the fix where the bytecode compiler
// did not emit a spread-aware opcode for calls of the form `fn(...args)`
// when fn is a local/global value (not a known top-level function). The
// spread list was passed as a single arg instead of being expanded.
func TestParitySpreadOnCallable(t *testing.T) {
	runParity(t, `import io;
func add(int a, int b, int c): int { return a + b + c; }
let fn = add;
let xs = [1, 2, 3];
io.println(fn(...xs));
let curry = func(any ...prefix): func {
  return func(any ...rest): any {
    let all = prefix.concat(rest);
    return fn(...all);
  };
};
let g = curry(10);
io.println(g(20, 30));
`, "6\n60\n")
}

// TestParityRangeBuiltin verifies the top-level `range(start, end[, step])`
// shorthand produces identical inclusive lists on both backends.
func TestParityRangeBuiltin(t *testing.T) {
	runParity(t, `import io;
io.println(range(1, 5));
io.println(range(10, 2, -2));
io.println(range(5, 1));
`, "[1, 2, 3, 4, 5]\n[10, 8, 6, 4, 2]\n[5, 4, 3, 2, 1]\n")
}

// TestParityCharRange verifies `'a'..'z'` produces a list<string> on both
// backends and respects the exclusive variant.
func TestParityCharRange(t *testing.T) {
	runParity(t, `import io;
io.println('a'..'e');
io.println('a'..<'e');
`, "[\"a\", \"b\", \"c\", \"d\", \"e\"]\n[\"a\", \"b\", \"c\", \"d\"]\n")
}

// TestParityListToListNoop verifies list.toList() is a no-op pass-through,
// preserving order.
func TestParityListToListNoop(t *testing.T) {
	runParity(t, `import io;
io.println([1, 2, 3].toList());
`, "[1, 2, 3]\n")
}

// TestParityTrailingCommaInListLiteral guards a parser regression
// where a trailing comma in a list literal raised "expected
// expression, got ]". Trailing commas are legal in dict/set literals
// already; list literals now agree.
func TestParityTrailingCommaInListLiteral(t *testing.T) {
	runParity(t, `import io;

let a = [1, 2, 3,];
let b = [
    "x",
    "y",
];
io.println(a.length());
io.println(b.length());
`, "3\n2\n")
}

// TestParityDictKeyFastPath guards the VM's `dictKeyFor` helper
// (a fast-path wrapper around native.DictKey for the common String
// and SmallInt key types). Hot dict ops (`dict[\"k\"]`, `dict.get`,
// `dict.contains`) use it; mixed key types still produce the
// canonical key string via native.DictKey.
func TestParityDictKeyFastPath(t *testing.T) {
	runParity(t, `import io;

dict<string, int> d = {};
for (int i = 0; i < 5; i++) {
    let k = "k" + (i as string);
    d[k] = i;
}
for (int i = 0; i < 5; i++) {
    let k = "k" + (i as string);
    io.println(d.contains(k));
    io.println(d.get(k));
}

dict<int, string> ints = {1: "one", 2: "two"};
io.println(ints.contains(1));
io.println(ints.get(2));

dict<any, int> mixed = {};
mixed["k"] = 1;
mixed[42] = 2;
mixed[true] = 3;
io.println(mixed.contains("k"));
io.println(mixed.contains(42));
io.println(mixed.contains(true));
io.println(mixed.length());
`, "true\n0\ntrue\n1\ntrue\n2\ntrue\n3\ntrue\n4\ntrue\ntwo\ntrue\ntrue\ntrue\n3\n")
}

// TestParityListSortAliasesSorted guards the `list.sort()` -> `list.sorted()`
// alias: the LSP catalog (`internal/lsp/catalog.go:142-143`) advertised both
// names but only `sorted` was dispatched at runtime, so any user code reading
// the documented surface and writing `xs.sort()` failed with
// `list has no method sort`. Both backends now accept both names.
func TestParityListSortAliasesSorted(t *testing.T) {
	runParity(t, `import io;
let xs = [3, 1, 4, 1, 5, 9, 2, 6];
io.println(xs.sort());
io.println(xs.sorted());
let desc = xs.sort(func(int a, int b): bool { return a > b; });
io.println(desc);
`, "[1, 1, 2, 3, 4, 5, 6, 9]\n[1, 1, 2, 3, 4, 5, 6, 9]\n[9, 6, 5, 4, 3, 2, 1, 1]\n")
}

// TestParityCallableSpread guards the lifted compiler parity gap:
// spread arguments on a callable VALUE (parenthesized selector
// expression like `(obj.fn)(...args)`, or any complex callable
// expression like `arr[i](...args)` or `getFn()(...args)`) used to
// route to the evaluator. The VM now compiles both forms directly,
// emitting OpMethodCallSpread with `__invoke`.
func TestParityCallableSpread(t *testing.T) {
	runParity(t, `import io;

class Holder {
    callable adder;
    func Holder() {
        this.adder = func(int a, int b, int c): int { return a + b + c; };
    }
}

let h = Holder();
let args = [1, 2, 3];
/* parenthesized selector callable spread */
io.println((h.adder)(...args));

/* complex callable spread: indexed list element */
let fns = [func(int a, int b): int { return a * b; }];
io.println(fns[0](...[4, 5]));

/* complex callable spread: function-call result */
func getMul(): callable {
    return func(int a, int b, int c): int { return a * b * c; };
}
io.println(getMul()(...[2, 3, 4]));
`, "6\n20\n24\n")
}

func TestParityStreamsForInIteratesLines(t *testing.T) {
	runParityWithStdlib(t, `import io;
import streams;

let mem = streams.memory("a\nb\nc\n");
for (line in mem) {
    io.println(line);
}
`, "a\nb\nc\n")
}

func TestParityProcStdinPipe(t *testing.T) {
	runParityWithStdlib(t, `import io;
import proc;

let p = proc.spawn("cat", []);
p.stdin.write("ping\n");
p.stdin.close();
let out = p.stdout.readAll();
let code = p.wait();
io.print(out);
io.println(code);
`, "ping\n0\n")
}

// New collection operations (flatMap/uniqueBy/takeWhile/dropWhile/windowed/
// unzip/scan) behave identically on both backends and via both surfaces.
func TestParityCollectionOps(t *testing.T) {
	runParity(t, `import io;
import collections;
let xs = [1, 2, 3, 4, 5];
io.println(xs.flatMap(func(int x): list<int> { return [x, x * 10]; }));
io.println([{"k":1},{"k":1},{"k":2}].uniqueBy(func(dict<string,any> d): any { return d["k"]; }));
io.println(xs.takeWhile(func(int x): bool { return x < 3; }));
io.println(xs.dropWhile(func(int x): bool { return x < 3; }));
io.println(xs.windowed(2));
io.println(xs.windowed(3, 2));
io.println([[1,"a"],[2,"b"]].unzip());
io.println(xs.scan(0, func(int acc, int x): int { return acc + x; }));
io.println(collections.flatMap(xs, func(int x): list<int> { return [x]; }));
io.println(collections.windowed(xs, 2));
io.println(collections.scan(xs, 0, func(int acc, int x): int { return acc + x; }));
`, "[1, 10, 2, 20, 3, 30, 4, 40, 5, 50]\n[{\"k\": 1}, {\"k\": 2}]\n[1, 2]\n[3, 4, 5]\n[[1, 2], [2, 3], [3, 4], [4, 5]]\n[[1, 2, 3], [3, 4, 5]]\n[[1, 2], [\"a\", \"b\"]]\n[0, 1, 3, 6, 10, 15]\n[1, 2, 3, 4, 5]\n[[1, 2], [2, 3], [3, 4], [4, 5]]\n[0, 1, 3, 6, 10, 15]\n")
}

// Sorting/searching ergonomics: dual-mode sort callbacks, sort(string.compare),
// sortBy descending, binarySearchBy, type statics as values, and slicing -
// identical on both backends.
func TestParitySortingAndSearching(t *testing.T) {
	runParity(t, `import io;
io.println([3,1,2].sort(func(int a, int b): bool { return a < b; }));
io.println([3,1,2].sort(func(int a, int b): int { return b - a; }));
io.println(["banana","apple","cherry"].sort(string.compare));
io.println([{"n":3},{"n":1},{"n":2}].sortBy(func(dict<string,any> x): any { return x["n"]; }, true));
io.println([1,3,5,7].binarySearch(5));
io.println([{"n":1},{"n":3},{"n":5}].binarySearchBy(func(dict<string,any> x): any { return x["n"]; }, 3));
io.println([1,2,3,4,5][::-1]);
io.println([1,2,3,4,5][::2]);
let cmp = string.compare;
io.println(cmp("a","b"));
`, "[1, 2, 3]\n[3, 2, 1]\n[\"apple\", \"banana\", \"cherry\"]\n[{\"n\": 3}, {\"n\": 2}, {\"n\": 1}]\n2\n1\n[5, 4, 3, 2, 1]\n[1, 3, 5]\n-1\n")
}

// searchFilter: dict-criteria metadata filtering (eq, range, in) over the
// in-memory store, on both backends.
func TestParityVectorStoreFilter(t *testing.T) {
	runParityWithStdlib(t, `import io;
import vectorstore as vs;
let store = vs.MemoryVectorStore();
store.add("a", [0.1, 0.2, 0.9], {"lang": "en", "year": 2020});
store.add("b", [0.12, 0.22, 0.88], {"lang": "fr", "year": 2021});
store.add("c", [0.11, 0.21, 0.89], {"lang": "en", "year": 2023});
io.println(store.searchFilter([0.1, 0.2, 0.9], 5, {"lang": "en"}).length());
io.println(store.searchFilter([0.1, 0.2, 0.9], 5, {"year": {"gte": 2021}}).length());
let r = store.searchFilter([0.1, 0.2, 0.9], 5, {"lang": "en", "year": {"gt": 2020}});
io.println((r.length() as string) + " " + r[0].record.id);
let inq = store.searchFilter([0.1, 0.2, 0.9], 5, {"lang": {"in": ["fr", "de"]}});
io.println((inq.length() as string) + " " + inq[0].record.id);
io.println(store.searchFilter([0.1, 0.2, 0.9], 5, {"year": {"ne": 2020}}).length());
`, "2\n2\n1 c\n1 b\n2\n")
}

// Dict spread into a CROSS-MODULE instance method orders named args on
// both backends (closes known-divergence 16 via ModuleMethodParamNames).
func TestParityCrossModuleDictSpreadMethod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(`module shapes;
export class Box {
    func Box() {}
    func describe(int width, int height, string label): string {
        return "${label}:${width}x${height}";
    }
}
`), 0o644); err != nil {
		t.Fatalf("write shapes: %v", err)
	}
	source := `import io;
import shapes;
let b = shapes.Box();
let opts = {"label": "crate", "height": 4, "width": 9};
io.println(b.describe(...opts));
io.println(b.describe(...{"width": 1, "height": 2, "label": "tiny", "ignored": true}));
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	const want = "crate:9x4\ntiny:1x2\n"
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: %q want %q", evOut.String(), want)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: %q want %q", vmOut.String(), want)
	}
}

// TestParityCollectionsSurfaceSweep pins one valid call for every
// collections function on both backends. The module is dual-implemented
// (evaluator wrappers vs the VM's collectionsNativeCall switch), so this
// sweep is the drift guard for that surface (audit G, 2026-06-12).
func TestParityCollectionsSurfaceSweep(t *testing.T) {
	runParity(t, `import io;
import collections;

let xs = [3, 1, 2];
let pairs = [[1, "a"], [2, "b"]];
let graph = {"a": ["b"], "b": []};
let sel = func(int x): int { return x; };
let pred = func(int x): bool { return x > 1; };
let acc = func(int a, int x): int { return a + x; };
let zipf = func(int a, int b): int { return a + b; };

func probe(string name, any result) {
    io.println("${name}=${result}");
}

probe("map", collections.map(xs, sel));
probe("filter", collections.filter(xs, pred));
probe("reduce", collections.reduce(xs, acc, 0));
probe("find", collections.find(xs, pred));
probe("findLast", collections.findLast(xs, pred));
probe("all", collections.all(xs, pred));
probe("any", collections.any(xs, pred));
probe("sorted", collections.sorted(xs));
probe("sortBy", collections.sortBy(xs, sel));
probe("sortBy3", collections.sortBy(xs, sel, true));
probe("minBy", collections.minBy(xs, sel));
probe("maxBy", collections.maxBy(xs, sel));
probe("sumBy", collections.sumBy(xs, sel));
probe("averageBy", collections.averageBy(xs, sel));
probe("topBy", collections.topBy(xs, sel, 2));
probe("topK", collections.topK(xs, 2));
probe("bottomK", collections.bottomK(xs, 2));
probe("frequencies", collections.frequencies(xs));
probe("mode", collections.mode(xs));
probe("unique", collections.unique([1, 1, 2]));
probe("uniqueBy", collections.uniqueBy(xs, sel));
probe("chunk", collections.chunk(xs, 2));
probe("windowed", collections.windowed(xs, 2));
probe("flatten", collections.flatten([[1], [2]]));
probe("flatMap", collections.flatMap(xs, func(int x): list<int> { return [x, x]; }));
probe("enumerate", collections.enumerate(["a", "b"]));
probe("partition", collections.partition(xs, pred));
probe("groupBy", collections.groupBy(xs, sel));
probe("indexBy", collections.indexBy(xs, sel));
probe("zip", collections.zip([1, 2], ["a", "b"]));
probe("zipWith", collections.zipWith([1, 2], [10, 20], zipf));
probe("unzip", collections.unzip(pairs));
probe("takeWhile", collections.takeWhile(xs, pred));
probe("dropWhile", collections.dropWhile(xs, pred));
probe("binarySearch", collections.binarySearch([1, 2, 3], 2));
probe("lowerBound", collections.lowerBound([1, 2, 2, 3], 2));
probe("upperBound", collections.upperBound([1, 2, 2, 3], 2));
probe("containsBy", collections.containsBy(xs, 2, sel));
probe("difference", collections.difference([1, 2, 3], [2]));
probe("differenceBy", collections.differenceBy([1, 2, 3], [2], sel));
probe("intersection", collections.intersection([1, 2, 3], [2, 3]));
probe("intersectionBy", collections.intersectionBy([1, 2, 3], [2, 3], sel));
probe("scan", collections.scan(xs, 0, acc));
probe("length", collections.length(xs));
probe("isEmpty", collections.isEmpty(xs));
probe("contains", collections.contains(xs, 2));
probe("bfs", collections.bfs(graph, "a"));
probe("dfs", collections.dfs(graph, "a"));
probe("topologicalSort", collections.topologicalSort(graph));
probe("shortestPath", collections.shortestPath(graph, "a", "b"));
`, `map=[3, 1, 2]
filter=[3, 2]
reduce=6
find=3
findLast=2
all=false
any=true
sorted=[1, 2, 3]
sortBy=[1, 2, 3]
sortBy3=[3, 2, 1]
minBy=1
maxBy=3
sumBy=6
averageBy=2
topBy=[3, 2]
topK=[3, 2]
bottomK=[1, 2]
frequencies={1: 1, 2: 1, 3: 1}
mode=3
unique=[1, 2]
uniqueBy=[3, 2, 1]
chunk=[[3, 2], [1]]
windowed=[[3, 2], [2, 1]]
flatten=[1, 2]
flatMap=[3, 3, 2, 2, 1, 1]
enumerate=[[0, "a"], [1, "b"]]
partition=[[3, 2], [1]]
groupBy={1: [1], 2: [2], 3: [3]}
indexBy=-1
zip=[[1, "a"], [2, "b"]]
zipWith=[11, 22]
unzip=[[1, 2], ["a", "b"]]
takeWhile=[3, 2]
dropWhile=[1]
binarySearch=1
lowerBound=1
upperBound=3
containsBy=true
difference=[1, 3]
differenceBy=[1, 3]
intersection=[2, 3]
intersectionBy=[2, 3]
scan=[0, 3, 5, 6]
length=3
isEmpty=false
contains=true
bfs=["a", "b"]
dfs=["a", "b"]
topologicalSort=["a", "b"]
shortestPath=["a", "b"]
`)
}

func TestParityDictSpreadOntoFunctionValue(t *testing.T) {
	runParity(t, `import io;
func f(string q = "Q", string product = "P"): string { return q + "/" + product; }
let g = f;
io.println(g(...{"q": "x"}));
io.println(g(...{"product": "y", "junk": 1}));
io.println(g(...{}));
let lam = func(int a, int b = 9): int { return a * 100 + b; };
io.println(lam(...{"a": 3}));
io.println(lam(...{"a": 3, "b": 4}));
`, "x/P\nQ/y\nQ/P\n309\n304\n")
}
