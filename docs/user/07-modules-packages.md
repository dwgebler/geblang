# Modules And Packages

## Imports

```gb
import io;
import web.router as router;
```

Module paths are dot-separated. Imports can use aliases.

An import binds a module under that name for qualified access (`name.member`).
Imported module bindings are constants and cannot be reassigned, so importing
the same module twice under the same name is idempotent.

Imports are required: using a module as a selector base without importing it
is a semantic error on both runtimes (1.19.0). Before 1.19.0 the bytecode
runtime resolved built-in modules without an import while the evaluator
rejected them at runtime; the static error makes the two agree and surfaces
the missing import in `geblang check`.

```gb
io.println(math.sqrt(4.0));   /* error[semantic]: module "math" is used
                                 without an import; add 'import math;' */
```

```gb
import json;
import json; # harmless

# json = {}; # invalid: imported module bindings are constant
```

A module name is not a first-class value. Use `module.member` to reach its
members, alias it with `import module as alias;`, or pass it to `dir(module)`
to list its members. Assigning, passing, returning, or storing the module name
itself is a compile-time error. To introspect a module by name at runtime, use
`reflect.module("name")` (a string).

```gb
import math;

let r = math.sqrt(16.0);        # ok: qualified member access
let names = dir(math);          # ok: list a module's members

# let x = math;                 # error: a module is not a value
# someFunc(math);               # error: cannot pass a module
let h = reflect.module("math"); # ok: introspect by name string
```

## Selective imports

`from X import Y` binds named symbols from a module into the current
scope without the namespace prefix. Multiple names may share one
statement and each name supports an `as` alias.

```gb
from crypt import passwordHash;
from crypt import passwordHash, passwordVerify;
from crypt import passwordVerify as verify;
from bytes import toHex as hex;

let h = passwordHash("hunter2");
io.println(verify("hunter2", h));   # true
io.println(hex(bytes.fromString("hi")));  # "6869"
```

The form is module-as-source, not module-as-binding: `from crypt
import passwordHash` does not bind `crypt` itself. Use `import
crypt;` alongside if you need both the namespace and a hoisted name.

A class imported this way can be used directly as a parent: `from
shapes import Shape; class Circle extends Shape { ... }` works the same
as the qualified `extends shapes.Shape`.

`from` is a soft keyword: existing identifiers named `from` (function
parameters, class fields) keep working unchanged.

## Exports

User modules export declarations explicitly:

```gb
export class User {}

export func findUser(string id): ?User {
    return null;
}
```

Declarations without `export` are private to the module. Use this for helper
functions, constants, and implementation classes that should not become part of
the public API.

```gb
module app.users;

const TABLE = "users";

func normalizeId(string id): string {
    return id.trim().lower();
}

export func findUser(string id): dict<string, any> {
    let key = normalizeId(id);
    return {"id": key, "table": TABLE};
}
```

### Exporting type aliases

`export type` publishes a type alias that importers can use under the module's
name. The alias is transparent: it means exactly its target in every type
position, including parameter, return, variable, and field annotations, generic
arguments, `implements`, `extends`, `instanceof`, and `as`.

```gb
module shop;

import shop.contract;

export type Client = contract.Client;
export type Money = decimal;
```

```gb
import shop;
from shop import Money;

class LocalClient implements shop.Client {
    func LocalClient() {}
    func charge(Money amount): bool { return true; }
}
```

Aliases are type-only, so `shop.Client(...)` is not a constructor call. A common
use is re-exporting an interface that lives in a leaf module, so implementations
in other submodules can declare `implements` without importing the facade
(which would be a circular import).

A name brought in with `from M import N` is not part of the importing module's
public surface. To re-export a type, declare an `export type` alias for it.

When you run `geblang check` over a file or directory, module declarations are
validated with the normal module resolver. If `module app.users;` resolves to a
different file than the one declaring it, or if two checked files declare the
same module name, `check` reports an error before anything is executed.

## Executable Modules

Use `geblang -m module.name` to run a module without writing a small wrapper
script. The module is imported normally, then its exported `main` function is
called with the remaining command-line arguments:

```gb
module app.cli;

import io;

export func main(list<string> args): int {
    io.println("hello " + (args[0] as string));
    return 0;
}
```

```sh
geblang -m app.cli Ada
```

The recommended contract is `main(list<string> args): int`. Returning `0`
means success; returning a non-zero integer exits with that code. A `void`
or `null` result is treated as success. Top-level module code still runs during
import, but executable behavior should live in `main`.

