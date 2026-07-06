# Classes, Interfaces, And Enums

## Classes

```gb
class User {
    string name;

    func User(string name) {
        this.name = name;
    }

    func displayName(): string {
        return this.name;
    }
}
```

Constructors use the class name. Instance methods use `this`.

Fields should be declared on the class so instances have a predictable shape:

```gb
class User {
    string id;
    string email;

    func User(string id, string email) {
        this.id = id;
        this.email = email;
    }
}
```

Keep constructors focused on valid initialization. Use named static factories
or module functions when construction needs parsing, I/O, or fallback behavior.

## Method Overloading

A class may define multiple methods (or constructors) with the same name, as
long as the signatures differ by the number or types of parameters. The runtime
selects the best-matching overload at call time:

```gb
import io;

class Printer {
    func print(string s): void {
        io.println("string: " + s);
    }

    func print(int n): void {
        io.println("int: " + n);
    }

    func print(string label, int n): void {
        io.println(label + ": " + n);
    }
}

let p = Printer();
p.print("hello");        # string: hello
p.print(42);             # int: 42
p.print("count", 7);     # count: 7
```

Constructor overloading follows the same rules:

```gb
class Point {
    decimal x;
    decimal y;

    func Point() {
        this.x = 0;
        this.y = 0;
    }

    func Point(decimal x, decimal y) {
        this.x = x;
        this.y = y;
    }
}

let origin = Point();
let p = Point(3.0, 4.0);
```

When a call matches no overload, or multiple overloads match equally well, a
runtime type error is raised identifying the method name and the arguments
that were passed.

Methods and constructors may also differ by return type when the surrounding
context provides an expected type. For example, assigning the result to a typed
variable can select the overload. Without that expected type, a call that
matches only by return type is ambiguous and raises a runtime type error.

## Inheritance

Classes use `extends` to inherit from one parent class:

```gb
class Admin extends User {
    func Admin(string name) {
        parent(name);
    }

    func displayName(): string {
        return "admin " + parent.displayName();
    }
}
```

Geblang classes currently support single class inheritance: one class can extend
one parent class. Multiple class inheritance is not part of the language today.
Use interfaces for multiple behavioral contracts and composition for sharing
services or collaborators.

Child classes inherit parent fields and methods. If the parent has a
zero-argument constructor and the child constructor does not explicitly call
`parent(...)`, Geblang calls the parent constructor automatically. If the parent
constructor requires arguments, call `parent(...)` yourself:

```gb
class Animal {
    string name;

    func Animal(string name) {
        this.name = name;
    }

    func speak(): string {
        return this.name + " makes a sound";
    }
}

class Dog extends Animal {
    func Dog(string name) {
        parent(name);
    }

    func speak(): string {
        return parent.speak() + ", then barks";
    }
}
```

`parent.method()` calls the parent implementation from an override. `parent(...)`
calls the parent constructor. An explicit `parent(...)` call suppresses the
automatic no-argument parent constructor call, so the parent constructor only
runs once.

Automatic parent constructor example:

```gb
class Base {
    int count = 0;

    func Base() {
        this.count = this.count + 1;
    }
}

class Child extends Base {
    func Child() {
        # Base() is called automatically before this body runs.
    }
}

io.println(Child().count); # 1
```

### Cross-module inheritance

A class can extend a parent defined in another module. Two forms are
equivalent:

```gb
# shapes.gb
module shapes;
export class Shape {
    string color;
    func Shape(string color) { this.color = color; }
    func area(): float { return 0.0f; }
}
```

Qualified import - use the module prefix in the `extends` clause:

```gb
import shapes;

class Circle extends shapes.Shape {
    float radius;
    func Circle(float radius) { parent("red"); this.radius = radius; }
    func area(): float { return 3.14159f * this.radius * this.radius; }
}
```

From-import - bring the name into scope first:

```gb
from shapes import Shape;

class Square extends Shape {
    float side;
    func Square(float side) { parent("blue"); this.side = side; }
    func area(): float { return this.side * this.side; }
}
```

Both forms give the subclass full access to inherited fields and methods.

`instanceof` is module-exact: its right-hand side resolves to the specific
class or interface declaration named in scope and matches by identity, walking
the value's real parent chain, so `x instanceof shapes.Shape` matches instances
of `Shape` (and its subclasses) declared in `shapes`, but never a same-named
class or interface from another module. Use `instanceof` rather than `typeof`,
which matches by name, to distinguish same-named types across modules.

Use inheritance for true specialization. Prefer composition when one object
merely needs to use another service:

```gb
class UserService {
    UserRepository repo;

    func UserService(UserRepository repo) {
        this.repo = repo;
    }
}
```

## Static Members

Classes can declare both immutable constants and mutable fields at
class scope, plus static methods. `static const` makes an immutable
class-level binding; `static let` and the typed form `static <type>`
declare a mutable class-level field that any code in scope can read
and reassign.

