# Geblang

Geblang is a statically-typed, general-purpose scripting language implemented in
Go. Current version: **1.34.0**. It combines the ergonomics of PHP and Python
with strong static typing, generics, decorators, async, and runtime reflection.

If you are coming from:

- **PHP**: the same curly-brace, class-based, web-first feel, now with always-on
  static typing, real reified generics, interfaces, and overloading. You deploy
  one binary instead of provisioning a runtime and its extensions.
- **Python**: the `dict`, `list`, and `set` literals, comprehensions, decorators,
  dunder methods, and REPL you already know, now without the GIL, so CPU-bound
  work scales across cores, and with generics and type checks that survive to
  runtime rather than staying hints.
- **Node or TypeScript**: the `async` / `await` you already use, but running on
  goroutines for genuine parallelism instead of one event-loop thread. Types are
  enforced at runtime, not erased by the compiler, and a project ships as one
  self-contained binary instead of a `node_modules` tree.

What you get either way:

- Non-null by default. `null` is opt-in with `?T`, so an entire class of
  null-reference bugs cannot get past the type checker.
- Generics and interfaces enforced both statically and at runtime;
  `instanceof list<int>` actually works.
- Pattern matching, a pipe operator, decimal numbers for exact money math, and
  arbitrary-precision integers with no special type or syntax.
- One toolchain that ships together: a static type checker, formatter, test
  runner and framework, language server, and VS Code extension. Nothing to
  assemble from separate linter, formatter, test, and type-check packages.
- A rich standard library covering HTTP server and client, WebSockets, SQLite /
  PostgreSQL / MySQL, Redis, message brokers, crypto and JWT, JSON / YAML / TOML
  / XML, templating, math, and more, so most programs need no third-party
  dependencies at all. Gebweb, a typed web framework, builds on top of it.
- `geblang build` produces a single self-contained binary with the runtime
  bundled in, and `scripts/cross-build.sh` cross-compiles it for any Linux,
  macOS, or Windows target. There is nothing else to install on the target: no
  separate interpreter, no virtualenv, no `node_modules`.

One language that reads like the scripting you already know and deploys like a Go
binary.