Running a file directly does the same thing: `geblang path/to/file.gb` (or just
`geblang file.gb`) auto-invokes an exported top-level `main` when the file
declares one, forwarding the remaining command-line arguments and using an
`int` return value as the exit code. So `geblang app/cli.gb Ada` runs the
`main` above without `-m` and without a wrapper. `-m` is the way to run a module
by its canonical name (resolved through the module path) rather than by file
path; both invoke `main` the same way. A file with no exported `main` runs as a
plain script (its top-level statements execute).

## Module Top-Level

A file that opens with `module name;` is a **module file** and is held to a
stricter shape than a script: only declarative statements are allowed at the
top level. Anything that performs work, such as a function call, an `if`, a
`for`, or an assignment to an existing binding, must live inside an
`init { ... }` block.

Allowed at the top level of a module:

| Statement                                 | Example                                  |
|-------------------------------------------|------------------------------------------|
| `module name;`                            | `module app.ids;`                        |
| `import ...`                              | `import uuid;`                           |
| `export ...`                              | `export func f(): int { return 1; }`     |
| `type alias`                              | `type UserId = string;`                  |
| Constant / variable declaration           | `const PREFIX = "x";`                    |
|                                           | `let bootId = uuid.v7();`                |
|                                           | `int counter = 0;`                       |
| `func` declaration                        | `func g(int n): int { return n + 1; }`  |
| `class` / `interface` / `enum` declaration | `class Tag { ... }`                     |
| `init { ... }` block (at most one)        | see below                                |

Anything else (`io.println("loaded");`, `if (cond) { ... }`, `[a, b] = ...`)
is rejected by `geblang check` with a diagnostic like
`free-standing top-level expression is not allowed in a module file; wrap
imperative setup in an init { ... } block`.

The reasoning: a module file should be readable as a contract. Looking at the
top of the file, a caller should see what the module declares (`const`, `func`,
`class`) and what setup runs on import (`init`). Hiding `io.println("loaded")`
between two declarations partway down the file is exactly the
load-order-as-execution-order trap that Python and PHP have to warn about in
style guides; Geblang prevents it at the parser level.

```gb
module app.ids;

import uuid;

const PREFIX = "usr";

# Side-effecting *initialiser* on a declaration is fine - it's part of
# the binding's value, not a free-standing effect.
let bootId = uuid.v7();

export func nextUserId(): string {
    return PREFIX + "-" + uuid.v7();
}
```

The cached-on-first-import behaviour still applies: the module body (including
the `init` block, if any) runs at most once. Subsequent imports reuse the
exports.

### Module-level mutable state across calls

A module's top-level `let`/typed variables are **persistent module state**.
After a successful exported-function call completes, later calls, including
calls from other modules, observe its assignments on both runtimes.

```gb
module counter;
let n = 0;
export func bump(): void { n = n + 1; }
export func get(): int { return n; }
```

```gb
import counter;
counter.bump();
counter.bump();
io.println(counter.get());   # 2 on both `geblang test` and a built binary
```

Module-level state is **shared mutable state**, so the usual concurrency rule
applies: two requests or async tasks mutating the same module global at the same
time race, and a contended write can be lost. For state that is shared and
mutated across concurrent tasks or requests, use an explicit concurrency-safe
holder such as `store.Store` (see the async chapter). An instance can hold state
for one owner, but sharing a mutable instance across goroutines still requires
synchronisation:

```gb
# suitable for one owner; do not mutate one shared instance concurrently
class Counter {
    int n = 0;
    func bump(): void { this.n = this.n + 1; }
    func get(): int { return this.n; }
}
```

The bytecode backend commits reassigned module-global slots when a
cross-module call returns, including a call that throws (assignments made
before the throw are kept, matching the evaluator). A synchronous re-entrant
call into the same module (through a callback, on one goroutine) sees the outer
call's pending assignments. An async task runs on its own goroutine and sees a
committed snapshot, not the live globals, so writes from a task are not
guaranteed to be observed by the caller. Do not use module globals for
concurrent or transactional coordination; use an explicit concurrency-safe
state object (for example `store.Store`).

### Init Blocks

`init { ... }` is the single place imperative module setup lives:

```gb
module app.metrics;

import metrics;
import sys;

const SERVICE = sys.getenv("SERVICE_NAME") ?? "unnamed";

init {
    metrics.register("requests_total");
    metrics.register("errors_total");
    metrics.tag("service", SERVICE);
}

export func recordRequest(): void { metrics.inc("requests_total"); }
```

Rules:

- **One per file.** The semantic analyzer rejects more than one init block
  per module with `only one init block is allowed per file`. If you have a
  lot of setup, break it into a private helper function called from inside
  `init`.
- **Runs once.** The block fires the first time the module is imported.
  Subsequent imports reuse the cached exports and `init` does not run again.
