# Syntax Basics

## Comments

```gb
# line comment

/* block
   comment */
```

`//` is integer division, not a comment.

Doc comments are retained for reflection and documentation tools. Use `##` for
line docblocks or `/** ... */` for block docblocks immediately before the
declaration they describe:

```gb
## Returns the display name.
func name(): string {
    return "Ada";
}

/**
 * Handles user routes.
 */
class UserController {}
```

## Variables And Constants

```gb
string name = "Ada";
int count = 3;
let inferred = 42;
const VERSION = "0.1.0";
```

Variables are mutable. Constants cannot be reassigned.

Use `let` when the type is obvious from the initializer. Use an explicit type
when it documents an API boundary, narrows a nullable value, or prevents an
accidental wider type.

```gb
let retries = 3;
dict<string, any> payload = json.parse(text);
```

### The `any` type

`any` is Geblang's explicit opt-in for dynamic values. It is useful at
boundaries where the concrete shape is not known until runtime: decoded JSON,
HTTP request payloads, framework metadata, extension values, and generic
dictionary contents.

```gb
any value = "queued";
value = 42;
value = {"status": "done"};
```

The binding is still statically typed: its static type is `any`. That means the
analyzer allows assignments from different runtime types, but it also means you
must narrow or cast before using type-specific behaviour:

```gb
dict<string, any> payload = json.parse(text);
string name = payload["name"] as string;
int count = payload["count"] as int;
```

Prefer precise types for application logic and public APIs. Use `any` only
where a value is intentionally dynamic or where a generic container needs to
hold mixed values.

## Strings

Double-quoted strings process escape sequences and support interpolation:

```gb
string msg = "Hello ${name}!\n";
```

The escapes are `\n`, `\t`, `\r`, `\\`, `\"`, `\0` (and the other
single-letter C escapes), plus `\u{HEX}` for a Unicode code point by hex
value:

```gb
io.println("tab\tend");
io.println("\u{41}");      # A
io.println("\u{20AC}");    # €
io.println("\u{1F600}");   # 😀  (U+1F600)
```

`\u{...}` is the idiomatic way to write a code point in a literal (the same
value `string.fromCodePoint(...)` produces). It takes 1 to 6 hex digits; an
empty, out-of-range, or surrogate value is a compile error. Escapes,
including `\u{...}`, are decoded inside interpolated strings too, so
`"emoji \u{1F600} for ${name}"` works as expected.

Any expression can appear inside `${...}`:

```gb
io.println("${a} + ${b} = ${a + b}");
io.println("count: ${items.length()}");
```

Non-string values are automatically converted to their string representation:

```gb
let n = 42;
io.println("n = ${n}");  # n = 42
```

Single-quoted strings are raw - no escape processing and no interpolation:

```gb
string raw = 'Hello ${name}\n';  # literal: Hello ${name}\n
```

Multiline strings use triple quotes:

```gb
let user = {"name": "Ada"};
string html = """
<h1>${user["name"]}</h1>
""";
```

Choose single-quoted strings for regex patterns, shell fragments, or examples
where backslashes should stay literal. Choose triple-quoted strings for HTML,
SQL, Markdown, and larger fixture text.

An interpolation expression can itself contain string literals, so a dictionary
key works directly with double quotes in any interpolated string, regular or
triple-quoted: the parser skips nested string literals when it scans for the
closing `}`. Single-quoted keys are equivalent, and the expression can be
anything:

```gb
let foo = {"bar": 1};
io.println("${foo["bar"]}");        # 1   (double-quoted key)
io.println("${foo['bar']}");        # 1   (single-quoted key, equivalent)
io.println("${foo["bar"] + 10}");   # 11  (any expression, nesting allowed)
```

### Format specifiers

An interpolation can carry an optional `:spec` formatter that follows
Python's mini-language. The spec is parsed as

```
[[fill]align][sign][#][0][width][,][.precision][type]
```

with the following type characters:

| Type | Use |
|------|-----|
| `d`  | integer decimal |
| `x` / `X` | hex (lower / upper) |
| `o`  | octal |
| `b`  | binary |
| `f`  | fixed-point float (`f` works on `decimal` too) |
| `e`  | scientific notation |
| `g`  | general float (auto-pick fixed or scientific) |
| `s`  | string (default) |
| `%`  | percentage (value * 100, suffixed with `%`) |

