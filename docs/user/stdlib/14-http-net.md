# HTTP, Networking, And WebSockets

## HTTP

Import `http` for client calls and simple servers.

- server: `serve`, `listen`, `close`, `shutdown`, `serverAddr`, `serverCert`
- client: `get`, `post`, `postJson`, `request`, `requestWithOptions`, `requestStream`
- streaming: `stream`, `streamWrite`, `streamFlush`, `streamClose` (server); `requestStream` (client)
- helpers: `parseJson`, `Headers`, `Cookie`, `response`, `jsonResponse`
- classes exported by import: `Request`, `Response`

```gb
import http;
import io;

let response = http.get("https://example.com");
io.println(response["status"]);
```

### The Response object

Client calls (`get`, `post`, `postJson`, `request`, the request builder's
`send`, the client methods, and `fetchAll`) return a `Response` object with
reader methods:

```gb
let r = http.get("https://api.example.com/users/7");

r.status();         # 200 (int)
r.ok();             # true for any 2xx
r.text();           # body as a string
r.bytes();          # body as raw bytes
r.json();           # body parsed as JSON
r.body();           # the raw body value, untyped (same as r["body"])
r.header("ETag");   # first value of a header, or null
r.headers();        # all response headers
r.url();            # the final URL after any redirects (empty for non-request responses)

r.isSuccessful();   # 2xx
r.isRedirect();     # 3xx
r.isClientError();  # 4xx
r.isServerError();  # 5xx
r.isNotFound();     # 404
```

For a single call like `http.get`, a request that never reaches the server
(DNS failure, connection refused, reset) is raised as an `IOError` (catch it
with `try { ... } catch (IOError e) { ... }`). A timeout is the `IOError`
subclass `TimeoutError`, and a TLS/certificate failure is `TlsError`, so you can
retry on a timeout but fail fast on a bad cert; `catch (IOError e)` still catches
all three. In a parallel batch (`getAll` / `fetchAll`) the same failure is
reported on the Response instead, so one bad request does not abort the batch:

```gb
r.isError();   # true only for a transport-level failure
r.error();     # the failure message, or null when isError() is false
```

The `Response` is also index-compatible with the plain dict shape used in
earlier versions, so existing code keeps working:

```gb
r["status"];        # same as r.status()
r["body"];          # same as r.body()
r["headers"];       # same as r.headers()
r["url"];           # same as r.url()
let plain = r.toDict();   # a plain dict<string, any> snapshot
```

Recoverable HTTP transport and server I/O failures are surfaced as Geblang
exceptions. Server bind failures, including port conflicts, are `IOError`
values:

```gb
try {
    http.serve("127.0.0.1:8080", handler);
} catch (IOError e) {
    io.println("server could not start: " + e.message);
}
```

HTTP response statuses are not exceptions. A `404`, `422` or `500` response is
returned as a normal response value; inspect `response["status"]` and decide how
your application should handle it.

### The request builder

`http.request(url)` with a single argument starts an immutable, fluent
request builder. Each `withX` method returns a new builder, so a base
builder can be safely reused for several requests:

```gb
let r = http.request("https://api.example.com/users")
    .withMethod("POST")
    .withQuery("page", "2")
    .withHeader("X-Trace", "abc")
    .withHeaders({"X-A": "1", "X-B": "2"})
    .withJson({"name": "ada"})       # sets the body and Content-Type
    .withBearer(token)               # Authorization: Bearer <token>
    .withTimeout(2000)               # per-request timeout in ms
    .send();                         # returns a Response

io.println(r.status());
```

Available builder methods: `withMethod`, `withHeader`, `withHeaders`,
`withQuery`, `withBody`, `withBodyFile`, `withJson`, `withBearer`,
`withBasicAuth`, `withTimeout`, and `send`. Because each step is
immutable, sibling requests never leak each other's headers:

```gb
let base = http.request(url).withMethod("POST").withJson(payload);
let withAuth = base.withBearer(token);   # base is unchanged
```

`withBodyFile(path)` streams a file from disk as the request body
(1.16.0). The file is opened at `send()` time, `Content-Length` comes
from the file size, and the contents never load into memory - use it for
large uploads. `withBody`, `withJson`, and `withBodyFile` replace each
other; the last one set wins:

