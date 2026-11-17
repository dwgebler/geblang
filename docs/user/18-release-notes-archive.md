# Release Notes Archive

Release notes for versions 1.20.0 through 1.41.4, kept for history. These
are not part of the published documentation site. For current
releases see the [release notes](18-release-notes.md).

## 1.41.4

### Performance

- Dictionaries avoid an extra allocation when passed, returned or stored
  inside another collection. Typed and frozen views retain their existing
  sharing behavior.

- Dict literals whose keys are all string literals (`{"a": i, "b": i + 1}`)
  build about 25% faster and allocate about half as many objects: keys are
  prepared at compile time and small dicts take one allocation instead of
  two.
- `length()`, `isEmpty()`, `get(key)` and `contains(x)` on strings, lists,
  dicts, sets and bytes run inline on the bytecode VM, other method calls
  with no arguments on those types no longer allocate, and a cast to the
  type a value already has (`x as int` on an int) skips the conversion. A
  loop of such calls runs about twice as fast.
- On the bytecode VM, `length()`, `isEmpty()`, `get(key)` and `contains(x)`
  called on a variable read the receiver directly instead of pushing it
  first: about 8% faster on a loop of such calls.
- Repeated casts to primitive types run faster on the bytecode VM while
  preserving aliases and generic bindings.
- The tree-walking evaluator does integer arithmetic, `++` and `--` without
  allocating while values fit in 64 bits, and skips re-resolving a cast's
  type when the value already has it: about twice as fast on method-heavy
  loops, 30% on dict literals and 20 to 30% on integer loops.
- Bytecode caches from earlier versions are recompiled on first run.

- Evaluator local scopes skip mutex operations until captured, reducing
  method-call, dictionary-literal and numeric-loop overhead. Captured scopes
  and shared module/session state retain synchronization.

### Fixes

- Closures returning a captured generic type now use that binding on the VM,
  including closures returned by inherited generic methods. Incorrect
  return values still raise `TypeError`.
- Named nested functions retain enclosing generic bindings on the evaluator.
  Generic function bodies also keep their type parameters during analysis.
- A captured generic type inside a collection or union in a closure's
  signature (`func(): list<T>`, `func(list<T> xs)`, `dict<U, T>`, `T[]`,
  `list<T> | string`) now uses the enclosing binding on both runtimes;
  valid values were rejected before.
- Function literal parameters now check collection element types on the
  bytecode VM (`func(list<int> xs)` given `["x"]` raises `TypeError`), as
  named functions and the tree-walking evaluator already did.
- A generic function nested inside an imported module's function no longer
  rejects arguments for its own type parameters on the bytecode VM.
- On the tree-walking evaluator, a function declared `func f<T>(T c): T[]`
  called with an explicit binding (`f<int>(1)`) no longer rejects its
  return value.
- A bound type parameter is now checked at every depth on both runtimes:
  `func wrap<T>(any x): list<T>` called as `wrap<int>("x")`, or
  `Box<int>.items()` returning `list<T>`, raises `TypeError`. Element types of
  generic collections were previously unchecked. Bindings inferred only from
  arguments are still not enforced.
- `int n = id(5);` and `return id(5);` from a function returning `int` no
  longer fail `geblang check` and startup with `no matching overload` when
  `id` infers its type argument. Explicit type arguments that name a common
  parent (`both<Animal>(dog, cat)`) are no longer reported as conflicting, and
  an `any` argument in a typed context is left to the runtime check.
- Function and class values print the same on both runtimes: `<func name>`,
  `<func>` for a function literal and `<class Name>`. The bytecode VM printed
  `<closure name>`, `<bytecode class module.Name>` and
  `<overloaded func name>` for a single function from another module.
- A return `TypeError` from a method called through a bound method value
  names `Class.method` on the tree-walking evaluator, as on the VM.
- An interface can be used as a value on the bytecode VM (`let s = Shape;`),
  as on the tree-walking evaluator; interface values compare equal to the same
  interface across modules.
- On the bytecode VM, a function name used as a value must match the declared
  case: `Helper` no longer resolves to a function `helper`.
- Parameter `TypeError` messages and `reflect.parameters` types spell the
  declared type the same way on both runtimes: aliases resolved, `int[]` kept
  as written and `dict<string, int>` with its spacing. A union parameter no
  longer reports an element detail on the bytecode VM.
- Typed variables are checked at runtime when the compiler cannot prove the
  value's type: `string s = raw(5)` where `raw` returns `any` raises
  `TypeError` on both runtimes, as do reassignment (`s = raw(5)`, `i += x`),
  reassigning a typed parameter and typed loop variables
  (`for (string n in raw([1]))`). Previously only collection declarations
  were checked. Values the compiler can prove are not re-checked.
- A built-in error such as `RuntimeError` or `TimeoutError` is accepted by a
  parameter or variable typed with a built-in ancestor (`Error`, `IOError`) on
  both runtimes. It was rejected, which also broke `async.retry` and
  `async.any` once typed variables were checked.
- Reassigning an integer literal to a `float` or `decimal` variable
  (`f = 5`) stores a `float` or `decimal`, as the declaration form does; it
  stored an `int` before.
- A declaration using a type parameter (`list<T> y = [x]` inside a generic
  function or method) no longer raises `TypeError` for valid values. Type
  parameters in variable declarations are not checked at runtime.
- A closure or nested function inside a generic function or method no longer
  rejects valid values when the enclosing type parameter is unbound, in
  returns, parameters and declarations. Bound type parameters are still
  enforced.
