# Geblang toolchain reference

The `geblang` CLI. Run `geblang help <topic>` for the authoritative, version-
matched text. Verified against Geblang 1.27.0.

## The two backends

Every program that compiles must produce identical output on both:

- the **tree-walking evaluator** - used by `geblang test`;
- the **bytecode VM** - the default for running a script and for `geblang build`.

Running a script attempts the VM and falls back to the evaluator on a construct
the VM cannot compile yet. `geblang --vm-strict` fails instead of falling back;
`geblang --disable-vm` forces the evaluator; `geblang --trace-exec` prints which
engine ran. If the two backends ever disagree, that is a bug to report.

## Running code

```sh
geblang app.gb [args...]            # run (VM, evaluator fallback)
geblang run app.gb [args...]        # explicit alias for the above
geblang --disable-vm app.gb         # force the evaluator
geblang --vm-strict app.gb          # VM only, no fallback
geblang --no-assert app.gb          # elide assert(...) (args not evaluated)
geblang -m app.main [args...]       # run a module's exported main()
geblang repl                        # interactive session
```

`args...` are available to the program via `sys.args()`.

## geblang check (static analysis)

```sh
geblang check [--json] [--no-lint] [--strict] <file-or-dir>
```

Parses, type-checks, and lints without executing. The contract:

- **error** = code both backends reject (also fails `test` / run / `build`).
- **warning** = advisory; never changes whether code runs; exits non-zero only
  under `--strict`. A `warning[vm-unsupported]` means valid evaluator code the VM
  cannot build yet (needs `--disable-vm` for a release build).

Run `geblang check` on code you write, and fix every diagnostic (including
pre-existing ones in files you touch).

## geblang fmt (formatting)

```sh
geblang fmt <file.gb> ...           # format in place (canonical style)
geblang fmt --clean <file.gb>       # minimal canonical form (drops redundant parens, flattens)
geblang fmt --strip-comments <file> # remove all comments
```

`fmt` re-parses its own output and refuses to write anything that is not
AST-identical to the input, so it can never change a program's meaning.

## geblang test and the test framework

```sh
geblang test <file-or-dir>          # discover and run *_test.gb (recursively)
geblang test --tag integration <p>  # only @tag("integration") methods (repeatable)
geblang test --class UserTest --method login <p>
geblang test --verbose <p>          # per-method PASS/FAIL (testdox)
geblang test --format teamcity <p>  # JetBrains service messages
```

Write tests by subclassing `test.Test` and marking methods with `@test`:

```gb
import test;

class CalculatorTest extends test.Test {
    func setup(): void { /* before each test method */ }
    func teardown(): void { /* after each test method */ }

    @test
    func addsTwoIntegers(): void {
        this.assertEquals(4, 2 + 2);
    }

    @test
    @tag("integration")
    func throwsOnDivideByZero(): void {
        this.assertThrows(func(): int { return divide(1, 0); }, "by zero");
        this.assertThrowsOf(func(): int { return divide(1, 0); }, "RuntimeError");
    }
}
```

The runner discovers `Test` subclasses and runs each `@test` method; there is no
manual `test.run()`. Lifecycle hooks: `setup` / `teardown` (per method) and
`setupClass` / `teardownClass` (per class).

Asserters on `test.Test` (each throws an `AssertionError` on failure):
`assertEquals`, `assertNotEquals`, `assertTrue`, `assertFalse`, `assertNull`,
`assertNotNull`, `assertContains`, `assertNotContains`, `assertEmpty`,
`assertNotEmpty`, `assertGreaterThan`, `assertGreaterThanOrEqual`,
`assertLessThan`, `assertLessThanOrEqual`, `assertThrows(callable[, substring])`,
`assertThrowsOf(callable, classOrName[, substring])`. Call `this.skip([reason])`
to record a method as skipped (not failed).

- `test.mock(module, {name: callable, ...})` patches stdlib functions for the
  duration of a test.
- **Testing private members**: declare the SAME module name in a sibling
  `*_test.gb` file. The test then runs inside the module and sees its private
  functions, classes, consts, and state. A test file with no `module`
  declaration instead exercises the module's exported surface via `import`.

Every Geblang feature you build needs a Geblang-level test here (and, in the
engine, a backend-parity test); neither substitutes for the other.

## Packages: geblang.yaml, init, install

A package has a `geblang.yaml` manifest:

```yaml
name: myapp
version: 0.1.0
source: src
paths: []
dependencies:
  somelib:
    path: ../somelib          # path dep (absolute, ~, $VAR also accepted)
  httplib:
    git: github.com/acme/httplib@v1.2.0   # git dep (scheme-less resolves to https)
permissions:
  ffi: true                   # bake capabilities into built binaries
  onnx: true
  processControl: true
```