```gb
class Build {
    static const VERSION = "0.1.0";

    static func label(): string {
        return Build.VERSION;
    }
}

class Counter {
    static let count = 0;            # untyped, mutable
    static int errors = 0;           # typed, mutable

    static func tick(): int {
        Counter.count = Counter.count + 1;
        return Counter.count;
    }
}

Counter.tick();
Counter.tick();
io.println(Counter.count);           # 2
Counter.errors = 5;                  # external assignment also works
io.println(Counter.errors);          # 5
```

Reading static members from inside the class uses the same `ClassName.member`
syntax; there is no implicit `self` for static methods.

## Immutable Classes

Apply the `@immutable` decorator to freeze every instance after its constructor
returns. Fields are readable; any assignment to a field raises `ImmutableError`.

```gb
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
}

Point p = Point(3, 4);
io.println(p.x);   # 3
p.x = 99;          # throws ImmutableError
```

Produce modified copies using the *wither* pattern instead of mutation:

```gb
@immutable class Point {
    int x;
    int y;
    func Point(int x, int y) { this.x = x; this.y = y; }
    func withX(int x): Point { return Point(x, this.y); }
}
```

See the `freeze` module documentation for `freeze.shallow`, `freeze.deep`,
`freeze.isFrozen`, `.copy()`, and `const` collection auto-freeze behavior.

### Set-once fields

For finer control, annotate an individual field with `@immutable` instead of
the whole class. A set-once field is freely writable while the constructor runs
and locked once construction completes; a later assignment raises
`ImmutableError`. Other fields stay mutable.

```gb
class User {
    @immutable string id;   # set once, then locked
    string name;            # mutable

    func User(string id, string name) {
        this.id = id;       # ok
        this.name = name;
    }
}

let u = User("u1", "Ada");
u.name = "Ada L.";          # ok
u.id = "u2";                # throws ImmutableError
```

A set-once field is locked at the end of construction, so the constructor may
assign it more than once if it needs to; the lock applies afterwards. An
`@immutable` field inherited from a parent class is locked too. An `@immutable`
field may not declare a default value (`@immutable int x = 5;` is a compile
error) - the constructor must set it.

## Data Classes

The `@dataclass` decorator generates the boilerplate that a value-style class
usually needs, from its declared fields:

- a constructor taking the fields in declaration order (generated only when the
  class declares no constructor of its own),
- value-based equality (`__eq`) comparing every field,
- a readable `__string` rendering (`Point(x=1, y=2)`),
- a `with(...)` copy helper that returns a new instance with named fields
  replaced.

```gb
@dataclass
class Point {
    int x;
    int y;
}

let p = Point(1, 2);
io.println(p);              # Point(x=1, y=2)
io.println(p == Point(1, 2));  # true
let q = p.with({"y": 9});  # Point(x=1, y=9), p is unchanged
```

A field with a default becomes an optional constructor parameter:

```gb
@dataclass
class Tag {
    string name;
    int weight = 1;
}

Tag("a");        # weight defaults to 1
Tag("b", 3);
```

Any member you write yourself takes precedence over the generated one, so you
can override `__string`, `__eq`, the constructor, or `with` as needed.

`@dataclass(frozen: true)` additionally makes instances immutable (the same
whole-instance freeze as `@immutable`), so a frozen data class is a true value
object:

```gb
@dataclass(frozen: true)
class Money {
    int cents;
}

let m = Money(500);
# m.cents = 0;  # throws ImmutableError
let n = m.with({"cents": 250});  # build a changed copy instead
```

A frozen instance is also usable as a dict key or set member by value: two
frozen instances with equal fields are the same key, so sets deduplicate them
and dict lookups find them regardless of which instance you pass.

```gb
let prices = {Money(100), Money(100), Money(200)};
io.println(prices.length());        # 2
io.println(prices.contains(Money(100)));  # true
```

Only frozen instances key by value; a mutable instance keys by identity (so it
cannot change out from under a dict). `@dataclass` operates on the class's own
declared fields and is intended for flat value classes; a data class that
extends another class must declare its own constructor.

## @override

Annotate a method with `@override` to assert that it overrides a method of the
same name declared on an ancestor class or an implemented interface. If neither
declares it, it is a compile-time error - which catches a parent or interface
method that was renamed or removed out from under the override.

```gb
class Animal {
    func speak(): string { return "..."; }
}

class Dog extends Animal {
    @override
    func speak(): string { return "woof"; }
}
```

The check is by method name. When the parent class is not visible (for example,
imported from another module the analyzer cannot resolve), the assertion is
skipped rather than reported, so it never false-positives.

## @deprecated

