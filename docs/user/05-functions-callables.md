# Functions And Callables

## Functions

```gb
func add(int a, int b): int {
    return a + b;
}
```

Return types may be explicit or inferred.

Use explicit return types for exported functions and public methods. Inferred
return types are convenient for short local helpers, but explicit signatures are
better module documentation.

```gb
export func loadUser(string id): ?User {
    return repository.find(id);
}
```

## Multiple Return Values

A function can return several values by separating them with commas, and the
caller unpacks them into multiple variables:

```gb
func minMax(list<int> xs): list<int> {
    int lo = xs[0];
    int hi = xs[0];
    for (x in xs) {
        if (x < lo) { lo = x; }
        if (x > hi) { hi = x; }
    }
    return lo, hi;        # returns the two values
}

let lo, hi = minMax([3, 1, 4, 1, 5]);   # unpack into two variables
io.println("${lo} ${hi}");              # 1 5
```

The returned values are carried as a list, so the declared return type is a
list (`list<int>` here, or `list<any>` for mixed types). Unpacking also works
in plain assignment and supports the swap idiom:

```gb
let a, b = 1, 2;     # declare and unpack a literal list
a, b = b, a;         # swap - no temporary needed
io.println("${a} ${b}");   # 2 1
```

The `let a, b = ...` form is shorthand for list destructuring (`let [a, b] =
...`); both are equivalent. Unpacking takes the leading values when there are
more on the right than names; too few raises a runtime error.

## Defaults And Named Arguments

Default parameters must be trailing:

```gb
func connect(string host, int port = 80, bool tls = true): void {}

connect("example.com");                       # uses both defaults
connect("example.com", 443);                  # overrides port
connect("example.com", 443, false);           # overrides both
```

Any parameter can be passed by name. The name matches the parameter
identifier in the function declaration:

```gb
connect(host: "example.com", port: 443, tls: false);
connect(host: "example.com", tls: false);     # default port kept
```

Positional and named arguments can be mixed in either order. A
positional argument fills the next parameter slot that nothing has
claimed yet, so naming an earlier parameter does not block later
positional ones:

```gb
connect("example.com", tls: false);           # positional then named
connect(host: "example.com", 443);            # named then positional: port=443
```

Unknown names raise an error so typos surface immediately:

```gb
connect("example.com", tsl: false);           # error - no parameter "tsl"
```

A default expression is evaluated **fresh on every call** where the argument
is omitted, so a mutable default like `[]` or `{}` is a new value each time -
Geblang has no Python-style "shared mutable default" pitfall:

```gb
func collect(list<int> xs = []): list<int> {
    xs.append(1);
    return xs;
}
io.println(collect());   # [1]
io.println(collect());   # [1]   (a fresh list, not [1, 1])
```

Named arguments also feed overload resolution. When overloads share
an arity, naming an argument can pick the intended one:

```gb
func area(decimal w, decimal h): decimal { return w * h; }
func area(decimal radius): decimal       { return 3.14159 * radius * radius; }

area(radius: 5.0);                            # picks the one-arg overload
```

## How Arguments Are Passed

Geblang passes arguments by **call by sharing** (the same model as Python,
Java, JavaScript, and Ruby). The argument is a reference to the same value the
caller holds, so:

- **In-place mutation of a collection or object is visible to the caller** -
  the in-place mutators (`list.push(x)`, `list.append(x)`, `list.set(i, x)`,
  `dict.set(k, v)`, and `obj.field = x`) all change the caller's value.
  `push` and `append` mutate the list and return the receiver (the same
  object), so chaining off the return value keeps mutating the caller's list.
- **The copy variants do not** - `list.sorted()`, `list.reversed()`, and
  `list.copy()` return new, independent lists and leave the caller's list
  unchanged.
- **Rebinding the parameter does not affect the caller** - assigning a whole new
  value to the parameter name (`xs = [9, 9]`) only changes the local binding.