- A failed `int[]` declaration names `int[]` on the bytecode VM, as on the
  tree-walking evaluator; it said `list<int>`.
- An expression inside `${...}` that fails to parse is reported as a
  compile-time error on both runtimes. It previously failed at runtime,
  printed a partial value (`${x )}`) or crashed the bytecode compiler (an
  unparenthesised function literal, whose return-type `:` starts a format
  spec).

- Self-referential lists and dictionaries passed to stateful native
  functions, including `store.Store.set`, no longer overflow the VM's stack.
  Shared entries and callbacks nested inside cycles remain usable.
- Socket and task callbacks no longer read or consume the parent VM's
  pending generic bindings while it constructs another value.
- Explicit generic calls such as `identity<int>(17)` are checked using the
  bound signature when a declaration or callback expects an `int` result.

- Calling a string, list, dict, set or bytes method with the wrong number of
  arguments gave a message without the receiver type on the bytecode VM
  (`contains expects one argument`); it now names it, as the tree-walking
  evaluator does (`string.contains expects one argument`). `isEmpty` with
  arguments no longer reports itself as `length`.
- `dict.clear(...)` with arguments was silently accepted on the bytecode VM;
  it now raises `dict.clear expects no arguments` on both runtimes.
- `-1 * x` where `x` is the smallest int (-9223372036854775808) returned
  -9223372036854775808 on the bytecode VM instead of 9223372036854775808.
- `get(i)` on a list, string or bytes with an out-of-range index threw a
  `ValueError` on the tree-walking evaluator but returned `null` on the
  bytecode VM. Both now return `null`; indexing with `x[i]` still throws.
- Calling a method a value does not have now reads `unknown method
  <type>.<name>` on both runtimes; the bytecode VM said `null has no method
  length` or `closure does not have method x` instead.
- On the tree-walking evaluator, unary `-`, `~`, `++` and `--` rejected ints
  returned by built-in methods (for example `-s.length()` raised
  `- expects numeric value, got int`), as did `math.abs` and `.abs()` of a
  negated one. They now work as on the bytecode VM.

## 1.41.3

### Added

- `reflect.parameters` and `reflect.constructors` entries gain `class` and
  `elementClass`: the class a parameter's type names (`Widget`, `?Widget`,
  `geo.Widget`, `Box<int>`), and the element class of `list<T>`, `set<T>`
  or `T[]`, resolved in the module that declares the function. Import
  aliases, `from` imports and unexported classes resolve even when the
  caller is another module, so code that binds or documents typed
  parameters no longer has to look a type up by its string.
- `reflect.fields` entries gain the same `class` and `elementClass`, and
  `reflect.fields(cls, inherited: true)` lists inherited fields after the
  class's own, nearest ancestor first, each tagged with `declaringClass`.
  The default result is unchanged apart from the two new keys.
- `reflect.returnClass(fn)` returns `{class, elementClass}` for a function's
  declared return type, resolved the same way.
- `reflect.class("module.Class")` finds a class by its module's canonical
  name, exported or not, even from a module that never imported it.
- `reflect.parentClass(cls)` returns the parent class itself, resolved in the
  child's module, or `null` for no parent or a built-in parent such as
  `Error`.
- `xml.parseAs` maps elements onto class fields: child elements and
  attributes fill same-named fields converted to their declared types,
  nested elements fill class fields and repeated elements fill lists.
  `xml.stringify` accepts a class instance and writes it in the same shape,
  so the two round-trip. Previously `xml.parseAs` left every field `null`.

### Performance

- Building a string with repeated `s = s + x` (or `+=`, or interpolation)
  was quadratic on both runtimes: every append copied the whole string.
  Appends now reuse spare buffer capacity, so such loops are linear; 50,000
  appends went from 3.2s to 31ms.
- Deleting dict keys no longer shifts the key order on every delete:
  removing 40,000 keys went from 527ms to 62ms. Iteration order is unchanged.
- Calling methods on strings, lists, dicts, sets and bytes is about 40%
  faster, and dict literals about 30% faster.
- `reflect.fields` caches each class's field metadata: an inherited lookup is
  about 2.4x faster and allocates about 80% less. Each call still returns
  fresh dicts.
- Smaller savings: field writes on classes without field decorators,
  checked casts, string-key dict lookups for keys over 32 bytes, startup (the
  HTML sanitiser policy is built on first use), and the memory held by idle
  cross-module worker VMs.
- Programs that call between modules or hand callbacks to native functions
  at a high rate, such as HTTP servers, do less work per call. A pooled
  worker reuses its copy of top-level values that have not changed instead
  of recopying them, per-request runtime state is created only when used,
  and decorator tables are shared instead of copied. On a small JSON HTTP
  endpoint this served 15 to 20% more requests per second, with about a
  third lower p99 latency and about 18% lower peak memory.
- The deployment guide now gives measured `GOGC` / `GOMEMLIMIT` trade-offs
  for HTTP servers.

### Fixes

- `re.compile` and `pcre.compile` kept every compiled pattern for the life of
  the process, so a program compiling patterns repeatedly grew without bound
  (about 210 bytes per compile). A pattern is now freed with its `Pattern`
  value.
- `json.parseAs`, `yaml.parseAs` and `toml.parseAs` left every inherited
  field `null` when the target was a subclass. Inherited fields, including
  those declared in another module, are now filled and type-checked like
  the class's own.
- `http.jsonResponse`, `http.postJson` and structured log fields rejected
  class instances with `json.stringify does not support X`. They now encode
  exactly as `json.stringify` does: public fields, inherited included, or
  the `__serialize()` result. Output for other values is unchanged.
