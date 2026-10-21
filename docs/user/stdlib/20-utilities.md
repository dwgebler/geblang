# Utility Modules

These modules are written in Geblang and distributed under `stdlib/`. Import
them like any other module - the runtime resolves them by name.

---

## freeze - Immutability

Import: `import freeze;`

The `freeze` module makes collections and class instances immutable at runtime.
Frozen values raise `ImmutableError` on any mutation attempt. `ImmutableError`
is a subclass of `Error` and is catchable with `try/catch`.

### Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `freeze.shallow(v)` | same type | Returns a frozen copy of `v`. Lists, dicts, sets, and class instances are frozen; nested collections are not. Primitives are returned unchanged. |
| `freeze.deep(v)` | same type | Returns a deeply frozen value: all nested collections and instances are frozen recursively. Collections are copied (keeping insertion order and element types); class instances are frozen in place. |
| `freeze.isFrozen(v)` | `bool` | Returns `true` if `v` is frozen or is a primitive (always immutable). |

All three functions work on both the evaluator and VM execution paths.

### Sharing frozen values between tasks

A deeply frozen value can never change, so concurrent tasks and server
requests may read the same one without copying or locking. This is the way to
share a large read-only structure, such as a compiled configuration, across
requests: build it once, `freeze.deep` it, and put it in a `store.Store`. The
store hands out deeply frozen values by reference instead of copying them (see
`05-async`).

```gb
import freeze;
import store;

let shared = store.Store();
shared.set("rules", freeze.deep(loadRules()));

# In any request or task: no copy is made.
let rules = shared.get("rules");
```

A value only counts as deeply frozen when everything inside it is frozen or
primitive. A function value inside it (which may capture changing state), or
a `freeze.shallow` value with mutable contents, makes the store copy it as
usual.

### Mutation guards

Once frozen, mutation raises `ImmutableError`:

```gb
import freeze;

let x = [1, 2, 3];
x = freeze.shallow(x);

x[0] = 99;   # throws ImmutableError: cannot modify frozen list
```

`ImmutableError` is catchable:

```gb
import freeze;
import io;

let x = freeze.shallow({"key": "value"});
try {
    x["key"] = "changed";
} catch (ImmutableError e) {
    io.println("caught: " + e.message);
}
```

### `.copy()`: mutable shallow copy

All collection types support a `.copy()` method that returns an unfrozen
shallow copy, even when the original is frozen:

```gb
import freeze;

let frozen = freeze.shallow([1, 2, 3]);
let mutable = frozen.copy();
mutable[0] = 99;   # ok (mutable is a new, unfrozen list)
```

### `const` auto-freeze

Declaring a variable with `const` automatically shallow-freezes collection
values at the point of declaration:

```gb
const config = {"host": "localhost", "port": 8080};
config["host"] = "example.com";   # throws ImmutableError
```

Primitives assigned with `const` are unchanged (they are already immutable).

### `@immutable` class decorator

Apply `@immutable` to a class to freeze every instance after its constructor
returns. Field reads still work; any field assignment raises `ImmutableError`:

```gb
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}

Point p = Point(3, 4);
io.println(p.x);   # 3
p.x = 99;          # throws ImmutableError: cannot modify frozen instance of Point
```

Use the *wither* pattern to produce modified copies from immutable classes:

```gb
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
    func withX(int x): Point { return Point(x, this.y); }
    func withY(int y): Point { return Point(this.x, y); }
}

Point a = Point(1, 2);
Point b = a.withX(10);   # new immutable Point(10, 2)
```

---

## clone - Deep Copies

Import: `import clone;`

`.copy()` is shallow: the new container shares the nested containers and
objects of the original, so mutating something nested shows through both.
When you need a fully independent copy, use a deep copy.

| Function / method | Returns | Description |
|-------------------|---------|-------------|
| `clone.deep(v)` | same type | Deep copy of any value. Containers and class instances are cloned recursively; primitives are returned unchanged; resource handles (sockets, files, DB connections) are passed through, not duplicated. |
| `xs.deepCopy()` | same type | Deep-copy method on lists, dicts, and sets - the deep counterpart of `.copy()`. |

```gb
import clone;
import io;

let a = [[1], [2]];
let b = a.deepCopy();
b[0].append(9);
io.println(a);   # [[1], [2]]   (unchanged)
io.println(b);   # [[1, 9], [2]]

class Box { int v; func Box() { this.v = 1; } }
let original = Box();
let copy = clone.deep(original);
copy.v = 99;
io.println(original.v);   # 1   (independent)
```

`clone.deep` accepts any value, including user-defined objects and nested
graphs (self-referential lists and object cycles are handled). It works
identically on the evaluator and the VM.

---

## maps - Dict-Like Objects

Import: `import maps;`

