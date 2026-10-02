# Release Notes

## 1.35.2

### Standard library

- `secureRandom.randomInt(min, max)` returns a cryptographically random
  integer with both ends included, and `secureRandom.randomBytes(n)`
  returns `n` random bytes, with no session needed. Pass a session as the
  first argument (`randomInt(s, min, max)`, `randomBytes(s, n)`) to make
  the same call a provably-fair draw that is logged and replayable.

### Fixes

- `cli.table` sizes columns by displayed width instead of byte length, so
  cells with accented or other multi-byte characters, East Asian wide
  characters, or ANSI styling from `cli.style` / `cli.color` line up and
  the header rule is the right length.

### Tooling

- `geblang fmt` keeps compound assignments (`x += 2`, `d["a"] -= 1`,
  `n ??= 4`, and the rest) as written; previously it rewrote them to the
  expanded form (`x = x + 2`).
- Editor completion and hover cover `secureRandom.randomInt` and
  `secureRandom.randomBytes`.

## 1.35.1

### Fixes

- A union-typed value can be passed on to a parameter declared with the
  same union (`func relay(A|int v) { return lift(v); }`) when running on
  the bytecode VM or `geblang check`; previously this was rejected with
  `lift expects A | int for parameter 'v', got A | int`, while the
  evaluator accepted it. Free functions, static methods, constructors
  and operator methods are all covered.
- A union-typed value passed to a parameter that accepts only some of its
  branches is now checked at runtime on the bytecode VM, matching the
  evaluator. The call is a static error only when no branch can match.
- `math.gcd` and `math.lcm` accept integers of any size; previously an
  operand beyond 64 bits raised `RuntimeError`.

### Tooling

- Go to definition in the language server now resolves local variables,
  function and lambda parameters, loop, `catch`, `with`, destructuring,
  comprehension and `match` bindings, and class fields and methods
  reached through `this`, honouring scope and shadowing. Previously only
  top-level declarations were found.
- `geblang fmt` no longer writes a doubled semicolon after a
  `from module import name;` statement, and indents one inside a function
  body.
- `geblang fmt` keeps the `geblang.` prefix on `import geblang.name;` and
  `from geblang.name import ...;`; previously it was dropped, which could
  change which module the import resolved to.

## 1.35.0

### Language and stdlib

- `list.shift()` removes the first element in place and returns the list
  (a no-op when empty), mirroring `pop()`.
- `list.takeFirst()`, `list.takeLast()` and `list.takeAt(index)` remove one
  element and return it. `takeFirst` / `takeLast` raise `ValueError` on an
  empty list; `takeAt` accepts negative indexes and raises on an
  out-of-range one. All raise `ImmutableError` on a frozen list, and all
  are supported by `geblang build --native`.
- `math.sum(xs)` sums a list or set with the same promotion rules as `+`
  (exact `int`, `decimal`, or `float`; mixing decimal and float raises
  `RuntimeError`), returning `0` for an empty input.
- `math.mean(xs)` returns the arithmetic mean of a list or set as a
  `float`, raising `RuntimeError` on an empty or non-numeric input.
- The `stats` module gains descriptive statistics: `variance` and `stdev`
  (sample by default, `{"population": true}` for the population form),
  `geometricMean`, `harmonicMean`, `weightedMean`, `range`, `iqr`, `mad`,
  `zscores`, and `describe`, a summary dict of count, mean, stdev, min,
  quartiles and max.

### Tooling

- Editor completion and hover cover the new list methods, `math.sum`,
  `math.mean`, and the new `stats` functions.

### Runtime

- Arithmetic on two integer literals that overflows 64 bits (for example
  `9223372036854775807 + 1`) now promotes to an arbitrary-precision int on
  the bytecode VM, matching the evaluator; previously the VM wrapped to a
  negative number.

### Bundling

- `geblang build --native` (experimental) reports an integer constant that
  does not fit in 64 bits as a Geblang diagnostic instead of emitting Go
  that fails to compile.
- `geblang build --native` (experimental) now uses floor modulo for `%` on
  int and float operands, matching the VM and evaluator; previously
  `7 % -3` produced `1` instead of `-2`.