- **In source order.** Code above the `init` block runs first (typically
  declarations and their initialisers), then the block, then code below.
- **Inside a module, init is the only imperative escape hatch.** Free-
  standing calls, `if`, `while`, `for`, `match`, `try` and assignments to
  existing bindings have to be inside `init` (or, if they belong to a piece
  of logic the module wants to expose, inside an exported function).
- **No special privileges.** `init` can do anything top-level code in a
  script can do: call functions, declare locals, throw exceptions (which
  propagate out of the import). It cannot `return` and is not an event hook.

### Script files

Files **without** a `module` declaration are scripts. They keep their full
top-level freedom: top-level imperative code is the whole point. `geblang
script.gb` runs `script.gb` top to bottom, so an `init` block isn't useful
there and isn't required.

The rule of thumb: if you're authoring a reusable module that other code
will `import`, write `module name;` at the top and keep imperative setup in
`init`. If you're writing a script, omit `module` and write the body
directly.

## Package Manifest

A package is any directory with a `geblang.yaml` manifest at its root. The
manifest names the package, points at its source, lists its dependencies, and
declares the resources a release binary should embed. The toolchain (`geblang`,
`geblang test`, `geblang build`, `geblang check`) finds a manifest by walking up
from the current file, so a manifest placed above your code configures
resolution for everything beneath it. `geblang.yml` and `geblang.json` are
accepted as alternate filenames.

Scaffold one with `geblang init`:

```sh
geblang init                                  # name inferred from the directory
geblang init --name acme.tools --source src
```

`geblang init` writes `geblang.yaml` (defaulting to `version: 0.1.0` and
`source: src`), creates the source directory, and refuses to overwrite an
existing manifest unless `--force` is given. With no `--name`, the package name
is derived from the current directory.

### Fields

```yaml
name: acme.tools          # package namespace (the canonical module prefix)
version: 0.1.0            # informational; also embedded by geblang build
source: src               # primary module root, relative to the manifest
paths:                    # extra module roots, relative to the manifest
  - generated
resources:                # files embedded into a geblang build binary
  - templates
  - assets
dependencies:             # other packages, by mount name (see below)
  shared: ../shared
  httplib:
    git: https://github.com/acme/httplib
    version: v1.4.0
```

| Field | Purpose |
|-------|---------|
| `name` | The package namespace and canonical module prefix. With `source: src`, the file `src/app/users.gb` is imported as `import app.users;`. Pick a top-level namespace you own (e.g. `acme.*`) to avoid colliding with a reserved built-in name. |
| `version` | Free-form version string. Resolution ignores it; `geblang build` embeds it as the binary's version (shown by the built binary's `--version`). |
| `source` | The primary module root, a single directory. Its tree resolves as modules: `import app.users;` finds `<source>/app/users.gb` or `<source>/app/users/init.gb`. When omitted, the manifest's own directory is the root. |
| `paths` | Additional module roots, each relative to the manifest, searched after `source`. Use for generated or co-located code that should also resolve as modules. `modulePaths` is accepted as an alias and merged in. |
| `resources` | Files and directories embedded into the binary by `geblang build`, so a single-file release can read them at runtime. They are not part of the import tree. See the Bundling chapter for details. |
| `dependencies` | Other packages this one imports, each mounted under a name and pointing at a local path or a git repository (next section). |

A nested `package:` block with `name:` and `version:` keys is accepted as an
alternative to the top-level `name` and `version`.

### Module resolution order

For an ordinary (non-reserved) name, the resolver searches, in order: this
package's roots (`source`, then each `paths` entry), each dependency's roots
(transitively), the bundled source stdlib, then any directory on `GEBLANG_PATH`.
The first matching `<name>.gb` (or `<name>/init.gb`) wins. Reserved built-in
names always resolve to the built-in regardless of local files (see Reserved
built-in module names below), so resolution is identical on the evaluator and
the bytecode VM.

### Dependencies

Each dependency is keyed by the name it is mounted under and is either a path or
a git source.

**Path dependencies** point at another package on disk. A relative path is
resolved against the manifest; an absolute path is used as-is. A leading `~`
expands to the home directory and `$VAR` / `${VAR}` expand from the environment.
A bare string is shorthand for a path; the mapping form is equivalent:

```yaml
dependencies:
  shared: ../shared
  widgets:
    path: ../packages/widgets
  vendored:
    path: /opt/geblang/packages/vendored
  tooling:
    path: ~/src/tooling
```

The target must itself be a package (have its own `geblang.yaml`); its `source`,
`paths`, and transitive dependencies are all honored. Path dependencies need no
install step.

