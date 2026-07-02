# Web Modules

The native `web` module provides a small router. Source modules split higher
level web application support by responsibility:

- `web.http`: request/response/context helpers, cookies, JSON, HTML, redirects.
- `web.router`: route groups, middleware registration, decorator mounting, and dispatch.
- `web.session`: signed cookie sessions, Redis/file/database session stores,
  and flash messages.
- `web.cache`: Redis/file/database cache stores.
- `web.auth`: login/logout/current user helpers, role/permission helpers, and CSRF.
- `web.validation`: request JSON/form validation and validation error responses.
- `web.forms`: SSR form binding, field errors, CSRF, and flash redirects.
- `web.middleware`: CORS, security headers, request IDs, and access logging.
- `web.sse`: server-sent event formatting and typed event-stream responses.
- `web.websocket`: WebSocket upgrades, clients, and typed connection helpers.
- `web.testing`: a small dispatch client and response predicates for route tests.

## Native `web`

- `new()`
- `route(app, method, path, handler)`, `get`, `post`
- `before(app, middleware)`, `use(app, middleware)`, `after(app, middleware)`
- `handle(app, request)`
- `withHeader(response, name, value)`

## Routing And HTTP Helpers

```gb
import http;
import web.http as wh;
import web.router as router;

let app = router.newRouter();

router.get(app, "/hello/:name", func(dict<string, any> request): dict<string, any> {
    let ctx = wh.context(request);
    return wh.jsonStatus({"hello": ctx.param("name")}, 200);
});

http.serve("127.0.0.1:8080", func(dict<string, any> request): dict<string, any> {
    return router.handle(app, request);
});
```

Use `web.http` for request builders and response helpers such as `request`,
`requestWithBody`, `context`, `jsonResponse`, `jsonStatus`, `jsonCreated`,
`jsonError`, `html`, `render`, `redirect`, `serveFile`, `normalize`,
`statusCode`, `body`, `header`, `withHeader`, `withCookieOptions`, and
`deleteCookie`.

### Serving files

`web.http.serveFile(path, opts?)` returns a response that streams a file
straight from disk. The file is not read into memory; the server sets
`Content-Length` and answers conditional and partial requests with the same
semantics a static file server would:

- `HEAD` returns the headers with an empty body.
- A `Range` request returns `206 Partial Content` with a `Content-Range`.
- A matching `If-None-Match` returns `304`. The server derives a default `ETag`
  from the file size and modification time when you set none, so conditional
  caching works out of the box; an `ETag` you set yourself is preserved.
- An `If-Modified-Since` at or after the file's modification time returns `304`.
- A missing or unreadable file returns `404` (the path is never disclosed).

```gb
router.get(app, "/downloads/{name}", func(dict<string, any> request): dict<string, any> {
    let name = ((request["params"] as dict<string, any>)["name"]) as string;
    return wh.serveFile("./downloads/" + name, {"contentType": "application/pdf"});
});
```

Options (all optional): `contentType` overrides the sniffed type, `name` sets
the download / sniff name (defaults to the base name), `headers` adds extra
response headers, and `status` sets the base status. Path safety is the
caller's responsibility; `serveFile` does not resolve `..` traversal, so
validate the path (for example with `path.real` and a root check) before
serving. The file marker `serveFile` returns is an opaque native value, so a
handler that echoes a parsed request body cannot forge a file response.

`Request`, `Response`, and `Context` are object wrappers over the same
dictionaries used by the native router. They are conveniences, not a separate
HTTP representation:

```gb
router.get(app, "/users/:id", func(dict<string, any> request): dict<string, any> {
    let req = wh.requestObject(request);
    return wh.responseObject(200, {"id": req.param("id")})
        .header("X-Route", "users.show")
        .toDict();
});
```

Response inspection helpers keep middleware readable:

```gb
router.use(app, func(dict<string, any> request, dict<string, any> response): dict<string, any> {
    if (wh.statusCode(response) >= 500) {
        return wh.withHeader(response, "X-Error", "server");
    }
    return response;
});
```

## Middleware Contracts

The router supports three middleware shapes:

- `router.before(app, middleware)`: middleware receives `(request)` and returns
  `null` to continue or a response-compatible value to stop dispatch.
- `router.use(app, middleware)`: middleware receives `(request, response)` and
  returns the transformed response.
- `router.after(app, middleware)`: alias for response middleware, useful when
  the application wants the intent to read as an after hook.

Route handlers receive `(request)` and may return a response dictionary, a
string, or `null`. `web.http.normalize(value)` applies the same normalization
rules used by the router: dictionaries pass through, strings become `200`
responses, and `null` becomes `204`.