- `geblang build --native` (experimental) reports decimal arithmetic and
  comparison operators, and `int / int`, as unsupported diagnostics;
  previously they emitted Go that failed to compile or silently truncated
  `1 / 2` to `0`.

## 1.34.0

### Language and stdlib

- `embed(path)` reads a file at compile time into a string constant, and
  `embed(path, {binary: true})` into a bytes constant. The path must be a
  string literal, resolved relative to the source file, and confined to
  its subtree (no `..`, no absolute paths). A bad path, a missing file,
  or a malformed call is a load-time error, not a runtime one. Works
  under `geblang run`, `geblang test`, the REPL, built binaries, and the
  experimental `geblang build --native` path. Editing an embedded file
  invalidates the cached bytecode for its module, so the next run picks
  up the change automatically. The name `embed` is reserved for this
  compile-time builtin; using it as a variable or value is a load error.
- Bare builtins (`assert dir dump parent range typeof zrange`) now match
  case-sensitively on both backends; previously a differently-cased call
  such as `Dump(x)` dispatched on some paths.
- String interpolation of a bytes value now renders lowercase hex on both
  backends; previously the bytecode VM rejected non-UTF-8 bytes inside
  `"${...}"` while the evaluator rendered hex.

### Static analysis

- `geblang check` reports an embed problem (missing file, invalid path,
  malformed call) as `error[embed]` and suppresses the misleading cascade
  of unrelated diagnostics that used to follow one.

### Tooling

- `parent` gained hover and completion in the editor catalog, and REPL
  tab-completion now lists every bare builtin (`assert`, `parent`, `range`,
  and `zrange` were previously missing) as well as `embed`.

### Bundling

- Built binaries now reuse their shipped precompiled bytecode at launch
  instead of recompiling from source (a cache-seeding fix).

### Runtime

- Bytecode chunk format version bumped 79 to 80 to carry embedded-file
  hashes and bytes constants; older cached bytecode rebuilds
  automatically.

## 1.33.0

### Language and stdlib

- Regex match results now carry position information. The dicts returned
  by `re.match` / `re.matchAll`, `pcre.match` / `pcre.matchAll`, and the
  `match` / `matchAll` methods on both compiled `Pattern` classes gain
  three fields: `span` (`[start, end]` of the whole match), `spans`
  (one `[start, end]` per capture group, aligned with `groups`, `null`
  for a group that did not participate), and `namedSpans` (the same for
  named groups). Offsets are character positions, end-exclusive, so
  `text.substring(span[0], span[1])` is exactly the matched text and
  multibyte characters count as one position. Existing fields keep
  their values, but match dicts now iterate and print in a fixed
  field order (text, span, groups, spans, named, namedSpans, with
  named entries in pattern order) where they previously rendered in
  sorted-key order.

## 1.32.2

### Tooling

