# Release Notes

## 1.40.1

### Added

- `string.byteLength()` counts stored UTF-8 bytes without a bytes conversion.
- `cli.promptDecimal` shares nullable typed-prompt validation, defaults and
  cancellation with `promptInt`, using exact decimal parsing.
- `money.sum` totals same-currency values in integer minor units; empty input
  requires an explicit currency.
- `json.write(value, stream)` writes incrementally, retries short writes and
  leaves the borrowed stream open and unflushed. Formatting matches stringify.
- Optional `MemoryCacheStore(ttl, capacity)` capacity enables LRU eviction and
  expiry reclamation while preserving shared state and JSON snapshot isolation.
- LSP completion/signature help and VS Code snippets cover the added APIs;
  primitive method signature help now uses the shared method catalog.
- The bytecode VM runs non-literal parameter defaults, instance field defaults
  and static initializers, which previously required the evaluator. Defaults
  run on every call or new instance and see earlier parameters, captured
  variables and `this` (parameters) or the declaring module (fields).

### Fixes

- Imported ancestor fields retain literal defaults even without constructors.
  Mutable collection defaults remain independent between instances.
- Computed parameter defaults are type-checked like supplied arguments; the
  evaluator previously accepted a value of the wrong type.
- Errors inside a parameter default are reported from the called function;
  errors inside a field default are reported under the class name followed by
  the line that created the instance.
- Traces after a call returns report the caller's line instead of the last
  line run inside the callee.
- Reading a global before its declaration runs reports `"name" is not declared`
  on the VM, matching the evaluator.
- `geblang check` type-checks a parameter default against the names in scope
  at that point, not against the parameter itself.
- An exception raised while importing a module keeps its class and message on
  both backends and can be caught around the `import`; traces show the module
  as `<module NAME>`. The evaluator previously replaced it with a generic
  import error and the VM wrapped runtime faults twice. `sys.exit` during an
  import now exits on the evaluator too, and module rejections read
  `module NAME: ...` on both backends.
- A class without a constructor runs the nearest ancestor constructor that
  takes no arguments, including through intermediate classes without
  constructors and across modules. Previously such classes skipped the parent
  constructor, and the VM skipped it for an empty child constructor whose
  parent was in another module.
- Traces for an automatic parent constructor call include the child
  constructor's frame on both backends.
- Release dead VM operand/local references after returns and exception unwinding,
  preserving escaped closure cells. Evaluator call/defer frames also release
  completed environments and arguments. Trim unusually large idle worker stacks.
- JSON encoding rejects mixed-key dictionaries and cyclic values with catchable
  errors. Parsing and encoding limit container nesting to 512 levels.
- Native router state is locked on the shared app rather than a callback
  evaluator, fixing races between async registration and dispatch.
- Unpaired JSON Unicode surrogates no longer swallow the next escape.
- Checked casts retain nested collection and reified class arguments, including
  nullable list shorthand, aliases, inherited generic bases and module identity.
  A same-named class from a different module no longer satisfies a checked cast.
- Explicit generic arguments survive imported and stored function calls, named
  and spread calls, async/generator workers and executable decorator forwarding.
  Overload selection uses each candidate's declared type parameters. Failed
  spread expansion cannot bind the next call accidentally.
- Isolated request snapshots preserve class/interface declaring-module identity
  and generic parent arguments. Raw native stream handles retain qualified
  casts consistently across both runtimes.

### Performance

- Escaped JSON tokens grow their own buffers instead of reserving the remainder
  of the document. Encoder pools clear references and discard oversized buffers.
- Compact dictionaries allocate storage for their actual small-object size;
  spilled dictionaries release their compact storage. JSON builds small objects
  at their final size.
- Dictionary key checks use mutation-aware shared metadata; `any` collection
  elements need no repeated structural walks. Concrete element checks remain.
- Native route dispatch captures append-only index/middleware prefixes under an
  app lock and uses precompiled path segments, preserving first-match order and
  the parameter dictionary's existing key order.
- Character indexing reuses immutable ASCII values and single-character Unicode
  metadata. Small JSON strings from large documents own their storage.

## 1.40.0

### Added

- Source stdlib `money` supplies immutable currency values backed by arbitrary-size
  integer minor units. Exact construction, currency-checked arithmetic, explicit
  rounding, total-preserving allocation and exact JSON serialization share the
  existing locale currency data.
- `locale.currencyInfo(code)` returns a canonical currency code and its standard
  fraction digits.
- `geblang test --jobs N` runs files in isolated worker processes. Output remains
  in discovery order, failures are aggregated, and the default is one worker.

### Performance