```gb
func addOne(list<int> xs): void {
    xs.push(1);       # mutates the caller's list in place
    xs = [99];        # rebinds the local only; caller is unaffected
}
let nums = [1, 2, 3];
addOne(nums);
io.println(nums);     # [1, 2, 3, 1]
```

Primitives (`int`, `decimal`, `float`, `bool`, `string`) are immutable, so the
distinction never matters for them.

When you want a function to work on independent data, pass a copy: `.copy()`
for a shallow copy or `clone.deep(x)` for a fully independent deep copy (see
the utilities chapter). When you want to guarantee a function cannot mutate
your data, freeze it (`freeze.shallow` / `freeze.deep`).

## `const` Parameters

Prefix a parameter with `const` to make it read-only inside the function. The
argument is shallow-frozen on entry, so any attempt to mutate it raises
`ImmutableError`, and the **caller's value is left untouched** (the function
receives a frozen shallow copy, not the original):

```gb
func sum(const list<int> xs): int {
    int total = 0;
    for (x in xs) { total = total + x; }
    # xs.append(0);   # would raise ImmutableError
    return total;
}

let nums = [1, 2, 3];
io.println(sum(nums));   # 6
io.println(nums);        # [1, 2, 3]  (caller unaffected)
```

`const` is shallow: it protects the parameter's own container or object, not
deeply nested values reached through it. Use it to document and enforce that a
function only reads its argument. (For a deep guarantee, pass
`freeze.deep(x)`.)

## Variadic Parameters And Spread

A variadic parameter collects trailing arguments into a list inside
the function:

```gb
func sum(int ...values): int {
    int total = 0;
    for (v in values) {
        total += v;
    }
    return total;
}

sum(1, 2, 3);                                 # 6
sum();                                        # 0
```

Inside the body the variadic name is an ordinary `list<T>`, so list
methods work on it directly (`values.length()`, `values.join(", ")`).
Defaulted parameters may sit before the variadic slot; the defaults
engage when the call leaves them unfilled:

```gb
func tag(string base, string sep = "-", string ...parts): string {
    if (parts.isEmpty()) { return base; }
    return base + sep + parts.join(sep);
}

tag("x");                                     # x
tag("x", "+", "y", "z");                      # x+y+z
tag("x", sep: "*");                           # x
```

At the call site, `...` (spread) does the inverse: it expands a
collection into arguments. Spread dispatches on the runtime type
of its operand. Spread works in every dispatch context: plain
functions, lambdas, instance methods, static methods, and
constructors.

### List spread to positional arguments

A list spread fills positional slots in order:

```gb
let nums = [1, 2, 3];
sum(...nums);                                 # 6

connect(...["example.com", 443, false]);      # host, port, tls
```

A list spread can follow positional arguments:

```gb
let tail = [443, false];
connect("example.com", ...tail);
```

A spread does not have to be last. Arguments after a spread, multiple
spreads, and named arguments before or after a spread all bind in
source order (named arguments bind by name, positionals fill the
remaining slots):

```gb
let mid = [443];
connect("example.com", ...mid, false);        # host, port, tls
connect(tls: false, ...["example.com", 443]); # host, port by position
```

### Dict spread to named arguments

A string-keyed dict spread maps each entry's key to the parameter
of the same name:

```gb
let opts = {"port": 443, "tls": false};
connect("example.com", ...opts);              # host="example.com", port=443, tls=false
```

Keys that do not name a parameter of the target function are
silently ignored, so options dicts can carry more entries than the
target consumes:

```gb
let opts = {"port": 443, "tls": false, "loggedAt": 1717000000};
connect("example.com", ...opts);              # loggedAt is dropped
```

A required parameter that the dict does not cover still errors:

```gb
let opts = {"port": 443};
connect(...opts);                             # error - missing host
```

A spread and an explicit named argument that target the same
parameter raise a "passed more than once" error. Build the dict
the way you want or override on the dict, not at the call site:

```gb
let opts = {"port": 443, "tls": true};
connect("example.com", ...opts, tls: false);  # error - tls passed twice
connect("example.com", ...opts.merge({"tls": false}));   # ok
```