```gb
let pi    = 3.14159;
let big   = 1234567;
let label = "Ada";

io.println("${pi:.2f}");        # 3.14
io.println("${big:,}");         # 1,234,567
io.println("${42:>5}|");        # "   42|"  (right-align width 5)
io.println("${42:<5}|");        # "42   |"  (left-align)
io.println("${42:^5}|");        # " 42  |"  (center)
io.println("${42:05}");         # 00042     (zero-pad)
io.println("${42:*>4}");        # **42      (custom fill)
io.println("${255:x}");         # ff
io.println("${255:#x}");        # 0xff
io.println("${42:+d}");         # +42       (forced sign)
io.println("${0.125:.2%}");     # 12.50%
io.println("${label:>10}|");    # "       Ada|"
io.println("${label:.3}");      # Ada
```

A bare `${expr}` without a `:` behaves as before - the value is
converted using its default representation.

Numeric specs (`f`, `e`, `g`, `%`) on a `decimal` format from the
decimal's exact value rather than a binary `float` approximation, so
`${d:.Nf}` matches `d.toString(N)`:

```gb
let d = 3.1415926536 as decimal;
io.println("${d:.13f}");      # 3.1415926536000
io.println(d.toString(13));   # 3.1415926536000  (identical)
```

When the expression itself contains a ternary `?:`, the format-spec
`:` doesn't get confused: `${cond ? a : b}` keeps the inner `:` as
part of the ternary. To attach a spec to a ternary result, parenthesise
the expression: `${(cond ? a : b):03d}`.

## Numbers

Three numeric types:

```gb
int     count  = 10;
decimal money  = 12.50;
float   ratio  = 0.25f;
```

Use `int` for counts, indexes, and whole-number values. Use `decimal` for
money, measurements, and any quantity where rounding errors are unacceptable.
Use `float` for scientific or graphics calculations where IEEE 754 semantics
and performance matter more than exactness.

Integer literals support decimal, binary, octal, hexadecimal, and `_`
separators:

```gb
let flags   = 0b1010;
let mode    = 0o644;
let mask    = 0xFF;
let million = 1_000_000;
```

Decimal and float literals also support scientific notation. An exponent makes
an unsuffixed literal a `decimal`; add the `f` suffix when you need an IEEE 754
`float`:

```gb
let exact = 1.5e-3;   # decimal
let wide  = 1e308f;   # float
```

### The `decimal` type

`decimal` stores values as exact rational numbers (numerator/denominator
pairs). Arithmetic never rounds - `0.1 + 0.2` is exactly `0.3`, not a
floating-point approximation:

```gb
decimal a = 0.1;
decimal b = 0.2;
decimal c = a + b;
io.println(c);   # 0.3000000000 (exact)
```

Because values are stored as fractions, dividing `1` by `3` produces the
exact fraction `1/3`, not a truncated approximation:

```gb
decimal third = 1.0 / 3.0;
io.println(third);   # 0.3333333333  (displayed to 10 decimal places)
io.println(third * 3);  # 1.0000000000  (exactly 1, no rounding error)
```

The default display always uses 10 decimal places, rounding the last digit
where needed. The stored value is always exact regardless of what the display
shows.

#### Controlling decimal places

`toString(scale)` and `format(scale)` format a `decimal` to a specific number
of decimal places:

```gb
decimal price = 4.0 / 3.0;

io.println(price.toString());    # 1.3333333333  (default: 10 dp)
io.println(price.toString(2));   # 1.33
io.println(price.toString(4));   # 1.3333
io.println(price.format(2));     # 1.33  (same result; format requires scale)
```

`toString()` with no argument is equivalent to `toString(10)`. `format(scale)`
always requires the scale argument.

Casting to `string` also uses 10 decimal places:

```gb
let s = price as string;   # "1.3333333333"
```

When you need to display a currency value at exactly two places, always call
`.format(2)` or `.toString(2)` explicitly:

```gb
decimal subtotal = 19.99;
decimal tax      = subtotal * 0.2;
io.println("Tax: " + tax.format(2));   # Tax: 4.00
```