- The language server now resolves document and workspace URIs sent by a
  Windows-hosted editor for WSL files (`\\wsl.localhost\<distro>\...` and
  legacy `\\wsl$\...` forms, plus `C:\` drive paths). Project imports no
  longer produce spurious "cannot resolve import" diagnostics in that
  setup, and hover, completion, and go-to-definition see the real files.
- The VS Code extension translates WSL UNC paths when VS Code runs on the
  Windows host: language-server URIs convert both ways (returned locations
  open correctly), and Run File and the test runner pass in-WSL paths to
  the toolchain. Executable paths using the legacy `\\wsl$\` form are now
  recognized.

## 1.32.1

### Tooling

- The language server now resolves exports from imported project modules.
  With `import app.foo`, go-to-definition and hover on `foo.bar` jump to
  and describe `bar` in the module's source file, `foo.` completion lists
  the module's members with their signatures, and signature help covers
  calls to imported functions. Aliased imports (`import app.foo as f`)
  and selective imports (`from app.foo import bar`) resolve the same way,
  including go-to-definition on a bare from-imported name; go-to-definition
  on the module alias itself opens the module file. Native and stdlib
  modules keep their catalog documentation.

## 1.32.0

### Changed

- `geblang test` now executes on the bytecode VM by default, the same backend
  release builds ship, so a test that passes only on the tree-walking evaluator
  no longer hides a VM-path regression. A test file the VM cannot run fails
  loudly, naming the file, instead of silently falling back. `--runtime=evaluator`
  (alias `--disable-vm`) runs the whole suite on the evaluator. A file covering a
  documented, temporarily-accepted divergence may carry a `# @vm-divergence: <key>`
  comment plus a matching `KNOWN_DIVERGENCES.md` row to run on the evaluator; the
  runner cross-validates the two and reports how many files ran that way.
- `instanceof` is now module-exact: the right-hand side resolves to the
  specific class or interface declaration named in scope, and an instance
  matches when its actual class is that declaration or a subtype of it. Two
  same-named classes from different modules no longer match each other.
  `typeof(x) == SomeClass` comparisons are unchanged (still by name). The
  types chapter documents the distinction.
- Deferred calls now evaluate their callee, receiver, and arguments when the
  `defer` statement executes, on both backends, as the documentation always
  stated. A variable mutated after the defer statement no longer changes what
  the deferred call sees; a defer inside a loop captures each iteration's
  values.
- Passing arguments to a parent constructor that does not exist is now an error
  on both backends: `parent(args)` where the base class declares no constructor
  raises a catchable `RuntimeError`. Previously one runtime silently ignored the
  arguments. Zero-argument `parent()` to such a base is still a no-op, and a base
  with a matching constructor is unchanged.

### Added

- Methods are first-class values: `let f = SomeClass.staticMethod` and
  `let m = instance.method` produce callable values that can be stored,
  passed, returned, deferred, and used as callbacks (`xs.sortBy(obj.key)`),
  locally and across modules. A bound method keeps its receiver by reference.
- `defer f(...xs)` accepts a list spread; the list reference freezes at the
  defer statement and its elements are expanded when the call fires.
- Modules can export overloaded functions: `mod.over(5)` and `mod.over("x")`
  select by argument types across the module boundary, in every form -
  qualified, from-import, as a stored value, with named arguments, deferred,
  and as a callback. Previously a module declaring two overloads of an
  exported function failed to compile on the standard runtime.
- An overloaded `async` function called through a stored value or an
  imported overload set returns a `Task` like a direct call does; in a
  mixed set the selected overload decides (async yields a `Task`, sync
  returns its value). Previously the body ran synchronously and returned
  the plain result.

### Fixes

- An error thrown inside a callback passed to a native higher-order function
  (`sortBy`, `map`, `filter`, `reduce`, and the `collections` equivalents) and
  caught by a surrounding try/catch no longer corrupts the VM: iteration stops
  at the throwing element and execution continues cleanly after the catch,
  matching the evaluator.
- An uncaught error thrown inside such a callback now keeps the full caller
  chain in the VM stack trace.
- A fault raised by a deferred call or a `del`-fired destructor whose body
  lives in another module is reported once with a clean message on both
  backends, instead of nesting a fully rendered error inside the failure text.
- The `==` operator and `assertEquals` now share one canonical equality
  definition, pinned by guard tests on both backends: comparing native handle
  values (for example two file-serve descriptors) no longer panics, and
  `assertEquals` agrees with `==` for complex numbers.
- `web.http`: a file response built by `serveFile` survives
  `http.responseFrom(...)`, so builder-style wrapping preserves the streamed
  file.
- A typed error thrown inside a callback or a decorated function, method, or
  constructor is catchable by its class on the VM (`catch (ValueError e)`
  now matches; previously the class was lost and the error rendered twice).
- Closures capture outer variables referenced through string interpolation,
  pipelines, partial application, comprehensions, match-case patterns, and
  destructuring assignments; on the VM these previously read wrong values or
  failed with "local is undefined".
- Named arguments on a decorated function or method bind to the original
  declared parameter names on both backends; decorators are transparent to
  callers.
- A class inheriting `__next`/`__done` or `__iter` from a class in another
  module iterates with `for-in` on the VM.
- `defer` accepts module-qualified functions (`defer mod.cleanup()`) and
  nested-selector static methods on the VM.
- `del` works on variables whose class is declared in another module.
- Cross-module interface hierarchies (`Sub extends Base` in another module)
  match `instanceof Base` on the VM.
- Running a test file's `test.run` on the VM reports failures with the same
  clean class-prefixed message and frames as `geblang test`.
- A spread argument works in any position (`f(1, ...rest, 4)`), combines
  with named arguments (`f(c: 1, ...rest)`), and multiple spreads expand in
  source order; previously arguments after a spread were silently dropped
  and named arguments before a spread were silently treated as positional.
  Native functions reject named arguments with spread with a clear error.
- An uncaught error thrown from a callback declared in another module, a
  capturing closure, or a callable object passed to a native higher-order
  function now keeps the full caller chain in the stack trace, identical on
  both backends. The reverse direction is also complete: when a function
  from another module passes your callback to a higher-order method, that
  function's own frame appears in the trace and in caught stackTrace()
  frames.
- Named arguments work on functions imported from another module
  (`mod.f(b: 2, a: 1)`), including out-of-order names, mixes with positional
  and spread arguments, omitted defaults, from-import forms, and deferred
  calls; previously these were rejected at runtime.
- Named arguments also work when constructing a class from another module
  (`mod.Point(y: 2, x: 1)`), including overloaded constructors, from-import
  and aliased forms, dict spreads, defaults, and deferred construction.
- A destructor (`func ~ClassName()`) on a class from another module fires at
  `del` or program exit like a local class does; previously it could fire
  eagerly right after construction, and `del` never invoked it. Exit-sweep
  ordering (reverse creation), del-fires-once, and destructors that construct
  further destructible values behave identically on both backends.
- An uncaught error thrown from a decorated function, method, or static method
  renders an identical stack trace on both backends: the wrapper frame is named
  after the decorated function and the top-level frame keeps its line. A class
  decorator's uncaught throw also keeps the top-level line.
- A DECORATED method used as a first-class value runs its decorator wrapper
  with the receiver bound, on both backends: `let f = s.handle; f(5)` behaves
  like `s.handle(5)`, including in callbacks and defers. Previously the call
  failed with a wrong-arity error, and a decorated static method taken as a
  value silently skipped its decorator.
- `profile.elapsed` and `metrics.duration` now return the documented
  milliseconds as a float with sub-millisecond precision (previously raw
  nanoseconds as an integer). The `now()` token they take is unchanged.
- Variables mutated inside a `try` body keep their values when an exception
  unwinds to the handler, and `finally` effects always run; previously the
  standard runtime rolled the frame's locals back to their try-entry values.
- A `select` statement inside a closure captures its channels; previously a
  worker running such a closure could read an undefined value and block
  forever.
- Calling a method on an instance whose class is exported and overridden in
  another module dispatches to the override; previously a statically
  resolved call could run the base method.
- A cross-module named-argument call may omit a defaulted parameter between
  two supplied ones (`f(a: 1, c: 3)` with `b` defaulted).
- `reflect.parameters` and `reflect.returnType` work on function values,
  including functions passed across module boundaries; `reflect.fields`
  reports declared field types and lossless decorator arguments (floats and
  lists included) for cross-module instances; `reflect.interfaces` returns
  bare interface names on both backends.
- `reflect.class(name)` prefers the program's own classes over a same-named
  class exported by a standard-library module.
- `test.mock` patches apply inside test methods on the standard runtime and
  restore automatically between methods.

### Performance

- `redis`: the reply reader buffers network chunks in linear time, so large
  bulk replies no longer copy quadratically (a 16 MB reply drops from roughly
  885 ms to 41 ms), and `getBytes` returns the raw payload without a
  decode and re-encode round trip.

## 1.31.1

### Debugging

- Step Over, Step Into, and Step Out in an asynchronous worker now resume other
  parked threads in continue mode. This prevents a selected worker from
  deadlocking when its next operation depends on another thread closing a
  channel, releasing a lock, or completing a task.
- Single-thread execution is supported: continue or step one selected thread
  while the others stay paused (the adapter advertises single-thread execution
  requests and honors the per-request `singleThread` flag).
- Setting a variable while paused now persists in the Variables view across
  refreshes, including in outer stack frames.

### Added

- A new `serveFile(path, opts)` in the `web.http` module returns a response the
  server streams from disk with HTTP conditional-request semantics: HEAD, byte
  ranges, If-None-Match/ETag, and If-Modified-Since, with Content-Length set and
  without reading the whole file into memory. A missing file yields 404. The file
  response is an opaque value that request data cannot forge, and the server sets
  a default ETag so If-None-Match works without the caller supplying one.
- A new `path.real(p)` returns the canonical absolute path with symlinks
  resolved, for containment checks that must not be fooled by a symlink.

### Fixes

- A runtime fault (for example division by zero) inside a function or method in
  another module is now caught with its clean message, matching the evaluator,
  instead of a rendered `uncaught ...` block; an uncaught fault still renders the
  full cross-module trace exactly once.
- An error thrown from a `with` resource's `__enter` or `__exit` is now caught by
  its real class with the clean message (for example `catch (ValueError e)`),
  instead of a flattened `RuntimeError`. A `with` resource whose `__enter`/`__exit`
  is inherited from a class in another module now runs it (it was silently
  skipped).
- When a `with` body and its `__exit` both raise, the `__exit` error now
  propagates and replaces the body's, consistently on both backends (Python
  `__exit__` semantics). A `with` body that returns is likewise overridden by an
  `__exit` that raises.
