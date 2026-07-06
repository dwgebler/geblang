package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityBytesToStringRejectsInvalidUTF8(t *testing.T) {
	runParity(t, `import bytes;
import io;
let invalid = bytes.fromHex("61ff62");
try {
    bytes.toString(invalid);
    io.println("module accepted");
} catch (RuntimeError e) {
    io.println(e.getMessage().contains("not valid UTF-8"));
}
try {
    invalid.toString();
    io.println("method accepted");
} catch (RuntimeError e) {
    io.println(e.getMessage().contains("not valid UTF-8"));
}
`, "true\ntrue\n")
}

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

// TestParityOptionResultAbsentPath pins that Option/Result work identically on
// both backends across the absent/error path (a typed unwrapOr fallback on
// none/err, orNull, ofNullable from null) - the path that regressed when an
// absent value bound the generic T to null's type.
func TestParityOptionResultAbsentPath(t *testing.T) {
	runParityWithStdlib(t, `import io;
import option;
import result;
func divideR(int a, int b): result.Result<int, string> {
    if (b == 0) {
        return result.err("err");
    }
    return result.ok(a // b);
}
io.println(option.none<int>().unwrapOr(99));
io.println(option.ofNullable<string>(null).unwrapOr("fb"));
io.println(option.some(5).unwrapOr(0));
io.println(option.none<int>().orNull() == null);
io.println(divideR(5, 0).unwrapOr(-1));
io.println(divideR(10, 2).unwrap());
`, "99\nfb\n5\ntrue\n-1\n5\n")
}

func TestParityMathStdlib(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.abs(-5));
io.println(math.max(3, 7));
io.println(math.min(3, 7));
io.println(math.floor(3.7));
io.println(math.ceil(3.2));
io.println(math.sqrt(16.0));
`, "5\n7\n3\n3\n4\n4\n")
}

func TestParityMathSpecialFunctions(t *testing.T) {
	runParityNumeric(t, `import io;
import math;
io.println(math.gamma(5.0));
io.println(math.lgamma(5.0));
io.println(math.beta(2.0, 3.0));
io.println(math.lbeta(2.0, 3.0));
io.println(math.erf(0.0));
io.println(math.erfc(0.0));
io.println(math.erfinv(0.0));
io.println(math.j0(0.0));
io.println(math.y0(1.0));
io.println(math.y1(1.0));
io.println(math.jn(2, 1.0));
io.println(math.yn(2, 1.0));
`, "24\n3.1780538303479458\n0.08333333333333333\n-2.4849066497880004\n0\n1\n0\n1\n0.08825696421567697\n-0.7812128213002887\n0.11490348493190049\n-1.6506826068162543\n")
}

func TestParityMathCombinatorics(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.factorial(0));
io.println(math.factorial(20));
io.println(math.factorial(25));
io.println(math.comb(50, 25));
io.println(math.comb(5, 8));
io.println(math.perm(20, 10));
io.println(math.gcd(48, 36));
io.println(math.gcd(-12, 8));
io.println(math.lcm(4, 6));
io.println(math.lcm(7, 0));
`, "1\n2432902008176640000\n15511210043330985984000000\n126410606437752\n0\n670442572800\n12\n4\n12\n0\n")
}

func TestParityJSONStdlib(t *testing.T) {
	runParity(t, `import io;
import json;
dict parsed = json.parse("{\"x\": 1}");
io.println(parsed["x"]);
io.println(json.stringify({"name": "geb"}));
`, "1\n{\"name\":\"geb\"}\n")
}

func TestParityBytesStdlib(t *testing.T) {
	runParity(t, `import io;
import bytes;
io.println(bytes.fromHex("48656c6c6f").toString());
io.println(bytes.fromString("hi").toHex());
`, "Hello\n6869\n")
}

func TestParityBytesBase64Url(t *testing.T) {
	runParity(t, `import io;
import bytes;
let raw = bytes.fromHex("fbff");
io.println(raw.toBase64Url());
io.println(bytes.toBase64Url(raw));
io.println(bytes.toHex(bytes.fromBase64Url("-_8")));
`, "-_8\n-_8\nfbff\n")
}

func TestParityCryptHashAcceptsBytes(t *testing.T) {
	runParity(t, `import io;
import bytes;
import crypt;
let raw = bytes.fromString("hi");
io.println(crypt.sha256(raw));
io.println(crypt.sha256("hi"));
io.println(crypt.md5(raw));
io.println(crypt.sha1(bytes.fromHex("00ff")));
`, "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4\n8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4\n49f68a5c8493ec2c0bf489821c21fc3b\naa3e5dcdd77b153f2e59bd0d8794fde33cb4e486\n")
}

func TestParityReModule(t *testing.T) {
	runParity(t, `import io;
import re;
io.println(re.test("\\d+", "abc123") as string);
io.println(re.test("\\d+", "abc") as string);
io.println(re.find("\\d+", "abc123def"));
let all = re.findAll("\\d+", "a1b22c333");
io.println(all.length() as string);
io.println(all.get(0));
io.println(all.get(1));
io.println(all.get(2));
let groups = re.match("(?P<name>[A-Za-z]+)([0-9]+)", "Ada123");
io.println(groups["text"]);
io.println(groups["groups"][1]);
io.println(groups["groups"][2]);
io.println(groups["named"]["name"]);
io.println(re.replace("o+", "0", "foobar"));
let parts = re.split(",\\s*", "a, b, c");
io.println(parts.length() as string);
`, "true\nfalse\n123\n3\n1\n22\n333\nAda123\nAda\n123\nAda\nf0bar\n3\n")
}

func TestParityMarkdownModule(t *testing.T) {
	runParity(t, "import io;\nimport markdown;\n\nlet source = \"# Title\\n\\nHello **Geblang** and `code`.\\n\\n- one\\n- two\\n\\n```gb\\nio.println(1);\\n```\";\nlet blocks = markdown.parse(source);\nio.println(blocks.length());\nio.println(blocks[0][\"type\"]);\nio.println(blocks[0][\"level\"]);\nio.println(blocks[1][\"text\"]);\nio.println(blocks[2][\"items\"][1]);\nlet html = markdown.renderHtml(source);\nio.println(html.contains(\"<h1 id=\\\"title\\\">Title</h1>\"));\nio.println(html.contains(\"<strong>Geblang</strong>\"));\nio.println(markdown.stripText(source).contains(\"io.println\"));\n", "4\nheading\n1\nHello Geblang and code.\ntwo\ntrue\ntrue\nfalse\n")
}

