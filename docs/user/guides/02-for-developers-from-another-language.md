# Geblang for developers from another language

## Who this is for

This guide is for developers arriving from Python, JavaScript, Go, Java, or PHP
who want to orient quickly to Geblang. It maps the concepts you already know onto
Geblang's equivalents, highlights where the language diverges from what you
expect, and points you at the relevant reference chapters. You do not need to
read this front to back; jump to the row or section that matches your
background.

Geblang takes much of its inspiration from PHP, so PHP developers will find a lot
already familiar: named arguments (`f(x: 1)`), `parent::` becoming `parent(...)`,
`?T` nullable types, `interface` / `extends`, and interpolation in double-quoted
strings all echo PHP.

## Quick orientation

The following program is a complete Geblang file. It defines a typed function,
a class with a constructor and a method, and a small entry point:

```gb
import io;

func greet(string name, int times = 1): string {
    string result = "";
    for (int i = 0; i < times; i++) {
        result = result + "Hello, " + name + "!\n";
    }
    return result;
}

class Counter {
    int value;

    func Counter(int start) {
        this.value = start;
    }

    func increment(): void {
        this.value = this.value + 1;
    }

    func get(): int {
        return this.value;
    }
}

io.print(greet("Ada", 2));

let c = Counter(0);
c.increment();
c.increment();
io.println("count: ${c.get()}");
```

Output:
```
Hello, Ada!
Hello, Ada!
count: 2
```

Things to notice:
- Types come before parameter names: `string name`, not `name: string`.
- The return type follows the parameter list: `): string`.
- Constructors share the class name; `this` refers to the instance.
- Double-quoted strings interpolate via `${expr}`.
- `#` starts a line comment; `//` is integer division.

## Coming from another language: concept mapping

| Concept | Python | JavaScript | Go | Java | PHP | Geblang |
|---------|--------|------------|----|------|-----|---------|
| Variable declaration | `x = 1` | `let x = 1` | `x := 1` | `int x = 1;` | `$x = 1;` | `int x = 1;` or `let x = 1;` |
| Typed declaration | (type annotations: `x: int = 1`) | (JSDoc / TypeScript: `let x: number`) | `var x int = 1` | `int x = 1;` | (typed params / properties only) | `int x = 1;` |
| Constant | (no keyword; convention `X = 1`) | `const X = 1` | `const X = 1` | `final int X = 1;` | `const X = 1;` | `const X = 1;` |
| Inferred variable | `x = 1` | `let x = 1` | `x := 1` | `var x = 1;` (Java 10+) | `$x = 1;` | `let x = 1;` |
| Function definition | `def add(a: int, b: int) -> int:` | `function add(a, b) { }` | `func add(a, b int) int { }` | `int add(int a, int b) { }` | `function add(int $a, int $b): int { }` | `func add(int a, int b): int { }` |
| Default parameters | `def f(x=0):` | `function f(x = 0) { }` | (not supported) | (overloads) | `function f($x = 0) { }` | `func f(int x = 0): void { }` |
| Named arguments | (positional or keyword: `f(x=1)`) | (not built in) | (not supported) | (not supported) | `f(x: 1)` (PHP 8+) | `f(x: 1)` |
| Lambda / anonymous func | `lambda x: x + 1` | `x => x + 1` | `func(x int) int { return x + 1 }` | `x -> x + 1` | `fn($x) => $x + 1` | `func(int x): int { return x + 1; }` |
| Class definition | `class User:` | `class User { }` | (no classes; structs) | `class User { }` | `class User { }` | `class User { }` |
| Constructor | `def __init__(self, name):` | `constructor(name) { }` | (struct literal / factory) | `User(String name) { }` | `function __construct($name) { }` | `func User(string name) { }` |
| Inheritance | `class Admin(User):` | `class Admin extends User { }` | (embedding) | `class Admin extends User { }` | `class Admin extends User { }` | `class Admin extends User { }` |
| Parent constructor | `super().__init__(name)` | `super(name)` | (not applicable) | `super(name);` | `parent::__construct($name);` | `parent(name);` |
| Interface | (Protocol / ABC) | (TypeScript interface) | `type I interface { }` | `interface I { }` | `interface I { }` | `interface I { }` |
| Generics | (type hints: `list[T]`) | (TypeScript: `function f<T>`) | `func f[T any](v T) T` | `<T> T f(T v)` | (no native generics; `@template` docblocks) | `func f<T>(T v): T` |
| Module import | `import math` | `import * as math from 'math'` | `import "math"` | `import java.lang.Math;` | `require 'math.php';` | `import math;` |
| Named import | `from math import sqrt` | `import { sqrt } from 'math'` | (dot access after import) | `import static ...Math.sqrt;` | `use function Math\sqrt;` | `from math import sqrt;` |
| Error handling | `try: ... except ValueError as e:` | `try { } catch (e) { }` | `if err != nil { }` | `try { } catch (Exception e) { }` | `try { } catch (ValueError $e) { }` | `try { } catch (ValueError e) { }` |
| Throw / raise | `raise ValueError("msg")` | `throw new Error("msg")` | `return nil, errors.New("msg")` | `throw new RuntimeException("msg");` | `throw new RuntimeException("msg");` | `throw RuntimeError("msg");` |
| Async function | `async def f():` | `async function f() { }` | (goroutine / channel) | `CompletableFuture<T>` | (no built-in async; Fibers) | `async func f(): T { }` |
| Await | `await coro` | `await promise` | (channel receive: `<-ch`) | `.get()` / `.join()` | (Fiber suspend / resume) | `await task` |
| List / array | `[1, 2, 3]` | `[1, 2, 3]` | `[]int{1, 2, 3}` | `List.of(1, 2, 3)` | `[1, 2, 3]` | `[1, 2, 3]` as `list<int>` |
| Dictionary / map | `{"a": 1}` | `{a: 1}` | `map[string]int{"a": 1}` | `Map.of("a", 1)` | `["a" => 1]` | `{"a": 1}` as `dict<string, int>` |
| Set | `{1, 2, 3}` | `new Set([1, 2, 3])` | `map[int]struct{}` | `Set.of(1, 2, 3)` | (no native set; array keys) | `{1, 2, 3}` as `set<int>` |
| String interpolation | `f"Hello {name}"` | `` `Hello ${name}` `` | `fmt.Sprintf("Hello %s", name)` | `"Hello " + name` (text blocks in Java 15+) | `"Hello {$name}"` | `"Hello ${name}"` (double-quoted only) |
| Integer division | `x // y` | `Math.floor(x / y)` | `x / y` (int operands) | `x / y` (int operands) | `intdiv($x, $y)` | `x // y` |
| True division | `x / y` | `x / y` | (float operands) | (float or cast) | `$x / $y` | `x / y` (returns decimal) |
| Null / nil | `None` | `null` / `undefined` | `nil` | `null` | `null` | `null` |
| Nullable type | `Optional[T]` / `T \| None` | `T \| null` (TypeScript) | `*T` (pointer) | `Optional<T>` | `?T` | `?T` |

