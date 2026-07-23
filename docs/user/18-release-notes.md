# Release Notes

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

## 1.29.2

### Fixes

- Explicit type arguments on a generic function or method call are now
  validated. A type argument that contradicts the actual arguments, such as
  `firstMatch<string>(listOfInts)`, is reported as an error rather than being
  silently discarded. The binding is also enforced at runtime, so a
  dynamically-typed argument that violates an explicit type argument throws,
  with the evaluator and bytecode VM in agreement.

## 1.29.1

### Logging

- New `log.syslog(opts)` destination sends structured log records to a syslog
  server or the local syslog daemon, framed as RFC 5424. It joins the existing
  logger family (`stdout` / `stderr` / `file` / `toStream` / `custom`), so the
  same level calls apply. Transport is `udp` (default), `tcp`, or `local`;
  `udp` and `tcp` work on Linux, macOS/BSD, and Windows, while `local` (the
  platform daemon socket) is Unix-only. The message body is the same JSON the
  other destinations emit, and facility, app name, and hostname are
  configurable. The logger connects when constructed (a bad address fails
  fast) and drops transient send failures rather than raising.

### Fixes

- `sys.exit(code)` now terminates the program cleanly with its code from any
  context, including inside an exported `main` and across module boundaries, on
  both runtimes. Previously it could surface as an uncaught error in those
  cases instead of exiting.

### Documentation

- Logging now has its own reference chapter, split out of the observability
  chapter for discoverability.

## 1.29.0

### Language

- Numeric literals now support scientific notation. Unsuffixed exponent
  literals are exact `decimal` values (`1e3`, `1.5e-3`, `2E8`); add `f` for
  IEEE 754 floats (`1e308f`).
- Enums can now be backed by scalar `string` or `int` values:
  `enum Status: string { Active = "active"; }`. Backed variants expose a
  read-only `.value`, `EnumName.from(value)` returns the matching variant or
  throws, and `EnumName.tryFrom(value)` returns the variant or `null`.
  Backing values must be unique literals, and variants cannot mix backing
  values with associated data.

### Runtime

- Bytecode chunk format version bumped 75 to 76 so backed enum metadata
  round-trips through cached bytecode and built binaries.
- The experimental `geblang build --native` path now rejects backed enums with a
  clear diagnostic instead of emitting incorrect Go; use the default bytecode
  build path for backed enums.

## 1.28.1

### Language

- A type annotation on a `for-in` loop variable is now checked and enforced, the
  same way a typed declaration is. `for (int x in xs)` is fine, but an unknown type
  name (for example `for (var x in xs)`, where `var` is not a type) and an
  element-type mismatch (`for (string x in [1, 2])`) are reported by `geblang
  check`, `run`, and `test`. Omit the type to infer it from the iterable.

### Tooling

- `geblang build --help` and `geblang test --help` now list every flag, including
  `--resource` and the `--allow-ffi` / `--allow-onnx` / `--allow-process-control` /
  `--allow-browser` capability flags.
- Editor autocomplete now shows the correct signatures for `io.captureStdout` and
  `io.captureStderr`: they take no arguments and return a capture stream you read
  with `io.toString`.

### Documentation

- Corrected reference-manual examples that used syntax the language does not accept:
  match block-body arms, `when` guards (the keyword is `if`), named enum fields,
  untyped lambda parameters, scientific-notation float literals, and a backtick
  string literal.

## 1.28.0

### Standard library

- New `html` module for parsing real-world HTML and querying it with CSS
  selectors. `html.parse(source)` returns the root node; nodes expose
  `select`/`selectFirst` (CSS3 selectors: type, class, id, attribute operators,
  descendant/child/sibling combinators, structural pseudo-classes, grouping),
  `text`, `attr`, `attrs`, `tag`, `html` (inner HTML), `children`, and
  `parent`. The parser is lenient, so malformed markup is repaired rather than
  rejected. See the HTML chapter in the standard-library reference.

### Fixes

- `dir()` now lists the methods of the native value types (n-dimensional arrays,
  data frames, series, distributions, complex numbers, generators, and html
  nodes) on the bytecode VM, matching the evaluator. Previously it returned an
  empty list for those types on the VM.

## 1.27.1

### Standard library

