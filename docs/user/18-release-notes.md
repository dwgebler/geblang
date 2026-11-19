# Release Notes

## 1.43.0

### Added

- Generator comprehensions: `(x * 2 for x in xs if x > 0)` builds a lazy
  `generator` with the same `for` and `if` clauses as list comprehensions. The
  first source is evaluated, and checked to be iterable, where the
  comprehension is written; later sources, filters and the body run as values
  are read.
- Arrow functions: `(int n) => n * 2`, `(a, b) => { ... }`, `() => value` and
  `async (int n) => ...`. A parameter may leave out its type when the arrow is
  passed to a callback method of a list with a known element type (`map`,
  `filter`, `sortBy`, `reduce` and the rest); the type is filled in before the
  program runs and checked at run time. Elsewhere an untyped parameter is an
  error naming the parameter. In the REPL, variables from earlier lines keep
  their declared or inferred types for this.
- `trim`, `trimStart` and `trimEnd` take an optional set of characters to
  remove instead of whitespace: `"/docs/api/".trim("/")` is `"docs/api"`.
- `upper(n)` and `lower(n)` change the case of the first `n` characters, or the
  last `-n` when `n` is negative: `"geblang".upper(1)` is `"Geblang"`.
- `geblang check` reports a built-in method called with an argument count it
  does not accept, such as `text.substring()` or `xs.length(1)`, when the
  receiver's type is known.
- `geblang check` warns (`discarded-task`) when `async.sleep`, `async.all`,
  `async.race` or `async.timeout` is called without `await`, which silently
  does not wait.

### Changed

- Decimals print in a shorter default form: up to 10 places with trailing zeros
  dropped and at least one place kept, so `12.5` prints `12.5` and `7 / 7`
  prints `1.0` instead of `12.5000000000` and `1.0000000000`. This applies to
  printing, interpolation, `as string`, `toString()`, JSON, YAML, TOML,
  MessagePack and CSV output. `toString(n)` and `format(n)` still give exactly
  `n` places, and `secureRandom` draws are unaffected.
- A generator declared `generator<T>` or `iterable<T>` checks every value it
  yields against `T`. A mismatch raises a `TypeError` such as `words expects
  string yield, got int` when the value is produced, and `geblang check`
  reports mismatches it can see statically. An `int` literal yielded from a
  `generator<float>` or `generator<decimal>` widens, as it does for `return`.
  Previously yields were not checked at all.
- A predicate passed to `filter`, `find`, `findLast`, `any`, `all`, `count`,
  `partition`, `takeWhile`, `dropWhile`, `indexBy`, a callable `search`, or
  `collections.lazyFilter` must return `bool`; any other result raises a
  `TypeError` naming the method. Previously the two runtimes treated such
  results differently (`all` could return `true` on one and `false` on the
  other).
- A built binary's `--version` (and the header of `--help`) lists the app's
  name and version, then the Geblang runtime version and each bundled
  dependency package with its version, one per line. `--help` notes that
  `-- --help` shows the application's own options.
- `geblang fmt` keeps a function literal on one line only when its body is a
  single `return` that fits the width; any other body is expanded onto its own
  lines, like `if`, loop and function bodies.
- `geblang fmt` keeps columns the author aligned: consecutive declarations,
  assignments, match arms, enum backing values, and the values of a multi-line
  dict or named-argument list that were lined up stay lined up, with the
  padding recomputed so the result is the same on every run. Other extra
  spacing is still reduced to one space.
- `geblang fmt` keeps the spacing before an end-of-line comment, so comments
  aligned in a column stay aligned (`--clean` still uses one space).
- `geblang fmt` leaves a field decorator on the field's line when it was
  written there (`@immutable string id;`).
- `geblang fmt` keeps a call's leading arguments on the call line when only
  the last argument is a multi-line collection (`f(a, {` ... `});`) instead of
  putting every argument on its own line.