### Rich `Request` / `Response` parameters

A handler or middleware can receive the rich `Request` object (the same one
`http.serve` builds, with `scheme()`, `host()`, `clientIp()`, `header()`,
`cookie()`, typed `query*` getters, `routeParam()` / `routeParams()`,
`isMethod`, `text`, `json`) simply by declaring its parameter type as `Request`
instead of `dict<string, any>`. An after-middleware whose second parameter is
typed `Response` receives a rich `Response` (status predicates, `header()`,
immutable `withStatus`/`withHeader`/`withBody`). Either form may return a
`Response`; the router serializes it.

```gb
router.get(app, "/users/:id", func(Request req): Response {
    string id = req.routeParam("id") as string;
    return http.jsonResponse({"id": id, "ua": req.header("User-Agent")});
});

router.after(app, func(Request req, Response resp): Response {
    return resp.withHeader("X-Path", req.path);
});
```

This is opt-in by parameter type: handlers and middleware typed
`dict<string, any>` keep receiving and returning dictionaries unchanged.
Matched path parameters are read by name with `req.routeParam("id")` (null when
absent) or all at once with `req.routeParams()`; the `dict<string, any>` form
reads the same values from the request `params` field.

## Validation

`web.validation` builds on `schema.validate` and keeps handler code focused on
request input rather than parsing details:

```gb
import web.validation as validation;

let userRules = validation.object({
    "name": validation.stringField(),
    "age": validation.numberField()
}, ["name"]);

router.post(app, "/users", func(dict<string, any> request): dict<string, any> {
    let result = validation.json(request, userRules);
    if (!validation.isValid(result)) {
        return validation.errorResponse(result);
    }
    let data = validation.data(result);
    return wh.jsonCreated({"name": data["name"]});
});
```

Use `validation.json(request, rules)` for JSON APIs,
`validation.form(request, rules)` for form posts, and
`validation.validate(data, rules)` for already parsed dictionaries. The helper
rule builders cover common cases: `object`, `stringField`, `intField`,
`numberField`, `boolField`, `arrayOf`, and `enumOf`.

## Forms

`web.forms` is for server-rendered forms. It binds URL-encoded form data,
validates with `web.validation`, exposes field-level errors, and provides
flash-friendly redirects:

```gb
import web.forms as forms;
import web.validation as validation;

let rules = validation.object({
    "name": validation.stringField()
}, ["name"]);

router.post(app, "/settings", func(dict<string, any> request): dict<string, any> {
    let result = forms.validate(request, rules);
    if (!forms.isValid(result)) {
        return wh.htmlStatus(forms.firstFieldError(result, "name"), 422);
    }
    return forms.redirectSuccess(sessionStore, request, "/settings", "Saved", {});
});
```

Use `forms.bind(request)` to read form data without validation,
`forms.fieldErrors(result, field)` for all field errors, and
`forms.withCsrf`/`forms.verifyCsrf`/`forms.csrfField` for CSRF workflows.

## Middleware

`web.middleware` contains reusable response middleware:

```gb
import log;
import web.middleware as middleware;

router.use(app, middleware.securityHeaders());
router.use(app, middleware.requestId());
router.use(app, middleware.cors("https://example.com", "GET, POST", "Content-Type, Authorization"));
router.use(app, middleware.accessLog(log.stdout()));
```

- `securityHeaders()` adds `X-Content-Type-Options`, `X-Frame-Options`, and
  `Referrer-Policy`.
- `headers(values)` adds custom static headers.
- `requestId()` propagates or creates `X-Request-ID`; `requestIdHeader(name)`
  uses a custom header name.
- `cors(origin, methods, headers)` and `corsCredentials(...)` add CORS
  response headers.
- `accessLog(logger)` logs method, path, and status through the `log` module.

## Server-Sent Events

`web.sse` formats SSE frames and response dictionaries:

```gb
import web.sse as sse;

router.get(app, "/events", func(dict<string, any> request): dict<string, any> {
    return sse.response([
        sse.comment("ready"),
        sse.named("ping", "ok"),
        sse.event("user.created", "{\"id\":42}", {"id": "42"}),
        sse.retry(5000)
    ]);
});
```

- `data(body)` formats a data-only frame.
- `named(name, body)` formats an event with an `event:` name.
- `event(name, body, options)` supports `id` and `retry` fields.
- `comment(text)` and `retry(milliseconds)` format utility frames.
- `response(frames)` and `responseText(body)` create finite
  `text/event-stream` responses with no-cache headers.
- `streaming(handler)` creates a long-lived streaming response. The handler
  receives an `EventStream`.
- `write(stream, frame)`, `flush(stream)`, and `close(stream)` control a live
  stream. `EventStream` also has `write`, `flush`, and `close` methods.