#### Rounding to an integer

`math.floor`, `math.round`, and `math.ceil` accept `decimal` and return `int`:

```gb
import math;
import io;

io.println(math.floor(2.9 as decimal));   # 2
io.println(math.round(2.5 as decimal));   # 3
io.println(math.ceil(2.1 as decimal));    # 3
```

To keep the result as a `decimal`, use the value-keeping methods. Each
takes an optional number of decimal places (default 0) and returns the
same numeric type. `round` rounds half away from zero:

```gb
io.println((2.567).round(2));      # 2.57
io.println((2.5).round());         # 3
io.println((2.9).floor());         # 2
io.println((2.1).ceil());          # 3
io.println((2.999).truncate(2));   # 2.99
```

These work on `float` too (`(3.14159f).round(2)` -> `3.14f`).

#### Mixed arithmetic

Arithmetic between `decimal` and `int` promotes the `int` to `decimal` and
returns `decimal`:

```gb
decimal price = 9.99;
int     qty   = 3;
decimal total = price * qty;   # 29.97 (exact)
```

Arithmetic between `decimal` and `float` is not directly supported - cast one
side explicitly:

```gb
decimal d = 1.5;
float   f = 2.0f;
decimal result = d * (f as decimal);
```

#### Type conversions

```gb
decimal d = 3.75;
int     i = d as int;     # 3  (truncates toward zero)
float   f = d as float;   # 3.75 (approximate; may lose precision)
string  s = d as string;  # "3.7500000000"

# Convert from string or int
decimal fromStr = "12.50" as decimal;
decimal fromInt = 7 as decimal;
```

`as type` is the idiomatic cast. The equivalent conversion methods
(`toInt`, `toDecimal`, `toFloat`, `toString`, `toBool`) are an
alternative that allows chaining and, for `toDecimal`, finer control: it
takes an optional precision and rounds to that many decimal places in a
single step.

```gb
import math;

decimal pi4 = math.pi().toDecimal(4);   # 3.1416 (rounded, as a decimal)
decimal exact = (7).toDecimal();         # 7 (no rounding)
```

Casting a fractional decimal to `int` truncates toward zero. If exact integer
conversion is needed, verify first:

```gb
if (d.isZero() || (d - (d as int as decimal)).isZero()) {
    int whole = d as int;
}
```

## Collections

```gb
list<int> nums = [1, 2, 3];
dict<string, int> scores = {"ada": 10, "grace": 12};
set<int> ids = {1, 2, 2, 3};
```

Indexing is zero-based. Negative indexes count from the end:

```gb
nums[0];
nums[-1];
scores["ada"];
```

Use `length()` to count the number of elements or entries in a collection:

```gb
list<int> nums = [1, 2, 3];
set<int> unique = {1, 2, 2, 3};
dict<string, int> scores = {"ada": 10, "grace": 12};

io.println(nums.length());   # 3
io.println(unique.length()); # 3
io.println(scores.length()); # 2
```

Use `isEmpty()` when you only need to know whether a collection has no
elements:

```gb
if (!nums.isEmpty()) {
    io.println(nums.first());
}
```

Use `hasKey` or `contains` for dictionary key membership:

```gb
let data = {"name": "Ada", "middle": null};
io.println(data.contains("middle")); # true
io.println(data.hasKey("missing"));  # false
```

List mutation works in place (1.16.0): `push`, `removeAt`, and the other
mutators modify the receiver and return it, so calls chain. `append` and
`extend` also mutate in place but return `null`. Dict mutators (`set`,
`delete`) modify in place too.

```gb
nums.append(4);          # in place; nums is now [1, 2, 3, 4]
nums.push(5);            # in place; returns nums, so pushes chain
nums.removeAt(0);        # in place; removes index 0
scores.set("linus", 7);  # in place
scores.delete("ada");    # in place
```

Use `copy()` (or `sorted()` / `reversed()`) when you need a modified copy
that leaves the original untouched.

Use `collections.map`, `collections.filter`, and related helpers when you want
to return a transformed collection rather than update the original.

### Spread in collection literals

A list, dict, or set literal can include `...source` entries that splice
the source collection's elements into the new collection. The pattern
mirrors how `...args` works at function call sites.