- HTTP client responses now expose the final URL after redirects. `r.url()` (and
  the index form `r["url"]`) returns the address the response was ultimately
  served from, so a request to a redirecting endpoint reports where it landed.
  It is an empty string for responses not produced by a request (for example
  `http.response(...)`), and is carried through `withStatus`/`withHeader`/`withBody`
  and `toDict`.

## 1.27.0

### Tooling

- `geblang run <script.gb> [args...]` is now an explicit alias for the default
  script-running form (`geblang <script.gb> [args...]`). Both invoke the bytecode
  VM with evaluator fallback and accept the same flags
  (`geblang run --vm-strict app.gb`). The verb stays optional; the next token
  after `run` is always a script path, never re-read as another subcommand.
- Cross-platform builds: `geblang build --runtime <path>` embeds the bundle into
  a runtime compiled for another platform, and the new `scripts/cross-build.sh`
  helper drives it end to end (`--target linux/amd64`, `darwin/arm64`,
  `windows/amd64`, ...) using the Go toolchain, so any host can build for any
  target.
- Geblang now builds and runs on Windows. FFI, local ONNX inference, and
  advisory file locking report a clear unsupported error there; the `hnsw`
  vector-store backend uses an exact index; and interactive console widgets and
  the REPL line editor fall back to plain line input. See the bundling chapter.

### Standard library

- The `math` module gains special functions and exact combinatorics. Special
  functions (real-valued): `gamma`, `lgamma`, `beta`, `lbeta`, `erf`, `erfc`,
  `erfinv`, and the Bessel functions `j0`/`j1`/`jn`/`y0`/`y1`/`yn`.
  Combinatorics return exact integers at arbitrary precision: `factorial`,
  `comb`, `perm`, `gcd`, `lcm`, and the log binomial coefficient `lcomb`.
  `factorial`, `perm`, and `comb` reject `n` above 100000 to bound memory and
  CPU.
- New `stats` module: probability distributions as objects. A constructor
  (`stats.normal(0, 1)`, `stats.binomial(20, 0.5)`, ...) returns a distribution
  exposing `pdf`, `cdf`, `ppf`, `mean`, `variance`, `std`, and `sample`. Twelve
  distributions are included: normal, uniform, exponential, gamma, beta,
  chi-squared, Student's t, F, log-normal, Weibull, binomial, and Poisson.
  `sample(n, {"seed": k})` draws reproducibly into an ndarray.
- The `stats` module also gains hypothesis tests and confidence intervals,
  each returning a dict. Tests: `tTestOneSample`, `tTestIndependent` (pooled or
  Welch), `tTestPaired`, `chiSquareTest`, `chiSquareIndependence`,
  `mannWhitneyU`, and `ksTest`. Intervals: `confidenceIntervalMean`,
  `confidenceIntervalProportion`, and `confidenceIntervalDiffMeans`.
- The `stats` module also gains regression: `linregress` (simple linear
  least-squares, returning slope, intercept, correlation, r-squared, p-value,
  and slope standard error), plus `polyfit` (least-squares polynomial fit) and
  `polyval` (polynomial evaluation).
- The `stats` module also gains descriptive extensions: `skewness` and
  `kurtosis` (population, with excess kurtosis), `covariance` (sample), and
  `corrcoef` (Pearson correlation).
- New `physics` module: twelve physical constants as zero-argument functions
  (`c`, `G`, `planck`, `hbar`, `avogadro`, `boltzmann`, `gasConstant`,
  `elementaryCharge`, `electronMass`, `protonMass`, `stefanBoltzmann`,
  `gravity`) and `convert(value, fromUnit, toUnit)` covering length, mass, time
  (scale), and temperature (affine: C, F, K).
- New `complex` module: complex number value with two constructors (`complex.of`,
  `complex.fromPolar`), a full method set (`re`, `im`, `abs`, `arg`, `conj`,
  `neg`, `exp`, `sqrt`, and binary `add`/`sub`/`mul`/`div`/`pow`/`equals`), and
  operator overloads for `+`, `-`, `*`, `/`, `**`, unary `-`, and `==`. Plain
  numbers are promoted automatically on either side of a binary operation.