## Key features for you

### Static typing with inference

Geblang is statically typed. Every value has a compile-time type; the analyzer
rejects mismatches before the program runs. When the type is obvious from the
initializer, `let` infers it:

```gb
import io;

let count = 0;        /* int inferred */
let ratio = 3.5;      /* decimal inferred */
let name  = "Ada";    /* string inferred */

count = count + 1;
io.println("${name}: ${count}");
```

Use an explicit type when it documents intent or narrows a nullable:

```gb
import io;

func clamp(int value, int lo, int hi): int {
    if (value < lo) { return lo; }
    if (value > hi) { return hi; }
    return value;
}

io.println(clamp(150, 0, 100));
```

Three numeric types exist: `int` (arbitrary-precision integer), `decimal`
(exact base-10), and `float` (IEEE 754). A bare decimal literal like `3.5` is a
`decimal`, not a float. Float literals need an `f` suffix: `3.5f`. See
[Types](../03-types.md) and [Syntax Basics](../02-syntax-basics.md).

### Classes, interfaces, and generics

Classes have explicit field declarations and a constructor named after the class.
Interfaces declare method contracts. Generics use angle-bracket type parameters:

```gb
import io;

interface Greeter {
    func greet(): string;
}

class Person implements Greeter {
    string name;

    func Person(string name) {
        this.name = name;
    }

    func greet(): string {
        return "Hi, I'm " + this.name;
    }
}

class Bot implements Greeter {
    string id;

    func Bot(string id) {
        this.id = id;
    }

    func greet(): string {
        return "Bot " + this.id + " online";
    }
}

func introduce(Greeter g): void {
    io.println(g.greet());
}

introduce(Person("Ada"));
introduce(Bot("R2D2"));
```

Inheritance uses `extends`; call the parent constructor with `parent(args)`,
not `super`:

```gb
import io;

class Animal {
    string name;
    func Animal(string name) { this.name = name; }
    func speak(): string { return this.name + " makes a sound"; }
}

class Dog extends Animal {
    func Dog(string name) { parent(name); }
    func speak(): string { return parent.speak() + ", then barks"; }
}

let d = Dog("Rex");
io.println(d.speak());
```

Generics track type bindings at runtime (they are reified, not erased):

```gb
import io;

class Box<T> {
    T value;
    func Box(T value) { this.value = value; }
    func get(): T { return this.value; }
}

let b = Box<string>("hello");
io.println(b.get());
```

See [Classes, Interfaces, and Enums](../06-classes-interfaces.md) and
[Types - Generics](../03-types.md).

### Modules and packages

Import a module by name. Use `from ... import ...` to bring a name into scope
without the prefix:

```gb
import io;
import math;
from math import sqrt;

io.println(math.pi());
io.println(sqrt(16.0));
```

Export declarations with `export` in your own modules; unexported names are
private. See [Modules and Packages](../07-modules-packages.md).

### Error handling

Errors must extend `Error`. Use `try`/`catch`/`finally`:

```gb
import io;

class ParseFailure extends RuntimeError {
    func ParseFailure(string msg) { parent(msg); }
}

func parseAge(string raw): int {
    if (!raw.isInt()) {
        throw ParseFailure("not an integer: " + raw);
    }
    return raw.toInt();
}

try {
    io.println(parseAge("42"));
    io.println(parseAge("bad"));
} catch (ParseFailure e) {
    io.println("parse error: " + e.message);
} catch (Error e) {
    io.println("unexpected: " + e.message);
}
```

Catch clauses are checked in order; put specific classes before broader ones.
See [Errors](../08-errors.md).

### Async and generators

Async functions return a `Task`. Calling one starts it immediately; `await`
blocks until the result is ready:

```gb
import io;
import async;

async func fetch(string label, int delay): string {
    await async.sleep(delay);
    return label + " done";
}

let a = fetch("A", 50);
let b = fetch("B", 30);

io.println(await b);
io.println(await a);
```

Generator functions use `yield` and produce a lazy `generator<T>` value:

```gb
import io;

func countdown(int from): generator<int> {
    for (int i = from; i > 0; i--) {
        yield i;
    }
}

for (n in countdown(3)) {
    io.println(n);
}
```

See [Async and Generators](../09-async-generators.md).

## Gotchas

These are the points most likely to trip up developers arriving from another
language.

**`parent()` not `super`**
Call the parent constructor with `parent(args)`. Reference the parent
implementation in an override with `parent.method()`. There is no `super`
keyword.

**`/* */` comments only; `#` for line comments; `//` is integer division**
`/* block comment */` and `# line comment` are the comment forms. `//` is the
floor-division operator, not a comment. This is the single sharpest edge for Go
and C-family developers.

```gb
import io;

let a = 7;
let b = 2;
io.println(a // b);   /* 3 - floor division, not a comment */
io.println(a / b);    /* 3.5000000000 - true division, returns decimal */
```

**Type-first parameter syntax**
Parameters are declared as `type name`, not `name: type`:

```gb
func add(int a, int b): int { return a + b; }   /* correct */
/* func add(a: int, b: int): int - this is NOT valid Geblang syntax */
```

**Double-quoted strings interpolate; single-quoted do not**
`"${expr}"` interpolates any expression. `'text'` is a raw literal with no
interpolation and no escape processing:

```gb
import io;

let x = 42;
io.println("value is ${x}");    /* value is 42 */
io.println('value is ${x}');    /* value is ${x} - literal text */
```

**`list.push(x)` mutates in place and returns the receiver**
Since 1.16.0, list mutators (`push`, `removeAt`, etc.) modify the list in place
and return the receiver. Use the list methods `.sorted()`, `.reversed()`, or
`.copy()` when you need a new list that leaves the original untouched:

```gb
import io;

let nums = [1, 2, 3];
nums.push(4);
io.println(nums);            /* [1, 2, 3, 4] */

let copy = nums.sorted();
io.println(copy);            /* [1, 2, 3, 4] */
io.println(nums);            /* [1, 2, 3, 4] - unchanged */
```

**`decimal` and `float` do not mix in arithmetic**
A bare decimal literal (`3.5`) is a `decimal`, not a float. Float literals need
the `f` suffix (`3.5f`). Mixing `decimal` and `float` in arithmetic is a type
error; cast one side explicitly:

```gb
import io;

decimal d = 1.5;
float   f = 2.0f;
io.println(d * (f as decimal));   /* 3.0000000000 */
```

**`/` returns `decimal`; use `//` for integer division**
In Geblang, `7 / 2` is `3.5` (a `decimal`), not `3`. Use `7 // 2` to get the
integer `3`. Assigning a division result directly to `int` is a compile-time
error.

**Conditions must be explicit booleans**
Geblang does not treat empty strings, zero, or empty collections as falsy.
Write explicit comparisons:

```gb
import io;

let items = [1, 2, 3];
if (items.length() > 0) {           /* correct */
    io.println(items.first());
}
/* if (items) - not valid; items is not a bool */
```

**No `char` type**
There is no separate character type. A single character is a `string` of length
one. Iterate over characters with `.chars()` on a string.

## Where to go next

- [Syntax Basics](../02-syntax-basics.md) - variables, strings, numbers,
  operators, and collections in detail.
- [Types](../03-types.md) - the full type system: primitives, nullability,
  unions, generics, and casts.
- [Functions and Callables](../05-functions-callables.md) - defaults, named
  arguments, overloads, closures, and the pipe operator.
- [Classes, Interfaces, and Enums](../06-classes-interfaces.md) - inheritance,
  interfaces, static members, decorators, and enums.
- [Modules and Packages](../07-modules-packages.md) - imports, exports, and
  project structure.
- [Errors](../08-errors.md) - the error hierarchy and custom error classes.
- [Async and Generators](../09-async-generators.md) - tasks, channels, and
  lazy generators.
- [examples/](https://github.com/dwgebler/geblang/tree/main/examples) - runnable example programs covering the full
  language surface.