When dict spread is involved in overload resolution and multiple
overloads can bind, the runtime prefers the overload that drops the
fewest spread keys. Two overloads tied on that score are still
reported as ambiguous.

## Pipe Operator

`x |> f(...)` desugars to a call where `x` is injected as the first
positional argument: `f(x, ...)`. The pipe is left-associative, so
chains read left-to-right as a transformation pipeline.

```gb
import io;

func double(int x): int     { return x * 2; }
func add(int a, int b): int { return a + b; }

io.println(5 |> double);                 # 10  (bare callable form)
io.println(5 |> double());               # 10  (explicit-paren form)
io.println(5 |> add(3));                 # 8   (extra positional args follow)
io.println(5 |> double() |> add(1));     # 11  (chained)
```

The right-hand side can be a bare identifier, a `module.fn` selector,
a free-function call, or a `class.staticMethod(...)` selector call.
For each, the pipe value is prepended to whatever arguments are
already there.

```gb
"hello" |> Util.wrap("[", "]")           # Util.wrap("hello", "[", "]")
xs |> collections.maxBy(scorer)          # collections.maxBy(xs, scorer)
```

The operator binds at very low precedence (just above assignment), so
each side absorbs full expressions:

```gb
2 + 3 |> double                          # (2 + 3) |> double  ->  10
(true ? 5 : 0) |> double                 # parenthesise to control grouping
```

If the right-hand side isn't a call, identifier, or selector, the
pipe is a parse-time error - `x |> 42` is rejected.

## Partial Application

A call where one or more arguments are the placeholder `_` does not call the
function; it returns a new callable with those positions left open. Supplying
the missing arguments later fills the holes left to right.

```gb
func add(int a, int b): int { return a + b; }

let add10 = add(_, 10);   # a function of one argument: add(x, 10)
io.println(add10(3));     # 13

let inc = add(1, _);      # add(1, x)
io.println(inc(9));       # 10
```

Holes may appear in any position, more than once, and as a named argument's
value:

```gb
func wrap(string a, string b, string c): string { return a + b + c; }
let f = wrap(_, "-", _);  # a function of two arguments
io.println(f("L", "R"));  # L-R

let openTmp = open(dir: "/tmp", mode: _);
```

The non-hole arguments are evaluated once, when the partial is created. The
function or method itself is resolved when the partial is applied, so a partial
over an overloaded function selects the overload from the full argument list at
call time. Partial application works for free functions, methods, static
methods, constructors, native builtins, module functions, callable values, and
objects with `__invoke`.

When a function has multiple overloads of the same arity distinguished only by
parameter type, the interpreter resolves the overload at application. A compiled
build (`geblang build`) rejects the partial at compile time because overloads
are resolved statically and the placeholder has no concrete type. Use a typed
wrapper function or a non-overloaded target in that case.

`_` is a hole only when it is an entire argument. Inside a larger expression
(`_ + 1`) it is an ordinary identifier. A hole cannot be combined with a spread
(`...`) argument in the same call.

