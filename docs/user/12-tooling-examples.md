# Tooling And Examples

## Command-Line Tools

Use `args` or `cli` for command-line interfaces:

```gb
import cli;

let parsed = cli.parseArgs(sys.args(), {
    "name": {"type": "string", "required": true},
    "verbose": {"type": "bool", "short": "v"}
});

let password = cli.password("Password: ");
```

`cli.command` provides source classes for command/option structure.

## Source API Documentation

Use `geblang doc` to generate API reference material from Geblang source
without executing the program:

```sh
geblang doc examples/api_docs.gb
geblang doc --format json examples/api_docs.gb
geblang doc --out build/api.md src
```

The command parses `.gb` files, reads docblocks, and emits declarations for
functions, classes, interfaces, type aliases, and enums. It also includes
decorator metadata, generic parameters, inheritance, class fields, class
methods, static methods, and interface method signatures.

When a file contains `export`, only exported declarations are included. This
matches package documentation expectations: private helpers can keep docblocks
for maintainers without appearing in public API output. In scripts or examples
with no `export` statements, all top-level documentable declarations are
included.

Doc comments immediately before `export` are attached to the exported symbol,
so this is valid API documentation syntax:

```gb
## Lists users for an API endpoint.
export @route("GET", "/users")
func listUsers(): list<string> {
    return ["Ada"];
}
```

Use JSON output for editor integrations, framework discovery tools, or custom
documentation pipelines. Use Markdown output when you want a quick reference
file that can be checked into project docs or converted by another site
generator.

## Static Checks

Use `geblang check` in editors and CI to validate code without executing it:

```sh
geblang check src/
geblang check --json src/
geblang check --no-lint src/
geblang check --strict src/
```

The checker parses files, runs semantic analysis, validates imports through the
normal module resolver, verifies declared module names resolve back to their
files, reports duplicate module declarations across a checked directory, and
reports lint warnings such as unused imports and unreachable statements.

It also performs cross-module symbol resolution: a `module.member` access or
`from module import name` is an error when the module does not export that
name. This works for both built-in modules and your own modules across a
multi-file project, so a typo like `io.foobar()` or an outdated API call is
caught before you run anything:

```sh
$ geblang check app.gb
app.gb:2:4: error[import]: io has no exported member foobar
```

Member checks resolve relative to each file and follow local scope, so a local
variable that shadows a module name (for example a list called `errors`) is not
mistaken for the module.

The same resolution covers methods on your own classes: calling a method that
does not exist on a typed instance's class - across its parent chain and
implemented interfaces, including classes imported from other modules - is an
error:

```sh
$ geblang check app.gb
app.gb:6:3: error[semantic]: Circle has no method bogus
```

The method check is conservative to avoid false positives: it stays silent when
the receiver's type is not a statically known class, when the class (or any
ancestor) defines `__call`, when the class carries decorators that may inject
members, or when any part of the hierarchy cannot be resolved.

These checks also run inside class method and constructor bodies, not just
top-level and free-function code. Inside a method, `this` is typed as the
enclosing class, so member access and argument types on `this.field` and on
typed locals are validated, and a `return` expression is checked against the
method's declared return type. Calls on `this` stay conservative: a missing
method on `this` is not flagged (a subclass may provide it), while the
arguments of a method that does exist are still validated.

A typo on a built-in type's method - for example `"x".fooBar()` or
`(42).nope()` - is also an error, checked against the authoritative
built-in method set for each type:

```sh
$ geblang check app.gb
app.gb:1:5: error[semantic]: string has no method fooBar
```

Calling a function that is not defined - not a top-level function,
imported name, class constructor, in-scope variable, or built-in - is an
error as well:

```sh
$ geblang check app.gb
app.gb:1:1: error[type]: unknown bytecode function notAThing
```

A bare type name in an annotation - parameter, return, field, variable, generic
argument, nullable, union, catch clause, or `as` cast - that resolves to no
known type (primitive, declared class, interface, enum, type alias, in-scope
generic type param, or built-in error class) is an error, at both check and
compile time, so a typo in a type hint is caught before you run:

```sh
$ geblang check app.gb
app.gb:1:8: error[semantic]: unknown type "Reqeust" in parameter req of function handle
```

A module-qualified type name (`mod.TypeName`) whose module is a resolved import
but does not export that name is also flagged, by `geblang check`:

```sh
$ geblang check app.gb
app.gb:8:13: error[type]: image has no exported type NopeType
```

`--no-lint` disables warning rules while keeping parse, semantic, import, and
module-layout validation. JSON output includes `severity`, `rule`, `file`,
`line`, `column`, and `message` fields so tools can route errors and warnings
separately.

### What an error means versus a warning

`geblang check` follows one contract so its result lines up with the rest of
the toolchain:

- An **error** is code both execution backends reject. Anything `geblang
  check` reports as an error also fails `geblang test`, `geblang run`, and
  `geblang build`. Fix errors before running anything.
- A **warning** is advisory and never changes whether code runs. By default a
  warning does not affect the exit code; pass `--strict` to make warnings exit
  non-zero in CI.

One warning rule bridges the two execution backends. Geblang runs on a tree-
walking evaluator (`geblang test`) and a bytecode VM (`geblang run`, `geblang
build`). When a construct runs on the evaluator but the bytecode VM cannot
build it yet, `geblang check` reports a `vm-unsupported` warning instead of an
error:

```sh
$ geblang check app.gb
app.gb: warning[vm-unsupported]: bytecode compiler does not support <construct> yet
```

The code is valid and runs under the evaluator, so this is not an error; the
warning tells you `geblang run` / `geblang build` need `--disable-vm` for that
file until the VM gains support. This keeps `geblang check` in agreement with
`geblang test` while still surfacing what would block a release build.