- The HTTP server now canonicalizes the request path (collapsing a leading `//`
  and resolving `.`/`..` segments) before it reaches handlers and routing, so a
  non-canonical path like `//admin` can no longer slip past a path-prefix check
  while still matching a trimmed route.
- `--allow-ffi <path-or-glob>` permissions now apply when running an exported module
  entry point with `geblang -m`, in addition to permissions declared in
  `geblang.yaml`. Top-level, `run`, and module help now document the repeatable
  FFI permission flag and its required library path or glob.
- The `redis` client now parses replies at the byte level, so values are read
  without converting the whole reply buffer to text. Values that are valid UTF-8
  are returned as strings as before; a value that is not valid UTF-8 raises a
  clear error naming `getBytes`, and a new `getBytes(key)` returns the raw bytes.
  This also fixes replies whose bytes span more than one socket read.
- The `messaging` backends (`sqs`, `sns`, `stomp`) now raise a clear error when a
  `bytes` payload is not valid UTF-8, telling the caller to encode it first (for
  example as base64), instead of failing with a generic decode error. Valid
  UTF-8 byte payloads are sent as text as before.
- `llm` image analysis (`analyzeImage`) again accepts binary image bytes; the
  base64 encoding no longer round-trips the image through a UTF-8 string.
- The file-backed session store now validates the session id read from the
  request cookie before using it in a file path. A malformed or tampered cookie
  is treated as no session instead of reaching the filesystem.