- `geblang fmt` writes an empty class or interface body as `{}` instead of
  splitting it over two lines.

### Fixed

- Declaring a function, variable, parameter or loop variable named `embed` is
  reported as a load error, since the name is reserved for the compile-time
  builtin. A function named `embed` used to be accepted while every call to it
  went to the builtin instead.
- `instanceof` honours nullable element types: a `list<?int>` holding `null`
  satisfies `instanceof list<?int>` and no longer satisfies `instanceof
  list<int>`, the reverse of before. The same applies to sets and dict values.
- A class, interface or enum that extends or implements a module-qualified
  type (`extends test.Test`, `implements rag.Embedder`) without importing the
  module is reported by `geblang check` and both runtimes. The VM used to
  accept it while the evaluator failed with "parent class is not declared".
- A call to a built-in module function with the wrong number of arguments is
  now rejected before the program runs on both runtimes and by `geblang
  check`; previously only the VM rejected it. Several built-in signatures were
  corrected so that every argument count the function accepts is allowed:
  `random.next`, `random.float`, `random.bool`, `random.choice` and
  `random.shuffle` take an optional generator first, `watch.wait(path,
  timeoutMs)` no longer needs a snapshot argument, `collections.range(end)`
  starts at 0, `cli.password()` and `cli.secret()` take an optional prompt,
  and `errors.new(class)` takes an optional message. `profiler.delta` rejects
  extra arguments.
- A local variable typed with a union alias (`type Id = int | string;
  Id x = 5;`) accepts any branch of the union; `geblang check` and both
  runtimes rejected it with "cannot assign int to Id x". Reassigning a
  union-typed local or parameter is now also checked by `geblang check`.
- List, set and dict comprehensions iterate classes that implement `__iter` or
  `__next`/`__done` on the evaluator (`--disable-vm`) too; they failed with
  "comprehension cannot iterate over". On the VM, a module can iterate an
  instance of a caller-defined subclass that inherits `__iter` from one of the
  module's classes; it failed with "unknown class".
- An `iterable<T>` parameter, return type or variable accepts anything a
  `for-in` loop accepts: lists, sets, ranges, dicts, strings, streams and
  classes with `__iter` or `__next`/`__done`, as well as generators.
  Previously it accepted only generators, and the VM reported the type as
  `generator<T>`. Elements of an eager collection are checked against `T`.
- A runtime error inside a generator body, such as a division by zero, reaches
  the consumer's `catch` with its own message on `geblang run` and in built
  binaries. Previously `e.message` held the whole rendered report, starting
  "uncaught RuntimeError:" and followed by a stack trace.
- A bare `return;` inside a generator ends it early instead of failing
  `geblang check`, `run` and `test` with "cannot return null". Returning a
  value from a generator is reported as an error, since the value was
  silently dropped.
- A function whose only `yield` is inside a `with` block is now a generator
  on the evaluator (`--disable-vm`) too, and a `yield` inside a `select` case
  or `default` makes a function a generator on both runtimes. Previously these
  failed with "yield can only be used inside a generator function".
- A generator that iterates another generator (a function, method, static
  method, interface default, closure or recursive call) now gets that
  generator's values on `geblang run` and in built binaries. Previously the
  inner generator's values were yielded straight out of the outer one, and the
  loop then failed with `null is not iterable`.
- `difference`, `intersection`, `differenceBy`, `intersectionBy`,
  `frequencies` and `mode` treat values of different types as different:
  `["1"].difference([1])` keeps `"1"`, where it used to match the int `1`.
- A closure created inside a comprehension, such as
  `[async.run(() => work(n)) for n in xs]`, captures the item it was created
  for on the VM, as on the evaluator. The VM gave every closure the last item.
- `geblang check` reports arguments passed to a class that declares no
  constructor (`class Money { int cents = 0; }` then `Money(5)`), which both
  runtimes reject.
