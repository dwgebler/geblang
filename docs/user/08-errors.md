# Errors

## Throwing

Only objects extending `Error` can be thrown.

```gb
throw RuntimeError("failed");
```

Throw errors when the caller can reasonably recover or report a domain-specific
failure. Use return values for ordinary negative results such as "not found"
when absence is expected.

## Catching

```gb
try {
    risky();
} catch (IOError e) {
    io.println("io failed: " + e.message);
} catch (Error e) {
    io.println("unexpected: " + e.message);
} finally {
    cleanup();
}
```

`finally` runs after the `try`/`catch` path completes.

Catch clauses are checked in order. Put specific error classes before broader
base classes. `catch (Error e)` acts as a catch-all for any throwable error.
An untyped `catch { ... }` block also catches everything but does not bind the
error value.

Geblang converts recoverable host/runtime failures into language-level
exceptions. For example, file and network failures such as a missing file,
connection refusal, or trying to bind a port that is already in use are raised
as `IOError` and can be caught. Unrecoverable interpreter and bytecode integrity
failures still abort execution because they indicate an implementation or
corrupt-program problem rather than an application error.

Two `IOError` subclasses let you branch on the kind of network failure:
`TimeoutError` (a request or dial deadline was exceeded) and `TlsError` (TLS
handshake or certificate verification failed). Because they extend `IOError`,
`catch (IOError e)` still catches both; catch them specifically to, for example,
retry on a timeout but fail fast on a bad certificate:

```gb
try {
    let r = http.get(url);
} catch (TimeoutError e) {
    /* the request timed out - retry */
} catch (TlsError e) {
    /* untrusted endpoint - do not retry */
} catch (IOError e) {
    /* other connection failure (refused, DNS, reset) */
}
```

```gb
import io;
import net;

let listener = net.listenTcp("127.0.0.1:0");
let addr = net.localAddr(listener);

try {
    let other = net.listenTcp(addr);
    net.close(other);
} catch (IOError e) {
    io.println("could not bind: " + e.message);
} finally {
    net.close(listener);
}
```

## Custom Error Classes

Define application error classes by extending `Error` or any built-in error
subclass:

```gb
class AppError extends Error {}
class NetworkError extends RuntimeError {}

try {
    throw AppError("something failed");
} catch (NetworkError e) {
    io.println("network: " + e.message);
} catch (AppError e) {
    io.println("app: " + e.message);
} catch (Error e) {
    io.println("other: " + e.message);
}
```

User error classes take an optional string message argument:

```gb
throw AppError();                     # no message
throw AppError("connection refused"); # with message
```

Caught error values expose `class` and `message` fields:

```gb
} catch (AppError e) {
    io.println(e.class + ": " + e.message);
}
```

Error class inheritance is fully hierarchical. A class extending `RuntimeError`
is also caught by `catch (Error e)`:

```gb
class DatabaseError extends RuntimeError {}
class QueryError extends DatabaseError {}

try {
    throw QueryError("syntax error near SELECT");
} catch (DatabaseError e) {
    io.println("db problem: " + e.message);
}
```

### Custom fields on error classes

Error classes can declare fields and a constructor. The constructor calls
`parent(msg)` to set the error message and then sets any custom fields on
`this`:

```gb
class HttpError extends RuntimeError {
    int code;
    func HttpError(int code, string msg) {
        parent(msg);
        this.code = code;
    }
}

try {
    throw HttpError(404, "page not found");
} catch (HttpError e) {
    io.println(e.code as string);    # 404
    io.println(e.message);           # page not found
}
```

Custom fields are accessible on the caught error by name just like `message`
and `class`.

## Built-In Error Classes

| Class | Description |
|-------|-------------|
| `Error` | Base class for all errors |
| `RuntimeError` | General runtime failures |
| `TypeError` | Runtime type mismatches, including operands, casts, typed boundaries, and index types |
| `ValueError` | Invalid values, including malformed conversions and out-of-range indices |
| `IOError` | File and network I/O errors |
| `ParseError` | Parsing failures |
| `MatchError` | Non-exhaustive match |
| `AssertionError` | Failed `assert(...)` (1.6.0) |
| `ImmutableError` | Mutation of a frozen value (whole-class `@immutable`, `@dataclass(frozen: true)`, or a set-once field after construction) (1.12.0) |
| `FatalError` | Unrecoverable fault; never caught (1.7.0) |

## Catchable Errors vs FatalError (1.7.0)

Errors come in two tiers:

- **Catchable `Error`** (and every class above it - `RuntimeError`,
  `IOError`, `ValueError`, `TypeError`, `AssertionError`, plus your own
  error classes). This includes *implicit* runtime faults: division by
  zero, index out of range, key-not-found, conversion failures like
  `"abc".toInt()`, and null access. They are caught by `try`/`catch`
  identically on the evaluator (`geblang test`) and the bytecode VM
  (`geblang run` / `geblang build`).

```gb
try {
    let n = userInput.toInt();   # may fault on bad input
} catch (Error e) {
    io.println("not a number: ${e.message}");
}
```

- **`FatalError`** is its own tier - it is *not* an `Error`
  (`x instanceof Error` is false) and **no `try`/`catch` intercepts it,
  not even `catch (any e)`**. It always unwinds to the top and
  terminates the program. It is reserved for unrecoverable conditions:
  raise one with `throw FatalError("message")` when continuing would be
  meaningless. Stack-overflow (exceeding the maximum call depth) is also
  a `FatalError`.

```gb
if (configMissing) {
    throw FatalError("config not found; cannot start");
}
```

