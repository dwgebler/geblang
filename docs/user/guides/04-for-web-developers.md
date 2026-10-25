# Geblang for web developers

## Who this is for

This guide is for developers who build HTTP APIs and web services and want to
understand how Geblang's language-level web stack maps to the tools they already
know. Whether you come from Express (Node.js), Flask (Python), or Go's `net/http`,
you will recognise the shape: a small, composable set of routing, middleware,
request/response, and JSON primitives that you wire together yourself. This guide
covers the stdlib `http` client and server, the `web-router`, JSON and schema
validation, typed handlers, and async parallel requests.

## Quick orientation

The following program starts a local server on a random port, exercises it with
the built-in client, and exits. It demonstrates the core pattern: `http.listen`
for the server, the fluent request builder for the client, and `http.close` to
shut down cleanly.

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    let name = req.query("name");
    if (name == null) {
        return http.jsonResponse({"error": "name param required"}, 400);
    }
    return http.jsonResponse({"hello": name});
});

let addr = http.serverAddr(server);

let r = http.get("http://" + addr + "/?name=Ada");
io.println(r.status());
io.println(r.json()["hello"]);

http.close(server);
```

Output:

```
200
Ada
```

The handler opts into the rich `Request` object by declaring `Request` as its
parameter type. The handler returns a `Response` built by `http.jsonResponse`.
Port `0` asks the OS to pick a free port; `http.serverAddr` reads back what was
chosen.

## Coming from Express, Flask, and net/http: concept mapping

| Concept | Express (Node.js) | Flask (Python) | net/http (Go) | Geblang |
|---------|-------------------|----------------|---------------|---------|
| Start server | `app.listen(port)` | `app.run(port=...)` | `http.ListenAndServe(addr, mux)` | `http.serve(addr, handler)` or `http.listen(addr, handler)` |
| Route definition | `app.get("/path", handler)` | `@app.route("/path")` | `mux.HandleFunc("/path", handler)` | `router.get(app, "/path", handler)` |
| Route parameter | `req.params.id` | `<id>` in route + `id` arg | Named param via mux | `req.routeParam("id")` or `ctx.param("id")` |
| Query string | `req.query.page` | `request.args.get("page")` | `r.URL.Query().Get("page")` | `req.query("page")` / `req.queryInt("page")` |
| Request body (JSON) | `req.body` (after middleware) | `request.get_json()` | `json.NewDecoder(r.Body).Decode(...)` | `req.json()` or `request["body"]` |
| Set response header | `res.set("X-Foo", "bar")` | `response.headers["X-Foo"] = "bar"` | `w.Header().Set(...)` | `http.response(body).withHeader("X-Foo", "bar")` |
| JSON response | `res.json({...})` | `jsonify({...})` | `json.NewEncoder(w).Encode(...)` | `http.jsonResponse({...})` |
| Redirect | `res.redirect("/new")` | `redirect("/new")` | `http.Redirect(w, r, "/new", 302)` | `http.redirect("/new")` |
| Before middleware | `app.use(fn)` | `@app.before_request` | Wrap the handler | `router.before(app, fn)` |
| Response middleware | `app.use(fn)` (next pattern) | `@app.after_request` | Wrap the handler | `router.use(app, fn)` |
| Route groups | `express.Router()` + `app.use("/prefix", sub)` | Blueprint | Sub-router / prefix mux | `router.group(app, "/prefix")` |
| JSON parse | `JSON.parse(str)` | `json.loads(str)` | `json.Unmarshal(data, &v)` | `json.parse(str)` |
| JSON stringify | `JSON.stringify(obj)` | `json.dumps(obj)` | `json.Marshal(v)` | `json.stringify(value)` |
| Request validation | joi / zod | marshmallow / pydantic | Hand-rolled / validator libs | `web.validation` or `schema.validate` |
| Parallel HTTP calls | `Promise.all([...])` | `asyncio.gather(...)` | Goroutines + WaitGroup | `await http.getAll([urls])` |

## Key features for you

### HTTP client

The `http` module provides a one-call API for simple requests and a fluent
builder for more complex ones. Client calls return a `Response` object with
typed accessors.

Simple GET (the example uses a local server so it runs without network access):

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"id": 1, "name": "Ada"});
});
let addr = http.serverAddr(server);

let r = http.get("http://" + addr + "/users/1");
io.println(r.status());     /* 200 */
io.println(r.ok());         /* true for any 2xx */
io.println(r.json()["name"]);

http.close(server);
```

