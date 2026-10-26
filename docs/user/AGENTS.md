# AGENTS.md: Geblang for AI agents

Dense cheatsheet for AI coding agents writing Geblang. Read once
at the start of a session.

## What Geblang is

A statically-typed scripting language for backend services,
implemented in Go. Files end in `.gb`. Two backends share the
language: an evaluator (`geblang test`) and a bytecode VM
(`geblang run`, `geblang build`). They must produce identical
output; report a divergence as a bug rather than working around
it.

## Project layout

A package has a `geblang.yaml` manifest:

```yaml
name: myapp
version: 0.1.0
source: src
paths: []
dependencies:
  somelib:
    path: ../somelib
```

Source files live under `src/` (whatever `source` points at).
Tests are `*_test.gb` files next to source.

```
geblang run main.gb              # run a script
geblang test tests/              # run *_test.gb under a path
geblang check src/               # static check (no execution)
geblang fmt src/                 # format in place (--clean, --strip-comments)
geblang doc src/                 # print signatures for a file or dir
geblang build --entry main --out app <package-dir>   # add --native or --docker
```

`geblang build` takes a MODULE name for `--entry` (the module must
export `func main(list<string> args)`), not a file path.

## Syntax

### Comments

```gb
# line comment
io.println(x);  # trailing line comment
/* block comment
   on multiple lines */
```

`//` is integer division, not a comment. Never use `//` for
comments anywhere.

### Imports and modules

```gb
import io;
import datetime as dt;              # alias
from crypt import passwordHash;     # selective
from bytes import toHex as hex;     # selective with alias
```

Imports go at file top. Stdlib modules don't need a path. Local
modules resolve through the manifest's `paths` or `dependencies`.

A module file starts with `module name;` and exports its public
surface explicitly; everything else is private to the module:

```gb
module app.users;

func normalizeId(string id): string { ... }    # private

export func findUser(string id): ?User { ... } # public
export const int MAX_PAGE = 100;
export class User { ... }
```

### Variables

```gb
let x = 1;                   # inferred (int)
int  y = 2;                  # explicit
?string maybe = null;        # nullable
const PI = 3.14;             # immutable; PI is decimal
```

`let x = 1.0` infers `decimal`, not `float`. Use `1.0f` or
`1.0 as float` when you specifically want the float type.
`decimal` is the default for floating-point literals because
money and precise arithmetic are more common than fast IEEE math.
Division `/` is true division and yields `decimal` (or `float`),
so `int n = a / b` is a compile error; use `//` for the integer
(floor) quotient or `(a / b) as int` to truncate.

### Primitive types

`int`, `float`, `decimal`, `string`, `bool`, `bytes`, `any`,
`null`. Integers are arbitrary precision (no overflow). Strings
are UTF-8 and interpolate inside double quotes:

```gb
let name = "Ada";
io.println("hello ${name}");    # interp
io.println('hello ${name}');    # literal (single quotes are raw)
io.println("${3.14159:.2f}");   # format spec -> 3.14
io.println("${42:04d}");        # -> 0042
```

### Collections

```gb
list<int>          xs = [1, 2, 3];      # int[] is an alias
dict<string, int>  d  = {"a": 1, "b": 2};
set<int>           s  = {1, 2, 3};      # brace literal w/o ':' is a set
```

- **List mutators work in place** (1.16.0+): `push`, `pop`,
  `prepend`, `insert`, `removeAt`, `remove`, `reverse`, `sort`,
  `sortBy` mutate the receiver AND return it (chainable).
  `sorted()`, `reversed()`, `copy()`, `deepCopy()` are the copy
  variants.
- **Dicts iterate in insertion order**, including `.keys()`,
  `.values()`, `.items()`, and `for (k, v in d)`.
- A bare `{}` is a dict; `set<int> s = {};` (a set-typed variable, parameter
  default or field) is an empty set.
- `type` is a keyword (type aliases: `type UserId = string;`); don't use it as
  a variable or parameter name.
- Slicing is Python-style on lists and strings: `xs[1:3]`,
  `xs[:2]`, `s[-3:]`. Negative indexes count from the end.
- Comprehensions: `[x * x for x in 1..5]`,
  `[x for x in xs if x > 0]`, `{k: v * 2 for k, v in d}`.
- `instanceof list<T>` works at runtime (reified generics).
- `xs.search(v)` / `xs.search(pred)` / `s.searchPattern(re)` return every
  matching locator (list indices, dict keys, string positions); `xs.fill(v, n)`
  appends n copies in place.