> See [Gebweb](https://github.com/dwgebler/gebweb) for the accompanying web and
> API framework built on top of Geblang.

## Quick overview

Geblang compiles to bytecode for a stack VM, with a tree-walking interpreter as
a fallback for anything the VM can't yet build (full parity is the goal) and
on-disk bytecode caching. It aims for the same ergonomics and gentle learning
curve as PHP and Python, but with the static guarantees those dynamic languages
struggle to support.

- Static typing with inference, plus an `any` escape hatch. Type errors are caught before execution by run/test/build, and both backends agree on what they reject.
- Reified generics; `instanceof list<int>` works at runtime.
- Modules and packages: manifest, lockfile, path- and git-based dependencies.
- Single inheritance + multiple interfaces, with operator overloading via Python-style dunder methods.
- Decorators / aspect-oriented programming: decorators can be either callable wrappers or pure metadata (a mix of PHP attributes and Python decorators).
- async/await and generators backed by goroutines (true concurrency, not a single-threaded event loop), with structured task groups and `async.race` / `async.all` / `async.timeout`.
- Pattern matching (`match`) and destructuring.
- Functional helpers on collections: `xs.map(f)`, `xs.filter(p)`, `xs.reduce(init, f)`, `xs.sortBy(key)`, `collections.maxBy` / `groupBy` / `chunk`.
- Runtime reflection over classes, functions, fields, decorators, and modules.
- Context managers (`with`), immutability (`@immutable`), null-coalescing (`??`), nullable types (`?T`), string interpolation.
- A large standard library oriented towards (but not limited to) web development and APIs.
- Built-in tooling: REPL, test runner and framework, static analysis, formatter, LSP + DAP, and a VS Code extension.

## Documentation

The full language and stdlib reference is in
[docs/user/](docs/user/) - start with
[01-getting-started.md](docs/user/01-getting-started.md). The
release notes are at
[docs/user/18-release-notes.md](docs/user/18-release-notes.md).
The same reference is rendered as a browsable HTML site at
[geblang.com/docs](https://geblang.com/docs).

## Install

Pre-built binaries for Linux, macOS, and Windows are attached to each
[GitHub release](https://github.com/dwgebler/geblang/releases/latest). Download
the archive for your platform, extract it, and put `geblang` on your `PATH`. The
[downloads page](https://geblang.com/downloads) lists per-platform links and
install steps.

To build from source instead:

```sh
make build              # produces ./geblang
make docs               # builds the static reference site
make docker-build       # produces ./build/{geblang,stdlib} via Docker
```

After `make docker-build`, the binary at `./build/geblang` runs
standalone; if you relocate it away from the bundled `stdlib/`,
set `GEBLANG_STDLIB` to point at the moved directory.

## Example

Naive vs `@memoize`d recursive Fibonacci, timed with the `profiler` timer context manager:

```gb
import profiler;
import io;

func fibNaive(int n): int {
    if (n < 2) { return n; }
    return fibNaive(n - 1) + fibNaive(n - 2);
}

@memoize
func fibMemo(int n): int {
    if (n < 2) { return n; }
    return fibMemo(n - 1) + fibMemo(n - 2);
}

let n = 32;

/* The with-binding does not escape the block, so pre-declare the timer and
 * the result, assign inside, and read elapsed afterwards. */
let naive = 0;
let tNaive = profiler.timer();
with (tNaive) {
    naive = fibNaive(n);
}

let memo = 0;
let tMemo = profiler.timer();
with (tMemo) {
    memo = fibMemo(n);
}

io.println("fib(${n})");
io.println("  naive recursion : ${naive} in ${tNaive.elapsedMs()} ms");
io.println("  @memoize        : ${memo} in ${tMemo.elapsedMs()} ms");
io.println("  speedup         : ${tNaive.elapsedMs() / tMemo.elapsedMs()}x");
```

Output:

```
fib(32)
  naive recursion : 2178309 in 324.027586 ms
  @memoize        : 2178309 in 0.108335 ms
  speedup         : 2990.977855725296x
```

A smaller example:

```gb
import io;

class Greeter {
    string name;

    func Greeter(string name) {
        this.name = name;
    }

    func greet(): string {
        return "Hello, " + this.name;
    }
}

Greeter g = Greeter("world");
io.println(g.greet());
```

Types are checked statically. `any` is opt-in for dynamic boundaries
(parsed JSON, untyped foreign data); most code is typed end-to-end.

## Benchmarks

A few simple benchmarks against Python, PHP, and Node, all run on the same
machine. Geblang holds its own across these everyday workloads, performing
competitively with the general-purpose dynamic languages it stands alongside.

```
case            language   median ms   min ms     max ms     output
--------------  ---------  ----------  ---------  ---------  ----------------
numeric_loop    geblang            71         70         77  666665000000
numeric_loop    python            125        122        126  666665000000
numeric_loop    php                28         27         37  666665000000
numeric_loop    node               28         26         29  666665000000
recursive_fib   geblang            57         54         67  317811
recursive_fib   python             36         35         37  317811
recursive_fib   php                22         22         23  317811
recursive_fib   node               25         24         27  317811
list_pipeline   geblang            14         14         15  2497500
list_pipeline   python             15         14         17  2497500
list_pipeline   php                10         10         11  2497500
list_pipeline   node               22         22         23  2497500
string_concat   geblang            13         12         13  25714
string_concat   python             19         18         19  25714
string_concat   php                11         11         12  25714
string_concat   node               23         23         26  25714
dict_ops        geblang            22         22         25  49995000
dict_ops        python             18         17         19  49995000
dict_ops        php                12         12         13  49995000
dict_ops        node               28         26         30  49995000
class_dispatch  geblang            25         24         27  2497475000
class_dispatch  python             19         18         21  2497475000
class_dispatch  php                12         12         13  2497475000
class_dispatch  node               24         23         25  2497475000
regex_match     geblang            36         35         37  66666
regex_match     python             42         41         45  66666
regex_match     php                15         15         15  66666
regex_match     node               26         25         26  66666
json_roundtrip  geblang           364        362        420  284013
json_roundtrip  python            479        469        496  284013
json_roundtrip  php               298        296        315  284013
json_roundtrip  node              263        258        268  284013
list_functional  geblang            18         17         20  166616670000
list_functional  python             16         16         18  166616670000
list_functional  php                12         12         13  166616670000
list_functional  node               24         23         24  166616670000
```

## Highlights

- **Static typing** with generics, unions (`T | U`), intersections
  (`T & U`), nullable types (`?T`), and explicit type assertions.
  `string`, `int`, `decimal`, `bool`, `bytes`, `list<T>`,
  `dict<K, V>`, `set<T>` are first-class.
- **Classes** with single inheritance and interfaces, decorators
  (callable + metadata), pattern matching, enums (with payloads
  and destructuring), and runtime reflection.
- **Concurrency** backed by goroutines. `async` functions, `await`,
  and generators run as real tasks on the Go scheduler, so CPU-bound
  work scales across cores instead of serializing on one event loop,
  with structured task groups (`async.scope`) and `async.race` /
  `async.all` / `async.timeout` combinators. `http.serve`,
  `http.listen`, and `net.serve` follow Go's standard server model:
  each request runs on its own goroutine, so a server fields many
  requests in parallel with no extra code. A handler passed directly
  to one of those server functions is isolated per request on both
  backends. Router and framework dispatch instead share the application
  graph across requests; shared mutable values must use `store.Store`
  or another concurrency-safe handle. An optional bounded-concurrency
  pool (`opts.maxConcurrent`,
  `opts.queueSize`, `opts.onOverload`) caps in-flight handlers and
  applies backpressure on overload: reject with 503, queue and wait,
  or drop the connection.
- **FFI** for calling C-ABI shared libraries directly (libtorch,
  libsqlite, libcurl, ...). In-process dispatch with no IPC;
  capability-gated default-off.
- **Bytecode VM** with a tree-walking evaluator as the reference
  semantics and fallback. Compiled bytecode is cached on disk by
  source hash so subsequent runs skip parse and compile.
- **Standard library**: HTTP server / client, WebSocket,
  TCP / UDP, SQLite / PostgreSQL / MySQL, Redis, SMTP / IMAP,
  RabbitMQ / Kafka / SQS / STOMP, file + streaming I/O, archives,
  process management, schedulers, OTLP traces, Prometheus metrics,
  JSON / YAML / TOML / XML / Markdown, regex, crypto (HMAC,
  Argon2id, bcrypt, JWT, AES-GCM, XChaCha20-Poly1305, RSA / EC /
  Ed25519), pathlib, math, and a Twig-style template engine.
- **Tooling**: `geblang` CLI (run / repl / check / test / fmt /
  init / install / build / doctor / cache), VS Code extension with
  LSP + DAP + test explorer, Dockerised reproducible build,
  single-binary bundling via `geblang build --out`.

## Docker

The official image ships the CLI and stdlib, so you can run Geblang
without installing anything:

```sh
docker pull dwgebler/geblang

# Run a script from the current directory (mounted at /app):
docker run --rm -v "$PWD":/app dwgebler/geblang hello.gb

# Start a REPL:
docker run --rm -it dwgebler/geblang repl

# Pin a version instead of latest:
docker run --rm -v "$PWD":/app dwgebler/geblang:1.21.0 hello.gb
```

The entrypoint is `geblang`, so anything after the image name is passed
straight to the CLI (`run`, `test`, `build`, `fmt`, ...). The working
directory is `/app`; mount your project there. The image is built on a
glibc base, so FFI (`dlopen`) works for shared libraries you install or
mount in.

For a containerised application, base your image on it:

```dockerfile
FROM dwgebler/geblang:latest
WORKDIR /app
COPY . /app
CMD ["main.gb"]
```

## Quickstart

### Hello world

```gb
# Geblang line comments use `#`. Block comments use `/* ... */`.
import io;

string name = "world";
io.println("Hello, " + name);
```

```sh
geblang hello.gb
```

Double-quoted strings evaluate escape sequences. Single-quoted
strings keep backslashes literal. `//` is integer division, not a
comment.

### Enums, pattern matching, typed methods

```gb
import io;

enum Shape {
    Circle(decimal),
    Rect(decimal, decimal),
    Triangle(decimal, decimal),
}

func area(Shape s): decimal {
    return match (s) {
        case Shape.Circle(decimal r)            => 3.14159 * r * r;
        case Shape.Rect(decimal w, decimal h)   => w * h;
        case Shape.Triangle(decimal b, decimal h) => 0.5 * b * h;
    };
}

list<Shape> shapes = [
    Shape.Circle(5.0),
    Shape.Rect(3.0, 4.0),
    Shape.Triangle(6.0, 2.5),
];

for (Shape s in shapes) {
    io.println(area(s));
}
```

Enums can also be backed by scalar values:

```gb
enum Status: string { Active = "active"; Closed = "closed"; }

io.println(Status.Active.value);      # active
io.println(Status.from("closed"));    # Status.Closed
```

Tagged enums destructure inside `match`, so the payload binds with
its declared type. Missing variants fail static analysis at
`geblang check` time.

### Generics + collections

```gb
import io;

class Repository<T> {
    list<T> items;

    func Repository() {
        this.items = [];
    }

    func add(T item): Repository<T> {
        this.items = this.items.push(item);
        return this;
    }

    func count(): int {
        return this.items.length();
    }
}

class User {
    string name;
    int age;
    func User(string name, int age) {
        this.name = name;
        this.age = age;
    }
}

Repository<User> users = Repository<User>();
users.add(User("Ada", 36)).add(User("Carla", 41));

io.println("count: " + (users.count() as string));
for (User u in users.items) {
    io.println(u.name + " (" + (u.age as string) + ")");
}
```

`list<T>.push` returns a new list, so type-safe chaining works
naturally. `dict<K, V>` and `set<T>` are generic in the same way.
Pass a `callable` parameter for predicate / mapper functions.

### Collection and string ergonomics

```gb
import io;

list<int> nums = [3, 1, 4, 1, 5, 9, 2, 6];

# enumerate() pairs each element with its index.
for (i, n in nums.enumerate()) {
    io.println((i as string) + " -> " + (n as string));
}

# Functional pipeline: flatMap, sliding windows, running scan.
list<int> spread = [1, 2, 3].flatMap(func(int x): list<int> {
    return [x, x * 10];
});
list<int> sums = [1, 2, 3, 4].scan(0, func(int acc, int x): int {
    return acc + x;
});
io.println("${spread}");           # [1, 10, 2, 20, 3, 30]
io.println("${nums.windowed(3)}"); # 3-wide sliding windows
io.println("${sums}");             # [0, 1, 3, 6, 10]

# String helpers.
io.println("the quick brown fox".title());   # The Quick Brown Fox
io.println("  ".isBlank() as string);        # true
```

Lists carry a functional toolkit (`map`, `filter`, `flatMap`,
`windowed`, `scan`, `takeWhile`, `uniqueBy`, ...) available both as
methods and `collections` module functions. Strings gain `title`,
`capitalize`, `lines`, `removePrefix` / `removeSuffix`, and
case-insensitive comparisons. `enumerate()` gives lists the same
indexed iteration that dicts already have with `for k, v in d`.

### Multiple return values

```gb
import io;

func divmod(int a, int b): list<int> {
    return a // b, a % b;
}

let q, r = divmod(17, 5);
io.println("17 = 5 * " + (q as string) + " + " + (r as string));   # 17 = 5 * 3 + 2

int x = 1;
int y = 2;
x, y = y, x;   # swap, no temporary
```

A function returns several values with `return a, b`, and the caller
unpacks them with `let a, b = f()` or `a, b = f()`. The values travel
as a `list`, so the declared return type is `list<T>`, and the swap
idiom `a, b = b, a` needs no temporary.

### Dict-like objects and the `in` operator

```gb
import io;

# `in` tests membership across lists, dicts, sets, strings, and ranges.
io.println("pear" in ["apple", "pear", "plum"]);   # true
io.println(7 in (1..10));                           # true (a range literal needs parentheses)

# A class becomes dict-like by implementing the subscript magic methods.
class Headers {
    dict<string, string> values;

    func Headers() {
        this.values = {};
    }

    func __setIndex(string key, string value): void {
        this.values[key.lower()] = value;
    }

    func __index(string key): ?string {
        return this.values.get(key.lower());
    }

    func __contains(string key): bool {
        return this.values.contains(key.lower());
    }
}

Headers h = Headers();
h["Content-Type"] = "application/json";
io.println(h["content-type"] as string);   # application/json (case-insensitive)
io.println("Content-Type" in h);           # true
```

The `in` operator works for every built-in container and dispatches to
`__contains` on user types. Implementing `__index` / `__setIndex` /
`__contains` lets a class support `obj[key]`, `obj[key] = value`, and
`key in obj` like a native dict; the `maps.DictInterface` stdlib
interface supplies `get`, `keys`, `values`, and friends as defaults.

### File I/O with typed streams

```gb
import datetime;
import streams;

streams.IOStream src = streams.open("/etc/hostname", "r");
string hostname = src.readLine() as string;
src.close();

streams.IOStream out = streams.open("/tmp/report.txt", "w");
out.writeln("hostname: " + hostname);
out.writeln("processed at: " + datetime.nowInstant().formatRFC3339());
out.close();
```

`streams.IOStream` is the typed wrapper around files, stdin /
stdout / stderr, in-memory buffers, sockets, and pipes; every
read returns `?string` or `?bytes` so the type system forces an
EOF check at the call site.

### Async + structured concurrency

```gb
import async;
import async.scope as scope;
import io;
import time;

async func fetchPrice(string symbol, int ms): decimal {
    await time.sleep(ms);
    return symbol.length().toDecimal() * 100.0;
}

let prices = scope.scope(func(scope.TaskGroup g): list<decimal> {
    let a = g.spawn(func(): decimal {
        return async.await(fetchPrice("AAPL", 50)) as decimal;
    });
    let b = g.spawn(func(): decimal {
        return async.await(fetchPrice("GOOG", 30)) as decimal;
    });
    return [
        async.await(a) as decimal,
        async.await(b) as decimal,
    ];
}) as list<decimal>;

for (decimal p in prices) {
    io.println(p.toString(2));
}
```

Children spawned inside `scope.scope` are awaited at scope exit.
A failing child cancels its siblings and rethrows after the
group drains - no orphan goroutines. `async.race`, `async.all`,
and `async.timeout` provide single-shot variants for simpler
cases.

### TCP echo server

```gb
import io;
import sockets;
import sys;

sockets.Listener server = sockets.serve("127.0.0.1", 9000,
    func(sockets.Socket conn): void {
        for (line in conn) {
            conn.writeln("echo: " + (line as string));
        }
        conn.close();
    });

io.println("listening on " + server.localAddr());
sys.sleep(60000);   /* serve for one minute */
server.close();
```

The handler receives a typed `sockets.Socket`: `for (line in conn)`
yields inbound lines, and `writeln` / `readLine` / `close` round out
the connection. No `dict<string, any>` boundary in sight.

### FFI: calling C-ABI shared libraries

```gb
import ffi;
import io;

let lib = ffi.dlopen("libm.so.6");
let sin = lib.symbol("sin", [ffi.DOUBLE], ffi.DOUBLE);
let hypot = lib.symbol("hypot", [ffi.DOUBLE, ffi.DOUBLE], ffi.DOUBLE);

io.println(sin(1.5707963267948966));   /* 1.0 */
io.println(hypot(3.0, 4.0));           /* 5.0 */
lib.close();
```

Run with `geblang --allow-ffi 'libm.so.*' script.gb`, or declare
the allow-list in `geblang.yaml` under `permissions.ffi`. The
dispatch is in-process - no IPC overhead - and covers primitive
integers, floats, pointers, C strings, and bytes. Use it for
numeric kernels and library bindings; for sandboxed extensions,
the subprocess `ext` protocol is the better fit.

### HTTP client

```gb
import http;
import io;

# A single typed request: the Response carries readers and status predicates.
let res = http.get("https://api.example.com/status");
if (res.ok()) {
    dict<string, any> body = res.json() as dict<string, any>;
    io.println("ok: ${body.keys()}");
} else if (res.isNotFound()) {
    io.println("not found");
}

# The immutable request builder composes a call without leaking state.
let api = http.request("https://api.example.com/me")
    .withBearer("token-123")
    .withHeader("Accept", "application/json")
    .withTimeout(5000);

let me = api.send();
io.println("status: " + (me.status() as string));
```

`http.get` / `http.post` / `http.request(url, spec)` return a rich
`Response` with `status()`, `ok()`, `text()`, `json()`, `header(name)`,
and predicates like `isSuccessful()` / `isNotFound()`. Called with a
single argument, `http.request(url)` starts an immutable builder whose
`withX` methods each return a fresh builder, so a base request can be
reused without leaking state. `http.getAll` and `http.fetchAll` run a
batch of requests in parallel.

### HTTP server (rich Request / Response)

A route handler opts into the typed `Request` and `Response` objects
just by declaring those parameter types. `Request` has clean accessors
(`routeParam`, `query`, `header`, `cookie`, `json`, `clientIp`, ...) and
`http.jsonResponse` / `http.response` build a `Response`:

```gb
import http;
import web.router as router;

class User {
    int id;
    string name;
    func User(int id, string name) {
        this.id = id;
        this.name = name;
    }
}

dict<int, User> store = {1: User(1, "Ada"), 2: User(2, "Carla")};

let app = router.newRouter();

router.get(app, "/users/:id", func(Request req): Response {
    int id = (req.routeParam("id") as string).toInt();
    if (!store.contains(id)) {
        return http.jsonResponse({"error": "not found"}, 404);
    }
    User u = store[id];
    return http.jsonResponse({"id": u.id, "name": u.name});
});

http.serve("127.0.0.1:8080", func(dict<string, any> req): dict<string, any> {
    return router.handle(app, req);
});
```

Handlers typed `dict<string, any>` still work, so adoption is
incremental. For larger typed-binding apps with controllers,
validation, and DI, see the `web.router` and `web.middleware`
chapters in the manual.

## CLI

```sh
geblang script.gb                    # run a script (VM by default)
geblang --disable-vm script.gb       # run via evaluator
geblang repl                         # interactive REPL
geblang check script.gb              # static analysis (no execution)
geblang check --strict path/         # treat warnings as errors
geblang test tests/                  # run *_test.gb files
geblang fmt path/                    # canonical formatter
geblang init --name acme.tools       # scaffold a new project
geblang doctor                       # environment + toolchain report
geblang build --out app              # bundle script into one binary
geblang -m http.server 8080          # run a stdlib module's main()
geblang --version
geblang --help
```

## Run tests

```sh
make test                            # full Go test suite
geblang test examples/sample_test.gb # run a Geblang test file
geblang test --tag fast examples     # filter by @tag
```

## Claude Code skill

This repo ships a Claude Code skill that equips Claude to write, run, test,
check, format, build, and bundle Geblang correctly: the idioms, the
cross-language gotchas (`//` is integer division, `parent()` not `super`,
decimal-default literals), the standard-library surface, and the `geblang` CLI
workflow.

It lives at [`.claude/skills/geblang/`](.claude/skills/geblang/). Claude Code
loads it automatically as a project skill when you work inside a checkout of
this repo. To use it in any project, copy it into your user skills:

```sh
cp -r .claude/skills/geblang ~/.claude/skills/geblang
```

## Project layout

- `cmd/geblang/` - CLI and REPL.
- `cmd/docsite/` - static reference manual generator.
- `internal/` - lexer, parser, semantic analyzer, AST,
  evaluator, bytecode compiler + VM, runtime, LSP, DAP,
  transpiler.
- `stdlib/` - source-distributed Geblang stdlib (loaded at
  runtime alongside the binary).
- `internal/native/` - pure stdlib functions shared by
  evaluator and VM.
- `internal/evaluator/` - tree-walking evaluator and stateful
  module implementations.
- `internal/bytecode/` - bytecode compiler, VM, format,
  parity tests.
- `docs/user/` - user reference manual (built into a static
  site by `make docs`).
- `examples/` - runnable examples for public APIs.
- `vscode-geblang/` - VS Code extension (syntax, LSP client,
  DAP, test explorer).

## License

MIT - see [LICENSE](LICENSE).
