# Control Flow

## Conditionals

Conditions must be `bool`.

```gb
if (score >= 90) {
    io.println("excellent");
} elseif (score >= 60) {
    io.println("pass");
} else {
    io.println("retry");
}
```

There is no implicit truthiness. Check length, nullability, or equality
directly:

```gb
if (items.length() > 0 && currentUser != null) {
    io.println("ready");
}
```

## Loops

```gb
while (i < 10) {
    i++;
}

for (let int i = 0; i < 3; i++) {
    io.println(i);
}

for (n in [1, 2, 3]) {
    io.println(n);
}

for (key in data.keys()) {
    io.println(key);
}
```

`break` exits a loop. `continue` skips to the next iteration.

The loop variable may carry a type annotation, and it is checked the same way a
typed declaration is: `for (int n in [1, 2, 3])` is fine, but `for (string n in
[1, 2, 3])` is an error ("cannot assign int to string n"), and an unknown type
name is rejected ("unknown type ..."). Omit the type to let it be inferred from
the iterable.

Use `for-in` for collections and generators. Use C-style `for` loops when the
index itself matters:

```gb
list<string> names = ["Ada", "Grace", "Linus"];

for (let int i = 0; i < names.length(); i++) {
    io.println("${i}: ${names[i]}");
}
```

This is the direct equivalent of a basic counter loop in C, JavaScript, PHP, or
Go. The loop variable is available in the loop body and can be used as an index.
Use this pattern when you need both the current value and its position.

Range literals support an optional `by` step:

```gb
for (i in 0..10 by 2) {    # 0 2 4 6 8 10
    io.print("${i} ");
}

for (i in 10..0 by -2) {   # 10 8 6 4 2 0
    io.print("${i} ");
}

for (i in 0..<10 by 3) {   # 0 3 6 9 (exclusive upper bound)
    io.print("${i} ");
}
```

The step defaults to 1 when omitted. It can be any integer expression.

### What for-in iterates over

`for-in` accepts lists, ranges, generator-returning calls, classes that
implement the iterator protocol, and (1.16.0) dicts, sets, and strings
directly:

- A **dict** yields insertion-ordered `[key, value]` pairs. Two binders
  destructure them: `for (k, v in d) { ... }`.
- A **set** yields its elements in the same sorted order as `set.toList()`.
- A **string** yields single-character strings, matching `.chars()`.

```gb
for (k, v in {"a": 1, "b": 2}) {
    io.println("${k}=${v}");      # a=1  b=2
}

for (n in {3, 1, 2}) {
    io.print("${n} ");            # 1 2 3
}

for (c in "abc") {
    io.print(c);                  # abc
}
```

For lazy range iteration over large sequences, import `collections` and use
`collections.range(start, end, step)`:

```gb
import collections;

for (i in collections.range(0, 5, 1)) {
    io.println(i);
}

for (i in collections.range(10, 0, -2)) {
    io.println(i);
}
```

`collections.range` is lazy and suitable for large ranges. Use the `by` clause
on range literals for the common case; use `collections.range` when you need
lazy evaluation or runtime-constructed sequences.

Three eager integer-range builtins exist. `range(start, end[, step])` builds an
*inclusive* list (`range(0, 3)` is `[0, 1, 2, 3]`). `zrange` is the *exclusive*,
Python-style counterpart: `zrange(0, 3)` is `[0, 1, 2]`, and the one-arg form
`zrange(n)` ranges from 0 (`zrange(5)` is `[0, 1, 2, 3, 4]`). Both accept an
optional step and infer a negative step when `start > end`. The `a..b` and
`a..<b` operators are the inclusive and exclusive range-literal equivalents;
`collections.range` is the lazy exclusive form.

## Range methods and properties

Range literals produce first-class values with methods and read-only properties.

<!-- doctest:skip (value/method REPL showcase, not a full program) -->
```gb
let r = 0..10 by 2;

r.length()       # 6
r.isEmpty()      # false
r.contains(4)    # true
r.contains(3)    # false
r.first()        # 0
r.last()         # 10
r.toList()       # [0, 2, 4, 6, 8, 10]

r.start          # 0
r.end            # 10
r.step           # 2
```

`toList()` materialises the range into a list. Avoid it for large ranges -
iterate with `for-in` instead.

`first()` and `last()` return `null` for empty ranges (e.g. `5..3`).

A range converts to a string via `as string` or interpolation:

```gb
io.println(r as string);         # 0..10 by 2
io.println("range: ${r}");       # range: 0..10 by 2
```

## Destructuring

```gb
let [first, second] = pair;
let {name, age} = person;

for (key, value in data.items()) {
    io.println(key);
}
```

Destructuring is best for small, well-known shapes. For request bodies, decoded
JSON, or optional fields, check membership first so errors are clearer:

```gb
if (person.hasKey("name")) {
    let name = person["name"];
    io.println(name);
}
```

## Comprehensions

Comprehensions build a list, set, or dict from an iterable in one
expression instead of an explicit loop + accumulator. The general shape:

```
[ body for binder in iterable (if cond)* ]
{ body for binder in iterable (if cond)* }
{ key: value for binder in iterable (if cond)* }
```

A list comprehension uses `[...]`, a set comprehension uses `{body}`,
and a dict comprehension uses `{key: value}` - the parser picks the
form from the brackets and the presence of `:`.

```gb
let squares  = [x * x for x in [1, 2, 3, 4]];        # [1, 4, 9, 16]
let evens    = [x for x in [1, 2, 3, 4] if x % 2 == 0];  # [2, 4]
let uniqLens = {s.length() for s in ["a", "bb", "a"]};   # set{1, 2}
let byScore  = {p.name: p.score for p in players};
```

### Filters

A comprehension can carry zero or more `if` filters after the `for`. They
chain as logical `AND` - the body fires only when every filter passes.

```gb
let primes = [x for x in range(2, 20) if x > 1 if x % 2 != 0 if x % 3 != 0];
```

### Nested iteration

A comprehension can carry more than one `for` clause. Successive clauses
nest, with the rightmost varying fastest, matching the equivalent loop.

```gb
let products = [x * y for x in [1, 2, 3] for y in [10, 20]];
# [10, 20, 20, 40, 30, 60]
```

Filters can sit between for-clauses; each filter applies at its position in
the iteration nesting.

```gb
[x * y for x in xs if x > 0 for y in ys if y > 0]
```

### Typed binders and destructuring

The `for` binder accepts the same forms as the corresponding `for-in`
loop: an untyped identifier, a typed identifier, or a comma-separated
binder list that destructures a two-element value.

```gb
[x * 2 for int x in [1, 2, 3]]
[k + "=" + (v as string) for k, v in {"a": 1, "b": 2}.items()]
```

### Result types

A list comprehension produces `list<T>`, a set comprehension produces
`set<T>`, and a dict comprehension produces `dict<K, V>`. Element types
are inferred from the body expression and the binder.

### What comprehensions iterate over

The same iterables the `for-in` loop accepts: `list`, `range`,
generator-returning calls, classes that implement the iterator protocol,
1.0.6-era `streams`, and (1.16.0) `dict` (insertion-ordered `[key, value]`
pairs, destructurable into two binders), `set` (sorted `toList()` order),
and `string` (per character, matching `.chars()`):

```gb
[k + "=" + (v as string) for k, v in {"a": 1, "b": 2}]   # ["a=1", "b=2"]
[n * 2 for n in {3, 1, 2}]                               # [2, 4, 6]
{c for c in "abca"}                                      # set{"a", "b", "c"}
```

`.items()`, `.keys()`, and `.chars()` remain available when you want the
intermediate list itself.

### Generator comprehensions

The lazy `(expr for x in xs)` form is not supported in 1.6.0; build a
list with `[...]` and call `.lazy*` higher-order helpers on it, or use a
`generator` function for lazy production.

## Match

`match` dispatches on a value, comparing it against a sequence of `case`
patterns. It works as either a **statement** or an **expression** depending
on context.

### Match expression

When assigned to a variable or used inside a larger expression, `match`
produces a value. Every branch must end with a semicolon and produce a value.
A trailing `;` after the closing `}` marks it as an expression statement:

```gb
let label = match (status) {
    case 200 => "ok";
    case 404 => "missing";
    default  => "error";
};
```

The entire `match (status) { ... }` evaluates to the value of the matched
branch. Use `default` (or `case _`) to ensure every possible input is covered;
a `MatchError` is thrown if no case matches and there is no default.

Match expressions can appear anywhere a value is expected:

```gb
io.println(match (x % 2) {
    case 0 => "even";
    default => "odd";
});
```

### Match statement

When `match` appears as a top-level statement - not assigned or used as a
value - each branch executes an action and there is no trailing `;` after `}`:

```gb
match (command) {
    case "serve"   => startServer();
    case "migrate" => runMigrations();
    default        => showHelp();
}
```

The distinction is syntactic: expression `match` is terminated by `;` after
`}` and the whole expression has a type; statement `match` is not and produces
no value.

### Multi-statement branches

An arm is a single expression - there is no block-body arm. When a branch needs
more than one step, call a helper that performs them and returns the value:

```gb
let report = match (status) {
    case "ok" => summarize(results);
    default   => "failed";
};
io.println(report);
```

### Or-patterns

A `case` can list alternates separated by `|`; the case matches when
any one of them does. Alternates are bindless - they cover literals,
bare types (or unions), and enum variants without payload.