- Numeric-check predicates test before converting: `s.isInt()` / `s.isDecimal()`
  / `s.isNumeric()` on strings; `f.isInt()` / `d.isInt()` on float / decimal.

### Ranges

```gb
for (i in 1..5)   { }     # inclusive: 1,2,3,4,5
for (i in 0..<10) { }     # exclusive end
let xs = (1..5).toList();
range(1, 9, 2);           # builtin -> [1, 3, 5, 7, 9] (inclusive)
zrange(0, 5);             # exclusive, Python-style -> [0,1,2,3,4]; zrange(n) from 0
r.length; r.first; r.last; r.contains(3);
```

### Control flow

```gb
if (cond) { } else if (other) { } else { }
for (x in xs) { }
for (k, v in d) { }
for (let int i = 0; i < n; i++) { }
while (cond) { }
do { } while (cond);
outer: for (i in 1..3) { continue outer; }    # labeled loops

let grade = score >= 50 ? "pass" : "fail";    # ternary

match (x) {
    case int n if (n > 0)  => handlePositive(n);
    case [first, second]   => handlePair(first, second);
    case ["go", n]         => handleGo(n);    # literal elements pin positions
    case string s          => handleString(s);
    default                => handleOther();
}
```

`match` uses `=>`, guards are `if (cond)` after the pattern, and
patterns match by type, literal value, or list shape and bind
names. As an expression (`let y = match (x) { ... };`) each arm
yields a value; an unmatched expression with no `default` throws
`MatchError`.

`defer expr;` runs at function exit (LIFO order), like Go.

### Null handling

```gb
?string name = lookup(id);

if (name != null) {
    io.println(name.length());          # narrowed to string inside
}

let display = name ?? "anonymous";      # null coalesce
let len = user?.profile?.bio ?? "";     # optional chaining
```

### Functions

```gb
func add(int a, int b): int { return a + b; }

let inc = func(int n): int { return n + 1; };

func greet(string name, string greeting = "Hello"): string { ... }
greet("Ada", greeting: "Hi");           # named argument

func sum(int start, int ...rest): int { # variadic: rest is list<int>
    return start + rest.reduce(func(int a, int b): int { return a + b; }, 0);
}
sum(1, 2, 3);
sum(...[1, 2, 3]);                      # list spread
greet(...{"name": "Ada"});              # dict spread -> named args
```

- Defaults, named args, variadics, and spread combine freely and
  work in every dispatch context: functions, lambdas, methods,
  static methods, constructors (1.17.0).
- Dict spread silently drops keys that name no parameter.
- Overloading: the same name with different signatures resolves by
  argument count and type at the call site.
- Closures capture by reference; captured variables can be
  reassigned through the closure.
- Pipe: `x |> f(y)` calls `f(x, y)`; chains read left to right.
- Partial application: a call with `_` placeholders returns a new callable with
  those positions open (`add(_, 10)`, `wrap(_, "-", _)`), across every dispatch
  context. Non-hole arguments are captured once at creation.

### Generics

```gb
func head<T>(list<T> xs): T { return xs[0]; }

class Box<T> {
    T item;
    func Box(T v) { this.item = v; }
    func get(): T { return this.item; }
}

func topBy<T implements Scored>(list<T> items): T { ... }
```

Generics are reified: `instanceof T`, `instanceof Box<string>`,
`reflect.typeBindings(x)`, and element-type enforcement on `list<T>.push` all
work at runtime. Explicit constructor type arguments (`Box<string>(42)`) and
class-type-parameter method parameters are enforced on both backends, and
constraints accept interface, class, and primitive leaves combined with `|`/`&`
(`<T implements string|int>`, or the bare `<T string|int>`).

### Generators

```gb
func counter(): generator<int> {
    yield 1;
    yield 2;
}
for (n in counter()) { io.println(n); }
```

Return type is `generator<T>`. Use `iterable<T>` for parameters
that accept any iterable (generators, lists, sets, ranges).
Manual stepping: `next()` advances (`null` once exhausted), `done()` peeks for
exhaustion, `close()` ends early; these compose with `for-in`.
Errors thrown inside a generator keep their class in the
consuming loop's `catch`.

### Async (true parallelism)

```gb
import async;
import io;

func work(int n): int { return n * n; }

let t1 = async.run(func(): int { return work(1); });
let t2 = async.run(func(): int { return work(2); });
io.println(await async.all([t1, t2])); # [1, 4], computed in parallel
```

