package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"net"
	"path/filepath"
	"strconv"
	"testing"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityDatabaseStandardBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parity.sqlite")
	runParityStateful(t, `import io;
import db;

let conn = db.Connection({
    "driver": "sqlite",
    "path": `+strconv.Quote(path)+`
});
defer conn.close();

conn.exec("drop table if exists users");
conn.exec("create table users (id integer primary key, name text, email text)");
conn.exec(
    "insert into users (name, email) values (:name, :email)",
    {"name": "Ada", "email": "ada@example.com"}
);
conn.exec(
    "insert into users (name, email) values (?, ?)",
    ["Grace", "grace@example.com"]
);

let rows = conn.query("select email from users where name = :name", {"name": "Ada"});
defer rows.close();
io.println(rows.first()["email"]);

let stmt = conn.prepare("select name from users where email = :email");
let prepared = stmt.query({"email": "grace@example.com"});
defer prepared.close();
io.println(prepared.first()["name"]);
stmt.close();
`, "ada@example.com\nGrace\n")
}

func TestParityLogInterface(t *testing.T) {
	runParityStateful(t, `import io;
import log;

class Capture implements log.LogInterface {
    string last = "";

    func handle(string level, string message, dict<string, any> fields): void {
        this.last = level + ":" + message + ":" + fields["id"];
    }
}

let capture = Capture();
let logger = log.custom(capture);
log.error(logger, "custom", {"id": "3"});
io.println(capture.last);
io.println(capture instanceof log.LogInterface);
`, "error:custom:3\ntrue\n")
}

func TestParityLogSyslogUDP(t *testing.T) {
	runParityStateful(t, `import io;
import net;
import log;
import json;

let srv = net.listenUdp("127.0.0.1:0");
net.setDeadline(srv, 5000);
let logger = log.syslog({
    "network": "udp",
    "address": net.localAddr(srv),
    "app": "paritytest",
    "hostname": "testhost",
    "facility": "local0"
});
log.info(logger, "hello", {"k": "v"});
let pkt = net.readFrom(srv, 4096);
log.close(logger);
net.close(srv);

let frame = (pkt["data"] as bytes).toString();
let body = frame.slice(frame.indexOf("{"), frame.length());
let parts = frame.slice(0, frame.indexOf("{")).trim().split(" ");
let doc = json.parse(body);
io.println(parts[0]);
io.println(parts[2]);
io.println(parts[3]);
io.println(doc["level"] as string);
io.println(doc["message"] as string);
`, "<134>1\ntesthost\nparitytest\ninfo\nhello\n")
}

// A file database gets the WAL journal default identically on both backends.
func TestParitySqliteWalDefault(t *testing.T) {
	runParityStatefulWithFile(t, `import db;
import io;
let c = db.Connection("sqlite", "TMPFILE");
io.println(c.query("PRAGMA journal_mode").toList()[0]["journal_mode"]);
`, "", "wal\n")
}

func TestParityFileReadClose(t *testing.T) {
	runParityStatefulWithFile(t, `import io;
let h = io.open("TMPFILE", "r");
let line = io.readLine(h);
io.close(h);
io.println(line);
`, "hello file\n", "hello file\n")
}

func TestParityFileReadAllLines(t *testing.T) {
	runParityStatefulWithFile(t, `import io;
let h = io.open("TMPFILE", "r");
let lines = io.readLines(h);
io.close(h);
io.println(lines.length());
io.println(lines[0]);
io.println(lines[1]);
`, "alpha\nbeta\n", "2\nalpha\nbeta\n")
}

func TestParityFileWriteAndRead(t *testing.T) {
	runParityStatefulWithFile(t, `import io;
let path = "TMPFILE";
let w = io.open(path, "w");
io.write(w, "written line");
io.close(w);
let r = io.open(path, "r");
let content = io.readLine(r);
io.close(r);
io.println(content);
`, "", "written line\n")
}

func TestParityIOStreamsAndCapture(t *testing.T) {
	runParityStateful(t, `import io;
let mem = io.memory("seed");
io.write(mem, "-text");
io.println(io.toString(mem));
let captured = io.captureStdout();
io.println("hidden");
let text = io.toString(captured);
io.close(captured);
io.print(text);
let redirected = io.memory();
let restore = io.redirectStdout(redirected);
io.println("redirected");
restore();
io.print(io.toString(redirected));
`, "seed-text\nhidden\nredirected\n")
}

func TestParitySchemaValidate(t *testing.T) {
	runParityStateful(t, `import io;
import schema;
let s = {"type": "string"};
let r1 = schema.validate("hello", s);
io.println(r1["valid"]);
let r2 = schema.validate(42, s);
io.println(r2["valid"]);
io.println(r2["errors"].length() > 0);
`, "true\nfalse\ntrue\n")
}

func TestParitySerdeRoundtrip(t *testing.T) {
	runParityStateful(t, `import io;
import serde;
let data = {"name": "bob", "score": 99};
let json_str = serde.stringify("json", data);
io.println(json_str.contains("bob"));
let parsed = serde.parse("json", json_str);
io.println(parsed["name"]);
io.println(parsed["score"]);
`, "true\nbob\n99\n")
}