func TestParityEncodingModule(t *testing.T) {
	runParity(t, `import io;
import encoding;
let encoded = encoding.base64Encode("hello");
io.println(encoded);
io.println(encoding.base64Decode(encoded));
io.println(encoding.urlEncode("a b&c=d"));
io.println(encoding.htmlEscape("<b>hi</b>"));
`, "aGVsbG8=\nhello\na+b%26c%3Dd\n&lt;b&gt;hi&lt;/b&gt;\n")
}

func TestParityURLModule(t *testing.T) {
	runParity(t, `import io;
import url;
let parts = url.parse("https://example.test:8443/api/v1/items?tag=a&tag=b&q=hello+world#top");
io.println(parts["scheme"]);
io.println(parts["host"]);
io.println(parts["port"]);
io.println(parts["path"]);
io.println(parts["query"]["q"]);
io.println(parts["query"]["tag"].length() as string);
io.println(parts["fragment"]);
io.println(url.encode("a b&c=d"));
io.println(url.decode("a+b%26c%3Dd"));
io.println(url.joinPath("https://example.test/api/", "/v1/", "items"));
io.println(url.stringify({
    "scheme": "https",
    "host": "example.test",
    "path": "/search",
    "query": {"q": "hello world", "tag": ["a", "b"]},
    "fragment": "top"
}));
let parsed = url.URL("https://example.test/api/v1/items?tag=a&tag=b&q=hello+world#top");
io.println(parsed.scheme());
io.println(parsed.host());
io.println(parsed.path());
io.println(parsed.query()["tag"].length() as string);
io.println(parsed.withPath("/api/v2/items").withQuery({"page": "2"}).toString());
io.println(parsed.resolve("../users/42").toString());
io.println(url.URL({"scheme": "https", "host": "example.test", "path": "/built"}).toString());
io.println(url.URL("https://example.test/a/../b?z=3&a=1").normalize().toString());
`, "https\nexample.test\n8443\n/api/v1/items\nhello world\n2\ntop\na+b%26c%3Dd\na b&c=d\nhttps://example.test/api/v1/items\nhttps://example.test/search?q=hello+world&tag=a&tag=b#top\nhttps\nexample.test\n/api/v1/items\n2\nhttps://example.test/api/v2/items?page=2#top\nhttps://example.test/api/users/42\nhttps://example.test/built\nhttps://example.test/b?a=1&z=3\n")
}

func TestParityDatetimeAdditions(t *testing.T) {
	runParity(t, `import io;
import datetime;
let parsed = datetime.parse("1970-01-01T00:00:00Z");
io.println(datetime.addDays(parsed, 1) as string);
io.println(datetime.addMonths(parsed, 1) as string);
io.println(datetime.addYears(parsed, 1) as string);
let delta = datetime.diff(parsed, datetime.addSeconds(parsed, 90061));
io.println(delta["days"] as string);
io.println(delta["hours"] as string);
io.println(delta["minutes"] as string);
io.println(delta["seconds"] as string);
io.println(datetime.toLocal(parsed, "UTC"));
io.println(datetime.toUtc(parsed));
let now = datetime.now();
io.println(now.hasKey("year") as string);
`, "86400\n2678400\n31536000\n1\n1\n1\n1\n1970-01-01T00:00:00Z\n1970-01-01T00:00:00Z\ntrue\n")
}

func TestParityDatetimeValueClasses(t *testing.T) {
	runParity(t, `import io;
import datetime;
let start = datetime.Instant("1970-01-01T00:00:00Z");
let duration = datetime.Duration(90061);
let later = start.add(duration);
io.println(start.unix() as string);
io.println(later.toString());
io.println(later.format("2006-01-02"));
let diff = start.diff(later);
io.println(diff.seconds() as string);
let parts = diff.toDict();
io.println(parts["days"] as string);
io.println(parts["hours"] as string);
io.println(parts["minutes"] as string);
io.println(parts["seconds"] as string);
let utc = datetime.Zone("UTC");
io.println(utc.name());
io.println(later.toLocal(utc));
io.println(utc.offsetAt(later) as string);
io.println(start.addDays(1).unix() as string);
io.println(start.addMonths(1).unix() as string);
io.println(start.addYears(1).unix() as string);
`, "0\n1970-01-02T01:01:01Z\n1970-01-02\n90061\n1\n1\n1\n1\nUTC\n1970-01-02T01:01:01Z\n0\n86400\n2678400\n31536000\n")
}

func TestParityUUIDModule(t *testing.T) {
	runParity(t, `import io;
import uuid;
let a = uuid.v4();
let b = uuid.v7();
io.println(a.length() as string);
io.println(a.get(8));
io.println(a.get(13));
io.println(a.get(14));
io.println(a.get(18));
io.println(a.get(23));
io.println(b.length() as string);
io.println(b.get(14));
`, "36\n-\n-\n4\n-\n-\n36\n7\n")
}

func TestParityUUIDExtended(t *testing.T) {
	runParity(t, `import io;
import uuid;
# nil UUID
io.println(uuid.nil());
# namespace constants are stable UUIDs
io.println(uuid.isValid(uuid.namespaceDNS()));
io.println(uuid.isValid(uuid.namespaceURL()));
# isValid
io.println(uuid.isValid("not-a-uuid"));
io.println(uuid.isValid("f47ac10b-58cc-4372-a567-0e02b2c3d479"));
# parse normalises to lowercase
let norm = uuid.parse("F47AC10B-58CC-4372-A567-0E02B2C3D479");
io.println(norm);
# v5 is deterministic
let id5a = uuid.v5(uuid.namespaceDNS(), "example.com");
let id5b = uuid.v5(uuid.namespaceDNS(), "example.com");
io.println(id5a == id5b);
# v3 is deterministic
let id3a = uuid.v3(uuid.namespaceDNS(), "example.com");
let id3b = uuid.v3(uuid.namespaceDNS(), "example.com");
io.println(id3a == id3b);
# v1 produces a valid UUID
io.println(uuid.isValid(uuid.v1()));
# toBytes / fromBytes round-trip
let id = uuid.v4();
let raw = uuid.toBytes(id);
io.println(uuid.fromBytes(raw) == id);
# ULID is 26 characters
let ul = uuid.ulid();
io.println(ul.length() as string);
`, "00000000-0000-0000-0000-000000000000\ntrue\ntrue\nfalse\ntrue\nf47ac10b-58cc-4372-a567-0e02b2c3d479\ntrue\ntrue\ntrue\ntrue\n26\n")
}

