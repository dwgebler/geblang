# Types

## Primitive Types

| Type | Description | Operations |
|------|-------------|------------|
| `int` | Arbitrary-precision signed integer (no overflow). | [Numeric methods](#numeric-methods) |
| `decimal` | Exact base-10 number (arbitrary precision, no binary rounding). | [Numeric methods](#numeric-methods) |
| `float` | 64-bit IEEE-754 binary floating point. | [Numeric methods](#numeric-methods) |
| `bool` | `true` or `false`. | [Boolean methods](#boolean-methods) |
| `string` | Immutable UTF-8 text. A single character is just a length-one string; there is no separate `char` type. | [String methods](stdlib/09-text.md) |
| `bytes` | Immutable sequence of raw bytes. | [bytes instance methods](#bytes-instance-methods) |
| `null` | The absence of a value. | - |
| `any` | A dynamic value at typed boundaries (decoded JSON, request data). | - |

Collection types:

| Type | Description | Operations |
|------|-------------|------------|
| `list<T>` / `T[]` | Ordered, growable sequence. | [Collection methods](stdlib/08-collections.md) |
| `dict<K, V>` | Insertion-ordered key/value map. | [Collection methods](stdlib/08-collections.md) |
| `set<T>` | Unordered collection of unique values. | [Collection methods](stdlib/08-collections.md) |

Every method list in this chapter is the set the engine actually
recognises - the same source powers `dir(value)`, editor completion,
and `geblang check`, so completion never lags the language.
Primitive method names are case-sensitive: use `isEmpty()`, `toInt()`,
and `startsWith()` exactly as documented.

Runtime and framework types include `Type`, `Task<T>`, `generator<T>`,
`iterable<T>`, classes, interfaces, and enums.

`func` (or its aliases `callable` and `function`) is the catch-all type for
any callable value - a function, lambda, method reference, or closure. Use it
when storing a function in a field or accepting one as a parameter:

```gb
class Scheduler {
    func cb;
    func Scheduler(func cb) { this.cb = cb; }
    func fire(): void {
        let handler = this.cb;
        handler();
    }
}
```

Use concrete collection element types whenever possible:

```gb
list<string> names = ["Ada", "Grace"];
dict<string, int> scores = {"Ada": 10};
```

Use `any` at dynamic boundaries such as decoded JSON, request dictionaries, and
extension responses. Convert or validate it before passing values deeper into
typed application code.

## Numeric methods

`int`, `decimal`, and `float` share a core set of numeric operations.
The rounding family returns the *same* type (keeping a `decimal` a
`decimal`), unlike `math.floor/ceil/round`, which return `int`. See
[stdlib/11-math-datetime](stdlib/11-math-datetime.md) for the `math`
module.

Common to all three numeric types:

| Method | Returns | Description |
|--------|---------|-------------|
| `abs()` | same type | Absolute value. |
| `sign()` | `int` | `-1`, `0`, or `1` by sign. |
| `clamp(lo, hi)` | same type | Constrain to the range `[lo, hi]`. |
| `isPositive()` | `bool` | True if `> 0`. |
| `isNegative()` | `bool` | True if `< 0`. |
| `isZero()` | `bool` | True if `== 0`. |
| `toString()` | `string` | Text form (see per-type notes below). |

`int` also has:

| Method | Returns | Description |
|--------|---------|-------------|
| `isEven()` | `bool` | True if divisible by 2. |
| `isOdd()` | `bool` | True if not divisible by 2. |
| `toString(base = 10)` | `string` | Format in base 2-36. |
| `toDecimal(places = 0)` | `decimal` | Convert, optionally rounded. |
| `toFloat()` | `float` | Convert to float. |

`decimal` and `float` add the value-keeping rounding family (each takes
an optional `places`, default 0, and rounds half away from zero for
`round`):

| Method | Returns | Description |
|--------|---------|-------------|
| `round(places = 0)` | same type | Round to `places` digits. |
| `floor(places = 0)` | same type | Round down to `places` digits. |
| `ceil(places = 0)` | same type | Round up to `places` digits. |
| `truncate(places = 0)` | same type | Round toward zero to `places` digits. |

`float` additionally has `isNaN()` and `isInf()`. `decimal`
additionally has `format(scale)` and `toString(scale = 10)` for
fixed-scale rendering (a `decimal` is an exact value with no stored
scale, so the default display is 10 places; pass a scale for more).

Both `float` and `decimal` have `isInt()`, which reports whether the
value is a whole number. `float.isInt()` is true only for a finite
value equal to its truncation (`3.0f.isInt()` is `true`, `3.5f`, `NaN`,
and infinities are `false`); `decimal.isInt()` is true when the exact
value has denominator 1.

A numeric literal like `2.9` is a `decimal`; floats are written with an
`f` suffix (`2.9f`). Scientific notation follows the same rule: `1e3`
is the exact decimal `1000`, while `1e3f` is a `float`. Decimals display
at 10 places by default (they store an exact value, not a scale), so use
`toString(scale)` or a format spec to control the rendered precision:

```gb
import io;
let r = (2.567 as decimal).round(2);
io.println(r);                  # 2.5700000000  (rounded to 2 places; 10-place default display)
io.println(r.toString(2));      # 2.57          (pick the display scale)
io.println((2.9).floor());      # 2.0000000000  (2.9 is a decimal; floor keeps the type)
io.println((-7).sign());        # -1
io.println((12).clamp(0, 10));  # 10
io.println((4).isEven());       # true
io.println((255).toString(16)); # ff
io.println((3.1415926536 as decimal).toString(13));  # 3.1415926536000
```

Rendering at a fixed scale rounds. `format(scale)` and `toString(scale)`
round half away from zero to the requested number of places, which is the
usual expectation for displaying a value. To scale a value *down* without
rounding, truncate it to that scale first and then render - the value
already sits at the target scale, so the render step has nothing left to
round:

```gb
import io;
decimal price = 1.246;
io.println(price.format(2));                 # 1.25  (rounds when rendering)
io.println(price.truncate(2).format(2));     # 1.24  (cut off toward zero, then render)
io.println((-1.246).truncate(2).format(2));  # -1.24 (truncates toward zero, not -1.25)
```

The same composition works with `toString`: `price.truncate(2).toString(2)`
yields `"1.24"`. `truncate` does the cutting-off; `format` / `toString`
just choose how many places to show.

### Mixing numeric types

The three numeric types interact by a single rule, designed to protect the
exact types from silent precision loss:

- `int` and `decimal` mix freely and stay exact: `3 + 2.5` is a `decimal`,
  `1 / 2` is the exact `decimal` `0.5` (not truncated).
- `int` and `float` mix in arithmetic by promoting the `int` to `float`:
  `3 + 2.5f` is the `float` `5.5`.
- `decimal` and `float` do **not** mix in arithmetic - `2.5 + 2.5f` is an error,
  reported by `geblang check` when both operand types are known and at runtime
  otherwise. A `decimal` is exact and a `float` is not, so the result type would
  be ambiguous and lossy. Cast one side, choosing the tradeoff: `as float` is
  fast but drops the decimal's exactness; `as decimal` keeps an exact type but
  adopts the float's binary imprecision.

Because `/` is true division, its result is a `decimal` (or `float`) even when it
divides evenly, so `int n = a / b` is a compile-time error. Use `//` (floor
division) for an integer result, `int n = a // b`, or truncate explicitly with
`int n = (a / b) as int`.

Comparisons (`== != < > <= >=`) and membership (`in`, `.contains()`) work across
*all* numeric types and compare by exact value - they never error and never lose
precision. This means they tell the truth: `3 == 3.0f` is `true`, `2.5 == 2.5f`
is `true` (2.5 is exactly representable as a float), but `0.1 == 0.1f` is
`false`, because the binary float `0.1f` is not exactly one tenth.

```gb
io.println(3 + 2.5f);     # 5.5    (int promoted to float)
io.println(3 + 2.5);      # 5.5000000000  (decimal, exact)
# io.println(2.5 + 2.5f); # error: cannot mix decimal and float in +
io.println(3 == 3.0f);    # true
io.println(0.1 == 0.1f);  # false  (they genuinely differ)
```

## Boolean methods

| Method | Returns | Description |
|--------|---------|-------------|
| `not()` | `bool` | Logical negation. |
| `toString()` | `string` | `"true"` or `"false"`. |
| `toInt()` | `int` | `true` -> 1, `false` -> 0. |

## Nullability

Types are non-null by default. Prefix with `?` to allow `null`:

```gb
string name = "Ada";
?string maybeName = null;

if (maybeName != null) {
    io.println(maybeName.length());
}
```

A nullable value must be checked or handled before calling methods that require
a non-null value. Optional chaining is useful when absence is acceptable:

```gb
let city = user?.address?.city ?? "unknown";
```

## Union types

A parameter or return type can list alternatives separated by `|`. The
caller may pass any value matching one of the branches; the runtime
enforces "any branch matches".

```gb
func get(int | string id): User {
    if (id instanceof string) { return User.byName(id as string); }
    return User.byId(id as int);
}

get(42);        # int branch
get("ada");     # string branch
```

Union types compose with nullability: `?int | string` is a three-way
union of `int`, `string`, and `null`. You can write the `?` on the
union as a whole or on individual branches; the meaning is the same.

```gb
func parseAge(?int | string raw): int {
    if (raw == null)            { return 0; }
    if (raw instanceof int)     { return raw as int; }
    return (raw as string).toInt();
}
```

Returns work the same way. The body must produce a value matching one
of the declared branches; the caller can narrow with `instanceof` /
`as` or just consume the union directly when an `any`-typed slot
suffices.

```gb
func lookup(string key): User | NotFoundError {
    let row = db.findByKey(key);
    if (row == null) { return NotFoundError("no user with key " + key); }
    return User.fromRow(row);
}
```

When a mismatching value reaches the function boundary, the runtime
throws a `RuntimeError` with the expected and actual types:

```
get expects int | string for parameter 'id', got bool
```

Catch with the standard `try` / `catch (RuntimeError e)` form - but only when
the mismatch is genuinely runtime-opaque (the value arrives through an
`any`-typed or otherwise dynamic path). A bad value the compiler can already
see at the call site (`get(true)`) is reported as a static error and aborts
before the `try` body runs, so it is not catchable.

The intersection operator `&` is also supported in parameter and
return positions: a value must match every branch. It's mainly useful
for interface intersection, e.g. `Comparable & Hashable`.

Variable-level unions (`let x: int | string;`) are deferred to a
future release; today the union form is only enforced at the function
boundary. Inside a function body, a union-typed parameter can be
narrowed with `instanceof` and re-bound via `as`.

## Casts And Type Checks

```gb
let text = value as string;
io.println(value instanceof string);
io.println(user instanceof User);
```

`typeof(value)` returns a `Type` value. Compare it with a built-in type name,
class name, or `.type` selector for exact runtime type equality; use
`instanceof` when subclasses or interface implementations should match:

```gb
io.println(typeof("x"));             # string
io.println(typeof(typeof("x")));     # Type
io.println(typeof("x") == string);   # true
io.println(typeof("x") == "string"); # true (compare to a type-name string)
io.println("x" instanceof string);   # true

class User {}
User u = User();
io.println(typeof(u) == User);     # true
io.println(typeof(u) == "User");   # true
io.println(u.type == User);        # true (shorthand for typeof)
io.println("x".type == string);    # true
io.println(string.type);           # string
```

The `.type` selector is a shorthand for `typeof` - `expr.type` is equivalent to
`typeof(expr)`. Primitive type names (`string`, `int`, `bool`, `decimal`, etc.)
and class names can be used directly as type values in comparisons.

`Type` comparisons match by NAME: `typeof(x) == SomeClass` is true for any
class named `SomeClass`, even one declared in a different module. `instanceof`,
by contrast, is module-exact and is the precise runtime check: its right-hand
side resolves to the specific class or interface declaration in scope and
matches by identity, walking the value's real parent chain (subclasses and
interface implementations still match). `x instanceof a.Config` is false for an
instance of a same-named `b.Config` from another module. Class values are
likewise module-exact (`a.Config == b.Config` is false for two same-named
classes), and `reflect.location(SomeClass)` reports the declaring module. When
two modules declare same-named classes, use `instanceof` (or a class-value
comparison) rather than `typeof` to distinguish them.

A `Type` is its own value, not a string, so it does not concatenate with
`+` directly. Compare it to a type or a type-name string (both shown above);
to build a message from it, convert with `as string` or interpolate it:

```gb
io.println(typeof(u) as string);   # "User"
io.println("got a ${typeof(u)}");  # "got a User"
# "got a " + typeof(u)             # error: a Type is not a string
```

### Conversion methods

`value as type` is the idiomatic cast. Primitives also carry conversion
methods (`toInt`, `toDecimal`, `toFloat`, `toString`, `toBool`) that do
the same thing but chain cleanly and, in two cases, offer finer control:

- `toDecimal(places)` rounds to a precision while converting, returning a
  `decimal` (`math.pi().toDecimal(4)` -> `3.1416`). With no argument it
  is a plain cast.
- `toInt(base)` on a string parses in the given base.

To test before converting (instead of catching a thrown error), a
string carries three non-throwing predicates:

- `isInt()` is `true` exactly when `toInt()` would succeed (same parse:
  signs, `0b`/`0o`/`0x` bases, and `_` separators).
- `isDecimal()` is `true` exactly when `toDecimal()` would succeed.
- `isNumeric()` is `true` when the string parses as an int or a decimal
  (`isInt() || isDecimal()`).

The value-keeping rounding methods (`round`, `floor`, `ceil`,
`truncate`) and the `sign` / `clamp` helpers also live on the numeric
types; see [Numeric methods](#numeric-methods) above for the full list.

### Generic type parameters and built-in collections

`typeof([1, 2, 3])` returns `list` (not `list<int>`); the base type
name remains the canonical identifier for the kind. There is no
`Type<T>` expression syntax.

The element-type bindings on `list<T>`, `dict<K,V>`, and `set<T>`
are **preserved when the value flows through a typed declaration or
parameter boundary** (since 1.0.2). The runtime attaches a reified
tag that downstream `reflect.typeBindings` and `instanceof` checks
can read:

```gb
import reflect;

let list<int> xs = [1, 2, 3];
reflect.typeBindings(xs);    # {"T": "int"}
xs instanceof list<int>;     # true
xs instanceof list<string>;  # false

# An untagged collection (no typed boundary) has no bindings;
# reflect.typeBindings returns an empty dict and instanceof
# walks the elements structurally.
let raw = [1, 2, 3];
reflect.typeBindings(raw);   # {}
raw instanceof list<int>;    # true (every element is int)
raw instanceof list<string>; # false
```

`any` is universally accepting: every list satisfies `list<any>`,
every dict satisfies `dict<K, any>`, and so on. Union arguments
match elementwise on untagged collections and tag-satisfies on
tagged ones, so both cases below hold:

```gb
[1, "f", true] instanceof list<string|bool|int>;  # true
xs instanceof list<int|string>;                   # true (since 1.5.1)
```

`dict<K,V>` exposes the tag as `{"K": "...", "V": "..."}`;
`set<T>` exposes `{"T": "..."}`.

Type annotations on parameters using generic collections are enforced at typed
function/method call boundaries and typed declaration boundaries:
`list<int>` on a parameter checks that the value is a list **and** that every
element is an `int` when the call is made. The same boundary check applies when
a typed variable declaration is initialized.

```gb
func sumInts(list<int> items): int {
    let total = 0;
    for (item in items) { total = total + item; }
    return total;
}

sumInts([1, 2, 3]);      # ok
sumInts(["a", "b"]);     # runtime type error  -  list<string> does not satisfy list<int>

list<int> nums = [1, 2, 3];   # ok
list<int> bad  = ["a", "b"];  # runtime type error  -  list<string> does not satisfy list<int>
```

The error message names the function (or variable) and describes both the
expected and actual element types, e.g.:

```
sumInts expects list<int> for parameter 'items', got list<string>
type error: cannot assign list<string> to list<int>
```

If you need a custom error message you can inspect elements yourself before
calling or assigning.

These checks do not make primitive collections permanently typed containers.
After a value has been accepted at a boundary, normal collection mutation
methods still operate on the mutable runtime value. Re-check at the next typed
boundary, or validate before mutation when the collection is shared widely.

### Reified generics on user-defined classes

Generic type parameters **are** preserved for user-defined generic classes.
`reflect.typeBindings(instance)` returns a dict mapping each type parameter
name to the concrete type it was bound with:

```gb
import reflect;

class Box<T> {
    T value;
    func Box(T v) { this.value = v; }
}

Box<string> b = Box("hello");
io.println(typeof(b) == Box);              # true
io.println(reflect.typeBindings(b));       # {"T": "string"}
io.println(reflect.typeBindings(b)["T"]);  # string
```

Use `reflect.typeOf(value)` when you want a stdlib function form that can be
passed around like any other callable.

## The bytes Type

`bytes` represents a raw, immutable sequence of octets. It is the correct type
for binary data: file content read in binary mode, cryptographic hashes and
ciphertext, compressed payloads, HTTP request/response bodies that are not
guaranteed to be valid UTF-8, and any value that must survive a round-trip
without text encoding assumptions.

`bytes` is distinct from `string`. A `string` is always valid text encoded as
UTF-8. A `bytes` value is just a sequence of unsigned octets with no inherent
encoding; converting it to `string` requires an explicit call.

### Creating bytes values

Use the `bytes` module to produce `bytes` values:

```gb
import bytes;

let raw  = bytes.fromString("hello");          # encode UTF-8 string to bytes
let hex  = bytes.fromHex("48656c6c6f");        # decode hexadecimal
let b64  = bytes.fromBase64("aGVsbG8=");       # decode Base64
let both = bytes.concat([raw, hex]);           # join two byte sequences
```

### bytes instance methods

`bytes` values expose these methods directly:

| Method | Returns | Description |
|--------|---------|-------------|
| `length()` | `int` | Number of bytes (also available as the `.length` property) |
| `isEmpty()` | `bool` | True when length is zero |
| `get(int index)` | `int` | Byte value at position (0-255) |
| `contains(int byte)` | `bool` | True if the byte value appears anywhere |
| `toString()` | `string` | Decode as UTF-8; throws on invalid bytes |
| `toHex()` | `string` | Lowercase hex representation |
| `toBase64()` | `string` | Standard Base64 encoding |

```gb
import bytes;

let data = bytes.fromString("Geblang");
io.println(data.length);        # 7  (property form)
io.println(data.length());      # 7  (method form)
io.println(data.toHex());       # 4765626c616e67
io.println(data.toBase64());    # R2VibGFuZw==
io.println(data.get(0));        # 71 (ASCII 'G')
```

> The `.length` property works on every sequence-like type: `list`, `dict`,
> `set`, `string`, `bytes`, and `range`. It is identical to calling
> `.length()` and is preferred for readability when no other arguments are
> needed.

### Common patterns

**Cryptography and hashing**: hash and sign functions in the `crypt` module
return `bytes`. Pass them directly to `bytes.toHex()` or `bytes.toBase64()` for
storage or transport:

```gb
import crypt;
import bytes;

let hash = crypt.sha256("password123");
io.println(hash.toHex());     # hex digest
io.println(hash.toBase64());  # Base64 digest
```

**Compression**: `compress.gzip` takes and returns `bytes`:

```gb
import bytes;
import compress;
import io;

let payload  = bytes.fromString("lots of repeated text...");
let packed   = compress.gzip(payload);
let unpacked = compress.gunzip(packed);
io.println(unpacked.toString());
```

**HTTP bodies**: when a response body is not guaranteed to be text, read it as
`bytes` and convert only when you know the encoding:

```gb
import http;
import bytes;
import io;

let resp = http.get("https://example.com/data.bin");
let body = bytes.fromString(resp["body"]);   # or use bodyBytes() on Response
io.println(body.length());
```

**Binary file I/O**: read/write binary files using the `io` module's binary
helpers; the value in memory is `bytes`.

### bytes vs string summary

| | `string` | `bytes` |
|---|----------|---------|
| Content | Valid UTF-8 text | Raw octets, any content |
| Literal syntax | `"hello"` or `'hello'` | No literal; use `bytes.fromString` / `bytes.fromHex` |
| Length | Characters (Unicode) | Octets |
| Indexing | Not directly - use `.chars()` | `.get(i)` returns `int` (0-255) |
| Concatenation | `+` operator | `bytes.concat([a, b])` |
| Conversion | `value as string` | `.toString()` (UTF-8 decode) / `.toHex()` / `.toBase64()` |

## Type Aliases

```gb
type UserId = string;
type Money = decimal;
type IntList = int[];
```

Aliases document intent but do not create distinct runtime types.

## Ranges and lists

A range expression `start..end` (or `start..<end` exclusive) produces
a `Range` value. Ranges are iterable - `for (x in 1..5) { ... }` walks
the inclusive sequence - and support `.length`, `.contains(n)`,
`.first`, `.last`, and `.toList()`. The list form is the canonical way
to assign a range to a typed declaration:

```gb
let xs = (1..5).toList();     # [1, 2, 3, 4, 5]
list<int> ys = (1..5).toList();
```

For convenience, the top-level `range(start, end[, step])` builtin
returns the list directly without an intermediate `Range`:

```gb
let xs = range(1, 5);            # [1, 2, 3, 4, 5]
let ys = range(10, 2, -2);       # [10, 8, 6, 4, 2]
let zs = range(5, 1);            # [5, 4, 3, 2, 1] (auto-negative step)
```

Both forms are inclusive of both endpoints. Use `..<` for a half-open
range (`(1..<5).toList()` is `[1, 2, 3, 4]`).

A char-range literal `'a'..'e'` evaluates eagerly to a `list<string>`
of single-character entries:

```gb
let letters = 'a'..'e';          # ["a", "b", "c", "d", "e"]
let digits  = '0'..<'5';         # ["0", "1", "2", "3"]
list<string> alphabet = 'a'..'z';   # works in a typed declaration too
```

> **Watch out for the bracket trap.** `['a'..'z']` is a list containing
> *one* element which is itself the char-range list. The char range
> *is* the list; you don't wrap it. Single and double quotes both
> work for the bounds (`"a".."z"` is equivalent to `'a'..'z'`).

Char ranges don't produce a `Range` value - they go straight to the
list, so `.toList()` on the result is a no-op (which `list.toList()`
supports for symmetry).

Geblang has no separate `char` type. Single characters are simply
strings of length one, so the element type of a char range is
`string`.

## Generics

Functions, classes, methods, and interfaces can declare type parameters inside
angle brackets. The type parameter is then available as a type name throughout
the declaration:

```gb
func identity<T>(T value): T {
    return value;
}

class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func get(): T { return this.value; }
}

let b = Box<string>("hello");
io.println(b.get());   # hello
```

### Reified type bindings

Geblang generics are *reified* - type bindings are tracked at runtime, not
erased. This means `instanceof T` works inside generic functions and methods,
and framework code can inspect actual bound types with `reflect.typeBindings`:

```gb
func assertIs<T>(any value): bool {
    return value instanceof T;
}

io.println(assertIs<string>("hello"));   # true
io.println(assertIs<int>("hello"));      # false
```

The parameter is `any value` because the function's purpose is to *test*
the value's type against T at runtime. T is bound from the explicit
`<int>` / `<string>` at the call site, and `instanceof T` consults that
binding inside the body.

Call-site bindings also project through to instances the body constructs
(1.16.0). A generic function returning a generic class reports the
concrete types, whether T was inferred from an argument or passed
explicitly:

```gb
class Pair<A, B> {
    A first;
    B second;
    func Pair(A first, B second) { this.first = first; this.second = second; }
}

func make<T>(T v): Pair<T, T> {
    return Pair(v, v);
}

reflect.typeBindings(make("hello"));   # {"A": "string", "B": "string"}
reflect.typeBindings(make(42));        # {"A": "int", "B": "int"}
```

> **Strict binding.** Explicit type arguments on a generic call replace T
> in every position of the signature - parameters, return type, and the
> body - just like in Kotlin, Swift, Rust, and C#. If you write
> `func id<T>(T value): T { return value; }` then `id<int>("hi")` is a
> type error (T is int; the string argument doesn't satisfy T). When you
> want a function that accepts any value but tests its type, use `any` on
> the parameter as `assertIs` does above.

Type bindings flow through call chains. When you call a method on a generic
class instance, the method sees the concrete binding that was established when
the instance was created:

```gb
class Typed<T> {
    T value;
    func Typed(T v) { this.value = v; }

    func isCorrectType(any candidate): bool {
        return candidate instanceof T;   # T is the concrete type at runtime
    }
}

let t = Typed<string>("Ada");
io.println(t.isCorrectType("Grace"));   # true
io.println(t.isCorrectType(42));        # false
```

### Inspecting type bindings

`reflect.typeBindings(instance)` returns a dict mapping each type parameter
name to its concrete bound type. This is how frameworks discover what types a
container, validator, or wrapper is parameterized with:

```gb
import reflect;

class Container<T> {
    list<T> items = [];

    func add(T item): void {
        this.items.push(item);
    }
}

let c = Container<string>();
io.println(reflect.typeBindings(c));   # {"T": "string"}
```

### Enforcement of explicit type arguments

Explicit type arguments are a contract, not a hint. When a constructor is
called with an explicit `<TypeArgs>` clause, every constructor parameter
typed with a bound type parameter is validated against the binding at
runtime, on both runtimes:

```gb
class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
}

let ok  = Box<string>("hello");   # fine
let bad = Box<string>(42);        # RuntimeError: Box expects T for parameter 'value', got int
```

Subtype arguments still pass (`Pen<Animal>(Dog())` is fine), and a call
without explicit type arguments stays inference-open (`Box(42)` binds
`T = int` from the argument). When the contradiction is statically visible -
a literal or a typed variable against an explicit type argument -
`geblang check` reports it as `error[semantic]` before anything runs, and so
do `geblang run`, `test`, and `build`.

Method parameters typed with a class-level type parameter are enforced the
same way, against the instance's reified bindings:

```gb
let b = Box<string>("hello");
b.put(42);    # RuntimeError: Box.put expects T for parameter 'value', got int
```

This applies to inherited methods too: with `class IntBox extends Box<int>`,
calling `put("nope")` on an `IntBox` throws, because the extends clause bound
`T = int` for every instance. A method's *own* type parameters
(`func tag<U>(U label)`) are unaffected - they bind per call as always.

`instanceof` consults the same bindings. A parameterized check against a user
generic class is true when the class matches and the recorded bindings match
the type arguments exactly (the same invariant model as `list<int>`):

```gb
let s = Box<string>("hi");
s instanceof Box<string>;   # true
s instanceof Box<int>;      # false
s instanceof Box;           # true (bare-name check is unchanged)
```

A declaration annotation over a direct constructor call of the same class is
explicit type arguments by another spelling: `Box<int> x = Box("text")`
validates exactly like `Box<int>("text")` and is rejected - statically by
`geblang check` when the contradiction is visible, at runtime otherwise. The
annotation still wins over inference for the recorded bindings; a `let`
declaration without an annotation stays inference-open.

Explicit type arguments also resolve constructor overloads. With both
`Box(T value)` and `Box(int value)` declared, `Box<string>(42)` is not
ambiguous: binding `T = string` rules the generic constructor out for an
int, so `Box(int value)` is selected. The bindings only break ties - a
single-candidate mismatch still reports the precise per-parameter error.

### Type inference

The type parameter is inferred from the call arguments - you rarely need to
write it explicitly for functions:

```gb
func first<T>(list<T> items): ?T {
    return items.length() > 0 ? items[0] : null;
}

let names = ["Ada", "Grace"];
let name = first(names);   # T inferred as string; returns ?string
```

For classes, specify the type parameter at construction when inference is not
possible from the constructor arguments:

```gb
let empty = Box<int>();   # T cannot be inferred from an empty constructor
```

### Invariance

User-defined generic class types are **invariant** in their type parameters.
Even when `Sub extends Base`, a `Box<Sub>` is *not* assignable to a
`Box<Base>` parameter or typed variable - the analyzer rejects it at compile
time, and the runtime rejects it at the function-parameter boundary when the
caller's reified bindings disagree with the callee's declared bindings:

```gb
class Base {}
class Sub extends Base {}
class Box<T> { func Box() {} }

Box<Base> b = Box<Sub>();   # static error: cannot assign Box<Sub> to Box<Base>

func consume(Box<Base> b): void {}
consume(Box<Sub>());        # runtime error: consume expects Box<Base> for parameter 'b'
```

This is the standard invariance rule. Without it, a function declared with
`Box<Base>` could be called with a `Box<Sub>` and then mutate the box with a
sibling subtype, leaving the original `Box<Sub>` containing values that
violate its declared element type. Invariance closes that hole.

When the value is constructed without explicit type arguments (so its
reified bindings are inferred rather than pinned), the runtime accepts
the call - the bindings inherit from the parameter type at that point.

### Covariance for built-in collections

Built-in collections (`list<T>`, `set<T>`, `dict<K,V>`) are **covariant**
in their element types, unlike the user-defined generics above. A
`list<Dog>` is accepted where a `list<Animal>` is expected, and any
collection is accepted where the element type is `any`:

```gb
class Animal {}
class Dog extends Animal {}

func count(list<Animal> xs): int { return xs.length(); }

list<Dog> dogs = [Dog(), Dog()];
count(dogs);            # ok - Dog is an Animal

func countAny(list<any> xs): int { return xs.length(); }
countAny([1, 2, 3]);   # ok - any element type satisfies list<any>
```

An *unrelated* element type is rejected, both by `geblang check`
statically and at the runtime boundary. There is no numeric widening:
a `list<int>` does not satisfy `list<float>` or `list<string>`.

```gb
func countStrings(list<string> xs): int { return xs.length(); }

list<int> ints = [1, 2, 3];
countStrings(ints);    # static + runtime error: list<int> is not list<string>
```

Covariant passing is *sound* because every typed collection carries its
reified element tag and enforces it on every write - `push`, `insert`,
`prepend`, `unshift`, index assignment, `list.set`, dict key and value
writes, and `set.add`. A function that received your `list<Dog>` through a
`list<Animal>` parameter cannot smuggle a `Cat` into it; the write throws
`TypeError` against the real tag, whatever the static view says:

```gb
func sneak(list<Animal> xs): void {
    xs.push(Cat());   # TypeError: cannot push Cat to list<Dog>
}

list<Dog> dogs = [Dog()];
sneak(dogs);
```

Writes that *honestly* satisfy the tag pass by hierarchy: a `Dog` or
anything implementing a tagged interface goes into a `list<Animal>` or
`list<Scored>` like you would expect. A nullable element type accepts
null: `null` may be written into a `list<?int>` or a `dict<string, ?int>`,
while a non-nullable `list<int>` rejects it.

The write barrier checks the *outer* element type only. Writing into a
`list<list<int>>` verifies the value is a list, not that its elements are
ints (declaration-time element checking goes deeper; per-write checking
is shallow to keep mutation cheap).

Covariance is convenient but, combined with mutation, is not fully
sound: a function that takes `list<Animal>` could insert a `Cat` into a
list the caller declared as `list<Dog>`. Re-validate at the next typed
boundary, or copy, when a collection is both shared and mutated across a
covariant boundary.

### Container types

Generic collection type hints document and enforce call/declaration boundaries.
The type binder infers `T` from the element type in the call arguments:

```gb
func sumBy<T>(list<T> items, func selector): int {
    int total = 0;
    for (item in items) {
        total = total + selector(item);
    }
    return total;
}

let total = sumBy(orders, func(Order o): int { return o.amount; });
```

### Constraints

Add `implements InterfaceName` after the type parameter to require the
concrete type to satisfy an interface. The constraint lets you call interface
methods inside the function:

```gb
interface Printable {
    func print(): string;
}

func show<T implements Printable>(T item): string {
    return item.print();   # valid because T implements Printable
}
```

Without the constraint, calling methods on `T` is a type error because the
compiler cannot verify that the method exists.

Union constraints (`|`) mean the type must satisfy at least one branch.
Intersection constraints (`&`, or a comma after `implements`) mean it must
satisfy all of them:

```gb
# T must implement Swimmer OR Runner
func move<T implements Swimmer | Runner>(T item): void {}

# T must implement both Printable AND Persistable
func persist<T implements Printable, Persistable>(T item): void {}
func archive<T implements Printable & Persistable>(T item): void {}
```

Constraint branches are not limited to interfaces. A primitive name
(`string`, `int`, `float`, `bool`, `decimal`, `bytes`) is satisfied by that
exact type, and a class name is satisfied by the class or any subclass:

```gb
func pick<T implements string|int>(T v): T { return v; }

pick(42);      # ok: int branch
pick("ada");   # ok: string branch
pick(true);    # RuntimeError: type bool does not satisfy constraint string|int for type parameter T
```

When the constraint is not an interface, the `implements` keyword can be
dropped - the bare form reads more naturally and means the same thing, on
functions and classes alike:

```gb
func pick<T string|int>(T v): T { return v; }

class Holder<T string|int> {
    T value;
    func Holder(T value) { this.value = value; }
}

Holder(7);        # ok, T = int
Holder(true);     # RuntimeError: constraint violated at construction
```

### Generics on methods and interfaces

Type parameters can appear on individual methods inside a class, not just on
the class itself. Interface declarations can also be generic:

```gb
class Converter {
    func to<T>(string raw): T {
        return raw as T;
    }
}

interface Container<T> {
    func get(): T;
    func set(T value): void;
}
```

### Generics across function boundaries

A lambda declared inside a generic function's body inherits the
outer call site's type bindings. The same is true when a generic
function is referenced by name and passed as a value: the reference
captures the surrounding generic frame's bindings at the moment the
value is taken. This lets higher-order code keep its type guarantees
even when control flows through a stdlib helper such as
`collections.maxBy`.

```gb
import collections;

interface Scored { func score(): int; }

func topBy<T implements Scored>(list<T> items): T {
    # The lambda's `T x` parameter resolves to the same concrete type
    # as the outer call - if topBy is called with a list<Player>, T is
    # Player inside the lambda too, and a non-Player would be rejected
    # at the parameter boundary.
    return collections.maxBy(items, func(T x): int { return x.score(); });
}
```

If the surrounding frame has no binding for the named type parameter
(for example when the lambda escapes its creation scope and is called
later from somewhere else), the matcher falls back to the literal
type-parameter behaviour and accepts any value.
