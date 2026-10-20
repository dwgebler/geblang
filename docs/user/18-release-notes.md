# Release Notes

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

Older releases (1.0.0 through 1.29.2) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