func TestParityDotenvParse(t *testing.T) {
	runParityStateful(t, `import io;
import dotenv;
let text = "KEY=value\nNAME=alice\nEMPTY=\n# comment\nQUOTED=\"hello world\"\nSINGLE='raw value'\n";
let env = dotenv.parse(text);
io.println(env["KEY"]);
io.println(env["NAME"]);
io.println(env["EMPTY"]);
io.println(env["QUOTED"]);
io.println(env["SINGLE"]);
`, "value\nalice\n\nhello world\nraw value\n")
}

func TestParityDotenvParseExport(t *testing.T) {
	runParityStateful(t, `import io;
import dotenv;
let env = dotenv.parse("export FOO=bar\nexport BAZ=qux\n");
io.println(env["FOO"]);
io.println(env["BAZ"]);
`, "bar\nqux\n")
}

func TestParityDotenvParseInlineComment(t *testing.T) {
	runParityStateful(t, `import io;
import dotenv;
let env = dotenv.parse("KEY=value # this is a comment\n");
io.println(env["KEY"]);
`, "value\n")
}

func TestParityNativeBindConflictIsCatchableIOError(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	_ = probe.Close()

	runParityStateful(t, `import io;
import net;

let listener = net.listenTcp("127.0.0.1:0");
let addr = net.localAddr(listener);
try {
    let other = net.listenTcp(addr);
    net.close(other);
    io.println("not caught");
} catch (IOError e) {
    io.println(e.name);
} finally {
    net.close(listener);
}
`, "IOError\n")
}

func TestParityProcessModule(t *testing.T) {
	runParityStateful(t, `
import process;
import io;
let r = process.run("printf", ["hello"]);
io.println(r.isOk());
io.println(r.code());
io.println(r.stdout());
io.println(r.timedOut());
`, "true\n0\nhello\nfalse\n")
}

func TestParityProcessStart(t *testing.T) {
	runParityStateful(t, `
import process;
import io;
let proc = process.start("cat", []);
proc.write("piped\n");
proc.closeStdin();
io.print(proc.readStdout());
io.println(proc.wait());
`, "piped\n0\n")
}

func TestParityHTTPClientConstruct(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let c = http.newClient();
let b = http.build("https://example.com");
let j = http.newCookieJar();
io.println(typeof(c));
io.println(typeof(b));
io.println(typeof(j));
`, "Client\nBuilder\nCookieJar\n")
}

func TestParityHTTPClientConfig(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let c = http.newClient({"baseUrl": "https://example.com", "timeoutMs": 5000});
let j = c.cookieJar();
io.println(typeof(c));
io.println(typeof(j));
let j2 = http.newCookieJar();
c.attachCookieJar(j2);
io.println(typeof(c));
`, "Client\nCookieJar\nClient\n")
}

// TestParityHTTPClientNewOptions verifies http.newClient accepts the
// cookieJar (instance and auto), keepAlive, maxIdleConns, proxy, and
// proxyFromEnv options on both backends.
// TestParityHTTPServerTLS exercises the HTTPS surface end to end on both
// backends: a self-signed server, a client trusting it via caCerts, an
// insecure client, and the default client rejecting the untrusted cert.
func TestParityHTTPServerTLS(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "secure"};
}, {"tls": {"selfSigned": true}});
let url = "https://" + http.serverAddr(server) + "/";
let cert = http.serverCert(server);
io.println(cert != null);
io.println(http.newClient({"tls": {"caCerts": cert}}).get(url)["body"] as string);
io.println(http.newClient({"tls": {"verify": false}}).get(url)["body"] as string);
try { http.newClient({}).get(url); io.println("strict-ok"); } catch (Error e) { io.println("strict-rejected"); }
http.close(server);
`, "true\nsecure\nsecure\nstrict-rejected\n")
}

// TestParityHTTPResponseObject verifies client calls return a rich
// Response object (reader methods + status predicates) that is also
// index-compatible with the legacy dict shape via __index.
func TestParityHTTPResponseObject(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 404, "body": "{\"ok\": false}", "headers": {"X-Test": "yes"}};
}, {});
let url = "http://" + http.serverAddr(server) + "/";
let r = http.get(url);
io.println(typeof(r));
io.println(r.status());
io.println(r.ok());
io.println(r.isNotFound());
io.println(r.isClientError());
io.println(r.text());
io.println(r.json()["ok"]);
io.println(r.header("x-test"));
io.println(r["status"]);
io.println(r["body"]);
http.close(server);
`, "Response\n404\nfalse\ntrue\ntrue\n{\"ok\": false}\nfalse\nyes\n404\n{\"ok\": false}\n")
}

// TestParityHTTPResponseURL checks Response.url() reports the final post-redirect address on both backends.
func TestParityHTTPResponseURL(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    if (req["path"] == "/start") {
        return {"status": 302, "body": "", "headers": {"Location": "/end"}};
    }
    return {"status": 200, "body": "arrived", "headers": {}};
}, {});
let base = "http://" + http.serverAddr(server);
let r = http.get(base + "/start");
io.println(r.status());
io.println(r.text());
io.println(r.url() == base + "/end");
io.println(r["url"] == base + "/end");
io.println(http.response("hi", 200).url() == "");
http.close(server);
`, "200\narrived\ntrue\ntrue\ntrue\n")
}

// TestParityHTTPRequestBuilder exercises the immutable withX request
// builder (http.request(url) one-arg form) on both backends, including
// that withX returns a fresh builder (sibling requests don't leak).
func TestParityHTTPRequestBuilder(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    let tag = "-";
    if ("X-Tag" in req["headers"]) { tag = req["headers"]["X-Tag"] as string; }
    let auth = "-";
    if ("Authorization" in req["headers"]) { auth = req["headers"]["Authorization"] as string; }
    return {"status": 200, "body": (req["method"] as string) + " " + tag + " " + auth + " " + (req["body"] as string), "headers": {}};
}, {});
let url = "http://" + http.serverAddr(server) + "/";
let base = http.request(url).withMethod("POST").withJson({"k": 1});
let a = base.withHeader("X-Tag", "AAA").withBearer("t");
let b = base.withHeader("X-Tag", "BBB");
io.println(a.send().text());
io.println(b.send().text());
http.close(server);
`, "POST AAA Bearer t {\"k\":1}\nPOST BBB - {\"k\":1}\n")
}

