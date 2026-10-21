---
name: geblang
description: >-
  Authoring, running, testing, checking, formatting, building, and bundling
  Geblang (.gb) programs. Use this whenever you write or modify Geblang source,
  write or run Geblang tests, or operate the `geblang` CLI (run / test / check /
  fmt / build / doc / install / bundle). Geblang is a statically-typed scripting
  language implemented in Go with a tree-walking evaluator and a bytecode VM.
  Trigger it even when the user only says "this .gb file", mentions Geblang, or
  shows code with `func`, `import io;`, `: int` return types, or `${...}`
  interpolation. Geblang's semantics diverge sharply from Python/JS/PHP/Go
  (`//` is integer division not a comment, `/` yields decimal, `parent()` not
  `super`, in-place list mutators, decimal-default float literals), so do NOT
  rely on priors from other languages - use this skill to stay correct.
---

# Geblang

Geblang is a statically-typed scripting language implemented in Go. Source
files end in `.gb`. It runs on two backends that must produce identical output:
a tree-walking evaluator (used by `geblang test`) and a bytecode VM (the default
for `geblang run` / `geblang build`). Geblang targets backend services, CLIs,
scripts, and tests.

This skill keeps you accurate. Geblang borrows surface syntax from PHP, Python,
and Go, but its semantics diverge in ways that silently break code written from
memory. Read the gotchas below before writing, and ground exact APIs against the
installed toolchain rather than guessing.

## Ground before you write

The reference files here are a map, not the whole API. The installed `geblang`
binary is the source of truth and always matches the user's version:

- `geblang doc <file-or-dir>` prints exact signatures from doc comments.
- `geblang help <topic>` documents a CLI command (topics: `run`, `build`,
  `test`, `check`, `fmt`, `init`, `install`, `doctor`, `doc`, `cache`, ...).
- `dir(value)` lists a module's or value's members at runtime; `typeof(x)` and
  `dump(x)` introspect a value.
- Unsure whether something compiles? Write a five-line script and run
  `geblang check file.gb` (static) or `geblang file.gb` (execute). Both backends
  are fast on small programs and errors name the line and the missing piece.

Never assume a name, dunder, or convention from another language. The
string-cast hook is `__string` (not `toString`); the parent call is `parent()`
(not `super`). Verify the name first, then write.

## Critical gotchas (these differ from other languages)

1. **`//` is integer (floor) division, NOT a comment.** Comments are `#` (line)
   and `/* ... */` (block). Writing `// note` is a silent bug.
2. **`/` is true division and yields `decimal` (or `float`)**, even for
   evenly-divisible integers. `int n = a / b;` is a COMPILE ERROR
   (`cannot assign decimal to int`). Use `//` for an integer result, or
   `(a / b) as int` to truncate.
3. **Floating literals are `decimal` by default.** `let x = 1.0;` infers
   `decimal`. Use the `f` suffix (`1.0f`) or `as float` for IEEE `float`. Mixed
   `decimal`/`float` arithmetic does not coerce and is a `check` error when both
   types are statically known - cast explicitly.
4. **`parent(args)` calls the parent constructor; `parent.method(args)` calls a
   parent method.** `super` is not a keyword.
5. **List mutators are in-place and return the receiver** (`push`, `pop`,
   `insert`, `remove`, `removeAt`, `prepend`, `reverse`, `sort`, `sortBy`,
   `fill`). `let ys = xs.sort();` leaves `xs` AND `ys` as the same sorted list.
   Use `sorted()` / `reversed()` / `copy()` / `deepCopy()` for copies.
6. **`as` is a CHECKED cast that throws on mismatch, not a type test.** Test with
   `instanceof`, then access with `as`:
   `if (v instanceof Foo) { (v as Foo).m(); }`.
7. **String interpolation only works inside double quotes:** `"hi ${name}"`.
   Single quotes are raw (no `${}`, no escapes). Concatenation does not coerce:
   `"n=" + 5` is an error - interpolate or cast.
8. **`typeof(x)` needs parentheses.** Dict iteration is insertion order, not
   sorted. A bare `{}` is a dict; `set<string> s = {};` (a set-typed variable,
   parameter default or field) is an empty set.
9. **Do not name a source file the same as a stdlib module it imports** - the
   resolver picks the local file by filename.
10. **`async.sleep(ms)` returns a Task and does not block on its own.** Write
    `await async.sleep(ms);` (or `async.await(async.sleep(ms))`) to pause;
    an un-awaited call returns immediately.
11. **`type` is a keyword** (type aliases: `type UserId = string;`), so it
    can't be a variable or parameter name.

## CLI workflow

| Command | Use |
|---|---|
| `geblang <script.gb> [args]` or `geblang run <script.gb> [args]` | Run a script (VM, evaluator fallback). |
| `geblang test <path>` | Discover and run `*_test.gb` files (evaluator). |
| `geblang check [--strict] <path>` | Parse + type-check + lint, no execution. Run on code you write. |
| `geblang fmt <file.gb> ...` | Format in place. `--clean` = minimal form; `--strip-comments` drops comments. |
| `geblang doc <path>` | Print exact API signatures. |
| `geblang build --entry <module> --out <path> [--native] [--docker]` | Bundle a package into a self-contained binary. `--entry` is a MODULE name whose `export func main(...)`, not a file. |
| `geblang init` / `geblang install [git-url[@ver]]` | Scaffold `geblang.yaml` / fetch dependencies. |
| `geblang doctor` / `geblang cache clean` | Diagnose the install / purge the bytecode cache. |
| `geblang repl` | Interactive session. |

After writing or changing Geblang code: run `geblang check` on it, `geblang fmt`
it, and run the relevant `geblang test` suite. If a change is subtle enough to
behave differently on the two backends, run it under both - `geblang file.gb`
(VM) and `geblang --disable-vm file.gb` (evaluator) - and confirm identical
output. A divergence is a bug to report, not to work around.

## Reference files

Read the one that matches your task:

- **`references/language.md`** - full syntax and semantics: types, collections,
  control flow, functions (defaults / named / variadic / spread / partial
  application), generics, classes, operator dunders, interfaces, enums, errors,
  async, generators, reflection, decorators.
- **`references/stdlib.md`** - the standard-library module surface and what each
  module is for. Always confirm exact signatures with `geblang doc` /
  `dir(module)`.
- **`references/toolchain.md`** - the full CLI reference, the `geblang.yaml`
  manifest, building and bundling (including `--native` and `--docker`), launch
  capabilities (`--allow-ffi` and friends), and writing tests in depth.