**Git dependencies** point at a remote repository:

```yaml
dependencies:
  httplib:
    git: https://github.com/acme/httplib
    version: v1.4.0      # tag or branch; `latest` for the newest semver tag; omit for the default branch
```

A scheme-less `git` value such as `github.com/acme/httplib` is treated as
`https://github.com/acme/httplib`. Explicit schemes (`https://`, `ssh://`) and
the scp-like `git@github.com:acme/httplib.git` form are used unchanged.

`geblang install` clones every git dependency into `vendor/<name>` beside the
manifest and pins the resolved commit in `geblang.lock`:

```sh
geblang install                                              # fetch all declared git deps
geblang install https://github.com/acme/httplib              # add one and fetch it
geblang install https://github.com/acme/httplib@v1.4.0 http  # pin a version, mount as http
```

The add form appends the dependency to `geblang.yaml`, inferring the mount name
from the URL's last path segment when you omit it. A git dependency that has not
been installed is skipped during resolution until you run `geblang install`.
Commit `geblang.lock` so collaborators and CI resolve the same commits:

```yaml
# geblang.lock
dependencies:
  httplib:
    url: https://github.com/acme/httplib
    version: v1.4.0
    commit: 9f2c1ab4e8d7b6...
```

## Source Stdlib

Geblang ships source modules under `stdlib/` as normal `.gb` modules. Current
source modules include:

- `config`
- `cli.command`
- `testing.assertions`
- `web.router`
- `redis`

The binary embeds these source modules and falls back to that embedded copy
when no stdlib is found on disk, so a standalone `geblang` resolves them with no
setup. Set `GEBLANG_STDLIB` to add or override stdlib roots - for example, to
develop against a working-copy `stdlib/` rather than the embedded one. An
on-disk stdlib root always takes precedence over the embedded copy.

## Reserved built-in module names

Built-in module names - every native module and every shipped stdlib module -
are reserved. A program or package module may not use one of these names: doing
so is an error reported by `geblang` and `geblang check`. Built-in names always
resolve to the built-in, so a stray local file can never silently shadow `io`,
`json`, `math`, and the like, and resolution is identical on both the evaluator
and the bytecode VM.

```gb
/* a file declaring a reserved name is rejected: */
module io;            /* error: module io shadows a reserved built-in name */
```

This applies to the declared module name, not the filename, so a namespaced
module is fine even if its file is named like a built-in - for example a file
`errors.gb` that declares `module myapp.errors;` is accepted, because its
canonical name is `myapp.errors`, not `errors`. Choose a top-level namespace for
your own modules (e.g. `myapp.*`) and you will never collide.

A stray local file that does declare a reserved name is never selected by
imports: `import math;` next to an offending local `math.gb` still binds the
built-in `math` on both runtimes, and running the offending file itself
reports the reserved-name error.

The `geblang` namespace is also reserved. `import geblang.X` resolves explicitly
and unambiguously to the built-in module `X` (whether native or stdlib):

```gb
import geblang.json as json;   /* always the built-in json */
import geblang.math;           /* always the built-in math */
```

`geblang.X` is only valid when `X` is a built-in; `import geblang.something` for
a non-built-in is an error. Resolution for ordinary (non-reserved) names is
unchanged: your own modules resolve from the program and package paths first,
then bundled stdlib, then `GEBLANG_PATH`.

### Native + stdlib dual-name modules (1.6.0)

When a stdlib `.gb` module and a native module share the same canonical
name (e.g. `async.sync` ships as both a Go-side native primitive set
and a Geblang stdlib of wrapper classes), the resolver picks the
stdlib externally and the native internally:

- `import async.sync` from user code returns the stdlib module. Method
  calls that miss the stdlib's exports fall back to the native
  registry, so both the classes and the underlying free functions are
  reachable through one alias.
- The stdlib's own `import async.sync as native;` (inside `module
  async.sync;`) resolves to the native module so the wrapper can call
  its primitives.

The two surfaces of a bundled dual-name module are always disjoint, so
a member resolves the same way on both runtimes.

## Multi-Module Layout

An idiomatic project keeps executable entry points thin and moves reusable code
into modules:

```text
geblang.yaml
src/
  main.gb
  config.gb
  domain.gb
  repository.gb
  service.gb
```

See `examples/expense_tracker/` for a small multi-module application.

Recommended shape:

```text
src/
  main.gb          # entry point, wiring, command dispatch
  config.gb        # config loading
  domain/
    expense.gb     # classes and domain rules
  storage/
    repository.gb  # database or file persistence
  web/
    routes.gb      # HTTP route registration
```

Keep entry points thin. The rest of the application should be importable and
testable without running the program.