Implement the `maps.DictInterface` interface to give a class dict-like
behaviour. You provide two abstract methods - `__index(key)` (backs `obj[key]`)
and `keys()` (the key set) - and inherit `contains`, `get(key, default)`,
`values`, `length`, `isEmpty`, and `__contains` (so `key in obj` works) as
default implementations. `get(key, default)` returns the value for `key` or
`default` when absent. For a mutable map, also define `__setIndex(key, value)` on the
class; it is intentionally not part of the interface, so read-only maps can omit
it.

```gb
import maps;
import io;

class CaseInsensitiveHeaders implements maps.DictInterface {
    dict<string, string> store;
    func CaseInsensitiveHeaders() { this.store = {}; }
    func __index(any key): any { return this.store.get((key as string).lower()); }
    func __setIndex(any key, any value): void { this.store.set((key as string).lower(), value as string); }
    func keys(): list<any> { return this.store.keys(); }
}

let h = CaseInsensitiveHeaders();
h["Content-Type"] = "application/json";
io.println(h["content-type"]);        # application/json
io.println("CONTENT-TYPE" in h);      # true
io.println(h.length());               # 1
io.println(h.get("missing", "n/a"));  # n/a
```

The default methods call only the abstract `keys()` / `__index()` you provide,
so an implementer needs just those two (plus `__setIndex` for writes). Works
identically on the evaluator and the VM.

---

## result - Explicit Success/Failure Values

Import: `import result;`

`Result<T,E>` represents either a success value (`ok`) or a failure value
(`err`). Use it when you want to model operations that can fail without
relying on exceptions for control flow.

### Constructors

| Function | Returns | Description |
|----------|---------|-------------|
| `result.ok(value)` | `Result<T,E>` | Wrap a success value |
| `result.err(error)` | `Result<T,E>` | Wrap a failure value |

### Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `isOk()` | `bool` | `true` for success results |
| `isErr()` | `bool` | `true` for error results |
| `unwrap()` | `T` | Return the success value; throws `ValueError` if this is an error |
| `unwrapOr(fallback)` | `T` | Return the success value, or `fallback` if this is an error |
| `unwrapErr()` | `E` | Return the error value; throws `ValueError` if this is a success |

### Example

```gb
import result;
import io;

func divide(int a, int b): Result<int, string> {
    if (b == 0) {
        return result.err("division by zero");
    }
    return result.ok(a / b);
}

let r = divide(10, 2);
if (r.isOk()) {
    io.println(r.unwrap());      # 5
} else {
    io.println(r.unwrapErr());
}

io.println(divide(5, 0).unwrapOr(-1));  # -1
```

Use `Result` at module or service boundaries where callers need to distinguish
success from specific error conditions. For unexpected runtime failures, prefer
`throw`/`catch`.

---

## option - Optional Values

Import: `import option;`

`Option<T>` represents a value that may or may not be present. It makes
absence explicit in the type rather than relying on `null` checks.

### Constructors

| Function | Returns | Description |
|----------|---------|-------------|
| `option.some(value)` | `Option<T>` | Wrap a present value |
| `option.none()` | `Option<T>` | An absent value |
| `option.ofNullable(value)` | `Option<T>` | `some(value)` when non-null, otherwise `none()` |

### Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `isSome()` | `bool` | `true` when a value is present |
| `isNone()` | `bool` | `true` when no value is present |
| `unwrap()` | `T` | Return the value; throws `ValueError` if absent |
| `unwrapOr(fallback)` | `T` | Return the value, or `fallback` if absent |
| `orNull()` | `?T` | Return the value or `null` |

### Example

```gb
import option;
import io;

func findUser(int id): option.Option<string> {
    dict<int, string> db = {1: "Ada", 2: "Grace"};
    return option.ofNullable(db.get(id));
}

let user = findUser(1);
io.println(user.isSome());                    # true
io.println(user.unwrap());                    # Ada
io.println(findUser(99).unwrapOr("unknown")); # unknown
io.println(findUser(99).orNull());            # null
```

`option.ofNullable` is the bridge between code that returns nullable values
and code that consumes `Option`. Use it at the boundary where a nullable result
enters option-aware code.

---

## schema.Validator - Reusable Validators

Import: `import schema.validator as sv;`

`Validator` wraps a schema dict into a reusable object with structured error
reporting, built on top of the native `schema.validate` function. Create a
validator once and call it repeatedly for different values.