- `instanceof` against a builtin type name that also names an imported module
  (such as `bytes` or `string`) is now accepted in expression position (return,
  argument, assignment), not only inside `if`/`while` conditions.
- `assertEquals` now treats the type value from `typeof(x)` as equal to its name
  string or to a class value of the same name, matching the `==` operator, so
  `assertEquals("bytes", typeof(x))` and `assertEquals(SomeClass, typeof(inst))`
  both pass.
- `json.parseAs` no longer fails with "class index out of range" when
  deserializing a class whose parent is in another module, including in
  `geblang build` binaries.

## 1.31.0

### Debugging

- The VS Code debugger now sets breakpoints inside concurrent worker bodies:
  `async.run` / `async.all` / `async.race` workers, generator bodies, and
  network request handlers, in addition to the main script. Each running
  worker appears as its own thread in the Call Stack pane; hitting a
  breakpoint stops all threads, Continue resumes them, and a step advances
  only the selected thread.

### Changed

- `bytes.toString()` and the `bytes` value `.toString()` method now reject data
  that is not valid UTF-8 with a clear error, instead of returning a string that
  carried the raw bytes. Convert non-text bytes with `.toHex()` or `.toBase64()`.

### Performance

- String index (`s[i]`), `substring` / `slice`, and `length()` now cache rune
  offsets for strings longer than 256 bytes, making each access amortized O(1)
  and a character-scanning loop O(n) instead of O(n^2). Shorter strings are
  rescanned per access with a maximum scan of 256 bytes. The cache is
  concurrency-safe and memory-bounded, with no API change.