// TestParityHTTPGetAllAndBatch verifies http.getAll(urls) and fetchAll
// accepting request Builders, with a concurrency limit, on both backends.
func TestParityHTTPGetAllAndBatch(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import async;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "ok:" + (req["path"] as string), "headers": {}};
}, {});
let base = "http://" + http.serverAddr(server);
let rs = await http.getAll([base + "/a", base + "/b"], {"limit": 1});
io.println(rs.length());
io.println(rs[0].text());
io.println(rs[1].text());
let rs2 = await http.fetchAll([
    http.request(base + "/x"),
    http.request(base + "/y").withMethod("GET")
]);
io.println(rs2[0].text() + "," + rs2[1].text());
http.close(server);
`, "2\nok:/a\nok:/b\nok:/x,ok:/y\n")
}

// TestParityHTTPBatchErrorResponse verifies a transport failure in a batch
// is reported as a Response with isError() rather than throwing.
func TestParityHTTPBatchErrorResponse(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import async;
let results = await http.getAll(["http://127.0.0.1:1/nope"]);
let r = results[0];
io.println(r instanceof Response);
io.println(r.isError());
io.println(r.status());
io.println(r.ok());
io.println(r.error() != null);
`, "true\ntrue\n0\nfalse\ntrue\n")
}

// TestParityHTTPServerRequestObject exercises the rich server Request
// object (opt-in via a func(Request) handler) including proxy-aware
// scheme/host/clientIp when behind a trusted proxy. This also guards the
// callback bridge that carries parameter types across the stateful-native
// boundary so the object handler works on both backends.
func TestParityHTTPServerRequestObject(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({
        "isPost": req.isMethod("POST"),
        "scheme": req.scheme(),
        "secure": req.isSecure(),
        "host": req.host(),
        "clientIp": req.clientIp(),
        "isJson": req.isJson(),
        "page": req.queryInt("page"),
        "debug": req.queryBool("debug"),
        "tag0": req.queryAll("tag")[0],
        "tags": req.queryAll("tag").length(),
        "cookie": req.cookie("sid")
    });
}, {"trustedProxies": ["127.0.0.1"]});
let url = "http://" + http.serverAddr(server) + "/s?page=3&debug=yes&tag=a&tag=b";
let body = http.request(url)
    .withHeader("X-Forwarded-For", "203.0.113.7")
    .withHeader("X-Forwarded-Proto", "https")
    .withHeader("X-Forwarded-Host", "example.com")
    .withHeader("Cookie", "sid=xyz")
    .send().json();
io.println(body["clientIp"]);
io.println(body["scheme"]);
io.println(body["secure"]);
io.println(body["host"]);
io.println(body["page"]);
io.println(body["debug"]);
io.println(body["tag0"]);
io.println(body["tags"]);
io.println(body["cookie"]);
io.println(body["isPost"]);
http.close(server);
`, "203.0.113.7\nhttps\ntrue\nexample.com\n3\ntrue\na\n2\nxyz\nfalse\n")
}

// TestParityHTTPServerUntrustedProxy verifies forwarded headers are ignored
// when the peer is not a trusted proxy (anti-spoofing), and the redirect
// builder, on both backends.
func TestParityHTTPServerUntrustedProxy(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"clientIp": req.clientIp(), "scheme": req.scheme()});
}, {});
let url = "http://" + http.serverAddr(server) + "/";
let body = http.request(url).withHeader("X-Forwarded-For", "203.0.113.7").withHeader("X-Forwarded-Proto", "https").send().json();
io.println(body["clientIp"]);
io.println(body["scheme"]);
http.close(server);
let rd = http.redirect("/login", 301);
io.println(rd.status());
io.println(rd.header("Location"));
io.println(rd.isRedirect());
`, "127.0.0.1\nhttp\n301\n/login\ntrue\n")
}

// TestParityHTTPServerDictRequestMeta verifies the dict-form server request
// (func(dict) handler) carries proxy-aware scheme/host/clientIp, honouring the
// trustedProxies list, on both backends.
func TestParityHTTPServerDictRequestMeta(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import json;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "headers": {"Content-Type": "application/json"}, "body": json.stringify({
        "scheme": req["scheme"],
        "host": req["host"],
        "clientIp": req["clientIp"]
    })};
}, {"trustedProxies": ["127.0.0.1"]});
let url = "http://" + http.serverAddr(server) + "/";
let body = http.request(url)
    .withHeader("X-Forwarded-For", "203.0.113.7")
    .withHeader("X-Forwarded-Proto", "https")
    .withHeader("X-Forwarded-Host", "example.com")
    .send().json();