Example:

```gb
router.get(app, "/live", func(dict<string, any> request): dict<string, any> {
    return sse.streaming(func(sse.EventStream stream): void {
        stream.write(sse.comment("connected"));
        stream.flush();
    });
});
```

## WebSockets

`web.websocket` wraps the native `websocket` module for applications built on
`web.router`. Use `upgrade(handler)` in a route to accept a WebSocket
connection. The handler receives a `Connection` and can exchange text,
bytes, or JSON messages:

```gb
import web.websocket as ws;

router.get(app, "/ws", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(ws.Connection conn): void {
        let message = conn.readJson();
        conn.sendJson({"echo": message["text"]});
        conn.close();
    });
});
```

Client helpers use the same module:

```gb
let conn = ws.connect("ws://127.0.0.1:8080/ws");
conn.sendText("hello");
let reply = conn.readText();
conn.close();
```

Available helpers are `upgrade`, `upgradeWithHeaders`, `connect`,
`connectWithHeaders`, `sendText`, `readText`, `sendJson`, `readJson`,
`sendBytes`, `readBytes`, `close`, and `echoText`. The free functions accept a
`Connection`; the same operations are also available as `Connection` methods.

## Sessions, Auth, Cache

```gb
import web.auth as auth;
import web.cache as cache;
import web.http as wh;
import web.session as session;

let sessions = session.fileSessionStore("/tmp/app-sessions", 3600);
let response = auth.login(sessions, wh.text("ok"), {"name": "Ada"}, {"httpOnly": true});
let cacheStore = cache.fileCacheStore("/tmp/app-cache", 3600);
```

Session stores are available for Redis, files, and SQL databases. The file
store requires a `geb_sid` cookie to match the 64-character lowercase-hex id
shape it generates before deriving a filesystem path from it; a cookie that
doesn't match is treated as no session rather than read or deleted.

### Cache: `web.cache`

Import `web.cache` to cache expensive lookups between requests. Three store
implementations share the same interface:

| Constructor | Description |
|-------------|-------------|
| `cache.memoryCacheStore(ttl)` | In-process store; fast, no dependency, cleared on restart |
| `cache.fileCacheStore(directory, ttl)` | File-backed store; `ttl` is in seconds |
| `cache.redisCacheStore(client, prefix, ttl)` | Redis-backed; `client` from the `redis` module |
| `cache.databaseCacheStore(conn, table, ttl)` | SQL-backed; call `.install()` once to create the table |

All stores expose `get(name)`, `set(name, value)`, `delete(name)`, and
`has(name)`.

```gb
import web.cache as cache;
import io;

let store = cache.fileCacheStore("/tmp/app-cache", 300);

func expensiveLookup(string key): string {
    if (store.has(key)) {
        return store.get(key) as string;
    }
    let result = "computed:" + key;
    store.set(key, result);
    return result;
}

io.println(expensiveLookup("user:1")); # computed:user:1 (first call)
io.println(expensiveLookup("user:1")); # computed:user:1 (from cache)
```

For Redis or database stores, pass a client handle from the appropriate module:

```gb
import redis;
import web.cache as cache;

let client = redis.connect("redis://127.0.0.1:6379");
let store = cache.redisCacheStore(client, "myapp:", 3600);
```

`web.auth` guard helpers return callable middleware:

```gb
router.before(app, auth.requireAuth(sessions));
router.before(admin, auth.requireRole(sessions, "admin"));
router.before(editor, auth.requirePermission(sessions, "posts.edit"));
router.before(account, auth.requireLogin(sessions, "/login"));
```

`requireAuth` returns a `401` JSON error for anonymous requests, `requireRole`
and `requirePermission` return `403` when the current user lacks the required
claim, and `requireLogin` redirects anonymous requests to the supplied path.
Use `currentUser`, `isAuthenticated`, `userHasRole`, and `userHasPermission`
when an application needs custom policy logic.

## Decorator Mounting

```gb
import web.http as wh;
import web.router as router;
import web.session as session;

let sessions = session.fileSessionStore("/tmp/app-sessions", 3600);
let app = router.newRouter();
let api = router.group(app, "/api");

class AdminController {
    @loginRequired
    @isGranted("admin")
    @route("POST", "/users")
    func create(dict<string, any> request): dict<string, any> {
        return wh.jsonCreated({"ok": true});
    }
}

router.mountWithOptions(api, AdminController(), {"sessionStore": sessions});
```

## Route Testing

```gb
import web.testing as webtest;

let client = webtest.client(app);
let response = client.get("/api/users");

io.println(webtest.hasStatus(response, 200));
```
