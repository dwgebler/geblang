# Testing

Geblang ships a built-in test runner and an assertion framework in the
`test` module. Tests are ordinary Geblang classes that extend `test.Test`
and mark methods with the `@test` decorator. The CLI command `geblang test`
discovers `*_test.gb` files, instantiates each test class, and runs every
`@test` method.

Tests run on the bytecode VM by default, the same backend `geblang build`
ships, so a test that passes cannot hide a VM-only regression. See "Which
backend runs your tests" below for the runtime flag and the escape hatch for
a documented, temporarily-accepted divergence.

## A first test

```gb
import test;

class CalculatorTest extends test.Test {
    @test
    func adds(): void {
        this.assertEquals(4, 2 + 2);
    }

    @test
    func subtracts(): void {
        this.assertEquals(0, 2 - 2);
    }
}
```

Save as `tests/calculator_test.gb` and run:

```sh
geblang test tests/calculator_test.gb
```

You can pass either a single file or a directory. Directory paths are
walked recursively for files matching `*_test.gb`.

## Assertions

`test.Test` exposes a fluent set of assertion methods. Each raises an
error with a descriptive message on failure; the runner counts the
failure and continues to the next method.

| Method | Meaning |
|--------|---------|
| `assertEquals(expected, actual)` | Deep-equal comparison |
| `assertNotEquals(unexpected, actual)` | Inverse |
| `assertTrue(value)` | `value` must be `true` |
| `assertFalse(value)` | `value` must be `false` |
| `assertNull(value)` | `value` must be `null` |
| `assertNotNull(value)` | `value` must not be `null` |
| `assertContains(container, needle)` | String/list/dict/set contains check |
| `assertNotContains(container, needle)` | Inverse |
| `assertEmpty(value)` | Empty string/list/dict/set/range, or `null` |
| `assertNotEmpty(value)` | Inverse |
| `assertGreaterThan(threshold, actual)` | Numeric / string ordering |
| `assertGreaterThanOrEqual(threshold, actual)` | |
| `assertLessThan(threshold, actual)` | |
| `assertLessThanOrEqual(threshold, actual)` | |
| `assertThrows(callable[, substring])` | `callable` must raise; optional message substring |
| `assertThrowsOf(callable, classOrName[, substring])` | `callable` must raise an error of that class |
| `fail([message])` | Unconditional failure |
| `skip([reason])` | Stop and record the method as skipped, not failed |

Equality uses Geblang's value-equality rules: lists, dicts, sets, and
class instances are compared field by field; enum variants compare on
their name and payload. Numbers compare by exact value across `int`,
`decimal`, and `float`.

A few methods have shorter aliases: `assertEqual` for `assertEquals`,
`assertNotEqual` for `assertNotEquals`, `isTrue` / `isFalse` for
`assertTrue` / `assertFalse`, `notNull` for `assertNotNull`, and
`equal(actual, expected)` (note the argument order: actual first).
Prefer the `assert*` forms in new tests for consistency.

### Asserting that code raises

`assertThrows` takes a zero-argument callable and fails unless it
raises. Pass a second string argument to also require the error
message contain a substring. `assertThrowsOf` additionally checks the
error class, walking the parent chain like a `catch` clause. The class
is given as a class value or a name string, so built-in error classes
work without reifying them:

```gb
import test;

func divide(int a, int b): int {
    if (b == 0) {
        throw RuntimeError("division by zero");
    }
    return a / b;
}

class DivideTest extends test.Test {
    @test
    func rejectsZeroDivisor(): void {
        this.assertThrows(func(): int { return divide(1, 0); });
        this.assertThrows(func(): int { return divide(1, 0); }, "by zero");
        this.assertThrowsOf(func(): int { return divide(1, 0); }, "RuntimeError");
        this.assertThrowsOf(
            func(): int { return divide(1, 0); }, "RuntimeError", "by zero");
    }
}
```

## Skipping a test

Call `this.skip()` (optionally with a reason) to abandon the current
method without failing it. The runner records it as skipped and moves
on. Statements after `skip` do not run:

```gb
class PlatformTest extends test.Test {
    @test
    func usesAFeatureNotAlwaysPresent(): void {
        if (!featureAvailable()) {
            this.skip("feature not available on this host");
        }
        this.assertTrue(useFeature());
    }
}
```

Skipped methods appear as `SKIP` in verbose output and are counted
separately in the summary line (see Running tests in CI below).

## Setup and teardown

`test.Test` recognises four optional lifecycle hooks. Override any of them
on your subclass:

```gb
class DatabaseTest extends test.Test {
    int conn;

    func setupClass(): void {
        # runs once before any @test method on this class
    }

    func setup(): void {
        # runs before every @test method
    }

    func teardown(): void {
        # runs after every @test method, even if it failed
    }

    func teardownClass(): void {
        # runs once after the last @test method
    }

    @test
    func selectsRows(): void {
        # ...
    }
}
```