- New `geo` module: `haversineDistance`, `bearing`, `midpoint`, and `destination`
  for geodetic calculations on the sphere. Coordinates are in decimal degrees
  using a mean Earth radius of 6371 km. Distance functions accept an optional
  unit argument (`"km"` default, `"m"`, `"mi"`, `"nmi"`); `midpoint` and
  `destination` return `{"lat", "lon"}` dicts.
- New `async.tasks` module: high-level task combinators over the async core for
  callers who would rather hand off a function and data than manage tasks,
  channels, and semaphores directly. `map`/`forEach` run a function over a
  collection concurrently (results in input order, optional `concurrency` cap,
  fail-fast); `retry` adds exponential-backoff retry; `settle` awaits every task
  without failing fast; `any` returns the first task to succeed; and `parallel`
  runs a list or dict of callables at once.

### Fixes

- A closure created in one module and passed to `async.run` from another module
  is now accepted on the bytecode VM, matching the evaluator. The VM previously
  rejected such a cross-module callable with "async.run expects a function".

### Platform

- macOS and BSD support: the toolchain now builds and runs on macOS and the
  BSDs. Terminal handling uses per-platform ioctl constants, and `sys.osVersion`
  plus `process.list` / `process.info` are implemented for those platforms.

## 1.26.0

### Language

- Partial application: a call with one or more `_` placeholder arguments now
  returns a new callable with those positions left open (`add(_, 10)`,
  `wrap(_, "-", _)`, `open(mode: _)`). Non-hole arguments are captured once at
  creation; the target resolves at application. Works across functions, methods,
  constructors, native builtins, module functions, and callable objects.
  (Partials over multiple same-arity overloads are resolved at application by
  the interpreter; compiled builds reject them statically - use a typed wrapper.)

## 1.25.0

### Language

