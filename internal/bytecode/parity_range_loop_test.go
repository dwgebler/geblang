package bytecode_test

import "testing"

// A for-in loop over a range literal runs the same on both backends, on the
// 64-bit counting path and on every fallback.
func TestParityRangeLoops(t *testing.T) {
	runParity(t, `import io;
func show(list<any> xs): void { io.println(xs); }
list<any> a = [];
for (i in 0..<5) { a.push(i); }
show(a);
list<any> b = [];
for (i in 1..3) { b.push(i); }
show(b);
list<any> c = [];
for (i in 10..0 by -3) { c.push(i); }
show(c);
list<any> d = [];
for (i in 5..<1) { d.push(i); }
show(d);
list<any> e = [];
for (i in 0..<10) {
    if (i == 2) { continue; }
    if (i == 6) { break; }
    e.push(i);
    i = 100;
}
show(e);
list<any> f = [];
for (i in 9223372036854775805..9223372036854775807) { f.push(i); }
show(f);
list<any> g = [];
for (i in -9223372036854775807..-9223372036854775808 by -1) { g.push(i); }
show(g);
list<any> h = [];
for (i in 9223372036854775808..<9223372036854775810) { h.push(i); }
show(h);
list<any> k = [];
for (ch in 'a'..'d') { k.push(ch); }
show(k);
let r = 2..<4;
list<any> m = [];
for (i in r) { m.push(i); }
show(m);
int n = 3;
list<any> p = [];
for (i in 0..<n) { for (j in i..<n) { p.push("${i}${j}"); } }
show(p);
try {
    for (i in 0..5 by 0) { io.println(i); }
} catch (Error err) {
    io.println(err.message);
}
`, "[0, 1, 2, 3, 4]\n[1, 2, 3]\n[10, 7, 4, 1]\n[]\n[0, 1, 3, 4, 5]\n[9223372036854775805, 9223372036854775806, 9223372036854775807]\n[-9223372036854775807, -9223372036854775808]\n[9223372036854775808, 9223372036854775809]\n[\"a\", \"b\", \"c\", \"d\"]\n[2, 3]\n[\"00\", \"01\", \"02\", \"11\", \"12\", \"22\"]\nrange step cannot be zero\n")
}
