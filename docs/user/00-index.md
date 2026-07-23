# Geblang Reference Manual

Geblang is a general-purpose scripting language implemented in Go.

Inspired by PHP and Python (and a little bit of Go), Geblang aims to offer the simplicity and ergonomics
of those languages while making static typing, generics, async programming,
runtime metadata and modern tooling part of the core language and standard
developer experience.

> See [Gebweb](https://github.com/dwgebler/gebweb) for the accompanying web and
> API framework built on top of Geblang.

To that end, Geblang combines features that will feel familiar to developers
from both ecosystems.

Features inspired by PHP include:

- C-style, curly brace syntax; no significant whitespace
- Single inheritance, multiple interfaces
- A web-first feel, with tooling and standard-library support for HTTP APIs and web apps
- A familiar set of application-focused functionality in the standard library
- Practical request/response, session, routing, database, Redis, mailer and filesystem modules
- Familiar class-based application structure for services, controllers, handlers and domain objects
- Magic methods for callable objects and property access patterns
- Simple deployment for scripts, CLI tools and web applications
- Developer ergonomics aimed at everyday application work

Features inspired by Python include:

- First-class objects for all types
- Built-in dict, list and set types
- Simple module resolution based on source paths
- Aspect-oriented programming with decorators
- Operator overloading via magic/dunder methods
- Clear, readable syntax with low boilerplate
- List and dict comprehensions
- REPL-first exploration and tooling
- Exceptions and custom error classes for structured failure handling
- A batteries-included standard library philosophy
- No private, protected, public modifiers
- Immutability via `freeze` module and `@immutable` decorator for classes

Other features offered by Geblang as core language or first-party tooling include:

- Static typing with type inference, plus the special `any` type for intentionally dynamic values
- Function and method overloading - multiple signatures of the same name, resolved by argument count and type at call time
- Generics with type checking and runtime reflection, including `instanceof T` checks
- Runtime type metadata via `typeof`, type values and reflection APIs
- Native async/await and generators as core syntax, backed by goroutines (tasks run in parallel on the Go scheduler, not a single-threaded event loop)
- Built-in bundling of Geblang programs into distributable binaries containing the interpreter, stdlib,
  application and dependency packages
- First-party LSP, debugger, formatter, linter/static analysis and documentation tooling
- Bundled Visual Studio Code extension for syntax highlighting, stdlib autocomplete and step debugging.
- A language design that keeps PHP/Python-style productivity while making types, metadata, packaging and
  tooling coherent from the start

Compared with TypeScript on Node, Geblang is not a typed layer over another
dynamic runtime: types, generics, reflection metadata, async execution,
decorators, the standard library and bundling are all part of one
language/runtime contract rather than erased at compile time or delegated to a
separate JavaScript platform. See the dedicated comparison below for what that
means in practice for HTTP services.

## Repository

Geblang is open source under the MIT License. The repository is at https://github.com/dwgebler/geblang

## What Geblang Is For

Geblang is designed for:

- Scripts that can grow into maintainable applications
- CLI tools, automation and developer utilities
- HTTP APIs, web apps and practical web services
- Data processing, file transformation and integration jobs
- Things you'd write in PHP or Python but where you want the language benefits of static types
- Applications that want to leverage runtime metadata
- Projects that should be easy to run, test, bundle and deploy

## Philosophy

Geblang aims to offer the same simplicity for developers of languages like PHP and Python while
adding the static guarantees those ecosystems often have to recover later with
community-driven conventions, frameworks or third party static analysis tools. 

Geblang was designed from the start to deliver the web-first benefits of PHP without being
constrained by some of the historical design decisions that have held PHP back. 
Common web, CLI, data, Redis, database, HTTP, socket, Markdown, and serialization tasks are available
out of the box. The goal is to provide enough well-typed building blocks that small applications can
stay small, and larger applications can build clear framework layers on top.

Geblang favors explicitness at module and API boundaries. Imports name the
module being used, user modules export their public surface deliberately, and
type hints document the values an API expects. Dynamic features exist, but they
are intended to work with typed code rather than replace it.

Two disciplines run through the implementation. First, types are real: the
static checks run on the compile path for `run`, `test`, and `build` alike,
reach across module boundaries, and the same type metadata is reified at
runtime for `instanceof`, reflection, and request binding. Second, behavior is
specified twice: every program must produce identical output on the bytecode
VM and the reference evaluator, enforced by a parity suite and a generative
fuzzer. Each backend acts as an executable specification for the other, which
is how a language this young holds its semantics steady from release to
release.

### Coming From PHP

PHP developers will find Geblang's syntax familiar: semicolons, curly braces,
`class`/`extends`/`implements`, `foreach`, `match`, closures, and a rich
built-in standard library. The key differences are all additions:

| Feature              | PHP                                       | Geblang |
|----------------------|-------------------------------------------|---------|
| Static typing        | Optional type hints, loose enforcement    | Always-on, enforced at declaration and call sites |
| Generics             | Docblock comments only                    | First-class, reified - `func identity<T>(T v): T` works at runtime |
| Null safety          | All types are nullable by default         | Non-null by default; `?string` opts in to null |
| Null coalescing      | `??` operator, ?-> operator               | `??` and `?.` optional chaining |
| Interfaces           | Runtime checks only                       | Statically enforced at declaration and call sites |
| Enums                | `enum` with scalar backing                | `enum` with or without associated values, full pattern matching |
| Async/await          | Fibers (PHP 8.1+, manual)                 | First-class `async func`, `await`, `Task<T>` |
| Decorators           | Attributes (PHP 8, metadata only)         | Callable decorators that wrap functions/methods, `reflect` module |
| Module system        | `require`/`include`/`use` with namespaces | Single `import` with explicit module resolution |
| Type introspection   | `gettype()`, `instanceof`, `get_class()`  | `typeof(x)`, `x instanceof T`, `x.type`, `reflect` module |
| Generics enforcement | Not available                             | `x instanceof T` resolves `T` to its concrete bound type at runtime |
| Bytecode VM          | Zend Engine                               | Embedded Go VM - scripts, APIs, and CLI tools in one binary |
| Distribution         | PHP runtime required                      | `geblang build` produces a self-contained executable |

Geblang also drops `public`, `protected`, `private`, and `final`
modifiers on classes and members. The reasoning follows the Python
side of the inspiration: visibility modifiers exist mainly to
enforce a convention that thoughtful API design and tooling already
handle.  The discipline that visibility modifiers normally encode lives 
in `export`, `@immutable`, interface contracts and being a responsible developer.

### Coming From Python

Python developers will recognise the `import` system, list/dict comprehension
style, `async`/`await`, decorators, and the goal of staying readable at small
scale. The key differences are:

| Feature | Python | Geblang |
|---------|--------|---------|
| Static typing | Annotations + mypy (optional, bolted on) | Built-in, enforced - no separate tool needed |
| Type syntax | `Optional[T]`, `list[T]`, `Union[T, U]` | `?T`, `list<T>`, interface union constraints |
| Null safety | `None` is any type; runtime `AttributeError` | `null` is only valid where `?type` is declared |
| Generics | PEP 484 annotations, not reified | Reified - `instanceof T` and `reflect.typeBindings` work at runtime |
| Classes and OO | Duck typing, no enforced interface contracts | Formal interfaces, static method signatures, `implements` checked |
| Module imports | Implicit `__init__.py`, star imports possible | Explicit `import module;`, deliberate export surface |
| Decimal math | `decimal.Decimal` class (import required) | `decimal` is a primitive type; `3.14` is decimal by default |
| Async runtime | `asyncio`, event loop plumbing visible | `async func`, `await`, and `Task<T>` - runtime managed, no loop setup |
| Decorators | Callable wrappers only, no runtime metadata | Both callable wrappers and inspectable metadata via `reflect` |
| Type introspection | `type()`, `isinstance()`, `__class__` | `typeof(x)`, `x instanceof T`, `x.type`, consistent everywhere |
| Enums | `enum.Enum` (import, class, `.value` access) | `enum Status: string { Active = "active"; }` - first-class syntax, pattern matching |
| Generators | `yield`, `yield from`, generator expressions | `yield` in functions and closures, `generator<T>` type hint |
| Distribution | `venv`, `pip`, runtime required | `geblang build` embeds interpreter, stdlib, and source into one binary |
| Indentation sensitivity | Required (syntax-level) | Braces (no whitespace sensitivity) |

Python features that Geblang omits include multiple inheritance and
untyped `**kwargs` bags. Typed variadic parameters (`int ...rest`,
collected as a `list<int>`), named arguments, default values, and
list/dict spread at call sites cover the same call shapes with the
types kept intact.

### Coming From TypeScript / Node

TypeScript fixed JavaScript's type problem at compile time, but the
types stop existing the moment the code runs. Geblang keeps them alive
at runtime, and replaces the Node platform underneath with a single
purpose-built runtime. For HTTP-layer services - APIs, gateways,
backends-for-frontends, webhook processors, integration glue - that
trade looks like this:

| Feature | TypeScript / Node | Geblang |
|---------|-------------------|---------|
| Types at runtime | Erased; `as` casts are unchecked assertions | Reified; casts are checked, `instanceof list<int>` works, APIs can validate against real types |
| Parallelism | Single-threaded event loop; worker threads are separate isolates with message passing | Goroutine-backed tasks across all cores; `async.run` is true parallelism with shared typed values |
| Request state | One shared mutable module scope for every in-flight request | Bare handlers are isolated per request; routers share an explicit application graph and concurrency-safe services |
| Blocking code | Blocks the event loop; everything must be async-aware | Blocks one goroutine; no function colouring, any code can be awaited |
| Toolchain | tsc + bundler + package manager + runtime, each configured separately | One binary: run, test, check, format, LSP, debug, bundle |
| Dependencies | `node_modules`, transitive sprawl, supply-chain surface | Batteries-included stdlib (HTTP server/client with TLS and LetsEncrypt, database, Redis, JSON/YAML/XML/CSV, crypto, templating, queues); packages exist but most services need few or none |
| Type-checked config and data | Zod/io-ts schemas duplicated alongside the types | The type IS the schema; reflection-driven binding and validation read the real signatures |
| Deployment | Runtime + lockfile + node_modules or a bundler step | `geblang build` emits one self-contained executable |
| Web framework | Express/Fastify/Nest, assembled from packages | Gebweb ships alongside: typed controllers, DI, validation, OpenAPI from your signatures |

The short version: if a service's job is to terminate HTTP, validate
and transform typed payloads, talk to databases and other services,
and stay up under concurrent load, Geblang gives you the static types
TypeScript promised, enforced at runtime, with true parallelism and a
one-binary deployment - and without the event-loop discipline, the
schema-duplication libraries, or the `node_modules` tree. TypeScript
remains the right call when you need the browser ecosystem or
isomorphic front/back code; Geblang aims at the server side of that
line.

## Quick Example

```gb
import io;
import collections;

interface Scored {
    func score(): int;
}

@immutable
class Player implements Scored {
    string name;
    int rating;

    func Player(string name, int rating) {
        this.name = name;
        this.rating = rating;
    }

    func score(): int {
        return this.rating;
    }
}

func topBy<T implements Scored>(list<T> items): T {
    return collections.maxBy(items, func(T x): int { return x.score(); });
}

let players = [Player("Ada", 10), Player("Grace", 12), Player("Linus", 7)];
io.println(topBy(players).name);   # Grace
```

The example shows a few of the language pieces a typical Geblang program might use:
a typed `@immutable` class with a constructor, an `interface` it
explicitly `implements`, a generic function with an interface-bounded
type parameter (`T implements Scored`) that is checked at compile time
and reified at runtime, a lambda whose `T` parameter resolves to the
outer call site's concrete bound (`Player` in this run), and a stdlib
higher-order helper (`collections.maxBy`) that takes the lambda by
value. The same code could later move `Player` into a module, hydrate
the list from a database, expose `topBy` through an HTTP handler, or
run inside an async task without changing the basic shape.

## Performance

Geblang is designed around a bytecode VM as the normal execution path, with the
tree-walking evaluator kept as a compatibility and implementation path. Use
`--trace-exec` when running a script to confirm whether the VM or evaluator was
used.

The repository includes a small benchmark harness for local performance checks:

```sh
make bench
```

The benchmark suite covers integer-heavy loops, recursive function
calls, list and dict construction, string concatenation, method
dispatch on a hot class, regex matching, JSON parse + stringify
round-trips, and lazy/eager functional collection pipelines. It
reports Geblang timings alongside equivalent Python and PHP scripts
when those runtimes are installed. There is also a version
available via Docker if you don't have PHP or Python locally:

```sh
make bench-docker
```

These numbers should be treated as loose signals only. Both PHP and
Python have made significant performance strides in recent years;
PHP in particular ships a real JIT in PHP 8, and CPython 3.11+ has
an improved interpreter. Geblang aims to be in the same ballpark as
those interpreters on realistic application code without attempting
to match the JIT.

Indicative numbers from a development machine, measured against an
earlier release (the absolute values will vary release to release and
machine to machine; medians of repeated runs):

| Benchmark          | Geblang | Python | PHP    | Node   |
|--------------------|---------|--------|--------|--------|
| `numeric_loop`     | 71 ms   | 125 ms | 28 ms  | 28 ms  |
| `recursive_fib`    | 57 ms   | 36 ms  | 22 ms  | 25 ms  |
| `list_pipeline`    | 14 ms   | 15 ms  | 10 ms  | 22 ms  |
| `string_concat`    | 13 ms   | 19 ms  | 11 ms  | 23 ms  |
| `dict_ops`         | 22 ms   | 18 ms  | 12 ms  | 28 ms  |
| `class_dispatch`   | 25 ms   | 19 ms  | 12 ms  | 24 ms  |
| `regex_match`      | 36 ms   | 42 ms  | 15 ms  | 26 ms  |
| `json_roundtrip`   | 431 ms  | 479 ms | 310 ms | 263 ms |
| `list_functional`  | 19 ms   | 16 ms  | 13 ms  | 25 ms  |

### What Geblang is quick at

`numeric_loop` runs a counted `for` loop two million times with a
small if/else and integer arithmetic in the body. The VM keeps small
integers unboxed and fuses the common compare-and-branch patterns, so
Geblang runs this at roughly twice CPython's speed and ahead of every
collection benchmark Node posts here.

`list_pipeline`, `list_functional`, and `string_concat` are
allocation-heavy patterns the VM has been tuned for: building lists
with `.push`, chaining `.map` / `.filter` / `.reduce`, and
concatenating string literals into a builder in tight loops. All
three sit level with CPython and PHP and well ahead of Node;
`string_concat` ties PHP outright.

`json_roundtrip` parses a 1 MB JSON payload and stringifies it back
200 times. The token-driven parser and direct encoder outperform
Python's C-implemented `json` module on this workload.

`dict_ops` and `class_dispatch` exercise dict get/set and instance
method dispatch in a tight loop; both are competitive with Python
and Node.

### What it's slower at

`recursive_fib(28)` is a large number of bare recursive calls and not
much else; Geblang trails Python, PHP, and Node there. Call-heavy
code pays for the per-call type validation that reified generics and
enforced signatures require - 1.17.0's dispatch-loop work narrowed
the gap, and further call-path tuning is ongoing. `regex_match` cost
is dominated by the Go regex engine: the cached native dispatch
puts it ahead of CPython, still behind PCRE (PHP) and V8's Irregexp
(Node).

For everyday application code - request handlers, parsing JSON,
walking lists and dicts, modest loops - the per-call difference
disappears into IO time. The benchmarks above are deliberately
hostile microloops; run `make bench` yourself and weigh the shapes
that match your workload.

## Concurrency And True Parallelism

Geblang's concurrency model is one of its clearest separations from
the languages it borrows ergonomics from. `async.run` hands work to a
real goroutine on the Go scheduler, so tasks execute in parallel
across every core:

```gb
import async;
import io;

let tasks = [
    async.run(func(): int { return heavyWork(1); }),
    async.run(func(): int { return heavyWork(2); }),
    async.run(func(): int { return heavyWork(3); }),
];
io.println(async.all(tasks));   # three cores, one wall-clock unit
```

What that means against the neighbours:

- **No GIL.** Python threads interleave on one core; Geblang tasks
  genuinely run simultaneously. CPU-bound fan-out scales with cores
  without multiprocessing, pickling, or worker pools.
- **No event loop.** Node gets concurrency by never blocking a single
  thread, which makes blocking code a bug and splits the ecosystem
  into sync and async halves. A Geblang task that blocks just parks
  its goroutine; there is no function colouring, and `await` works on
  anything.
- **No per-request process model.** PHP gets isolation by tearing the
  world down after every request. Geblang keeps the process resident.
  A handler passed directly to `http.serve`, `http.listen`, or
  `net.serve` receives an isolated copy of its captured state. Router
  and framework applications instead share their application graph,
  which keeps services and pools reusable; mutable cross-request state
  belongs in `store.Store` or another concurrency-safe handle.

Generators, channels (`async.channel`'s `Channel<T>`), `select`, worker pools, and
structured fan-out (`async.all`, `http.fetchAll` with a concurrency
cap) are all built on the same primitives, and the test runner,
profiler, and web framework understand them natively. The async
chapter covers the full surface.

## Manual Chapters

- Getting started: running scripts, checking code, REPL basics, bytecode cache.
- Syntax basics: comments, declarations, expressions, strings, collections.
- Types: primitive types, nullability, casts, type aliases, generics, type
  values.
- Control flow: conditionals, loops, pattern matching, defer.
- Functions and callables: defaults, named arguments, variadic parameters,
  anonymous functions, closures, decorators.
- Classes and interfaces: constructors, inheritance, static members, magic
  methods, interfaces, enums.
- Modules and packages: imports, exports, manifests, source stdlib.
- Errors: throwing, catching, built-in error classes, stack traces.
- Async and generators: tasks, await, lazy generators, iterable type hints.
- Testing: writing `*_test.gb` files with `test.Test`, `@test`/`@tag`,
  built-in assertions, `geblang test` runner, CI integration.
- Standard library reference: native modules and bundled source modules.
- Web development: HTTP, routing, middleware, sessions, cache, SSR helpers.
- Reflection: `typeof`, `.type`, `instanceof`, reified generics, and the
  `reflect` module for inspecting classes, functions, decorators, and modules
  at runtime.
- Tooling and examples: CLI commands, tests, docs, Docker builds.
- Bundling and standalone executables: `geblang build`, bundle format, package
  layout, first-run extraction, limitations.
- VS Code extension: building and installing the extension, syntax highlighting,
  live diagnostics, step debugging, launch configurations.
- Internals: pipeline overview, lexer, parser, AST, semantic analyzer, runtime
  values, bytecode format, compiler, VM, evaluator, module system, native and
  source stdlib, CLI structure.

## Status

Geblang is at version 1.33.0 and under active development, with
regular minor releases since 1.0 (see the release notes chapter for
the full history). The bytecode VM is the default execution path; the
tree-walking evaluator backs the test runner and acts as a
compatibility path. The two backends are held to byte-identical
output by a parity test suite and a generative fuzzer, so programs
behave the same whichever engine runs them. Use `--trace-exec` to see
which engine ran a script.

## Guides

Onboarding guides for developers coming from other backgrounds:

- [Guides overview](guides/00-index.md)
- [Geblang for data scientists](guides/01-for-data-scientists.md)
- [Geblang for developers from another language](guides/02-for-developers-from-another-language.md)
- [Geblang for systems programmers](guides/03-for-systems-programmers.md)
- [Geblang for web developers](guides/04-for-web-developers.md)

## For AI agents

A condensed cheatsheet for AI coding agents working in Geblang
lives at [AGENTS.md](AGENTS.md). It's denser than this manual,
focused on syntax, idioms, and common pitfalls, and intended to
be read once at the start of a session. Point your agent at that
file before asking it to edit Geblang code.