- `json.stringify` escaped `<`, `>` and `&` in the string fields of class
  instances (`\u003cb\u003e`) but not in dicts, lists or plain strings.
  Instance fields are now written as-is too.
- On the bytecode VM, `reflect.returnType` dropped type arguments
  (`list` for `list<int>`, `?Box` for `?Box<int>`) on functions, lambdas,
  methods and static methods. It now reports the full type, as the
  evaluator always did.
- On the bytecode VM, `reflect.class` with a name string whose prefix is an
  import alias (`let n = "geo.Widget"; reflect.class(n)`) returned `null`;
  it now resolves to the aliased module's export, as on the evaluator.
- A bare `reflect.class("Widget")` that several modules could satisfy picked
  the last-declared class on the evaluator and an arbitrary one on the
  bytecode VM. Both now look in the calling module, then the entry script,
  then the other modules in alphabetical order.
- `parseAs` resolved a nested field's class by its bare name, so a field
  typed `geo.Widget` could be filled with another module's `Widget`. Nested
  fields now use the class the declaring module names.

## 1.41.2

### Fixes

- A module-qualified type annotation naming a class, interface or enum that
  its module declares without `export` (`lib.Box` in a parameter, a local or
  a `type` alias) was rejected with `lib has no exported type Box` by
  `geblang check`, `geblang run`, `geblang test` and `geblang build`, a
  regression in 1.41.1. Such types are accepted again; calling or importing
  an unexported name (`lib.Box()`, `from lib import Box`) is still an error.

## 1.41.1

### Added

- `secureRandom.randomInts(s, count, min, max)` draws `count` unbiased ints
  in `[min, max]` as one audited draw: one nonce and one audit-log entry whose
  output is the whole list, which `replay` reproduces. A round that needs many
  values (a reel feature placing 150 symbols, say) pays the per-draw cost of
  building and logging a message once instead of for every value.
- `geblang run --go-profile <cpu.pprof>` (or `GEBLANG_GO_PROFILE`) profiles
  the engine itself: it writes Go's CPU profile, and lock-contention and heap
  profiles beside it, when the program exits, for `go tool pprof`. Where
  `--profile` shows which Geblang code is slow, this shows time going to
  garbage collection, memory or contention between workers.

### Performance

- A call into another module's code (a method on an object from another
  module, a constructor, a module function) runs on a pooled VM, and handing
  it back cleared the VM's whole stack, locals and frame capacity, about
  90 KB, however little the call used. It now clears only what the call
  wrote. Cross-module-heavy code runs faster, most of all on several
  workers at once, where the clearing saturated memory bandwidth: a slot
  simulation on 6 workers ran 67% faster.

### Fixes

- `geblang check` (and the language server) treated every top-level
  declaration as exported, so `lib.Helper()` passed the check when `Helper`
  lacked `export`, and both runtimes then failed with `module lib has no
  export Helper`. The checker now counts only `export` declarations, for
  members, `from` imports and qualified types alike.