Output:

```
200
true
Ada
```

The request builder handles headers, query parameters, bearer tokens, timeouts,
and JSON bodies:

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"created": true});
});
let addr = http.serverAddr(server);
let token = "secret-token";

let r = http.request("http://" + addr + "/users")
    .withMethod("POST")
    .withQuery("format", "json")
    .withHeader("X-Trace", "abc")
    .withBearer(token)
    .withJson({"name": "Ada"})
    .withTimeout(5000)
    .send();

io.println(r.status());
http.close(server);
```

Output:

```
200
```

Transport failures (connection refused, DNS failure, timeout) throw `IOError`.
HTTP error statuses like 404 or 500 are returned as normal `Response` values;
check `r.ok()`, `r.isClientError()`, `r.isServerError()`, or `r.status()`
explicitly:

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.response("not found", 404);
});
let addr = http.serverAddr(server);

try {
    let r = http.get("http://127.0.0.1:19999/not-running");
    io.println(r.status());
} catch (IOError e) {
    io.println("connection failed: " + e.message);
}

let r2 = http.get("http://" + addr + "/missing");
io.println(r2.status());
io.println(r2.isNotFound());
io.println(r2.ok());

http.close(server);
```

Output:

```
connection failed: Get "http://127.0.0.1:19999/not-running": dial tcp 127.0.0.1:19999: connect: connection refused
404
true
false
```

A reusable client with a base URL and connection pool:

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"ok": true});
});
let addr = http.serverAddr(server);
let token = "secret-token";

let api = http.newClient({
    "baseUrl":   "http://" + addr,
    "timeoutMs": 5000,
    "headers":   {"Authorization": "Bearer " + token}
});

let users = api.get("/users");
io.println(users.status());
let me = api.get("/me");
io.println(me.status());

http.close(server);
```

Output:

```
200
200
```

See [stdlib/14-http-net.md](../stdlib/14-http-net.md) for the full client reference.

### HTTP server

`http.listen(addr, handler)` starts a server on a background goroutine and
returns a handle immediately. `http.serve` is the blocking variant.

The handler receives a plain `dict<string, any>` by default. Declare the
parameter as `Request` to get typed accessors; the router dispatches both forms
transparently:

```gb
import io;
import http;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    if (!req.isMethod("GET")) {
        return http.response("method not allowed", 405);
    }
    let name = req.query("name");
    if (name == null) {
        return http.jsonResponse({"error": "name param required"}, 400);
    }
    return http.jsonResponse({"hello": name});
});

let addr = http.serverAddr(server);

let r = http.get("http://" + addr + "/?name=Ada");
io.println(r.status());
io.println(r.json()["hello"]);

http.close(server);
```

Output:

```
200
Ada
```

Key `Request` accessors: `req.method`, `req.path`, `req.query("k")`,
`req.queryInt("page")`, `req.queryBool("debug")`, `req.header("Accept")`,
`req.cookie("sid")`, `req.text()`, `req.json()`, `req.routeParam("id")`,
`req.routeParams()`, `req.clientIp()`, `req.isJson()`.

The request body is automatically capped at a configurable size
(`maxBodyBytes`). Enable debug mode during development with the `debug: true`
server option or `GEBLANG_DEBUG=1` so handler panics return a stack trace to
the client.

For graceful shutdown:

```
/* fragment - graceful shutdown on SIGTERM */
import http;
import sys;