func TestParityCryptModule(t *testing.T) {
	runParity(t, `import io;
import crypt;
io.println(crypt.sha256("hello"));
io.println(crypt.sha512("hello").length() as string);
io.println(crypt.md5("hello"));
io.println(crypt.sha1("hello"));
io.println(crypt.sha3_256("hello").length() as string);
io.println(crypt.blake2b("hello").length() as string);
io.println(crypt.crc32("hello") > 0);
io.println(crypt.hmacSha256("key", "data").length() as string);
io.println(crypt.base64Encode("hi"));
io.println(crypt.base64Decode(crypt.base64Encode("hi")));
`, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824\n128\n5d41402abc4b2a76b9719d911017c592\naaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d\n64\n64\ntrue\n64\naGk=\nhi\n")
}

func TestParityJWT(t *testing.T) {
	runParity(t, `import io;
import crypt;
let payload = {"sub": "user-1", "role": "admin"};
let secret = "supersecret";
let token = crypt.jwtSign(payload, secret);
io.println(token.contains(".") as string);
let verified = crypt.jwtVerify(token, secret);
io.println(verified["sub"]);
io.println(verified["role"]);
let bad = crypt.jwtVerify(token, "wrongsecret");
io.println((bad == null) as string);
let decoded = crypt.jwtDecode(token);
io.println(decoded["header"]["alg"]);
io.println(decoded["payload"]["sub"]);
`, "true\nuser-1\nadmin\ntrue\nHS256\nuser-1\n")
}

// re.compile / pcre.compile return Pattern handles whose methods
// mirror the module functions; the plain module functions are
// unchanged (still pure native calls).
func TestParityRegexCompileHandles(t *testing.T) {
	runParity(t, `import io;
import re;
import pcre;
let p = re.compile("[a-z]+[0-9]+");
io.println(p.test("foo123"));
io.println(p.find("__xy42__"));
io.println("${p.findAll("a1 b2")}");
io.println(p.replace("_", "a1 b22"));
let pc = pcre.compile("^foo$", "im");
io.println(pc.test("FOO"));
io.println(pc.test("bar"));
try { re.compile("(broken"); } catch (Error e) { io.println("caught"); }
`, "true\nxy42\n"+`["a1", "b2"]`+"\n_ _\ntrue\nfalse\ncaught\n")
}

// An HS token forged with a verifier's public PEM as the HMAC secret
// must not verify: with no opts.algs the allowed family is pinned to
// the key type (alg-confusion defense).
func TestParityJWTAlgConfusionPinned(t *testing.T) {
	runParity(t, `import io;
import crypt;
let privPem = crypt.generateRsaKey(2048);
let pubPem = crypt.publicKey(privPem);
let rsToken = crypt.jwtSign({"sub": "alice"}, privPem, {"alg": "RS256"});
io.println((crypt.jwtVerify(rsToken, pubPem) != null) as string);
let forged = crypt.jwtSign({"sub": "mallory", "role": "admin"}, pubPem, {"alg": "HS256", "allowedAlgs": ["HS256"]});
io.println((crypt.jwtVerify(forged, pubPem) == null) as string);
io.println((crypt.jwtVerify(forged, pubPem, {"allowedAlgs": ["HS256"]}) != null) as string);
let hsToken = crypt.jwtSign({"sub": "bob"}, "secret");
io.println((crypt.jwtVerify(hsToken, "secret") != null) as string);
io.println((crypt.jwtVerify(rsToken, "secret") == null) as string);
`, "true\ntrue\ntrue\ntrue\ntrue\n")
}

func TestParityMathExtensions(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.log2(8.0));
io.println(math.trunc(3.9));
io.println(math.trunc(-3.9));
io.println(math.sign(-5));
io.println(math.sign(0));
io.println(math.sign(7));
io.println(math.cbrt(27.0));
io.println(math.hypot(3.0, 4.0));
io.println(math.isNaN(math.nan()));
io.println(math.isInf(math.inf()));
io.println(math.isNaN(1.0));
io.println(math.isInf(1.0));
`, "3\n3\n-3\n-1\n0\n1\n3\n5\ntrue\ntrue\nfalse\nfalse\n")
}

// TestParityMathIsPrime covers the small / known-prime cases plus
// negative, zero, one, and the edge between Int and SmallInt.
func TestParityMathIsPrime(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.isPrime(2));
io.println(math.isPrime(3));
io.println(math.isPrime(4));
io.println(math.isPrime(17));
io.println(math.isPrime(97));
io.println(math.isPrime(1));
io.println(math.isPrime(0));
io.println(math.isPrime(-7));
io.println(math.isPrime(1000003));
io.println(math.isPrime(1000004));
`, "true\ntrue\nfalse\ntrue\ntrue\nfalse\nfalse\nfalse\ntrue\nfalse\n")
}

func TestParityBytesContains(t *testing.T) {
	runParity(t, `import io;
import bytes;
let b = bytes.fromString("hello");
io.println(b.contains(104));
io.println(b.contains(122));
`, "true\nfalse\n")
}

func TestParityBytesSlice(t *testing.T) {
	runParity(t, `import io;
import bytes;
let b = bytes.fromHex("0102030405060708090a0b0c0d0e0f10");
io.println(bytes.toHex(b.slice(0, 5)));
io.println(bytes.toHex(b.slice(5)));
io.println(bytes.toHex(b.slice(2, 7)));
io.println(bytes.toHex(b.slice(-3)));
io.println(bytes.toHex(b.slice(0, 0)));
io.println(bytes.toHex(b.slice(100)));
io.println(bytes.toHex(b.slice(-100, 3)));
`, "0102030405\n060708090a0b0c0d0e0f10\n0304050607\n0e0f10\n\n\n010203\n")
}