- String-key dictionary reads avoid allocating an encoded key, including
  indexing, membership, primitive methods and native option lookups.
- Test preparation validates the merged source and generated runner in one
  semantic pass. Files without selected tests still receive validation.

- Embedded source stdlib resolution reuses the executable's content digest
  within a process, retaining version/content cache invalidation.

### Fixes

- Unclosed declaration and control-flow bodies report a syntax error at EOF.
- Method values inherited through a local subclass of an imported ancestor
  resolve to the ancestor's module before magic property fallback.
- Bound method values accept named arguments and omitted defaults, including
  methods inherited from another module.
- Omitted collection defaults are fresh for each named call; explicitly supplied
  collections retain their identity even when equal to a declared default.
- Variadic method calls check each element's type, including the receiver's
  concrete generic bindings in direct and stored-method calls.
- Local descendants acquire concrete type bindings from imported ancestors even
  when the descendant has no type parameters of its own.
- Parallel workers use the divergence ledger selected for the parent invocation.

## 1.39.0

### Added

- Nullable `cli.readLine` and `cli.promptInt` distinguish EOF and cancellation
  from valid input. Typed prompts retry invalid input and support defaults,
  bounds, choices and custom validation.
- Shared validation rules for `cli.parseArgs` and fluent
  `cli.command.Option` builders.
- `cli.withContext` scopes input, output, prompt suppression and terminal
  detection, restoring the previous context after success or failure.
- `cli.widgets.withProgress` provides throttled terminal progress, guaranteed
  display cleanup and cooperative cancellation through `CancelledError`.
- Table column descriptors support alignment, format specifications and
  typed record accessors. Strict tables reject missing keys.
- String `toIntOrNull`, `toDecimalOrNull` and `toFloatOrNull` methods return
  null for invalid numeric text.
- Session-free `secureRandom.bool()` and `secureRandom.choice(items)` use
  cryptographic randomness alongside the existing audited overloads.

### Fixes

- CLI and IO readers share buffered stdin without losing piped input.
  Nested input scopes restore unread outer input after exceptions.
- Confirmation and choice prompts retry invalid answers and raise `IOError`
  at EOF. Blank confirmation uses its default, false when omitted.
- Progress rendering rejects invalid widths and handles very large task
  counts without integer overflow.
- Formatting preserves indentation for nested multi-line callbacks.
- Assigning a local subclass to an imported ancestor type follows the
  source module's parent chain without loading or executing it.
- Native named arguments select the matching overload, preserving audited
  RNG calls alongside their session-free forms.

## 1.38.1

### Fixes

- Returning from `finally` overrides a pending return without crashing the
  compiler or leaving extra values on the VM stack. Breaking or continuing
  an inner loop does not run an enclosing finalizer early.
- Nested named functions retain captured locals after their enclosing call
  returns, including recursive functions and captures through another lambda.
  Decorated functions retain their declared argument names, defaults and
  overload types on both runtimes. Closure capture respects block and
  comprehension scope, including a loop binding that shadows its iterable.
- Async calls made through callbacks no longer share instruction storage
  that a reused worker can rewrite concurrently. Socket callbacks do not read
  temporary execution state from their running parent VM.
- Idle VM workers release completed call arguments and stale global
  snapshots instead of keeping them alive between calls.
- List loops observe element replacements, including after a list grows,
  and stop at the original length. Appending does not extend the loop.
- Explicit `null` range starts and steps are rejected on both runtimes.
  Character ranges evaluate an explicit `by` expression once on both.
- Debugger breakpoints in imported functions and inherited methods stop
  in their defining files, and caller frames keep their own source paths.
- Editor rename respects lexical bindings and leaves plain strings,
  comments and unrelated variables unchanged. Navigation, rename,
  completion and diagnostic positions account for UTF-16 text positions.
- The language server exits on an `exit` notification without waiting for
  its input to close, and reports the current toolchain version.
- Sampling deduplicates workers running on the same goroutine. A profile
  report ending in `.folded` keeps a separate folded-stack output file.
  Starting a collection is safe while cancelled workers finish.

### Performance

- HTTP callback contexts construct builtin function tables only when they
  need them, reducing request setup allocations while preserving isolation.

## 1.38.0

### New

- The `browser` module can capture more than the visible viewport.
  `screenshot(path, opts)` takes one element (`selector`), a `clip` region or
  the `fullPage`, in `png`, `jpeg` or `webp` with a `quality`, and can make the
  background transparent. `screenshotBytes(opts)` returns the image as bytes.
  `pdf(path, opts)` takes page size, margins, orientation, scale and page
  ranges. `startScreencast(dir, opts)` and `stopScreencast()` record the page as
  numbered frames.