- On the evaluator, a method overriding a parent class's method from another
  module no longer becomes a second overload when it names a parameter type
  through the import alias (`base.State` in the subclass, `State` in the
  parent's module). Calls to it failed with `ambiguous overload`; the
  bytecode VM already treated it as the override.

## 1.41.0

### Added

- Opt-in project workspaces: list packages with `workspace.members` and
  reference them explicitly with `workspace: true` dependencies. Existing
  package layouts, source paths, imports and path/Git dependencies remain
  supported.
- `geblang install --workspace` installs member and transitive dependencies,
  retaining each package's vendor directory and lockfile.
- `geblang resolve` reports an import's source file or built-in provider.
  `--explain` shows candidate paths and manifests, `--from` chooses the lookup
  context, and `--strict` opts into failure on competing providers.

### Fixes

- Dependencies are traversed in alphabetical key order. Previously, an
  ambiguous import could select different providers on repeated lookups.
- Import-check diagnostics include the underlying resolution error, including
  workspace configuration problems.
- Database `Rows` no longer accumulate for the life of the process. Every
  query result stayed registered with its cached rows until the program
  exited, closed or not, so a long-running server's memory grew with each
  query (about 14 KB each for a small select). A `Rows` value that is
  closed, read to the end or no longer referenced is now freed, and one
  dropped part-way through is closed and returns its connection to the pool.
- A failed checked collection cast now says what failed and where, on both
  runtimes: `cannot cast dict to dict<string, any>: key 0 is int, not
  string`, or `: ["stats"][2] is string, not int` for a nested element.
  It previously said only `cannot cast dict to dict<string, any>`.

## 1.40.4

### Fixes

- On the VM, an interface that extends an interface from a module imported
  under an alias (`import lib.base as basemod;` then
  `interface Contract extends basemod.Contract {}`) lost that parent when used
  from other modules. A class implementing it did not match the parent in
  `instanceof`, and casting it to the parent failed with `cannot cast`. The
  evaluator was not affected.
- On the VM, `reflect.interfaceParents` returns bare parent names for a
  qualified parent, as the evaluator does.
- On the VM, `reflect.interfaceParents`, `reflect.interfaceMethods` and
  `reflect.doc` accept an interface imported from another module instead of
  failing with `expects interface, got interface`.

## 1.40.3

### Performance

- Recovers most of the performance lost in 1.40.1. Releasing references held
  by popped values and returned call frames no longer clears memory in bulk on
  every instruction, and values that hold no reference skip the release, so
  call-heavy and arithmetic code is close to 1.40.0 speed again. Memory is
  still released as in 1.40.1.
- JSON encoding runs its cycle and nesting-depth checks only on lists,
  dictionaries and objects and compares ancestors by identity directly,
  recovering most of the encoding time added in 1.40.1. The checks themselves
  are unchanged.
- Casts to `string`, `int`, `decimal`, `float` and `bool` of values other than
  objects and errors take a direct path, removing the per-cast overhead added
  in 1.40.1. Results and errors are unchanged.

## 1.40.2

### Fixes

- Regression in 1.40.1: on the evaluator, a qualified type in a cast (such as
  `value as cfg.Weights`) resolved its module alias from the most recent import
  of that alias anywhere in the program instead of from its own module's
  import. When two modules imported different modules under the same alias,
  the cast failed with `cannot cast`. The VM was not affected.

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

## 1.37.8

### Fixes

- `geblang fmt` keeps the final `else` of an `if` / `else if` chain on the
  line of the closing brace (`} else {`). It moved the `else` to a line of
  its own, indented by one space.
- `geblang fmt` writes an exported type alias with one semicolon
  (`export type W = m.Weights;`). It added a second semicolon.

## 1.37.7

### Fixes

- A collection typed with an interface from another module (for example
  `list<shapes.Shape>`, `dict<string, shapes.Shape>` or `set<shapes.Shape>`)
  accepts objects whose class implements that interface. On the bytecode VM,
  `push`, index assignment and `add` refused every implementing class with
  `cannot push Square to list<shapes.Shape>`, with or without an import alias;
  a list literal or a variable declared with the interface type already
  worked.
- `geblang check` no longer reports a local class that implements an imported
  interface (`class Tri implements shapes.Shape`) as incompatible where that
  interface is expected. A class that does not declare the interface is still
  an error.
- `geblang check` rejects arithmetic and bitwise operators on lists, dicts,
  and sets before runtime. For list concatenation, copy the first list and
  call `extend` on the copy; class-defined operator overloads remain valid.
- `geblang check` rejects method-call syntax on callable fields. Call the
  field value as `(obj.field)(...)` for the same result on both backends.

## 1.37.6

### New

- Database connections accept an optional transaction deadline with
  `begin(timeoutMs)`. `execWithTimeout` and `queryWithTimeout` also bound pool
  waits; streamed query rows retain their deadline until closed or exhausted.
  SQLite lock waits can still last until the configured busy timeout because
  the underlying driver does not interrupt a busy handler promptly.

## 1.37.5

### New

- `set<T> s = {};` declares an empty set of `T`, as documented. An empty
  `{}` takes a set type when the declaration (variable, parameter default or
  class field) is a set; on its own `{}` is still an empty dict. It was
  rejected with `cannot assign dict to set<T>`.

- `store.Store` hands out deeply frozen values (`freeze.deep`) by reference
  instead of copying them on every `get`, so one large read-only structure can
  be shared cheaply by every request or task. Other values are still copied in
  and out.
- On the bytecode VM, `for (i in a..b)` loops over ranges that fit in 64 bits
  count without allocating, and arithmetic on the loop variable takes the
  small-integer fast path; such loops run about three times faster.
- On the bytecode VM, constructing an instance of a class from a multi-module
  program reuses the class's runtime description instead of rebuilding it for
  every object.
- On the bytecode VM, a call into another module tracks the module globals it
  writes instead of scanning every global slot afterwards, which takes about
  a third off the fixed cost of each cross-module call.

### Fixes

- `async.rate.debounce` waits for its quiet window before calling the
  function, `async.tasks.retry` waits its backoff delay between attempts, and
  the RabbitMQ transport's `receive(timeout)` and subscription loop pause
  between polls. Each called `async.sleep` without awaiting the task it
  returns, so it did not wait at all (debounce only appeared to work when the
  calls were scheduled in order, retry did not back off, and the subscription
  loop kept a CPU core busy).
- Native handles are freed with the last reference to them: `secureRandom`
  sessions (with their audit logs), `store.Store`, `random.Generator`,
  `async.channel` channels, `async.sync` mutexes, semaphores and wait groups,
  `async.atomic` values, images and HNSW indexes. Each used to stay in memory
  until the process exited, so a server opening, for example, a
  `secureRandom` session per game round grew without bound.
- The agent guide (`docs/user/AGENTS.md`) and the bundled Claude skill show
  `async.timeout(task, ms)` (the skill had the arguments reversed), say that
  `async.sleep` must be awaited, that `type` is a keyword, and how to share
  deep-frozen data between tasks.
- `freeze.deep` keeps a dict's insertion order and the element types of typed
  lists, dicts and sets; frozen dicts came back with their keys sorted.
- A frozen value stored in a `store.Store` stays frozen when read back
  (`get`, `getOrSet`, `values`); the copy was mutable.
- Copying a class instance (`deepCopy`, `store.Store`) keeps its generic type
  bindings and its extra `instanceof` types.
- Assigning a frozen list to a typed variable (`list<int> xs = frozen`) no
  longer writes the new element type onto the frozen list itself, which could
  race when the list was shared between tasks.

## 1.37.4

### New

- SQLite connections accept `txLock` (`"deferred"`, `"immediate"` or
  `"exclusive"`), which sets how `begin()` starts a transaction.
  `"immediate"` takes the write lock up front and waits on the busy timeout,
  so read-then-write transactions from several connections no longer fail
  with `database is locked`.
- `geblang check` checks calls into your own modules (`mod.f(...)`,
  `mod.Class(...)` and `from mod import f` names) against the called
  function's or constructor's parameters, reporting a wrong argument count or
  a built-in-typed argument that can't match (for example an `int` variable for
  a `decimal` parameter).