See the [schema module reference](07-data-formats.md#schema-validation) for the
full list of supported schema keywords (`type`, `required`, `properties`,
`items`, `minimum`, `maximum`, `minLength`, `maxLength`, `enum`, etc.).

### Constructors

| Function | Returns | Description |
|----------|---------|-------------|
| `sv.of(schemaDict)` | `Validator` | Create a reusable validator from a schema dict |
| `sv.validate(value, schemaDict)` | `dict` | One-shot validate without a Validator instance |

### Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `validate(value)` | `dict` | Raw `{valid: bool, errors: list<string>}` result |
| `isValid(value)` | `bool` | `true` when validation passes |
| `errors(value)` | `list<string>` | All validation error messages |
| `fieldErrors(value, field)` | `list<string>` | Errors matching a specific field path prefix |

### Example

```gb
import schema.validator as sv;
import io;

let userSchema = sv.of({
    "type": "object",
    "required": ["name", "email"],
    "properties": {
        "name":  {"type": "string", "minLength": 1},
        "email": {"type": "string"},
        "age":   {"type": "number", "minimum": 0}
    }
});

let valid = {"name": "Ada", "email": "ada@example.com", "age": 37};
io.println(userSchema.isValid(valid));  # true

let bad = {"name": "", "age": -1};
io.println(userSchema.isValid(bad));        # false
io.println(userSchema.errors(bad));         # ["name: minLength 1", "age: minimum 0", ...]
io.println(userSchema.fieldErrors(bad, "name")); # errors for the "name" field only
```

In a request handler, check `isValid` first and return error details from
`errors()` or `fieldErrors()` when validation fails.

---

## random - Deterministic pseudo-random number generation

Import: `import random;`

The `random` module is a seedable PRNG suitable for simulation, sampling,
shuffling, procedural generation, fuzz inputs, and tests where
reproducibility matters. **It is not cryptographically secure**. For
security tokens, session IDs, salts, OTPs, and anything an attacker
shouldn't be able to predict, use the `secrets` module instead (see
`12-security`).

### Module-level helpers

These call into a process-wide default generator. Seed it with
`random.seed(n)` for reproducibility.

| Function | Returns | Description |
|----------|---------|-------------|
| `random.seed(n)` | `null` | Reseed the default generator |
| `random.next()` | `int` | Random non-negative int63 |
| `random.intRange(min, max)` | `int` | Uniform int in `[min, max)` |
| `random.float()` | `float` | Uniform float in `[0.0, 1.0)` |
| `random.bool()` | `bool` | Uniform coin flip |
| `random.choice(list)` | element | Random element from a non-empty list |
| `random.shuffle(list)` | `list` | New list with elements in random order; original is unchanged |

```gb
import random;
import io;

random.seed(42);
io.println(random.intRange(1, 7));           # int in 1..6
io.println(random.float());                  # float in 0..1
io.println(random.choice(["red", "green", "blue"]));
io.println(random.shuffle([1, 2, 3, 4, 5]));
```

### Independent generators

For threads, tests, or parallel simulations that must not share state
with the default generator, create dedicated instances with
`random.Generator(seed)`. The same module-level helpers accept the
generator as their first argument:

```gb
import random;
import io;

let rng = random.Generator(99);
io.println(random.intRange(rng, 0, 100));
io.println(random.choice(rng, ["heads", "tails"]));
io.println(random.shuffle(rng, [1, 2, 3]));

# Two generators with the same seed produce the same stream.
let a = random.Generator(7);
let b = random.Generator(7);
io.println(random.next(a) == random.next(b));   # true
```

---

## functools - Functional Composition

Import: `import functools;`

Small higher-order helpers for composing and reshaping callables.

### Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `functools.pipe(fns)` | `func` | Returns a function that applies `fns` left-to-right: `pipe([f, g, h])(x)` is `h(g(f(x)))`. An empty list returns the identity function. |
| `functools.compose(fns)` | `func` | Returns a function that applies `fns` right-to-left: `compose([f, g, h])(x)` is `f(g(h(x)))`. |
| `functools.partial(fn, ...bound)` | `func` | Returns a function that calls `fn` with `bound` prepended to its arguments. |
| `functools.memoize(fn, max = 128)` | `func` | Returns an LRU-cached wrapper. Cache keys are derived from the JSON representation of the args, so memoized functions must take JSON-serialisable arguments. |

```gb
import functools;
import io;

let inc    = func(int n): int { return n + 1; };
let double = func(int n): int { return n * 2; };
let square = func(int n): int { return n * n; };

let f = functools.pipe([inc, double, square]);
io.println(f(1));                      # ((1+1)*2)^2 = 16

let add = func(int a, int b): int { return a + b; };
let add10 = functools.partial(add, 10);
io.println(add10(3));                  # 13

int calls = 0;
let expensive = func(int n): int { calls = calls + 1; return n * n; };
let fast = functools.memoize(expensive);
fast(5); fast(5); fast(5);
io.println(calls);                     # 1 - only first call executed
```

`memoize` is best for pure functions of primitive or
collection-of-primitive arguments. For non-serialisable args (instances
of classes, generators, callables) write a thin wrapper that converts
to a key explicitly.

For binding arguments in any position (not just leading), use the native `_` placeholder syntax described in the [Functions and Callables](../05-functions-callables.md#partial-application) chapter.