io.println(body["scheme"]);
io.println(body["host"]);
io.println(body["clientIp"]);
http.close(server);
`, "https\nexample.com\n203.0.113.7\n")
}

// TestParityHTTPMutualTLS exercises server-side client-certificate
// verification (tls.clientCa + clientAuth) and req.clientCert() on both
// backends: a presented cert is verified and surfaced; a request without a
// cert is rejected under "require".
func TestParityHTTPMutualTLS(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import crypt;
let caKey = crypt.generateEcKey("P-256");
let caBundle = crypt.generateSelfSignedCert({"subject": {"commonName": "GeblangCA"}, "key": caKey});
let clientKey = crypt.generateEcKey("P-256");
let csr = crypt.generateCsr({"key": clientKey, "subject": {"commonName": "svc-a"}});
let clientCert = crypt.signCertificate({"csr": csr, "caCert": caBundle["cert"], "caKey": caKey});
let server = http.listen("127.0.0.1:0", func(Request req): Response {
    let c = req.clientCert();
    if (c == null) { return http.jsonResponse({"who": "anon"}); }
    return http.jsonResponse({"who": c["subject"]});
}, {"tls": {"selfSigned": true, "clientCa": caBundle["cert"], "clientAuth": "require"}});
let url = "https://" + http.serverAddr(server) + "/";
let mtls = http.newClient({"tls": {"verify": false, "clientCert": clientCert, "clientKey": clientKey}});
io.println(mtls.get(url).json()["who"]);
try { http.newClient({"tls": {"verify": false}}).get(url); io.println("accepted"); } catch (Error e) { io.println("rejected"); }
http.close(server);
`, "CN=svc-a\nrejected\n")
}

// TestParityHTTPResponseBuilder verifies the body-first http.response builder
// (body, status, headers) and the keyed single-dict form, on both backends.
func TestParityHTTPResponseBuilder(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let a = http.response("hi", 201, {"X-T": "y"});
io.println(a.status());
io.println(a.text());
io.println(a.header("X-T"));
let b = http.response("plain");
io.println(b.status());
let c = http.response({"status": 404, "body": "nope"});
io.println(c.status());
io.println(c.isNotFound());
let body = http.response("hello", 200).bytes() as bytes;
io.println(body.length);
`, "201\nhi\ny\n200\n404\ntrue\n5\n")
}

// TestParityHTTPAutoCertConfig is an acceptance test for the ACME autocert
// option: a server configured with tls.autoCert starts (and rejects mixing
// autoCert with cert/key/selfSigned) on both backends. The live ACME path
// needs a public host and is verified manually, not in CI.
func TestParityHTTPAutoCertConfig(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "ok"};
}, {"tls": {"autoCert": ["example.com", "www.example.com"], "autoCertCacheDir": "/tmp/geblang-acme-parity", "autoCertEmail": "ops@example.com"}});
io.println(server > 0);
http.close(server);
try {
    http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> { return {"status": 200, "body": ""}; }, {"tls": {"autoCert": "x.com", "selfSigned": true}});
    io.println("accepted");
} catch (Error e) {
    io.println("rejected");
}
`, "true\nrejected\n")
}

func TestParityHTTPClientNewOptions(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let jar = http.newCookieJar();
let withJar = http.newClient({"cookieJar": jar, "keepAlive": true, "maxIdleConns": 8});
io.println(typeof(withJar));
let autoJar = http.newClient({"cookieJar": true});
io.println(typeof(autoJar));
let proxied = http.newClient({"proxy": "http://proxy.example.com:3128", "timeoutMs": 1000});
io.println(typeof(proxied));
let envProxied = http.newClient({"proxyFromEnv": true});
io.println(typeof(envProxied));
`, "Client\nClient\nClient\nClient\n")
}

// TestParityHTTPCookieJarInspect verifies the CookieJar's cookies(url) /
// setCookies(url, list) / clear() round trip on both backends.
func TestParityHTTPCookieJarInspect(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let jar = http.newCookieJar();
jar.setCookies("https://example.com/", [
    {"name": "sid", "value": "abc", "path": "/"},
    {"name": "csrf", "value": "xyz", "secure": true}
]);
let cookies = jar.cookies("https://example.com/");
io.println(cookies.length);
io.println(cookies[0]["name"] + "=" + cookies[0]["value"]);
io.println(cookies[1]["name"] + "=" + cookies[1]["value"]);
jar.clear();
io.println(jar.cookies("https://example.com/").length);
`, "2\nsid=abc\ncsrf=xyz\n0\n")
}

func TestParityHTTPBuilderChain(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let b = http.build("https://example.com")
    .method("POST")
    .header("Content-Type", "application/json")
    .body("{}")
    .timeout(1000);
io.println(typeof(b));
`, "Builder\n")
}

func TestParityHTTPFetchStreamEmpty(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
let stream = http.fetchStream([]);
io.println(typeof(stream));
io.println(stream.done());
io.println(stream.remaining());
io.println(stream.next());
`, "FetchStream\ntrue\n0\nnull\n")
}

func TestParityHTTPFetchAllEmpty(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import async;
let results = await http.fetchAll([]);
io.println(results.length());
`, "0\n")
}

func TestParityHTTPClientBatchMethods(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
import async;
let c = http.newClient({"timeoutMs": 5000});
let results = await c.fetchAll([]);
io.println(results.length());
let stream = c.fetchStream([]);
io.println(typeof(stream));
io.println(stream.done());
io.println(stream.remaining());
`, "0\nFetchStream\ntrue\n0\n")
}