```gb
import async.channel as ch;
import io;

let c = ch.Channel<int>(8);
c.send(1);
io.println(c.recv());                   # channels also iterate: for (x in c)
```

Tasks are goroutines: CPU-bound fan-out scales across cores (no
GIL, no event loop). `async.await(t)`, `async.all`, `async.race`,
`async.timeout(task, ms)` (raises `TimeoutError`), `async.cancel` compose tasks; channels
and `select` coordinate them. `async.sleep(ms)` returns a Task: `await` it to
pause (an un-awaited call returns immediately).

Never mutate a plain `dict` / `list` / object from several tasks at once;
share mutable state through `store.Store`. A `freeze.deep` value can never
change, so concurrent tasks and requests may read it, and `store.Store` shares
it without copying: build large read-only data once, freeze it, share it.

### Classes

```gb
class Counter {
    int value;
    static int created = 0;
    const string KIND = "counter";

    func Counter(int start = 0) {
        this.value = start;
        Counter.created += 1;
    }
    func bump(): int {
        this.value += 1;
        return this.value;
    }
    static func total(): int { return Counter.created; }
}

class TaskCounter extends Counter {
    string label;
    func TaskCounter(string label) {
        parent(0);                  # calls parent CONSTRUCTOR
        this.label = label;
    }
    func bump(): int {
        return parent.bump() + 100; # calls parent METHOD
    }
}
```

- `parent(args)` calls the parent CONSTRUCTOR; `parent.method(args)`
  calls a parent METHOD. `super` is not a keyword.
- `@abstract` on a class blocks direct instantiation; on a method it
  forces subclasses to override.
- `@immutable` on a class makes fields set-once (construction only).
- No `public`/`private`/`protected` modifiers; module `export` is
  the visibility boundary.

### Operator overloading (dunders)

```gb
class Money {
    int cents;
    func Money(int c) { this.cents = c; }
    func __add(Money o): Money { return Money(this.cents + o.cents); }
    func __eq(Money o): bool { return this.cents == o.cents; }
    func __lt(Money o): bool { return this.cents < o.cents; }
    func __string(): string { return "$${this.cents / 100}"; }
}
```

Dunders: `__add __sub __mul __div __intdiv __mod __pow`, ordering
`__lt __lte __gt __gte` (defining ONE ordering dunder derives the
other three operators), `__eq`, bitwise `__bitand __bitor __bitxor
__lshift __rshift`, prefix `__not __neg __bitnot`, plus `__index`,
`__contains` (for `in`), `__iter`/`__next`/`__done`, `__call`,
`__invoke` (callable objects), and cast dunders `__string __int
__float __bool __decimal __bytes` (drive `as TYPE`, `println`, and
interpolation).

### Interfaces and enums

```gb
interface Notifier {
    func send(string msg): void;
    func sendAll(list<string> ms): void {   # default body allowed
        for (m in ms) { this.send(m); }
    }
}

enum Color { Red, Green, Blue }
enum Shape { Circle(decimal), Rect(decimal, decimal) }

let area = match (shape) {
    case Shape.Circle(decimal r)          => 3.14 * r * r;
    case Shape.Rect(decimal w, decimal h) => w * h;
};
```

Enums can also declare instance methods (`match this` inside the body) and
`implements` an interface, and expose `EnumName.values()` (nullary variants in
declaration order) and `EnumName.fromName(s)` (lookup by exact name, else
`null`).

### Errors

```gb
try {
    risky();
} catch (ValueError e) {
    io.println("${e.class}: ${e.message}");
} finally {
    cleanup();
}

class MyError extends ValueError {
    func MyError(string m) { parent(m); }
}
throw MyError("boom");
```

Catches dispatch by class hierarchy (subclasses match parent
catches). Built-in classes include `Error`, `RuntimeError`,
`ValueError`, `TypeError`, `IOError`, `NetworkError`,
`DatabaseError`, `NotFoundError`, `TimeoutError`, `TlsError`,
`PermissionError`, `AssertionError` (`TimeoutError`/`TlsError` are `IOError`
subclasses; `PermissionError` guards capability-gated ops). `errors.wrap(inner,
msg)`, `errors.is(e, cls)`, `errors.stackTrace(e)`, `errors.frames(e)` are the
stdlib helpers.

### Context managers and decorators