### Fixes

- `resources.path` and `resources.exists` accept a declared resource directory
  (or a directory inside one), in source runs and built binaries, so a
  directory such as `templates` can be handed to an API that takes a path.

- A class method or function can call a function declared later in the same
  file without passing its defaulted parameters; this was rejected with
  `no matching overload`.
- `geblang check` (and compilation) rejects a named argument that is not a
  parameter of the stdlib function being called, such as
  `json.stringify(value, indent: 2)`; it previously failed only at run time.
- `geblang check` accepts a call to a function with a union-typed parameter
  (`string|int`) where the call's result is returned or assigned; it reported
  `no matching overload`.
- `geblang check` (and compilation) rejects a stdlib call with too few or too
  many arguments, such as `math.pow(2.0)`; it previously failed only at run
  time. Functions with several call forms (an optional leading logger, session
  or generator, or variadic arguments) accept each documented form.
- The stdlib catalog used by `geblang check`, `geblang doc` and the language
  server now matches the runtime for `datetime.unix(seconds)` (returns an
  RFC3339 string), `io.mkdir(path, mode)` (mode is required),
  `cli.help(name, spec)` and `args.help(name, spec)`, and the optional
  arguments of `http.shutdown`, `http.response`, `io.tryLock`, `cli.choose`,
  `cli.table`, `stats.variance` and `stats.stdev`.
- `geblang check` scopes a `for (x in ...)` loop variable to the loop body, so
  a loop variable named like a top-level function (`for (t in tags)` beside
  `func t(...)`) is no longer typed as that function.
- On the evaluator, an error raised while choosing an overload or constructor
  (`no matching overload`) reports the call's line in its stack trace when an
  argument was itself a function call; it reported the last line that
  function ran.
- On the bytecode VM, an integer literal passed to a `float` or `decimal`
  parameter of a function, overloaded function or class constructor imported
  from another module is converted, as it is for a call within the same
  module and on the evaluator; it failed with `expects decimal for parameter`
  (or `no matching overload`).
- `ext.call` and `ext.callWithOptions` forward named arguments to the extension
  as keyword arguments, as documented, on both runtimes; they failed with
  `call has no parameter ...`. The names `conn`, `function` and `options` still
  bind to the call's own parameters.

- On the bytecode VM, `reflect.parameters` (and `reflect.returnType`) works on a
  closure created in another module, no longer lists a closure's captured
  variables as parameters, and reports parameter types with their type
  arguments (`list<int>`, `dict<string, any>`, `int[]`) as the evaluator does.

- `test.mock` applies to calls made inside imported modules on the bytecode VM
  (previously only on the evaluator) and to every native module function,
  including `sys`. Mocking a name that is not a native function throws a
  `ValueError` instead of being silently ignored.
- `geblang check` checks the bodies of function literals (closures) as it does
  named functions, so typed declarations, returns and parameter defaults inside
  them are type-checked.
- Type-mismatch diagnostics for declarations, returns and parameter defaults
  carry a line and column.

- An integer-literal branch of a conditional or `??` takes a `float` or
  `decimal` target in every context, including instance and static field
  assignments (`this.total = paid ? amount : 0`) and `decimal x = maybe ?? 0`.
  Static field assignments behaved differently on the two runtimes.
- `geblang check` checks each branch of a conditional or `??` against its
  target, so an `int` variable branch assigned, returned or defaulted where a
  `float` or `decimal` is declared is reported; it was previously accepted and
  stored as an `int`.
- `geblang build --native` lowers a conditional or `??` that mixes an integer
  literal with a typed value; it emitted Go that failed to compile, or failed at
  run time for field assignments.

- `smtp.send` (and `mailer.Mailer.send`) accepts an int `port` on the bytecode
  VM; it previously failed with `config.port must be int`. Other native
  functions that took int handles or counts strictly accept the same values on
  both runtimes.
- `smtp.send` uses the bare address for the SMTP envelope when `from` or a
  recipient includes a display name (`"App <noreply@example.com>"`). Servers
  rejected the whole header value as `MAIL FROM`.

## 1.37.3

### Language

- `static async func` declares an async static method; calling it returns a
  `Task` on both backends. The reversed `async static func` is a parse error
  that suggests the canonical order.
- Enum methods can be `async func`; calling one on a variant returns a `Task`,
  including on an enum from another module.
- Instances of a class with an ordering dunder (`__lt` or `__gt`) can be sorted
  and ranked: `sort()`, `sorted()`, `sortBy`, `minBy`, `maxBy`, `topK`,
  `bottomK`, `topBy`, the binary searches, and `collections.sort` use it, as
  the `<` operator does.

### Fixes

- `geblang fmt` no longer drops code. It used to delete interface properties
  and default methods, field decorators, destructors, enum backing types and
  values, and could turn an inclusive slice `xs[1..3]` into an exclusive one or
  unwrap a string literal written inside `${...}`. Its safety check now compares
  the whole syntax tree, so any construct it cannot print faithfully is refused
  and the file is left unchanged.
- `geblang fmt` keeps a chain of `catch` clauses on one line, places decorators
  before `export`, and preserves blank lines between enum methods.