func TestParityNetIP(t *testing.T) {
	// parseIp + parseCidr core fields.
	runParityStateful(t, `import io;
import net;
let ip = net.parseIp("10.0.0.1");
io.println(ip["version"]);
io.println(ip["address"]);

let c = net.parseCidr("10.0.0.0/8");
io.println(c["network"]);
io.println(c["prefixLen"]);
io.println(c["version"]);
io.println(c["first"]);
io.println(c["last"]);
io.println(c["count"]);
`, "4\n10.0.0.1\n10.0.0.0\n8\n4\n10.0.0.0\n10.255.255.255\n16777216\n")

	// cidrContains positive + negative.
	runParityStateful(t, `import io;
import net;
io.println(net.cidrContains("10.0.0.0/8", "10.5.5.5"));
io.println(net.cidrContains("10.0.0.0/8", "11.0.0.0"));
io.println(net.cidrContains("192.168.0.0/16", "192.168.42.1"));
io.println(net.cidrContains("192.168.0.0/16", "10.0.0.1"));
`, "true\nfalse\ntrue\nfalse\n")

	// IPv6 CIDR + count overflows int64 -> Int.
	runParityStateful(t, `import io;
import net;
let c = net.parseCidr("2001:db8::/32");
io.println(c["version"]);
io.println(c["first"]);
io.println(c["count"]);
`, "6\n2001:db8::\n79228162514264337593543950336\n")

	// Classification helpers.
	runParityStateful(t, `import io;
import net;
io.println(net.isIpv4("192.168.1.1"));
io.println(net.isIpv4("::1"));
io.println(net.isIpv4("not-an-ip"));
io.println(net.isIpv6("::1"));
io.println(net.isIpv6("192.168.1.1"));
`, "true\nfalse\nfalse\ntrue\nfalse\n")

	// Bytes round trip.
	runParityStateful(t, `import io;
import net;
import bytes;
let b = net.ipToBytes("192.168.1.1");
io.println(b.length());
io.println(bytes.toHex(b));
io.println(net.ipFromBytes(b));
`, "4\nc0a80101\n192.168.1.1\n")
}

// TestParityAliasedNativeImport verifies the bytecode compiler now
// recognises aliased native imports - calls like `natpath.clean(...)`
// dispatch to the canonical `path.clean(...)` on both backends without
// the runtime fallback that previously papered over this gap.
// `path` is a stateful native module so this needs the stateful
// parity harness; that detail is incidental to the alias support.
func TestParityAliasedNativeImport(t *testing.T) {
	runParityStateful(t, `import io;
import path as natpath;
io.println(natpath.clean("/a/../b"));
`, "/b\n")
}

// TestParityAliasedNativeImportDeferred exercises the defer code path
// for an aliased native call (a separate compileDeferStatement branch
// from compileCallExpression).
func TestParityAliasedNativeImportDeferred(t *testing.T) {
	runParityStateful(t, `import io;
import path as natpath;
func work(): void {
    defer io.println(natpath.clean("/a/../b"));
    io.println("before defer fires");
}
work();
`, "before defer fires\n/b\n")
}

// TestParityHTTPDefaultUserAgent verifies outgoing requests carry the
// Geblang default User-Agent header (and not Go's default).
func TestParityHTTPDefaultUserAgent(t *testing.T) {
	runParityStateful(t, `
import http;
import io;
func handle(dict<string, any> req): dict<string, any> {
    let ua = req["headers"].get("User-Agent");
    return {"status": 200, "body": ua as string};
}
let server = http.listen("127.0.0.1:0", handle);
let base = "http://" + http.serverAddr(server);
io.println(http.get(base + "/")["body"]);
http.shutdown(server, 100);
http.close(server);
`, "Geblang/1.0\n")
}

// TestParityWebParseMultipart guards the new `web.parseMultipart` native:
// both backends parse a `multipart/form-data` body into a
// `{fields, files}` dict, where each file is `{filename, contentType,
// bytes}`. The native is stateful so the VM dispatches through the
// evaluator, but the parity test still documents the public shape.
func TestParityWebParseMultipart(t *testing.T) {
	runParityStateful(t, `import io;
import web;

let boundary = "----parity1";
let body = "------parity1\r\n" +
    "Content-Disposition: form-data; name=\"name\"\r\n\r\n" +
    "alice\r\n" +
    "------parity1\r\n" +
    "Content-Disposition: form-data; name=\"avatar\"; filename=\"a.png\"\r\n" +
    "Content-Type: image/png\r\n\r\n" +
    "PNG_BYTES\r\n" +
    "------parity1--\r\n";

let r = {
    "method": "POST",
    "path": "/u",
    "headers": {"Content-Type": "multipart/form-data; boundary=" + boundary},
    "body": body,
};

let parsed = web.parseMultipart(r) as dict<string, any>;
let fields = parsed["fields"] as dict<string, any>;
let files = parsed["files"] as dict<string, any>;
let avatar = files["avatar"] as dict<string, any>;

io.println(fields["name"]);
io.println(avatar["filename"]);
io.println(avatar["contentType"]);
io.println((avatar["bytes"] as bytes) as string);
`, "alice\na.png\nimage/png\nPNG_BYTES\n")
}