- Mixing `decimal` and `float` in arithmetic - the deliberate precision wall that
  keeps a `decimal`'s exactness from silently leaking into a `float` - is now
  reported by `geblang check` when both operand types are statically known, not
  only at runtime. The error names both fixes and their tradeoff: cast with
  `as float` (fast, drops the decimal's exactness) or `as decimal` (keeps an
  exact type, adopts the float's binary imprecision).
- `reflect.fields(class)` now includes a `doc` key on each field entry: the
  docblock written immediately before the field (a `##` line or a `/** */`
  block), or `null` when there is none. This matches how class, function, and
  method docblocks were already surfaced.

### Standard library

- New experimental `browser` module: drive a headless Chrome/Chromium over the
  DevTools Protocol for functional testing and scripted control. `browser.launch`
  opens a browser; pages can `goto`, `waitFor`, `click`, `type`/`fill`,
  `evaluate` JavaScript, read `text`/`content`/`title`, `screenshot`/`pdf`,
  manage `cookies`, list tabs, and intercept requests with `route` (continue,
  block, or fulfill a mock response). It speaks the protocol directly with no
  external driver, is gated behind `--allow-browser`, and always terminates the
  browser on close. Needs Chrome installed (located via `$GEBLANG_CHROME` or an
  `executable` option).

### Tooling

- `geblang fmt` is substantially more robust. It now re-parses its own output
  and refuses to write anything that is not identical to the input at the AST
  level, so formatting can never silently change a program's meaning or break
  its syntax. By default it follows a width-aware layout standard (a 100-column
  target): explicit grouping parentheses, intentional blank lines, comments
  (including trailing same-line comments), and any construct the author split
  across lines (operator and method chains, list/dict/set literals, call
  argument lists) are kept, and a collection or argument list is wrapped onto
  one item per line when a single line would exceed the width.
- `geblang fmt` gained two formatting modes. `--clean` produces the minimal
  canonical form (drops redundant parentheses, flattens multi-line chains,
  concatenations, and collections onto one line); `--strip-comments` removes all
  comments. The flags are independent and may be combined.

## 1.24.1

### Bundling

- Built binaries (`geblang build`) can now carry privileged capabilities baked in
  at build time, so they run with no launch flags. Declare them in `geblang.yaml`
  under `permissions:` - `ffi` (existing), `onnx: true`, `processControl: true` -
  and/or pass `geblang build --allow-ffi <path-or-glob>` / `--allow-onnx` /
  `--allow-process-control`; build flags add to what the manifest declares.
- The `onnx` and `processControl` permissions also enable those capabilities for
  `geblang run` / `geblang test` in the project, matching how `permissions.ffi`
  already behaved. A binary (or run) without them stays locked down: a gated call
  throws `PermissionError`.

### Tooling

- The `geblang` binary now embeds the source standard library, so a standalone
  binary - copied anywhere, with no `GEBLANG_STDLIB` and no stdlib directory
  beside it - resolves source stdlib modules (`llm`, `rag`, `vectorstore`, ...)
  with no setup. An on-disk stdlib (a repo checkout or `GEBLANG_STDLIB`) still
  takes precedence, so developing against a working copy is unchanged.

### Runtime

- A running program now starts a background memory sweeper that periodically
  returns freed heap pages to the OS, so a long-running server's RSS no longer
  stays pinned at its allocation high-water mark after a burst (e.g. buffering
  large uploads). It is on by default and tuned with `GEBLANG_GC` (`off` to
  disable), `GEBLANG_GC_INTERVAL` (default `30s`), and `GEBLANG_GC_THRESHOLD_MB`
  (default `64`). `profile.gc()` still forces a collection on demand.
- Streaming HTTP response handles (`http.requestStream`, `http.fetchStream`) are
  now released as soon as the stream is fully read or closed, and any left open
  are swept at shutdown, so a long-running server that streams many responses no
  longer accumulates handles for the life of the program.

## 1.24.0

### Standard library

- `vecmath` gained `normalize` (L2-normalize a vector, or each vector in a list)
  and `semanticSearch(queries, corpus, k, metric)`, the multi-query form of
  `topK` that returns the top-k corpus matches per query as `{index, score}`.
- The `llm` client gained `models()` (list the models available to the account)
  and `embedBatch(texts, opts)` (embed many strings in one call, returning
  `{vectors, model, usage}`), across the OpenAI, Anthropic, and Bedrock
  providers (Anthropic has no embeddings API, so `embedBatch` throws there).
- The `llm` `chat` method now supports tool / function calling. Pass
  `opts.tools` (provider-neutral `{name, description, parameters}` schemas); when
  the model calls a tool the result carries `toolCalls` (`{id, name, arguments}`),
  and you continue the conversation with a `{role: "tool", toolCallId, content}`
  message. One portable shape across OpenAI, Anthropic, and Bedrock Claude models.
- New `http.requestStream(options)` performs a request and returns a
  `StreamResponse` whose `read()` yields the body line-by-line as it arrives
  (`null` at end), the client-side analog of server-sent events. `status()`,
  `headers()`, `done()`, and `close()` round it out.
- The `llm` client gained `chatStream(messages, opts, callback)`: streaming chat
  that invokes `callback` with each content delta and returns the assembled
  result. Supported for OpenAI and Anthropic (server-sent events); Bedrock uses a
  binary event-stream protocol and throws there.
- New experimental local-model modules. `transformers.tokenize` runs WordPiece
  tokenization from a HuggingFace `tokenizer.json` (BERT-family encoders),
  returning padded `input_ids` / `attention_mask` / `token_type_ids`.
  `onnx.session(modelPath)` loads an ONNX model for cgo-free local inference via
  ONNX Runtime; `Session.run` maps int64 input ndarrays to float64 output
  ndarrays. Both are gated behind the new `--allow-onnx` launch flag, and ONNX
  Runtime is located via `opts.libPath` / `$GEBLANG_ONNXRUNTIME`.
  `transformers.pool` reduces a `[batch, seq, dim]` hidden state + mask to
  `[batch, dim]` sentence embeddings (mean / cls / max, L2-normalized), so
  tokenize -> `onnx.session` -> pool gives fully local, offline sentence
  embeddings that feed straight into `vecmath` / `vectorstore` / `rag`.
  `rag.LocalEmbedder(modelDir)` wraps that pipeline as a drop-in `Embedder`, for
  a fully on-device index/retrieve loop with no API calls.
- HTTP client failures now carry a precise, catchable class: a timeout raises
  `TimeoutError` and a TLS/certificate failure raises `TlsError`, both new
  `IOError` subclasses (so `catch (IOError e)` still catches them) - so callers
  can retry on a timeout but fail fast on a bad certificate. The same
  classification applies engine-wide to any timeout/TLS failure. A mid-stream
  read failure on `http.requestStream` now surfaces as a catchable error rather
  than looking like a clean end of stream.

## 1.23.3

### Standard library

- New `search` / `searchPattern` methods on lists, dicts, and strings return
  every matching locator (not just the first): list indices, dict keys (matched
  on their values), or string positions. `search(value)` matches by equality,
  `search(callable)` by a predicate, and `searchPattern(regex)` by regular
  expression. Each returns an empty list when nothing matches.

## 1.23.2

### Standard library

- Lists gained `fill(value, count)`: it appends `count` copies of `value` in
  place and returns the list, so it chains. `count` must be `>= 0`; a typed
  list rejects a value of the wrong element type.

## 1.23.1

### Language

- A cast to a primitive followed by `<` now parses as a comparison:
  `x as int < y` is `(x as int) < y`. Primitive types never take type arguments,
  so `<` after one is the less-than operator. Generic casts such as
  `x as list<int>` are unchanged.

### Standard library

- `crypt` gained real X.509 chain support: `crypt.verifyCertChain` verifies a
  certificate chain's signatures up to a trusted root (throwing on failure),
  `crypt.parseCert` now also returns the `publicKey` PEM and raw `extensions`,
  `crypt.asn1Decode` decodes DER into a nested structure, and
  `crypt.parseAndroidAttestation` reads the Android Key Attestation extension.
- SQLite connections accept tuning options on the `db.Connection({...})`
  options-dict, applied as per-connection pragmas at connect time: `wal`,
  `synchronous`, `foreignKeys`, `busyTimeoutMs`, `cacheSizeKb`, `mmapSizeMb`,
  and `tempStoreMemory`. Each is explicit (`wal: true` sets only WAL). A new
  `connection.optimize()` runs `PRAGMA optimize` for query-planner maintenance.

### Type checking

- Division `/` is true division and its result type is `decimal` (or `float`),
  even when the operands divide evenly. Assigning a division result to an `int`
  (`int n = a / b`) is now a compile-time error on every path
  (`check` / `test` / `run` / `build`), reported as `cannot assign decimal to
  int`. Use `//` for an integer (floor) result, or `(a / b) as int` to truncate.
  This closes a case where the bytecode VM crashed at runtime while the evaluator
  produced a decimal.

### Modules and packages

- Path dependencies in `geblang.yaml` now accept absolute paths, a leading `~`
  for the home directory, and `$VAR` / `${VAR}` environment references. Relative
  paths are still resolved against the manifest.
- A scheme-less `git` dependency value (for example `github.com/acme/httplib`)
  is now resolved as `https://...`. Explicit schemes and the scp-like
  `git@host:path` form are left untouched.

## 1.23.0

### Standard library

- `io` file handles gained random access: `io.seek(handle, offset, whence)`
  (whence `"start"` / `"current"` / `"end"`), `io.tell`, `io.truncate`, and
  `io.atEnd`. Open modes now include `r+`, `w+`, `a+`, and the exclusive-create
  `x` / `x+`.
- New filesystem helpers: `io.copy`, `io.copyTree`, `io.move` (rename with a
  cross-device copy fallback), `io.lstat`, `io.scanDir`, `io.touch`, and
  `io.writeTextAtomic`. The `io.stat` / `io.lstat` dictionaries gained `isFile`
  and `isSymlink`.
- New `file` module: a `File` object (`file.open(path, mode)`) wraps a handle
  with method-style read / write / seek, `with`-block auto-close, and line
  iteration. `streams.IOStream` gained `seek` / `tell` / `truncate` / `atEnd`.

### Fixes

- `reflect.classes()` called from inside an imported module now returns the
  entry module's classes on the bytecode VM, matching the evaluator (the VM
  previously omitted them), so whole-program class scans behave identically on
  both backends.