let server = http.listen(":8080", handler);
sys.onSignal("SIGTERM", func(string sig): void {
    http.shutdown(server, 10000);
});
http.wait(server);
```

See [stdlib/14-http-net.md](../stdlib/14-http-net.md) and
[11-web-development.md](../11-web-development.md).

### The web router

For multi-route applications use `web.router` and `web.http`. The router adds
path parameters, route groups, and middleware registration on top of the bare
`http.serve` contract.

```gb
import io;
import http;
import web.http as wh;
import web.router as router;

let app = router.newRouter();

/* Response middleware: runs after every handler */
router.use(app, func(dict<string, any> request, dict<string, any> response): dict<string, any> {
    return wh.withHeader(response, "X-Powered-By", "Geblang");
});

router.get(app, "/users/:id", func(dict<string, any> request): dict<string, any> {
    let ctx = wh.context(request);
    return wh.jsonStatus({"id": ctx.param("id")}, 200);
});

router.post(app, "/echo", func(dict<string, any> request): dict<string, any> {
    let ctx = wh.context(request);
    return wh.jsonStatus({"echoed": ctx.body()}, 200);
});

let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return router.handle(app, request);
});

let addr = http.serverAddr(server);

let r = http.get("http://" + addr + "/users/42");
io.println(r.status());
io.println(r.json()["id"]);
io.println(r.header("X-Powered-By"));

http.close(server);
```

Output:

```
200
42
Geblang
```

Route groups scope a set of routes under a shared prefix and can have their own
middleware:

```
/* fragment - route groups */
import web.router as router;

let api   = router.group(app, "/api");
let admin = router.group(app, "/admin");

router.before(admin, auth.requireRole(sessions, "admin"));
router.get(api, "/users/:id", showUser);
router.post(api, "/users", createUser);
```

A handler may also declare its parameters as `Request` and `Response` directly
(the same opt-in that `http.listen` supports):

```
/* fragment - rich Request in a router handler */
import web.router as router;
import http;

router.get(app, "/users/:id", func(Request req): Response {
    string id = req.routeParam("id") as string;
    return http.jsonResponse({"id": id, "ua": req.header("User-Agent")});
});
```

See [stdlib/15-web-router.md](../stdlib/15-web-router.md) and
[11-web-development.md](../11-web-development.md).

### JSON and data formats

The `json` module covers the full lifecycle: parse, stringify, typed
deserialization, streaming, and schema validation.

**Encode and decode:**

```gb
import io;
import json;

let payload = {"user": "Ada", "age": 37, "roles": ["admin", "editor"]};
let encoded = json.stringify(payload);
io.println(encoded);

let decoded = json.parse(encoded);
io.println(decoded["user"]);
io.println((decoded["age"] as int) + 1);
```

Output:

```
{"age":37,"roles":["admin","editor"],"user":"Ada"}
Ada
38
```

**Typed round-trip with classes:** `json.parseAs` reconstructs a class instance
from JSON. By default it matches JSON keys to constructor parameter names.
Classes control their serialized form with `__serialize()`:

```gb
import io;
import json;

class User {
    string name;
    int age;
    func User(string name, int age) {
        this.name = name;
        this.age = age;
    }
}

let u = User("Ada", 37);
let text = json.stringify(u);
io.println(text);

let u2 = json.parseAs(text, User);
io.println("${u2.name} is ${u2.age}");
```

Output:

```
{"age":37,"name":"Ada"}
Ada is 37
```

**Tolerant parse:** `json.tryParse` returns `null` on malformed input instead
of throwing, which is useful when handling untrusted bodies:

```
/* fragment - tolerant JSON parse */
import json;

let body = json.tryParse(rawText);
if (body == null) {
    /* respond with 400 - bad JSON */
}
```

See [stdlib/07-data-formats.md](../stdlib/07-data-formats.md).

### URL parsing and manipulation

`url.URL` parses and manipulates URLs. All `with*` methods return new values
rather than mutating in place, so a base URL can be safely reused:

```gb
import io;
import url;

let u = url.URL("https://api.example.com:8443/users/42?page=1&sort=asc#results");
io.println(u.scheme());
io.println(u.host());
io.println(u.port());
io.println(u.path());