func TestParityYamlPreservesMappingOrder(t *testing.T) {
	runParity(t, `import io;
import yaml;
let p = yaml.parse("services:\n  Gamma: {}\n  Alpha: {}\n  Beta: {}\n") as dict<string, any>;
let svcs = p["services"] as dict<string, any>;
io.println(svcs.keys());
`, "[\"Gamma\", \"Alpha\", \"Beta\"]\n")
}

func TestParityDatetimeCore(t *testing.T) {
	runParity(t, `import io;
import datetime;
let ts = datetime.nowUnix();
io.println(ts > 0);
let formatted = datetime.unix(ts);
io.println(formatted.length() > 0);
let back = datetime.parse(formatted);
io.println(back > 0);
let d = datetime.format(ts, "2006-01-02");
io.println(d.length() == 10);
`, "true\ntrue\ntrue\ntrue\n")
}

func TestParityDatetimeMake(t *testing.T) {
	runParity(t, `import io;
import datetime;
let ts = datetime.make(2024, 1, 15);
let d = datetime.formatDate(ts);
io.println(d);
let ts2 = datetime.make(2024, 6, 1, 12, 30, 0);
let t2 = datetime.formatTime(ts2);
io.println(t2);
let r = datetime.formatRFC3339(ts);
io.println(r.length() > 0);
let back = datetime.parseRFC3339(r);
io.println(back == ts);
`, "2024-01-15\n12:30:00\ntrue\ntrue\n")
}

func TestParityDatetimeHelpers(t *testing.T) {
	runParity(t, `import io;
import datetime;
io.println(datetime.weekdayName(0));
io.println(datetime.weekdayName(1));
io.println(datetime.weekdayName(6));
io.println(datetime.monthName(1));
io.println(datetime.monthName(12));
`, "Sunday\nMonday\nSaturday\nJanuary\nDecember\n")
}

func TestParityMarkdownRenderHtml(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "# Hello\n\nA paragraph.\n\n- alpha\n- beta\n\n`+"```"+`go\nx := 1\n`+"```"+`";
let h = markdown.renderHtml(src);
io.println(h.contains("<h1"));
io.println(h.contains("Hello</h1>"));
io.println(h.contains("<p>"));
io.println(h.contains("<ul>"));
io.println(h.contains("<li>alpha</li>"));
io.println(h.contains("<pre><code"));
`, "true\ntrue\ntrue\ntrue\ntrue\ntrue\n")
}

func TestParityMarkdownRenderHtmlGFM(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "| a | b |\n|---|---|\n| 1 | 2 |\n\n~~strike~~\n\n- [x] done\n- [ ] todo";
let h = markdown.renderHtml(src);
io.println(h.contains("<table>"));
io.println(h.contains("<th>"));
io.println(h.contains("<del>strike</del>"));
io.println(h.contains("<input"));
`, "true\ntrue\ntrue\ntrue\n")
}

func TestParityMarkdownRenderHtmlBlockquote(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "> quoted text\n\n---";
let h = markdown.renderHtml(src);
io.println(h.contains("<blockquote>"));
io.println(h.contains("<hr"));
`, "true\ntrue\n")
}

func TestParityMarkdownParseBasic(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "## Section\n\nHello world.\n\n- x\n- y\n\n`+"```"+`js\nconsole.log(1);\n`+"```"+`";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
io.println(blocks[0]["level"]);
io.println(blocks[0]["text"]);
io.println(blocks[1]["type"]);
io.println(blocks[1]["text"]);
io.println(blocks[2]["type"]);
io.println(blocks[2]["items"][0]);
io.println(blocks[3]["type"]);
io.println(blocks[3]["lang"]);
`, "4\nheading\n2\nSection\nparagraph\nHello world.\nlist\nx\ncode\njs\n")
}

func TestParityMarkdownParseTable(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "| Name | Score |\n|------|-------|\n| Ada | 10 |\n| Grace | 12 |";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
io.println(blocks[0]["headers"][0]);
io.println(blocks[0]["headers"][1]);
io.println(blocks[0]["rows"][0][0]);
io.println(blocks[0]["rows"][1][1]);
`, "1\ntable\nName\nScore\nAda\n12\n")
}

func TestParityMarkdownParseTaskList(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "- [x] done\n- [ ] todo";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
io.println(blocks[0]["items"][0]["text"]);
io.println(blocks[0]["items"][0]["checked"]);
io.println(blocks[0]["items"][1]["text"]);
io.println(blocks[0]["items"][1]["checked"]);
`, "1\ntask_list\ndone\ntrue\ntodo\nfalse\n")
}

func TestParityMarkdownParseOrderedList(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "1. first\n2. second\n3. third";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
io.println(blocks[0]["items"][0]);
io.println(blocks[0]["items"][2]);
`, "1\nordered_list\nfirst\nthird\n")
}

func TestParityMarkdownParseBlockquote(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "> important note";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
io.println(blocks[0]["text"]);
`, "1\nblockquote\nimportant note\n")
}