```gb
let resp = http.request(uploadUrl)
    .withMethod("PUT")
    .withHeader("Content-Type", "application/octet-stream")
    .withBodyFile("/var/backups/archive.tar.gz")
    .send();
```

The older mutating builder from `http.build(url)` (`.method`, `.header`,
`.body`, `.timeout`) still works.

### Parallel requests

`http.getAll(urls)` issues parallel GETs and returns a list of Responses
in the same order as the input. `http.fetchAll(requests)` is the general
form: each element may be a request builder or a request-spec dict. Both
accept an options dict whose `limit` caps how many run at once (0 or
omitted means unbounded), and both are awaitable.

When every request is a plain GET, pass the URLs directly:

```gb
import async;
import http;

let pages = await http.getAll([
    "https://example.com/page/1",
    "https://example.com/page/2",
    "https://example.com/page/3"
], {"limit": 4});
```

When the requests differ, build each one with the request builder, bind it
to a name, and hand the list to `fetchAll`. Because each builder is an
immutable value, this reads as a set of described requests sent together:

```gb
let createUser = http.request(usersUrl)
    .withMethod("POST")
    .withBearer(token)
    .withJson({"name": "ada"});

let listTeams = http.request(teamsUrl).withBearer(token);

let responses = await http.fetchAll([createUser, listTeams]);

for (response in responses) {
    if (response.isError()) {
        /* The request never reached the server (see below). */
        io.println("request failed: " + response.error());
    } else if (response.ok()) {
        io.println(response.json());
    } else {
        io.println("server returned status " + (response.status() as string));
    }
}
```

**Reading the results.** Every entry is a `Response`, in the same order as
the input, so the list is uniform and you never have to type-check it. There
are three cases to tell apart:

- A request that succeeded: `response.ok()` is true (a 2xx status).
- A request that reached the server but came back with an HTTP error status:
  `response.ok()` is false, but `response.isError()` is *also* false. A
  `404` or `500` is a normal `Response`; check `response.status()` or the
  `isClientError()` / `isServerError()` predicates.
- A request that never completed a round trip at all (DNS lookup failed,
  connection refused, the timeout elapsed): `response.isError()` is true and
  `response.error()` holds the failure message. Its `status()` is `0`.

The batch never throws because one request failed, so a single bad entry
does not lose the others; check `response.isError()` first, as above.

A configured client exposes the same `fetchAll` for connection-pool reuse.

`http.fetchStream(requests)` takes the same list of builders or request-spec
dicts but hands back each `Response` as soon as it completes, instead of
waiting for the whole batch. It returns a `FetchStream`:

| Method | Description |
|--------|-------------|
| `next()` | Blocks for the next completed `Response`, or returns `null` once every one has been read. |
| `nextAsync()` | Awaitable form of `next()`. |
| `remaining()` | How many responses have not been read yet. |
| `done()` | `true` once every response has been read. |

Responses arrive in completion order, not input order. Each one carries
`response["index"]` (its position in the input list) and `response["url"]`.
Failed requests follow the same `isError()` rules as `fetchAll`.

```gb
let stream = http.fetchStream([
    http.request(usersUrl),
    {"method": "GET", "url": teamsUrl}
]);
while (!stream.done()) {
    let response = stream.next();
    io.println("request ${response["index"]} finished: ${response.status()}");
}
```

A configured client exposes the same `fetchStream`.

### Client-side response streaming (1.24.0)

`http.requestStream(options)` performs a request like `requestWithOptions`, but
instead of buffering the whole body it returns a `StreamResponse` you read
line-by-line as data arrives. This is the client-side analog of server-sent
events: long-polling feeds, chunked logs, and LLM token streams.

| Method | Description |
|--------|-------------|
| `read()` | The next line of the body (trailing newline stripped), or `null` at end of stream. |
| `status()` | The HTTP status code. |
| `headers()` | The response headers as a `dict<string, string>`. |
| `done()` | `true` once the stream has been fully read or closed. |
| `close()` | Closes the underlying connection (call it when you stop early). |