- Visual regression checks: `browser.compareImages(a, b, opts)` compares two
  PNG, JPEG or WebP images (paths or bytes) pixel by pixel with a per-pixel
  `threshold`, and can write a highlighted difference image.
  `page.matchScreenshot(baseline, opts)` compares a screenshot with a stored
  baseline, creating or updating it as asked.
- Viewport and device emulation: a `viewport` option on `browser.launch`, and
  `setViewport`, `setUserAgent`, `emulateMedia` (print media, dark colour
  scheme, reduced motion), `setTimezone`, `setLocale`, `setOffline`,
  `setExtraHeaders` and `emulateFocus` on pages.
- Waiting: `waitForFunction(js, opts)` waits until a JavaScript expression is
  truthy and returns its value; `waitFor` takes a `state` (`attached`,
  `visible`, `hidden`, `detached`); `goto`, `reload`, `goBack`, `goForward` and
  the new `waitForNavigation` take `waitUntil` (`load`, `domcontentloaded`,
  `networkidle`); `waitForRequest` and `waitForResponse` (with the response
  body) wait for network traffic, and `requests()` lists it. Every wait takes
  `timeoutMs` and raises `TimeoutError`.
- `consoleMessages()`, `pageErrors()` and `clearConsole()` expose what a page
  logged and threw. `onDialog` and `dialogs()` answer and record `alert`,
  `confirm` and `prompt` dialogs.
- Downloads: `setDownloadPath(dir)` and `waitForDownload()`, which names the
  file after the server's suggested name.
- Input by coordinates for canvas apps and games: `mouseMove`, `mouseDown`,
  `mouseUp`, `mouseClick`, `drag` and `wheel`, plus `keyDown`, `keyUp`, key
  combinations such as `press("Control+A")`, `hover`, `dblclick`, `focus`,
  `check`, `uncheck`, `setInputFiles`, and `button`, `clickCount` and
  `position` options on `click`.
- Element queries: `count`, `isVisible`, `isEnabled`, `texts`, `innerHTML`,
  `inputValue` and `boundingBox`.
- `browser.newContext(opts)` returns a `Context` with its own cookies and
  storage, so several users can run side by side in one browser. Its
  `viewport`, `userAgent`, `locale` and `timezone` apply to each page it opens.
- `addInitScript`, `setContent` and `bringToFront` on pages.
- `geblang run --profile <report>` (or `GEBLANG_PROFILE=<report>`, which also
  works for built binaries and servers) samples a program's Geblang call stacks
  about once a millisecond, across modules and `async` tasks, and on exit
  writes the functions and lines it spent its time in, plus folded stacks for
  flame-graph tools. Both backends support it.
- `sys.cpuCount()` returns how many CPUs the process can run code on at once,
  for sizing a pool of CPU-bound `async` tasks.
- `sys.setGcPercent(percent)` sets the garbage collector's target (Go's
  `GOGC`) and returns the previous one, so a CPU-bound batch job can trade
  memory for fewer collections.

### Changes

- `goto` and `reload` now wait for the page's `load` event by default, report
  a navigation the browser could not complete as an error, and raise
  `TimeoutError` when the page does not load in time. They previously returned
  once the document was interactive, and silently after 30 seconds.
- `waitFor` raises `TimeoutError` (a subclass of `Error`) when the selector does
  not appear in time.
- Dialogs no longer block a page: `alert`, `confirm` and `prompt` are dismissed
  unless `onDialog("accept")` was called, and `beforeunload` is accepted.
- `press` sends proper key codes for named keys and rejects unknown key names.
- Less garbage for the collector: cross-module calls reuse their worker VMs
  (they were rebuilt after every collection), a `secureRandom` session keys
  its HMAC once instead of on every draw (outputs and verification are
  unchanged), and a `for` loop over a range literal counts without building a
  range value.
- Calls into another module no longer take one process-wide lock. Calls
  that read a module's globals share a per-module read lock, and a call that
  reassigned none takes no lock on return, so CPU-bound `async` tasks calling
  into the same modules scale better across cores (about 20% more throughput
  with 12 tasks in one benchmark).

### Fixes

- `list.slice()` returns a new list. It shared the original's storage, so
  assigning to an element of the slice changed the original, and pushing onto
  it could overwrite the original's next element.
- A call to a function in another module can name a later argument and leave
  defaulted parameters before it out (`lib.run(1, workers: 4)`). The VM
  reported "missing argument" for the skipped parameter; the evaluator was
  already correct.

Older releases (1.0.0 through 1.37.8) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