func TestParityMarkdownParseHR(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "---";
let blocks = markdown.parse(src);
io.println(blocks.length());
io.println(blocks[0]["type"]);
`, "1\nhr\n")
}

func TestParityMarkdownStripText(t *testing.T) {
	runParity(t, `import io;
import markdown;
let src = "# Title\n\nHello **world**.\n\n`+"```"+`\ncode here\n`+"```"+`";
let stripped = markdown.stripText(src);
io.println(stripped.contains("Title"));
io.println(stripped.contains("Hello"));
io.println(stripped.contains("world"));
io.println(stripped.contains("code here"));
`, "true\ntrue\ntrue\nfalse\n")
}

// TestParityEncodingBase64AndSanitize covers the text-oriented base64 returns
// (base64UrlDecode -> string), base64Encode accepting bytes, and the HTML
// sanitizer, identically on both backends.
func TestParityEncodingBase64AndSanitize(t *testing.T) {
	runParity(t, `
import encoding;
import bytes;
import io;
io.println(encoding.base64UrlDecode("YW55IGNhcm5hbCBwbGVhc3VyZS4"));
io.println(encoding.base64Encode(bytes.fromHex("6869")));
io.println(encoding.base64Decode("aGk="));
let clean = encoding.sanitizeHtml("<p>ok</p><script>x</script><a href=\"/y\" onclick=\"e()\">go</a>");
io.println(clean.contains("script"));
io.println(clean.contains("onclick"));
io.println(clean.contains("<p>ok</p>"));
io.println(encoding.htmlEscape("<b>x</b>"));
`, "any carnal pleasure.\naGk=\nhi\nfalse\nfalse\ntrue\n&lt;b&gt;x&lt;/b&gt;\n")
}

func TestParitySecureRandom(t *testing.T) {
	// Deterministic seed; both backends must produce identical draws + outcomes.
	const seedHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	runParity(t, `import io;
import secureRandom;
let s = secureRandom.fromSeed("`+seedHex+`", "Alice");
io.println(secureRandom.commitment(s));
io.println(secureRandom.uintRange(s, 1, 7));
io.println(secureRandom.uintRange(s, 0, 52));
io.println(secureRandom.bool(s));
io.println(secureRandom.float(s));
io.println(secureRandom.choice(s, ["A", "K", "Q", "J"]));
io.println(secureRandom.shuffle(s, [1, 2, 3, 4, 5, 6]));
io.println(secureRandom.weightedChoice(s, ["x", "y", "z"], [1.0, 2.0, 3.0]));
`, "4884fdaafea47c29fea7159d0daddd9c085d6200e1359e85bb81736af6b7c837\n1\n41\ntrue\n0.5284162290333269\nQ\n[5, 4, 6, 1, 3, 2]\ny\n")

	// Verifier: commitment passes for the right seed, fails for the wrong one.
	runParity(t, `import io;
import secureRandom;
let s = secureRandom.fromSeed("`+seedHex+`", "Bob");
let commit = secureRandom.commitment(s);
let seed = secureRandom.reveal(s);
io.println(secureRandom.verifyCommitment(commit, seed));
io.println(secureRandom.verifyCommitment(commit, "0000000000000000000000000000000000000000000000000000000000000000"));
`, "true\nfalse\n")

	// Replay reproduces the same outcome from raw inputs.
	runParity(t, `import io;
import secureRandom;
let s = secureRandom.fromSeed("`+seedHex+`", "Carol");
let v1 = secureRandom.uintRange(s, 100, 1000);
let v2 = secureRandom.uintRange(s, 100, 1000);
let seed = secureRandom.reveal(s);
let r1 = secureRandom.replay(seed, "Carol", 0, "uintRange", [100, 1000]);
let r2 = secureRandom.replay(seed, "Carol", 1, "uintRange", [100, 1000]);
io.println(v1 == r1);
io.println(v2 == r2);
`, "true\ntrue\n")
}

// TestParityRegexNumberedGroups verifies re.match exposes numbered
// capture groups via the "groups" list on both backends.
func TestParityRegexNumberedGroups(t *testing.T) {
	runParity(t, `import re;
import io;
let m = re.match("([a-z]+)([0-9]+)", "abc123");
io.println(m["text"]);
io.println(m["groups"][0]);
io.println(m["groups"][1]);
io.println(m["groups"][2]);
`, "abc123\nabc123\nabc\n123\n")
}

// TestParityRegexNamedGroups verifies re.match exposes named capture
// groups via the "named" dict on both backends.
func TestParityRegexNamedGroups(t *testing.T) {
	runParity(t, `import re;
import io;
let m = re.match("(?P<word>[a-z]+)(?P<num>[0-9]+)", "abc123");
io.println(m["named"]["word"]);
io.println(m["named"]["num"]);
`, "abc\n123\n")
}

// TestParityRegexMatchAll verifies re.matchAll iterates non-overlapping
// matches and returns a dict per match on both backends.
func TestParityRegexMatchAll(t *testing.T) {
	runParity(t, `import re;
import io;
let all = re.matchAll("([a-z]+)=([0-9]+)", "x=1 y=22 z=333");
io.println(all.length);
io.println(all[0]["groups"][1]);
io.println(all[1]["groups"][2]);
io.println(all[2]["text"]);
`, "3\nx\n22\nz=333\n")
}

// TestParityRegexNoMatch verifies re.match returns null and re.matchAll
// returns an empty list when the pattern does not match.
func TestParityRegexNoMatch(t *testing.T) {
	runParity(t, `import re;
import io;
io.println(re.match("xyz", "abc") == null);
io.println(re.matchAll("xyz", "abc").length);
`, "true\n0\n")
}

// TestParityTimerFires verifies time.scheduler.Timer runs its callback
// once after the requested delay and reports didFire=true on both
// backends.
func TestParityTimerFires(t *testing.T) {
	runParityWithStdlib(t, `import time.scheduler as sched;
import async;
import io;
let fired = false;
let t = sched.Timer(20, func(): void { fired = true; });
async.await(t.wait());
io.println(fired);
io.println(t.didFire());
`, "true\ntrue\n")
}

// TestParityTimerCancelled verifies a Timer cancelled before its delay
// expires reports didFire=false and never invokes the callback.
func TestParityTimerCancelled(t *testing.T) {
	runParityWithStdlib(t, `import time.scheduler as sched;
import async;
import io;
let fired = false;
let t = sched.Timer(200, func(): void { fired = true; });
t.cancel();
async.await(async.sleep(60));
io.println(fired);
io.println(t.didFire());
`, "false\nfalse\n")
}

// TestParityJSONStringifyInstance verifies that an instance with
// no __serialize__ override serialises its public (non-underscore)
// fields as a JSON object on both backends.
func TestParityJSONStringifyInstance(t *testing.T) {
	runParity(t, `import io;
import json;
class Point {
    int x;
    int y;
    int _hidden;
    func Point(int x, int y) { this.x = x; this.y = y; this._hidden = 99; }
}
io.println(json.stringify(Point(3, 4)));
`, "{\"x\":3,\"y\":4}\n")
}

// TestParityJSONSerializeOverride verifies a class can customise
// its serialisation by defining __serialize__().
func TestParityJSONSerializeOverride(t *testing.T) {
	runParity(t, `import io;
import json;
class Tagged {
    string label;
    func Tagged(string label) { this.label = label; }
    func __serialize__(): dict {
        return {"kind": "tagged", "label": this.label};
    }
}
io.println(json.stringify(Tagged("hi")));
`, "{\"kind\":\"tagged\",\"label\":\"hi\"}\n")
}

// TestParityJSONParseAsConstructor verifies json.parseAs uses the
// constructor's parameter names to map dict keys when the class
// has no __deserialize__ static method.
func TestParityJSONParseAsConstructor(t *testing.T) {
	runParity(t, `import io;
import json;
class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}
let q = json.parseAs("{\"x\": 3, \"y\": 4}", Point);
io.println(q.x);
io.println(q.y);
`, "3\n4\n")
}

// TestParityJSONParseAsDeserialize verifies the static
// __deserialize__ hook is preferred when defined.
func TestParityJSONParseAsDeserialize(t *testing.T) {
	runParity(t, `import io;