```gb
let stream = http.requestStream({
    "method":  "GET",
    "url":     "https://example.com/events",
    "headers": {"Accept": "text/event-stream"}
});
if (stream.status() != 200) {
    stream.close();
    throw RuntimeError("stream failed: " + (stream.status() as string));
}
let line = stream.read();
while (line != null) {
    if ((line as string).startsWith("data: ")) {
        io.println("event: " + (line as string).substring(6));
    }
    line = stream.read();
}
stream.close();
```

### Client configuration

`http.newClient(opts)` creates a configurable, reusable client. Most options
shape the underlying connection pool and cookie behaviour:

| Key            | Type                          | Default | Meaning |
|----------------|-------------------------------|---------|---------|
| `timeoutMs`    | int                           | none    | Per-request deadline (entire request lifecycle) |
| `baseUrl`      | string                        | none    | Prefix for relative URLs passed to `client.get(...)` etc. |
| `headers`      | dict<string, string> / Headers | empty  | Default headers added to every request |
| `cookieJar`    | CookieJar or `true`           | none    | Persist cookies across requests. Pass an explicit `http.newCookieJar()` to inspect/share, or `true` to attach a fresh jar |
| `keepAlive`    | bool                          | `true`  | Reuse the underlying TCP/TLS connection. Set `false` to force a new connection per request (debugging only) |
| `maxIdleConns` | int                           | Go default | Idle-connection pool size (per host and total) |
| `proxy`        | string                        | none    | Forward all requests through this HTTP/HTTPS proxy URL |
| `proxyFromEnv` | bool                          | `false` | Honour `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` environment variables |
| `tls`          | dict                          | none    | TLS settings (see TLS below): `verify`, `caCerts`, `caCertsOnly`, `clientCert`, `clientKey` |

Per-call options accepted by `Client.request(opts)` cover retries and
timeouts in addition to the standard `method`, `url`, `body`, and `headers`:

| Key                  | Type      | Default                | Meaning |
|----------------------|-----------|------------------------|---------|
| `timeoutMs`          | int       | client default         | Overrides the per-request timeout for this call |
| `retries`            | int       | `1` (no retry)         | Total attempts including the first |
| `retryBackoffMs`     | int       | `100`                  | Base backoff before retry 2; doubled each retry, with full jitter |
| `retryBackoffMaxMs`  | int       | `5000`                 | Upper bound on a single sleep |
| `retryStatuses`      | list<int> | `[502, 503, 504, 429]` | Status codes that trigger a retry |

### TLS (HTTPS)

Clients verify TLS certificates against the system trust store by default. The
`tls` option customises this:

| Key           | Type           | Default | Meaning |
|---------------|----------------|---------|---------|
| `verify`      | bool           | `true`  | Set `false` to skip certificate verification (testing / self-signed only) |
| `caCerts`     | string / bytes | none    | PEM certificate(s) to trust in addition to the system roots |
| `caCertsOnly` | bool           | `false` | When `true`, trust *only* `caCerts` and ignore the system roots |
| `clientCert`  | string / bytes | none    | PEM client certificate for mutual TLS |
| `clientKey`   | string / bytes | none    | PEM private key for `clientCert` (the two must be supplied together) |

`http.serve` / `http.listen` serve HTTPS when their opts dict carries a `tls`
block: pass `{cert, key}` (PEM) for a real certificate, or `{selfSigned: true}`
to generate an in-memory certificate for local development. A self-signed cert
covers `localhost`, `127.0.0.1`, `::1`, and the bind host; pass
`selfSigned: ["host", ...]` to set the SANs explicitly. `http.serverCert(server)`
returns the served certificate as PEM so a client can trust it precisely
instead of disabling verification - write it to a file with
`io.writeText` to add it to a local trust store.

```gb
import http;
import io;

let server = http.listen("127.0.0.1:0", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "secure"};
}, {"tls": {"selfSigned": true}});

let url = "https://" + http.serverAddr(server) + "/";

# trust the server's self-signed cert precisely (no verify:false needed)
let client = http.newClient({"tls": {"caCerts": http.serverCert(server)}});
io.println(client.get(url)["body"]);   # secure

http.close(server);
```

#### Mutual TLS (server-side client certificates)

A server verifies client certificates by adding `clientCa` (the CA pool to
check presented certs against) and `clientAuth` to its `tls` block:

| Key          | Type           | Meaning |
|--------------|----------------|---------|
| `clientCa`   | string / bytes | PEM CA certificate(s) that signed acceptable client certs |
| `clientAuth` | string         | `"require"` (default when `clientCa` is set) presents and verifies; `"optional"` verifies only if a cert is offered |

A handler that received a rich `Request` reads the verified peer certificate
with `req.clientCert()` - `null` when no client certificate was presented,
otherwise a dict with `subject`, `issuer`, `serialNumber`, `notBefore`,
`notAfter`, and `dnsNames`:

```gb
let server = http.listen("127.0.0.1:0", func(Request req): Response {
    let cert = req.clientCert();
    if (cert == null) {
        return http.response("client certificate required", 401);
    }
    return http.jsonResponse({"caller": cert["subject"]});
}, {"tls": {"selfSigned": true, "clientCa": caPem, "clientAuth": "require"}});
```

The client presents its certificate with the `clientCert` / `clientKey`
options shown in the table above. Under `"require"` a request without a
valid certificate fails the TLS handshake (surfaced as an `IOError` on the
client).

#### Automatic certificates (ACME / Let's Encrypt)

Instead of supplying `cert`/`key`, a public server can obtain and renew
certificates automatically with `autoCert`:

| Key                | Type           | Meaning |
|--------------------|----------------|---------|
| `autoCert`         | string / list  | The host (or hosts) to obtain certificates for; this is the allowlist |
| `autoCertCacheDir` | string         | Directory to persist issued certs (defaults to a per-user cache dir); use a writable, persistent path in production |
| `autoCertEmail`    | string         | ACME account contact address (optional) |

```gb
http.serve(":443", handler, {"tls": {"autoCert": "example.com"}});

http.serve(":443", handler, {"tls": {
    "autoCert": ["example.com", "www.example.com"],
    "autoCertCacheDir": "/var/lib/geblang/acme",
    "autoCertEmail": "ops@example.com"
}});
```

Certificates are issued on the first TLS connection for an allowlisted host
via the TLS-ALPN-01 challenge on the same `:443` listener, so no separate
port-80 challenge server is needed. The host must be publicly resolvable to
the server for issuance to succeed. `autoCert` cannot be combined with
`cert`/`key` or `selfSigned`.

#### HTTP/2