## The `assert` Builtin (1.6.0)

`assert(cond)` and `assert(cond, message)` are top-level builtins that
throw `AssertionError` when `cond` is `false`. They are the idiomatic
way to express "this must be true at this point in the program" - both
for debugging help and for shipped runtime invariants.

```gb
func transfer(int amount, Account from, Account to): void {
    assert(amount > 0, "amount must be positive");
    assert(from.balance >= amount, "insufficient funds");
    from.balance = from.balance - amount;
    to.balance   = to.balance + amount;
}
```

The condition must be a `bool`, like every other Geblang condition.
When the optional `message` is omitted, the thrown error includes the
source text of the condition expression so failures are
self-describing:

```gb
assert(1 == 2);
# AssertionError: assertion failed: (1 == 2)
```

`AssertionError` is a direct subclass of `Error`, sibling to
`RuntimeError`, so it is catchable both specifically and generically:

```gb
try {
    assert(invariantHolds());
} catch (AssertionError e) {
    log.error("invariant violated: " + e.message);
}
```

### Disabling at runtime

Both `geblang <script>` and `geblang build` accept a `--no-assert`
flag that elides every `assert(...)` call site at compile time. With
the flag set, **the condition and the message are not evaluated**, so
the call has truly zero runtime cost (and any side effects inside the
arguments are lost - assertions should never have side effects).

```sh
geblang --no-assert myapp.gb
geblang build --entry app.main --out app --no-assert
```

`geblang test` always runs assertions; the flag is intentionally not
honoured there.

## The `errors` Module

```gb
import errors;

# Check error class hierarchy
let err = AppError("bad input");
io.println(errors.class(err));            # AppError
io.println(errors.message(err));          # bad input
io.println(errors.is(err, "Error"));      # true
io.println(errors.is(err, "TypeError"));  # false

# Create an error from a class name string
let dyn = errors.new("AppError", "dynamic");

# Wrap an error with additional context
let wrapped = errors.wrap(err, "request failed");
io.println(wrapped.message);  # request failed: bad input
```

`errors.is` performs a full hierarchy-aware check. It traverses user-defined
class chains as well as the built-in error hierarchy.

### Structured stack traces

Caught errors keep stack information. Use `errors.stackTrace(e)` or
`e.stackTrace()` to get an `errors.StackTrace` value instead of parsing the
formatted uncaught-error text yourself:

```gb
import errors;
import io;

func inner(): void {
    throw RuntimeError("boom");
}

func outer(): void {
    inner();
}

try {
    outer();
} catch (RuntimeError e) {
    let frames = e.stackTrace();
    let first = frames.first();

    io.println(errors.hasStackTrace(e)); # true
    io.println(frames.length() > 0);     # true
    io.println(first.function());        # inner
    io.println(first.line() > 0);        # true
}
```

`errors.StackTrace` methods:

| Method | Returns | Description |
|--------|---------|-------------|
| `frames()` | `list<errors.Frame>` | Structured frame values |
| `length()` | `int` | Number of frames |
| `first()` | `errors.Frame|null` | Innermost frame, or `null` when empty |
| `toList()` | `list<dict>` | Plain dictionaries with `function` and `line` |
| `toString()` | `string` | Original formatted stack text |

`errors.Frame` methods:

| Method | Returns | Description |
|--------|---------|-------------|
| `function()` | `string` | Function name, or `<top level>` |
| `line()` | `int` | Source line when available, otherwise `0` |
| `toDict()` | `dict` | `{function, line}` |
| `toString()` | `string` | Human-readable single frame |

Method frames are qualified with their declaring class as `Class.method`
(an inherited method reports the class that declared it, not the subclass
instance). Plain functions report their bare name.

For dictionary-oriented code, `errors.frames(e)` is shorthand for
`errors.stackTrace(e).toList()`.

## Runtime Failures

Runtime type mismatches raise `TypeError`. A supported conversion with invalid
content and an out-of-range collection index raise `ValueError`. Other runtime
faults, including division by zero and unknown fields, raise `RuntimeError`.
These classes are siblings under `Error`, so use `catch (Error e)` for broad
recovery and a specific class when handling one failure category. Parse,
semantic, and startup failures are reported directly
because the script has not reached a recoverable runtime point;
stack-overflow and other unrecoverable conditions surface as
`FatalError`, which `try`/`catch` never intercepts.

## Stack Traces

An error that escapes to the top level prints a classed header followed
by one line per stack frame, identically on the evaluator and the
bytecode VM (1.19.0):

```
uncaught ValueError: x too big: 7
  at inner (line 6)
  at middle (line 12)
  at <top level> (line 15)
```

Frames are innermost first. The innermost frame shows the line where the
error happened; every caller frame shows the line of its call site; the
final `<top level>` frame shows where top-level code entered the chain.
Runtime faults use the same shape with their error class. Method
frames are qualified as `Class.method`, anonymous functions render as
`<closure>`, and a deep tail-recursive run collapses into a single
`at f (line 8) [x1000]` entry instead of a thousand identical lines.

Caught errors expose the same information through `errors.StackTrace` and
`errors.Frame`, which is more stable for logging, testing, and tooling than
parsing the displayed uncaught-error output. Failing `geblang test` methods
report the same trace beneath the `FAIL` line.

When you convert low-level failures into domain errors, preserve the useful
message:

```gb
func loadProfile(string path): dict<string, any> {
    try {
        return json.parse(io.readText(path));
    } catch (Error e) {
        throw RuntimeError("could not load profile " + path + ": " + e.message);
    }
}
```