import json;
class Tagged {
    string kind;
    string label;
    func Tagged(string kind, string label) { this.kind = kind; this.label = label; }
    static func __deserialize__(dict d): Tagged {
        return Tagged(d["kind"] + "-decoded", d["label"]);
    }
}
let t = json.parseAs("{\"kind\":\"x\",\"label\":\"hi\"}", Tagged);
io.println(t.kind);
io.println(t.label);
`, "x-decoded\nhi\n")
}

// TestParityJSONParseAsInheritedDeserializeSameFile: a subclass uses a same-file base's inherited static __deserialize__ on both backends.
func TestParityJSONParseAsInheritedDeserializeSameFile(t *testing.T) {
	runParity(t, `import io;
import json;
class Base {
    int value;
    func Base(int value) { this.value = value; }
    static func __deserialize__(dict d): Base { return Base(d["value"] * 10); }
    func show(): int { return this.value; }
}
class Sub extends Base {
    func Sub(int value) { parent(value); }
}
io.println(json.parseAs("{\"value\": 4}", Sub).show());
`, "40\n")
}

// TestParityJSONParseAsCanonicalDeserialize: the single-underscore canonical __deserialize name is honored identically on both backends.
func TestParityJSONParseAsCanonicalDeserialize(t *testing.T) {
	runParity(t, `import io;
import json;
class Point {
    int x;
    func Point(int x) { this.x = x; }
    static func __deserialize(dict d): Point { return Point(d["x"] + 100); }
}
io.println(json.parseAs("{\"x\": 5}", Point).x);
`, "105\n")
}

// TestParityJSONParseAsOverrideDeserialize: an overriding subclass uses its own __deserialize__, not the inherited one, on both backends.
func TestParityJSONParseAsOverrideDeserialize(t *testing.T) {
	runParity(t, `import io;
import json;
class Base {
    int value;
    func Base(int value) { this.value = value; }
    static func __deserialize__(dict d): Base { return Base(d["value"] * 10); }
    func show(): int { return this.value; }
}
class Sub extends Base {
    func Sub(int value) { parent(value); }
    static func __deserialize__(dict d): Sub { return Sub(d["value"] + 1); }
}
io.println(json.parseAs("{\"value\": 4}", Sub).show());
`, "5\n")
}

// TestParityJSONParseAsInheritedDeserializeCrossModule: a subclass inheriting a static __deserialize__ from a cross-module parent dispatches to it on both backends.
func TestParityJSONParseAsInheritedDeserializeCrossModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Base {
    int value;
    func Base(int value) { this.value = value; }
    static func __deserialize__(dict d): Base { return Base(d["value"] * 10); }
    func show(): int { return this.value; }
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	runParityModulesDir(t, dir, `import io;
import json;
import base;
class Sub extends base.Base {
    func Sub(int value) { parent(value); }
}
io.println(json.parseAs("{\"value\": 4}", Sub).show());
`, "40\n")
}

// TestParityJSONParseAsNoInheritedDeserializeCrossModule: a subclass whose cross-module parent has no factory hydrates via its own constructor on both backends.
func TestParityJSONParseAsNoInheritedDeserializeCrossModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"), []byte(`module base;
export class Base {
    int value;
    func Base(int value) { this.value = value; }
    func show(): int { return this.value; }
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	runParityModulesDir(t, dir, `import io;
import json;
import base;
class Sub extends base.Base {
    func Sub(int value) { parent(value); }
}
io.println(json.parseAs("{\"value\": 7}", Sub).show());
`, "7\n")
}

// TestParityHofCaughtCallerLine: a caught structured trace reports the HOF call-site line on the caller frame, identically on both backends.
func TestParityHofCaughtCallerLine(t *testing.T) {
	runParity(t, `import io;
func throwingKey(int x): int {
    if (x == 1) { throw RuntimeError("key boom"); }
    return x;
}
func run(): void {
    try {
        let s = [3, 1, 2].sortBy(throwingKey);
    } catch (RuntimeError e) {
        let frames = e.stackTrace().frames();
        for (f in frames) {
            io.println("${f.function()}:${f.line()}");
        }
    }
}
run();
`, "throwingKey:3\nrun:8\n<top level>:16\n")
}

// TestParityHofCrossModuleCallerLine: a HOF (sortBy) inside a foreign module invokes that module's own callback which throws; the caught trace attributes the HOF call-site line to the module function's caller frame identically on both backends.
func TestParityHofCrossModuleCallerLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sortdonor.gb"), []byte(`module sortdonor;

func donorKey(int x): int {
    if (x == 1) { throw RuntimeError("donor key boom"); }
    return x;
}

export func doSort(list<int> xs): list<int> {
    return xs.sortBy(donorKey);
}
`), 0o644); err != nil {
		t.Fatalf("write sortdonor: %v", err)
	}
	runParityModulesDir(t, dir, `import io;
import sortdonor as s;

func caller(): void {
    try {
        let r = s.doSort([3, 1, 2]);
    } catch (RuntimeError e) {
        let frames = e.stackTrace().frames();
        for (f in frames) {
            io.println("${f.function()}:${f.line()}");
        }
    }
}
caller();
`, "donorKey:4\ndoSort:9\ncaller:6\n<top level>:14\n")
}

// TestParityJSONRoundTrip verifies stringify followed by parseAs
// reconstructs structurally-equal instances on both backends.
func TestParityJSONRoundTrip(t *testing.T) {
	runParity(t, `import io;