HTTPS servers negotiate HTTP/2 automatically (Go's `net/http`); no option is
required. Plain-text HTTP/1.1 is used for non-TLS servers.

### Cookie jars

A cookie jar persists `Set-Cookie` responses and replays them on subsequent
requests to matching hosts. This is the right shape for any client that needs
to maintain a session (login, CSRF tokens, server-stored basket, etc.).

```gb
let jar     = http.newCookieJar();
let session = http.newClient({"cookieJar": jar, "baseUrl": "https://api.example.com"});

session.post("/login", "{\"user\":\"ada\",\"password\":\"...\"}",
             {"Content-Type": "application/json"});

# the session cookie is automatically sent on subsequent requests
let me = session.get("/me");

# inspect or persist the cookies between runs
for (c in jar.cookies("https://api.example.com")) {
    io.println(c["name"] + "=" + c["value"]);
}
```

`CookieJar.cookies(url)` returns a `list<dict>` with `name`, `value`,
`domain`, `path`, `secure`, and `httpOnly`. `CookieJar.setCookies(url, list)`
populates the jar from a saved list (useful for reusing a session across
process restarts). `CookieJar.clear()` empties the jar.

### Proxies and connection control

Most production workloads benefit from a small idle pool and a proxy:

```gb
let client = http.newClient({
    "baseUrl":      "https://internal.example.com",
    "proxy":        "http://proxy.example.com:3128",
    "keepAlive":    true,
    "maxIdleConns": 32,
    "timeoutMs":    10000
});
```

Set `"proxyFromEnv": true` to defer to `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`
environment variables instead of hard-coding a URL.

### User-Agent

Every outgoing HTTP request defaults to `User-Agent: Geblang/1.0`. Override
by setting your own value either in the per-call `headers` argument or as a
`headers` default on the client:

```gb
let c = http.newClient({"headers": {"User-Agent": "MyApp/2.0"}});
```

Network errors are always retried up to `retries`. When all attempts are
exhausted, the last response (or the last network error) is returned to
the caller; no exception is thrown.

```gb
let client = http.newClient({"timeoutMs": 5000});
let resp = client.request({
    "method": "GET",
    "url": "https://example.com/flaky",
    "retries": 5,
    "retryBackoffMs": 200,
    "retryStatuses": [502, 503, 504, 429, 500]
});
```

`http.stream(handler)` creates a low-level streaming response. The source-level
`web.sse.streaming(handler)` wrapper gives the handler an `sse.EventStream`
that can write and flush chunks while the request remains open:

```gb
import http;
import sys;
import web.sse as sse;

func events(dict<string, any> request): dict<string, any> {
    return sse.streaming(func(sse.EventStream stream): void {
        for (n in 1..3) {
            stream.write(sse.event("tick", "count " + (n as string), {}));
            stream.flush();
            sys.sleep(1000);
        }
        stream.close();
    });
}
```

### Concurrency limits

`http.listen` and `http.serve` accept an optional opts dict that caps
simultaneous in-flight handlers and decides what happens when the cap is
reached. Defaults preserve the unbounded behaviour - the opts dict is
opt-in.

```gb
let id = http.listen(":8080", handler, {
    "maxConcurrent": 1000,    # at most 1000 active handlers; 0 = unbounded
    "queueSize":     500,     # requests waiting for a slot
    "onOverload":    "reject" # "reject" | "wait" | "drop"
});
```

`onOverload` controls the action when slots and queue are both full:

- `"reject"` (default once a cap is set) responds with HTTP 503 and the
  body `server at capacity`.
- `"wait"` keeps the request blocked until a slot opens. There is no
  rejection at all; the queue size becomes informational.
- `"drop"` closes the connection silently without writing a body.

WebSocket connections inherit the same cap because the upgrade happens
inside an HTTP handler. A WebSocket holds its slot for the entire
connection lifetime, which gives you a hard cap on simultaneous
WebSocket clients.

Read the pool's running counters with `http.serverStats(server)`:

```gb
let stats = http.serverStats(id);
# {"active": 42, "queued": 7, "rejected": 0, "maxConcurrent": 1000}
```

#### Which concurrency model do I want?

There are three points on the spectrum; this section picks one for you
based on what your workload actually looks like.

- **Default (no opts).** Go's runtime spawns a goroutine per request
  and multiplexes them onto OS threads via its M:N scheduler. The
  scheduler uses epoll on Linux / kqueue on macOS / IOCP on Windows
  under the hood, so blocking-looking handlers do not actually pin a
  thread. This handles thousands of concurrent connections without
  any opt-in. Use this when your handlers are fast (sub-100 ms),
  total active connections are below ~10k, and load is bursty rather
  than sustained. Most apps stop here.

- **Bounded concurrency (`maxConcurrent` opt).** Adds a semaphore in
  front of the handler. Same Go scheduler underneath; the cap is on
  in-flight work, not on the runtime's I/O multiplexer. Use this
  when handlers are slow or hold expensive resources (DB connections,
  large memory buffers, long-lived WebSocket sessions) and an
  unbounded goroutine pile can run you out of memory or starve
  downstream services. The cap gives you predictable peak memory and
  a clean backpressure signal (503) instead of an OOM.

- **Full epoll/kqueue reactor.** Not built into Geblang. A real
  reactor (gnet-style) would bypass `net/http` entirely, manage file
  descriptors directly via the kernel poll APIs, and run each request
  on a fixed-size worker pool. It only meaningfully helps past ~100k
  persistent connections per box, where Go's per-goroutine stack
  (~8 KB minimum) and scheduler overhead start to matter. Below that
  scale the bounded-concurrency option above is the same wall-clock
  performance with a fraction of the implementation surface. If you
  hit a workload that needs more, the right move is dropping below
  Geblang into a Go program that uses `gnet` or `evio` directly.

Rule of thumb: start without opts, switch to `maxConcurrent` the first
time a slow handler or a memory ceiling matters, and don't reach for a
custom reactor until you have measurements proving the Go scheduler is
the bottleneck (it usually isn't).

### Handler isolation (`shareHandler`)

By default each request (`http.serve` / `http.listen`) or connection
(`net.serve`) gets an isolated copy of the handler: its captured state is
deep-cloned, so concurrent invocations cannot see or race each other's
mutations. Set `shareHandler: true` in the opts dict to share one handler
across all invocations instead - this is how a framework keeps a single
application graph (one router, one pool, one config):

```geblang
http.serve(":8080", handler, {"shareHandler": true});
http.listen(":8080", handler, {"shareHandler": true});
net.serve("127.0.0.1", 9000, handler, {"shareHandler": true});
```

When a handler is shared, do not mutate a plain `dict`, `list`, `set`, or
object reachable from more than one invocation; use a `store.Store` for
shared mutable state. Both backends behave identically.

### Server request and response objects

A handler receives a plain request dict by default. To opt into a rich
`Request` object, declare the handler parameter as `Request`; everything
else stays the same. The handler may return a `Response` (from
`http.response`, `http.jsonResponse`, or `http.redirect`) or a plain dict.

The dict form carries `method`, `path`, `query`, `headers`, `body`, and
`remoteAddr`, plus the proxy-aware `scheme`, `host`, and `clientIp` keys
(resolved exactly as the `Request` accessors below, honoring
`trustedProxies`). A verified mTLS peer certificate appears under
`_clientCert`.

```gb
import http;

let id = http.listen(":8080", func(Request req): Response {
    if (!req.isMethod("GET")) {
        return http.response("method not allowed", 405);
    }
    if (req.path == "/old") {
        return http.redirect("/new");          # 302 by default
    }
    let page = req.queryInt("page");           # ?int, null when absent
    return http.jsonResponse({
        "path": req.path,
        "page": page,
        "user": req.cookie("user"),            # ?string
        "secure": req.isSecure()
    });
});
```

`Request` accessors:

```gb
req.method          # field: "GET", "POST", ...
req.path            # field: the URL path
req.isMethod("POST")
req.scheme()        # "http" or "https"
req.isSecure()      # scheme is https
req.host()          # request host
req.clientIp()      # client IP (no port)
req.clientCert()    # ?dict: verified client cert (mTLS), or null
req.header("Accept")    # ?string, first value
req.isJson()        # Content-Type is JSON
req.text()          # body as a string
req.json()          # body parsed as JSON
req.cookie("sid")   # ?string
req.query("q")      # ?string (first value)
req.queryInt("page")    # ?int
req.queryBool("debug")  # ?bool
req.queryAll("tag")     # list<string> (all values)
req.routeParam("id")    # ?string: a path parameter from a "/users/:id" route
req.routeParams()       # dict<string, string>: all path parameters
req.bodyBytes()         # body as raw bytes (text()/bodyText() give a string)
req.toDict()            # a plain dict<string, any> snapshot
```

Server Response builders all return a `Response` (the same object the
client returns, with the `status()` / `ok()` / status-predicate methods):
`http.response(body, status=200)`, `http.jsonResponse(payload, status=200)`,
and `http.redirect(url, status=302)`.

#### Trusted proxies

By default `scheme()`, `isSecure()`, `host()`, and `clientIp()` come from
the connection itself, and `X-Forwarded-*` headers are ignored (they are
trivially spoofable). When the server runs behind a reverse proxy or load
balancer, list the proxy addresses in `trustedProxies` so the forwarded
headers are honored only for connections coming from those addresses:

```gb
http.listen(":8080", handler, {
    "trustedProxies": ["10.0.0.0/8", "127.0.0.1"]
});
# common shorthand for any private / loopback peer:
http.listen(":8080", handler, { "trustedProxies": ["private"] });
```

Entries are IPs or CIDRs; the keyword `"private"` expands to loopback,
link-local, and RFC 1918 / ULA ranges. When the immediate peer is trusted,
`clientIp()` is taken from `X-Forwarded-For` (the rightmost address that is
not itself a trusted proxy), `scheme()`/`isSecure()` from
`X-Forwarded-Proto`, and `host()` from `X-Forwarded-Host`. From an
untrusted peer the socket values win, so a client cannot spoof its IP.

#### Request body limits

`maxBodyBytes` caps the request body the server will read. A request
whose body exceeds the cap (by `Content-Length` or while streaming) is
answered with `413 Request Entity Too Large` before the handler runs.
Absent (or `0`) means unlimited:

```gb
http.listen(":8080", handler, { "maxBodyBytes": 10 * 1024 * 1024 });
```

#### Handler errors and debug mode

When a handler throws and nothing catches it, the client receives a
generic `500 Internal Server Error` body and the server logs one line
to stderr (`http.serve: handler error: ValueError: ...`). Error text is
never sent to clients in this default mode (1.19.0; earlier releases
returned the raw error text in the 500 body).

During development, enable debug mode to get the full stack trace both
in the server log and in the 500 response body. Set the `debug` server
option, or set the `GEBLANG_DEBUG` environment variable to any
non-empty value other than `0`:

```gb
http.listen(":8080", handler, { "debug": true });
```

```sh
GEBLANG_DEBUG=1 geblang server.gb
```

#### Graceful shutdown

`http.shutdown(server, timeoutMs = 5000)` stops accepting new
connections and lets in-flight requests finish within the deadline.
`http.wait(server)` blocks until the server stops serving, however
that happens - so a signal handler can drain a server the main
goroutine is waiting on:

```gb
let server = http.listen(":8080", handler);
sys.onSignal("SIGTERM", func(string sig): void {
    http.shutdown(server, 10000);
});
http.wait(server);   # returns once the drain completes
```

#### Server error logging

Connection-level failures (TLS handshake errors, malformed requests) happen
before any handler runs, so they cannot be caught as Geblang errors. By
default they are silent rather than being written to the process's standard
error. Pass an `onError` callback in the server options to observe them (for
logging, metrics, or alerting):

```gb
http.listen(":8080", handler, {
    "onError": func(string msg): void {
        io.println("server error: " + msg);
    }
});
```

The callback receives one message string per failure and runs on the
connection's goroutine; its return value is ignored. Errors from genuine
listener startup (bad address, missing certificate) are still returned from
`http.serve` / `http.listen` and surface as ordinary catchable Geblang errors.

## Net

Import `net` for DNS, TCP, and UDP:

- address helpers: `joinHostPort`, `splitHostPort`, `lookupHost`
- TCP: `listenTcp`, `connectTcp`, `accept`, `read`, `write`, `close`,
  `localAddr`, `remoteAddr`, `setDeadline`, `clearDeadline`
- UDP: `listenUdp`, `dialUdp`, `readFrom`, `writeTo`

For task-returning socket calls, import `async.net`.

`net.serve` accepts the same `maxConcurrent` / `queueSize` /
`onOverload` opts as `http.listen`. On overload, the listener closes
the accepted socket immediately (the same effect for `reject` and
`drop`). Poll counters with `net.serverStats(handle)`.

Socket operations use the same error model as HTTP: connection failures,
deadline failures, and bind conflicts throw `IOError` so applications can
recover or report a clean message.

### IP and CIDR utilities (1.6.0)

`net` also exposes pure helpers for IP addresses and CIDR ranges,
useful for allow-lists, deny-lists, network classification, and
binary protocols. Backed by Go's `net/netip`.

| Function | Returns | Description |
|----------|---------|-------------|
| `net.parseIp(s)` | `dict<string, any>` | `{version: 4\|6, address: canonical, bytes: 4 or 16}` for any valid IPv4 / IPv6 address. Throws on malformed input. |
| `net.parseCidr(s)` | `dict<string, any>` | `{network, prefixLen, version, first, last, count}`. The network field is the masked base; count is the total host count and lifts to bigint for IPv6 ranges. Throws on malformed input. |
| `net.cidrContains(cidr, ip)` | `bool` | True when `ip` falls within `cidr`. |
| `net.cidrRange(cidr)` | `dict<string, any>` | `{first, last, count}` for the CIDR's inclusive range. |
| `net.isIpv4(s)` | `bool` | True when `s` parses as IPv4. Never throws. |
| `net.isIpv6(s)` | `bool` | True when `s` parses as a native IPv6 address. Never throws; returns false for IPv4-mapped addresses. |
| `net.ipToBytes(s)` | `bytes` | Raw 4-byte (IPv4) or 16-byte (IPv6) representation. Throws on malformed input. |
| `net.ipFromBytes(b)` | `string` | Decodes 4 or 16 bytes back to a canonical IP string. Throws on any other length. |

```gb
import net;
import io;

let allow = ["10.0.0.0/8", "192.168.0.0/16", "127.0.0.1/32"];

func isInternal(string ip): bool {
    for (cidr in allow) {
        if (net.cidrContains(cidr, ip)) { return true; }
    }
    return false;
}

io.println(isInternal("10.5.5.5"));     # true
io.println(isInternal("8.8.8.8"));      # false

let c = net.parseCidr("2001:db8::/32");
io.println(c["count"]);   # 79228162514264337593543950336
```

## Static File Server

`http.server` is an executable source module. Run it with `-m` to serve the
current directory for local development:

```sh
geblang -m http.server 8080
```

The optional port defaults to `8000`. The server binds to `127.0.0.1`, serves
`index.html` for directories when present, and otherwise renders a simple
directory listing.

## WebSocket

Import `websocket` for low-level WebSocket client and server operations, or
`web.websocket` for the higher-level OO interface.

### Server: `web.websocket`

`ws.upgrade(handler)` returns a special HTTP response dict that triggers the
WebSocket handshake when returned from a route handler. Inside the handler,
`conn` is a `Connection` value with the full OO interface:

```gb
import web.router as router;
import web.websocket as ws;

let app = router.newRouter();

router.get(app, "/socket", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(ws.Connection conn): void {
        let msg = conn.readText();
        conn.sendText("echo: " + msg);
        conn.close();
    });
});
```

`Connection` methods: `sendText`, `readText`, `sendJson`, `readJson`,
`sendBytes`, `readBytes`, `close`.

### Client: `web.websocket`

```gb
import web.websocket as ws;

let conn = ws.connect("ws://127.0.0.1:8080/socket");
conn.sendJson({"message": "hello"});
let reply = conn.readJson();
conn.close();
```

`ws.connectWithHeaders(url, headers)` is available when custom headers are
needed (e.g. for authentication tokens).

### Low-level: `websocket`

For scripts that don't use the router, import the native `websocket` module
directly. `upgrade(handler)` works the same way - it returns a response dict
- but the handler receives a raw opaque handle rather than a `Connection`:

```gb
import http;
import websocket;

http.serve("127.0.0.1:8080", func(dict<string, any> request): dict<string, any> {
    return websocket.upgrade(func(any conn): void {
        websocket.sendText(conn, "hello " + websocket.readText(conn));
        websocket.close(conn);
    });
});
```

## URL Parsing And Manipulation

Import `url` to parse, construct, and transform URLs:

```gb
import url;
import io;

let u = url.URL("https://example.com:8080/search?q=hello#results");
io.println(u.scheme());    # https
io.println(u.host());      # example.com
io.println(u.port());      # 8080
io.println(u.path());      # /search
io.println(u.fragment());  # results

let q = u.query();
io.println(q["q"]);        # hello
```

### `url.URL` methods

| Method | Description |
|--------|-------------|
| `scheme()` | scheme string |
| `host()` | host without port |
| `port()` | port string (empty if default) |
| `path()` | path component |
| `query()` | `dict<string, string>` of query params |
| `fragment()` | fragment after `#` |
| `toString()` | full URL string |
| `toDict()` | dict with `scheme`, `host`, `port`, `path`, `query`, `fragment` keys |
| `withScheme(s)` | new URL with scheme replaced |
| `withHost(h)` | new URL with host replaced |
| `withPath(p)` | new URL with path replaced |
| `withQuery(q)` | new URL with query replaced (dict or raw string) |
| `withFragment(f)` | new URL with fragment replaced |
| `resolve(ref)` | resolve a reference URL against this base |
| `normalize()` | clean path and re-encode query string |

All `with*` methods return new `url.URL` values rather than mutating in place.

### Module-level functions

```gb
# parse returns a plain dict
let parts = url.parse("https://example.com/path?k=v");

# stringify reassembles a dict back into a URL string
let text = url.stringify({"scheme": "https", "host": "example.com", "path": "/users"});

# encode / decode query-string components
let encoded = url.encode("hello world");   # hello+world
let decoded = url.decode("hello+world");   # hello world

# joinPath appends path segments to a base URL string
let api = url.joinPath("https://api.example.com", "v1", "users");
# https://api.example.com/v1/users
```

Use `url.URL` for chaining or repeated access to URL parts. Use `url.parse`
when you need a plain dict, for example to pass directly to JSON.