- A class declared in the entry file now resolves the entry file's top-level
  bindings (imports and module-level `let` / `const`) when its methods are
  dispatched from another module on the bytecode VM, matching the evaluator.

### Native compilation (experimental)

- Module-level `let` / `const` read by a function now compiles natively, in both
  imported modules and the entry module: the binding lowers to a package-level
  value so the function can see it. A module's functions can also call each other.
  These previously reported as unsupported or failed to build with an
  undefined-symbol error.
- `list.join(sep)` compiles natively for lists of any element type.
- Comparing a list, dict, set, or bytes value to `null` compiles natively (a nil
  check), matching the interpreter; structural `==` between two collections
  remains deferred.
- Unwrapping a nullable dict with `as` (e.g. `value as dict<string, any>` after a
  null check) compiles natively.
- `io.mkdir` compiles natively.

## 1.22.0

### Language

- Enums expose two operations on the enum type itself. `EnumName.values()`
  returns a list of the simple (nullary) variants in declaration order, and
  `EnumName.fromName(s)` resolves a variant by its exact, case-sensitive name,
  returning the variant or `null` when none matches. Tagged variants are
  excluded from both, since a bare name cannot construct a variant that carries
  fields. Identical on both backends.
- Numeric-check predicates let a value be tested before converting, instead of
  catching a failed cast. On strings: `isInt()`, `isDecimal()`, and
  `isNumeric()` report whether the string parses as an integer, a decimal, or
  either, using the same rules as `toInt()` / `toDecimal()` (so `s.isInt()` is
  true exactly when `s.toInt()` would succeed, bases and underscore separators
  included). On numbers: `float.isInt()` and `decimal.isInt()` report whether
  the value is a whole number (`NaN` and infinity are not). Identical on both
  backends.