let q = u.query();
io.println(q["page"]);

let encoded = url.encode("hello world");
io.println(encoded);

let api = url.joinPath("https://api.example.com", "v1", "users");
io.println(api);
```

Output:

```
https
api.example.com
8443
/users/42
1
hello+world
https://api.example.com/v1/users
```

See [stdlib/14-http-net.md](../stdlib/14-http-net.md#url-parsing-and-manipulation).

### Schema validation

Use `schema.validate` for JSON Schema-style validation of request bodies and
parsed data, or `web.validation` for a higher-level API that integrates with
the router:

```gb
import io;
import json;
import schema;

let requestSchema = {
    "type": "object",
    "required": ["name", "email"],
    "properties": {
        "name":  {"type": "string"},
        "email": {"type": "string"},
        "age":   {"type": "number", "minimum": 0, "maximum": 150}
    }
};

let good = json.parse('{"name":"Ada","email":"ada@example.com","age":37}');
let result = schema.validate(good, requestSchema);
io.println(result["valid"]);

let bad = json.parse('{"name":"Ada"}');
let result2 = schema.validate(bad, requestSchema);
io.println(result2["valid"]);
for (err in result2["errors"]) {
    io.println(err);
}
```

Output:

```
true
false
$.email: required field is missing
```

`web.validation` provides a fluent builder API designed for handler code and
returns validation errors in a format suitable for API responses:

```gb
import io;
import http;
import web.http as wh;
import web.router as router;
import web.validation as validation;

let app = router.newRouter();

let userRules = validation.object({
    "name": validation.stringField(),
    "age":  validation.intField()
}, ["name"]);

router.post(app, "/users", func(dict<string, any> request): dict<string, any> {
    let result = validation.json(request, userRules);
    if (!validation.isValid(result)) {
        return validation.errorResponse(result);
    }
    let data = validation.data(result);
    return wh.jsonCreated({"created": data["name"]});
});

let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return router.handle(app, request);
});

let addr = http.serverAddr(server);

let r1 = http.request("http://" + addr + "/users")
    .withMethod("POST")
    .withJson({"name": "Ada", "age": 37})
    .send();
io.println(r1.status());
io.println(r1.json()["created"]);

let r2 = http.request("http://" + addr + "/users")
    .withMethod("POST")
    .withJson({"age": 37})
    .send();
io.println(r2.status());

http.close(server);
```

Output:

```
201
Ada
422
```

See [stdlib/15-web-router.md](../stdlib/15-web-router.md#validation) and
[stdlib/07-data-formats.md](../stdlib/07-data-formats.md#schema-validation).

### Async parallel requests

`http.getAll` and `http.fetchAll` issue requests in parallel and return a list
of `Response` values in the same order as the input:

```gb
import io;
import http;
import async;

let server = http.listen("127.0.0.1:0", func(Request req): Response {
    return http.jsonResponse({"ok": true});
});
let addr = http.serverAddr(server);

let responses = await http.getAll([
    "http://" + addr + "/users",
    "http://" + addr + "/teams"
]);

for (r in responses) {
    if (r.isError()) {
        io.println("transport error: " + r.error());
    } else {
        io.println("status " + (r.status() as string) + " ok=" + (r.ok() as string));
    }
}

http.close(server);
```

Output:

```
status 200 ok=true
status 200 ok=true
```

For mixed request types, use `http.fetchAll` with request builder instances:

```
/* fragment - parallel mixed requests */
import http;
import async;

let createUser = http.request(usersUrl)
    .withMethod("POST")
    .withBearer(token)
    .withJson({"name": "Ada"});

let listTeams = http.request(teamsUrl).withBearer(token);