```gb
with (f = io.open("data.txt")) {
    io.println(f.readAll());
}
# the with-binding (f) does not escape the block

import profiler;
import io;
let t = profiler.timer();
with (t) { doWork(); }
io.println(t.elapsedMs());

@memoize
func fib(int n): int { ... }            # built-in caching decorator

func logged(func f): func {             # custom wrapping decorator
    return func(int x): int { io.println("call ${x}"); return f(x); };
}
@logged
func double(int x): int { return x * 2; }
```

Decorators are callable wrappers when the name resolves to a
function, otherwise inspectable metadata (`reflect.decorators`).

### Reflection

```gb
import reflect;

reflect.typeOf(value);                # "int", "list", class name...
reflect.class("module.ClassName");    # resolve class by name
reflect.classes();                    # every loaded class
reflect.fields(cls);                  # field metadata
reflect.methods(cls);                 # method names
reflect.parameters(fn);               # param metadata dicts
reflect.decorators(target);           # [{name, args, namedArgs}]
reflect.hasDecorator(target, "Get");
reflect.typeBindings(value);          # {"T": "int"} for reified generics
reflect.function("math.sqrt");        # resolve function by name (native too)
```

`reflect.interfaces(cls)` returns the DIRECT `implements` clause
only; walk `reflect.parent(cls)` for the full chain. `typeof(x)`
(parens required), `dir(value)`, and `dump(value)` are ambient
introspection builtins.

## Idioms

- **Type cast for access**: `(value as Foo).method()`. Casts are
  CHECKED and throw on mismatch; use `instanceof` to test first.
- **Stringify**: interpolate (`"${n}"`), `n.toString()`, or
  `(n as string)`. String concat does not coerce.
- **Joint dict iteration**: `for (k, v in d) { }`.
- **Accumulate with push**: `xs.push(item);` mutates in place.
- **Hot-loop strings**: `strbuilder` or `+=` on a local (the VM
  fuses the accumulation); avoid rebuilding via `s = s + x` on
  fields.
- **Single-token YAML/parameter**: a `"%key%"` string that wraps
  exactly one marker preserves the referenced value's native type.

## Anti-idioms

- Don't use `//` for comments (it's integer division).
- Don't write `"hello" + 5`. Concat does not coerce; interpolate
  or cast.
- Don't use `super`. Use `parent(args)` / `parent.method(args)`.
- Don't name a source file the same as a stdlib module you import
  (the resolver picks the local file by filename).
- Don't reassign list mutator results expecting a copy:
  `let ys = xs.sort();` leaves `ys` and `xs` the SAME mutated
  list. Use `sorted()` for a copy.
- Don't write multi-paragraph code comments. One short line for
  WHY, never WHAT.

## Stdlib at a glance

| Module | Purpose |
|--------|---------|
| `io` | stdin/stdout, files (`open`, `readText`, `writeText`, `readBytes`), `exists`, `tempDir`. |
| `sys` | env vars, args, `cwd`, `exit`, `goroutineId`, `cpuCount`, `setGcPercent`. |
| `path` / `pathlib` | `join`, `dir`, `base`, `ext`, `abs`; object-style paths. |
| `json`, `yaml`, `toml`, `xml`, `csv`, `msgpack` | parse / stringify; insertion-ordered. |
| `encoding` | base64, hex, URL encoding. |
| `crypt` | AES, HMAC, SHA, bcrypt password hashing, JWT (`jwtSign(p, k, {alg})`). |
| `secrets` | constant-time compare, random tokens. |
| `bytes` | hex/base64, `concat`, `slice`. |
| `strings` | `compare`, `equalsFold`, `fromCodePoint`, splitting helpers. |
| `re`, `pcre` | regex (Go engine / PCRE-style). |
| `db` | SQL connect/query/exec; sqlite, postgres, mysql; transactions, prepared statements. |
| `redis` | client incl. pub/sub. |
| `http` | client (`get`, `post`, `request(url)` fluent builder, `fetchAll`) and server (`serve`, TLS, autocert). |
| `sockets`, `net` | TCP/UDP, low-level networking. |
| `time` | `now`, `sleep`, `monotonicNs`. |
| `datetime` | `parse`, `format`, `Instant`, zones, arithmetic. |
| `async` | tasks, channels, select, worker pools. |
| `store` | synchronised cross-request/goroutine state. |
| `reflect` | class/function/decorator metadata. |
| `collections` | `maxBy`, `groupBy`, `chunk`, `sortBy`, lazy `lazyMap`/`lazyFilter`. |
| `streams` | lazy iteration pipelines. |
| `strbuilder` | O(n) string accumulation. |
| `math` | trig, log, floor/ceil/round, prime tests, special functions (gamma, error functions, Bessel). |
| `ndarray` | N-dimensional numeric arrays: broadcasting, views, reductions, linear algebra, seeded random. |
| `dataframe` | columnar frames: typed columns, expression/predicate filters, `groupBy().agg()`, joins, pivot, CSV/JSON/SQL IO. |
| `process` | own pid/uid/gid; inspect (`list`/`info`/`exists`); gated control (`kill`/`signal`/`setuid`) behind `--allow-process-control`. |
| `file` | `File` object wrapping a handle with method-style read/write/seek and `with`-block auto-close. |
| `llm` | provider-neutral chat/embed client (OpenAI/Anthropic/Bedrock): `chat`, `chatStream`, tool calling, `embed`/`embedBatch`. |
| `profiler` | `timer()`, `profile()` context managers; CPU/memory/wall. Whole-program sampling: `geblang run --profile <report>` / `GEBLANG_PROFILE`. |
| `image` | decode/encode PNG/JPEG/GIF/WebP, resize, crop, rotate. |
| `errors` | `wrap`, `is`, stack traces. |
| `ffi`, `clib.*` | call C libraries (zstd, magic, ncurses, systemd). |
| `messaging` | RabbitMQ / Kafka / SQS / STOMP producers and consumers. |
| `test` | the test framework (below). |