- `zrange(start, end[, step])` is the exclusive, Python-style counterpart to the
  inclusive `range` builtin: it omits the end value (`zrange(0, 5)` is
  `[0, 1, 2, 3, 4]`) and adds a one-arg form `zrange(n)` that ranges from 0. It
  returns an eager `list<int>`; `collections.range` remains the lazy exclusive
  form. Identical on both backends.

### Tooling

- `geblang check` now verifies that a `match` over an enum subject handles every
  variant. A match that omits a variant and has no `default` is reported as
  `warning[match-nonexhaustive]`, naming the missing variants. It is advisory
  (the code still runs and throws `MatchError` only if an unhandled value reaches
  the match); a variant handled only by a guarded case counts as missing, since
  the guard may be false. Surfaced in the editor through the language server.

### Fixes

- `option` and `result`: the absent/error path no longer binds the generic type
  to null. `none()`, `err()`, and `ofNullable(null)` now keep their declared type,
  so a typed `unwrapOr` fallback works (`divide(5, 0).unwrapOr(-1)` returns `-1`
  instead of throwing), `orNull()` returns `?T`, and the documented examples run.
- A private in-memory SQLite database (`:memory:`) now pins its connection pool
  to a single connection. Each `:memory:` connection is a separate database, so
  concurrent access previously could open a second, empty one (`no such table`);
  pinning makes concurrent reads and writes share one database. Use
  `file::memory:?cache=shared` or a file path for a larger pool.
- `reflect.fields`, `reflect.methods`, `reflect.className` and the other reflect
  introspection calls now accept a nested `reflect.class(value)` (or
  `reflect.function`) argument identically on both backends. Previously the
  bytecode backend rejected the nested form with a non-literal argument,
  requiring the class to be bound to a variable first.
- `cli.choose(label, options, default)` accepts a literal default index again; a
  small integer literal was wrongly rejected as "default index must be int".
- `sys.run(command, args)` accepts its documented `list<string>` argument form
  again (it was rejected with "arguments must be strings"); the trailing-varargs
  form keeps working.

### Standard library

- `dataframe.filterFn(row -> bool)` filters a frame with a per-row Geblang
  predicate, each row passed as a dict of column name to value, complementing
  the faster columnwise expression filter. A `throw` inside the predicate
  propagates and is catchable, and predicates run safely from concurrent async
  tasks. Identical on both backends.
- `json.parseAs` (and `yaml` / `toml` / `xml` `parseAs`) now reconstructs nested
  class fields recursively: a field whose declared type is another class, or a
  `list` / `dict` of one, is itself deserialized into an instance, so a whole
  object tree comes back fully typed (including across modules). `any` and
  primitive fields keep their raw parsed value, a class with `__deserialize`
  still controls its own nesting, and a value whose shape does not match the
  declared field is left as-is. Identical on both backends.
