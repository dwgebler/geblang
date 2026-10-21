# Geblang language reference

Verified against Geblang 1.27.0. When a signature matters, confirm it with
`geblang doc <file>` or a five-line `geblang check` probe rather than trusting
memory.

## Comments

```gb
# line comment
io.println(x);   # trailing comment
/* block comment
   over multiple lines */
```

`//` is integer division, never a comment. `/** ... */` is the conventional
multi-line doc block (preferred over stacked `#` lines).

## Imports and modules

```gb
import io;
import datetime as dt;            # alias
from crypt import passwordHash;   # selective
from bytes import toHex as hex;   # selective + alias
```

Imports go at the top of the file. Using a module as a selector base without
importing it is a semantic error on both backends. Stdlib modules need no path;
local modules resolve through the manifest's `paths` / `dependencies`.

A module file declares its name and exports its public surface explicitly;
everything else is private to the module:

```gb
module app.users;

func normalizeId(string id): string { ... }      # private
export func findUser(string id): ?User { ... }    # public
export const int MAX_PAGE = 100;
export class User { ... }
```

## Variables and primitive types

```gb
let x = 1;              # inferred int
int y = 2;             # explicit
?string maybe = null;  # nullable
const PI = 3.14;       # immutable; PI is decimal
```

Primitives: `int`, `float`, `decimal`, `string`, `bool`, `bytes`, `any`, `null`.
Integers are arbitrary precision (no overflow). Strings are UTF-8.

- `let x = 1.0;` infers **`decimal`**, not `float`. Use `1.0f` or `1.0 as float`
  for IEEE `float`. `decimal` is the default because exact arithmetic (money) is
  more common than fast binary math.
- **`/` is true division** and produces `decimal` (or `float`), even when the
  operands divide evenly. `int n = a / b;` is a compile error. Use `//` for the
  integer (floor) quotient, or `(a / b) as int` to truncate.