- On the bytecode VM, operator and protocol dunders work on instances of a class
  imported from another module and on local subclasses that inherit them:
  `<`, `>`, `<=`, `>=`, `==`, arithmetic, bitwise and prefix operators, `in`,
  subscripts, and `as` casts. Comparisons previously failed with
  `unknown class`, and a subclass lost every inherited operator.
- On the bytecode VM, a decorator function imported with `from m import f`
  now applies to functions, methods, classes, and fields, as it does on the
  evaluator; it was previously ignored. A method decorator from one module
  applied to a class in another keeps `this` when it calls the wrapped method.
- `geblang build` bundles modules that are reached only through
  `from module import name`; such a module was left out of the binary, which
  then failed at run time with `cannot resolve module`.
- On the bytecode VM, calling an `async func` method returns a `Task`, as on the
  evaluator, instead of running the method to completion and returning its
  value. This holds for every call shape: on an instance or through `this`,
  inherited, with named or spread arguments, as a bound method value, and on a
  class from another module.
- On the bytecode VM, an async function or method called from inside other async
  code, or from a callback, returns a `Task` instead of running inline, so
  fan-out inside an async function (`async.all([...])`) runs concurrently. A
  decorated async function stays a single Task: the decorator's call to the
  original body runs inside it.
- A generic class or function constrained with `<T implements X>` accepts a type
  argument declared in another module (including the entry script) when it
  satisfies the constraint; it was rejected with `type ... does not satisfy
  constraint` on both backends.
- On the bytecode VM, a generic class's constraints are checked when an
  instance is built, as on the evaluator: `Basket<Rock>()` with no constructor,
  and a subclass whose `extends Base<Rock>` clause breaks the base's constraint,
  now raise `TypeError` instead of succeeding.
- On the bytecode VM, a generic type whose type argument is qualified by a
  module alias (`boxes.Crate<goods.Apple>`) matches correctly as a field,
  parameter, or variable type and as an `instanceof` target; it was rejected
  with `cannot assign` or tested false.
- A field decorator runs on every assignment to its field, as documented,
  including assignments written in another module, compound assignments, and
  assignments in a subclass declared elsewhere; previously only assignments
  inside the declaring module ran it. On the bytecode VM a subclass in the same
  module also skipped an inherited field's decorator.
- A field decorator resolves in the scope that declares the class, so a
  constructor parameter with the same name (`@sku string sku;` with
  `func Product(string sku)`) no longer shadows it; the evaluator reported
  `decorator is not callable: string`.
- On the evaluator, an overloaded operator method (`__mul(int)` and
  `__mul(decimal)`, `__index(int)` and `__index(string)`, ...) dispatches to
  the overload matching the operand, as on the VM; it always called the first
  declaration.
- A `match` case can destructure a variant of an enum from another module
  through its module alias (`case shipping.Quote.Offered(string c, Money p)`),
  which was a parse error, and `instanceof shipping.Quote.Offered` tests the
  variant on both backends.
- `x == null` and `x != null` compare with null directly on both backends; a
  class's `__eq` is no longer called with `null`, which raised `TypeError` when
  `__eq` was typed for its own class. The same holds inside lists and dicts.
- An expired `async.timeout(task, ms)` raises `TimeoutError` (an `IOError`
  subclass) instead of `RuntimeError`, so a timeout can be caught on its own.
  Code that caught the timeout as `RuntimeError` should catch `TimeoutError`.
- On the bytecode VM, `assertThrowsOf(fn, "IOError")` accepts a `TimeoutError`
  or `TlsError`, as `catch (IOError e)` and the evaluator already did.
- `geblang check` tracks the result type of built-in methods through a chain,
  so `value.trim().upper().matches(...)` is reported as
  `string has no method matches` instead of failing only at run time.
- On the bytecode VM, named arguments to a static method of a class from
  another module, or inherited from one (`pricing.Pricing.total(net: 100)`),
  bind by name; they failed or were bound positionally.
- `@override` accepts a method that replaces an interface's default method;
  `geblang check` and both backends reported that no ancestor or interface
  declared it.

## 1.37.2

### Fixes

- A program on the bytecode VM whose async tasks made their first calls into
  stdlib modules at the same moment could crash intermittently; those calls are
  now safe to run concurrently.
- An argument of the wrong type for a constructor, method, or static method with
  a single signature raises `TypeError` naming the parameter
  (`User expects string for parameter 'name', got int`) on both backends, as
  functions already did, instead of `RuntimeError: no matching overload`.
- On the bytecode VM, a variable declared `any` is no longer treated as the type
  of its initializer: `any missing = null; greet(missing)` is a catchable
  run-time `TypeError`, as on the evaluator, instead of a compile error.
- The bytecode VM chooses among same-arity overloads at run time when the
  arguments' types are only known then: a call to an overloaded function or
  constructor with `any`-typed arguments, and a partial application over
  overloads (`describe(_)`), now run on the VM and in `geblang build` binaries
  instead of failing to compile.
- `geblang check` and the compiler accept an `any`-typed argument to an
  overloaded method or static method; the overload is chosen at run time, as
  for overloaded functions and constructors. A literal that fits no overload is
  still reported.
- A decorator that cannot be applied reports the same message, error class, and
  line on both backends, naming the decorator and the declaration
  (`decorator @wrap cannot be called for greet: wrap expects int for parameter
  'count', got string`).

### Documentation

- The generated reference manual has a search box: every page searches the
  manual's sections by heading and text, works offline and from disk, and
  focuses with `/`.

## 1.37.1

### Fixes