A `match` over an enum that omits a variant and has no `default` is reported as
`match-nonexhaustive`, naming the missing variants:

```sh
$ geblang check app.gb
app.gb:5:12: warning[match-nonexhaustive]: match on enum 'Color' is not exhaustive: missing Blue (add the missing case(s) or a 'default:' case)
```

It is a warning, not an error: the code still runs and throws `MatchError` only
if an unhandled value reaches the match. See chapter 4 for the exhaustiveness
rules (guards do not count as covering a variant).

## Introspection Builtins

Two builtins help inspect values at runtime, on both execution backends:

- `dir(value)` returns the sorted list of method names callable on a value.
- `dump(value)` returns a type-annotated debug string for a value.

```gb
import io;

io.println(dir([1, 2, 3]));   # ["all", "any", "append", ...]
io.println(dump({"a": 1}));   # dict{string("a"): int(1)}
io.println(dump([1, "x"]));   # list[int(1), string("x")]
```

`dir()` with no argument lists the names in the current scope and is available
only in the REPL/evaluator.

`embed(path)` is the other builtin function available without an import; it reads a file at compile time into a string or bytes constant and is documented in chapter 13, [Compile-Time File Embedding](13-bundling.md#compile-time-file-embedding).

## Module Invocation

Executable modules expose `main(args)` and can be launched with `-m`:

```sh
geblang -m app.cli --name Ada
geblang -m http.server 8080
```

This is useful for source-stdlib utilities and package-local tools because it
uses normal module resolution instead of requiring a wrapper file.

## Documentation Website

The reference manual website is generated from `docs/user/*.md`, configured
source API documentation roots, and curated runnable examples from
`docs/examples/`:

```sh
make docs
```

Static HTML is written to `docs/site/`. Open `docs/site/index.html` in a
browser or serve the directory with any static file server. By default,
`make docs` also parses `stdlib/` and adds generated API pages under
`docs/site/api/`. It also parses structured file docblocks from
`docs/examples/**/*.gb` and adds example source pages under
`docs/site/examples/`.

Override the generated API inputs when building project-local docs:

```sh
make docs DOCS_API_SRC="src examples/api_docs.gb"
```

Override the examples tree when building docs for another project:

```sh
make docs DOCS_EXAMPLES_SRC="docs/examples"
```

## Command Help

Use top-level help to list commands:

```sh
geblang --help
geblang help
```

Every command also supports local help with usage examples:

```sh
geblang test --help
geblang check --help
geblang build --help
```

## Shell Completion

Enable bash tab-completion for subcommands:

```sh
source <(geblang completion bash)
```

Add that line to `~/.bashrc` to make it permanent. With completion
enabled, `geblang li<tab>` completes to `geblang licenses`; the first
argument completes against the subcommand list and later arguments
complete filenames. Regenerate after upgrading so the command list
stays current.

## Third-Party Notices

`geblang licenses` prints the assembled third-party attribution text.
On an interactive terminal it pages the output through `$PAGER` (falling
back to `less -R`, then `more`); when piped or redirected it writes
plain text, so `geblang licenses > NOTICES.txt` works unchanged. Use
`--no-pager` to force plain output on a terminal.

## Standalone Executables

Use `geblang build` to package a Geblang application as a self-contained
binary that needs no Geblang installation on the target machine:

```sh
geblang build --entry myapp.main --out ./dist/myapp [<package-dir>]
```

The output binary embeds all source modules and precompiled bytecode. See the
[Bundling chapter](13-bundling.md) for the full package layout, how bundling
works, and complete examples.

## Docker Binary Build

Build a distributable binary and bundled stdlib through Docker:

```sh
make docker-build
```

The target writes:

```text
build/geblang
build/stdlib/
```

This build path only requires Docker and `make` on the host. It is intended for
reproducible binary builds and release packaging. Local development can use
`make build` instead.

To build both the binary and the VS Code extension together:

```sh
make compose-build
```

To build only the VS Code extension:

```sh
make vscode-build
```

See [VS Code Extension](14-vscode-extension.md) for full installation and usage
details.

## Example Scripts

Useful examples:

```sh
geblang examples/core.gb
geblang examples/functions.gb
geblang examples/classes.gb
geblang examples/generics.gb
geblang examples/generators.gb
geblang examples/collections_module.gb
geblang examples/api_docs.gb
geblang examples/markdown.gb
geblang examples/async/file.gb
geblang examples/async/io.gb
geblang examples/async/http.gb
geblang examples/async/sockets.gb
geblang examples/async/streams.gb
geblang examples/async/network_engine.gb
geblang examples/source_web_router.gb
geblang examples/web_decorators.gb
geblang examples/expense_tracker/src/main.gb
```

`examples/http_server.gb` starts a blocking HTTP server on
`127.0.0.1:8080`.

## Project Commands

```sh
make build
make test
make docs
make docker-build
make clean
```

Use `geblang init --name package.name` to create a package manifest.

## Claude Code Skill

The toolchain ships a Claude Code skill that equips Claude to author and operate
Geblang: the language idioms and gotchas, the standard-library surface, and the
`geblang` CLI (run, test, check, fmt, build, doc). It is bundled at
`.claude/skills/geblang/` as a top-level `SKILL.md` plus reference files for the
language, the stdlib, and the toolchain.

Claude Code discovers it automatically as a project skill when you open a
checkout of the toolchain repository. To make it available in any project, copy
it into your user skills directory:

```sh
cp -r .claude/skills/geblang ~/.claude/skills/geblang
```

The skill directs Claude to confirm exact signatures with `geblang doc` and
`geblang help` rather than guessing, so it stays accurate against your installed
version.