- Mixed `decimal`/`float` arithmetic does not coerce; `geblang check` reports it
  when both operand types are known. Cast with `as float` (drops decimal
  exactness) or `as decimal` (adopts the float's binary imprecision).

Strings interpolate inside double quotes only:

```gb
io.println("hello ${name}");       # interpolated
io.println('hello ${name}');       # raw (single quotes)
io.println("${3.14159:.2f}");      # -> 3.14  (format spec)
io.println("${42:04d}");           # -> 0042
```

Numeric-check predicates test before converting: `s.isInt()`, `s.isDecimal()`,
`s.isNumeric()` on strings; `f.isInt()` / `d.isInt()` on `float` / `decimal`
(whole-number test).

## Collections

```gb
list<int>         xs = [1, 2, 3];     # int[] is an alias
dict<string, int> d  = {"a": 1};
set<int>          s  = {1, 2, 3};     # brace literal without ':' is a set
```

- **List mutators work in place AND return the receiver** (chainable): `push`,
  `pop`, `prepend`, `insert`, `removeAt`, `remove`, `reverse`, `sort`, `sortBy`,
  `fill(value, count)`. `sorted()` / `reversed()` / `copy()` / `deepCopy()` are
  the copy variants.
- **Dicts iterate in insertion order**, including `.keys()`, `.values()`,
  `.items()`, and `for (k, v in d)`. A bare `{}` is a dict; `set<int> s = {};`
  (set-typed variable, parameter default or field) is an empty set.
- `search` / `searchPattern` on lists, dicts, and strings return every matching
  locator: `xs.search(value)` (equality), `xs.search(pred)` (predicate),
  `s.searchPattern(regex)`.
- Slicing is Python-style: `xs[1:3]`, `xs[:2]`, `s[-3:]`; negative indexes count
  from the end.
- Comprehensions: `[x * x for x in 1..5]`, `[x for x in xs if x > 0]`,
  `{k: v * 2 for k, v in d}`.
- `instanceof list<T>` works at runtime (generics are reified).

## Ranges

```gb
for (i in 1..5)   { }     # inclusive: 1,2,3,4,5
for (i in 0..<10) { }     # exclusive end
range(1, 9, 2);           # builtin -> [1, 3, 5, 7, 9] (inclusive, eager)
zrange(0, 5);             # exclusive, Python-style -> [0,1,2,3,4]; zrange(n) from 0
let r = 1..5;
r.length; r.first; r.last; r.contains(3); r.toList();
```

## Control flow

```gb
if (cond) { } else if (other) { } else { }
for (x in xs) { }
for (k, v in d) { }
for (let int i = 0; i < n; i++) { }
while (cond) { }
do { } while (cond);
outer: for (i in 1..3) { continue outer; }     # labeled loops

let grade = score >= 50 ? "pass" : "fail";     # ternary

match (x) {
    case int n if (n > 0) => handlePositive(n);  # type pattern + guard
    case [first, second]  => handlePair(first, second);
    case ["go", n]        => handleGo(n);        # literal elements pin positions
    case string s         => handleString(s);
    default               => handleOther();
}
```

`match` uses `=>`; guards are `if (cond)` after the pattern; patterns match by
type, literal value, or list shape and bind names. As an expression each arm
yields a value; an unmatched expression with no `default` throws `MatchError`.
`geblang check` warns when a `match` over an enum omits a variant and has no
`default`.

`defer expr;` runs at function exit in LIFO order, like Go.

## Null handling

```gb
?string name = lookup(id);
if (name != null) { io.println(name.length()); }   # narrowed to string inside
let display = name ?? "anonymous";                  # null coalesce
let bio = user?.profile?.bio ?? "";                 # optional chaining
```

## Functions

```gb
func add(int a, int b): int { return a + b; }
let inc = func(int n): int { return n + 1; };

func greet(string name, string greeting = "Hello"): string { ... }
greet("Ada", greeting: "Hi");            # named argument

func sum(int start, int ...rest): int {  # variadic: rest is list<int>
    return start + rest.reduce(func(int a, int b): int { return a + b; }, 0);
}
sum(1, 2, 3);
sum(...[1, 2, 3]);                       # list spread
greet(...{"name": "Ada"});               # dict spread -> named args (extra keys dropped)
```

- Defaults, named args, variadics, and spread combine and work in every dispatch
  context: functions, lambdas, methods, static methods, constructors, native
  builtins, module functions, callable objects.
- **Partial application**: a call with one or more `_` placeholders returns a new
  callable with those positions open: `add(_, 10)`, `wrap(_, "-", _)`,
  `open(mode: _)`. Non-hole arguments are captured once at creation. (Compiled
  builds reject partials over multiple same-arity overloads - use a typed
  wrapper there.)
- Overloading resolves by argument count and type at the call site.
- Closures capture by reference; captured variables can be reassigned through the
  closure. A `for` loop variable is a single shared binding, so a closure that
  captures it directly (e.g. spawning tasks in a loop and reading the loop variable
  inside them) sees the final value; bind a fresh `let n = i;` per iteration first.
- Pipe: `x |> f(y)` calls `f(x, y)`; chains read left to right.

## Generics

```gb
func head<T>(list<T> xs): T { return xs[0]; }

class Box<T> {
    T item;
    func Box(T v) { this.item = v; }
    func get(): T { return this.item; }
}

func topBy<T implements Scored>(list<T> items): T { ... }
```

- Reified: `instanceof T`, `instanceof Box<string>`, `reflect.typeBindings(x)`,
  and element-type enforcement on typed collections all work at runtime.
- Explicit constructor type arguments are enforced: `Box<string>(42)` throws.
  Method parameters typed with a class type parameter are checked against the
  instance's bindings: `put(42)` on a `Box<string>` throws.
- Constraints accept interface, class, and primitive leaves combined with `|`
  and `&`: `<T implements string|int>`; the bare form drops the keyword
  (`<T string|int>`). A class leaf is satisfied by the class or any subclass.
- Typed-collection writes are covariant-safe: a `list<Dog>` received as
  `list<Animal>` rejects a `Cat` write against its real element tag.

## Generators

```gb
func counter(): generator<int> {
    yield 1;
    yield 2;
}
for (n in counter()) { io.println(n); }
```

Return type is `generator<T>`; use `iterable<T>` for parameters accepting any
iterable (generators, lists, sets, ranges). Manual stepping: `next()` advances
(returns `null` once exhausted), `done()` peeks for exhaustion, `close()` ends
early; these compose with `for-in`. An error thrown inside a generator keeps its
class in the consuming loop's `catch`.

## Async (true parallelism)

```gb
import async;

let t1 = async.run(func(): int { return work(1); });
let t2 = async.run(func(): int { return work(2); });
io.println(async.all([t1, t2]));         # parallel on real cores

import async.channel as ch;
let c = ch.Channel<int>(8);
c.send(1);
io.println(c.recv());                    # channels also iterate: for (x in c)
```

Tasks are goroutines: CPU-bound fan-out scales across cores (no GIL, no event
loop). `async.await`, `async.all`, `async.race`, `async.timeout(task, ms)`,
`async.cancel` compose tasks; channels and `select` coordinate them.
`async.sleep(ms)` returns a Task: `await` it to pause. For
higher-level orchestration, `import async.tasks as task` adds `map`/`forEach`
(bounded parallel over a collection), `retry` (backoff), `settle` (await-all,
no fail-fast), `any` (first success), and `parallel` (run a list/dict of
callables), all built on the async core.

Never mutate a plain `dict` / `list` / object from several tasks at once (a
race can crash the process); share mutable state through `store.Store`. A
value frozen with `freeze.deep` can never change, so tasks and server requests
may read it concurrently, and `store.Store` hands it out without copying
(1.37.5+): build a large read-only structure once, freeze it, and share it.

## Classes

```gb
class Counter {
    int value;
    static int created = 0;
    const string KIND = "counter";

    func Counter(int start = 0) {
        this.value = start;
        Counter.created += 1;
    }
    func bump(): int { this.value += 1; return this.value; }
    static func total(): int { return Counter.created; }
}

class TaskCounter extends Counter {
    string label;
    func TaskCounter(string label) {
        parent(0);                  # parent CONSTRUCTOR
        this.label = label;
    }
    func bump(): int { return parent.bump() + 100; }   # parent METHOD
}
```

- `parent(args)` calls the parent constructor; `parent.method(args)` calls a
  parent method. `super` is not a keyword.
- `@abstract` on a class blocks instantiation; on a method forces an override.
- `@immutable` on a class makes fields set-once (construction only).
- No `public`/`private`/`protected`; module `export` is the visibility boundary.

## Operator overloading (dunders)

```gb
class Money {
    int cents;
    func Money(int c) { this.cents = c; }
    func __add(Money o): Money { return Money(this.cents + o.cents); }
    func __lt(Money o): bool { return this.cents < o.cents; }
    func __eq(Money o): bool { return this.cents == o.cents; }
    func __string(): string { return "$${this.cents / 100}"; }
}
```

Dunders: arithmetic `__add __sub __mul __div __intdiv __mod __pow`; ordering
`__lt __lte __gt __gte` (defining ONE ordering dunder derives the other three
operators); `__eq`; bitwise `__bitand __bitor __bitxor __lshift __rshift`;
prefix `__not __neg __bitnot`; plus `__index`, `__contains` (for `in`),
`__iter`/`__next`/`__done`, `__call`/`__invoke` (callable objects), and cast
dunders `__string __int __float __bool __decimal __bytes` (these drive `as TYPE`,
`println`, and interpolation - the string-cast hook is `__string`, NOT
`toString`).

## Interfaces and enums

```gb
interface Notifier {
    func send(string msg): void;
    func sendAll(list<string> ms): void {     # default body allowed
        for (m in ms) { this.send(m); }
    }
}

enum Color { Red, Green, Blue }
enum Shape { Circle(decimal), Rect(decimal, decimal) }   # tagged variants

enum Planet {
    Earth, Mars;
    func greeting(): string {                 # enums can declare methods
        return match (this) { case Planet.Earth => "hi"; default => "bonjour"; };
    }
}
```

- `EnumName.values()` lists the simple (nullary) variants in declaration order;
  `EnumName.fromName(s)` resolves a variant by exact name or returns `null`.
  Tagged variants are excluded from both.
- Enums may `implements` an interface; conformance is checked and interface
  defaults apply. Enums stay immutable.

```gb
let area = match (shape) {
    case Shape.Circle(decimal r)          => 3.14 * r * r;
    case Shape.Rect(decimal w, decimal h) => w * h;
};
```

## Errors

```gb
try {
    risky();
} catch (ValueError e) {
    io.println("${e.class}: ${e.message}");
} finally {
    cleanup();
}

class MyError extends ValueError { func MyError(string m) { parent(m); } }
throw MyError("boom");
```

Catches dispatch by class hierarchy (subclasses match a parent catch); a catch
clause requires a type, and `Error` is the base. Built-in classes: `Error`,
`RuntimeError`, `ValueError`, `TypeError`, `IOError`, `NetworkError`,
`DatabaseError`, `NotFoundError`, `TimeoutError`, `TlsError`, `PermissionError`,
`AssertionError` (`TimeoutError`/`TlsError` are `IOError` subclasses;
`PermissionError` guards capability-gated ops). Helpers: `errors.wrap(inner,
msg)`, `errors.is(e, cls)`, `errors.stackTrace(e)`, `errors.frames(e)`. Uncaught
errors render identically on both backends (classed header + stack trace).

The `option`/`result` types model presence/failure without exceptions:
`none()`, `some(v)`, `ok(v)`, `err(e)`, `ofNullable(x)`, `.unwrapOr(default)`,
`.orNull()` keep their declared type on the absent/error path.

## Context managers and decorators

```gb
with (f = io.open("data.txt")) { io.println(f.readAll()); }   # f does not escape

@memoize
func fib(int n): int { ... }                  # built-in caching decorator

func logged(func f): func {                    # custom wrapping decorator
    return func(int x): int { io.println("call ${x}"); return f(x); };
}
@logged
func double(int x): int { return x * 2; }
```

A decorator that resolves to a function is a callable wrapper; otherwise it is
inspectable metadata (`reflect.decorators(target)`).

## Reflection

```gb
import reflect;

reflect.typeOf(value);              # "int", "list", a class name, ...
reflect.class("module.ClassName");  # resolve a class by name
reflect.classes();                  # every loaded class
reflect.fields(cls);                # field metadata (each entry has a `doc` key)
reflect.methods(cls);               # method names (declared case)
reflect.parameters(fn);             # parameter metadata dicts
reflect.decorators(target);         # [{name, args, namedArgs}]
reflect.hasDecorator(target, "Get");
reflect.typeBindings(value);        # {"T": "int"} for reified generics
reflect.function("math.sqrt");      # resolve a function (native too)
reflect.parent(cls);                # walk the chain
reflect.interfaces(cls);            # DIRECT implements clause only
```

`typeof(x)` (parens required), `dir(value)`, and `dump(value)` are ambient
introspection builtins.

## Idioms and anti-idioms

- Access after a checked cast: `(value as Foo).method()`; test first with
  `instanceof`.
- Stringify by interpolation (`"${n}"`), `n.toString()`, or `(n as string)` -
  concatenation does not coerce.
- Accumulate with `xs.push(item);` (in place). Hot-loop strings: `strbuilder` or
  `+=` on a local; avoid rebuilding via `s = s + x` on a field.
- Do NOT use `//` for comments, `super` for parent calls, or reassign a list
  mutator result expecting a copy.
- Do NOT name a source file the same as a stdlib module you import.

## Pitfalls, in priority order

1. `//` is integer division, not a comment.
2. `/` yields `decimal`; `int n = a / b` is a compile error - use `//`.
3. List mutators are in-place and return the receiver; `sorted()` copies.
4. `as` is a checked cast, not a test; use `instanceof` to test.
5. Floating literals are `decimal`; suffix `f` or cast for IEEE `float`.
6. Mixed numeric arithmetic does not coerce; cast explicitly.
7. String interpolation needs double quotes; single quotes are raw.
8. `typeof` needs parentheses; dict iteration is insertion order.
9. Do not shadow stdlib module filenames in your source files.
10. `reflect.interfaces(cls)` is the direct clause only; walk `reflect.parent`.