The `functools` module's `partial` helper remains available for binding leading
arguments; the `_` syntax is the general form because holes may sit anywhere.
See also: [functools - Functional Composition](stdlib/20-utilities.md#functools---functional-composition).

## Function Overloading

Multiple functions may share the same name as long as they differ in the number
or types of their parameters. At call time the runtime picks the best-matching
overload:

```gb
import io;

func describe(string s): string { return "string: " + s; }
func describe(int n): string    { return "int: " + n; }
func describe(any x): string    { return "other: " + x; }

io.println(describe("hello"));  # string: hello
io.println(describe(42));       # int: 42
io.println(describe(true));     # other: true
```

Overload resolution rules:

1. A call must match exactly one overload by argument count and compatible
   parameter types.
2. If no overload matches, or more than one overload matches, a runtime error is
   raised.
3. The `any` type matches any value. Avoid mixing an `any` overload with more
   specific overloads of the same arity unless the call site provides enough
   context to avoid ambiguity.

When overloads differ only by return type, Geblang needs an expected type from
the surrounding context. A typed assignment, typed argument, or explicit cast can
select the intended overload. A bare call with no expected return type is
ambiguous:

```gb
func load(string id): User { return findUser(id); }
func load(string id): Order { return findOrder(id); }

User user = load("u-123");      # expected type selects User overload
Order order = load("o-123");    # expected type selects Order overload
# load("x");                    # ambiguous without an expected return type
```

An overloaded function used as a value carries all of its overloads. Calling it
through that value (for example as a callback) selects from the positional
arguments supplied at runtime, including arity, defaults, variadics, unions,
nullability, and exact runtime type names:

```gb
func scale(int x): int        { return x * 2; }
func scale(int x, int y): int { return x + y; }

func apply(any f, int v): int { return f(v); }

apply(scale, 5);   # 10 - the one-argument overload is selected at the call
```

A callable value has no named-argument syntax or surrounding expected return
type, so those direct-call tie breakers are unavailable. Class/interface
subtype matching for an overloaded callback is also currently less complete on
the bytecode backend than direct-call matching; prefer an exact parameter type
or a non-overloaded adapter when passing such a callback.

Overloading works in the same way for class methods (see the Classes chapter).
Named overloads improve error messages: when a call fails you will see which
name and which expected types did not match.

## Anonymous Functions And Closures

```gb
let inc = func(int x): int {
    return x + 1;
};

func makeCounter(): callable {
    int n = 0;
    return func(): int {
        n++;
        return n;
    };
}
```

Closures capture outer variables by reference.

That means updates made inside the closure are visible to later calls:

```gb
let next = makeCounter();
io.println(next()); # 1
io.println(next()); # 2
```

Use closures for callbacks, collection helpers, middleware, decorators, and
small pieces of behavior that do not need a full class.

## Callable Objects

Objects can be callable with `__invoke`:

```gb
class Prefixer {
    string prefix;
    func Prefixer(string prefix) { this.prefix = prefix; }
    func __invoke(string name): string { return this.prefix + name; }
}

let hello = Prefixer("hello ");
io.println(hello("Ada"));
```

Use `callable` as a type hint when an API accepts a function literal, named
function, decorated callable, or object implementing `__invoke`.

```gb
func twice(callable fn, int value): int {
    return fn(fn(value));
}
```

## Decorators

Decorators attach metadata and can also be callable wrappers when their names
resolve to functions:

```gb
func logged(any next): any {
    return func(string name): string {
        log.info("enter");
        defer log.info("exit");
        return next(name);
    };
}

@logged
func greet(string name): string {
    return "hello " + name;
}
```

`reflect.decorators(value)` exposes decorator metadata for functions, classes,
methods, and static methods.

Decorators are evaluated at declaration time. A decorator can be used as pure
metadata for reflection, as a callable wrapper, or both. Framework code can scan
metadata with `reflect` and register handlers without introducing framework
syntax into the language.

A callable decorator's wrapper is transparent to callers: `greet(name: "Ada")`
binds `name` against the original `greet` function's declared parameters, even
though the wrapper above names its own parameter `name` too (or, for a
`func(any ...args)` forwarding wrapper, declares no named parameter at all).

## @memoize

`@memoize` caches a top-level function's result by its arguments: the body runs
once per distinct argument tuple, and later calls with the same arguments return
the stored result. Recursion through the function's own name is memoized too, so
naive recursive definitions become efficient.

```gb
@memoize
func fib(int n): int {
    if (n < 2) { return n; }
    return fib(n - 1) + fib(n - 2);  # each fib(n) body runs once
}
```

The cache key is the canonical form of the arguments (the same value identity
used by dict keys), so equal values share a cache entry. The cache is unbounded
and lives for the life of the process, so `@memoize` is intended for pure
functions over a bounded input domain. Applying `@memoize` to a method, an
`async` function, a generator, or a `void` function is a compile-time error.