- The REPL reports runtime faults in an evaluated expression with their class
  (`TypeError`, `ValueError`, ...) instead of `RuntimeError`, matching scripts
  and tests.
- `geblang check` now analyses code inside `with` and `select` blocks,
  comprehensions, `${value:spec}` interpolations, enum methods, destructors,
  interface default methods, closures, parameter defaults, and decorator
  arguments. Unknown module members, unknown qualified types, and unknown
  `instanceof` types in those places are reported instead of failing only at
  run time.
- `geblang check` no longer reports an import as unused when its only uses are
  in those places or in a module-qualified decorator (`@mod.Name`), and it
  reports unreachable statements inside them.
- A name bound by a match case, a `with` block, or a comprehension now shadows
  an import of the same name in `geblang check`, so `json.size()` on a local
  named `json` is no longer reported as an unknown module member.
- Field types are now enforced at run time on both backends. Writing a value of
  the wrong type to a typed field throws `TypeError`
  (`cannot assign string to int field User.age`), whether it comes from an
  assignment, a method or constructor, a field default, a static field,
  `reflect.setField`, or `parseAs`. Previously any value was stored. `null` is
  accepted only by nullable fields, type aliases are resolved, and a field of
  generic type `T` is checked against the binding of the class that declares
  it (`class Child<T> extends Base<int>` checks `Base`'s field against `int`).
  Code that stored a decimal literal in a `float` field (`this.rate = 0.0`) now
  fails; write `0.0f`.
- `geblang check` and the compiler report a field write whose value type is
  known and cannot fit the declared field type, such as
  `account.balance = "lots"` on an `int` field.
- Assignments to a field or list/dict element evaluate the target (object and
  index) before the assigned value on both backends, left to right.
- `json.parseAs`, `yaml.parseAs`, and `toml.parseAs` convert a parsed number to
  the numeric type of its field or constructor parameter (`10` into a `decimal`
  or `float`, `3.0` into an `int`, including elements of numeric lists and
  dicts). These and `xml.parseAs` throw `TypeError` naming the field for any
  other mismatch instead of storing the value unchanged.
- An integer literal, including a negated one such as `-1` and a conditional
  of two literals (`on ? 1 : 2`), takes the type of a `float` or `decimal`
  target: a field or static field (`this.total = 0`), a field default (plain or
  negated literal), a declaration (`float x = -1` now stores a `float`), a
  return value, or a positional or named argument of a function, constructor,
  `parent(...)` or `parent.method(...)` call, static method, or method
  (`half(3)`), including a generic `T` parameter bound to `float` or `decimal`
  directly or through an `extends` clause. An `int` overload is still preferred,
  and among overloads that need converting the fewest conversions wins. Literals
  are not typed for variadic or enum-method parameters. On the bytecode VM a call
  to a closure value, or to a function, constructor, or method of another
  module, still rejects the `int`.
- On the bytecode VM, a static field of a class from another module can be
  assigned (`lib.Holder.count = 4`), and reads see writes made by the module's
  own code.
- The tree-walking evaluator no longer crashes intermittently when async tasks
  call the same bound method (`reflect.methodBound`) concurrently.
- On the tree-walking evaluator, a parameter or field typed with a
  module-qualified enum (`palette.Color`) rejects a same-named enum from a
  different module, as the VM does.
- The bytecode VM accepts negative numeric literals as field, static, and
  parameter defaults (`decimal offset = -2`).
- On the bytecode VM, a parameter or field typed with an enum from another
  module (`palette.Color`) accepts that enum's values.
- `geblang build --native` lowers the new field rules: an integer literal written
  to a `float` or `decimal` field compiles, and a value typed `any` written to a
  scalar field is checked with the same `TypeError`.
- `geblang build --native` compiles an integer literal, negated literal, or
  conditional of two literals passed, assigned, declared, or returned where a
  `float` or `decimal` is expected (`decimal d = 3;`, `half(on ? 1 : 2)`),
  instead of failing in the Go build.
- On the bytecode VM, a method overridden in another module's subclass is
  called when the receiver's declared type is a non-exported class with an
  exported descendant; previously the base method ran.
- A generic class that passes its own type parameter to its parent
  (`class Leaf<U> extends Box<U>`) binds the parent's `T`, so inherited methods
  and fields accept a `U` value (`Leaf<decimal>().put(2.5)`) on both backends.
- A method that declares its own type parameter with the same name as its
  class's (`func id<T>(T x)` in `class Box<T>`) uses its own `T`, so
  `Box<float>().id(5)` accepts the `int`. Explicit type arguments on such a call
  (`b.id<int>(n)`) work on the bytecode VM as on the evaluator.
- `is` and `is not` compare object identity on both backends: two references to
  one instance, list, dict, set, or function value are identical, and equal but
  separately built values are not. Previously the bytecode VM reported a list or
  dict as not identical to itself and the evaluator compared collections by
  value. Numbers, strings, and other value kinds compare by type and value
  (`1 is 1.0f` is `false`). The operators are now documented.
- On the bytecode VM, a parameter typed with a local collection alias
  (`type Nums = list<float>`) checks the collection's elements, and `parseAs`
  converts numbers for a constructor parameter of that type.
- Editor signatures for `http.fetchStream` (takes a list of requests) and
  `web.parseMultipart` (takes a request dict) are corrected, and `FetchStream`
  methods now appear in completion.

### Documentation

- The `crypt` hash functions are documented as returning a lowercase hex
  `string`; `crypt.hmacSha256Bytes`, `http.fetchStream`, `web.parseMultipart`,
  `datetime.nowInstant`, `amqp.declareExchange`, and `amqp.queueBind` are now
  documented.
- Self-contained code examples in the manual now import every module they
  use.

## 1.37.0

### Language

- Runtime operand type mismatches now raise `TypeError`. This includes unary and
  binary operators and decimal/float arithmetic. Dynamic parameter, generic
  constraint, and typed assignment mismatches use the same class.
- Unsupported casts and invalid cast-hook return types raise `TypeError`.
  Supported conversions with invalid content raise `ValueError`, including
  `as` casts and primitive conversion methods.
- Collection access uses `TypeError` for an index of the wrong type and
  `ValueError` for an out-of-range index or zero slice step. Typed collection
  writes continue to raise `TypeError`; missing dict keys still return `null`.
- Return annotations are now checked at runtime with the same rules as
  parameters, and a mismatch raises `TypeError`. This covers primitives,
  collections and their element types, classes, interfaces, enums, `func`,
  unions, and nullable types; a bare type parameter is checked against its
  receiver or explicit call binding. Previously returns were not checked. A
  function annotated `dict<...>` that returns an object, `list<int>` that returns
  a list of strings, or `Dog` that falls off the end without returning now fails.
- Type errors raised for closure parameters and returns name the function
  `<closure>` on both backends.
- Non-bool conditions in `if`, `while`, ternaries, `&&`, and `||` raise
  `TypeError: condition must be bool, got <type>`; `!` reports the operand type.
- Bitwise operators with non-int operands report
  `unsupported operands for <op>`, matching the other binary operators.
- `typeof(...)` produces a `Type`, so returning it from a function declared
  `string` now needs an explicit `as string` cast or interpolation.
- `export type Name = Target;` publishes a type alias to importers. `mod.Name`
  means exactly `Target` in every type position, including generic arguments,
  `implements`, `extends`, `instanceof`, `as`, and `from mod import Name`.
  Aliases are type-only and private aliases stay in their module.
- Names brought in with `from M import N` are no longer part of the importing
  module's surface: `facade.N` for a merely imported `N` is a `geblang check`
  error. Re-export a type with `export type N = M.N;`.
- Type aliases now expand in `instanceof` (previously an alias name never
  matched), and an alias declared without type arguments accepts them:
  `type Rows = list;` makes `Rows<int>` mean `list<int>`.
- Unresolved generic parameters are left out of `reflect.typeBindings` instead
  of reporting the parameter's own name (`{"E": "string"}` rather than
  `{"E": "string", "T": "T"}`).