- `geblang check` reports a wrong argument count on a method, imported
  function or imported constructor call even when an argument is typed `any`,
  and on assertions inherited from `test.Test` (such as
  `this.assertTrue(ok, "message")`). A test class's own helper methods are now
  checked too; previously extending `test.Test` switched these checks off.
- `list.clear(...)` rejects arguments, like `dict.clear`; it used to ignore
  them.
- The manual and editor hover describe `containsBy(value, keyFn)` and
  `indexBy(predicate)` (index of the first match, or `-1`) as they behave, and
  no longer list `take`, `lazyMap` and `lazyFilter` as list methods (they are
  `collections` functions). Editor signatures also show the optional `end` of
  `substring` and `slice`, the optional count of `replace` and the precision of
  `string.toDecimal`, and `topBy` takes the selector before the count.
- `geblang check` now examines every expression: ternaries, string
  interpolation, comprehensions, match subjects, guards and arms, `if`,
  `else if` and `while` conditions, `for` iterables, `with` and `select`
  statements, casts, `await`, ranges, spreads, pipes, parameter and field
  defaults, and interface default methods were previously skipped, so a
  misspelled method there was only caught when the code ran.
- `async.rate.throttle` measures its window with the monotonic clock, so a
  wall-clock correction can no longer make a call after the window return the
  cached result.
- On the VM, calling a function value stored in another module's exported
  variable (`export let f = func(...) {...};` then `mod.f(...)`) now works,
  with positional, named and spread arguments; it previously failed with
  "is not callable". An exported callable object (`__invoke`) can now be called
  the same way on the evaluator.
- `geblang check` now checks the returned expression in a function or function
  literal that has no declared return type; it was previously skipped.
- `geblang build --native` now supports `list.sorted(comparator)`, with a
  less-than or three-way comparator, including arrow functions.
- `geblang build --native` now builds a function literal that has no return
  type but returns a value; it previously produced a Go compile error.
- Web response middleware (`use` / `after`) now also runs on the `404` for a
  path no route matches, so its headers apply to missing pages and it can
  replace the response. Previously that `404` skipped all response middleware.
- A built binary uses only the standard library bundled inside it. It
  previously also searched `GEBLANG_STDLIB`, the binary's own directory, and
  the current directory and its parents for a `stdlib` folder, so running it
  from inside a Geblang checkout could change how its modules resolved.
- A module in a bundled dependency package keeps its package-qualified name
  even when it declares a short one (`module i18n;` in package `webkit` is
  `webkit.i18n`), as it does when run from source. Previously such a module
  could be rejected at start-up as shadowing a built-in module name.