- `cli.multiChoose(label, options)` selects several options at once: an arrow-key
  checkbox UI on a terminal (up/down or j/k move, space toggles, `a` toggles all,
  enter confirms, q/ctrl-c cancels) with a numbered comma-separated fallback when
  stdin is not a terminal. An optional third argument pre-checks options by index.
- `io.withStdin(input, body)` runs `body` with the console readers consuming
  `input` (line input or raw key sequences) instead of stdin, then restores it.
  The interactive UI is suppressed while it runs, so it makes prompts (including
  `cli.multiChoose`) testable. Returns the body's value.

### Native compilation (experimental)

- `geblang build --native` now lowers direct `as` casts from a string to `int`
  or `float`, and from a non-bool to `bool`, matching the interpreter (these
  previously reported as unsupported). The new numeric-check predicates compile
  natively as well.
- The enum static surface (`values()` / `fromName()`) compiles natively, as do
  nullable enums (`?Color`) and equality on nullable value-types (`?int == 5`),
  which previously reported as unsupported or failed to build.
- `as decimal` casts (from an int, float, string, or dynamic value) compile
  natively, matching the interpreter.

## 1.21.0

### Language

- Enums can declare instance methods, callable on any variant with `this` bound
  to the receiving variant and read via `match this`. There is one method body
  per name; per-variant behaviour is expressed inside it, not as an override
  table. Methods sit beside the existing associated-value and backed-scalar
  access (which still resolve first), and a method named like a built-in variant
  accessor is a compile error. Enums stay immutable.
- Enums can implement interfaces with `implements`. Conformance is checked (a
  missing method is an error agreeing across check, test, run, and build),
  interface defaults apply where an enum leaves a method unimplemented, and an
  enum value flows into an interface-typed slot with dispatch landing on the
  enum's method. The bare `enum Name { A, B }` form is unchanged. Identical on
  both backends.

### Native compilation (experimental)

- `geblang build --native` is a preview that compiles a program to a standalone
  native binary by transpiling it to Go, for a speedup on compute-heavy code. It
  supports a growing subset of the language and standard library and fails the
  build with a clear diagnostic on anything unsupported, never producing a binary
  that behaves differently from the interpreter; use plain `geblang build` for
  the full language. The supported set is unstable and will change between
  releases. See the bundling chapter for what is and is not supported.

### Process and system management

- `sys.osVersion()` returns the OS kernel and release string, complementing
  `sys.platform()` and `sys.arch()`.
- The `process` module reports the running process's own identity and
  credentials: `pid()`, `ppid()`, `uid()`, `gid()`, `euid()`, `egid()`, and
  `groups()`.
- It can inspect other running processes with `process.list()`,
  `process.info(pid)`, and `process.exists(pid)`.
- Privileged control of arbitrary processes (`process.setuid()`,
  `process.setgid()`, `process.kill(pid)`, `process.signal(pid, sig)`) is gated
  behind the opt-in `--allow-process-control` launch flag and raises a
  `PermissionError` when the flag is absent. Handle methods acting on a process
  the script launched itself stay ungated.
- Identity and credential functions are unix-oriented and process enumeration
  is Linux-first; a function unsupported on the host platform raises a clear,
  catchable error rather than returning an empty or wrong result.

### Fixes

- Nullable typed-collection element tags accept `null`: a `null` may be written
  into a `list<?int>` or a `dict<string, ?int>` on every write surface
  (`push`, `insert`, index assignment, `set`, `add`), while a non-nullable
  `list<int>` still rejects it. Identical on both backends.
- The bytecode VM's unary-minus type error now matches the evaluator's wording.
- `x as any` is accepted as a widening no-op on the bytecode VM, matching the
  evaluator; it previously raised a spurious "cannot cast" error.
- `geblang build` reports a missing or incompatible entry `main` at build time
  and writes no binary, instead of failing only when the built binary runs. The
  entry module must `export func main()` or
  `export func main(list<string> args)`, optionally returning `: int`.

## 1.20.0

### Language

- Explicit constructor type arguments are enforced at runtime on both
  runtimes: `Box<string>(42)` throws `RuntimeError` (previously the
  bytecode runtime accepted it silently while the evaluator threw, and
  the recorded bindings could contradict the stored value). Subtype
  arguments still pass and calls without explicit type arguments stay
  inference-open. Enforcement applies same-module and across module
  boundaries. This is a behavior change for code that relied on the
  silent acceptance.