import json;
class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}
let p = Point(10, 20);
let q = json.parseAs(json.stringify(p), Point);
io.println(q.x);
io.println(q.y);
io.println(p.x == q.x);
io.println(p.y == q.y);
`, "10\n20\ntrue\ntrue\n")
}

// TestParityJSONParseAsNested pins recursive deserialization: a field
// whose declared type is a user class (or a list / dict of one) is
// reconstructed into an instance on both backends, while any / primitive
// fields stay raw.
func TestParityJSONParseAsNested(t *testing.T) {
	runParity(t, `import io;
import json;
class Inner { string label; }
class Item { int qty; }
class Outer {
    string name;
    Inner inner;
    list<Item> items;
    dict<string, Inner> tags;
    dict<string, any> extra;
}
let o = json.parseAs("{\"name\":\"n\",\"inner\":{\"label\":\"L\"},\"items\":[{\"qty\":2},{\"qty\":5}],\"tags\":{\"a\":{\"label\":\"T\"}},\"extra\":{\"raw\":1}}", Outer);
io.println(typeof(o.inner));
io.println(o.inner.label);
io.println(typeof(o.items[0]));
io.println(o.items[1].qty);
io.println(typeof(o.tags["a"]));
io.println(o.tags["a"].label);
io.println(typeof(o.extra));
`, "Inner\nL\nItem\n5\nInner\nT\ndict\n")
}

// TestParityWithStdinAndChoose pins io.withStdin feeding an interactive
// cli prompt: both backends consume the injected input identically. Uses
// the stateful harness since io / cli route through the native bridge.
func TestParityWithStdinAndChoose(t *testing.T) {
	runParityStateful(t, `import io;
import cli;
io.withStdin("2\n", func(): void {
    io.println("picked " + cli.choose("Pick:", ["a", "b", "c"]));
});
`, "picked b\n")
}

func TestParityMathStats(t *testing.T) {
	runParity(t, `import io;
import math;

let xs = [0, 10, 20, 30, 40];
io.println(math.median(xs));
io.println(math.percentile(xs, 25));
io.println(math.percentile(xs, 75));
io.println(math.mode([1, 1, 2, 2, 3]));
`, "20\n10\n30\n1\n")
}

// TestParityCryptS3WalkthroughCertAndJwe walks the three S3
// follow-up surfaces (signCertificate, jweEncrypt+Decrypt for
// dir, jweEncrypt+Decrypt for RSA-OAEP-256) on both backends so
// any future divergence in the native dispatch shape is caught.
func TestParityCryptS3WalkthroughCertAndJwe(t *testing.T) {
	runParity(t, `import io;
import crypt;
import bytes;

let caKey = crypt.generateEcKey("P-256");
let caBundle = crypt.generateSelfSignedCert({
    "subject": {"commonName": "ParityCA"},
    "key": caKey
});
let leafKey = crypt.generateEcKey("P-256");
let csr = crypt.generateCsr({
    "key": leafKey,
    "subject": {"commonName": "parity.leaf"}
});
let signed = crypt.signCertificate({
    "csr": csr,
    "caCert": caBundle["cert"],
    "caKey": caKey
});
let parsed = crypt.parseCert(signed);
io.println(parsed["issuer"]["commonName"] as string);
io.println(parsed["subject"]["commonName"] as string);

let cek = bytes.fromHex(crypt.randomHex(32));
let dirTok = crypt.jweEncrypt("dir-payload", cek, {"alg": "dir", "enc": "A256GCM"});
io.println(dirTok.split(".").length as string);
io.println(bytes.toString(crypt.jweDecrypt(dirTok, cek)));

let rsaKey = crypt.generateRsaKey(2048);
let rsaPub = crypt.publicKey(rsaKey);
let rsaTok = crypt.jweEncrypt("rsa-payload", rsaPub, {"alg": "RSA-OAEP-256", "enc": "A256GCM"});
io.println(bytes.toString(crypt.jweDecrypt(rsaTok, rsaKey)));
`, "ParityCA\nparity.leaf\n5\ndir-payload\nrsa-payload\n")
}

// TestParityJwtUnifiedSurface exercises the alg-dispatching
// crypt.jwtSign / crypt.jwtVerify pair across HMAC and asymmetric
// algorithms; the assertion that both backends produce a token
// that the other can verify proves the dispatch and key handling
// line up. The allowedAlgs guard against alg-confusion is also
// hit so a divergence in the dispatch table is caught.
func TestParityJwtUnifiedSurface(t *testing.T) {
	runParity(t, `import crypt;
import io;

let hs = crypt.jwtSign({"u": "ada"}, "shh", {"alg": "HS512"});
let claims = crypt.jwtVerify(hs, "shh");
io.println(claims["u"] as string);

let priv = crypt.generateEcKey("P-256");
let pub = crypt.publicKey(priv);
let es = crypt.jwtSign({"u": "ec"}, priv, {"alg": "ES256"});
io.println(crypt.jwtVerify(es, pub)["u"] as string);

let blocked = crypt.jwtVerify(hs, "shh", {"allowedAlgs": ["RS256"]});
if (blocked == null) {
    io.println("blocked");
} else {
    io.println("leaked");
}

let unsigned = crypt.jwtSign({"u": "n"}, "", {"alg": "none", "allowedAlgs": ["none"]});
let defaultVerify = crypt.jwtVerify(unsigned, "");
if (defaultVerify == null) {
    io.println("none-default-blocked");
} else {
    io.println("none-default-leaked");
}
let optedIn = crypt.jwtVerify(unsigned, "", {"allowedAlgs": ["none"]});
io.println(optedIn["u"] as string);
`, "ada\nec\nblocked\nnone-default-blocked\nn\n")
}

// TestParityHmacSha256Bytes verifies the raw-bytes HMAC variant
// produces the AWS sigv4 reference kDate value when fed the
// documented test inputs (AWS docs sample for
// "AWS4SECRET" + "20150830" -> kDate is a known hex digest).
// This guards against accidental signature drift between the
// evaluator and the VM and confirms the Bytes-typed return.
func TestParityHmacSha256Bytes(t *testing.T) {
	// Reference: AWS Signature V4 "Examples of How to Derive a
	// Signing Key" - kDate when secretAccessKey is
	// "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY" and dateStamp is
	// "20150830". Expected kDate hex:
	//   0138c7a6cbd60aa727b2f653a522567439dfb9f3e72b21f9b25941a42f04a7cd
	runParity(t, `import crypt;