`geblang doc <file-or-dir>` prints exact signatures; `dir(module)`
lists members at runtime.

## Testing

```gb
import test;

func half(int n): int {
    if (n % 2 != 0) { throw ValueError("odd"); }
    return n / 2;
}

class MathTest extends test.Test {
    @test
    func addsTwoIntegers(): void {
        this.assertEquals(5, 2 + 3);
    }

    @test
    func throwsOnInvalidInput(): void {
        this.assertThrows(func(): void { half(3); });
    }
}
```

`setup()` / `teardown()` (per method) and `setupClass()` /
`teardownClass()` (per class) are optional lifecycle hooks. The test
runner picks up `Test` subclasses; no manual `test.run()`. Run with
`geblang test path/`. Asserters include `assertEquals`,
`assertNotEquals`, `assertTrue`, `assertFalse`, `assertNull`,
`assertNotNull`, `assertContains`, `assertEmpty`, `assertGreaterThan`,
`assertThrows`, and `assertThrowsOf`; `this.skip([reason])` skips a
method and `@tag("name")` categorizes it for `--tag`.

To test a module's PRIVATE members, declare the SAME module name
in a sibling `*_test.gb` (1.17.0): the test then runs inside the
module and sees private functions, classes, consts, and state.

```gb
# users.gb:       module app.users;  func normalizeId(...) ...
# users_test.gb:  module app.users;  import test;  class ... extends test.Test
```

## Pitfalls in priority order

1. **`//` is integer division**, not a comment.
2. **List mutators are in-place** (1.16.0+): `push`/`sort`/etc.
   mutate AND return the receiver. `sorted()`/`reversed()`/`copy()`
   are the copy variants.
3. **`as` is a checked cast, not a type test**; it throws on
   mismatch. Use `instanceof` for tests.
4. **Floating literals are `decimal`** by default; suffix `f`
   (`1.5f`) or cast for IEEE `float`. Typed fields are enforced:
   `this.rate = 0.0` on a `float` field is a compile error (a
   `TypeError` when the value is only known at run time); write
   `0.0f`. Integer literals convert (`this.rate = 0`).
5. **Don't shadow stdlib module filenames** in your source files.
6. **Dict iteration is insertion order**, not alphabetical.
7. **String interpolation requires double quotes**; single quotes
   are raw (no `\n`, no `${}`).
8. **`typeof` needs parentheses**: `typeof(x)`.
9. **`reflect.interfaces(cls)`** returns the DIRECT `implements`
   clause only; walk `reflect.parent(cls)` for the chain.
10. **Mixed numeric arithmetic does not coerce** between
    `int`/`decimal` and `float`; cast explicitly.
11. **`is` tests identity, `==` tests value**: `[1] is [1]` is
    `false`, `[1] == [1]` is `true`.

## When in doubt

Write a five-line script and run it with `geblang run` or
`geblang check`. Both backends are fast on tiny programs and the
error messages name the line and the missing piece.