- Passing a named argument to a method on a list, string, dict or other
  built-in value now raises the same error on both runtimes ("named arguments
  are only supported for Geblang functions and methods"); the VM previously
  reported a different message.
- Editor completion and hover now show `list.reduce(fn, initial)` with its
  arguments in the order the runtimes take them; they previously listed the
  initial value first.
- `geblang fmt` no longer deletes a comment inside a function literal that
  sits in a multi-line list, dict or argument list.
- `geblang fmt` keeps comments inside multi-line method chains, operator
  chains, collections, argument lists and match arms next to the code they
  follow, and keeps a comment after an opening brace on that line. Previously
  they were moved after the statement or into the body. When statements that
  shared a line are split, the line's comment stays with the last of them.
- `geblang fmt` keeps blank lines between comments and between a comment and
  the code after it, and keeps a comment that ends a class, interface, enum,
  function or block body inside that body instead of moving it after the
  closing brace.
- `geblang fmt` keeps comments between enum variants and after decorators in
  place.
- `geblang fmt` keeps parentheses written around any expression, including
  inside string interpolation. Previously it dropped them around literals,
  names, calls and other simple expressions, so `(2.5).round()` became
  `2.5.round()`.
- `geblang fmt` keeps a method chain the author split across lines when the
  chain includes index steps (`resp["headers"]["Set-Cookie"]` then
  `.split(";")[0]` on the next line); it previously flattened such chains.
- `geblang fmt` keeps a typed static field as written (`static int n = 0;`)
  instead of rewriting it to `static let int n = 0;`.
- `geblang fmt` no longer changes the text inside a block comment; spaces in
  `/* ... */` are kept as written.
- `geblang fmt` output is now stable when run twice: a call, chain or list
  element that follows a multi-line function literal is no longer moved onto
  a new line by the second run.

## 1.42.3

### Fixed

- On the VM, calling a bound method value (`let m = obj.hit; m();`) or a
  `reflect.method` handle now keeps the method's writes to module globals.
  Previously the writes were silently lost when the method belonged to the
  entry script, or to a module whose state was held further up the call (as in
  a `geblang build` binary), so a counter or cache updated this way never
  changed. Handlers that run isolated per request still keep their writes
  per request.
- On the VM, module-global assignments made inside an async task
  (`async.run`), an `async func` or async method, or a generator are now kept.
  Previously every such write was lost, so a counter bumped in a task was
  unchanged after `await`. Writes land when the task or generator finishes,
  so they are visible after `await` or once a generator is exhausted.
- On the VM, a dict spread in a deferred call (`defer f(...opts)`) now works for
  functions, methods and closures, ignoring keys the callee does not declare,
  as a direct call does. Previously it failed with "spread argument must be a
  list" after the function body had run.
- On the VM, named arguments and dict spreads now reach a method a class
  inherits from a class in another module (`class Sub extends lib.Base`,
  `Sub().go(b: 2, a: 1)`), and a dict spread into a function value from
  another module ignores undeclared keys. Previously these failed with
  "unknown method" or "has no parameter".
- On the VM, a spread into `io.println`, `io.print`, `io.stdoutWrite` or
  `sys.exit` is now checked like any other call. Previously
  `io.println(...["x"])` printed the list itself and `io.println(...5)` did
  not fail.
- A failing deferred call to a built-in function now keeps its error class
  (an `IOError` stays catchable as `IOError`), has no extra message prefix,
  and is reported at the `defer` statement's line on both runtimes.
- `sys.exit(code)` now runs pending deferred calls in every active function,
  innermost first, before exiting, on both runtimes. Previously the VM ran
  none, and `defer sys.exit(...)` did nothing on the VM. A deferred call that
  exits again replaces the code; one that throws replaces the exit with that
  error.
- Calling a method value with the wrong arguments now reports the method as
  `Class.member` on both runtimes, for arity, missing, unknown and repeated
  arguments, as type mismatches already did. On the VM the argument counts in
  the message no longer include the bound receiver (`expects at most 2
  arguments, got 3` instead of `3 ... got 4`).
- On the VM, spreading arguments into an overloaded function, constructor or
  static method (`pick(...xs)`, `Point(...opts)`, `Shape.make(...args)`),
  including one imported from another module or called from a `defer`, now
  selects the overload once the spread is expanded, as the evaluator does. A
  dict spread prefers the overload that drops the fewest unknown keys.
  Previously the VM rejected these calls ("cannot use spread with overloaded
  function").
- A spread call to an overloaded function is no longer rejected by type
  checking when its result is assigned to a typed variable or returned from a
  typed function (`string r = pick(...xs);`); only the return type is checked
  there, since the overload is chosen at run time.
- On the VM, a deferred constructor call (`defer Report(...)`) now works,
  with positional, named and spread arguments. Previously it failed to compile
  with "defer: unknown bytecode function".
- On the VM, calling a class value with the wrong arguments (`let make = Point;
  make();`) now raises an error whose message is just the failure, instead of
  one that contained a rendered `uncaught ...` trace.

## 1.42.2

### Added

- `net.ipSet(entries)` returns a thread-safe `net.IpSet` of IPs and CIDR
  prefixes (`add` with optional expiry, `remove`, `contains`, `size`,
  `clear`) with near constant-time lookups.
- New `ipBlocklist` option on `http.serve`, `http.listen` and `net.serve`
  takes a `net.IpSet` and drops blocked clients when they connect, before TLS
  or HTTP parsing, and again per request (using the resolved client IP behind
  `trustedProxies`).
- `re.set(patterns)` compiles many patterns into one `RegexSet` that scans
  text once; `match(text)` returns the index of the matching pattern or -1.

### Security

- `net.serve` no longer stops accepting connections after an accept error. A
  connection flood that exhausted the process's file descriptors used to stop
  the listener for good; it now backs off and keeps accepting.
- `net.serve` now closes each connection when its handler returns or throws.
  Previously a connection stayed open unless the handler closed it, so a
  client that made the handler throw leaked a file descriptor.
- Server WebSockets now have keepalive and timeouts. The server pings every 25
  seconds, a read that sees no message or pong for 60 seconds raises
  `TimeoutError`, and a send that cannot complete in 10 seconds raises
  `TimeoutError`. Previously a silent client, or one that stopped reading,
  held its handler (and any `maxConcurrent` slot) forever. Tune them with the
  new `webSocketPingIntervalMs`, `webSocketIdleTimeoutMs` and
  `webSocketWriteTimeoutMs` server options; `0` disables one.
- A WebSocket handler error is no longer sent to the client as the close
  reason. The client sees `internal error` and the error is logged to stderr.
- `http.serve` / `http.listen` now limit request bodies to 10 MiB by default
  (`maxBodyBytes`; `0` still means unlimited). Previously the default was
  unlimited and the whole body was buffered in memory.
- A request now takes its `maxConcurrent` slot after its body has arrived, so
  slow uploads can no longer occupy every slot.
- New `writeTimeoutMs` server option (default 60000): a response that makes no
  progress for that long is abandoned and the connection closed, so a client
  that stops reading cannot hold a handler and its slot forever.
- HTTP client calls now time out after 30 seconds by default (`TimeoutError`)
  and reject response bodies over 64 MiB with `IOError`. Previously a stalled
  upstream could hang a caller forever and any response size was buffered.
  Set `timeoutMs` / the new `maxResponseBytes` option on `requestWithOptions`,
  `http.newClient` or `Client.request` to change them (`0` removes the limit).
  `requestStream` keeps streaming bodies unlimited; its default timeout covers
  only connecting and the response headers.
- `llm` providers accept a `timeoutMs` option for non-streaming calls,
  defaulting to ten minutes so long generations are not cut off by the new
  30 second client default.
- `compress.gunzip`, `archive.zipRead` and `archive.tarGzRead` now stop at
  256 MiB of decompressed output by default and raise `IOError`, so a small
  compressed "bomb" cannot exhaust memory. A new optional `maxBytes` argument
  raises or (with `0`) removes the limit.
- `StreamResponse.read()` no longer holds a process-wide lock while waiting
  for data, so a stream stalled on a slow server no longer blocks every other
  response stream. `close()` from another task ends a blocked `read()` with
  `IOError`, and a streamed line over 1 MiB raises `IOError`.
- An internal runtime failure inside an async task, generator, `net.serve`
  connection, or signal, file-watch or browser callback no longer terminates
  the whole process. It surfaces as a `RuntimeError` ("internal error: ...")
  to the code awaiting that task, or is logged for callbacks.
- `readAll` and `readLines` on socket streams (`net.serve`, `net.dial`,
  `sockets`) now stop at 64 MiB in total and raise `IOError`, so a peer that
  never closes cannot exhaust memory. A new optional `maxBytes` argument
  changes the cap (`0` removes it). Files and other streams are unaffected.
  The stdout and stderr of an `ssh` session get the same line and total caps.
- Line reads on network streams (`readLine`, `readLines` and line iteration on
  `net.serve` / `net.dial` / `sockets` connections) now raise `IOError` for a
  line over 1 MiB, so a peer that never sends a newline cannot exhaust memory.

### Changed

- Passing an engine-built dict (an HTTP request, a header map) to a
  `dict<string, any>` parameter no longer rescans its keys on every call, and
  type checks for built-in types skip the type-alias lookup. Server request
  handling spends about 40% less time on runtime type checks.
- Serving a request allocates less: the per-request evaluator no longer builds
  state it immediately replaces with its parent's, and copied dicts are sized
  up front (about 7% fewer bytes allocated per request).
- VM callbacks such as server handlers reuse their worker VMs across garbage
  collections and share their call sequences, cutting a further 14% of the
  bytes allocated per request.

### Fixed

- `net.serve` now logs handler errors to stderr. Previously they were
  silently dropped.
- Editor completion and hover for `http.Client` and `http.CookieJar` now list
  the methods that exist. The `Client` entry described methods it does not
  have (`put`, `delete`, `send`, `close` and others), gave `request` the wrong
  signature, and omitted `requestAsync`, `fetchAll`, `setTimeout` and others;
  `CookieJar` listed `cookiesFor` / `setCookie` instead of `cookies` /
  `setCookies`.
- An error raised inside a string interpolation (`"${...}"`) now reports the
  line the expression is on, in uncaught traces, `e.stackTrace()` and debug
  output. Previously it often reported line 1, and the VM and the evaluator
  could disagree.
- Classes returned by native modules, `re.Pattern`, `pcre.Pattern`, and the
  new `re.RegexSet` and `net.IpSet`, can now be used as type annotations,
  parameter and return types, and with `instanceof`. Previously an annotation
  such as `re.Pattern p = re.compile("a")` failed with "re has no exported
  type Pattern".
- On the VM, native handles opened while serving a request or connection
  (`http.serve` / `http.listen` / `net.serve` handlers, WebSocket handlers,
  and tasks they start) are now closed when the request ends, as they already
  were on the evaluator. Previously they stayed open until the program
  exited, so a handler that forgot to close a socket or file leaked it on
  every request. Open long-lived resources outside the handler. On the VM, a
  `shareHandler` handler's module-variable writes now become visible to other
  requests when its call returns.
- On the VM, network failures raised by native methods (`Client.get`,
  `Builder.send`, `StreamResponse.read` and similar) are now `IOError` /
  `TimeoutError` / `TlsError`, as on the evaluator. Previously the VM raised
  `RuntimeError`, so `catch (IOError e)` missed them.
- On the VM, a server handler that throws or faults is now logged as one line,
  `http.serve: handler error: <Class>: <message>`, as on the evaluator and as
  documented. Previously the VM logged the full rendered trace under a second
  `RuntimeError` prefix. The `debug` server option renders the same uncaught
  error on both backends.
- With `--disable-vm` (and `geblang test --runtime=evaluator`), native handles
  now work across async tasks the same way as on the VM. Previously a task
  could not close or use a WebSocket, socket, trace span or other handle that
  its parent opened (closing a WebSocket from a task left the reader blocked
  forever), the parent could not use a handle a task opened, and a task's own
  connection could take the place of the parent's, so `sendText` from a task
  could deliver the message to the wrong peer.
- With `--disable-vm`, `metrics` and `trace` calls made inside async tasks and
  HTTP request handlers now show up in `metrics.snapshot()`,
  `metrics.toPrometheus()` and `trace.snapshot()`. Previously they were
  dropped.
- `geblang test --method name` now runs only the named methods on the VM, the
  default test runtime. Previously the filter applied only with
  `--runtime=evaluator`, and the VM ran every test method. The same fix
  applies to the `methods` option of `test.run`.
- WebSocket connection failures now always raise `IOError`, as other network
  failures do. Previously a peer disconnect, a message over the size limit, a
  failed handshake, use of a closed connection and a second `close()` raised
  `RuntimeError`, while a read ended by a local `close()` raised `IOError`. Code
  that caught `RuntimeError` to detect a disconnect should catch `IOError`.

## 1.42.1

### Security

- `http.serve` and `http.listen` now time out slow clients: 10 seconds to
  send request headers, 60 seconds to send the request body, and 120 seconds
  idle on a keep-alive connection. Previously there was no limit, so a client
  that stalled mid-body held a `maxConcurrent` slot indefinitely. A body that
  times out is answered with 408. Change the limits with the new
  `readHeaderTimeoutMs`, `readBodyTimeoutMs` and `idleTimeoutMs` options
  (`0` disables one); raise `readBodyTimeoutMs` for uploads that take longer
  than a minute. Handler run time, streamed responses, server-sent events and
  WebSockets are not affected.
- WebSocket servers now reject a message larger than 1 MiB by default.
  Previously a client could send a message of any size, which was read into
  memory in full. An oversize message closes the connection with status 1009
  and the handler's read raises an error. Change the limit with the new
  `maxWebSocketMessageBytes` server option (`0` disables it).

### Fixed

- `geblang fmt` keeps a blank line between the `module` / `import` lines and
  a following `const`, `let` or other statement, as it already did before a
  function or class. It was dropped, including above a doc comment.
- The compiler and `geblang check` reject repeated local declarations in one
  scope, matching the evaluator. Nested shadowing and rebinding after `del`
  remain valid; a `catch` body may shadow its catch variable, as a function
  body may shadow a parameter.
- `reflect.location` preserves the declaring module and position for function
  literals, closures, and own or inherited method values across module
  boundaries. Imported method values also resolve against the correct module
  when a local class has the same name.

- A caught error inside `finally` or a context manager's `__exit` no longer
  consumes the original exception being propagated by the bytecode VM. A new
  uncaught cleanup error and an explicit return still replace the original,
  matching the evaluator.
- On the bytecode VM, `if (x % n == 0)` on an `int` local no longer fails
  with "mod-jump fast path requires int local" when a closure has captured
  `x` or its value is outside the 64-bit range.
- On the bytecode VM, appending a string literal to a `string` local
  (`s += "..."`) works when a closure has captured `s`, including a closure
  created after earlier appends.
- On the bytecode VM, the dynamic hooks `__get`, `__set`, `__call`,
  `__getStatic`, `__setStatic` and `__callStatic` now run for a class defined
  in another module, for a subclass whose parent is in another module, and
  for an instance used from a module other than its own. Previously `__get`
  failed with "has no field", `__set` wrote a plain field instead, and a
  local class with the same name as an imported one could have its hook
  called for the imported instance. `__index`, `__setIndex` and `__string`
  inherited within another module are also found.
- `Class.name(...)` on a class that defines `__callStatic` is no longer
  rejected as "has no method" before the program runs.
- On the bytecode VM, `reflect.decorators`, `reflect.hasDecorator` and
  `reflect.decorator` accept any function value: a function passed as an
  argument or stored in a variable, a function literal, a closure, or an
  overload set. They previously failed with "expects reflect target, got
  func". Frameworks that read decorators from handler values are affected.

### Performance

- Adding two ints, a cast to the type a value already has, and `length()` on
  a short string take shorter paths on the bytecode VM. A loop that sums
  method results runs about 20% faster.
- An `int` local combined with an integer literal (`n - 1`, `n + 1`,
  `if (n < 2)`) runs as a single bytecode VM step. Recursive functions of
  that shape run about 20% faster.
- Release binaries are linked with 64-byte function alignment, which keeps
  the bytecode VM's main loop from slowing down by about 5% depending on
  where the linker happened to place it.
- Dict literals whose string-literal keys are all different skip the
  duplicate-key check while they are built.
- Casting an int to a string (`n as string`) formats the value directly on
  the bytecode VM; loops that build keys or labels this way run about 10%
  faster.
- `re` and `pcre` functions no longer build an error label on every call;
  a tight matching loop runs about 5% faster.
- The first garbage collection is deferred until the heap reaches about
  64 MB, so short programs that allocate many small values spend less time
  collecting (about 10% on a dict-building loop). Setting `GOGC` or
  `GOMEMLIMIT` turns this off. `profiler` and `profile.memStats` heap figures
  are unchanged by it.

## 1.42.0

### Standard library

- `async.waitAny(tasks, timeoutMs)` waits for a completion index without cancelling
  tasks or consuming their results.
- New `jobs` module supplies SQL queues, renewable fenced claims, bounded
  workers, durable UTC schedules, attempt history and explicit migration.
  `geblang worker --entry app.worker` runs an application-owned bootstrap.
- Broker handles offer optional managed consumption with cooperative stop and
  acknowledgement after successful handlers, while preserving existing APIs.
- A job worker survives transient store errors with backoff
  (`maxStoreFailures`, default 10), fails a job whose retry policy cannot be
  evaluated instead of stopping, and records work that returns normally during
  shutdown as completed. `Scheduler.revision(id)` returns the stored revision.
- Opening a new SQLite file from several processes at once no longer fails
  with `SQLITE_BUSY`; the first open waits for the journal-mode switch.
- `Store.update` passes the callback a deep copy of the current value, so a
  callback that mutates its argument no longer defeats the compare-and-set.
- `cron.nextAfter` and `nextN` accept `dayMatch: "and"` in an options dictionary.
- `db.Connection.driver()` reports the normalized SQL dialect, and transaction
  `begin(timeoutMs, opts)` can select an explicit isolation level.

- `image` gains drawing. `Image` has `line`, `rect`, `circle`, `ellipse`,
  `arc`, `polygon`, `paste` and `text`, all anti-aliased, plus `getPixel`,
  `setPixel` and `fill`. Drawing methods modify the image in place and
  return it, so calls chain; styles are passed as an options dict
  (`{"fill": "#3366ff", "color": "black", "width": 2}`).
- Colors are strings: `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa` or a color
  name. `image.rgba(r, g, b, a)` builds one from channel values.
- `image.blank(w, h, fill)` takes an optional fill color. New transforms
  `flipH()`, `flipV()` and `copy()` return a new image.
- Text rendering needs no font file: `regular`, `bold`, `italic` and
  `mono` faces are built in. `image.loadFont(path)` and
  `image.loadFontBytes(data)` load TrueType / OpenType fonts, and
  `image.measureText(text, opts)` returns the size text would occupy.
- New `barcode` module: `barcode.qr(data, opts)` and
  `barcode.encode(data, {"format": ...})` return an `image.Image`.
  Formats: `qr`, `datamatrix`, `aztec`, `pdf417`, `code128`, `code39`,
  `code93`, `codabar`, `ean13`, `ean8`, `upca` and `itf`, with scale,
  exact size, quiet-zone margin, color and error-correction options.
  `barcode.formats()` lists them.
- Drawing on a loaded image keeps the exact color of semi-transparent
  pixels.

### Language

- A member is a constructor only when its name matches the class name
  exactly. A method differing only by case (`box()` on class `Box`) is
  now an ordinary method; previously it joined the constructor overloads
  and calls failed with "ambiguous overload". A class that declared its
  constructor in a different case from the class name must rename it to
  match.

### Fixes

- Native class constructors and inherited methods work through module bridges.
- Native bound methods retain their receiver in callbacks. Inherited reflected
  methods and callable alias checks behave consistently across runtimes.
- `store.Store.update` isolates mutable callback snapshots before compare-and-set,
  preventing endless retries when callbacks mutate nested collections.
- Evaluator async tasks share broker handle ownership with their parent.

- On the bytecode VM, an error raised by a native function behind a
  standard library class (for example `image.blank(4, 4).rotate(45)`)
  now reports its real message instead of "module ... has no export ...".
- A subclass calling `parent(...)` on a class from another module reaches
  the constructor when that class also has a method named like the class
  in a different case.

Older releases (1.20.0 through 1.41.4) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