```sh
geblang init [--name n] [--source dir]   # scaffold geblang.yaml
geblang install                          # fetch all deps into vendor/, pin geblang.lock
geblang install <git-url>[@version] [<name>]   # add + fetch one dependency
geblang doc <file-or-dir> [--format markdown|json] [--out file]   # API docs
```

Source files live under `source` (default `src/`); tests are `*_test.gb` next to
source. Commit `geblang.lock` for reproducible builds; `vendor/` may be
gitignored and regenerated.

## geblang build (bundle to a binary)

```sh
geblang build --entry <module> --out <path> [--no-assert] [--native] \
              [--docker] [--docker-port N] [--force] [<package-dir>]
```

- `--entry` is a canonical MODULE name whose `export func main()` or
  `export func main(list<string> args)` (optionally `: int` exit code) is the
  entry point - NOT a file path. A missing/incompatible main is a build error.
- The default build bundles reachable source, precompiled bytecode, the source
  stdlib, and vendored deps into a self-contained binary needing no separate
  install.
- `--native` (EXPERIMENTAL) transpiles to a standalone native Go binary for a
  speedup on compute-heavy code. It supports a growing subset of the language and
  fails the build with a clear diagnostic on anything unsupported (never emitting
  a binary that behaves differently). The supported set is unstable; use the
  plain build for the full language.
- `--docker` writes a ready-to-build Dockerfile (distroless, NOTICES included)
  beside the binary; `--docker-port N` adds `EXPOSE N`.
- Built binaries answer `--help`, `--version`, and `--notices`; `--` passes
  everything after it to the application.

## Launch capabilities (default-deny)

Privileged native operations require an opt-in flag at launch, or a
`geblang.yaml` `permissions:` entry baked into a built binary. A gated call
without permission throws `PermissionError`.

```sh
geblang --allow-ffi <path-or-glob> app.gb   # call C libraries (ffi, clib.*)
geblang --allow-process-control app.gb       # setuid/setgid, kill/signal by pid
geblang --allow-onnx app.gb                   # local ONNX model inference
geblang --allow-browser app.gb                # headless-browser automation
```

## FFI binding generator

```sh
geblang bind [--out file] <manifest.yaml>
```

Generates a Geblang module wrapping a C-ABI shared library. Manifest sections:
`module`, `library`, `doc`, `constants`, `structs`, `functions`. Types: `VOID`,
`INT8..INT64`, `UINT8..UINT64`, `FLOAT`, `DOUBLE`, `PTR`, `CSTRING`, `BYTES`.

## Profiling (finding where time goes)

Measure before optimising, and start with the Geblang-level profile:

```sh
geblang run --profile profile.txt app.gb args   # or GEBLANG_PROFILE=profile.txt for any run / built binary
```

`profile.txt` ranks Geblang functions and lines by samples (about one a
millisecond), and `profile.folded` beside it feeds a flame graph. Time in a
native call (`secureRandom`, `json`, a query) counts towards the Geblang
function that made it.

When that profile does not explain the time, profile the engine itself
(1.41.1+). Typical signs: many workers barely faster than one, time spread
evenly over everything, or a run much slower than its hot functions suggest.

```sh
geblang run --go-profile engine.pprof app.gb args   # or GEBLANG_GO_PROFILE=engine.pprof
go tool pprof -top engine.pprof             # CPU inside the engine: GC, memclr, dispatch
go tool pprof -top engine.mutex.pprof       # lock contention between workers
go tool pprof -top engine.heap.pprof        # live heap at exit
go tool pprof -peek 'someFunc' engine.pprof # who calls a hot engine function
```

For a long-running server, `GEBLANG_PPROF=localhost:6060` serves Go's
`net/http/pprof` endpoint instead (bind to localhost only; unauthenticated).

Draw-heavy code: each `secureRandom` session draw has a fixed cost (message,
HMAC, audit entry), so take many values in one draw with
`secureRandom.randomInts(s, count, min, max)` (1.41.1+) rather than a loop of
single draws.

## Diagnostics, cache, IDE

```sh
geblang doctor [--json]              # tooling / manifest / cache health
geblang cache stats [--json]         # bytecode cache size + entries
geblang cache clean                  # purge .geblang-cache/ (use when bytecode looks stale)
geblang lsp                          # Language Server (stdio)
geblang dap                          # Debug Adapter (stdio)
geblang completion bash              # shell completion: source <(geblang completion bash)
```