// Binds an int (SmallInt) param and a bytes BLOB, then reads both back.
func TestParityDBBindIntAndBlob(t *testing.T) {
	runParityStateful(t, `import db;
import binary;
import io;
let conn = db.connect("sqlite", ":memory:");
conn.exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, v BLOB)");
let blob = binary.pack("<3f", 1.0f, 2.0f, 3.0f);
conn.exec("INSERT INTO t (id, name, v) VALUES (?, ?, ?)", 7, "row", blob);
let rows = conn.query("SELECT id, name, v FROM t WHERE id = ?", 7).all();
io.println(rows.length());
io.println(rows[0]["id"]);
io.println(rows[0]["name"]);
io.println(typeof(rows[0]["v"]));
let back = binary.unpack("<3f", rows[0]["v"] as bytes);
io.println("${back[0]}");
io.println("${back[2]}");
`, "1\n7\nrow\nbytes\n1\n3\n")
}

// TestParityDBMemoryConcurrentInsert pins that concurrent async writes to an
// in-memory sqlite db all land (the pool is pinned to one connection so they
// share a single database rather than each seeing a private empty one).
func TestParityDBMemoryConcurrentInsert(t *testing.T) {
	runParityStateful(t, `import db;
import async;
import io;
let conn = db.connect("sqlite", ":memory:");
conn.exec("CREATE TABLE t (n INTEGER)");
list<any> tasks = [];
for (i in range(1, 20)) {
    tasks.push(async.run(func(): any { conn.exec("INSERT INTO t (n) VALUES (1)"); return null; }));
}
for (t in tasks) { async.await(t); }
let rows = conn.query("SELECT COUNT(*) AS c FROM t").all();
io.println(rows[0]["c"]);
`, "20\n")
}

// TestParityDataFrameIO pins the stateful dataframe IO surface: CSV
// file round trip and SQL load/store through in-memory SQLite.
func TestParityDataFrameIO(t *testing.T) {
	runParityStateful(t, `import io;
import db;
import dataframe as df;

let users = df.fromDict({"name": ["Ada", "Grace"], "age": [36, null]});
let dir = io.tempDir();
let path = dir + "/users.csv";
df.writeCsv(users, path);
let back = df.readCsv(path, {"types": {"age": "int"}});
io.println(back.dtypes());
io.println(back.col("age").toList());

let conn = db.connect("sqlite", ":memory:");
df.toTable(users, conn, "users");
let loaded = df.fromQuery(conn, "SELECT name, age FROM users ORDER BY name");
io.println(loaded.toDicts());
io.println(loaded.dtypes());
`, `{"name": "string", "age": "int64"}
[36, null]
[{"age": 36, "name": "Ada"}, {"age": null, "name": "Grace"}]
{"age": "int64", "name": "string"}
`)
}

// dataframe.filterFn runs a Geblang predicate per row through the native
// callable invoker; the predicate captures outer state and both backends agree.
func TestParityDataFrameFilterFn(t *testing.T) {
	runParityStateful(t, `import io;
import dataframe as df;

let users = df.fromDict({
    "country": ["UK", "US", "FI", "US"],
    "age": [36, 41, 50, 55]
});
let us = users.filterFn(func(any row): bool { return row["country"] == "US"; });
io.println(us.rows());
let threshold = 40;
let older = users.filterFn(func(any row): bool { return (row["age"] as int) > threshold; });
io.println(older.rows());
io.println(older.col("age").toList());
`, "2\n3\n[41, 50, 55]\n")
}

// filterFn predicates fired from concurrent async tasks must not corrupt backend
// state and must each see their own captured value (the per-goroutine invoker
// isolation proof). Run with -race.
func TestParityDataFrameFilterFnConcurrent(t *testing.T) {
	runParityStateful(t, `import io;
import async;
import dataframe as df;

let frame = df.fromDict({"v": [10, 20, 30, 40, 50]});
let thresholds = [10, 20, 30];
let tasks = [];
for (int th in thresholds) {
    let threshold = th;
    tasks = tasks.push(async.run(func(): int {
        return frame.filterFn(func(any row): bool { return (row["v"] as int) > threshold; }).rows();
    }));
}
io.println(async.await(async.all(tasks)));
`, "[4, 3, 2]\n")
}

// A throw inside a filterFn predicate propagates across the native boundary with
// its class and message, and is catchable, on both backends.
func TestParityDataFrameFilterFnError(t *testing.T) {
	runParityStateful(t, `import io;
import dataframe as df;
let frame = df.fromDict({"v": [1, 2, 3]});
try {
    frame.filterFn(func(any row): bool { throw RuntimeError("boom"); });
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}
io.println("done");
`, "caught: boom\ndone\n")
}

// db.Rows streams without caching; for-in iterates the cursor; all()
// after streaming returns the REMAINING rows (1.19.0 semantics).
func TestParityDBRowsStreaming(t *testing.T) {
	runParityStateful(t, `import db;
import io;
let conn = db.connect("sqlite", ":memory:");
conn.exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)");
for (i in 1..5) {
    conn.exec("INSERT INTO t (id, name) VALUES (?, ?)", i, "n${i}");
}
int sum = 0;
for (row in conn.query("SELECT id FROM t ORDER BY id")) {
    sum = sum + (row["id"] as int);
}
io.println(sum);
let rows = conn.query("SELECT id, name FROM t ORDER BY id");
io.println(rows.next());
io.println(rows.row()["name"]);
io.println(rows.all().length());
io.println(rows.next());
let eager = conn.query("SELECT name FROM t WHERE id = ?", 3).all();
io.println(eager[0]["name"]);
io.println(db.query(conn, "SELECT name FROM t WHERE id = ?", 4)[0]["name"]);
`, "15\ntrue\nn1\n4\nfalse\nn3\nn4\n")
}