`TypeError`, `ValueError`, and `RuntimeError` remain siblings under `Error`.
Code that caught `RuntimeError` for the reclassified failures should catch the
specific class or `Error` for broad recovery.

### Fixes

- On the bytecode VM, an integer literal assigned or returned where a `float`
  or `decimal` is expected now takes that type, matching the evaluator.
  `float f = 3;` was previously stored as an `int`.
- On the bytecode VM, `&&` and `||` with a non-bool right operand now raise
  `TypeError` instead of returning the operand.
- On the bytecode VM, a variable declared with a collection alias
  (`type Ints = list<int>; Ints xs = ...;`) is element-checked, matching the
  evaluator.
- On the bytecode VM, a `?T` parameter or return on a bound generic accepts
  `null`, and a nullable constructor parameter (`?T`) still infers `T` from a
  non-null argument.
- On the evaluator, a module's private type alias no longer changes what the
  same name means in other modules.

### Standard library

- `llm.Client` is now an alias of the `llm.contract.Client` interface, which
  the OpenAI, Anthropic, and Bedrock clients implement explicitly. Values from
  `llm.client(...)` are accepted wherever an `llm.Client` is expected.
- `messaging.MessageQueue` and `messaging.MessageTopic` are aliases of the
  `messaging.contract` interfaces, which every queue and topic backend
  implements explicitly.
- `async.Channel.recv()` and `tryRecv()` are declared `?T`, matching their
  documented `null` result for a closed or empty channel.

## 1.36.1

### Fixes

- Package modules may keep a short declaration that matches a standard-library
  name, such as `module i18n;` imported as `app.i18n`. Module checking and
  imports use the package-qualified identity; top-level built-in names remain
  reserved.
- `geblang build --resource /absolute/source=destination` again accepts an
  explicitly mapped source outside the project. Manifest resources and relative
  CLI sources remain project-bound, and symlinks cannot escape the selected
  source directory.

## 1.36.0

### Standard library

- `streams` adds binary stream adapters for limiting, buffering, and teeing
  reads, plus bounded `copyN` and correct handling of short writes.
- `archive` adds streaming zip, tar, and tar.gz readers and writers, with
  bounded extraction that rejects unsafe paths and links.
- `i18n` adds explicit-locale number, date, and currency formatting, collation,
  plural rules, and message catalogs with locale fallback.
- `config` adds layered loading, environment overrides, and schema validation.
- `datetime.period` adds zoned periods, recurrence, and business-day helpers.
- `mailparse` adds bounded MIME message parsing and attachment access.
- `jsonl` adds line-oriented JSON reading and writing.
- `resources` adds safe access to bundled package resources.

### Fixes

- Native datetime objects now pass `instanceof` consistently in the evaluator
  and bytecode VM.
- Cached embedded standard-library modules now use their content in the cache
  key, so a rebuilt toolchain does not reuse stale module bytecode.

### Tooling

- Editor completion and hover cover the new standard-library APIs.

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

Notes for 1.0.0 through 1.19.x have been removed; they remain in the
repository history.
