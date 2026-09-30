# Release Notes Archive

Release notes for versions prior to 1.30.0, kept for history. These
are not part of the published documentation site. For current
releases see the [release notes](18-release-notes.md).

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

## 1.19.0

### Language

- Uncaught errors render identically on both runtimes: a classed header
  (`uncaught ValueError: ...`) followed by a full stack trace with one
  line per frame, ending in `<top level>` with its line. The
  `bytecode runtime error at L:C:` prefix is gone.
- The bytecode runtime no longer drops the calling function's frame for
  `return f(x)` calls, and traces survive module boundaries and
  deferred calls intact. Deep tail-recursive frames collapse into a
  single `[xN]` trace line on both runtimes.
- Runtime faults (division by zero, index out of range) report as
  `RuntimeError` consistently, and `errors.frames()` returns structured
  frame data on both runtimes without string parsing.
- Trace frame naming is canonical: methods qualify as `Class.method` in
  declared case (inherited methods report the declaring class),
  anonymous functions render as `<closure>`. `reflect.methods` returns
  declared-case names on both runtimes.
- A throw escaping a deferred call now keeps its class and trace: it is
  catchable as its original class (previously the bytecode runtime
  reported a wrapped `RuntimeError`).
- Failing `geblang test` methods print the error's stack trace beneath
  the `FAIL` line. The REPL prints uncaught errors in the same
  canonical format.
- Generators support manual stepping on both runtimes: `next()`
  advances and returns the next value (`null` once exhausted),
  `done()` peeks for exhaustion without consuming, and `close()` ends
  iteration early. Manual stepping composes with `for-in`.
- Imports are required: using a module as a selector base without
  importing it is now a semantic error on both runtimes and in
  `geblang check` (previously the bytecode runtime resolved built-in
  modules bare while the evaluator threw at runtime). This is a
  behavior change for scripts that relied on the bare access.
- A failing destructor is reported identically on both runtimes: one
  stderr block with the error class, message, and `Class.~Name` frame
  attribution (previously the bytecode runtime wrote a wrapped form to
  stdout and the evaluator omitted the trace).
- Dict spread now works on cross-module instance methods
  (`inst.m(...opts)` where the class lives in another module);
  previously the bytecode runtime rejected it.
- Dicts nested inside containers now print in insertion order like
  top-level dicts, completing the 1.5.1 insertion-order contract (a
  leftover alphabetical sort applied only to nested rendering;
  `json.stringify` output remains alphabetical by design).
- The advisory `div-by-zero` warning covers literal `/ 0` alongside
  `//` and `%`, and REPL warnings no longer block evaluation: the
  snippet runs (and throws canonically) after the warning prints.

### Standard library

- New `ndarray` module: N-dimensional numeric arrays over contiguous
  typed storage (`float64` / `int64`). Constructors (`array`, `zeros`,
  `ones`, `full`, `eye`, `arange`, `linspace`), elementwise arithmetic
  with NumPy-style broadcasting, zero-copy slice/transpose/reshape
  views, comparisons with masked selection (`where`), whole-array and
  per-axis reductions, linear algebra (`matmul`, `dot`, `solve`,
  `inv`, `det`), and seeded random generation (`random`, `randn`).
  Identical on both runtimes; see the stdlib ndarray chapter.
- New `dataframe` module: columnar frames with typed columns
  (`float64` / `int64` / `string` / `bool`) and per-column null masks.
  Construction from dicts, records, CSV, JSON, and SQL queries;
  expression-based `filter`/`withColumn` (`dataframe.col("age").gt(30)`
  evaluated columnwise in native code); `sort`, `unique`, `rename`,
  `drop`, `dropNulls`, `fillNull`, `describe`; `groupBy().agg()` with
  count/sum/mean/min/max/std/first/last/collect; hash joins
  (inner/left/right/outer) and `concat`; `readCsv`/`writeCsv`,
  `fromQuery`/`toTable` for files and databases. Numeric columns
  expose their data as 1-D ndarrays via `Series.values()`. All verbs
  are immutable. Identical on both runtimes; see the stdlib dataframe
  chapter.

### Tooling

- `geblang build --docker` writes a ready-to-build `Dockerfile` next to
  the output binary (distroless base, NOTICES included, optional
  `--docker-port` for `EXPOSE`; existing files preserved unless
  `--force`).

### Database

- `db.Rows` is now a true streaming cursor: a `next()`/`row()` loop (or
  the new `for (row in rows)` iteration) holds one row at a time, so a
  million-row scan runs in constant memory on SQLite, PostgreSQL, and
  MySQL (measured ~830 MB down to ~39 MB). The random-access methods
  (`all`, `first`, `get`, `length`, `isEmpty`) cache from their first
  call; mixing styles has remaining-rows semantics.
- Connection pool options (`maxOpenConns`, `maxIdleConns`,
  `connMaxLifetimeMs`, `connMaxIdleTimeMs`) in the `db.Connection`
  options dict now apply at connect time (previously only
  `db.configure` applied them, and concurrent load against a shared
  connection churned the pool at Go's 2-idle default - 32 concurrent
  query tasks improved from 175 ms to 33 ms). With no pool options the
  idle pool defaults to 8.
- SQLite connections default to a five-second `busy_timeout` on every
  pooled connection, eliminating spurious `database is locked` errors
  under concurrency; override with your own DSN pragma.
- The functional helpers (`db.query`, `db.exec`, ...) accept
  `Connection`/`Transaction`/`Statement` objects as well as raw
  handles, and normalize `?` placeholders per driver like the class
  API.
- MySQL `TEXT`/`VARCHAR`/`DECIMAL` columns now arrive as strings
  (previously raw bytes); only `BLOB`/`BINARY` columns map to `bytes`.
- New env-gated live test suite for PostgreSQL and MySQL streaming
  (`GEBLANG_PG_DSN` / `GEBLANG_MYSQL_DSN`) and a `make bench-db`
  benchmark target.

### HTTP

- `http.serve`/`http.listen`: uncaught handler errors no longer send
  error text to the client. Production responses are a generic 500 and
  the server logs one line to stderr. Set the `debug: true` server
  option or the `GEBLANG_DEBUG` environment variable to log and return
  full traces during development. This is a behavior change.

## 1.18.0

### Language

- Signal interception: `sys.onSignal(name, handler)` traps `SIGINT`,
  `SIGTERM`, `SIGHUP`, `SIGQUIT` (plus `SIGUSR1`/`SIGUSR2` on unix);
  handlers run isolated, share state via `store`, and a handler that
  calls `sys.exit` runs runtime cleanup before terminating.
  `sys.clearSignal(name)` restores default delivery and
  `sys.raise(name)` signals the current process. `SIGKILL` is
  rejected.

### Bundling

- Built binaries answer standard first-argument flags: `--help`,
  `--version` (application name and version from `geblang.yaml` plus
  the engine version), and `--notices` for the embedded third-party
  licence text. `--` passes everything after it to the application
  untouched.

### Performance

- The bytecode VM shares once-prepared chunk metadata and constant
  pools across VM instances and recycles both wrapper and
  cross-module-call VMs through escape-guarded pools. Static class
  members move to a synchronized overlay (semantics unchanged) so the
  constant pool can be shared untouched. Server workloads that
  dispatch through callbacks and cross-module calls speed up
  substantially: a representative typed web route serves roughly 20x
  more requests per second than 1.17.0, with median latency cut from
  tens of milliseconds to ~1 ms, and VM-mode serving is now the
  fastest deployment path.
- Compiled instructions are a third smaller in memory (packed source
  positions, consolidated operand storage), cutting a loaded server's
  resident memory by ~15% under sustained load at unchanged speed.
- Native function calls are cached per call site in every run
  configuration (previously only in embedded VMs without a stateful
  host), removing two per-call map lookups from each stdlib call:
  regex-heavy loops run ~30% faster, and every pure-native call path
  benefits.
- The evaluator (the `geblang test` runtime) gains allocation-free
  integer arithmetic and comparison fast paths (identical floor
  division/modulo and overflow-promotion semantics to the VM),
  environments that hold small scopes inline without a map, and no
  scope allocation at all for blocks that declare nothing. Recursive
  integer workloads run about 60% faster under the evaluator.

### HTTP server

- `http.serve` and `http.listen` accept `opts.maxBodyBytes` to cap the
  request body; oversize requests are answered with 413 before the
  handler runs.
- `http.wait(server)` blocks until a listening server stops serving,
  pairing with `http.shutdown` for graceful-drain entrypoints (a
  signal handler can shut down a server the main goroutine waits on).

### Performance

- Small dictionaries (up to eight entries) now use an inline storage
  form instead of a hash map, so building them - parsing JSON,
  assembling option dicts, request handling - allocates once instead
  of several times. JSON round-trip throughput improves about 20%;
  dictionary semantics are unchanged.

### Regular expressions

- `re.compile(pattern)` and `pcre.compile(pattern, flags)` return a
  reusable `Pattern` object that carries the compiled expression, so
  a loop states the pattern once: `re.compile(p).test(text)`. The
  object's methods mirror the module functions without the pattern
  argument. Invalid patterns raise at compile time. Performance is on
  par with the cached module functions for a single hot pattern and
  steadier across several interleaved patterns.

### JWK / JWKS

- `crypt.jwk(pem, opts)` builds RFC 7517 public JWKs (RFC 7638
  thumbprint kids by default) for RSA, EC, and Ed25519 keys;
  `crypt.jwks(keys)` assembles the key-set document.
- `crypt.jwtVerify` accepts a JWKS or single-JWK dict as its key,
  selecting by the token's `kid` and pinning the algorithm to the
  matched key. `crypt.jwtSign` writes `opts.kid` into the token
  header.

### Argument binding

- One shared binder now orders and validates arguments for the
  compiler, the VM, and the evaluator, ending a family of
  per-implementation drift. Binding errors use one precise wording
  everywhere: `f missing argument a`, `f expects at most 2 arguments,
  got 3`, `f has no parameter c`, `f parameter a passed more than
  once` (anonymous callables report as `<closure>`).
- Two evaluator divergences are fixed by the unification: positional
  arguments after named ones now fill the next unassigned slot (as
  the compiler and VM always did), and named-parameter matching is
  case-insensitive on every path.

### Fixes

- `crypt.jwtVerify` without `opts.allowedAlgs` pins the accepted
  algorithm family to the key type (raw secret: HS only; RSA public
  key: RS only; EC: ES only; Ed25519: EdDSA only). This closes the
  algorithm-confusion forgery where an HS token minted with a public
  PEM as the HMAC secret verified against that PEM. Pass
  `opts.allowedAlgs` to widen or narrow the policy explicitly.
- Dict spread works on callable values on the bytecode VM, matching
  the evaluator: `let g = f; g(...{"q": "x"})` binds named arguments
  (and engages declared defaults) for function values, lambdas,
  closures, and reflection-obtained callables.
- `io.exists` returns `false` instead of throwing `IOError` when a
  path component is a regular file (`/some/file.txt/child`). An
  existence predicate never throws for a path that cannot exist;
  genuine faults (such as permission errors) still throw.

## 1.17.0

### Language

- Default parameters combine correctly with a variadic parameter. A
  signature like `f(int a, int b = 10, int ...rest)` now binds in every
  context (plain function, lambda, method, static method, constructor)
  and call shape (positional with the default engaged, named arguments,
  spread). Previously the bytecode VM rejected valid calls at compile
  time and the evaluator could crash on named arguments or method calls.
- A variadic parameter is typed as `list<T>` inside the function body,
  so list methods (`rest.length()`, `parts.join(sep)`) type-check.
  Previously the analyzer treated `int ...rest` as a bare `int`, which
  rejected valid code at top level and broke module compilation on the
  bytecode VM.
- Spread arguments now work in every dispatch context on both backends:
  constructors, instance methods, and static methods accept `...list`
  and `...dict` (extra dict keys are dropped, matching plain functions),
  including mixed positional-plus-spread calls.
- Defining a single ordering dunder enables all four comparison
  operators: `a > b` derives from `b.__lt(a)` (and `a < b` from
  `b.__gt(a)`); `<=` and `>=` negate the strict comparison when the
  direct dunder is missing. A defined dunder always wins over a derived
  one.
- `range.first` and `range.last` work in field form alongside
  `range.length`, matching the documented surface; empty ranges yield
  `null`.

### Fixes

- An exception thrown inside a generator keeps its class in the
  consuming loop on the bytecode VM, so `catch (ValueError e)` matches
  it (including subclasses and comprehension consumption). Previously
  the VM collapsed it into a generic runtime error.
- The evaluator derives `<=` and `>=` from a lone `__gt` / `__lt` the
  same way the bytecode VM always has, removing a latent divergence.

### Performance

- The bytecode VM dispatch loop fetches instructions by pointer instead
  of copying them: integer-loop and arithmetic benchmarks improve by
  roughly 7-15% and recursive call workloads by ~18%.
- Numeric literals are parsed once and cached on the syntax tree instead
  of re-parsed on every evaluation. Allocations in call-heavy evaluator
  workloads drop by about 23%, which speeds up `geblang test` runs.

### Testing

- Same-module test files: a `*_test.gb` that declares the same module
  name as a sibling module file runs inside that module under
  `geblang test`, so private functions, classes, constants, and module
  state are directly testable without exporting them (the Go
  same-package test convention). `geblang check` and the editor
  understand the convention; outside the test runner private members
  stay private. See the testing chapter for details.

### Tooling

- The parity fuzzer generates random required/default/variadic
  signatures called positionally, with named arguments, and with spread
  across all dispatch contexts, plus generators that throw typed
  errors - locking the new behavior against backend drift.

## 1.16.0

### Breaking: in-place collection mutators

- List mutators now mutate the receiver and return it instead of
  allocating a copy: `push`, `pop`, `prepend`, `unshift`, `insert`,
  `removeAt`, `remove`, `reverse`, `sort`, and `sortBy`. Accumulation
  loops are amortised O(1) per element; `xs = xs.push(v)` keeps working
  (the reassignment is now a no-op). Code that relied on the receiver
  being left unchanged must take a copy: `reversed()` and `sorted()` are
  the copy variants, and `copy()` / `deepCopy()` cover the rest
  (`xs.copy().sortBy(...)`).
- `set.add` and `set.remove` mutate in place and return the receiver.
- All in-place mutators raise `ImmutableError` on frozen receivers, and
  the growth methods enforce the declared element type (`TypeError`),
  including the compiler's fused `xs = xs.push(v)` fast path, which
  previously skipped both checks.
- `pop()` returns the receiver, not the removed element; use `last()`
  before `pop()` to peek.
- A function value now satisfies a `callable` element type in list
  growth checks (`list<callable>.push(fn)`).

### Language

- `for-in` loops and comprehensions now iterate dicts, sets, and strings
  directly on both backends. Dicts yield insertion-ordered `[key, value]`
  pairs (destructurable into two binders: `for (k, v in d)`), sets yield
  elements in their sorted `toList()` order, and strings yield
  single-character strings matching `.chars()`. Previously dict iteration
  worked only on the evaluator's `for-in`, set iteration only in evaluator
  comprehensions, and string iteration not at all.
- List patterns in `match` accept literal elements alongside binders:
  `case ["go", n] if (n > 10) => ...` pins positions by equality and
  captures the rest. Numbers (including negatives), strings, bools, and
  `null` work as literal elements.
- Arrow-bodied arms in match STATEMENTS now execute their action
  expression. Previously `match (cmd) { case "serve" => startServer(); }`
  - the documented action form - matched the case and silently did
  nothing on both backends; only the `case X:` block form ran.
- Generic function call-site inference projects through to constructed
  instances: `make("hello")` for `func make<T>(T v): Pair<T, T>` reports
  `reflect.typeBindings` of `{"A": "string", "B": "string"}` instead of
  the bare type-parameter name. The bytecode VM already behaved this way;
  the evaluator now matches.

### HTTP

- The request builder gains `withBodyFile(path)`: the file streams from
  disk as the request body with `Content-Length` taken from the file
  size, so large uploads never load into memory. `withBody`, `withJson`,
  and `withBodyFile` replace each other.

### Reflection

- `reflect.function` now resolves native module functions by qualified name
  (`reflect.function("math.sqrt")`) to a first-class callable, the same value
  the bare `math.sqrt` expression produces. Import aliases work
  (`import math as m` resolves `"m.sqrt"`); unknown members return `null`.
- `reflect.function` accepts a native function value directly on both
  backends, and structural introspection (`reflect.parameters`,
  `reflect.location`, `reflect.returnType`) degrades gracefully for native
  functions (empty parameters, `null` location, `void` return) instead of
  raising on the bytecode VM.
- `reflect.module` resolves pure native modules even in loader-less
  embeddings of the VM.
- All `reflect.*` functions dispatch without `import reflect` on the
  evaluator, matching the VM (previously only `reflect.function`,
  `reflect.class`, `reflect.module`, and `reflect.classes` were ambient
  there; `reflect.parameters` and the rest required the import).

### Fixes

- Calling an undeclared bare name (for example a misspelled function or a
  non-existent error class) is now a static error on the evaluator path
  too, matching the bytecode compiler. Previously `geblang run
  --disable-vm` and `geblang test` ran such programs until the bad call,
  which could be silently swallowed by `try/catch`.
- A literal division or modulo by zero (`5 // 0`, `5 % 0`) is no longer a
  hard compile error on the bytecode VM. It throws a catchable runtime
  error on both backends, matching evaluator semantics; `geblang check`
  flags it with `warning[div-by-zero]`.
- The bytecode VM's integer fast paths reported "integer division by zero"
  for a modulo by zero; they now say "modulo by zero" like the evaluator
  and the generic path.