```gb
match (x) {
    case 1 | 2 | 3 => io.println("small");
    case Color.Red | Color.Blue => io.println("warm-ish");
}

func numeric(any v): string {
    return match (v) {
        case int | float | decimal => "numeric";
        case string | bytes        => "text";
        default                    => "other";
    };
}
```

Bare-type forms use Geblang's existing union-type syntax
(`int | float`), so they tolerate generic arguments and nullable
markers - `case ?string | bytes` is valid. Literal and enum
alternates use `|` directly between the patterns.

Guards apply to the whole or-pattern: `case A | B if (cond) => ...`
means "match A or B, then check cond".

Bindings inside an or-pattern are not supported in 1.6.0; use
separate cases when a branch needs to bind a value, or a single
typed case with an internal `if/else`.

### Guard clauses

An `if` guard filters a case with an additional boolean condition:

```gb
match (score) {
    case int n if (n >= 90) => io.println("A");
    case int n if (n >= 70) => io.println("B");
    default                 => io.println("C");
}
```

### Pattern matching with types

`case` can match by type, binding the value to a name if it matches:

```gb
match (value) {
    case int n    => io.println("int: " + (n as string));
    case string s => io.println("string: " + s);
    case null     => io.println("null");
    default       => io.println("other");
}
```

### Enum payload destructuring

Enum variants with associated values can be destructured in `case`:

```gb
enum Result { Ok(string), Err(string) }

let text = match (result) {
    case Result.Ok(string value)   => value;
    case Result.Err(string message) => "error: " + message;
};
```

### List patterns

A bracketed pattern matches a list of exactly that length and
binds each element. Bindings may be typed (the element must match)
or untyped (matches any value); `_` is a wildcard that skips
binding. Length mismatch and type mismatch both fall through to
the next case.

Elements can also be literals (1.16.0): a non-identifier element
matches by equality instead of binding, so a pattern can pin some
positions and capture the rest. Numbers (including negatives),
strings, bools, and `null` all work:

```gb
let cmd = ["go", 50];
let action = match (cmd) {
    case ["go", n] if (n > 10) => "fast ${n}";
    case ["go", n]             => "slow ${n}";
    case ["stop"]              => "halt";
    case [1, x, 3]             => "sandwiched ${x}";
    default                    => "unknown";
};
```

```gb
let pair = [3, 7];
let label = match (pair) {
    case [int x, int y] if (x > y)   => "first wins";
    case [int x, int y] if (x == y)  => "tie";
    case [int x, int y]              => "second wins";
    default                          => "no match";
};
```

Mixed-type rows and wildcard slots:

```gb
let mixed = ["ada", 37];
let greet = match (mixed) {
    case [string name, int age]      => name + " (" + (age as string) + ")";
    default                          => "unknown";
};

let xs = [99, 100];
let first = match (xs) {
    case [int kept, _]               => kept;     # discard second element
    default                          => 0;
};
```

A non-list value (string, dict, int, ...) never matches a list
pattern; the case falls through to whichever later case (typed,
literal, or default) can handle it.

```gb
let value = "hello";
match (value) {
    case [int a, int b] => io.println("a pair");
    case string s       => io.println("a string: " + s);
}
# prints: a string: hello
```

### Exhaustiveness checking

When a `match` subject is an enum, `geblang check` verifies that every
variant is handled. A match that omits a variant and has no `default`
case is reported as `warning[match-nonexhaustive]`, listing the missing
variants:

```gb
enum Color { Red, Green, Blue }

func describe(Color c): string {
    return match (c) {
        case Color.Red   => "red";
        case Color.Green => "green";
    };
}
# geblang check: warning[match-nonexhaustive]: match on enum 'Color'
# is not exhaustive: missing Blue (add the missing case(s) or a 'default:' case)
```

Add the missing cases, or a `default` (or a `case Color c` catch-all),
to make it exhaustive. A variant handled only by a guarded case
(`case Color.Blue if (...)`) still counts as missing, since the guard
may be false at runtime. The check is advisory: a non-exhaustive match
still runs and throws `MatchError` only if an unhandled value reaches
it.

## Defer

`defer` registers a call to run when the surrounding function or top-level
script exits. Deferred calls run in last-in, first-out order.

```gb
func run(): void {
    defer io.println("done");
    io.println("working");
}
```

Arguments to deferred calls are evaluated when the `defer` statement is
executed. A spread argument (`defer f(...xs)`) freezes which list `xs`
refers to at that point; a later reassignment of `xs` does not change what
runs, but an in-place mutation of the same list does, since the deferred
call reads the list's elements when it fires.

`defer` is especially useful with files, sockets, database transactions, and
locks:

```gb
func writeLocked(any file, string text): void {
    io.lock(file);
    defer io.unlock(file);
    io.writeln(file, text);
}
```