// next()-driven walk over a mixed/nested document, identical on both backends.
func TestParityJSONReaderNextDrivenWalk(t *testing.T) {
	runParityStateful(t, `
import json;
import io;
let src = '[1, "two", {"k": true, "n": null}, [9]]';
let reader = json.reader(src);
let ev = reader.next();
while (ev != null) {
  io.println("${ev["type"]} | ${ev["value"]}");
  ev = reader.next();
}
io.println("eof=${reader.next()}");
reader.close();
`, "startArray | null\nvalue | 1\nvalue | two\nstartObject | null\nkey | k\nvalue | true\nkey | n\nvalue | null\nendObject | null\nstartArray | null\nvalue | 9\nendArray | null\nendArray | null\neof=null\n")
}

// hasNext()-gated iteration, identical on both backends; hasNext() stays false past EOF.
func TestParityJSONReaderHasNextLoop(t *testing.T) {
	runParityStateful(t, `
import json;
import io;
let reader = json.reader('{"a": 1, "b": [true, "x"]}');
let count = 0;
while (reader.hasNext()) {
  let ev = reader.next();
  io.println("${ev["type"]}=${ev["value"]}");
  count = count + 1;
}
io.println("events=${count}");
io.println("more=${reader.hasNext()}");
`, "startObject=null\nkey=a\nvalue=1\nkey=b\nstartArray=null\nvalue=true\nvalue=x\nendArray=null\nendObject=null\nevents=9\nmore=false\n")
}

// A malformed document surfaces an error event identically on both backends.
func TestParityJSONReaderErrorEvent(t *testing.T) {
	runParityStateful(t, `
import json;
import io;
let reader = json.reader('[1, @, 3]');
let ev = reader.next();
while (ev != null) {
  io.println(ev["type"]);
  ev = reader.next();
}
`, "startArray\nvalue\nerror\n")
}

func TestParityIOSeekTruncate(t *testing.T) {
	runParityStateful(t, `import io;
let dir = io.tempDir("geb-parity-seek-*");
let p = dir + "/d.txt";
io.writeText(p, "Hello, World!");
let f = io.open(p, "r+");
io.println(io.read(f, 5));
io.println("${io.tell(f)}");
io.seek(f, 7, "start");
io.println(io.read(f, 5));
io.seek(f, -1, "end");
io.println(io.read(f, 1));
io.truncate(f, 5);
io.close(f);
io.println(io.readText(p));
let g = io.open(p, "r");
io.println(io.readAll(g));
io.println("${io.atEnd(g)}");
io.close(g);
io.remove(dir);
`, "Hello\n5\nWorld\n!\nHello\nHello\ntrue\n")
}

func TestParityIOCopyMove(t *testing.T) {
	runParityStateful(t, `import io;
let dir = io.tempDir("geb-parity-copy-*");
io.writeText(dir + "/a.txt", "data");
io.copy(dir + "/a.txt", dir + "/b.txt");
io.println(io.readText(dir + "/b.txt"));
io.move(dir + "/b.txt", dir + "/c.txt");
io.println("${io.exists(dir + "/b.txt")}");
io.println(io.readText(dir + "/c.txt"));
io.mkdir(dir + "/tree/sub", 0o755);
io.writeText(dir + "/tree/sub/x.txt", "deep");
io.copyTree(dir + "/tree", dir + "/tree2");
io.println(io.readText(dir + "/tree2/sub/x.txt"));
io.remove(dir);
`, "data\nfalse\ndata\ndeep\n")
}

func TestParityIOStatScanDir(t *testing.T) {
	runParityStateful(t, `import io;
let dir = io.tempDir("geb-parity-stat-*");
io.writeText(dir + "/f.txt", "hi");
io.symlink(dir + "/f.txt", dir + "/lnk");
io.println("${io.stat(dir + "/f.txt")["isFile"]}");
io.println("${io.lstat(dir + "/lnk")["isSymlink"]}");
io.println("${io.stat(dir + "/lnk")["isSymlink"]}");
io.touch(dir + "/t");
io.writeTextAtomic(dir + "/atomic", "ok");
io.println(io.readText(dir + "/atomic"));
let names = [];
for (entry in io.scanDir(dir)) { names = names.push(entry["name"]); }
io.println(names.sorted().join(","));
io.remove(dir);
`, "true\ntrue\nfalse\nok\natomic,f.txt,lnk,t\n")
}

func TestParityIOExistsThroughFilePath(t *testing.T) {
	runParityStatefulWithFile(t, `import io;
io.println(io.exists("TMPFILE"));
io.println(io.exists("TMPFILE/child.html"));
io.println(io.exists("TMPFILE/a/b"));
`, "content", "true\nfalse\nfalse\n")
}

