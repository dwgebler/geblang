package bytecode_test

import "testing"

// Native-module classes (re.Pattern, re.RegexSet, pcre.Pattern, net.IpSet) were not exported as types, so annotations failed.
func TestParityNativeClassTypeAnnotations(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import re;
import pcre;
import net;
re.Pattern p = re.compile("a+");
io.println(p.test("aa"));
re.RegexSet s = re.set(["a", "b"]);
io.println(s.match("b"));
pcre.Pattern q = pcre.compile("(?<=a)b");
io.println(q.test("ab"));
net.IpSet ips = net.ipSet(["10.0.0.0/8"]);
io.println(ips.contains("10.1.1.1"));
io.println("${p instanceof re.Pattern} ${s instanceof re.RegexSet} ${ips instanceof net.IpSet} ${p instanceof re.RegexSet}");
func takes(net.IpSet set, re.Pattern pat): string { return "${set.size()} ${pat.test("a")}"; }
io.println(takes(ips, p));
try { re.Pattern bad = re.set(["a"]) as any; io.println("assigned wrong type"); } catch (Error e) { io.println("wrong type: ${typeof(e)}"); }
`)
	want := "true\n1\ntrue\ntrue\ntrue true true false\n1 true\nwrong type: TypeError\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