## Mocking and patching

The `test` module can swap stdlib functions for the duration of a
single `@test` method. `test.mock(moduleName, replacements)` takes a
module name and a dict mapping function names to replacement
callables; calls to those functions, including from code nested deep
inside the unit under test, see the replacement.

```gb
import test;
import datetime;

class ClockTest extends test.Test {
    @test
    func usesAFixedTime(): void {
        test.mock("datetime", {"nowUnix": func(): int { return 1700000000; }});
        this.assertEquals(1700000000, datetime.nowUnix());
    }
}
```

Patches are scoped to the method that installs them. They roll back
automatically before the next method runs, so one test cannot leak a
mock into another:

```gb
@test
func seesRealTimeAgain(): void {
    # the patch from the previous method is already gone
    this.assertTrue(datetime.nowUnix() > 1_000_000_000);
}
```

Replacement callables receive the real arguments, so a mock can return
different values per input:

```gb
test.mock("crypt", {
    "sha256": func(string s): string {
        if (s == "key") { return "matched"; }
        return "default";
    }
});
```

To clear patches before the method ends, use `test.restore(moduleName,
fname)` to drop a single patch or `test.restoreAll()` to clear every
active patch. Mocking a module or function that was never registered
is harmless: the patch simply sits unused.

| Function | Meaning |
|----------|---------|
| `test.mock(module, {name: callable, ...})` | Patch one or more functions on a module |
| `test.restore(module, name)` | Remove a single patch |
| `test.restoreAll()` | Clear every active patch |

## Tags and selective runs

Decorate `@test` methods with `@tag("name")` to put them into a category:

```gb
class WebTest extends test.Test {
    @tag("integration")
    @test
    func talksToAServer(): void {
        # ...
    }

    @test
    func parsesAUrlOffline(): void {
        # ...
    }
}
```

Then on the command line:

```sh
geblang test --tag integration tests/
geblang test --tag integration --tag slow tests/
```

`geblang test` runs only methods that carry at least one of the supplied
tags. Without `--tag` it runs every `@test` method.

To narrow a run further, `--class ClassName` restricts to one test
class and `--method methodName` restricts to one method (repeat
`--method` for several). These combine with `--tag` and with each
other:

```sh
geblang test --class UserTest --method login tests/user_test.gb
```

## Data-driven tests

There is no separate parameterized-test decorator; a plain loop over a
table of cases inside one `@test` method does the job. Each iteration
asserts, and a failure reports which row failed via the assertion
message:

```gb
class LengthTest extends test.Test {
    @test
    func countsCharacters(): void {
        let cases = [
            {"input": "abc", "expected": 3},
            {"input": "", "expected": 0},
            {"input": "hello", "expected": 5}
        ];
        for (row in cases) {
            let input = row["input"] as string;
            this.assertEquals(row["expected"] as int, input.length());
        }
    }
}
```

## Verbose output

Pass `--verbose` (or `-v`) to print each test class and method as it
runs, with `PASS` or `FAIL` per case. This is similar to PHPUnit's
testdox format:

```sh
geblang test --verbose tests/
```

```text
FunctoolsTest
  PASS pipeLeftToRight
  PASS pipeIdentityForEmpty
  PASS composeRightToLeft
  SKIP memoizeOnHugeInput: skipped under CI
  FAIL memoizeLruEvictsOldest: expected 4, got 3
tests: total=8 failed=1 passed=6 skipped=1
```

Without `--verbose`, only the failure lines and the summary are
printed.

## Capturing standard streams from a test

Tests that exercise code which writes to stdout, stderr, or reads
from stdin can intercept those streams in-process. The capture
helpers are evaluator-local; they do not touch the real terminal.

```gb
import io;
import test;

class GreetTest extends test.Test {
    @test
    func writesGreeting(): void {
        let capture = io.captureStdout();
        greet("Ada");
        let text = io.toString(capture);
        io.close(capture);
        this.assertTrue(text.contains("Hello, Ada"));
    }
}
```

`io.captureStdout()` and `io.captureStderr()` redirect the named
stream into a memory buffer that you read with `io.toString`. Closing
the capture restores the previous target. For more control, the
lower-level `io.redirectStdout(stream)` / `redirectStderr` /
`redirectStdin` family returns a `restore` callable; pair them with
`defer restore();` when a test temporarily swaps a stream.

See `docs/user/stdlib/01-io.md` (Streams And Capture) for the
complete API and a worked memory-stream example.

## Which backend runs your tests

Geblang has two execution backends: the bytecode VM (used by `geblang run` and
`geblang build`) and the tree-walking evaluator. `geblang test` runs the VM by
default, so the tests gate the same backend a release ships.