import bytes;
import io;
let kDate = crypt.hmacSha256Bytes("AWS4wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830");
io.println(bytes.toHex(kDate));
`, "0138c7a6cbd60aa727b2f653a522567439dfb9f3e72b21f9b25941a42f04a7cd\n")
}

// Grapheme cluster methods segment by user-perceived character (UAX #29):
// an emoji ZWJ sequence and a base+combining-mark each count as one, while
// length()/codePoints() stay code-point based. Identical on both backends.
func TestParityGraphemes(t *testing.T) {
	runParity(t, `import io;
let family = "\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}";
io.println(family.length());
io.println(family.graphemeLength());
io.println("e\u{301}llo".graphemeLength());
io.println("abcd".truncateGraphemes(2));
io.println("xy".graphemes());
`, "5\n1\n4\nab\n[\"x\", \"y\"]\n")
}

// time.humanize renders compact duration strings identically on both backends.
func TestParityTimeHumanize(t *testing.T) {
	runParity(t, `import io;
import time;
io.println(time.humanize(45));
io.println(time.humanize(999));
io.println(time.humanize(1000));
io.println(time.humanize(1500));
io.println(time.humanize(1230));
io.println(time.humanize(45000));
io.println(time.humanize(59999));
io.println(time.humanize(184000));
io.println(time.humanize(7501000));
io.println(time.humanize(90061000));
io.println(time.humanize(-1500));
`, "45ms\n999ms\n1s\n1.5s\n1.2s\n45s\n1m\n3m 4s\n2h 5m\n1d 1h\n-1.5s\n")
}

func TestParityTimeMonotonicNs(t *testing.T) {
	runParity(t, `import time;
import io;
let a = time.monotonicNs();
let b = time.monotonicNs();
io.println(a >= 0);
io.println(b >= a);
io.println(typeof(a) == int);
`, "true\ntrue\ntrue\n")
}

// Datetime value-method ergonomics: unix accessors, part accessors, ISO
// weekday, comparisons, duration arithmetic, friendly format/parse layouts,
// and zone offset - identical on both backends.
func TestParityDateTimeErgonomics(t *testing.T) {
	runParity(t, `import io;
import datetime;
let a = datetime.Instant(1700000000);
let b = datetime.Instant(1700000100);
io.println(a.year());
io.println(a.month());
io.println(a.day());
io.println(a.weekday());
io.println(a.dayOfYear());
io.println(a.isWeekend());
io.println(a.toUnixMillis());
io.println(a.isBefore(b));
io.println(a.equals(a));
io.println(a.diff(b).inSeconds());
io.println(a.sub(datetime.Duration(100)).toUnix());
io.println(datetime.Duration(-90).abs().negate().seconds());
io.println(datetime.Duration(60).add(datetime.Duration(30)).inMillis());
io.println(a.format("%Y-%m-%d"));
io.println(a.format("datetime"));
io.println(a.formatHTTP());
io.println(datetime.parse("2023-11-14", "%Y-%m-%d"));
io.println(datetime.Zone("UTC").offsetAt(a));
`, "2023\n11\n14\n2\n318\nfalse\n1700000000000\ntrue\ntrue\n100\n1699999900\n-90\n90000\n2023-11-14\n2023-11-14 22:13:20\nTue, 14 Nov 2023 22:13:20 GMT\n1699920000\n0\n")
}

func TestParitySeqStream(t *testing.T) {
	runParityWithStdlib(t, `
import seq;
import io;
io.println(seq.stream([1,2,3,4,5,6]).filter(func(any n): bool { return (n as int) % 2 == 0; }).map(func(any n): any { return (n as int)*(n as int); }).sum());
io.println(seq.stream([1,2,3,4,5,6]).drop(1).take(3).toList());
io.println(seq.stream([3,1,3,2,1]).distinct().toList());
io.println(seq.stream([3,1,2]).sorted().toList());
io.println(seq.stream([1,2,3]).map(func(any n): any { return "${n}"; }).join("-"));
io.println(seq.stream(1..1000000).map(func(any n): any { return (n as int)*2; }).first());
io.println(seq.stream([2,4,6]).all(func(any n): bool { return (n as int)%2==0; }));
io.println(seq.stream([3,1,2]).min());
`, "56\n[2, 3, 4]\n[3, 1, 2]\n[1, 2, 3]\n1-2-3\n2\ntrue\n1\n")
}

// vecmath: float32 similarity score + batched top-k over list and blob vectors.
func TestParityVecmath(t *testing.T) {
	runParity(t, `import io;
import vecmath;
import binary;
io.println(vecmath.score("cosine", [1.0, 0.0], [1.0, 0.0]));
io.println(vecmath.score("cosine", [1.0, 0.0], [0.0, 1.0]));
io.println(vecmath.score("dot", [1.0, 2.0], [3.0, 4.0]));
io.println(vecmath.score("euclidean", [0.0, 0.0], [3.0, 4.0]));
let vs = [binary.pack("<2f", 1.0f, 0.0f), binary.pack("<2f", 0.0f, 1.0f), binary.pack("<2f", 0.9f, 0.1f)];
let r = vecmath.topK(vs, [1.0, 0.0], 2, "cosine");
io.println(r.length());
io.println(r[0]["index"]);
io.println(r[1]["index"]);
let all = vecmath.topK([[1.0,0.0],[0.0,1.0]], [1.0,0.0], 5, "cosine");
io.println(all.length());
io.println(all[0]["index"]);
`, "1\n0\n11\n-5\n2\n0\n2\n2\n0\n")
}

// math.lerp / math.remap: exact decimal for int/decimal, float for float.
func TestParityMathInterpolation(t *testing.T) {
	runParity(t, `import io;
import math;
io.println(math.lerp(0, 10, 0.5));
io.println(math.lerp(10, 20, 0.25));
io.println(math.remap(450000, 400000, 500000, 11500, 10000));
io.println(math.remap(1, 0, 3, 0, 1));
io.println(math.lerp(0.0f, 1.0f, 0.5f));
io.println(math.remap(5.0f, 0.0f, 10.0f, 0.0f, 100.0f));
`, "5.0000000000\n12.5000000000\n10750.0000000000\n0.3333333333\n0.5\n50\n")
}