```gb
let xs = [1, 2, 3];
io.println([0, ...xs, 4]);                 # [0, 1, 2, 3, 4]

let defaults = {"port": 80, "tls": false};
let opts     = {...defaults, "port": 443}; # {"port": 443, "tls": false}

let extra = {"x": 0, ...defaults};         # {"x": 0, "port": 80, "tls": false}

let small = {1, 2, 3};
let big   = {0, ...small, 4};              # set{0, 1, 2, 3, 4}
```

Rules:

- **List spread** requires a list source. The source's elements are inserted in
  order at the spread position.
- **Dict spread** requires a dict source. Subsequent entries (and later spreads)
  overwrite earlier values on key collision - last write wins.
- **Set spread** accepts a set or a list source; duplicates collapse naturally.
- A literal whose entries are all spreads (`{...a}`, `{...a, ...b}`) is treated
  as a dict by default. To force a set, include at least one bare element:
  `{x, ...s}`.

## Operators

Arithmetic, comparison, equality, boolean, bitwise, null coalescing, optional
chaining, ternary, and casts are available:

```gb
let total = price * quantity;
let ok = enabled && count > 0;
let name = maybeName ?? "anonymous";
let city = user?.address?.city;
let text = value as string;
let label = count > 0 ? "items" : "empty";
```

The `in` operator tests membership and returns a `bool`: element for lists,
key for dicts, member for sets, substring for strings, and value-in-range for
ranges. Negate with `!`. (A range literal needs parentheses because `..` binds
looser than `in`: `x in (1..10)`.)

```gb
io.println(2 in [1, 2, 3]);        # true
io.println("id" in {"id": 1});     # true (key membership)
io.println("ell" in "hello");      # true (substring)
io.println(5 in (1..10));          # true
io.println(!(9 in [1, 2, 3]));     # true
```

User classes can support `in` by implementing `__contains` (see the classes
chapter); `for x in collection` loops are unaffected by the operator.

### Identity: `is` and `is not`

`==` compares values; `is` asks whether two references are the same object.
For instances, lists, dicts, sets, function values, tasks, and generators, `x is
y` is `true` only when both name one object, so a change made through one is
seen through the other. Two equal but separately built values are `==` without
being `is`. `is not` is the negation.

```gb
import io;

class Point {
    int x;
    func Point(int x) { this.x = x; }
}

let a = Point(1);
let b = a;
io.println(a is b);              # true  (same object)
io.println(a is Point(1));       # false (equal fields, different object)
io.println(a == Point(1));       # true

let items = [1, 2];
let alias = items;
alias.push(3);
io.println(items is alias);      # true
io.println(items is [1, 2, 3]);  # false
io.println(items is not alias);  # false
```

Numbers, strings, `bool`, `bytes`, enum members, and `null` have no object
identity to observe, so `is` compares them by type and value: `1 is 1` and
`"a" is "a"` are `true`, while `1 is 1.0f` is `false` even though `1 == 1.0f`.

A function value is identical to its copies. A named function or static method
is identical to itself wherever it is referenced (`helper is helper`). Each
evaluation of an anonymous `func` literal, and each `obj.method` binding,
creates a new value, so `obj.method is obj.method` is `false`; keep the value in
a variable to compare it later.

The ternary operator `condition ? then_expr : else_expr` is a compact inline
conditional. The condition must be a `bool` - values are never implicitly
treated as truthy or falsy:

```gb
let status = isActive ? "on" : "off";
io.println(score > 90 ? "A" : (score > 70 ? "B" : "C"));
```

Compound assignment operators are available for all binary operators:

```gb
x += 1;    x -= 1;    x *= 2;    x /= 4;    x //= 3;
x %= 10;   x **= 2;
x &= 0xff; x |= 0x01; x ^= mask; x <<= 2;   x >>= 1;
n ??= "default";   # assigns only if n is null
```

Conditions are explicit. Values are not implicitly treated as truthy or falsy:

```gb
if (name != "") {
    io.println(name);
}

if (items.length() > 0) {
    io.println(items.first());
}
```

## Closing Bodies

Every body opened with `{` must close with `}`, including the last declaration
in a file. An unfinished class, function, interface, enum, match or select body
is a syntax error at end of input.