A test file the VM cannot run fails loudly, naming the file and the gap, instead
of silently falling back to the evaluator. This is deliberate: a green test run
must mean the code works on the backend it ships on.

To run the whole suite on the evaluator instead, pass `--runtime=evaluator`
(the alias `--disable-vm` also works):

```sh
geblang test --runtime=evaluator tests/
```

### Accepted divergences (escape hatch)

A few constructs behave differently on the two backends by design, or hit a VM
gap that is documented and temporarily accepted. Such a file may opt out of the
VM lane with a comment naming a key:

```gb
# @vm-divergence: partial-overload-resolution
import test;
# ...
```

and a matching row in a `KNOWN_DIVERGENCES.md` ledger placed alongside the tests:

```text
| key | file | reason | date |
|-----|------|--------|------|
| partial-overload-resolution | functions/partials_test.gb | reason, one line | 2026-07-06 |
```

An annotated, ledgered file runs on the evaluator (it still runs; it is not
skipped) and the summary reports how many files did so. The runner
cross-validates both directions: an annotation with no ledger row, or a ledger
row whose file lacks the annotation, is an error. Keep the ledger empty whenever
possible; an entry is a promise to fix the gap, not a permanent waiver.

## Running the suite from `make`

If your project uses a Makefile, add:

```makefile
test-lang: build
	./geblang test tests/                       # VM (default, authoritative)

test-lang-eval: build
	./geblang test --runtime=evaluator tests/   # evaluator lane
```

and depend `make test` on both lanes plus the Go test target. The Geblang
reference repo's own `Makefile` is a working example.

## Running tests in CI

`geblang test` exits with code 0 if every test passes, 1 if any test
fails, and 2 on usage errors. The summary line prints
`tests: total=<N> failed=<M> passed=<P> skipped=<S>` on stdout, so CI
scripts can both rely on the exit code and parse the summary if they
need it. Skipped tests do not cause a non-zero exit. When one or more
files ran on the evaluator via an accepted divergence, an
`accepted divergences: <N> file(s) ran on the evaluator` line precedes
the summary.

For JetBrains IDE test runners, `--format teamcity` emits
`##teamcity[...]` service messages instead of the plain summary.

Tests that load native libraries through `ffi` / `clib.*` need the
FFI permission, exactly like `geblang run`:

```sh
geblang test --allow-ffi 'libzstd.so.*' tests/
geblang test --allow-ffi '*' tests/        # allow everything (CI sandbox)
```

Without it, such tests fail with a "could not load native library"
error naming the candidates that were tried.

## Test layout convention

The reference project structure groups tests by feature area:

```
tests/
  core/            # syntax, control flow, operators
  types/           # narrowing, aliases, optional
  generics/        # generic functions, reified runtime checks
  classes/         # inheritance, interfaces, immutability
  functions/       # closures, overloading, decorators
  async_generators/
  errors/
  regex/
  stdlib/          # one file per stdlib module
  check/           # files that must fail `geblang check`
```

Files under `check/` are not part of the regular `geblang test` run; they
exercise `geblang check`'s static diagnostics. The reference `Makefile`
has a `check-lang` target that drives a script which asserts every file
under `tests/check/` produces a diagnostic while every other test file
checks clean.

## Testing private module members

Module members without `export` are private: importing code cannot see
them. To test them without exporting implementation details, declare the
SAME module name in the test file and place it alongside the module
source. `geblang test` then runs the test file inside the module, so
private functions, classes, constants, and module-level state are
directly visible:

```gb
# users.gb
module app.users;

func normalizeId(string id): string {
    return id.trim().lower();
}

export func findUser(string id): ?User { ... }
```

```gb
# users_test.gb
module app.users;
import test;

class NormalizeTest extends test.Test {
    @test
    func trimsAndLowers(): void {
        this.assertEquals("abc", normalizeId("  ABC  "));
    }
}
```

The convention follows Go's same-package tests. The rules:

- The test file name must end in `_test.gb` and declare exactly the
  module name its sibling module file declares.
- The private view exists only under `geblang test`. Everywhere else
  (`run`, `build`, importing code) private members stay private.
- `geblang check` and the editor understand the convention, so private
  references in a same-module test file check clean.
- A test file that declares a module name with no matching module source
  is an error.

Tests without a module declaration keep working as before: they import
the module under test and exercise its exported surface.

## Reified generics in tests

The runtime enforces declared element types for `list<T>`, `set<T>`, and
`dict<K,V>`. A test can confirm the enforcement:

```gb
@test
func listRejectsWrongElement(): void {
    let threw = false;
    try {
        list<int> bad = [1, "two", 3];
    } catch (RuntimeError e) {
        threw = true;
    }
    this.assertTrue(threw);
}
```

These tests act as guards against regressions in the type checker (the
static side) and the reified-generics runtime (the dynamic side).