Mark a function, method, or class `@deprecated` to flag it for removal. Every use
site is reported by `geblang check` as a `warning[deprecated]`; an optional
message points callers at the replacement. It is advisory only and never changes
whether code runs.

```gb
@deprecated("use fetchUser instead")
func getUser(int id): User { return fetchUser(id); }

# geblang check: warning[deprecated]: use of deprecated getUser: use fetchUser instead
let u = getUser(1);
```

## Other built-in decorators

`@memoize` caches a function's result by its arguments. It applies to top-level
functions only (not methods), so it is documented with the other function
features in [Functions And Callables](05-functions-callables.md#memoize).

## Interfaces

```gb
interface Printable {
    func print(): string;
}

class Report implements Printable {
    func print(): string {
        return "report";
    }
}
```

Interfaces can inherit from other interfaces. Classes explicitly declare
`implements`.

Interfaces may extend multiple interfaces:

```gb
interface Printable {
    func print(): string;
}

interface Serializable {
    func serialize(): string;
}

interface Reportable extends Printable, Serializable {
    func title(): string;
}
```

Classes can implement multiple interfaces:

```gb
class Report implements Printable, Serializable {
    func print(): string {
        return "report";
    }

    func serialize(): string {
        return "{\"type\":\"report\"}";
    }
}
```

This is the main way to model "multiple inheritance" style contracts in
Geblang: one concrete parent class for implementation inheritance, many
interfaces for capabilities.

Interfaces are structural contracts for application boundaries. They work well
for repositories, renderers, cache stores, middleware, and test doubles.

```gb
interface CacheStore {
    func get(string key): any;
    func set(string key, any value, int ttl): void;
    func delete(string key): void;
}
```

Interfaces work well with dependency injection:

```gb
class CachedUsers {
    CacheStore cache;

    func CachedUsers(CacheStore cache) {
        this.cache = cache;
    }
}
```

### Default method implementations

An interface method with a body is a **default implementation**.
Classes that implement the interface and don't override the method
inherit the body as-is. Classes that override get their own.

```gb
interface Greetable {
    string name;
    func greet(): string {
        return "hello, " + this.name;
    }
    func loudName(): string;
}

class User implements Greetable {
    func User(string n) { this.name = n; }
    func loudName(): string { return this.name.upper(); }
    /* greet inherited from Greetable */
}

class Loud implements Greetable {
    func Loud(string n) { this.name = n; }
    func greet(): string { return "HELLO, " + this.name; }
    func loudName(): string { return this.name.upper(); }
}

io.println(User("ada").greet());   /* "hello, ada" */
io.println(Loud("ada").greet());   /* "HELLO, ada" */
```

### Interface properties

An interface can declare property requirements as bare field
declarations. Every implementing class automatically gains those
fields - no need to redeclare them in the class body.

```gb
interface Greetable {
    string name;          /* every implementer has `name` */
    int age;              /* and `age` */
    func greet(): string { return this.name + " is " + (this.age as string); }
}

class User implements Greetable {
    func User(string n, int a) {
        this.name = n;    /* set the inherited field */
        this.age = a;
    }
}
```

When a class implements multiple interfaces that declare the same
field name, the runtime keeps one field as long as the declared
types match. Conflicting types are a compile-time error.

### Multi-interface defaults: the diamond

When `class C implements A, B` and **both** `A` and `B` provide a
default body for the same method signature, `C` must override the
method explicitly. Inheriting one of the two defaults silently
would be ambiguous, so the compiler rejects the class:

```gb
interface A { func foo(): string { return "A"; } }
interface B { func foo(): string { return "B"; } }

class C implements A, B {}              /* error: ambiguous default for foo() */

class C implements A, B {
    func foo(): string { return "C"; }   /* OK */
}
```

The rule only fires for **conflicting defaults**. If only one of
the interfaces provides a default and the other declares the same
signature without a body, `C` inherits the default unambiguously.

## Decorators

Decorators are `@`-prefixed annotations applied to classes, functions,
methods, or fields. They come in two flavours depending on where you
put them, and the two flavours are doing very different things. This
section explains both.

### Behavioural decorators on functions, methods, and classes

A decorator applied to a `func` or `class` declaration whose name
resolves to a function in scope **wraps the target**. The target value
is passed to the decorator at definition time, and the decorator's
return value replaces it. Use this to add cross-cutting behaviour like
logging, caching, retry, or timing.

```gb
func log(callable fn): callable {
    return func(any ...args): any {
        io.println("calling", reflect.className(fn));
        return fn(...args);
    };
}

@log
func greet(string name): string {
    return "hi, " + name;
}
```

Calling `greet("Ada")` invokes the wrapped value: the log line prints
first, then the underlying body runs. Multiple decorators stack in
source order. The topmost decorator wraps the inner wrapped value
last.

The same rule applies to **class decorators**. A class decorator
receives the class value at definition time and decides what to
return. Two useful shapes:

```gb
/* Register-in-place. Side effect, return the class unchanged. */
dict<string, any> services = {};
func service(any cls): any {
    services[reflect.className(cls)] = cls;
    return cls;
}

@service
class Auth { ... }

/* Wrap. Return a callable that becomes the new constructor. */
func audited(any cls): any {
    return func(...args): any {
        io.println("constructing", reflect.className(cls));
        return cls(...args);
    };
}

@audited
class Order { ... }
```

Inside a wrap closure, calling the captured `cls(args)` constructs
the original class without re-triggering the decorator chain, so
the body can pre/post-process around construction without
recursing.

**Typed delegation.** A wrap closure may return an instance of a
*different* class than the decorated one. The runtime stamps the
returned instance so it still satisfies `instanceof` against the
original class name:

```gb
class JsonRepository {
    func JsonRepository(string conn) { ... }
    func find(string id): User { ... }
}

func storage(any cls): any {
    return func(string conn): any { return JsonRepository(conn); };
}

@storage
class UserRepository {
    func UserRepository(string conn) {}
}

let ur = UserRepository("postgres://...");
io.println(ur instanceof UserRepository);  /* true */
io.println(ur instanceof JsonRepository);  /* true */
ur.find("ada");                            /* JsonRepository's find runs */
```

The instance is structurally a JsonRepository (its methods + fields)
but typed as both. Useful when one declared type fronts an
implementation that gets chosen by a decorator at definition time
(swap-by-config, ORM proxies, test stubs).

### Annotation-only (metadata) decorators

A decorator whose name does **not** resolve to a function in scope is
treated as **pure metadata**. The runtime does not execute it. The
name and any arguments are recorded on the target so reflection can
read them back. This is the form frameworks use to drive
configuration-by-annotation:

```gb
@Get("/users/{id}")
@Summary("Fetch one user")
func getUser(string id): User { ... }
```

`@Get` and `@Summary` are not functions; nothing happens at definition
time. A web framework reads them later via
`reflect.decorators(getUser)`.

Dotted names like `@Assert.email` or `@Foo.bar.baz` are valid and
parse as a single composite identifier. The dot is part of the name,
so dispatch is by exact string match. Use this to group related
annotations under a common prefix.

### Field-level decorators

A field decorator whose name resolves to a function in scope is a
**write barrier**: it runs on every assignment to that field
(including the constructor's first write), transforms the incoming
value, and the transformed value is what gets stored. Use them for
normalisation, validation, formatting, audit.

```gb
func upper(string v): string { return v.upper(); }
func minLen(int min, string v): string {
    if (v.length() < min) { throw RuntimeError("too short"); }
    return v;
}

class User {
    @minLen(2)
    @upper
    string name;
    func User(string n) { this.name = n; }
}

let u = User("ada");
io.println(u.name);  /* "ADA" - @upper ran on the constructor write */

u.name = "x";        /* throws RuntimeError("too short") */
```

The decorator's last parameter is the value being assigned. Any
earlier parameters come from the decorator's literal args
(`@minLen(2)` passes `2` to `min`). Decorators stack bottom-up:
the one closest to the field declaration runs first, and each
transform's output feeds the next.

A field decorator whose name does **not** resolve to a function is
treated as **pure metadata**: the runtime never executes it, but
frameworks read it back via reflection to drive
configuration-by-annotation. This is the form used by libraries
like Gebweb for validation rules, serialisation filters, OpenAPI
hints, and similar concerns:

```gb
class CreateUserDTO {
    @Assert.email
    string email;

    @Assert.minLength(2)
    @Assert.maxLength(64)
    string name;
}
```

Here `@Assert.email` doesn't resolve to a function called `Assert.email`
in scope, so it stays metadata; `reflect.fields(CreateUserDTO)` returns
each field's decorator list as `{name, args, namedArgs, ...}` dicts.

**Resolution rule.** When the runtime sees `@foo` on a field, it
looks up `foo` in scope. Callable -> write barrier. Unresolved ->
metadata only. The same name can mean different things in different
scopes; that's fine.

**Decorator arguments.** Literal values (strings, ints, floats,
decimals, bools, null) and literal list / dict / set composites
built from those. Names from scope are not resolved at decorator-arg
time, so the metadata stays serialisable into the compiled chunk.

```gb
class Item {
    @Assert.range(1, 100)
    @Groups("read", "write", "admin")
    int quantity;
}
```

`@Assert.range(1, 100)` to metadata `{name: "Assert.range", args: [1, 100]}`.
`@Groups("read", "write", "admin")` to metadata
`{name: "Groups", args: ["read", "write", "admin"]}`.

### Parameter-level decorators

Parameter decorators are **metadata-only**. They never run, never
wrap the value, never alter the call. They sit on the parameter
and `reflect.parameters(target)` surfaces them so frameworks can
read them back.

```gb
class DbConn {
    func DbConn(@Param("db.url") string url) { ... }
}

@Get("/users/{id}")
func show(
    @PathParam("id") string id,
    @QueryParam("limit") int limit = 10,
    @Header("X-Api-Key") string apiKey
): User { ... }
```

Same arg rules as field decorators: literal strings, ints, floats,
decimals, bools, null, and literal list/dict/set composites of
those. Names from scope are not resolved.

Reflection:

```gb
for (p in reflect.parameters(show)) {
    if ((p as dict<string, any>).contains("decorators")) {
        for (d in p["decorators"] as list<any>) {
            io.println((d as dict<string, any>)["name"]);
        }
    }
}
/* PathParam, QueryParam, Header */
```

The `decorators` key is only present on parameters that carry at
least one. The dict per decorator has the same shape as
`reflect.decorators(target)` returns: `name`, `args`, `namedArgs`,
`target` (`"parameter"`), `position`, `overload`, `line`, `column`.

Parameter decorators work on constructor parameters, method
parameters, free-function parameters, and lambda parameters - one
rule, every parameter list.

### Reading decorator metadata

| Call | Returns |
| --- | --- |
| `reflect.decorators(target)` | List of `{name, args, namedArgs, line, column}` dicts. |
| `reflect.hasDecorator(target, name)` | Bool. `true` when at least one decorator with that name is present. |
| `reflect.decorator(target, name)` | First decorator dict with that name, or `null`. |
| `reflect.fields(class)` | List of field dicts; each has a `decorators` key when at least one annotation is present. |

`target` is either a class value, a function/method value, or a class
instance (in which case the call delegates to the instance's class).
Names match exactly: `@Assert.email` is the name `"Assert.email"`,
not `"Assert"` with a sub-key.

### Built-in decorator: `@abstract`

`@abstract` marks a class or method as not directly instantiable.
A class is abstract when **either**:

- the class itself is decorated `@abstract`, or
- it has (or inherits) a method decorated `@abstract` with no
  concrete override in any more-derived class.

Direct construction of an abstract class throws `RuntimeError`.

```gb
@abstract
class Repository {
    func describe(): string { return "repo"; }
}

class Storage {
    @abstract
    func read(string key): string { return ""; }
}

class MemoryStorage extends Storage {
    func read(string key): string { return "..."; }
}

Repository();       /* throws: cannot instantiate abstract class Repository */
Storage();          /* throws: cannot instantiate Storage: abstract method
                       Storage.read is not implemented */
MemoryStorage();    /* OK - read() is overridden */
```

Method bodies on `@abstract` methods are still parsed (Geblang has no
`abstract` keyword) and they execute if a concrete subclass calls
`parent.read(key)` against them, so it's reasonable to put a sensible
default or an explicit throw in there.

## Magic Methods

Magic methods are ordinary methods with reserved names. They let a class opt
into dynamic property access, callable object behavior, and operator
overloading. Keep them focused: a class should only implement the magic methods
that match its public role.

## Dynamic Access And Method Dispatch

Use `__get`, `__set`, and `__call` for dynamic objects such as records, proxies,
configuration wrappers, or framework adapters.

```gb
class Bag {
    dict<string, any> values;

    func Bag() {
        this.values = {};
    }

    func __get(string name): any {
        if (this.values.hasKey(name)) {
            return this.values[name];
        }
        return null;
    }

    func __set(string name, any value): void {
        this.values[name] = value;
    }

    func __call(string name, list<any> args): any {
        return {"method": name, "args": args};
    }
}
```

Dynamic access is useful at framework boundaries, but normal declared fields and
methods should be preferred for domain code because they are easier to type
check and document.

### Subscript access (`__index`, `__setIndex`)

`__get`/`__set` handle dot access (`obj.name`). To make a class usable with
`[]` subscript syntax (like a dict or list), implement `__index` for reads and
`__setIndex` for writes:

```gb
class Headers {
    dict<string, string> values;

    func Headers() { this.values = {}; }

    func __index(string key): ?string {
        return this.values.get(key);
    }

    func __setIndex(string key, string value): void {
        this.values.set(key, value);
    }
}

let h = Headers();
h["Content-Type"] = "application/json";
io.println(h["Content-Type"]);   # application/json
io.println(h["missing"]);        # null
```

A class without `__index` is not subscriptable (`obj[key]` raises "not
indexable"), so subscript behaviour is fully opt-in.

Implement `__contains(key)` to support the `in` membership operator
(`key in obj`). For a full dict-like object, implement the `maps.DictInterface`
stdlib interface (`__index` + `keys`, optional `__setIndex`) and inherit
`contains`/`get`/`values`/`length`/`isEmpty` and `__contains` as defaults (see
the utilities chapter).

## Callable Objects

Implement `__invoke` when an object should be usable like a function. This is
useful for middleware, guards, predicates, command handlers, and strategy
objects that need constructor state.

```gb
class Prefixer {
    string prefix;

    func Prefixer(string prefix) {
        this.prefix = prefix;
    }

    func __invoke(string value): string {
        return this.prefix + value;
    }
}

let shout = Prefixer("hello ");
io.println(shout("Ada"));
```

Callable objects can be passed to parameters typed as `callable`.

## Operator Overloading

Operator methods customize how instances interact with operators:

- equality: `__eq(other)`
- ordering: `__lt(other)`, `__lte(other)`, `__gt(other)`, `__gte(other)`
- arithmetic: `__add`, `__sub`, `__mul`, `__div`, `__intdiv`, `__mod`,
  `__pow`
- bitwise: `__bitand`, `__bitor`, `__bitxor`, `__lshift`, `__rshift`
- prefix: `__not`, `__neg`, `__bitnot`

Example:

```gb
class Money {
    int cents;

    func Money(int cents) {
        this.cents = cents;
    }

    func __add(Money other): Money {
        return Money(this.cents + other.cents);
    }

    func __eq(Money other): bool {
        return this.cents == other.cents;
    }

    func __lt(Money other): bool {
        return this.cents < other.cents;
    }
}

let total = Money(500) + Money(250);
io.println(total == Money(750));
```

An instance nested in a list, dict, or set compares the same way it does at top
level: two containers are equal when their instance elements are equal field by
field (or by `__eq` when the class defines one), not by object identity. So
`[Money(500)] == [Money(500)]` is `true`.

Operator methods should return the type users expect from the operator.
Comparison and equality methods must return `bool`; arithmetic methods should
usually return the same domain type.

Defining a single ordering dunder is enough for all four comparison
operators. Missing operators are derived: `a > b` falls back to
`b.__lt(a)` (and `a < b` to `b.__gt(a)`), while `a <= b` and `a >= b`
fall back to the negated strict comparison. With only `__lt` on `Money`
above, `<`, `>`, `<=`, and `>=` all work. A defined dunder always wins
over a derived one.

## Cast Overloading

A class can control how its instances respond to `as TYPE` casts by
defining a cast dunder for each target primitive:

- `__string(): string` for `as string`
- `__int(): int` for `as int`
- `__float(): float` for `as float`
- `__bool(): bool` for `as bool`
- `__decimal(): decimal` for `as decimal`
- `__bytes(): bytes` for `as bytes`

Each dunder must declare the matching return type. The semantic
analyzer rejects mismatches at compile time, and the runtime checks
the actual returned value as a defensive backstop.

```gb
class Money {
    int cents;

    func Money(int cents) {
        this.cents = cents;
    }

    func __string(): string {
        return "$" + (this.cents as string);
    }

    func __int(): int {
        return this.cents;
    }

    func __bool(): bool {
        return this.cents != 0;
    }
}

let m = Money(550);
io.println(m as string);   # $550
io.println(m as int);      # 550
io.println(m as bool);     # true
io.println(Money(0) as bool);  # false
```

When the class does not define a dunder for the target primitive, the
default cast logic runs (errors when the conversion is undefined for
the receiver's type).

`__string` also drives implicit string rendering: an instance that
defines it is rendered through `__string` by string interpolation
(`"${m}"`), `io.println`, and `io.print`, not only by an explicit
`as string` cast. A class without `__string` falls back to the default
inspection form (`<ClassName>`).

```gb
let m = Money(550);
io.println(m);           # $550
io.println("paid ${m}"); # paid $550
```

## Destructors

A class can declare a destructor with the `func ~ClassName()` syntax. The
destructor takes no arguments and is called when an instance reaches the end
of its lifetime - either at program exit (the runtime sweeps every
destructor-bearing instance that hasn't already been destroyed) or via an
explicit `del x;` statement. Destructors are end-of-lifetime hooks; they are
**not** tied to `with`-blocks, which serve a separate purpose (see *Context
Managers* below).

```gb
import io;

class FileHandle {
    string path;
    int fd;

    func FileHandle(string path) {
        this.path = path;
        this.fd = io.open(path, "r");
    }

    func ~FileHandle() {
        io.close(this.fd);
        io.println("closed " + this.path);
    }
}

let f = FileHandle("data.txt");
/* ... use f ... */
/* At program exit (or when `del f;` runs), ~FileHandle fires. */
```

At the program-exit sweep, destructors fire in reverse-creation order (LIFO)
so younger instances - which may depend on older ones - clean up first.
Destructor exceptions are logged to stderr but never abort the sweep; every
remaining instance still gets a chance to run.

### Explicit destruction with `del`

Use `del x;` to retire a binding mid-script. The runtime invokes the
destructor (if the class declares one) immediately and removes the binding
from the surrounding scope:

```gb
let f = FileHandle("data.txt");
useFile(f);
del f;          /* ~FileHandle fires here. */
io.println("file already closed");
```

`del` only accepts an identifier - `del a.b;` and `del a[0];` are parse
errors. After `del x`, the static analyzer rejects subsequent references to
`x` in the same control-flow path with `use of destroyed binding "x"`. A
fresh `let x = ...;` re-introduces the name with a new lifetime.

Destructors that throw during a sweep print the error to stderr but do not
crash the sweep.

## Iterator Protocol (`__iter`, `__done`, `__next`) (1.0.6)

A class becomes usable in `for (x in obj)` by implementing the iterator
protocol. Two methods drive each step:

- `__done(): bool` returns `true` when iteration is exhausted.
- `__next()` returns the next value (called only when `__done()` is
  `false`).

A class that exposes both `__done` and `__next` directly is its own
iterator. To make a class iterable without making it the iterator
itself, also implement `__iter()` which returns the iterator (often a
fresh instance, or `this` after resetting internal state):

```gb
class Range {
    int from;
    int to;
    int cur;

    func Range(int from, int to) {
        this.from = from;
        this.to = to;
        this.cur = from;
    }

    func __iter(): Range {
        this.cur = this.from;
        return this;
    }

    func __done(): bool {
        return this.cur >= this.to;
    }

    func __next(): int {
        int v = this.cur;
        this.cur = this.cur + 1;
        return v;
    }
}

for (n in Range(2, 5)) {
    io.println(n);
}
/* 2
 * 3
 * 4
 */
```

The loop calls `__iter()` once at the start to obtain the iterator,
then alternates `__done()` and `__next()` until `__done()` returns
`true`. `__iter()` can return any class instance that exposes
`__done`/`__next`; this lets a single iterable produce fresh
iterators for nested or repeated traversal.

When a class has no `__iter()` but does expose `__next`/`__done`, the
instance itself is used as the iterator. Useful for one-shot
iterators that should not be restarted.

User-defined iterables compose with `iterable<T>` parameters and slot
straight into the generator/list/dict iteration paths.

## Context Managers (`with`, `__enter`, `__exit`)

The `with` statement runs the magic methods `__enter()` and `__exit()`
on the bound resource. It is a scoped-cleanup construct, distinct from the
destructor lifecycle.

```gb
class Transaction {
    string label;

    func Transaction(string label) { this.label = label; }

    func __enter(): Transaction {
        io.println("begin " + this.label);
        return this;
    }

    func __exit(): void {
        io.println("commit " + this.label);
    }
}

with (tx = Transaction("write")) {
    io.println("inside " + tx.label);
}
/* Output:
 *   begin write
 *   inside write
 *   commit write
 */
```

Two forms are accepted: `with (expr) { ... }` discards the value;
`with (name = expr) { ... }` binds the result of `__enter()` (or the
expression itself when `__enter()` is undefined) to `name`. At block exit
- normal completion, exception, `return`, `break`, or `continue` - the
runtime invokes `__exit()` if defined; otherwise the block exits as a
no-op. The class destructor is **not** called at this point; it fires later
via the lifetime mechanism described above.

If you want both - per-block cleanup AND end-of-lifetime cleanup - declare
both methods.

## Serialisation: `__serialize` And `__deserialize`

Class instances serialise out of the box. `json.stringify`, `yaml.stringify`,
and `toml.stringify` accept an instance and dump its **public** fields:

- Fields whose name does not start with `_` are emitted.
- Fields beginning with `_` or `__` are treated as private and skipped.

No opt-in is needed for plain data classes.

```gb
import json;

class Point {
    int x;
    int y;
    int _secret;
    func Point(int x, int y) { this.x = x; this.y = y; this._secret = 99; }
}

io.println(json.stringify(Point(3, 4)));
/* {"x":3,"y":4} - _secret is omitted. */
```

A class can replace the default by implementing `__serialize()`. The return
value is itself serialised by the stringify call, so any dict/list/scalar shape
works:

```gb
class Tagged {
    string kind;
    string label;
    func Tagged(string kind, string label) {
        this.kind = kind; this.label = label;
    }
    func __serialize(): dict {
        return {"kind": this.kind, "label": this.label, "v": 1};
    }
}
```

The symmetric `parseAs(text, ClassRef)` reconstructs an instance:

```gb
let p = json.parseAs("{\"x\": 3, \"y\": 4}", Point);
io.println(p.x);
io.println(p.y);
```

`parseAs` first looks for a static `__deserialize(dict)` factory on the
target class. When present it is called with the parsed value:

```gb
class Tagged {
    string kind;
    string label;
    func Tagged(string kind, string label) {
        this.kind = kind; this.label = label;
    }
    static func __deserialize(dict d): Tagged {
        return Tagged(d["kind"], d["label"]);
    }
}
```

When no `__deserialize` exists, `parseAs` matches the dict keys to the
constructor's parameter names and calls the constructor positionally. A
missing required parameter raises a runtime error.

The same machinery applies to `yaml.parseAs`, `toml.parseAs`, and
`xml.parseAs`.

## Enums

```gb
enum Color { Red, Green, Blue }
enum Result { Ok(string), Err(string) }

let color = Color.Red;
let result = Result.Ok("saved");
```

Enums support equality, `instanceof`, and match destructuring.

Enums are a good fit for closed sets and tagged results:

```gb
enum SaveResult {
    Saved(string),
    Duplicate(string),
    Failed(string)
}

let result = SaveResult.Duplicate("email");
let message = match (result) {
    case SaveResult.Saved(string id) => "saved " + id;
    case SaveResult.Duplicate(string field) => "duplicate " + field;
    case SaveResult.Failed(string error) => error;
};
```

### Backed enums

An enum can be backed by a distinct scalar value. Backing types are limited to
`string` and `int`, and every case in a backed enum must declare a unique
literal value:

```gb
enum Status: string {
    Active = "active";
    Closed = "closed";
}

Status.Active.value;      # "active"
Status.from("active");    # Status.Active
Status.tryFrom("x");      # null
```

`from(value)` returns the matching variant or throws when no variant has that
backing value. `tryFrom(value)` returns the matching variant or `null`. The
argument type must match the enum backing type. A variant can have associated
data or a backing value, not both.

```gb
enum Code: int {
    Ok = 200;
    NotFound = 404;
}

Code.NotFound.value;   # 404
Code.from(200);        # Code.Ok
```

### Enum methods

An enum may declare instance methods, callable on any variant. The body is the
variant list, then a single `;`, then the method declarations:

```gb
enum Status {
    Active, Suspended, Closed(string);

    func isTerminal(): bool {
        return match (this) {
            case Status.Closed(string r) => true;
            default => false;
        };
    }

    func describe(): string {
        return match (this) {
            case Status.Active        => "active";
            case Status.Suspended     => "suspended";
            case Status.Closed(string r) => "closed: " + r;
        };
    }
}

let s = Status.Closed("fraud");
s.isTerminal();   # true
s.describe();     # "closed: fraud"
```

Inside a method `this` is the receiving variant, typed as the enum. Per-variant
behaviour is expressed with `match (this)` in one body; there is no per-variant
override. A method may call sibling methods on `this`. Methods sit beside the
existing data access: associated values and a backed scalar are read first, so a
method never shadows them. A method named like a built-in variant accessor
(`variant`, `fields`, and `value` on backed enums) is a compile error.

Enums remain immutable value types. A method cannot mutate the receiver; the bare
`enum Name { A, B }` form is unchanged.

### Enums and interfaces

An enum may implement one or more interfaces with `implements`, mirroring the
class form (there is no `extends` for enums). Every interface method must be
satisfied by a declared enum method of matching arity, or the program is
rejected; an interface default applies when the enum leaves a method
unimplemented. A conforming enum value flows into an interface-typed slot, and
interface-typed dispatch lands on the enum's method:

```gb
interface Describable { func describe(): string; }

enum Status implements Describable {
    Active, Closed(string);

    func describe(): string {
        return match (this) {
            case Status.Active => "active";
            case Status.Closed(string r) => "closed: " + r;
        };
    }
}

Describable d = Status.Active;
d.describe();                       # "active"
Status.Active instanceof Describable;  # true
```

### Enum static surface

Two operations are available on the enum type itself:

- `EnumName.values()` returns a `list` of the simple (nullary) variants, in
  declaration order.
- `EnumName.fromName(s)` resolves a variant by its exact name and returns
  `?Variant`: the matching variant, or `null` when no variant has that name.
  The match is case-sensitive.
- Backed enums additionally expose `EnumName.from(value)` and
  `EnumName.tryFrom(value)` lookup by backing value.

```
enum Status { Active, Suspended, Closed }

Status.values();              # [Status.Active, Status.Suspended, Status.Closed]
Status.fromName("Suspended"); # Status.Suspended
Status.fromName("unknown");   # null
Status.fromName("active");    # null (case-sensitive)
```

Tagged variants are excluded from both: a bare name cannot construct a variant
that carries fields, so `values()` lists only the nullary variants and
`fromName` resolves only their names. Because `fromName` returns `?Variant`, a
caller can supply a fallback at the call site without catching an error:

```
Status s = Status.fromName(input) ?? Status.Active;
```

Apart from `values`, `fromName`, and backed-enum `from` / `tryFrom`, the enum
surface is instance methods and interface implementation only.
