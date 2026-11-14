# Release Notes

## 1.42.1

### Fixed

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

Older releases (1.0.0 through 1.37.8) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