- Method parameters typed with a class-level type parameter are
  enforced against the instance's reified bindings on both runtimes:
  `put(42)` on a `Box<string>` throws
  `Box.put expects T for parameter 'value', got int`. Inherited
  methods enforce extends-clause bindings (`IntBox extends Box<int>`);
  a method's own type parameters still bind per call. This is a
  behavior change for code that relied on unchecked `T` parameters.
- `instanceof` answers parameterized checks against user generic
  classes from the instance's reified bindings, with the same
  invariant model as `list<int>`: `b instanceof Box<string>` is true
  when the class matches and `T` is bound to `string` (previously
  always false). Bare-name checks are unchanged; type-parameter names
  in the argument list resolve in generic frames.
- A declaration annotation over a direct constructor call of the same
  class validates like explicit type arguments:
  `Box<int> x = Box("text")` is rejected (statically when the
  contradiction is visible, at runtime otherwise). The annotation
  still wins over inference for the recorded bindings; `let` without
  an annotation stays inference-open. This is a behavior change.
- Explicit type arguments resolve constructor overloads: with
  `Box(T value)` and `Box(int value)` declared, `Box<string>(42)`
  selects `Box(int value)` instead of reporting an ambiguous
  overload. Bindings only break ties; single-candidate mismatches
  keep the precise per-parameter error.
- Calling a function declared to return `any` in a statically typed
  context (typed parameter, generic constructor argument, typed
  declaration) no longer fails compilation on the bytecode runtime;
  the value is validated at runtime on both runtimes, matching the
  evaluator's long-standing behavior.

- The element-tag write barrier on typed collections is now
  hierarchy-aware and complete. Subclass and implementer writes pass
  (`list<Animal>.push(Dog())` previously threw - name-equality only),
  and the barrier covers every mutation surface: index assignment
  (`xs[0] = v`), `list.set`, dict key AND value writes
  (`d[k] = v`, `dict.set`), and `set.add` were previously unchecked.
  Covariant generic passing is now sound: a `list<Dog>` received as
  `list<Animal>` rejects a `Cat` write against its real tag. This is
  a behavior change in both directions.
- Generic constraints accept primitive and class leaves alongside
  interfaces, combined with `|` and `&`: `<T implements string|int>`
  now works (previously primitives never satisfied a constraint), and
  a class leaf is satisfied by the class or any subclass. A bare form
  drops the keyword: `<T string|int>` on functions and classes means
  the same thing. Constraint-violation messages are now identical on
  both runtimes
  (`type bool does not satisfy constraint string|int for type
  parameter T`).

### Standard library

- `ndarray` and `dataframe` gain operator support: `+ - * / **` and
  unary `-` work elementwise on arrays and numeric series (scalars on
  either side, broadcasting included), `< > <= >=` on arrays return
  0/1 masks (`a.where(a > 2.0)`), and dataframe expressions build
  with operators (`df.col("age") > 30`,
  `df.col("price") * df.col("qty")`). `==` and `!=` keep their
  language-wide meaning everywhere; `eq()`/`ne()` cover elementwise
  equality. Series operator results are ndarrays.
- `dataframe.pivot({"index", "columns", "values", "agg"})` spreads a
  column's values into per-value columns with aggregation (default
  `sum`; any groupBy aggregator except `collect`).

### Static analysis

- `geblang check` (and the compile path of `run`, `test`, and `build`)
  reports `error[semantic]` when a constructor call's explicit type
  arguments contradict a statically-known argument type
  (`Box<string>(42)`). Covariant passing stays clean; `any`-typed,
  named, and spread arguments defer to the runtime check.
- An unknown type name in an `instanceof` target is now an error
  instead of silently evaluating to false (`x instanceof Nope`).
  Cross-module trailing-name matching and generic type parameters
  stay recognized; the check stands down when an import's surface
  cannot be resolved.
- An unknown type name in a generic constraint clause
  (`<T implements Nope>`) is flagged at the declaration, like every
  other annotation position, instead of only failing when the
  function is called.

Older releases (1.0.0 through 1.19.0) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