let responses = await http.fetchAll([createUser, listTeams]);
```

See [stdlib/14-http-net.md](../stdlib/14-http-net.md#parallel-requests) and
[09-async-generators.md](../09-async-generators.md).

### Concurrent request handling and shared state

The server spawns a goroutine per request. Handlers running concurrently share
whatever the handler closure captured, so any mutable value accessed from more
than one request needs protection.

The rule is simple: share read-only values (configuration, compiled route trees,
service handles) freely. For mutable counters or per-application state, use a
`store.Store` which provides atomic operations:

```
/* fragment - safe shared counter */
import http;
import web;
import store;

let app = web.new();
let hits = store.Store();

web.get(app, "/", func(Request req): Response {
    int n = hits.incr("home");
    return http.jsonResponse({"visits": n});
});

http.serve("127.0.0.1:8080", func(dict<string, any> request): dict<string, any> {
    return web.handle(app, request);
});
```

Do not mutate a plain `dict`, `list`, or `set` that is shared across requests.

See [11-web-development.md](../11-web-development.md#concurrency-and-shared-state).

## Gotchas

**`decimal` and `float` are distinct types.** A bare literal `2.5` is a `decimal`,
not an IEEE 754 float. The `f` suffix produces a float: `2.5f`. Mixing `decimal`
and `float` in arithmetic is a type error; cast one side with `as float` or
`as decimal`. This matters when you pull numeric values from parsed JSON and pass
them to functions that expect a specific numeric type.

**`/` returns `decimal`, not int.** `7 / 2` is `3.5`. Use `7 // 2` for integer
floor division. Assigning a division result to an `int` variable is a
compile-time error.

**HTTP error statuses are not exceptions.** A 404 or 500 from a server is a
normal `Response` value; only transport failures (connection refused, timeout,
DNS failure) throw `IOError`. Always check `r.ok()` or `r.status()` rather than
relying on a catch to detect server errors.

**Type-first parameter syntax.** Parameters are written `int n`, not `n: int`.
Return type follows the closing paren: `func handler(Request req): Response`.

**`"${x}"` for string interpolation.** Double-quoted strings interpolate with
`"${value}"`. Single-quoted strings do not interpolate.

**`parent()`, not `super`.** In subclass constructors, call the parent
constructor with `parent(args)`, not `super(args)`.

**Conditions must be explicit booleans.** `null`, `0`, and empty collections
are not falsy. Write `if (value != null)`, `if (n > 0)`,
`if (items.length() > 0)`.

**Do not mutate shared collections across requests.** A `dict`, `list`, or
`set` captured by the handler closure is shared by every concurrent request.
Use `store.Store` for mutable shared state; keep per-request state in local
variables.

**No `//` line comments.** Geblang uses `#` for line comments, `/* ... */` for
block comments, and `/** ... */` for doc blocks.

## Where to go next

- [Web development](../11-web-development.md) - routing, middleware, sessions,
  auth, SSE, WebSockets, SSR, and application layout
- [HTTP and networking](../stdlib/14-http-net.md) - full client/server reference,
  TLS, trusted proxies, concurrency limits, URL parsing
- [Web modules](../stdlib/15-web-router.md) - `web.router`, `web.http`,
  `web.validation`, `web.session`, `web.auth`, `web.middleware`, `web.sse`,
  `web.websocket`
- [Data formats](../stdlib/07-data-formats.md) - JSON, YAML, TOML, CSV,
  schema validation, streaming readers
- [Types](../03-types.md) - the type system, generics, union types, nullable
  values, casts
- [Async and generators](../09-async-generators.md) - tasks, `await`,
  `async.all`, `async.race`, generators
- [examples/http_client.gb](https://github.com/dwgebler/geblang/blob/main/examples/http_client.gb) - HTTP client
  patterns
- [examples/http_server.gb](https://github.com/dwgebler/geblang/blob/main/examples/http_server.gb) - server example
- [examples/source_web_router.gb](https://github.com/dwgebler/geblang/blob/main/examples/source_web_router.gb) -
  router with groups and middleware
- [examples/web.gb](https://github.com/dwgebler/geblang/blob/main/examples/web.gb) - native web module example
- [examples/json.gb](https://github.com/dwgebler/geblang/blob/main/examples/json.gb) - JSON encode, decode, streaming