- Runtime error messages for unknown primitive methods and unsupported
  binary operands are now identical on both backends ("unknown method
  set.bogus"; "unsupported operands for -: string and int"). The VM
  previously said "set has no method bogus" and the misleading "left
  operand must be numeric".
- `await` now rethrows the error raised inside an async function with its
  original class and message on the bytecode VM, so typed catch clauses
  (`catch (ValueError e)`) match it. Previously the VM collapsed the failure
  into a generic runtime error that only a base `catch (Error e)` could see,
  with a mangled message. The evaluator already behaved correctly; both
  backends now agree. This also applies to `task.await()` and
  `async.await(task)`.

## 1.15.0

### Standard library

- `datetime.Instant` is the canonical datetime object: construct it from calendar
  components (`datetime.Instant(2024, 1, 15)`), the current time
  (`datetime.Instant()`), a unix timestamp, or an RFC3339 string; copy it with
  `.copy()`; and list its methods with `dir`. It is immutable - every operation
  returns a new instant. The unused interpreter-only `DateTime` reference class
  was removed; use `datetime.Instant`.

### Fixes

- Deep-cloning a module value now preserves the module's canonical identity.
  Previously, contexts that deep-clone a captured environment (per-request
  request handlers, `clone.deep`, goroutine isolation) dropped it, so a native
  function called through an aliased import (for example a module imported under
  a short alias) could resolve against the wrong module and fail with an
  "unknown function" error. Cloned handlers now resolve aliased native calls
  correctly.
- Using a module name as a value (assigning it to a variable, passing it to a
  function, returning it, or storing it in a collection) is now a clear
  compile-time error on both backends. Reference a module's members with
  `module.member` or alias the import with `import module as name`. Module
  introspection by name uses `reflect.module("name")` (a string).
- `dir(<module>)` now works on both backends (it previously failed to compile a
  bare module name on the bytecode backend). Under the evaluator it lists a
  module's full accessible member surface.
- `reflect.module`, `reflect.class`, and `reflect.function` now resolve imported
  native modules consistently on both backends. Previously the bytecode backend
  returned null for `reflect.module("<native>")` and failed to compile
  `reflect.class` / `reflect.function` over a native module; both now match the
  interpreter (a native module's class exports are reflectable; native functions
  remain non-reflectable and return null).
- Reflection over imported user-module members is now consistent on both
  backends. `reflect.location(target).module` reports the declaring module's
  canonical name on the interpreter (it previously left it blank); `reflect.function("name")`
  resolves a function exported by an imported module by bare name on the
  interpreter (it previously returned null); and `reflect.location` of a
  qualified-name class lookup (`reflect.class("module.Class")`) reports the
  declaring module and source position on the bytecode backend (it previously
  returned null).

## 1.14.0

### Standard library

- New profiling context managers in the `profiler` module. `profiler.timer()`
  brackets a block with `with` and reports wall-clock time afterwards
  (`elapsedMs()` at microsecond precision, `elapsedNs()`); `profiler.profile()`
  additionally captures CPU and memory (`elapsedMs()`, `cpuMs()`, `heapBytes()`,
  `allocs()`, `gcCount()`, and a full `report()` dict). Create the object, run
  the work inside `with (...)`, then read the results from the object after the
  block.
- New `time.monotonicNs()`: a monotonic nanosecond clock for high-resolution
  duration measurement. The existing `time.monotonic()` is millisecond-grained.

### Tooling

- The editor now offers completion and hover for every standard-library module
  written in Geblang, including their classes and methods. Many source-backed
  modules (for example the async primitives, the string builder, and others)
  were previously absent from completion.

### Fixes

- Context managers now run `__enter` and `__exit` for an object whose class is
  defined in an imported module when running compiled bytecode. These were
  previously skipped on that path, so a `with` block over an imported class
  could leave its setup or cleanup unrun.

## 1.13.0

### Standard library

- New `image` module: a portable, native raster-image toolkit that needs no
  system library. Decode PNG / JPEG / GIF / WebP from a file or bytes, create
  blank images, and transform via resize (high-quality resampling), crop, and
  90-degree rotation. Encode back to PNG / JPEG / GIF. Each transform returns a
  new image; the source is left unchanged. Released through the `Image` class
  plus `image.load`, `image.loadBytes`, and `image.blank`.
- New `clib.zstd` module: Zstandard compression over the system libzstd, with
  `compress(data, level = 3)` and `decompress(frame)`.
- New `clib.magic` module: content-based file type and MIME detection over the
  system libmagic, via `detect(path)`, `mime(path)`, and a `Magic` class for
  reuse and buffer input.
- New `clib.systemd` module (Linux): the sd_notify readiness protocol
  (`ready`, `watchdog`, `status`, and raw `notify`) and structured journald
  logging (`journal`), over the system libsystemd.
- New `clib.curses` module: a full-screen terminal UI surface over ncurses
  (screen lifecycle, cursor movement and output, key input, colour pairs, and
  text attributes). Single-owner: drive it from one task.

The `clib.*` modules load system shared libraries through the in-process FFI, so
they require FFI to be enabled in `geblang.yaml` (or `--allow-ffi`). Each is
safe to call from any async task except where its docs note a per-handle lock or
a single-owner constraint.

### Type checking

- Static type checking now runs on the compile path on both runtimes, not only
  in `geblang check`. A type error is reported before execution by `geblang run`,
  `geblang test`, and `geblang build` alike, and the two runtimes agree on what
  they reject.
- Type checking now extends into class method and constructor bodies. Argument
  types, member access on `this.field` and on typed locals, and `return`
  expressions are validated inside methods, not only in top-level and
  free-function code. The checks reach across module boundaries: a call on a
  class inherited from another module is validated against the inherited
  signature.
- Unknown type names in annotations are flagged. A bare type name used in any
  annotation position (parameter, return, field, variable, generic argument,
  nullable, union, catch clause, or `as` cast) that resolves to no known type
  (primitive, declared class, interface, enum, type alias, in-scope generic type
  param, or built-in error class) is an error at both `geblang check` and compile
  time, so a typo in a type hint is caught before it runs. A module-qualified
  type name whose module does not export that name is flagged by `geblang check`.
- Type-mismatch errors are clearer. A failed call now names the specific
  parameter and the expected and actual types (for example,
  `g expects int for parameter 'x', got string`) rather than a generic
  "no matching overload". When an unknown-type error already explains a bad
  signature, the redundant follow-on error is suppressed.

### Language

- A class imported with `from module import Name` can now be used directly as a
  parent class, the same as the qualified `extends module.Name` form.
- Cross-module inheritance now behaves identically on both runtimes in every
  position. Calling an inherited method, reading or writing an inherited field,
  `instanceof`, static members, and interface default methods all resolve
  correctly when a class extends a class, or implements an interface, declared in
  another module, including through a local intermediate subclass and across
  multi-level chains.
- An interface default method is now available on a subclass of the implementing
  class on both runtimes.

### Fixes

- Multi-level `parent.method()` chains now resolve to the correct ancestor on
  both runtimes. A method that calls `parent.method()` where that ancestor also
  calls `parent.method()` no longer recurses on itself.
- Several type-checker false positives were corrected: passing `null` to an `any`
  parameter, assigning `null` to a nullable variable after a non-null narrowing,
  and returning or assigning a value to a generic type parameter are no longer
  wrongly rejected.
- The JSON, XML, CSV, and YAML streaming readers, and the in-memory IO buffer,
  stream, and capture objects, now work on the bytecode runtime (`geblang run`
  and `geblang build`), matching the evaluator. `reader.next()` and
  `reader.hasNext()` produce the same event stream on both.

### Documentation

- The internals reference now documents how the FFI is implemented and its
  threading and thread-safety model: FFI calls run on the calling goroutine's OS
  thread, native library state is the caller's responsibility, and `errno` is
  valid only immediately after a call.
- The language and standard-library reference received a broad accuracy pass:
  corrected examples and signatures, a worked cross-module inheritance example,
  the `ImmutableError` built-in, the JSON streaming-reader API, and the current
  scope of `geblang check`.

## 1.12.0

### Classes

- Fields can be declared set-once with a field-level `@immutable` decorator. A
  set-once field is writable while the constructor runs and locked afterwards; a
  later assignment raises `ImmutableError`, while other fields stay mutable. The
  lock is inherited by subclasses. An `@immutable` field may not declare a
  default value.
- A class that defines `__string` is now rendered through it by string
  interpolation (`"${x}"`), `io.println`, and `io.print`, not only by an
  explicit `as string` cast. Classes without `__string` keep the default
  inspection form.
- New `@dataclass` decorator generates a constructor, value-based equality, a
  `__string` rendering, and a `with(...)` copy helper from a class's declared
  fields. `@dataclass(frozen: true)` also makes instances immutable. Any member
  written by hand overrides the generated one. Operates on the class's own
  fields; a data class that extends another class declares its own constructor.
- Frozen instances (whole-class `@immutable` or `@dataclass(frozen: true)`) are
  now usable as dict keys and set members by value: two frozen instances with
  equal fields are the same key. Mutable instances continue to key by identity.
- New `@override` decorator asserts that a method overrides an ancestor method;
  a method marked `@override` that overrides nothing is a compile-time error.
  The check is by name and skips parents the analyzer cannot resolve.
- New `@deprecated("message")` decorator marks a function, method, or class for
  removal. `geblang check` reports every use site as `warning[deprecated]` with
  the optional message. Advisory only; it never changes whether code runs.
- New `@memoize` decorator caches a top-level function's result by its arguments
  (unbounded, per-process); recursion through the function's own name is memoized
  too. Applying it to a method, async function, generator, or void function is a
  compile-time error.

### Fixes

- Field-level `@immutable` is now enforced across module boundaries: a subclass
  inheriting a set-once field from a parent in another module can no longer
  mutate it after the parent constructor runs. The field locks when its
  declaring constructor completes, on both runtimes.
- `__string` is now applied consistently across runtimes for implicit string
  rendering; previously string interpolation rendered an instance through
  `__string` under one runtime but used the default inspection form under the
  other.

- Whole-class `@immutable` is now preserved through the bytecode cache. It was
  dropped when a class's compiled chunk was loaded from a `.gbc` cache or a
  built binary (the class immutability flag was not serialized), so a frozen
  instance could be mutated on a second run. Class immutability metadata now
  round-trips through encode/decode. The bytecode format version increments, so
  stale caches recompile automatically.

## 1.11.0

### Bundling

- `geblang.yaml` gains a `resources:` list. `geblang build` embeds the listed
  files (directories are embedded recursively; glob patterns match files) into
  the bundle at their project-relative path, so a built binary can ship
  templates, static assets, and data files. A pattern that matches nothing is a
  build error. `geblang build --resource <path>` embeds additional resources
  beyond the manifest list; `--resource <path>=<bundlePath>` remaps a resource's
  bundle location, so a build step can embed processed copies without altering
  the source tree.
- New `sys.bundleDir()` returns the extract directory of a built binary's
  embedded resources, or `""` when not running from a bundle. Resolve resource
  paths against it (falling back to the project directory when empty) so the
  same code reads its files in development and in a built binary.
- `geblang build` now embeds source-backed standard-library modules (such as
  `async.sync`) that are also natively registered. Previously these were skipped
  as native and left out of the bundle, so a built binary that used them failed
  at runtime. Modules with no source remain provided by the runtime binary.

### Running

- Running a file directly (`geblang <file>`) now auto-invokes an exported
  top-level `main` when one is declared: a `module` that `export func main(list
  <string> args)` runs the same whether executed directly or built with `geblang
  build`. Command-line arguments are forwarded to `main`, and an `int` return
  value becomes the process exit code. Files with no exported `main` run as
  scripts exactly as before.

### Fixes

- Fixed a bytecode-VM bug where a closure created inside a `for` or `while` loop
  captured variables incorrectly: a closure that returned could crash, and a
  `let` declared in the loop body was shared across iterations instead of being
  a fresh binding. The VM now matches the evaluator exactly - a loop-body `let`
  is a fresh binding each iteration (closures stored per iteration keep their
  own value), the loop variable itself is a single shared binding, and
  assignment to a captured variable is visible through every closure that
  captured it.

## 1.10.0

### Numeric types

- Mixed-type numeric operations now follow one consistent, precision-safe rule
  on both backends. `int` and `float` mix in arithmetic by promoting the int to
  float (`3 + 2.5f` is `5.5`). `decimal` and `float` still may not mix in
  arithmetic (a `decimal` is exact, a `float` is not), so `2.5 + 2.5f` is a clear
  error - convert explicitly. `int` and `decimal` continue to mix exactly.
- Comparisons (`== != < > <= >=`) and membership (`in`, `.contains()`) now work
  across all numeric types and compare by exact value. Previously `3 == 3.0f`
  returned `false` (comparing types, not values) and `3 < 2.5f` was an error;
  now `3 == 3.0f` is `true`, `2.5 == 2.5f` is `true`, and `0.1 == 0.1f` is
  `false` (the binary float is genuinely not one tenth). No precision is lost and
  comparisons never error on numeric operands.

### Math

- New `math.lerp(a, b, t)` (linear interpolation, `a + (b - a) * t`) and
  `math.remap(x, inLow, inHigh, outLow, outHigh)` (linearly remap `x` from one
  range onto another). Both preserve precision: `int`/`decimal` inputs compute
  exactly and return a `decimal`, `float` inputs return `float`, and mixing
  `float` with `int`/`decimal` is an error (matching the arithmetic operators).
  `remap` errors on a zero-width input range. Neither clamps. Useful for
  precision-safe interpolation over lookup tables (fee schedules, rate bands).

### Modules

- Built-in module names (every native and stdlib module) are now reserved: a
  program or package module may not declare one of these names, and a built-in
  name always resolves to the built-in, identically on the evaluator and the
  bytecode VM. This removes a divergence where a local source file could shadow
  a built-in on one backend but not the other. The reservation is on the
  declared module name, not the filename, so a namespaced module (e.g.
  `module myapp.errors;` in a file named `errors.gb`) is unaffected. A collision
  is reported by `geblang` and `geblang check`.
- New reserved `geblang.` import prefix: `import geblang.json` resolves
  explicitly and unambiguously to the built-in module, regardless of local
  files. The `geblang.*` namespace is reserved for built-ins.

### AI and retrieval

- New `PgVectorStore` (in `vectorstore`): a Postgres + pgvector backend behind
  the existing `VectorStore` interface. Uses a typed `vector(D)` column, a
  metric-matched HNSW index, and `jsonb` metadata, with index-using approximate
  nearest-neighbour queries - the same shape as idiomatic pgvector usage. Built
  on the `db` module; no new dependency. The extension, table, and index are
  created on construction; `add` upserts by id.
- New `HnswVectorStore` (in `vectorstore`): an in-process HNSW index for
  sublinear approximate-nearest-neighbour search with no external service - the
  middle ground between the exact O(n) in-memory store and a database backend.
  Tune recall with `m` and `efSearch`. Behind the same `VectorStore` interface.
- New `searchFilter(query, k, criteria)` on every vector store: a portable,
  dict-based metadata filter (`{"field": value}` for equality; nested
  `{"field": {"gte": x}}` for `gt`/`gte`/`lt`/`lte`/`ne`/`in`). In-memory and
  SQLite stores apply it in process; `PgVectorStore` pushes it down to SQL as
  jsonb containment and range predicates. The callable `searchWhere` remains for
  arbitrary in-process predicates.

### Testing

- Tests can now be skipped. `this.skip(reason)` skips at runtime (for
  conditional cases such as an integration test that needs a service or
  environment variable); the `@Skip` / `@Skip("reason")` decorator skips a method
  unconditionally. Skips count separately from passed/failed, do not affect the
  exit code, and appear in the summary (`skipped=N`) and as `SKIP` lines under
  `--format verbose`. `test.run()` results gain a `skipped` count.

## 1.9.0

### Tooling

- `geblang check` now flags method calls whose arguments match no overload -
  wrong argument type, wrong element type (e.g. `list<int>` into a
  `list<string>` parameter), or wrong arity - the way both backends already
  reject them at runtime. Previously only free-function calls were validated;
  method calls were unchecked. The check stays conservative (silent on generic
  parameter positions, untyped parameters, variadic overloads, or unknown
  argument types) to avoid false positives.

### Collections

- New `seq` module: `seq.stream(source)` wraps any iterable (list, set, range,
  generator) in a lazy, single-use fluent pipeline. Intermediate operations
  (`map`, `filter`, `flatMap`, `take`, `drop`, `takeWhile`, `dropWhile`,
  `distinct`, `peek`, `sorted`, `sortedBy`) build a generator chain and run
  nothing until a terminal operation (`toList`, `toSet`, `forEach`, `count`,
  `reduce`, `first`, `firstOr`, `find`, `any`, `all`, `none`, `sum`, `min`,
  `max`, `join`) pulls values through once, so no intermediate lists are
  materialised and huge or unbounded sources stay cheap. The lazy counterpart
  to the eager `collections` module and the built-in list methods.

### AI and retrieval

- New `vectorstore` module: stores `(id, vector, metadata)` records and ranks
  them by similarity (cosine, dot, or euclidean). `MemoryVectorStore` is a
  mutex-guarded brute-force in-memory store; `SqliteVectorStore` persists
  vectors as float32 BLOBs through the `db` module (table auto-created, upsert
  by id). Both share a `VectorStore` interface with `add`, `addAll`, `get`,
  `delete`, `search`, `searchWhere` (metadata-filtered), `count`, and `clear`.
  Vectors are stored packed as float32 and scored by the native `vecmath`
  kernel, keeping search off the interpreted path.
- New `vecmath` module: float32 similarity kernels - `score(metric, a, b)` and a
  batched `topK(vectors, query, k, metric)` - over vectors given as lists or
  packed float32 blobs.
- New `rag` module: retrieval-augmented-generation helpers on top of
  `vectorstore`. `chunk` splits text into overlapping windows (by words,
  characters, or paragraphs); `index` chunks, embeds, and stores a document;
  `retrieve` returns the most similar chunks for a query; `context` assembles
  them into a prompt-ready block. Built on a small `Embedder` interface (with an
  `LlmEmbedder` adapter) so it stays provider-agnostic and testable without a
  network.

### Fixes

- `collections.sortBy(list, selector, descending)` now accepts the optional
  `descending` flag through the module-function form, matching the list-method
  form and both backends.
- Database parameter binding now accepts plain integers and `bytes` values
  (BLOBs); query results now decode the full range of integer and float column
  types returned by the supported drivers.
- Spreading a list into a native function's variadic parameter (`f(...list)`)
  now expands correctly when running on the bytecode VM, matching the evaluator.

## 1.8.0

### Dict-like objects

- Classes can opt into subscript syntax with the `__index(key)` and
  `__setIndex(key, value)` magic methods, so `obj[key]` and `obj[key] = value`
  dispatch to the class.
- New `in` membership operator: `key in collection` returns a bool for lists
  (element), dicts (key), sets, strings (substring), and ranges, and dispatches
  to `__contains(key)` on user objects. Negate with `!`; a range literal needs
  parentheses (`x in (1..10)`). The `for x in collection` loop is unchanged.
- New `maps.DictInterface` stdlib interface: implement `__index` + `keys`
  (and optional `__setIndex`) to inherit `contains`, `get`, `values`,
  `length`, `isEmpty`, and `__contains` (so `in` works) as defaults.

### HTTP client

- HTTP client calls (`http.get`, `http.post`, `http.request`, the request
  builder's `send`, the client methods, and `fetchAll`) now return a rich
  `Response` object with reader methods: `status()`, `ok()`, `text()`,
  `bytes()`, `json()`, `body()` (the raw body value, the method form of
  `resp["body"]`), `header(name)`, `headers()`, plus the status
  predicates `isSuccessful()`, `isRedirect()`, `isClientError()`,
  `isServerError()`, and `isNotFound()`.
- The `Response` object stays index-compatible with the previous dict shape:
  `resp["status"]`, `resp["body"]`, and `resp["headers"]` still work, and
  `resp.toDict()` returns the plain dict. Existing code keeps working
  unchanged.
- New immutable request builder: `http.request(url)` (one argument) starts a
  fluent builder with `withMethod`, `withHeader`, `withHeaders`, `withQuery`,
  `withBody`, `withJson`, `withBearer`, `withBasicAuth`, `withTimeout`, and
  `send`. Each `withX` returns a fresh builder, so a base builder can be
  reused for several requests without leaking state.
- New `http.getAll(urls, {limit})` performs parallel GETs and returns a list
  of Responses in input order. `http.fetchAll` now accepts request builders
  as well as spec dicts, and both take an optional `{limit}` to cap
  concurrency. A configured client's `fetchAll` gains the same options.
- In a parallel batch, a request that never completes a round trip (DNS
  failure, connection refused, timeout) is reported as a `Response` with
  `isError()` true and the message in `error()` (status `0`), so the result
  list is uniform and one failure does not abort the batch. `resp["error"]`
  still returns the message.

### HTTP server

- Handlers can opt into a rich `Request` object by declaring the parameter as
  `Request` (the plain request-dict handler stays the default). The object
  adds `scheme()`, `isSecure()`, `host()`, `clientIp()`, `isMethod(name)`,
  `isJson()`, `text()`, `cookie(name)`, typed query getters `query`,
  `queryInt`, `queryBool`, `queryAll`, and route-parameter accessors
  `routeParam(name)` / `routeParams()` (route params also appear in
  `toDict()`). Object handlers now also work under the bytecode VM, not just
  the evaluator.
- New `http.redirect(url, status=302)` builder returns a `Response` with the
  `Location` header set; it shares the status predicates with all responses.
- Plain request-dict handlers now also receive the proxy-aware `scheme`,
  `host`, and `clientIp` keys (resolved like the `Request` accessors,
  honoring `trustedProxies`), plus `_clientCert` for a verified mTLS peer.
- New `trustedProxies` server option (a list of IPs/CIDRs, or the keyword
  `"private"`). When the immediate peer is trusted, `clientIp()` is taken
  from `X-Forwarded-For`, `scheme()`/`isSecure()` from `X-Forwarded-Proto`,
  and `host()` from `X-Forwarded-Host`. Otherwise the forwarded headers are
  ignored so a client cannot spoof its address.
- Mutual TLS: a server `tls` block accepts `clientCa` (PEM CA pool) and
  `clientAuth` (`"require"` or `"optional"`) to verify client certificates.
  A rich `Request` exposes the verified peer certificate via `clientCert()`
  (`subject`, `issuer`, `serialNumber`, `notBefore`, `notAfter`, `dnsNames`,
  or null). Outbound client certificates (`tls.clientCert`/`clientKey`) were
  already supported.
- Connection-level server errors (TLS handshake failures, malformed requests)
  are now quiet by default instead of being written to standard error. Pass an
  `onError` callback in the server options to observe them; it receives one
  message string per failure. These happen before any handler and cannot be
  caught as Geblang errors, so the callback is the way to log or count them.
- Automatic certificates: a server `tls` block accepts `autoCert` (a host or
  list of hosts), with optional `autoCertCacheDir` and `autoCertEmail`, to
  obtain and renew ACME (Let's Encrypt) certificates via the TLS-ALPN-01
  challenge on the same listener. HTTPS servers also negotiate HTTP/2
  automatically.
- `web.router` handlers and middleware can opt into the rich `Request` and
  `Response` objects by declaring those parameter types (an after-middleware's
  second parameter typed `Response` receives one), and may return a `Response`.
  This is opt-in by type; `dict<string, any>` handlers and middleware are
  unchanged.
- Server request handlers (`http.serve`, `http.listen`, `net.serve`) now run
  with per-request isolated state on both the evaluator and the bytecode VM: a
  handler's mutations to captured state are private to that request and cannot
  race a concurrent request, so a handler that touches a captured value no
  longer risks a crash under concurrent load. Such state does not persist across
  requests - share cross-request state through a thread-safe handle (database,
  cache, key-value store) rather than a captured mutable container.

### Encoding and templates

- New `encoding.sanitizeHtml(html)` sanitizes untrusted HTML against a safe
  allow-list (keeps common formatting tags, strips scripts/styles and `on*`
  event handlers) - for rendering user-submitted HTML. This complements
  `encoding.htmlEscape`, which neutralizes all markup.
- `encoding.base64Encode` now accepts a `string` or `bytes` (matching the
  other base encoders).
- Breaking: `encoding.base64UrlDecode` now returns a `string` (consistent with
  `encoding.base64Decode`), so URL-safe Base64 round-trips strings without a
  manual conversion. For binary, decode with `bytes.fromBase64Url` (or
  `bytes.fromBase64`), which return `bytes`.
- The `template` module reference now documents the full engine (data binding,
  `if`/`range`/`with`, pipelines, `Engine`/`load`/`Template.render`) and its
  contextual auto-escaping.

### Concurrency

- New `sys.goroutineId()`: returns the current goroutine's id (positive, stable
  within a goroutine, unique among live goroutines). An advanced primitive for
  goroutine-local / request-scoped state, e.g. keying a `store.Store` by it.
- New `store.Store`: a thread-safe shared key-value store for state shared
  across concurrent tasks or request handlers. Every operation is serialised
  internally and values are deep-copied in and out (isolated snapshots), with
  atomic `incr`, `getOrSet`, `compareAndSet`, and `update(key, fn)`. Sharing a
  plain dict/list across goroutines is unsafe; reach for a `Store` whenever you
  need a shared mutable map. A lower-level functional API (`store.new()`,
  `store.get(h, key)`, ...) backs the class.

### Other

- `typeof(x)` can now be compared to a type name string: `typeof(x) ==
  "int"` and `typeof(obj) == "Response"` work as expected. `typeof` still
  returns a type value, so `typeof(x) == int` keeps working too.
- `geblang build` now writes a `<output-path>.NOTICES.txt` sidecar with the
  third-party attribution notices for the components the binary embeds, so a
  distributed binary stays licence-compliant. It is a sidecar file, not a
  built-in flag, so it never clashes with a `licenses` argument the built
  program may define.
- `geblang check` now flags collection element-type mismatches that only the
  runtime caught before, e.g. passing a `list<int>` where a `list<string>` is
  expected. Built-in collections stay covariant, so a `list<Dog>` into a
  `list<Animal>` parameter and any collection into `list<any>` remain clean;
  only genuinely unrelated element types are reported.
- `sockets.serve(host, port, handler)` now hands the callback a typed `Socket`
  (the same type `sockets.dial` returns: `for (line in conn)`, `readLine`,
  `writeln`, `close`, `localAddr`/`remoteAddr`) instead of a raw
  `{handle, stream, ...}` dict. Breaking: a handler typed `dict<string, any>`
  that read `raw["stream"]` should now take a `sockets.Socket` and use it
  directly.

### Fixes

- `for (x in obj)` where `obj.__iter()` returns another object that itself
  needs iterator resolution (for example an object whose `__iter` returns a
  second iterable object, or a stream) now follows the chain on the
  tree-walking evaluator, matching the bytecode VM. Previously the evaluator
  stopped at the first hop and reported the inner object as "not iterable".
- Interface default methods now resolve correctly when invoked outside the
  method-call path (the `in` operator, subscript access, serialization,
  reflection) and across module boundaries: a cross-module interface default
  can call sibling default methods on `this`. Previously these silently failed
  to find interface-default implementations.
- HTTP handlers can now use app-global handles created at setup. A handler runs
  in a callback evaluator, and web-app, http-client, and cookie-jar lookups now
  resolve through the parent (db and logger handles already did), so an app
  with routes registered up front serves correctly over a real socket instead
  of failing with "unknown web app handle". Handle ids created inside a handler
  stay isolated to that request.
- A module-qualified call such as `mod.foo()` no longer mis-binds to a
  same-spelled class that differs only in case (for example a `Mod` class in
  scope) on the bytecode VM. Call-site dispatch and static-value access are now
  case-sensitive on both backends, matching the tree-walking evaluator.
- Calling a method that does not exist on a native class instance (for example a
  `Response`) now raises a clear "unknown method" error on the bytecode VM,
  matching the evaluator, instead of the misleading "module ... is not loaded".
- `reflect.class(instance)` now includes the class's own decorators on the
  bytecode VM when the class is declared in another module, matching the
  evaluator. Previously class-level decorators were dropped across a module
  boundary on the VM, so code reflecting over a class from a different module
  (the pattern frameworks use to read class annotations) saw none of them.

## 1.7.2

### Indexed iteration

- `enumerate()` (list method and `collections.enumerate`) pairs each element
  with its index, so you can iterate with the index in hand:
  `for i, v in xs.enumerate()`. Lists previously had no indexed-iteration form
  (dicts already support `for k, v in d`).

### Multiple return values

- A function can return several values with `return a, b`, and the caller
  unpacks them with `let a, b = f()` or `a, b = f()`. The swap idiom
  `a, b = b, a` works too. Values are carried as a list (so the return type is
  a list); `let a, b = ...` is shorthand for list destructuring.

### `const` parameters

- A function parameter can be declared `const` (`func f(const list<int> xs)`)
  to make it read-only: the argument is shallow-frozen on entry, so mutating
  it inside the function raises `ImmutableError` while the caller's value is
  left untouched. Documents and enforces that a function only reads an
  argument.

### Deep copies

- New `clone.deep(value)` returns a deep copy of any value - containers and
  class instances are cloned recursively, primitives pass through, and
  resource handles are left as-is. Self-referential lists and object cycles
  are handled.
- Lists, dicts, and sets gain a `deepCopy()` method, the deep counterpart of
  the shallow `copy()`.

### Fixes

- `dict.copy()` now preserves insertion order on both backends (the
  tree-walking path previously returned the entries in an arbitrary order).

- Concurrent field access on an object shared across async tasks no longer
  crashes the interpreter. A per-instance guard makes ordinary field reads
  and writes safe under parallelism; it is gated so sequential code keeps
  its previous speed. Logical correctness for shared mutable state still
  needs a lock or atomic (see the async chapter); whole-object reads such as
  serialising or reflecting over an object while another task mutates it
  remain a data race to synchronise.
- Iterating a channel with `for x in c` no longer mutates the shared channel,
  so producer tasks sending on other goroutines can no longer trigger a
  concurrent-access crash. Iteration now uses a per-consumer cursor, which also
  lets two consumers iterate the same channel without clobbering each other.

### String ergonomics

New string methods:

- `capitalize()` / `title()` - upper-case the first character, or title-case
  each word (the rest is lower-cased).
- `removePrefix(p)` / `removeSuffix(s)` - strip a fixed affix if present.
- `lines()` - split on line boundaries (LF and CRLF; no trailing empty).
- `isBlank()` - true when empty or only whitespace.
- `equalsIgnoreCase(other)` / `containsIgnoreCase(sub)` - case-insensitive
  comparison and substring test.

### More collection operations

Seven new list operations, available both as methods (`xs.flatMap(f)`) and as
`collections` module functions (`collections.flatMap(xs, f)`):

- `flatMap(fn)` - map each element to a list and concatenate.
- `uniqueBy(fn)` - remove duplicates compared by a key function.
- `takeWhile(fn)` / `dropWhile(fn)` - leading run by predicate, and the rest.
- `windowed(size, step=1)` - overlapping sliding windows (complements `chunk`).
- `unzip()` - inverse of `zip`: a list of pairs becomes `[firsts, seconds]`.
- `scan(initial, fn)` - running fold returning every intermediate accumulation.

### Time ergonomics

- `time.humanize(ms)` renders a millisecond duration as a compact string:
  `45ms`, `1.5s`, `3m 4s`, `2h 5m`, `1d 1h` (largest one or two units).
- New `time.stopwatch` module with a monotonic `Stopwatch` class
  (`elapsed`, `elapsedFloat`, `lap`, `reset`) for lap timing without
  juggling timestamps. Backed by `time.monotonic()`, so it is immune to
  wall-clock jumps.

### Datetime ergonomics

- `Instant` gained direct part accessors so you no longer index a parts dict:
  `year()`, `month()`, `day()`, `hour()`, `minute()`, `second()`,
  `weekday()` (ISO 1=Monday .. 7=Sunday), `dayOfYear()`, `isWeekend()`,
  plus `inZone(zone)`.
- `Instant` comparisons and conversions: `isBefore`, `isAfter`, `equals`,
  `sub(duration)`, `toUnix`, `toUnixMillis`, `toUnixNanos`, `formatHTTP`.
- `Duration` arithmetic and conversions: `add`, `sub`, `abs`, `negate`,
  `inSeconds`, `inMillis`, `inNanos`.
- `Zone.offset()` returns the current UTC offset (alongside the existing
  `offsetAt(instant)`).
- `format` and `parse` accept friendlier layouts: strftime tokens
  (`%Y-%m-%d`) and preset names (`iso`, `date`, `time`, `datetime`, `http`),
  in addition to the existing Go reference-time layout. `parse(text, layout)`
  now takes an optional custom layout.

### Native module functions are first-class values

A function from an imported native module can now be referenced as a value,
not just called - `let f = math.abs;` or `xs.map(math.abs)` after
`import math`. This completes the first-class-function story: builtin type
statics (`string.compare`) already worked bare, and native module functions
now work once their module is imported.

### Grapheme clusters (user-perceived characters)

Strings gain `graphemes()`, `graphemeLength()`, and `truncateGraphemes(n)`,
which work in Unicode grapheme clusters (UAX #29) rather than code points.
A combining sequence (`"e\u{301}"`) or an emoji ZWJ sequence (a family emoji)
counts as one grapheme even though it is several code points, so these are
the right tools for display width, truncation, and cursor movement.
`length()` / `chars()` / `codePoints()` remain code-point based.

### Clearer sorting and searching

Sort callbacks are now consistent and more flexible. `xs.sort(cb)` /
`xs.sorted(cb)` accept either a less-than predicate `(a, b) -> bool` or a
three-way comparator `(a, b) -> int`, so a comparator like `string.compare`
can be passed directly (`names.sort(string.compare)`). Previously only a bool
predicate worked and the docs wrongly described a -1/0/1 comparator.
`xs.sortBy(selector)` takes an optional `descending` flag, and a new
`xs.binarySearchBy(selector, key)` searches a list sorted by a key. Builtin
type statics (`string.compare`, `string.fromCodePoint`, `bytes.fromString`,
...) are now first-class values, so they can be passed straight to sort, map,
and other higher-order methods without a wrapper. The collections guide now
documents Python-style slicing, including the step form (`xs[::2]`) and
reverse (`xs[::-1]`).

### Escape sequences are decoded inside interpolated strings

Escape sequences in a double-quoted string that also contains `${...}`
interpolation are now decoded, the same as in a non-interpolated string.
Previously `"line\n${x}"` emitted a literal backslash-n instead of a
newline, and `"\u{1F600} ${name}"` left the `\u{...}` escape undecoded.
Relatedly, an invalid `\u{...}` escape (empty, out of range, or a
surrogate) is now a clear compile-time error instead of producing a
malformed string. See the string-escape reference in the syntax guide,
which now documents `\u{HEX}` for Unicode code points.

### `geblang install pkg@latest` resolves to the highest semver tag

`geblang install <git-url>@latest` now queries the remote with
`git ls-remote --tags --refs`, picks the highest stable semver
tag (skipping pre-releases unless every tag is one), and clones
that tag. Bare-numeric tags like `1.2.3` are accepted alongside
the `vX.Y.Z` shape. Non-semver refs (`dev`, `release-1`, branch
names) are ignored during resolution. Re-running `install` with
`@latest` always re-resolves; pinned versions keep their existing
lock-skip behaviour. New dependency: `golang.org/x/mod` (BSD-3-
Clause); added to `NOTICES.md`.

## 1.7.1

### HTTP TLS: client verification control and HTTPS servers

HTTP clients verify TLS certificates against the system trust store by
default. A new `tls` option on `http.newClient` controls this: `verify`
(set `false` to skip verification), `caCerts` (PEM certificate(s) to
trust), `caCertsOnly` (trust only those, ignoring system roots), and
`clientCert` / `clientKey` (PEM, for mutual TLS). HTTP servers now serve
HTTPS when `http.serve` / `http.listen` are given a `tls` block: either
`{cert, key}` (PEM) or `{selfSigned: true}` to generate an in-memory
certificate for local development (with optional explicit SANs). The new
`http.serverCert(server)` returns the served certificate as PEM so a
client can trust a self-signed server precisely.

### Builtin type static methods no longer require an import

Static methods on a builtin type - `bytes.fromString(...)`,
`bytes.fromHex(...)`, `string.fromCodePoint(...)`, `string.compare(...)`,
and the like - now resolve without `import bytes;` / `import string;`,
matching how the rest of the toolchain already behaved. Previously the
tree-walking evaluator rejected these with `unknown method Type.X`
unless the type was imported first, while compiled programs accepted
them; the two backends now agree.

### Type-conversion methods for codepoints and byte lists

New methods round out converting between strings, codepoints, and byte
lists. `s.codePoints()` returns a string's Unicode code points as a
`list<int>` (the list form of `codePointAt`, and the inverse of
`string.fromCodePoints`). `b.toList()` returns a bytes value's byte
values as a `list<int>`, and `bytes.fromList(list<int>)` builds bytes
from byte values (0-255, rejecting out-of-range elements). Note that
`string.fromCodePoint` / `codePointAt` already serve as chr / ord.

### Web request handler runs app-level before-middleware ahead of routing

The built-in web request handler now runs every app-level
before-middleware once, against the original request, before route
matching. Previously the middleware ran inside the matching loop after
the path had already been read, so a middleware could not rewrite
`request["path"]` and have routes match the new value. Two consequences:
middleware that strips or rewrites the path (locale prefix, version
prefix, host rewrites) now influences which route matches, and
before-middleware fires even when no route ends up matching (404).
Path parameters are no longer present on the request dict at the time
middleware runs; reading parameters is a routing concern that belongs
in a route-bound layer.

### Top-level redeclaration is now consistently rejected

Declaring the same top-level name twice - an `import` and a `func`,
two `let`s, an `enum` and a `class`, an `interface` and a `func`, and
so on - is a compile-time error on both backends. The evaluator already
rejected these; the bytecode compiler used to silently let the later
declaration shadow the earlier binding, so a program could run under
one backend and misbehave under the other. Three cases stay allowed and
behave identically on both backends: function overloads (two `func`s
with the same name and different signatures), re-importing the same
module, and re-binding a name after `del`. Type aliases live in a
separate namespace and never collide with values. A name brought in by
`from M import X;` is immutable: it cannot be locally redeclared or
overloaded (import the module and use `M.X`, or alias with `as`). Use
`import X as Y;` when the local name is taken.

### `del` operates on variables only

`del` now applies only to variable bindings and the instances they hold
(whose destructor still fires). Deleting a class, function, enum, or
interface declaration is a compile-time error on both backends.
Previously the evaluator removed such a binding while the bytecode
backend handled it inconsistently, so the two could disagree. Re-binding
a variable after `del` (`del x; let x = ...;`) is unchanged.

### Subclass constructor across modules no longer crashes

A subclass whose name matches its parent's (`class X extends mod.X`)
and whose constructor explicitly forwards via `parent(...)` no longer
fails at construction with `no matching overload for X` on the
evaluator. The VM was already correct. The fix targets the overloaded-
function dispatch path: when an explicit `parent(...)` call resolves
to the parent's constructor, the auto-parent-chaining trigger now
checks the matched function's owner class before re-firing, so
dispatching the parent's same-named constructor no longer re-attempts
the chain with zero arguments.

## 1.7.0

### Runtime faults are catchable on both backends

Implicit runtime faults - division by zero, index out of range,
key-not-found, conversion failures like `"abc".toInt()`, and null access
- are now catchable with `try`/`catch` identically on the tree-walking
evaluator and the bytecode VM. Previously the bytecode VM (used by
`geblang run` / `geblang build`) let these escape `try`/`catch` and
terminate the program, while the evaluator caught them, so the same code
behaved differently between `geblang test` and a built binary.

### FatalError tier

A new `FatalError` class sits outside the `Error` hierarchy and is never
intercepted by `try`/`catch` - not even `catch (any e)`. It always
unwinds to the top and terminates. Raise one with
`throw FatalError("message")` for unrecoverable conditions. Exceeding the
maximum call depth (stack overflow) now surfaces as a `FatalError` on
both backends.

### time.monotonic

`time.monotonic()` returns monotonic milliseconds since process start
and never decreases. Use it for durations, timeouts, and TTLs:
`time.now()` / `time.unix()` read the wall clock, which can jump
backwards on clock correction.

### Shell completion

`geblang completion bash` prints a bash completion script. Enable it
for the current shell with `source <(geblang completion bash)`, or add
that line to `~/.bashrc` to make it permanent. It completes subcommands
at the first position (so `geblang li<tab>` becomes `geblang licenses`)
and filenames afterwards.

### Paged licenses output

`geblang licenses` now pages its output through `$PAGER` (falling back
to `less -R`, then `more`) when run in an interactive terminal. Output
is written plain when piped or redirected, so `geblang licenses > file`
and CI capture are unaffected. Pass `--no-pager` to force plain output
on a terminal.

### Exact decimal formatting in f-string specs

Numeric format specs (`:f`, `:e`, `:g`, `:%`) on a `decimal` value now
format from its exact value instead of routing through a binary `float`,
so `${d:.Nf}` matches `d.toString(N)` with no binary-rounding artifacts.
`float` values are unchanged.

### Dual-name modules (native + stdlib)

The import resolver now lets a native module and a Geblang stdlib
`.gb` module share the same canonical name. From outside the
stdlib wins; from inside, self-import returns the native module so
the wrapper can call its primitives. A missed export on a module
receiver falls back to the native registry, so users can reach
both surfaces through a single alias.

```gb
import async.sync as sync;
let m = sync.Mutex();      # stdlib OO wrapper class
let h = sync.mutexNew();   # native handle, via the same alias
```

Used by the new `async.sync` and `async.atomic` modules (below).
Existing dual-named pairs that previously had to use distinct names
(`strbuilder` + `strings.StringBuilder`, etc.) keep working unchanged.

### Channels (`async.channel`)

Typed message-passing between tasks. `Channel<T>(buffer = 0)`
creates a channel; `buffer = 0` is synchronous handoff, positive
buffer queues up to N values before sends block.

```gb
import async;
import async.channel as ch;

let c = ch.Channel<int>(0);
async.run(func(): void {
    for (let int i = 0; i < 5; i++) { c.send(i); }
    c.close();
});
for (var v in c) {
    io.println(v);
}
```

Methods: `send`, `recv`, `tryRecv`, `trySend`, `close`, `isClosed`.
`recv()` returns `null` once the channel is closed and drained, so
`for (x in channel)` iterates naturally to the end.

Send-after-close and double-close throw. Recv on a still-open empty
channel blocks; `tryRecv` returns `null` without blocking when
nothing is pending.

### `select` statement

`select` waits on multiple channel operations and runs the case
whose op fires first. New `select` keyword in the lexer.

```gb
select {
    case let v = c1.recv(): handleA(v);
    case c2.send(x): handleB();
    default: nothingReady();
}
```

Case heads are `c.recv()` (with or without a `let` binding) or
`c.send(value)`. `default` makes the select opportunistic; without
it the select blocks. When several cases are simultaneously ready
the chosen one is pseudo-random so producers and consumers cannot
starve each other through ordering. Backed by Go's `reflect.Select`.

### Synchronisation primitives (`async.sync`, `async.atomic`)

Two new sub-modules under `async` add the canonical concurrency
building blocks. `async.run` already spawns real goroutines, so
these primitives coordinate across them.

```gb
import async;
import async.sync as sync;
import async.atomic as atomic;

let counter = atomic.AtomicInt(0);
let wg = sync.WaitGroup();
for (let int i = 0; i < 100; i++) {
    wg.add(1);
    async.run(func(): void {
        counter.add(1);
        wg.done();
    });
}
wg.wait();
io.println(counter.load());   # 100
```

`async.sync` exposes `Mutex`, `RWMutex`, `Semaphore`, and
`WaitGroup`. Each constructor returns an instance whose methods
delegate to Go's `sync` package; `Mutex.tryLock`,
`RWMutex.tryLock` / `tryRLock`, and `Semaphore.tryAcquire`
provide non-blocking variants.

`async.atomic` exposes lock-free `AtomicInt` (int64) and
`AtomicBool`. Operations are sequentially consistent;
`compareAndSwap(old, new)` returns whether the swap happened.

## 1.6.0

### geblang check: clearer error-versus-warning contract

`geblang check` now follows one contract: an error is code both execution
backends reject, and a warning is advisory and never changes whether code
runs. Code that the tree-walking evaluator runs but the bytecode VM cannot
build yet is reported as a `vm-unsupported` warning instead of an error, so
`geblang check` agrees with `geblang test` while still flagging what would
need `--disable-vm` for `geblang run` / `geblang build`.

### profiler available on the evaluator

The `profiler` module (`snapshot`, `delta`, `memory`, `cpu`, `peak`) now
works on the evaluator - and therefore in `geblang test` - in addition
to compiled runs, so profiling helpers behave identically on both
execution paths.

### List, set, and dict comprehensions

New Python-style comprehension syntax for building a list, set, or dict
from an iterable in one expression. Multiple `for` clauses nest;
multiple `if` filters chain as logical AND.

```gb
let evens   = [x for x in xs if x % 2 == 0];
let squares = {x * x for x in xs};
let byId    = {u.id: u for u in users};
let pairs   = [a + ":" + b for a in xs for b in ys if a != b];
```

The binder accepts the same forms as the `for-in` loop: untyped,
typed (`for int x in xs`), or destructuring (`for k, v in d.items()`).

The lazy generator-comprehension form `(expr for x in xs)` is not
included in this release.

### Pipe operator `|>`

Elixir/F#-style pipe injects the left value as the first positional
argument of the right-hand call:

```gb
xs |> filter(positive) |> map(double) |> sum()
# = sum(map(filter(xs, positive), double))
```

The right-hand side can be a call (`x |> f(a)` -> `f(x, a)`), a bare
identifier (`x |> f` -> `f(x)`), or a selector (`x |> mod.fn(a)` ->
`mod.fn(x, a)`). The operator is left-associative and binds at very
low precedence so each side absorbs full expressions.

### Spread in list / dict / set literals

`...source` is now a valid entry inside a list, dict, or set literal,
splicing the source's elements into the new collection.

```gb
[0, ...xs, 4]                       # list spread
{...defaults, "port": 443}          # dict spread - last-write-wins on key collision
{0, ...someSet, 4}                  # set spread - sources can be set or list
{...a, ...b}                        # all-spread literals default to dict merge
```

List spread requires a list source; dict spread requires a dict source;
set spread accepts a set or a list. A literal whose entries are all
spreads is treated as a dict by default; force a set form by including
at least one bare element.

### Or-patterns in `match`

`case A | B | C => ...` matches when any alternate matches. Alternates
are bindless and cover three pattern kinds: literals (`case 1 | 2`),
bare types using Geblang's existing union-type syntax (`case int |
float`), and enum variants without payload (`case Color.Red | Color.Blue`).

```gb
match (v) {
    case int | float | decimal => "numeric";
    case 1 | 2 | 3             => "low";
    case Color.Red | Color.Blue => "warm";
    default                     => "other";
}
```

Guards apply to the whole or-pattern. Bindings inside alternates are
not supported in this release.

This change also fixes a pre-existing bug where union-typed
`case T | U =>` patterns matched only the first arm; the dispatcher
now consults the full type string on both backends.

### f-string format specifiers

String interpolation now accepts a Python-style format spec after the
expression: `${expr:spec}`. The spec follows
`[[fill]align][sign][#][0][width][,][.precision][type]` with type
characters `d`, `x`, `X`, `o`, `b`, `f`, `e`, `g`, `s`, and `%`.

```gb
"${pi:.2f}"         // 3.14
"${1234567:,}"      // 1,234,567
"${42:>5}"          // "   42"  (right-align width 5)
"${42:05}"          // 00042    (zero-pad)
"${255:#x}"         // 0xff
"${0.125:.2%}"      // 12.50%
"${name:.3}"        // first 3 chars
```

The `f` / `e` / `g` types operate on `decimal` as well as `float`,
matching Geblang's default-decimal numeric convention. Width and
alignment also apply to strings. Plain `${expr}` (no `:`) behaves
exactly as before.

### Math constants

Twelve new zero-arg constant functions on the `math` module, matching
the existing `math.pi()` / `math.e()` shape:

| Constant | Value |
|----------|-------|
| `math.tau()` | `2 * pi` |
| `math.ln2()` | natural log of 2 |
| `math.ln10()` | natural log of 10 |
| `math.sqrt2()` | square root of 2 |
| `math.phi()` | golden ratio |
| `math.sqrt2Pi()` | sqrt(2 * pi) |
| `math.log2Pi()` | log(2 * pi) |
| `math.maxInt()` / `math.minInt()` | int64 limits |
| `math.maxFloat()` / `math.minFloat()` | float64 limits |
| `math.epsilon()` | smallest float `eps` such that `1 + eps != 1` |

### Bug fix: try/catch across stdlib module boundary (VM)

Fixed a VM-mode regression where exceptions thrown from inside a
class method defined in an imported stdlib module were not caught
by a `try / catch` in the calling module. The dispatcher's
foreign-class native-trampoline branch was wrapping the inner
error with `runtimeError`, which collapsed the typed-throw chain
to a plain string before the calling VM could propagate it to its
exception-handler stack. The evaluator path was always correct;
behaviour is now consistent across both backends.

```gb
import option;
try {
    option.Option(false, 0).unwrap();
} catch (ValueError e) {
    # now catchable on the VM, as on the evaluator
}
```

### Bug fix: iterator dispatch across stdlib module boundary (VM)

Fixed a sibling VM-mode regression for the iterator protocol:
`for (x in instance)` failed with `<Class> is not an iterator`
when the instance's class was defined in an imported stdlib
module. The user-iterator dispatcher looked the class up via the
running chunk's local class table, which doesn't contain
foreign-module classes. Fix routes the `__done` / `__next`
presence check through the trampoline table the module loader
populates at import time, and threads any thrown errors back to
the calling VM's `pendingThrow` via the same propagation path the
catch fix uses.

```gb
import deque;
let d = deque.Deque<int>();
d.pushBack(1); d.pushBack(2);
for (var x in d) {
    # now iterates on the VM, as on the evaluator
}
```

### `assert` builtin

New top-level `assert(cond)` / `assert(cond, message)` builtin and a
companion `AssertionError` class (direct subclass of `Error`). When
`cond` is false the call throws `AssertionError`; otherwise it is a
no-op. With no explicit message, the error includes the source text
of the condition expression so failures are self-describing:

```gb
assert(balance >= amount, "insufficient funds");
assert(1 == 2);
# AssertionError: assertion failed: (1 == 2)
```

Both `geblang <script>` and `geblang build` accept a `--no-assert`
flag that elides every `assert(...)` call at compile time. Neither
the condition nor the message is evaluated when the flag is set, so
the call is truly zero-cost (caveat: side effects inside assert
arguments are lost). `geblang test` always runs assertions.

The LSP catalog also surfaces signatures and hover docs for
`assert`, `typeof`, `range`, `dump`, and `dir`, which until now
were callable but invisible to the IDE.

### Cron expression parser (`cron`)

New native `cron` module: parses standard 5-field cron specs
(plus `@hourly / @daily / @weekly / @monthly / @yearly /
@annually / @midnight` shortcuts) and computes their next
firings. Hand-rolled, no Go dependency.

```gb
import cron;
import time;

if (cron.isValid(spec)) {
    let next = cron.nextAfter(spec, time.unix());
}

let preview = cron.nextN("0 9 * * 1-5", time.unix(), 5);
```

Surface: `parse` (returns a normalised dict with field arrays),
`isValid` (cheap bool), `nextAfter` (next firing strictly after a
unix-seconds time), `nextN` (next N firings). Standard Vixie
semantics: when both day-of-month and day-of-week are restricted,
they are OR'd. `@reboot` is intentionally rejected (it has no
scheduled firing). Field names (`jan-dec`, `sun-sat`) are accepted
case-insensitively.

### IP / CIDR utilities (`net`)

The `net` module gains pure helpers for IP addresses and CIDR
ranges. Useful for allow-lists, deny-lists, classification, and
binary protocols. Backed by Go's `net/netip`.

```gb
import net;

io.println(net.cidrContains("10.0.0.0/8", "10.5.5.5"));   # true
let c = net.parseCidr("192.168.1.0/24");
io.println(c["first"]);   # 192.168.1.0
io.println(c["last"]);    # 192.168.1.255
io.println(c["count"]);   # 256
```

Surface: `parseIp`, `parseCidr` (returning a dict with `network`,
`prefixLen`, `version`, `first`, `last`, `count`), `cidrContains`,
`cidrRange`, `isIpv4`, `isIpv6` (never throw), `ipToBytes`,
`ipFromBytes`. IPv6 CIDR counts lift to bigint automatically.

### Unicode normalisation (`unicode`)

New native `unicode` module exposing the four Unicode
normalisation forms via `unicode.normalize(s, form)` and a cheap
`unicode.isNormalized(s, form)` check. `form` is the canonical
`"NFC" / "NFD" / "NFKC" / "NFKD"`.

```gb
import unicode;

let canonical = unicode.normalize(userInput, "NFC");
if (!unicode.isNormalized(stored, "NFC")) {
    log.warn("stored value is not NFC-normalised");
}
```

Backed by `golang.org/x/text/unicode/norm`. NFC composes, NFD
decomposes, NFKC / NFKD additionally fold compatibility
equivalents (ligatures, full-width, superscripts).

### MessagePack codec (`msgpack`)

New native `msgpack` module with `encode`, `decode`, `tryDecode`,
and `validate`. Hand-rolled implementation - no Go dependency -
covering the MessagePack 5 common cases: nil, bool, signed
integers (int family), float64, str family, bin family, array
family, and map family.

```gb
import msgpack;

let bytes = msgpack.encode({"items": [1, 2, 3], "ok": true});
let value = msgpack.decode(bytes);
```

Type mapping is one-to-one for primitives and containers. `bytes`
round-trip via the bin family; `decimal` round-trips as a
MessagePack string (lossless, portable). Ext types and the
timestamp extension are not supported in 1.6.0; integers outside
int64 range raise on encode.

### `lrucache.LruCache<K, V>`

New stdlib LRU cache with O(1) get / put / evict and optional
time-to-live. Backed by a doubly-linked list (for ordering) plus
a dict (for lookup); pure Geblang.

```gb
import lrucache;

let c = lrucache.LruCache<string, int>(100);
c.put("a", 1); c.put("b", 2);
io.println(c.get("a"));   # 1 - now most recent

let withTtl = lrucache.LruCache<string, int>(100, 60);   # 60s expiry
```

`get(key)` returns `null` on a miss (or on a hit whose entry has
expired). Pair with `has(key)` when you need to distinguish a
stored-null value from an absent key. Operations: `get`, `put`,
`delete`, `has`, `length`, `capacity`, `isEmpty`, `clear`,
`keys`, `values`, `stats`. `stats()` returns lifetime
`{hits, misses, evictions, expirations}` counters useful for
tuning capacity.

Expiry is lazy: an expired entry is dropped on the next `get` or
`has`, no background scan. Capacity must be at least 1.

### `deque.Deque<T>`

New stdlib double-ended queue with amortised O(1) push / pop at
both ends. Backed by a ring buffer that doubles in capacity when
full.

```gb
import deque;

let d = deque.Deque<int>();
d.pushBack(1); d.pushBack(2); d.pushBack(3);
d.pushFront(0);
io.println(d.popFront());   # 0
io.println(d.popBack());    # 3
```

Operations: `pushFront`, `pushBack`, `popFront`, `popBack`,
`peekFront`, `peekBack`, `get(i)` (O(1) random access; negative
counts from the back), `length`, `isEmpty`, `clear`, `toList`.
Implements the iterator protocol so `for (x in d)` walks
front-to-back. `popFront` / `popBack` / `peekFront` /
`peekBack` / `get` throw `ValueError` on out-of-range access.

### `priorityq.PriorityQueue<T>`

New stdlib priority queue (binary min-heap). Without a comparator,
elements are ordered by Geblang's `<` operator (works for `int`,
`float`, `decimal`, `string`); a `func(T, T): int` comparator
covers custom types or reverse order.

```gb
import priorityq;

let q = priorityq.PriorityQueue<int>();
q.push(3); q.push(1); q.push(2);
q.pop();   # 1

let byPriority = priorityq.PriorityQueue<Job>(
    func(Job a, Job b): int { return a.priority - b.priority; }
);
```

Operations: `push`, `pop`, `peek`, `length`, `isEmpty`,
`pushPop` (atomic push-then-pop, useful for top-K), `drain`
(returns the remaining elements as a sorted list), and `clear`.
`pop()` and `peek()` throw `ValueError` on an empty queue.

### Provably-fair RNG (`secureRandom`)

New `secureRandom` stdlib module for auditable random outcomes
(gaming, lotteries, public draws, anywhere "did the operator
cheat?" matters). It implements a commit / reveal scheme: the
server publishes the SHA-256 commitment of a freshly generated
32-byte seed, draws values from an HMAC-SHA-256 stream keyed by
that seed and the caller's `clientSeed`, then reveals the seed so
any third party can re-derive every draw and verify the
commitment.

```gb
let s = secureRandom.openSession({"clientSeed": "player#42"});
publish(secureRandom.commitment(s));

let roll = secureRandom.uintRange(s, 1, 7);

let seed = secureRandom.reveal(s);
audit(secureRandom.auditLogJson(s));
```

Draw helpers: `bytes`, `uintRange`, `float`, `bool`, `choice`,
`shuffle`, `weightedChoice`. Verification helpers:
`verifyCommitment` and `replay` (reproduces a single draw
outside any session). `uintRange` uses rejection sampling so the
distribution is unbiased even for ranges that are not powers of
two. After `reveal` the session refuses further draws.

For plain unpredictable randomness (session IDs, OTPs, salts),
keep using `secrets.*`. `secureRandom` is for the narrower case
where the audit trail matters.

### Numeric precision methods

`decimal` and `float` gain value-keeping rounding methods that return
the same type, unlike `math.floor/round/ceil` which return `int`. Each
takes an optional number of decimal places (default 0); `round` rounds
half away from zero.

```gb
io.println((2.567).round(2));      # 2.57
io.println((2.5).round());         # 3
io.println((2.9).floor());         # 2
io.println((2.999).truncate(2));   # 2.99
io.println((3.14159f).round(2));   # 3.14
```

`toDecimal` now accepts an optional precision, converting and rounding
to that many places in one step:

```gb
decimal pi4 = math.pi().toDecimal(4);   # 3.1416
```

New numeric helpers: `sign()` returns -1, 0, or 1; `clamp(lo, hi)`
constrains a number to a range and returns the receiver's type; `isEven()`
and `isOdd()` test parity of an `int`.

```gb
io.println((-7).sign());        # -1
io.println((12).clamp(0, 10));  # 10
io.println((4).isEven());       # true
```

The conversion methods (`toInt`, `toDecimal`, `toFloat`, `toBool`) work
on every primitive; `value as type` remains the idiomatic cast, with the
methods offering chaining and finer control.

### Cross-module symbol checking in geblang check

`geblang check` now resolves `module.member` and `from module import
name` against the actual exported surface of each module, for both
built-in modules and your own modules across a multi-file project. An
unknown member is reported as an error, so typos and outdated API calls
are caught statically:

```sh
$ geblang check app.gb
app.gb:2:4: error[import]: io has no exported member foobar
```

Checks resolve relative to each file and respect local scope, so a local
variable that shadows a module name is not mistaken for the module. The
same resolution backs the editor language server.

It also flags a method call on a typed instance whose class - across its
parent chain and implemented interfaces, including classes imported from
other modules - has no such method:

```sh
$ geblang check app.gb
app.gb:6:3: error[semantic]: Circle has no method bogus
```

The method check is conservative: it stays silent when the receiver's
type is not a statically known class, when the class or an ancestor
defines `__call`, when decorators may inject members, or when any part
of the hierarchy cannot be resolved.

Typos on built-in type methods (e.g. `"x".fooBar()`, `(42).nope()`) are
flagged too, checked against the authoritative per-type method set, and
a call to an undefined function (not a function, imported name,
constructor, variable, or built-in) is reported as well.

### dir() reports the correct method set

`dir(value)` previously listed several string methods that do not exist
(`trimLeft`, `padLeft`, `codeAt`) and omitted many real ones. It now
reports the accurate, complete method set for each built-in type
(identical on both backends), including methods such as `string.count`
/ `slice` / `reverse`, the `list` collection helpers (`groupBy`,
`chunk`, `zip`, `partition`, `topK`, ...), and the `dict` graph helpers
(`bfs`, `dfs`, `shortestPath`, `topologicalSort`).

## 1.5.4

### Bytecode VM: fused mod-zero branch

`if (local % const_int == 0)` and `if (local % const_int != 0)` now
compile to a single `OpJumpIfModNotZero` / `OpJumpIfModZero`
superinstruction on the VM. The opcode reads the int local
directly, computes the modulo against the constant divisor, and
branches in one dispatch, replacing the previous five-opcode
GetLocal+Const+ModInt+Const+JumpIfX sequence. The fast path
preserves Geblang's modulo semantics (negative-operand
correction, zero-divisor error). On the `numeric_loop` benchmark
this drops VM time by ~23% (95ms -> 73ms median).

### json.stringify: skip the sort when keys are already ordered

`json.stringify(dict)` now iterates the dict's insertion-order
record (`Dict.Order`) when valid and tracks whether successive
keys are non-decreasing. When the dict's keys are already in
alphabetical order (the common case for parsed JSON being
re-stringified), the encoder skips the per-dict sort entirely.
Output ordering is unchanged: dicts built in non-alphabetical
insertion order still produce alphabetical output via the
fallback sort path.

### json.parse: pre-sized Dict allocation

The parser now allocates each Dict with a capacity hint, avoiding
1-3 map and slice grow cycles per dict. Combined with the
stringify fast path the `json_roundtrip` benchmark drops by ~12%
(599ms -> 526ms median).

### JSON encoder: zero-alloc direct dict encode + int formatting

`json.stringify` now writes dict entries directly while iterating
the dict's insertion-order record on the alphabetical fast path,
skipping the pooled `pairs` scratch slice and the sort entirely.
Integer formatting also uses `strconv.AppendInt` against a stack
scratch buffer rather than `strconv.FormatInt`, eliminating the
per-int string allocation and the corresponding GC pressure.

### JSON parser: cached small-int interface wrappers

The parser now returns pre-boxed `runtime.Value` wrappers for
integers in `[-128, 1024)` from a process-wide cache, skipping
the per-call interface allocation that Go performs when wrapping
a struct-typed `SmallInt`. Cached values share identity but
compare and behave identically to freshly boxed ints. Combined
with the encoder changes, `json_roundtrip` drops by a further
~8% (526ms -> 498ms median).

### Node.js added to the benchmark suite

`benchmarks/run.sh` now compares Geblang against Node.js alongside
CPython and PHP. Each of the nine benchmark workloads has a
`benchmarks/node/<case>.js` variant matching the existing
Python/PHP semantics. Host mode picks up `node` from PATH; Docker
mode pulls `node:22-alpine` by default, overridable via
`BENCH_NODE_IMAGE`. When a runtime is missing on the host the
corresponding rows are reported as `skipped`.

## 1.5.3

### New copy-and-return list methods

- `list.reverse()` returns a new list with elements in reverse order.
  `list.reversed()` is the alias.
- `list.prepend(value)` returns a new list with `value` at the front.
  `list.unshift(value)` is the alias.
- `list.remove(value)` returns a new list with the first occurrence of
  `value` removed; returns an equivalent list if `value` is absent.

All three follow the existing copy-and-return convention used by
`push`, `pop`, `sort`, `sorted` and friends: a new list is allocated
and the receiver is unchanged.

### Dict alias methods

- `dict.entries()` is an alias for `dict.items()`.
- `dict.insert(key, value)` is an alias for `dict.set(key, value)`.
- `dict.remove(key)` is an alias for `dict.delete(key)`.

### `dir()` introspection fixes

- `dir(setValue)` now returns the set's methods instead of an empty
  list.
- `dir(dictValue)` now returns the dict's methods instead of the dict's
  keys. The previous behaviour conflated data with surface.
- `dir(listValue)` now returns the full list-method surface, not the
  stale four-entry subset.
- `dir(rangeValue)`, `dir(stringValue)`, and `dir(bytesValue)` now use
  the canonical primitive-method tables, picking up methods added in
  previous releases.

### Collections documentation

The collections reference (`docs/user/stdlib/08-collections.md`) gains
explicit coverage of the comparator shape for `sort`/`sorted`, the
canonical reverse-sort idiom, and tables for the new list and dict
aliases.

### Semantic check rejects unknown lower-case type names

A typed declaration whose type name is fully lower-case and is neither
a built-in (`string`, `int`, ...) nor a declared alias, class, or
interface now errors at semantic-analysis time. This catches the
common typo `aaa bbb;` where two bare identifiers parse as a typed
declaration with `aaa` as the type. Generic type parameters (`T`,
`U`, ...) and `PascalCase` user types are unaffected.

### REPL `del` and identifier lookup see prior-prompt bindings

The REPL now seeds each prompt's semantic analyzer with the names
already declared in the session. `del x;` (and any other identifier
reference) on a later prompt resolves to the binding from an earlier
prompt instead of failing with "unknown identifier".

### Dict spread tolerates extra keys

`foo(...dict)` now silently drops dict keys that do not name a
parameter of `foo`, so options-dict patterns can carry more entries
than the target function consumes. Required parameters that the
dict does not cover still error; explicit `foo(typo: 9)` still
errors so typos are caught. Overload resolution prefers the
overload that drops the fewest spread keys when more than one binds.

The named-arguments and spread reference in
`docs/user/05-functions-callables.md` was rewritten to cover
positional/named mixing, ordering rules, dict spread semantics,
and overload interaction.

## 1.5.2

### Lists are reference-typed; in-place growth methods landed

Lists now have full reference semantics: two variables bound to the
same list share its identity, and in-place mutations are visible
through every reference. This matches the semantics that index
assignment (`xs[0] = v`) already had and that other engines provide
for arrays / lists.

Three new in-place methods take advantage of this:

- `list.append(value)` adds a single value to the end. Amortised
  O(1) per call; building a list of n elements is O(n) total work
  rather than O(n^2) as it was when accumulating with `push`.
- `list.extend(other)` appends every element of another list.
- `list.clear()` empties the list.

All three return `null`, mutate the receiver, and propagate to every
alias. On a frozen list each one raises `ImmutableError`. When the
receiver still carries its declared element-type tag at runtime,
`append` and `extend` reject mismatched values with `TypeError`.

The previously copy-and-return methods (`push`, `pop`, `prepend`,
`unshift`, `insert`) continue to behave the same way: they allocate
a new list and leave the receiver unchanged. Reach for `append` when
you mean to grow the list; reach for `push` when you want a fresh
list back.

### `dict.clear()`

New in-place method that empties a dict. `dict.delete(key)` already
mutated in place; `clear` rounds out the surface. Both raise
`ImmutableError` on a frozen dict.

### `freeze.shallow` now freezes the receiver

Previously `freeze.shallow(xs)` for a list / dict / set returned a
frozen copy and left the original mutable. The behaviour now matches
the existing `*Instance` case and the documented "shares internal
data" promise: a single shared underlying value is marked frozen and
mutations through any reference are blocked.

If your code relied on the old behaviour to keep a mutable handle
alongside a frozen copy, build the copy explicitly: `let frozen =
freeze.shallow(xs.slice(0));`.

## 1.5.1

### Bytes

- `bytes.slice(start[, end])` cuts a fresh bytes value out of an
  existing one. Negative indices count from the end; out-of-range
  bounds clamp. The two-arg form is half-open `[start, end)`.

### `instanceof` over generic collections

- `list<any>` / `dict<K, any>` / `dict<any, V>` / `set<any>` are
  now universal-accept: every list / dict / set satisfies them,
  matching the documented "any accepts anything" rule.
- Union arguments (e.g. `list<string|int>`) match elementwise on
  untagged collections (each element must satisfy any arm) and
  satisfy the tagged-collection invariance check when the tag's
  type appears in the union (`list<int>` satisfies
  `list<int|string>`).
- Both fixes apply on the evaluator and bytecode VM in lockstep.

### Methods on `int` work on every int representation

Chained calls like `s.length().toString()` no longer fail on the
evaluator backend with `unknown method int.toString`. Every
documented `int` method (`toString`, `abs`, `isZero`,
`isPositive`, `isNegative`) now dispatches on both runtime
representations.

### Default arguments work in return position

A function declared with a default argument can now be called
without that argument from any call site, including `return` of
a function whose return type matches. The previous behaviour
required the caller to pass the explicit value (or bind the
result through `let` first) when the call appeared in return
position.

### Dict insertion order is preserved

- `dict.keys()`, `dict.values()`, `dict.items()`, `for ... in dict`,
  and string interpolation of a dict now return entries in the
  order they were inserted, deterministically. Updating an
  existing key keeps its original position; deleting and
  re-inserting moves the key to the end.
- `yaml.parse` and `json.parse` preserve the source mapping /
  object order.
- Inspect / string-interpolation output of a dict no longer sorts
  keys alphabetically. The new order is "what you wrote".

## 1.5.0

### Decorators

- `@abstract` class decorator: direct instantiation throws
  RuntimeError. Subclasses without `@abstract` instantiate normally.
- `@abstract` method decorator: a class that declares (or inherits)
  any abstract method without a concrete override is itself abstract.
  Error message names the unimplemented method.
- Class decorators that return a callable are now supported as the
  third runtime shape, alongside the existing register-in-place and
  swap shapes. The returned callable becomes the new constructor; the
  captured class value is marked raw so calling it from the closure
  builds the original without re-triggering the decorator chain.
- Typed delegation: a wrap closure may return an instance of a
  different class than the decorated one. The runtime stamps the
  instance so `instanceof` against the original class still returns
  true, even though the runtime class is the replacement. Useful when
  one declared type fronts an implementation chosen by a decorator
  at definition time.

### Field decorators run as write barriers

A field decorator whose name resolves to a callable in scope now
runs on every assignment to that field (including the constructor's
first write), transforms the incoming value, and the transformed
value is what gets stored. Decorators stack bottom-up, output of one
feeds the next. Names that don't resolve stay as pure metadata (the
existing framework-annotation contract).

```gb
func upper(string v): string { return v.upper(); }
func minLen(int min, string v): string {
    if (v.length() < min) { throw RuntimeError("too short"); }
    return v;
}

class User {
    @minLen(2) @upper
    string name;
    func User(string n) { this.name = n; }
}
```

### Interface default methods and properties

Interface bodies now accept three forms:

- abstract method signatures (`func foo(): T;`) - the prior surface
- default method bodies (`func foo(): T { ... }`) - implementing
  classes inherit the body when they don't override
- property declarations (`string name;`) - implementing classes gain
  the field automatically, no redeclaration needed

When two implemented interfaces both provide a default for the same
method signature and the class doesn't override, the compiler rejects
the class with an error naming both source interfaces. The rule fires
only on conflicting defaults; one default + one signature inherits
unambiguously.

### JSON-like container Inspect

`io.println(dict)`, `io.println(list)`, and `io.println(set)` now
produce JSON-like output (sorted dict keys, quoted strings inside
containers, depth guard for cycles). Top-level strings stay unquoted
so `io.println("x")` -> `x` is unchanged.

### Cross-module throws no longer swallowed

A method inherited from a parent class in another module could throw
silently: the bytecode VM's cross-module dispatch fallback treated any
loader error as "method not found" and dropped real `throw` errors on
the floor. The dispatcher now distinguishes the two and propagates a
throw to the calling VM's nearest `try / catch`.

### In-process FFI for C-ABI shared libraries

New `ffi` stdlib module loads shared libraries through dlopen and
calls into them with no IPC overhead. Sits alongside the existing
subprocess [`ext` protocol](stdlib/env-ext.html); use FFI for
hot numeric kernels and library bindings (libtorch, libsqlite,
libcurl, libopencv), `ext` for sandboxed or polyglot extensions.

- `ffi.dlopen(path)` returns a `Library` handle. `Library.symbol(
  name, [argTypes], retType)` returns a Geblang callable bound to
  the native function; invoking it dispatches into C through a
  per-signature trampoline cached on the library.
- Type table covers `INT8`-`INT64`, `UINT8`-`UINT64`, `FLOAT`,
  `DOUBLE`, `PTR`, `CSTRING`, `BYTES`, `VOID`. CSTRING marshals
  both directions; BYTES is zero-copy in.
- Memory helpers: `ffi.alloc`, `ffi.free`, `ffi.readBytes`,
  `ffi.writeBytes`, `ffi.readCString`, `ffi.cString`, `ffi.errno`.
- C struct layouts via `ffi.StructOf([[name, type], ...])`.
  `Struct.size` reports the byte size, `Struct.alloc()` allocates
  one instance, `Struct.get(ptr, name)` and
  `Struct.set(ptr, name, value)` read and write fields with
  standard C alignment.
- `ffi.callback(fn, argTypes, retType)` wraps a Geblang function
  in a C function pointer for libraries that drive their own loop
  (qsort comparators, libcurl multi-handle, audio callbacks).
  Signature types restricted to INT*, UINT*, PTR. Callbacks live
  for process lifetime.
- Typed arrays: `ffi.sizeOf(type)`, `ffi.writeArray(ptr, type,
  list)`, `ffi.readArray(ptr, type, length)` for passing
  homogeneous arrays of primitives by pointer + length. Element
  types: INT*, UINT*, FLOAT, DOUBLE, PTR.
- `ffi.bytesView(ptr, length)` is the zero-copy view counterpart
  to `ffi.readBytes`. The returned bytes value aliases the C
  memory; callers guarantee the buffer outlives every use.
- `geblang bind <manifest.yaml>` generates a Geblang module
  wrapping a C-ABI shared library from a declarative manifest
  (library + constants + structs + function signatures). The
  output is a normal Geblang module; `import` it and call
  exported functions like any other code. Sugar over the raw
  `lib.symbol(...)` form, useful for libraries with more than a
  handful of functions.
- Capability-gated, default-off. Projects opt in through a
  `permissions.ffi` block in `geblang.yaml`; standalone scripts
  opt in via repeated `--allow-ffi <path-or-glob>` CLI flags
  (also accepted by `geblang test`). `PermissionError` is a new
  built-in error class, catchable from Geblang.
- Recommended pattern: wrap C handles in a Geblang class with
  `__enter` / `__exit` and lifecycle them with `with` blocks so
  the release call fires automatically at scope exit.
- `geblang doctor` reports the active FFI policy and allow-list
  rules. LSP catalog covers the `ffi` module surface; VS Code
  ships `ffidlopen`, `ffisymbol`, and `ffihandle` snippets.

Dispatch backs onto `purego` (pure-Go reimplementation of
dlopen + dispatch); no cgo, no extra build dependency. Supported
platforms: Linux/macOS/Windows on x86_64 and arm64.

See [Foreign Function Interface](stdlib/ffi.html) for the full
reference. Real-library acceptance tests cover libm (sin, cos,
sqrt, hypot), libc (getpid, strlen, malloc/free, memcmp, errno),
and a complete SQLite open/exec/prepare/step/finalize/close
walkthrough.

### Test framework

- New `this.assertThrowsOf(callable, classOrName[, substring])`
  narrows the existing `assertThrows` contract to a specific
  exception class. `classOrName` accepts either a class value
  (for user-defined classes in scope as identifiers) or a class
  name as string (works for the built-in errors that aren't
  reified - `RuntimeError`, `TypeError`, `ValueError`, `IOError`,
  `ParseError`, `MatchError`, `ImmutableError`, `PermissionError`).
  The match walks the parent chain like a catch clause, so a
  subclass instance matches the parent class. Optional third
  argument is a substring that must appear in the error message.
  Failure messages name both expected and actual class.

### Language

- Dunder method names normalised to prefix-only: `__enter`,
  `__exit`, `__serialize`, `__deserialize` are now the canonical
  forms, matching the rest of the dunder surface (`__get`,
  `__set`, `__call`, `__eq`, `__read`, `__write`, `__close`,
  `__iter`, ...). The legacy prefix-and-suffix forms still work
  so existing tests and scripts keep running.
- Parameter-level metadata decorators: any name attached to a
  function or constructor parameter (`@SomeName(args)`) surfaces
  through `reflect.parameters(fn)` as a `decorators` key per
  parameter dict, mirroring the existing class- and method-
  decorator metadata. Pure metadata; the runtime never invokes
  them. Frameworks read the structure to drive dispatch.
- Bytecode VM identifier dispatch is now case-sensitive at the
  call site, matching the evaluator. A module that exports both
  a `view` function and a `View` class now resolves each call
  correctly; previously `view(args)` could bind to the class
  constructor and surface as "no matching overload for View" at
  runtime.

### Cross-module

- Interface default methods and property declarations
  (introduced earlier in 1.5.0) now propagate across module
  boundaries. A class can `implements donor.Greetable` and
  inherit a default `greet()` plus a declared `name` field.
- `reflect.fields` returns full type info for class references
  passed across modules; the receiving module's parity tests
  now see declared types and nullability rather than the
  collapsed `any` / non-nullable fallback.
- Two-hop class extension: `class Leaf extends middle.Middle`
  where `Middle extends donor.Base` resolves inherited methods
  through both module hops. Inherited `throw` calls propagate
  to the caller's `try / catch` across the same chain.

### Reflection

- `reflect.classes()` enumerates every class declared in the
  current program (user classes plus imports). Useful for
  framework discovery passes that scan for `@OnMessage`, `@Job`,
  `@Scheduled`, or other class-level decorators without
  forcing the user to register handlers explicitly.

### Stdlib

- `math.isPrime(n)` tests primality on arbitrary-precision
  integers. Backed by Baillie-PSW plus Miller-Rabin (20
  rounds), so deterministic for inputs that fit in an `int64`
  and effectively certain for larger values. Returns `false`
  for `n < 2` including negatives.

### Errors

- "Unknown method" now raises a catchable `RuntimeError`
  carrying the receiver class and missing method name; a
  `try / catch (RuntimeError e)` block reaches it on both
  backends. Previously the bytecode VM dropped the message on
  the floor for cross-module dispatch.

### Engine parity

- Evaluator widens `SmallInt` to `decimal` and `float` on `as`
  casts. Method results like `list.length()` produce the
  compact `SmallInt`; the bytecode VM handled the widening
  but the evaluator only matched the big-integer variant,
  surfacing as "cannot cast int to decimal" in `geblang test`
  runs where the same code worked under `geblang run`.

### Stdlib (messaging)

- AWS SNS backend for `messaging.topic({"driver": "sns", ...})`.
  `publish()` signs each request with sigv4 and POSTs to the
  regional SNS endpoint; `subscribe(handler)` polls a paired SQS
  queue and forwards notifications to the callback. Joins the
  existing `rabbitmq` / `stomp` / `kafka` pub/sub drivers.

### Stdlib (LLM)

- New `llm` module: a provider-agnostic client for chat
  completions, text embeddings, image analysis, and image
  generation. Pick the backend with `llm.client({"provider":
  "openai" | "anthropic" | "bedrock", ...})`; the rest of the
  calling code is the same across providers. OpenAI covers all
  four operations; Anthropic covers chat + image analysis;
  Bedrock covers chat + image analysis through the Anthropic
  Messages schema, embeddings via `amazon.titan-embed-*` and
  `cohere.embed-*` model families, and image generation via
  `amazon.titan-image-*` and `stability.*` model families.
  Calls with an unsupported operation / unrecognised model
  family raise a `RuntimeError` naming the missing method and,
  for Bedrock, pointing at the lower-level `invoke(model,
  payload)` escape hatch.

### Other

- Thread-safe WebSocket writes: concurrent sends from multiple
  Geblang tasks on the same `WebSocket` value no longer race
  the underlying TLS / TCP write path.

## 1.4.5

### Engine

- Bytecode VM now supports `class Sub extends mod.Parent` patterns
  where the parent class lives in another `.gb` module. Both
  `parent(args)` constructor calls and `parent.method(args)`
  dispatch through the parent module's chunk. Method lookup on
  the subclass instance also walks across the module boundary so
  inherited methods like `subInstance.parentMethod()` work
  natively under the VM (the evaluator already supported this).
  Removes the long-standing requirement to run apps that import
  libraries with subclassable base classes via `--disable-vm`.

## 1.4.4

### Stdlib

- `crypt.md5` / `sha1` / `sha256` / `sha512` / `sha3_256` /
  `blake2b` / `crc32` now accept either `string` or `bytes` input
  (previously string-only). `crypt.hmacSha256` and
  `hmacSha256Bytes` accept string or bytes for both the key and
  the message. Existing string callers are unchanged.

## 1.4.3

### Time

- New `time.unix()` / `time.unixMilli()` / `time.unixMicro()` /
  `time.unixNano()` / `time.unixFloat()` / `time.unixDecimal()` for
  PHP / Python-style unix-time access. `time.unix()` is whole
  seconds (PHP `time()`); `time.unixFloat()` is fractional seconds
  (PHP `microtime(true)` / Python `time.time()`); `time.unixDecimal()`
  is lossless nanosecond-precision seconds as a `decimal`.
- `time.elapsedFloat(start)` is the float-seconds analogue of
  `time.elapsed`.
- `time.now()` keeps returning milliseconds; nothing in
  `async.sleep` / scheduler / `timeoutMs` semantics changes.

### Networking

- `http.listen`, `http.serve`, and `net.serve` accept an optional
  `opts` dict with `maxConcurrent`, `queueSize`, and `onOverload`
  ("reject" / "wait" / "drop") for bounded concurrency and
  backpressure. Defaults are unchanged - no opts means unbounded.
  WebSocket connections share the parent HTTP server's cap, so a
  `maxConcurrent: 1000` listen becomes a hard cap on simultaneous
  WebSocket clients.
- `http.serverStats(server)` and `net.serverStats(handle)` return
  `{active, queued, rejected, maxConcurrent}` so callers can wire
  pool counters into metrics or alerts.

## 1.4.2

### Language

- Selective imports: `from X import Y;`, `from X import Y, Z;`,
  `from X import Y as Z;`. Binds the named symbols into the current
  scope without the module namespace prefix. The source module itself
  is not bound by the from-import - pair with `import X;` when you
  need both. `from` is a soft keyword so existing identifiers named
  `from` (function parameters, class fields) still parse.

### Bug fixes

- REPL: left / right arrows now follow the line correctly when the
  input wraps to a second terminal row, and Home / End jump to the
  start / end of the logical line instead of the start / end of the
  current physical row. Backspace, Delete, and history navigation
  also reposition the cursor properly across wrapped rows.
- REPL: pressing Enter after navigating away from the end of a
  wrapped line now puts evaluator output on a clean new line below
  the entire input, instead of overwriting the trailing wrapped row.
  Tab-completion candidate listings and the `^C` clear-line message
  walk past wrapped rows the same way.

## 1.4.1

### Stdlib

- `int.toString(base)` and `string.toInt(base)` accept any base
  2-36 for arbitrary base conversion (lowercase digits a-z).
- `encoding.base64UrlEncode` / `base64UrlDecode` for unpadded
  URL-safe Base64 (RFC 4648 section 5); decoder accepts padded
  or unpadded input.
- `bytes.toBase64Url` / `bytes.fromBase64Url` module helpers and
  a `b.toBase64Url()` method on bytes values.
- `crypt.passwordHash(pw, opts?)` and `crypt.passwordVerify(pw, hash)`
  produce and verify hashes interchangeably with PHP's
  `password_hash` / `password_verify`. Output uses the `$2y$`
  prefix for bcrypt (PHP default) or PHC format for argon2id /
  argon2i. Verify auto-detects the algorithm from the hash
  prefix and accepts `$2a$`, `$2b$`, `$2y$`, `$argon2id$`, and
  `$argon2i$` hashes from any compatible source.
- New `binary` module with Python `struct`-style pack/unpack:
  `binary.pack(format, ...values)`, `binary.unpack(format, data)`,
  `binary.unpackNamed(spec, data)`, and `binary.size(format)`.
  Format codes cover signed/unsigned 8/16/32/64-bit ints, 32/64-bit
  floats, fixed-length byte strings, and pad bytes; the first
  character may set endianness (`<` little, `>` big, `!` network,
  `=` native).

### Tooling

- LSP diagnostics now cover unresolved imports, bytecode type errors,
  unused imports, and cross-module symbol checks (`foo.bar()` is
  flagged when `bar` isn't exported by `foo`). Both `geblang check`
  and the in-editor squiggles go through the same shared pipeline.
- New LSP capabilities: `textDocument/codeAction` quick-fix for
  unresolved imports (suggests nearest-match replacements);
  `textDocument/references` and `textDocument/rename` for the
  identifier under the cursor (single-file scope);
  `workspace/symbol` search across every `.gb` file in the open roots.
- VS Code extension gains a `Geblang Language Server` output channel
  and a status-bar item showing the LSP state (click to focus the
  channel). `editor.formatOnSave` works as expected; no extension
  setting needed.

## 1.4.0

### Performance

- Tight integer loops are noticeably faster: `BenchmarkIntLoop`
  3.7 ms to 2.74 ms, `BenchmarkIntArithmetic` 6.2 ms to 5.2 ms.
- `recursive_fib` scoreboard 86 ms to 67 ms.
- `list_functional` scoreboard 14 ms to 12 ms (matches PHP).
- Recursive call paths drop ~20 allocations per call from the
  new function-frame layout; long-running recursive workloads
  see lower GC pressure.

### Language

- List-shape patterns in `match`:
  `case [int x, int y] if x > y => ...`. Each binding may be
  typed (must match) or untyped (any value); `_` is a wildcard
  that skips binding. Length and type mismatches both fall
  through to the next case.
- Union types (`T | U`) at parameter and return positions:
  `func get(int | string id): User | NotFoundError`. The runtime
  enforces "any branch matches" and throws a catchable
  `RuntimeError` on mismatch (parameter-validation errors now
  go through the standard throw path on the VM, matching the
  evaluator). Intersection (`T & U`) supported with "every
  branch matches" semantics.
- Structured concurrency via the `async.scope` module:
  `async.scope.TaskGroup` with `.spawn(fn)` / `.cancel()` and
  the `async.scope.scope(body)` runner. The body receives the
  group; spawned children are awaited at scope exit; if the
  body or any child throws, remaining children are cancelled
  and the first error is rethrown after the drain completes.

### Stdlib

- New `messaging` module with a unified queue + topic facade and
  pluggable backends. Backends: AWS SQS over HTTPS with sigv4,
  RabbitMQ over AMQP 0.9.1, STOMP 1.2 (covers ActiveMQ natively
  and RabbitMQ via the STOMP plugin), and Kafka.
  `messaging.connect({driver, ...})` returns a queue handle with
  `publish` / `receive` / `ack` / `consume` / `close`.
  `messaging.topic({driver, ...})` returns a pub/sub handle with
  `publish` / `subscribe` / `close`; RabbitMQ uses a fanout
  exchange, STOMP uses `/topic/` destinations, Kafka uses a fresh
  consumer group per subscriber. SQS is queue-only on this
  surface; use AWS SNS for fan-out and target SQS queues as SNS
  subscriptions. Lower-level `amqp` and `kafka` native modules
  are exposed for cases beyond the facade.
- New `archive` module with zip / tar / tar.gz readers and
  writers: `archive.zipRead` / `zipWrite` / `tarRead` /
  `tarWrite` / `tarGzRead` / `tarGzWrite`. Entries are dicts
  with `name`, `data` (bytes), `isDir`, and `size`; writers
  accept string or bytes payloads and sort tar entries by name
  for deterministic output.
- `crypt.jwtSign(payload, key, opts?)` and
  `crypt.jwtVerify(token, key, opts?)` now dispatch on the
  `alg` option (or the token header) and cover every supported
  algorithm in one pair: HS256 / HS384 / HS512, RS256 / RS384 /
  RS512, ES256 / ES384 / ES512, and EdDSA (Ed25519). Pass
  `opts.allowedAlgs` to defend against alg-confusion attacks.
  The default allow-list excludes `none` on both sign and
  verify; opt in by passing `"none"` inside `opts.allowedAlgs`
  when you genuinely need unsigned tokens.
  `crypt.jwtSignRS256` / `jwtVerifyRS256` / `jwtSignES256` /
  `jwtVerifyES256` remain as deprecated shims for 1.5.0 removal.
- New `crypt.jweEncrypt(payload, key, opts)` and
  `crypt.jweDecrypt(token, key)` for encrypted JWTs. Key-wrap
  algorithms: `dir` (32-byte CEK) and `RSA-OAEP-256` (wraps a
  fresh CEK with an RSA public key). Content encryption is
  `A256GCM`. Tampered tokens fail the AEAD authentication and
  throw.
- New `crypt.pkcs12Decode(pfx, password)` returning
  `{key, cert, caCerts}` where `key` is a PKCS#8 PEM and the
  certificate fields are CERTIFICATE PEM strings. Encoding to
  PFX is not in scope for 1.4.0.
- New `crypt.signCertificate(options)` signs a CSR with a CA
  certificate and key, returning the issued certificate PEM.
  Options: `csr`, `caCert`, `caKey` (all PEMs), `validDays`
  (default 365), `isCA` (default false), `dnsNames`,
  `ipAddresses`, `serialBits` (default 128). Completes the
  CSR-to-issued-cert pipeline (`generateCsr` to
  `signCertificate` to `parseCert`).
- `metrics.counter(name, opts)` / `gauge` / `histogram` declare
  typed metrics with optional labels; `metrics.observe(name,
  value, labels)` records histogram samples; `metrics.toPrometheus()`
  emits Prometheus v0.0.4 text exposition format. Legacy
  `metrics.inc` / `set` keep working unchanged.
- `log.toStream(stream)` writes JSON log lines to any
  `streams.IOStream` (memory buffer, TCP socket, pipe).
- `trace.toOtlpJson(opts?)` serialises recorded spans as OTLP/HTTP
  JSON; `trace.exportOtlp(endpoint, opts?)` POSTs them to a
  collector at `endpoint/v1/traces`. Child spans via
  `trace.start(name, attrs, {parent})`.

### Performance

- Faster tight loops that mix arithmetic with collection length /
  modulo: `regex_match` 62 ms to 45 ms, `numeric_loop` 123 ms to
  92 ms, `recursive_fib` 65 ms to 58 ms.
- `BenchmarkRecursiveFib` allocs/op 301 to 24.

### Tooling

- `make bench` now builds geblang via `make build` first and
  benches that binary, so source changes always reach the
  scoreboard.

## 1.3.0

### Stdlib

- New `pcre` module: PCRE-compatible regex engine for patterns
  that need lookahead, lookbehind, backreferences, atomic groups,
  or named captures via either `(?P<name>...)` or `(?<name>...)`.
  Surface mirrors `re.*` (test, find, findAll, match, matchAll,
  replace, split, quote) with an optional flags string (`imsx`).
  Coexists with the existing `re` module (RE2, linear-time, no
  catastrophic backtracking) - reach for `pcre` only when you
  need PCRE-only features.

### Language

- `test.Test.assertThrows(callable, expectedSubstring = "")` is
  now a built-in assertion on both the evaluator and the VM.
  Fails if the no-arg callable returns without raising; when the
  optional substring is given it must appear in the error
  message.
- `test.run(class, opts)` accepts a new `methods` option (a list
  of method names) so tooling can run a single test.
- `geblang test --class ClassName` and `--method methodName`
  flags filter to a single class or method within the discovered
  test files.
- `geblang test --format teamcity` emits `##teamcity[...]`
  service messages (with `locationHint='geblang_test://Class/
  method'`) so JetBrains IDE test runners parse events natively.
  Replaces the verbose PASS/FAIL output for IDE integration.
- `test.mock(moduleName, {fname: callable})` swaps stdlib
  functions for the duration of the current `@test` method;
  the runner snapshots patches before each method and restores
  them after, so mocks never leak across tests. Pair with
  `test.restore(module, fname)` / `test.restoreAll()` for mid-
  method toggling.
- New `crypt.hmacSha256Bytes(key, message): bytes` returns the
  raw HMAC output (instead of hex). Useful when the HMAC output
  is the next round's key - sigv4, HKDF, TLS PRF, etc. Verified
  against the AWS sigv4 reference vector.

### Fixes

- VM: string `<`, `<=`, `>`, `>=` now work the same way they did
  in the evaluator (lexicographic comparison). Previously the VM
  dispatched relational ops through `native.NumericCompare` which
  rejected strings with "comparison expects compatible numeric
  operands"; the evaluator's own `compareValues` already covered
  it. Both paths now share the extended `NumericCompare`. New
  parity test guards the behaviour.

### Tooling

- LSP: `this.<TAB>` inside a class extending `test.Test` now
  surfaces every inherited assertion (assertEquals, assertTrue,
  assertThrows, fail, etc.).
- LSP: `<typedVar>.<TAB>` and `<typedVar>.<method>(<cursor>)` on
  stdlib-class locals (http.Request, db.Connection, datetime.Instant,
  url.URL, streams.IOStream, proc.Process, sockets.Socket/Listener,
  ssh.SSHClient/SSHSession/SSHTunnel, strbuilder.StringBuilder,
  random.Generator, websocket.Connection, template.Template/Engine,
  log.Logger) surfaces methods with parameter and return types.
  Triggers when the local is declared via `<module>.<Class> name;`.
- VS Code: new `assertThrows` snippet under the Geblang language.

## 1.2.0

### Stdlib

- New `sockets` module: `sockets.dial(host, port, opts)` opens a
  TCP or TLS connection and returns a `Socket` wrapping the
  stream protocol. `sockets.serve(host, port, handler)` binds a
  listener and dispatches each accepted connection to the handler
  callback. `server.close()` joins the accept goroutine so reads
  of module-level state from the parent happen-after the last
  handler invocation. Sockets implement `read` / `readAll` /
  `readLine` / `lines` / `write` / `writeln` / `close` plus the
  dunder protocol for `streams.copy` and `for (line in sock)`.
- New `ssh` module: a Geblang-native SSH client.
  `ssh.connect(target, opts)` opens an authenticated connection
  (password / private key / passphrase / agent), with host-key
  verification via `knownHostsFile`. `client.exec(cmd)` runs a
  one-shot command returning `{stdout, stderr, exitCode}`.
  `client.spawn(cmd)` returns an `SSHSession` with
  `streams.IOStream`-shaped stdin / stdout / stderr (same shape
  as `proc.Process`), plus `wait()`, `kill()`, `signal(name)`.
  SFTP: `upload`, `download`, `sftpList`, `sftpRemove`,
  `sftpMkdir`, and `sftpOpen` (returns an IOStream for piping
  remote files through `streams.copy`). Port forwarding via
  `forwardLocal(port, target)` and `forwardRemote(port, target)`
  returns `SSHTunnel` handles that close cleanly.
- `http.post` / `http.request` / `http.requestWithOptions` accept
  a `streams.IOStream` (or any class wrapping one) for the
  request body, in addition to the existing string and bytes
  shorthand. Useful for multi-GB uploads that shouldn't load
  into memory.
- New `cli.widgets.Spinner` and `cli.widgets.ProgressBar` render
  ANSI control sequences to stderr (so stdout piping stays
  usable). The spinner has `tick()` / `update(msg)` / `stop()`;
  the bar has `advance(n)` / `set(value)` / `updateLabel(label)`
  / `finish()`.

### Bug fixes

- File / stream / socket close paths now suppress
  already-closed errors so user code that closes the same handle
  twice no longer surfaces the harmless errno (covers
  `os.ErrClosed`, `net.ErrClosed`, and the "use of closed
  network connection" string fallback).

## 1.1.0

### Stdlib

- New `streams.IOStream` class wraps a file or in-memory handle
  with `read`, `readAll`, `readLine`, `lines`, `write`, `writeln`,
  `flush`, `close`, and `for (line in stream)` iteration.
  Memory-backed instances also expose `toString()`.
- New factories: `streams.open(path, mode)`,
  `streams.memory(initial)`, and
  `streams.stdin / stdout / stderr()`.
- New helpers `streams.readAll(src)` and
  `streams.copy(src, dst)` consume any value implementing the
  stream protocol.
- New `proc` module starts child processes that stream
  concurrently with the parent: `proc.spawn(cmd, args, opts)`
  returns a `Process` with `stdin`, `stdout`, `stderr` (each an
  `IOStream`), plus `wait()`, `kill()`, `signal(name)`, and
  `pid`. `{pty: true}` attaches a pseudo-terminal so interactive
  programs see a TTY. The existing synchronous `process.run` /
  `sys.run` are unchanged.
- New `watch.start(path, callback, opts)` registers an fsnotify
  watcher and fires `callback({path, type})` for each filesystem
  event. `{recursive: true}` walks subdirectories at register
  time. `watch.stop(handle)` waits for the in-flight callback to
  finish before returning. Polling helpers (`watch.snapshot` /
  `watch.wait`) remain available.

### Language features

- Stream protocol dunders: classes implementing `__read(int)`,
  `__write(string)`, and `__close()` plug into `streams.copy`,
  `streams.readAll`, and `for (line in stream)` directly.
- Generator methods on user classes now run on the VM.

### Bug fixes

- `io.readLine` over an in-memory stream returned `null` after
  the first line; now reads each line in turn.
- Cross-goroutine timer and ticker callbacks no longer trip the
  race detector.
- Methods on a main-script class invoked from a stdlib module
  work without an `unknown class` error.
- `sys.sleep` and `process.signal` accept any int value.

### Performance

- Tight integer loops stay lock-free after the timer-race fix;
  `numeric_loop` 166 ms to 136 ms.

## 1.0.6

### Performance

- JSON parse + stringify overhaul: shorter dict-key tags, repeated-
  key interning, direct float/string encoders, pooled scratch
  buffers; `json_roundtrip` 1665 to 520 ms, faster than Python's C
  json on the bench.
- Function-call hot path passes function metadata by pointer rather
  than copying a 350-byte struct on every call; `recursive_fib` 88
  to 75 ms.
- Method dispatch VMValue fast path on `instance.method(args)`;
  `class_dispatch` 30 to 21 ms.
- Tail-call elimination: `return f(args)` reuses the current frame
  for primitive-typed-arg functions, removing the stack-depth
  ceiling on tail-recursive loops.

### Language features

- Iterator protocol: classes with `__iter()` / `__done()` /
  `__next()` work in `for (x in obj)`.
- `streams.Stream` fluent collection pipeline (`map`, `filter`,
  `take`, `toList`, `toSet`, `count`, `first`, `reduce`, `forEach`,
  `anyMatch`, `allMatch`); lazy by default.
- `reflect.location(target)` returns `{module, line, column}` for
  functions, classes, closures, decorator targets, and instances.
- Named arguments in `defer` for callable, instance-method, and
  module-function shapes.
- Nested generic call-site inference: `list<dict<K, V>>` and
  deeper shapes bind every leaf type parameter.

### Bug fixes

- Cross-chunk closures invoked through a stdlib-module class
  method no longer resolve to the wrong function index.

### Stdlib

- New `streams` module: `streams.of(source)` to wrap an iterable,
  plus the `Stream` class.

## 1.0.5

### Performance

- `acc = acc + "literal"` in tight loops now uses a hidden builder;
  `string_concat` 78 to 8 ms.
- Callbacks (`collections.map` / `filter` / `reduce` etc.) no longer
  rebuild a sub-VM per call; `list_functional` 1604 to 13 ms.
- Regex compile cache (`re.*`); `regex_match` 187 to 64 ms.
- Field-access inline cache; `class_dispatch` 26 to 21 ms.
- Tagged-`VMValue` arithmetic on hot ops; `dict_ops` 27 to 19 ms.
- Compile-time folding of literal arithmetic; div-by-zero is a
  check-time error.
- Direct `runtime.Value` to JSON encoder.

### Stdlib

- `strings.StringBuilder` class for explicit builder-backed string
  assembly.
- `csv.parse` / `csv.parseDict` / `csv.stringify` for in-memory CSV;
  options: `delimiter`, `trimSpace`.
- `path.glob` now supports Python-style `**` recursive matches.
- `math.median` / `percentile` / `quantile` / `mode` over numeric
  lists. R type-7 linear interpolation.
- String methods `splitRegex`, `replaceRegex`, `matchesRegex` for
  regex-aware split / replace-all / test.

### Benchmarks

Three new scoreboard benches: `regex_match`, `json_roundtrip`,
`list_functional`.

## 1.0.4

Bytecode VM hot-path performance + a lifted compiler parity gap,
on top of the type-matcher fixes that surfaced building the Gebweb
Tasks example app.

### Language features

- **Cast overloading via dunder methods.** A class can now control
  how its instances respond to `as TYPE` casts by defining
  `__string`, `__int`, `__float`, `__bool`, `__decimal`, or
  `__bytes`. The dunder's declared return type must match the
  target primitive; the semantic analyzer rejects mismatches at
  compile time, and the runtime double-checks the returned value.
  Falls back to the existing built-in cast logic when no dunder is
  defined for the target type. Parallel to the existing operator
  overloading dunders (`__add`, `__lt`, etc.). New
  `async.token()` builtin returns a fresh uncompleted Task used as
  a pure cancellation signal by the redesigned Timer/Ticker stdlib.

### Known limitations

- **Async callbacks that close over a `BytecodeClosure` / `Function`
  passed to a stateful native module can race with parent VM state.**
  The wrap layer that bridges Geblang callable values into native
  code captures the parent VM by pointer; when the native side then
  invokes the wrapped value on a goroutine (as `async.run`,
  `async.sleep`, etc. do), reads of `vm.globals` from the worker
  goroutine race with continued writes on the main goroutine. The
  go test race detector flags two parity tests
  (`TestParityTimerFires`, `TestParityTickerStops`); functional
  output is correct on both backends but ordering of those reads is
  technically unsynchronised. Closing the race needs a refactor of
  the native bridge to thread a per-goroutine VM context; queued
  for a 1.0.5 architectural pass.

### Performance

Profile-guided round (Geblang's own `profiler.snapshot()` /
`profiler.delta()` plus Go's pprof on `recursive_fib`,
`string_concat`, `class_dispatch`):

- **Static-type propagation through function calls and class
  fields.** `staticIntExpr` / `staticStringExpr` now recurse into
  `CallExpression` (when every matching overload declares the
  target return type) and `SelectorExpression` (when the receiver
  is `this` or a typed class instance and the named field is
  declared with the target type). Required flushing
  `class.FieldNames` / `class.FieldTypes` into `chunk.Classes`
  before compiling each method body so field metadata is visible
  during method compilation. Profile shows `vm.add` disappearing
  from `recursive_fib` (was 10.26% of CPU) and `class_dispatch`
  hot paths; replaced by the inline-handled `OpAddInt` family.

- **Fused string-append peephole.** `local = local + "literal"`
  and `global = global + "literal"` now compile to a single
  `OpAppendStringConst` / `OpAppendGlobalStringConst` instead of
  the three-opcode sequence `OpGetLocal`/`OpAddStringConst`/
  `OpSetLocal`. Bytecode chunk format version bumped 53 to 54.

- **REPL accepts `1 as bool`.** The REPL's auto-semicolon-insertion
  treated only identifier / numeric-literal stmt-end tokens as
  triggering insertion. `bool` is the only type name that lexes as
  a keyword (string/int/float/decimal/bytes lex as `Ident`), so
  `as bool` at the end of a REPL line gave `expected ;, got EOF`.
  `Bool` is now in the stmt-end set.

Bench impact (3-run medians, after the full sequence of Tier A +
type-propagation + peephole + earlier 1.0.4 work):

| Bench | Pre-1.0.4 | After 1.0.4 | Δ |
|-------|----------:|------------:|--:|
| numeric_loop   | 131 | 124 | -5% |
| recursive_fib  |  92 |  86 | -7% |
| list_pipeline  |  13 |   9 | -31% |
| string_concat  |  84 |  70 | -17% |
| dict_ops       |  24 |  19 | -21% |
| class_dispatch |  47 |  37 | -21% |

Geblang now beats Python on `numeric_loop`, `list_pipeline`, and
`dict_ops`; competitive on the rest. The remaining gap to PHP
(2.5x to 5.4x on the slow benches) is structural: Go interpreter
floor on dispatch, Go string-concat allocator pressure on
`string_concat`, and per-call frame setup on `recursive_fib`. The
roadmap entries for B1 (flat-stack locals) and C-tier interface
removal would address those; both queued for 1.0.5.

### Tooling

- **`profiler.delta` / `memory` / `peak` / `cpu` return dicts that
  are usable from Geblang.** They previously returned
  `*runtime.Dict` (pointer); the VM's index handler only matches
  `runtime.Dict` by value, so `d["elapsed_ms"]` raised "dict is
  not indexable". Now returns `runtime.Dict` directly; the docs
  example actually works.

### Documentation

- **`docs/user/03-types.md`** now points to
  `stdlib/08-collections.md` for the full list/dict/set method
  catalogue (`length`, `push`, `pop`, `slice`, `map`, `filter`,
  `reduce`, `keys`, `values`, `items`, `add`, `union`,
  `difference`, etc.).
- **`docs/user/stdlib/08-collections.md`** gains a "Keyed
  Functional Helpers" section enumerating the instance-method form
  of `sortBy`, `minBy`/`maxBy`, `topK`/`bottomK`, `sumBy`,
  `frequencies`, `indexBy`, `containsBy`, `binarySearch`,
  `lowerBound`/`upperBound`, `take`, `zipWith`, `lazyMap` /
  `lazyFilter`, etc. (interchangeable with the `collections.X(list,
  ...)` module form).

### Earlier 1.0.4 performance work

- **`OpAdd` string fast-path** (`vm.go:add`). The dispatcher used
  to call `callBinaryOperatorMethod` first on every add, including
  the common `string + string` case where the built-in `string`
  type has no `__add` magic method. The runtime.String check now
  short-circuits at the top of `OpAdd` when both operands are
  strings; the method-dispatch detour is preserved for class
  instances on the left.

- **Single-overload method dispatch shortcut**
  (`vm.go:selectRuntimeFunction`). Most user classes declare a
  single overload per method. The dispatcher now skips the
  matches-slice allocation + post-loop "ambiguous overload" check
  for `len(indices) == 1`, going straight to arity + type
  validation on the lone candidate. Behaviour is unchanged.

- **String-key dict fast path** (`vm.go:dictKeyFor`). A new helper
  inlines `runtime.String` and `runtime.SmallInt` key conversion
  (the 99% case) and falls through to `native.DictKey` for
  composite keys. Wired into the hot dict ops: index get/set,
  `dict.contains`, `dict.get`, and the `set` membership check.

- **Compile-time `OpAddString` opcode** (`compiler.go` +
  `vm.go`). When the compiler can prove both operands of `+` are
  statically typed `string` (via `staticStringExpr` mirroring the
  existing `staticIntExpr`), it emits a specialised `OpAddString`
  opcode that runs the concat inline with no type switch or
  method-dispatch detour. Mirrors the existing `OpAddInt` family
  for ints. Bytecode chunk format version bumped 50 to 51.

- **Method-pointer lookup cache** (`vm.go:lookupMethodLower`).
  A single-slot cache keyed by (class name, lowered method name)
  short-circuits the `classInfo.Methods` Go-map access on the
  second-and-later dispatches to the same method on the same
  class. Tight loops calling one method on one class (every
  `class_dispatch`-shaped workload) hit the cache on >99% of
  calls and skip the map lookup entirely.

Bench impact (median ms, before to after Tier 1 + Tier 2,
over 3 runs):

| Bench | Before | After | Δ |
|-------|-------:|------:|--:|
| numeric_loop   | 131 | 129 |  -2% |
| recursive_fib  |  89 |  85 |  -4% |
| list_pipeline  |  13 |   9 | -31% |
| string_concat  |  84 |  70 | -17% |
| dict_ops       |  24 |  19 | -21% |
| class_dispatch |  47 |  38 | -19% |

Geblang is now faster than Python on `numeric_loop`,
`list_pipeline`, and `dict_ops`; competitive on the rest.

New parity tests `TestParityStringAddFastPath`,
`TestParitySingleOverloadMethodDispatch`,
`TestParityDictKeyFastPath`, `TestParityOpAddStringStaticTyping`,
`TestParityMethodLookupCache`; new language test
`tests/core/vm_hot_path_test.gb`.

Tier A follow-up (`vm.go` + `bytecode.go`):

- **`OpAddString` writes the result VMValue directly into the
  stack slot**, mirroring the `OpAddInt`-family inline write. The
  handler reads operands from `vm.stack[n-2]` / `vm.stack[n-1]`
  without calling `vm.pop()`/`vm.push()`, so the interface
  materialise + function call overhead drops out. The interface
  box for the result `runtime.String` is unchanged and dominates
  the per-iteration cost; closing that gap is the job of B2
  (VMKindString variant on VMValue), planned next.

- **Per-call type-validation loop is now skipped for functions
  whose params are entirely empty / `any`-typed.** A new
  `FunctionInfo.requiresParamValidation` bool is precomputed at
  chunk-load time (in `prepareFunctionTypeMetadata`); both the
  fast and slow call paths short-circuit the whole validation
  walk when it's false.

- **Inline-cache experiment on `OpMethodCall` / `OpMethodCallNamed`
  did NOT ship.** A per-instruction class-pointer cache was
  implemented, measured, and reverted: the existing VM-global
  `methodLookupClass/Name/Indices` single-slot cache already hits
  >99% on the monomorphic call sites the `class_dispatch` bench
  exercises, so the per-call bounds check and cache compares the
  inline cache added cost more than they saved. Documented as a
  future candidate when a polymorphic-call benchmark exists.

- **`VMKindString` variant on `VMValue` did NOT ship.** A new kind
  was added with an inline `Str string` field so `OpAddString`
  could skip the interface-box heap allocation per push. Grew
  `VMValue` from 32 B to 48 B (+50 %), which regressed
  `string_concat` (69 to 82 ms), `dict_ops` (18 to 23 ms),
  `class_dispatch` (37 to 42 ms), and `recursive_fib` (85 to 95 ms)
  via worse cache locality on the stack/locals/globals slices.
  Reverted. The runtime.String interface box remains the dominant
  per-iteration cost on `string_concat`; closing it cleanly needs
  either a smarter VMValue layout (e.g. unsafe-overlay onto the
  existing Boxed field) or compile-time string interning that
  reduces the alloc rate enough to make the GC pressure go away.

### Parity

- **Empty-container defaults compile to bytecode directly.** Parameter
  and class-field defaults of the form `dict opts = {}` and
  `list xs = []` previously routed through the evaluator
  (`compiler.go:4757` rejected anything beyond primitive literals).
  The compiler now accepts empty `DictLiteral`, `ListLiteral`, and
  `SetLiteral` as defaults; the runtime constant pool gets three
  new tags (10/11/12) for the empty containers. To avoid the
  Python-style mutable-default trap, the VM clones the container
  at fill time via a new `cloneContainerDefault` helper, so each
  call (or new class instance) sees a fresh empty container.
  Non-empty container defaults (e.g. `list xs = [1, 2, 3]`) still
  fall back to the evaluator - lifting those needs full
  expression evaluation at call time, which is a bigger
  restructuring of the calling convention. Bytecode chunk format
  version bumped 51 to 52. New parity test
  `TestParityEmptyContainerDefaults`; new language test
  `tests/functions/empty_container_defaults_test.gb`.

- **`static func` methods compile to bytecode directly.** Previously
  any class with a `static func` declaration tripped the
  `compiler.go:741` "does not support static functions yet" parity
  error and the CLI fell back to the tree-walking evaluator. The
  parity error reflected an incomplete implementation, not a real
  constraint: the runtime infrastructure for static methods
  (`class.StaticMethods`, `OpCallStaticMethod`, `OpGetStaticValue`)
  was already in place. The compiler now lowers static method
  bodies through the same pipeline as regular methods (skipping
  the implicit `this` receiver), so scripts using static methods
  (including every `@ApiResource` entity in Gebweb that carries
  `static func repositoryClass()`) now run purely on the VM.
  New parity test `TestParityStaticFunctionLifted`; new language
  test `tests/classes/static_methods_test.gb`. Two pre-existing
  Go tests that asserted the static-func rejection were updated
  to use non-literal class field defaults as their canonical
  "still-unsupported" feature.

- **Spread arguments on a callable VALUE compile to bytecode
  directly.** Two compiler.go sites used to reject spread on
  callable values: parenthesized-selector callable expressions
  (`(obj.fn)(...args)`, line 2440) and complex callable
  expressions (`fns[i](...args)`, `getFn()(...args)`, line
  2602). Both forms now emit the same `OpMethodCallSpread`
  with the `__invoke` method name that the existing
  identifier-callable spread path uses. Static args before the
  spread are supported (`(h.adder)(1, ...rest)`); named args
  mixed with spread are rejected at compile time as before.
  New parity test `TestParityCallableSpread`; new language
  test `tests/functions/callable_spread_test.gb`.

### Bug fixes

- **`cli.table` accepts the documented options-dict form.** The
  user manual (`docs/user/stdlib/13-cli.md`) showed
  `cli.table(rows, {columns: [...], headers: [...], separator: " | "})`
  but the implementation only accepted an optional bare list of
  header strings; calling with a dict raised
  `cli.table headers must be list<string>`. The implementation
  now accepts both forms: the legacy `cli.table(rows, ["A", "B"])`
  AND the documented options dict. `columns` picks the dict
  fields to render and their order; `headers` overrides the
  display labels (defaulting to the column key names);
  `separator` customises the inter-column gap (defaulting to two
  spaces). The legacy list form continues to work unchanged.

- **REPL multi-line container literals no longer get a spurious
  semicolon injected** (`cmd/geblang/repl.go:replInsertSemicolons`).
  The ASI-style semicolon-insertion walked the token stream looking
  for statement-ender tokens at line ends, but didn't track bracket
  nesting. A list of dict literals like:

  ```
  let rows = [
      {"name": "Alice"},
      {"name": "Bob"}
  ];
  ```

  had a `;` inserted after the closing `}` of each inner dict
  (because `}` is a statement-ender token), splitting the outer
  list literal and producing `expected next token to be ], got ;`.
  The injector now tracks `(`/`[` nesting depth across the source
  and only inserts at depth 0. Braces `{` are deliberately not
  counted so semicolons are still inserted inside function /
  if / for bodies. New regression test
  `TestReplInsertSemicolonsRespectsNesting`.

- **User class named `Task` no longer collides with the runtime
  async `Task`.** The evaluator's overload / parameter type-matcher
  unconditionally rejected any value flowing into a `Task`-typed
  parameter when the value wasn't a `*runtime.Task`. A user-declared
  `class Task { ... }` therefore broke at every dispatch. `repo.save(t)`
  with `save(Task entity)` and a user-class Task argument errored as
  `no matching overload`. The matcher now falls through to user-class
  matching when the value isn't the async Task primitive, so a
  user class named `Task` works exactly like any other user class.
  The VM was already correct (its type-name dispatch routes through
  `vmTypeKindForBase` and never hard-codes the `Task` string), so
  only the evaluator path needed the fix.

- **`?UserClass` parameter matching on the VM.** The VM's
  `parseVMTypeSpec` kept the leading `?` on `spec.base`, so the
  user-class comparison `value.TypeName() == spec.base` always
  failed for nullable parameter types like `?AuthConfig` or
  `?Task`. Only the `?T` shape for primitives was working
  (those routed through the kind switch). The parser now strips
  the `?` from `spec.base` at parse time and carries the
  nullability on the separate `spec.nullable` flag, so a
  `?UserClass` parameter accepts a `UserClass` instance again.

New parity test `TestParityUserClassNamedTaskNoCollision`; new
language test `tests/classes/user_class_named_task_test.gb`.

## 1.0.3 (released)

Small parity / ergonomics fixes uncovered while extending Gebweb.

### New

- **`web.parseMultipart(request)`.** New native that decodes a
  `multipart/form-data` request body into a
  `{fields: dict<string, string>, files: dict<string, dict>}` dict,
  where each file entry is `{filename, contentType, bytes}`.
  Returns an error (catchable, wrappable as 400) when the body
  isn't multipart or the boundary is missing. Gebweb's
  `dict<string, UploadedFile>` parameter binding is built on top
  of this. New parity test `TestParityWebParseMultipart`.

### Bug fixes

- **Import alias collisions no longer leak across files (evaluator).**
  The evaluator kept a process-wide `importNames` map that recorded
  the LAST `import X as Y` per alias `Y`, so two files that both
  used the same alias for different canonical modules collided: e.g.
  a user file `import web.websocket as websocket;` overwrote the
  alias mapping that stdlib `import websocket;` had registered for
  the native, and stdlib code calling `websocket.upgrade(...)`
  routed through the user's wrapped module instead, surfacing as
  "module websocket has no export upgrade". The fix adds a
  `Canonical` field to `runtime.Module` (per-binding canonical
  module name) and has the call dispatcher consult the env-local
  binding's `Canonical` before falling back to the shared map. The
  VM was already correct because each compiled chunk owns its own
  globals; only the evaluator needed the fix. New parity test
  `TestParityImportAliasDoesNotCollideAcrossFiles`; new language
  test `tests/core/import_alias_collision_test.gb`.

- **`list.sort()` is an alias for `list.sorted()`.** The LSP
  catalog (`catalog.go:142-143`) advertised both names but only
  `sorted` was wired into the method dispatcher, so user code reading
  the documented surface and calling `xs.sort()` failed with `list
  has no method sort` on both backends. Both names now route to the
  same implementation. New parity test
  `TestParityListSortAliasesSorted`; new test methods
  `listSortMethod` / `listSortWithComparator` in
  `tests/stdlib/collections_test.gb`.

- **`geblang check` recognises the `string` native module.** The
  CLI's `nativeImportModules` allowlist - used by `geblang check`'s
  import-resolution pass to skip native modules that have no
  on-disk source - was missing `string`, so any file with
  `import string;` produced `error[import]: cannot resolve import
  string` even though the evaluator and VM both registered the
  module. Tests passed because the test runner doesn't gate on
  check diagnostics; `make check-lang` exposed the
  false-positive. Added `"string"` to the allowlist alongside
  `"smtp"`. `tests/core/cross_type_casts_test.gb` is now check-clean.

- **Cross-module `implements` on the bytecode compiler.** A class
  declaring `implements mod.Interface<T>` against an interface
  exported from another module - the canonical case is
  `class WidgetRepo implements repository.Repository<Widget>` in
  Gebweb - failed the bytecode compile with `bytecode compiler
  interface mod.Interface is not declared` and fell back silently to
  the evaluator. The evaluator's `resolveTypeValue` already walks
  imports; the compiler's `c.interfaces` lookup only knew about
  locally-declared interfaces, mirroring a parent-class case that
  was already allowed at `compiler.go:891`. The compiler now accepts
  any dotted name (`strings.Contains(name, ".")`) under both
  `implements` clauses and interface `extends` clauses, and the VM's
  `interfaceMatches` strips the module prefix from the stored name
  so `instanceof Repository` continues to match the
  module-qualified entry stored on the class. Six gebweb test files
  + the `widgets.gb` example that previously errored on
  `geblang check` now compile cleanly to bytecode. New parity test
  `TestParityCrossModuleImplements`; new language test
  `tests/classes/cross_module_interface_test.gb`.

- **`func` value casts to `callable` on the evaluator.** The
  evaluator's CastExpression handler matched the value's
  `TypeName()` ("func") against the target ("callable") and
  rejected the cast with `cannot cast func to callable`. The VM
  accepted it because its `castValue` returned `value` when the
  target was a known alias of the value's runtime type. Both
  backends now route the `callable` / `func` / `function` family
  through `runtime.IsCallableValue`, so any callable runtime value
  (Function, OverloadedFunction, BytecodeFunction, instance with
  `__invoke`) casts cleanly. Surfaced from gebweb middleware
  helpers that store user callbacks in a `dict<string, any>`
  options dict and later cast them back to `callable`. New
  parity test `TestParityFuncAsCallable`; new test class
  `FuncAsCallableTest` in `tests/core/cross_type_casts_test.gb`.

## 1.0.2 (2026-05)

A quality-of-life release polishing two papercuts that surfaced while
building Gebweb.

### Highlights

- **Range-to-list shorthand.** The top-level `range(start, end[, step])`
  builtin returns a `list<int>` directly, inclusive of both endpoints.
  `Range` gains a `.toList()` method for symmetry with the literal form
  `(1..5).toList()`. The char-range literal `'a'..'z'` now produces a
  `list<string>` of single-character entries eagerly, so
  `let list<string> letters = 'a'..'e'` works without an intermediate
  conversion. `list.toList()` is a no-op pass-through so the same
  `.toList()` call works whether you have a `Range`, a `Set`, or
  already a list.
- **Tagged generic collections.** `list<T>`, `dict<K,V>`, and `set<T>`
  values flowing through a typed declaration or parameter boundary
  now carry their declared element types as a reified tag.
  `reflect.typeBindings(xs)` on a `list<int>` returns `{"T": "int"}`;
  untagged collections (raw literals not bound to a typed name)
  return `{}` rather than erroring. The "not preserved at runtime"
  caveat in chapter 3 is gone.
- **`instanceof <TypeRef>`.** The right operand of `instanceof` is
  now a full type reference - `xs instanceof list<int>`,
  `d instanceof dict<string, User>`, `x instanceof ?int` all parse
  and dispatch. Tagged collections compare bindings invariantly
  (same rule as 1.0.1 user-class generics); untagged collections fall
  back to a structural walk over their elements.
- **Reflection harmonised across backends and primitives.** The
  reflect API now produces the same shape on the evaluator and the
  VM, regardless of whether you pass a class value, a class instance,
  or a name string. `reflect.class("Foo")` resolves a class declared
  in another loaded module via the module loader.
  `reflect.methods(value)` accepts an instance or a primitive
  (`reflect.methods([1, 2, 3])` returns the list method names,
  `reflect.methods("hi")` returns the string method names).
  `reflect.fields(class)` now returns a list of
  `{name, type, nullable, hasDefault}` dicts (was a list of name
  strings) - the type info was previously discarded. Cross-module
  `instanceof Parent`, `e as Parent`, and `catch (Parent e)` all
  walk the full parent chain (error-derived classes capture their
  parents at construction so the chain is reachable even when the
  catch site is in a different chunk).
- **`json.parseAs(text, ClassWithoutCtor)` data-class shape.**
  Classes without a constructor now have their fields populated
  directly from the parsed dict (previously they were instantiated
  with empty fields). Matches the canonical "data class" usage.
- **Numeric `//` and `as int` close cross-type gaps.** The floor-
  division operator `//` now accepts `decimal // decimal` and
  `float // float` in addition to `int // int`. Result type
  matches the operands (same-kind policy): `7 // 2` is `3`,
  `7.5 // 2.0` is the decimal `3.0000000000`, `5.5 // 2.0` (float)
  is `2.0`. The companion `%` is a floor-modulo on all three
  numeric types, so the sign of the remainder follows the divisor
  (`-7 % 2 == 1`, `-7.5 % 2.0 == "0.5000000000"`). The `as int`
  cast now truncates toward zero from `decimal` and `float` (e.g.
  `2.7 as int == 2`, `-2.7 as int == -2`) and accepts `bool`
  (`true as int == 1`). Previously `decimal as int` rejected any
  non-integer-valued operand and `bool as int` was not supported.
  New parity tests `TestParityFloorDivOnDecimalAndFloat` and
  `TestParityCastTruncatesDecimalAndFloat`; new language test
  `tests/core/floor_div_and_cast_test.gb`.
- **Cast failures are catchable on both backends.** A failed
  `x as Y` (e.g. `"hi" as bytes`) used to escape the VM as an
  uncatchable `bytecode runtime error: cannot cast ...`, even
  inside a surrounding `try / catch (RuntimeError e)`. The
  evaluator already raised a catchable `RuntimeError`. The VM
  now throws the same catchable `RuntimeError` via the typed-
  throw path, so frameworks and user code can defensively
  `try` a cast on both backends. New parity test
  `TestParityCastErrorIsCatchable`; new test case
  `castFailureIsCatchable` in `tests/errors/try_catch_test.gb`.
- **`getMessage()` and `getClass()` on built-in errors.** Built-
  in error values previously only exposed the `.message` and
  `.class` *fields*. The Java / PHP / Python idiom -
  `e.getMessage()`, `e.getClass()` - errored with "X has no
  method getMessage". Both accessors are now methods on every
  `Error`-derived class, including user-defined subclasses
  (they dispatch through the same built-in path). The fields
  still work; choose either form. New parity test
  `TestParityErrorGetMessageAndGetClass`; new test cases
  `getMessageAndGetClassOnBuiltin` and
  `getMessageOnUserDerivedError` in
  `tests/errors/try_catch_test.gb`.
- **Cross-type casts: `string <-> bytes` and `list <-> set`.**
  `"hello" as bytes` encodes UTF-8; a `bytes` value
  `as string` decodes UTF-8 (the cast raises a catchable
  `RuntimeError` if the byte sequence isn't valid UTF-8).
  `[1, 1, 2, 3] as set<int>` deduplicates (first occurrence
  wins); `{1, 2, 3} as list<int>` materializes in unspecified
  order. Previously each of these raised "cannot cast X to Y".
  The element-type generic argument is required for the
  collection casts to match the typed-declaration rules.
  New parity test
  `TestParityCrossTypeCastsForBytesAndCollections`; new test
  classes `StringBytesCastTest` and `ListSetCastTest` in
  `tests/core/cross_type_casts_test.gb`.
- **New `string` module: factory and static helpers.** A small
  namespace for things that don't fit as instance methods on a
  string value:
  - `string.fromCodePoint(n)` -> single-character string for
    Unicode codepoint `n`. Rejects negative values, codepoints
    above U+10FFFF, and the UTF-16 surrogate range
    (U+D800..U+DFFF). Counterpart to the existing
    `.codePointAt(i)` instance method.
  - `string.fromCodePoints(list<int>)` -> multi-character
    string built from a list of codepoints, same validation
    per element.
  - `string.compare(a, b)` -> three-way comparison returning
    -1 / 0 / +1. Useful as a sort key
    (`xs.sortBy(string.compare)`). Java / Go convention.
  - `string.equalsFold(a, b)` -> case-insensitive equality
    respecting Unicode case folding (`"CafÉ" == "café"`).
  For timing-attack-safe equality (HMAC verification, token
  comparison) use `secrets.constantTimeEqual` instead; the
  string-module helpers are not constant-time.
  New parity test `TestParityStringModule`; new test class
  `StringModuleTest` in `tests/core/cross_type_casts_test.gb`.
- **`null as ?T` is a successful cast on both backends.** Casting
  a `null` value to a nullable type previously errored with
  "cannot cast null to T" on the evaluator (the cast path
  dropped the nullable bit from the target TypeRef before calling
  castValue). The VM accepted it after the 1.0.2 cast-error
  catchability work but the eval side still failed. The
  evaluator's CastExpression handler now special-cases a
  nullable target ahead of the class-chain match, mirroring the
  VM. New parity test `TestParityNullAsNullableType`; new test
  class `NullableCastTest` in
  `tests/core/cross_type_casts_test.gb`.
- **Method-dispatch hot path: name-lowering and classInfo lookup
  caches.** Two small VM-side memoising caches reduce per-call
  overhead on tight method-call loops. `nameLowerCache` skips
  the repeated `strings.ToLower(methodName)` on every dispatch
  (the chunk's `ClassInfo.Methods` is keyed lowercase, so the
  lowered form is what the lookup actually needs).
  `classInfoNameCache` skips the `vm.classIndex` lookup when
  the receiver's `instance.Class.Name` has already been
  resolved. Measurable on the `class_dispatch` extended
  benchmark (~12% improvement, 42ms -> 37ms median for 50000
  calls); numeric / list / string benchmarks unchanged.

### Bug fixes

- **VM closure capture for camelCase identifiers.** The compiler's
  free-variable scanner lowercased identifier names while local
  scope entries kept their original case, so closures that captured
  a variable with uppercase letters silently missed the capture.
  The closure body then read the wrong stack slot at runtime,
  producing puzzling type errors. Both `freeVarSet` and the
  enclosing scope are now case-sensitive throughout. New parity
  test `TestParityClosureCaptureCamelCase`.
- **`null` matches `any` in VM method overload resolution.** The
  VM rejected `obj.send(null)` when `send` was declared as
  `func send(any body)` - the early null-vs-nullable check fired
  before the `vmTypeAny` short-circuit, so the overload selector
  reported "no matching overload". Evaluator was already correct.
  New parity test `TestParityNullMatchesAnyParam`.
- **Cross-module typed-parameter dispatch.** A function declared
  with a parameter typed as a module-qualified class (e.g.
  `func f(appmod.GebwebApp app)`) failed at the dispatch boundary
  because the runtime kept the qualified name. The strip-prefix
  path now applies uniformly so a stdlib facade can declare
  `appmod.GebwebApp` parameters and accept values built by the
  same module.
- **`reflect.class(name)` finds user classes from stdlib modules.**
  When called from inside an imported module (e.g.
  `gebweb.binding`), `reflect.class("UserDTO")` previously
  returned null because the module loader only scanned imported
  modules' chunks. The loader now also scans the main chunk, so
  framework code can resolve user-script classes.
- **Cross-chunk deserialize.** `json.parseAs(text, UserDTO)` from
  inside a stdlib module crashed with "class index out of range"
  because the VM tried to resolve the user class's index against
  the wrong chunk. The deserialize path now dispatches via the
  module loader to a sub-VM bound to the right chunk, so the
  framework can deserialize main-chunk DTOs from binding helpers.
- **Cross-VM exception propagation.** A throw originating in user
  code that bubbled across a sub-VM boundary collapsed to a plain
  "uncaught RuntimeError" string at the boundary, losing the
  original class and parent chain. `catch (errors.HttpException e)`
  in a stdlib closure no longer matched the original
  `NotFoundError`. The VM now wraps the underlying `runtime.Error`
  in a `vmThrownError` so the calling VM can recover it via
  `errors.As` and re-throw it as a typed `pendingThrow`. New
  parity test `TestParityCrossModuleThrowCatch`.
- **`as` widens to a parent class on both backends.** The cast
  operator previously rejected widening an error- or instance-
  derived value to a declared parent (`e as errors.HttpException`
  for a `NotFoundError`). Both backends now walk the parent chain
  the same way `instanceof` does and treat the cast as a no-op
  when the value is already an instance of the target.
- **Eval `reflect.method(...)()` preserves module access.** A
  bound method returned by `reflect.method` ran on a fresh stub
  Evaluator with no module loader, so the method body couldn't
  reference any imported module (`gebweb.notFound(...)`,
  `json.stringify(...)`, ...). The bound Native closure now
  captures the live host Evaluator. New parity test
  `TestParityReflectMethodPreservesModuleAccess`.
- **Parenthesized selector forces value-then-call semantics.**
  `(obj.fn)(args)` previously parsed identically to `obj.fn(args)`
  and dispatched as a method call on `obj`, ignoring the parens.
  The parser now flags the SelectorExpression so the evaluator and
  the VM both invoke the VALUE of `obj.fn` (a closure stored in a
  field, a method reference, anything callable) instead. Required
  for caching callables on instance fields - `let response =
  (this.dispatch)(request);`. New parity test
  `TestParityParenthesizedSelectorInvokesValue`.
- **`reflect.className(value)`.** New reflect builtin returning
  the class's own identifier. For a class value, returns its name
  (`reflect.className(User)` returns `"User"`); for an instance,
  same as `reflect.typeOf(instance)`; for primitives, the runtime
  type name (`"int"`, `"string"`). Symmetric with `reflect.class(name)`
  going the other way. Required by gebweb's DI container to
  identify a service by its class without instantiating it. New
  parity test `TestParityReflectClassName`.
- **Class-ref runtime construction via `classRef()`.** A class
  value passed through a variable or obtained from `reflect.class`
  is now callable to construct an instance - previously the VM
  treated `classRef()` as a static-method lookup of `__invoke` and
  errored with "unknown static method ... __invoke". Both backends
  now construct the class (routing through the moduleLoader when
  the class was declared in a different chunk). Required by
  `gebweb.app([HelloController])` to instantiate user controllers
  via DI. New parity test `TestParityClassRefRuntimeConstruction`.
- **Cross-chunk `reflect.constructors`.** When given a class value
  declared in another chunk, the VM now dispatches through a new
  `ConstructorsForModuleClass` loader hook so the metadata
  reflects the originating chunk's constructor list rather than
  the caller chunk's stale view. Same pattern as the deserialize
  and construct module-class hooks.
- **Dotted decorator names.** `@Foo.bar`, `@Foo.bar.baz` and
  longer chains parse as a single composite identifier. The whole
  dotted string is the decorator's name; dispatch is by exact
  string match. Lets framework-style families like
  `@Assert.email`, `@Assert.minLength(2)` group related rules
  under a common prefix without naming-collision worries.
- **Field-level decorators.** `@`-prefixed annotations on field
  declarations inside a class body now parse and persist into
  `reflect.fields(class)` as a per-field `decorators` list. Field
  decorators are pure metadata - the runtime never executes them
  automatically; frameworks read them via reflection to drive
  validation, serialisation filters, OpenAPI enrichment, etc.
  See *Classes And Interfaces > Decorators* in the manual for the
  semantics. New parity test
  `TestParityFieldDecoratorsAndDottedNames`.
- **Chunk format Version 49 to 50.** New per-field decorator-
  metadata list parallel to `FieldNames` so reflection over
  cross-chunk classes returns the field annotations from the
  declaring chunk.
- **Bare `return;` in a `void` function.** The static analyzer
  previously rejected `return;` inside a function declared as
  returning `void`, raising "cannot return null from F returning
  void". An early exit from a void body is legal - there is no
  value being returned, only an early termination. Both the
  evaluator and the VM already handled this at runtime; only the
  analyzer needed adjusting. New parity test
  `TestParityBareReturnInVoidFunction`.
- **Trailing comma in list literals.** `[1, 2, 3,]` and multi-line
  variants now parse. Dict / set literals already supported it;
  list literals catch up. New parity test
  `TestParityTrailingCommaInListLiteral`.
- **Class declaration inside a block is rejected at parse time.**
  Previously `class X {...}` inside a method body parsed but
  produced confusing downstream failures. Now emits a clear
  "class declaration is only allowed at the top level" error.
  Same for `interface` and `enum`.
- **Lexical shadowing wins over module dispatch.** When a local
  variable shadows a built-in module name (e.g.
  `let errors = [...]` while the `errors` module is loaded), the
  receiver of an `X.method(...)` call now resolves to the local
  rather than the module. Eliminates a class of confusing
  "X is not a module" runtime errors. Both backends. The analyzer
  also emits a *warning* (not an error) at the shadowing
  declaration so a reader doesn't have to wonder why a familiar
  module name is bound to a list. New parity test
  `TestParityLocalShadowsBuiltinModule`.
- **`reflect.getField(instance, name)` and `reflect.setField(instance,
  name, value)`** native builtins. Dynamic field-by-name access /
  assignment for framework code (Gebweb's `@Assert` validator
  walks an instance's fields by name; the `@ApiResource` PATCH
  handler updates an entity field-by-field). Replaces the previous
  `json.parse(json.stringify(instance))` round-trip workaround.
  Both backends. New parity test `TestParityReflectGetFieldSetField`.
- **Forward function references in the bytecode compiler.**
  Previously `func a() { return b(); } func b() { ... }` failed
  with "no matching overload for b" because `b`'s parameter / return
  metadata wasn't populated until its body was compiled. The
  compiler now pre-populates function signatures during the
  initial sweep so call sites see the real shapes. New parity test
  `TestParityForwardFunctionReferences`.
- **Cross-chunk `reflect.fields(instance)` preserves field
  decorators.** Previously `reflect.fields(instance)` on a value
  handed to a sub-module returned no decorator info because the
  originating chunk's ClassInfo wasn't reachable. The instance's
  `runtime.Class.Fields` is now populated at construction time
  with each field's name and decorator metadata so framework
  code in another module sees the same annotations the
  declaring module does. The chunk-local path still wins when
  available (full type strings); the new path is the fallback
  for cross-chunk reflection. New parity test
  `TestParityCrossChunkInstanceFields`.

### Bytecode

- Chunk format Version 48 -> 49. New per-field type strings
  parallel to `FieldNames` so cross-chunk reflection on classes
  produces the same shape as the evaluator without consulting the
  source AST.

### Other

- Build: still Go 1.26.3.
- VS Code extension: bumped to 1.0.2 for parity.

## 1.0.1 (2026-05)

A correctness release that closes two related holes in the 1.0 generics
story.

### Highlights

- **Generic invariance** - User-defined generic class types are now
  *invariant* in their type parameters. Even when `Sub extends Base`,
  a `Box<Sub>` is not assignable to a `Box<Base>` parameter or typed
  variable. The static analyzer rejects the assignment at compile
  time; the runtime rejects it at the function-parameter boundary
  when the value's reified bindings disagree with the declared
  bindings. This is the standard invariance rule that
  Kotlin/Java/C# enforce, and it eliminates the classic unsoundness
  where a function widens `Box<Sub>` to `Box<Base>` and then inserts
  a sibling subtype. See chapter 3's *Generics: invariance*.
- **Explicit type-argument call syntax** - `ClassName<T>(args)` and
  `funcName<T>(args)` now parse correctly. Before 1.0.1 the parser
  treated `<` and `>` as comparison operators in these positions, so
  `Box<int>()` failed with a syntax error and `assertIs<string>("x")`
  silently compiled into a chained comparison that exploded at
  runtime. The new lookahead disambiguates: an identifier followed by
  `<TypeRef (, TypeRef)*>` immediately followed by `(` is a generic
  call; everything else stays a comparison. This is the form needed
  to write the invariance check above: `Box<Sub> b = Box<Sub>();`

### Tooling

- VS Code extension bumped to 1.0.1.

### Build

- Documentation now references the Go 1.26.3 toolchain (matching
  `go.mod`) as the minimum supported build environment.

## 1.0.0 (2026-05)

The first stable release. Everything documented in this manual is in scope
for the 1.0 stability promise: source-level syntax, stdlib APIs, runtime
semantics, and the bytecode chunk format. Future 1.x releases will add
features but not break what is below.

### Highlights

- **Destructors and context managers (two separate concerns)** -
  `func ~ClassName()` declares an end-of-lifetime hook. Destructors
  fire at program exit (the runtime sweeps every destructor-bearing
  instance that hasn't already been destroyed, in reverse-creation
  order) or via an explicit `del x;` statement, which retires the
  binding and invokes the destructor inline. `del` accepts an
  identifier only; the semantic analyzer flags any reference to a
  destroyed binding with `use of destroyed binding "x"` so the type
  system stays sound. The unrelated `with (resource) { ... }` block
  is the context-manager construct: it calls `__enter__()` on entry
  and `__exit__()` on exit (any exit path - normal, exception,
  return, break, continue) but **does not** invoke the destructor.
  See chapter 6's *Destructors* and *Context Managers* sections.
- **Class (de)serialisation** - `json.stringify(instance)` (and the
  YAML / TOML equivalents) accept user-defined class instances and
  dump their public fields by default (`_` / `__` prefixed names are
  private and skipped). Classes can override with `__serialize__()`
  (any returned dict / list / scalar is recursively serialised). The
  symmetric `json.parseAs(text, ClassRef)` reconstructs an instance,
  preferring a static `__deserialize__(dict)` factory when defined and
  falling back to positional constructor calls matched on parameter
  names. Same for `yaml.parseAs`, `toml.parseAs`, `xml.parseAs`. See
  chapter 6's *Serialisation*.
- **Async combinators** - `async.all([tasks])`, `async.race([tasks])`,
  `async.timeout(task, ms)`, and `async.cancel(task)` (also reachable
  via `task.cancel()` and the `task.cancelled` property). The pre-1.0
  "cancellation and structured scheduling remain roadmap items" caveat
  is gone. See chapter 9.
- **Scheduling primitives** - `time.scheduler.Timer`,
  `time.scheduler.Ticker`, and `time.scheduler.Interval` give callback-
  style scheduling with cancellation. Use `setTimeout` / `setInterval`
  aliases if you prefer the JavaScript naming.
- **Symmetric encryption** - `crypt.aesEncrypt` /
  `crypt.aesDecrypt` (AES-256-GCM) and `crypt.chacha20Encrypt` /
  `crypt.chacha20Decrypt` (XChaCha20-Poly1305) join the existing
  hash / HMAC / Argon2id / RSA / EC stack. See `stdlib/12-security.md`.
- **Reusable HTTP clients** - `http.newClient({...})` now accepts
  `cookieJar`, `keepAlive`, `maxIdleConns`, `proxy`, and
  `proxyFromEnv` options for production-shape HTTP usage. Session
  flows that need to retain `Set-Cookie` across requests just pass
  `"cookieJar": http.newCookieJar()`. Default User-Agent is now
  `Geblang/1.0` (override via `headers`).
- **Improved regex API** - `re.match` returns a clean `{text, groups,
  named}` dict instead of ad-hoc numeric-string keys, and `re.matchAll`
  iterates every non-overlapping match. See `stdlib/09-text.md`.
- **Wider encoding coverage** - `encoding.base32Encode` /
  `base32Decode` (RFC 4648, padded or unpadded) and
  `encoding.base58Encode` / `base58Decode` (Bitcoin / IPFS alphabet,
  preserves leading zeros). Accepts both string and bytes inputs.
- **`.length` everywhere** - `list`, `dict`, `set`, `string`, `bytes`,
  and `range` all expose `.length` as a property (alongside the
  existing `.length()` method).
- **Inherited generic type bindings** - `class Sub extends Base<string>`
  now propagates `T to string` to subclass instances, visible via
  `reflect.typeBindings(instance)["T"]`.
- **`func` as a field type** - `class Holder { func cb; ... }` parses
  correctly. `callable` and `function` work too.
- **Module top-level discipline** - files that begin with `module name;`
  now require declarative top-level statements only (`import`, `export`,
  `const` / `let` / typed declarations, `func`, `class`, `interface`,
  `enum`, `type` alias, and at most one `init { ... }` block).
  Free-standing calls, `if`/`while`/`for`/`match`/`try`, and bare
  assignments are rejected with a clear diagnostic; the rule does not
  apply to script files. See `docs/user/07-modules-packages.md`.
- **Static errors abort `geblang run`** - the previous behaviour
  printed `warning: ...` for static-analysis errors caught by the
  bytecode compiler (e.g. no matching overload, type mismatch),
  fell back to the evaluator, and crashed partway through the run.
  Now those errors abort the run cleanly before any statement is
  executed. Genuine compiler gaps (the bytecode compiler doesn't yet
  support a feature) still fall back silently. `geblang check`
  output is the source of truth for what blocks `geblang run`.
- **Diagnostic severity** - `semantic.Diagnostic` now carries
  `Severity` (Error / Warning). All existing checks remain at Error
  by default; the field is in place so future analyzer passes can
  emit Warning-level findings that surface in VS Code's Problems
  panel and in `geblang check` but don't block execution.
- **Aliased native imports compile on the VM** - the bytecode
  compiler now recognises `import path as natpath; natpath.clean(...)`
  and dispatches to the canonical `path.clean(...)` directly. Previously
  these calls failed to compile with `unknown bytecode name natpath`
  and fell back silently to the evaluator; under `--vm-strict` they
  were rejected outright. `stdlib/pathlib.gb` and
  `stdlib/schema/validator.gb` now also run on the VM rather than
  via fallback. Unknown-identifier failures from the bytecode
  compiler are no longer treated as parity gaps and abort the run
  along with the other static errors.
- **Autocomplete for primitive-type methods** - typing
  `someDecimal.<TAB>` after `decimal d = 3.14;` now surfaces
  `format`, `abs`, `toString`, etc. The LSP server's completion
  path detects the receiver type via a light lexical scan
  (`<primitiveType> <name>` declarations) and looks up the methods
  in a per-type table. Covers `string`, `int`, `float`, `decimal`,
  `bool`, `bytes`, `list`, `dict`, `set`, and `range`. Inferred
  declarations (`let x = ...`) and complex annotations (`?string`,
  `list<int>`) are best-effort; the catalog can be extended later
  if needed.
- **DAP launch pre-flight** - VS Code's *Run Without Debugging*
  (Ctrl+F5) and *Debug* launches both went through the DAP server's
  evaluator-only path, which skipped the static-analysis pre-flight
  the CLI run path performs. A script with a static error
  (no-matching-overload, type mismatch, undeclared identifier,
  module-top-level violation) would run partway through before
  crashing in the Debug Console, instead of aborting cleanly. The
  DAP server now runs the same semantic + bytecode pre-checks as
  `geblang run`; static errors abort the launch and surface the
  diagnostic in the Debug Console. `bytecode.IsParityError` is
  exported so cmd/geblang and internal/dap share the same
  parity-gap classifier.

### Toolchain

- **Go 1.26.3 build** - Docker image and `go.mod` now target Go 1.26.3.
  Dependency versions refreshed (`pgx` v5.5 to v5.9, `sqlite` v1.29 to
  v1.50, `golang.org/x/crypto` v0.17 to v0.51). `scripts/upgrade-go.sh`
  installs the matching toolchain on Ubuntu/WSL.
- **Vet-clean** - audited and fixed 181 printf-style call sites that
  Go 1.26's promoted vet check flagged in parser / semantic / vm.
- **Cleaner build output** - `make docker-build` / `make vscode-build`
  no longer print spurious "Error response from daemon: No such
  container" lines on first runs.

### Performance

Geblang's bytecode VM remains competitive with the reference
interpreters. Measured on this build (median of 7 runs; lower is better):

| Benchmark       | Geblang | Python  | PHP     |
|-----------------|---------|---------|---------|
| numeric_loop    | 122 ms  | 135 ms  | 29 ms   |
| recursive_fib   | 83 ms   | 46 ms   | 22 ms   |
| list_pipeline   | 7 ms    | 14 ms   | 11 ms   |

The 1.0 round of work was deliberately feature-focused; the post-Phase-O
performance baseline holds. See `benchmarks/run.py` for the harness.

### Bug fixes worth flagging

- **VM `??` infinite loop**: `value ?? default` inside an async-run
  callback or HTTP handler used to enter an infinite loop because
  `OpNullCoalesce` was missing from the bytecode VM's jump-shift
  list. Same fix for `?.` (`OpOptionalChain`). Fixed; both paths now
  have parity tests.

### Migration from 0.9.x to 1.0

If you have code written against a pre-release Geblang build, two changes
need source updates:

1. **`re.match` result shape**

   ```diff
   - let m = re.match("(?P<name>\\w+):(\\d+)", text);
   - io.println(m["0"]);     // full match (string key "0")
   - io.println(m["1"]);     // group 1
   - io.println(m["name"]);  // named group

   + let m = re.match("(?P<name>\\w+):(\\d+)", text);
   + io.println(m["text"]);          // full match
   + io.println(m["groups"][1]);     // group 1
   + io.println(m["named"]["name"]); // named group
   ```

   `re.findAll` was unchanged. The new `re.matchAll` returns a list of
   the new-shape dicts for iteration.

2. **Default User-Agent**

   Outgoing HTTP requests used to inherit Go's `Go-http-client/1.1`.
   They now send `Geblang/1.0`. If a server allow-lists by User-Agent,
   update its rules or pass `headers: {"User-Agent": "..."}`.

Everything else - syntax, control flow, classes, async, generators,
stdlib calls, decorators, error model - is source-compatible with the
last pre-1.0 build.

### Bytecode chunk format

Chunk `Version` bumped to 48 (was 46) to accommodate the new
`ParentArguments` slot and the `DestructorIndex` slot on `ClassInfo`.
Compiled `.gbc` artefacts from 0.9.x will not load on 1.0; rebuild
them with `geblang` 1.0 or run `geblang cache clean`.