## 1.30.1

### Changed

- Primitive method names now use their documented, case-sensitive spelling on
  both backends. The bytecode VM previously accepted arbitrary casing, and
  conversion helpers such as `toInt` accepted arbitrary casing on both
  backends.

### Performance

- Bytecode primitive-method dispatch no longer normalizes the method name on
  every call. Canonical camel-case calls no longer allocate for normalization;
  a template-shaped dispatch benchmark improved from about 38-40 ns to
  21-22 ns per call.

### Fixes

- `reflect.method(instance, name)` now returns `null` on the bytecode backend
  when `instance` belongs to another module and the method does not exist,
  matching the evaluator instead of reporting `unknown class`.

## 1.30.0

### Changed

- Module-level variables mutated by a function call across a module boundary
  now persist on the bytecode backend, including a call that throws (the
  assignments it made before the throw are kept) and a synchronous re-entrant
  call (it sees the outer call's in-progress assignments). Module globals
  remain unsuitable for concurrent or transactional state; use `store.Store`
  or another explicit synchronized handle for those cases.
- `http.serve`, `http.listen`, and `net.serve` accept an `opts.shareHandler`
  flag: when true the handler is shared across requests instead of isolated
  per request, for frameworks that manage their own per-request isolation.
- Native (built-in) functions accept named arguments, the same as user
  functions: an argument may be passed by parameter name, in any order (for
  example `math.pow(base: 2.0, exponent: 3.0)`), identically on both backends.
  An unknown or duplicated parameter name is a runtime error.

### Performance

- Cross-module function, method, constructor, and static-method calls
  are substantially faster on the bytecode backend.

### Fixes

- `instanceof`, an inherited `__serialize`, and overloaded-callback selection
  now work for a class that extends, or is declared in, another module; the
  bytecode backend previously resolved only same-module hierarchies.
- An overloaded function used as a value (passed to a callback or stored in a
  variable) now retains all overloads and selects by positional arity,
  defaults, variadics, and runtime types, including module-qualified and
  user-generic parameter types (for example `Box<Dog>` versus `Box<Animal>`).
  Previously the bytecode backend could keep only the last overload and could
  not disambiguate generic parameters.
- An async function returns a Task when it is passed as a value, used as a
  callback, or called across a module boundary, matching a direct async call.
  Previously the bytecode backend could run it synchronously and return the
  raw value.
- A `defer` inside a function that throws across a module boundary now runs as
  the error propagates to the caller.
- A function body may reference a top-level `const` or `let` declared later in
  the same file.
- A generator that mutates a module-level variable writes the change back when
  it is consumed.
- Concurrent calls to already-loaded modules run on isolated workers, dispatch
  of an already-loaded module is race-free while another module lazily loads,
  and method dispatch on a returned instance no longer retains its
  construction VM.
- Deserializing a class instance whose type defines methods no longer fails on
  the constructor's implicit receiver parameter.
- A handler passed directly to `http.serve`, `http.listen`, or `net.serve` is
  isolated per request on the bytecode backend: its captured state is
  deep-cloned for each request, matching the evaluator. Previously the bytecode
  backend shared the handler's captured state across concurrent requests.
- A handler defined in a different module from its `http.serve`, `http.listen`,
  or `net.serve` call is now isolated per request on the bytecode backend too;
  it was still shared across requests.
- Deep-cloning a value that contains a reference cycle (for example a dict that
  holds itself) no longer recurses without bound; this could happen during
  per-request handler isolation.
- The `messaging` module (RabbitMQ and Kafka) runs on the bytecode backend;
  those calls previously failed there with `unsupported native call`.

Older releases (1.0.0 through 1.29.2) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