// TestParityFileObject pins the file.File wrapper (open/write/seek/truncate,
// line iteration, and the with-statement auto-close) on both backends.
func TestParityFileObject(t *testing.T) {
	runParityWithStdlib(t, `import file;
import io;
let dir = io.tempDir("geb-parity-file-*");
let p = dir + "/log.txt";
let w = file.open(p, "w");
w.writeln("one");
w.writeln("two");
w.close();
let r = file.open(p, "r+");
io.println(r.read(3));
io.println("${r.tell()}");
r.seek(0, "start");
with (g = file.open(p, "r")) {
    for (line in g) {
        io.println("> " + line);
    }
}
r.truncate(3);
r.close();
io.println(io.readText(p));
io.remove(dir);
`, "one\n3\n> one\n> two\none\n")
}

// TestParitySysRunListArgs pins sys.run's list<string> argument form on
// both backends (previously rejected as "arguments must be strings").
func TestParitySysRunListArgs(t *testing.T) {
	runParityStateful(t, `import io;
import sys;
let r = sys.run("echo", ["hello", "world"]);
io.println(r["code"]);
io.println((r["stdout"] as string).trim());
`, "0\nhello world\n")
}

func TestParityOnnxGate(t *testing.T) {
	runParityStateful(t, `import onnx;
import io;
try {
    onnx.session("/no/such/model.onnx");
    io.println("no-throw");
} catch (PermissionError e) {
    io.println("gated");
}
`, "gated\n")
}

func TestParityHTTPRequestStream(t *testing.T) {
	runParityStateful(t, `import io;
import http;
import sys;
let server = http.listen("127.0.0.1:0", func(dict<string, any> _): dict<string, any> {
    return {"status": 200, "headers": {}, "body": "one\ntwo\nthree\n"};
});
let port = (http.serverAddr(server).split(":")[1] as string) as int;
sys.sleep(20);
let stream = http.requestStream({"method": "GET", "url": "http://127.0.0.1:" + (port as string) + "/"});
io.println(stream.status());
let n = 0;
let line = stream.read();
while (line != null) {
    n = n + 1;
    line = stream.read();
}
stream.close();
http.shutdown(server);
io.println(n);
`, "200\n3\n")
}

// Cross-module dispatch into the llm module, pinned against an in-process HTTP stub so a real completion shape (and the catchable RuntimeError path) is deterministic rather than dialing api.openai.com live.
func TestParityLLMCrossModuleDispatch(t *testing.T) {
	runParityWithStdlib(t, `import io;
import http;
import sys;
import llm;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    if ((req["path"] as string) == "/v1/models") {
        return {"status": 200, "headers": {"Content-Type": "application/json"}, "body":
            "{\"data\":[{\"id\":\"gpt-5\",\"owned_by\":\"openai\"},{\"id\":\"gpt-4o\",\"owned_by\":\"openai\"}]}"};
    }
    return {"status": 200, "headers": {"Content-Type": "application/json"}, "body":
        "{\"model\":\"gpt-5\",\"choices\":[{\"finish_reason\":\"stop\",\"message\":{\"role\":\"assistant\",\"content\":\"Hi there\"}}]," +
        "\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}"};
});
sys.sleep(20);
let port = http.serverAddr(server).split(":")[1] as string;
let c = llm.client({"provider": "openai", "apiKey": "sk", "endpoint": "http://127.0.0.1:" + port});
let r = c.chat([{"role": "user", "content": "x"}], {"model": "gpt-5"});
io.println(r["content"] as string);
io.println(r["model"] as string);
io.println(r["stopReason"] as string);
let ms = c.models();
io.println(ms.length() as string);
io.println(ms[0]["id"] as string);
try {
    c.chat([{"role": "user", "content": "x"}], {});
    io.println("no-throw");
} catch (RuntimeError e) {
    io.println("threw");
}
http.shutdown(server);
`, "Hi there\ngpt-5\nstop\n2\ngpt-5\nthrew\n")
}

// TestParityLLMStreamingCallback is the dividend of the bcloader extraction: a source-stdlib llm.chatStream re-enters an entry-script callback that mutates an entry global, needing the loader's live entry-chunk globals (mainVM); the pre-extraction harness lacked mainVM and this failed on the VM.
func TestParityLLMStreamingCallback(t *testing.T) {
	runParityWithStdlib(t, `import io;
import http;
import sys;
import llm;
let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "headers": {"Content-Type": "text/event-stream"}, "body":
        "data: {\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n" +
        "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n" +
        "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n" +
        "data: [DONE]\n"};
});
sys.sleep(20);
let port = http.serverAddr(server).split(":")[1] as string;
let c = llm.client({"provider": "openai", "apiKey": "sk", "endpoint": "http://127.0.0.1:" + port});
let pieces = [];
let r = c.chatStream([{"role": "user", "content": "hi"}], {"model": "gpt-5"}, func(string delta): void {
    pieces = pieces.push(delta);
});
http.shutdown(server);
io.println(pieces.length() as string);
io.println(pieces[0] as string);
io.println(r["content"] as string);
io.println(r["stopReason"] as string);
`, "2\nHello\nHello world\nstop\n")
}

// TestParityReflectFieldDoc pins field-docblock exposure via reflect.fields on both backends.
func TestParityReflectFieldDoc(t *testing.T) {
	runParityStateful(t, `import reflect;
import io;
class User {
    /** the unique id */
    int id;
    string name;
}
let fs = reflect.fields(User);
io.println((fs[0] as dict<string, any>)["doc"] ?? "null");
io.println((fs[1] as dict<string, any>)["doc"] ?? "null");
`, "the unique id\nnull\n")
}
