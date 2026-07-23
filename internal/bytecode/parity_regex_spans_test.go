package bytecode_test

import "testing"

// Regex span parity: rune-offset, end-exclusive match positions on both backends.

func TestParityReMatchSpans(t *testing.T) {
	runParity(t, `
import io;
import re;

let m = re.match('(\w+)@(?P<host>\w+)', "joe@example");
io.println(m["span"]);
io.println(m["spans"]);
io.println(m["namedSpans"]);
io.println(m);
`, `[0, 11]
[[0, 11], [0, 3], [4, 11]]
{"host": [4, 11]}
{"text": "joe@example", "span": [0, 11], "groups": ["joe@example", "joe", "example"], "spans": [[0, 11], [0, 3], [4, 11]], "named": {"host": "example"}, "namedSpans": {"host": [4, 11]}}
`)
}

func TestParityReMatchAllSpansMultibyte(t *testing.T) {
	runParity(t, `
import io;
import re;

let text = "héllo wörld wörld";
for (m in re.matchAll('w\S+', text)) {
    let s = m["span"];
    io.println("${m["text"]} ${s} ${text.substring(s[0], s[1])}");
}
`, `wörld [6, 11] wörld
wörld [12, 17] wörld
`)
}

func TestParityReSpanUnmatchedGroup(t *testing.T) {
	runParity(t, `
import io;
import re;

let m = re.match('(a)(?P<opt>b)?', "ac");
io.println(m["spans"]);
io.println(m["namedSpans"]);
`, `[[0, 1], [0, 1], null]
{"opt": null}
`)
}

func TestParityRePatternSpans(t *testing.T) {
	runParity(t, `
import io;
import re;

let p = re.compile('\d+');
io.println(p.match("v12 and 23")["span"]);
for (m in p.matchAll("v12 and 23")) {
    io.println(m["span"]);
}
`, `[1, 3]
[1, 3]
[8, 10]
`)
}

func TestParityPcreMatchSpans(t *testing.T) {
	runParity(t, `
import io;
import pcre;

let m = pcre.match('(?P<word>[a-z]+)([0-9]+)', "abc123");
io.println(m["span"]);
io.println(m["spans"]);
io.println(m["namedSpans"]);
let u = pcre.match('(a)(b)?', "ac");
io.println(u["spans"]);
`, `[0, 6]
[[0, 6], [3, 6], [0, 3]]
{"word": [0, 3]}
[[0, 1], [0, 1], null]
`)
}

func TestParityPcrePatternSpansMultibyte(t *testing.T) {
	runParity(t, `
import io;
import pcre;

let text = "wörld wörterbuch";
for (m in pcre.compile('ö\w+').matchAll(text)) {
    let s = m["span"];
    io.println("${m["text"]} ${s} ${text.substring(s[0], s[1])}");
}
`, `örld [1, 5] örld
örterbuch [7, 16] örterbuch
`)
}
