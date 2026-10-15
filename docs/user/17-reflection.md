# Reflection

Reflection lets a program inspect its own types, classes, functions, and
metadata at runtime. Geblang's reflection surface is built into the language
(`typeof`, the `.type` shorthand, `instanceof`) and the `reflect` module.
Everything described here works at runtime and behaves identically on both
execution backends (the evaluator used by `geblang test` and the bytecode VM
used by `geblang run` and `geblang build`).

Reflection is what makes Geblang's metadata-driven features possible: decorators
are inspectable rather than erased, generics are reified, and frameworks can
discover classes, read their fields and decorators, and bind request data to
typed handler parameters without any framework syntax in the language itself.

```gb
import reflect;
```

The import is optional (1.16.0): an unshadowed bare `reflect.X(...)`
dispatches ambiently on both backends. Importing it explicitly remains
the documented style.

## Type introspection

`typeof(x)` returns the runtime type name of any value as a string:

```gb
import io;

io.println(typeof(42));        # int
io.println(typeof("hi"));      # string
io.println(typeof([1, 2, 3])); # list
io.println(typeof(3.14));      # decimal
```

Every value also carries a `.type` shorthand that returns the same name:

```gb
let x = 42;
io.println(x.type);   # int
```

For a richer type value (rather than a bare name string), use
`reflect.typeOf`, which returns a `Type` value:

```gb
io.println(reflect.typeOf(42));         # int
io.println(reflect.typeOf("hi"));       # string
io.println(reflect.typeOf([1, 2, 3]));  # list
```

### instanceof

`instanceof` tests whether a value is an instance of a class, a subclass, or an
implementer of an interface:

```gb
interface Greeter {
    func greet(): string;
}

class Animal {
    string name;
    func Animal(string name) { this.name = name; }
}

class Dog extends Animal implements Greeter {
    func Dog(string name) { parent(name); }
    func greet(): string { return "Woof"; }
}

let d = Dog("Rex");
io.println(d instanceof Dog);      # true
io.println(d instanceof Animal);   # true (subclass)
io.println(d instanceof Greeter);  # true (implements)
```

### Reified generics

Generic type parameters are reified, so `instanceof` can test the element type
of a parameterized collection - and, with the same invariant model, the
recorded bindings of a user generic class instance:

```gb
let xs = [1, 2, 3];
io.println(xs instanceof list<int>);     # true
io.println(xs instanceof list<string>);  # false

class Box<T> {
    T value;
    func Box(T v) { this.value = v; }
}
let b = Box<string>("hi");
io.println(b instanceof Box<string>);    # true
io.println(b instanceof Box<int>);       # false
```

`reflect.typeBindings(value)` returns a dict mapping each type parameter name to
the concrete type it was bound to. This is how generic containers, validators,
and wrappers discover what they were parameterized with:

```gb
class Box<T> {
    T value;
    func Box(T v) { this.value = v; }
}

let b = Box<int>(5);
io.println(reflect.typeBindings(b));   # {"T": "int"}
```

## Looking things up by name or value

`reflect.class`, `reflect.function`, and `reflect.module` each accept either a
name string or a value of the matching kind, and return the corresponding
reflectable target (or `null` when nothing resolves):

```gb
class Dog {
    func Dog() {}
}

let d = Dog();

let byName  = reflect.class("Dog");   # look up by name
let byValue = reflect.class(d);       # extract the class from an instance

io.println(reflect.className(byName));   # Dog
io.println(reflect.className(byValue));  # Dog
```

`reflect.function(name)` returns an inspectable handle for a top-level function.
Pass the handle to `reflect.parameters`, `reflect.returnType`,
`reflect.decorators`, and the other introspection calls below:

```gb
func helper(int n, string s = "a"): int { return n; }

let f = reflect.function("helper");
io.println(reflect.returnType(f));    # int
io.println(reflect.parameters(f));    # [{... "name": "n" ...}, {... "name": "s" ...}]

io.println(reflect.function("missing"));   # null
```

### Reflecting over imported native modules

`reflect.module("name")` resolves an imported module by its string name.
`reflect.class` resolves a native module's class exports using the
`module.Class` form:

```gb
import http;
import io;
import reflect;

let request = reflect.class("http.Request");
io.println(reflect.className(request));   # Request
```

`reflect.function` also resolves a native module's functions (1.16.0). The
result is a first-class callable, the same value `math.sqrt` produces as an
expression. Unknown members return `null`:

```gb
import math;
import io;
import reflect;

let sqrt = reflect.function("math.sqrt");
io.println(sqrt(16.0));                       # 4
io.println(reflect.function("math.nope"));    # null
```

Native functions carry no source-level metadata, so structural calls such as
`reflect.parameters` or `reflect.location` report nothing useful for them;
they resolve and call.

## Class reflection

Given a class value (from `reflect.class`, an instance, or a class name used
directly), these calls report its structure. Field and method listings cover the
class's own declared members; inherited members live on the parent class, which
you can reach with `reflect.parent`.

```gb
@immutable
class Animal {
    string name;
    int legs = 4;
    func Animal(string name) { this.name = name; }
    func describe(): string { return this.name; }
}

class Dog extends Animal implements Greeter {
    ?string breed;
    func Dog(string name, ?string breed) {
        parent(name);
        this.breed = breed;
    }
    func greet(): string { return "Woof"; }
}
```

`reflect.className(target)` returns the class name as a string:

```gb
io.println(reflect.className(Dog));   # Dog
```

`reflect.fields(class)` returns one dict per declared field, each with `name`,
`type`, `nullable`, `hasDefault`, `doc`, and `decorators`. A docblock (a `##`
line comment or a `/** ... */` block) written immediately before a field is
surfaced as its `doc` string; fields without one report `doc` as `null`:

```gb
class Dog {
    /** the breed, or null if unknown */
    ?string breed;
}

io.println(reflect.fields(Dog));
# [{"decorators": [], "doc": "the breed, or null if unknown", "hasDefault": false, "name": "breed", "nullable": true, "type": "?string"}]
```

`reflect.methods(class)` returns the names of the class's own methods:

```gb
io.println(reflect.methods(Dog));   # ["greet"]
```

`reflect.constructors(class)` returns one entry per constructor overload, each a
list of parameter dicts (see [parameter metadata](#parameter-metadata) below):

```gb
io.println(reflect.constructors(Dog));
# [[{"hasDefault": false, "name": "name", "type": "string", "variadic": false},
#   {"hasDefault": false, "name": "breed", "type": "?string", "variadic": false}]]
```

`reflect.parent(class)` returns the parent class name as a string, or `null`
when the class has no parent. `reflect.interfaces(class)` returns the names of
the interfaces the class implements:

```gb
io.println(reflect.parent(Dog));       # Animal
io.println(reflect.parent(Animal));    # null
io.println(reflect.interfaces(Dog));   # ["Greeter"]
```

### Parameter metadata

`reflect.parameters(function)` returns one dict per parameter. Each entry has
`name`, `type`, `hasDefault`, and `variadic`. `reflect.returnType(function)`
returns the declared return type:

```gb
func handler(int id, string name = "anon"): string { return name; }

io.println(reflect.parameters(handler));
# [{"hasDefault": false, "name": "id", "type": "int", "variadic": false},
#  {"hasDefault": true, "name": "name", "type": "string", "variadic": false}]

io.println(reflect.returnType(handler));   # string
```

The same shape is produced for class fields (`reflect.fields`), constructors
(`reflect.constructors`), and methods, so framework code can walk parameters
uniformly.

### Enumerating classes

`reflect.classes()` takes no arguments and returns a list of every class
declared in the program. Frameworks use it to scan for classes carrying a
particular decorator:

```gb
let all = reflect.classes();
io.println(all instanceof list);   # true
```

## Instances and fields

`reflect.getField(instance, name)` reads a named field off an instance, and
returns `null` when the field is absent (no separate existence probe needed):

```gb
let d = Dog("Rex", "Lab");
io.println(reflect.getField(d, "name"));    # Rex
io.println(reflect.getField(d, "breed"));   # Lab
io.println(reflect.getField(d, "missing")); # null
```

`reflect.setField(instance, name, value)` writes a named field. It accepts a
field that was not declared, which is what dynamic binding code (PATCH-style
partial updates, deserializers) needs. A declared field keeps its type: a value
of the wrong type throws `TypeError`, exactly as a normal assignment does.

```gb
reflect.setField(d, "name", "Fido");
io.println(d.name);   # Fido
```

## Decorators and metadata

Decorators in Geblang are inspectable, not erased. `reflect.decorators(target)`
returns the decorator metadata attached to a function, class, method, or field.
Each entry carries the decorator `name`, its positional `args`, its `namedArgs`,
the `target` kind, and the source `line` / `column`:

```gb
@tag("admin")
func handler(int id): string { return ""; }

io.println(reflect.decorators(handler));
# [{"args": ["admin"], "column": 1, "line": 1, "name": "tag",
#   "namedArgs": {}, "overload": 0, "position": 0, "target": "function"}]
```

`reflect.hasDecorator(target, name)` reports whether a target carries a named
decorator, and `reflect.decorator(target, name)` returns that single decorator's
metadata dict (or `null` when absent):

```gb
@immutable
class Widget {
    func Widget() {}
}

io.println(reflect.hasDecorator(Widget, "immutable"));   # true
io.println(reflect.hasDecorator(Widget, "sealed"));      # false
io.println(reflect.decorator(Widget, "immutable"));
# {"args": [], "column": 1, "line": 1, "name": "immutable",
#  "namedArgs": {}, "overload": 0, "position": 0, "target": "class"}
```

A decorator can act as pure metadata for reflection, as a callable wrapper, or
both. See [Functions and callables](05-functions-callables.md#decorators) for
how to define and apply decorators.

## Docblocks

A docblock written immediately before a declaration is retained and readable at
runtime. A docblock is either a run of `##` line comments or a `/** ... */`
block placed directly above the declaration.

`reflect.doc(target)` returns the raw docblock string for a function, method,
class, or interface, or `null` when it has none:

```gb
/** Greets a person by name. */
func greet(string name): string { return "hi " + name; }

io.println(reflect.doc(greet));   # Greets a person by name.
```

`reflect.docs(target)` returns the same text parsed into a dict with `text` (the
full docblock), `summary` (the first non-empty line), `body` (everything after
the summary), and `lines` (the raw lines):

```gb
io.println(reflect.docs(greet)["summary"]);   # Greets a person by name.
```

Field docblocks are surfaced separately, through the `doc` key of each
`reflect.fields` entry (see [Class reflection](#class-reflection) above).

## Modules

`reflect.module("name")` resolves an imported module by name. `reflect.exports`,
`reflect.location`, and friends accept the resulting module value.

`reflect.exports(module)` lists the names a user module exports (functions and
classes):

```gb
# in mymod.gb:
#   export func add(int a, int b): int { return a + b; }
#   export func sub(int a, int b): int { return a - b; }
#   export class Thing { int n; func Thing(int n) { this.n = n; } }

import mymod;
import io;
import reflect;

let m = reflect.module("mymod");
io.println(reflect.exports(m));   # ["Thing", "add", "sub"]
```

`reflect.location(target)` returns the source position of a function or class
declaration as `{module, line, column}`, or `null` when no position was
recorded:

```gb
let cls = reflect.class("Thing");
io.println(reflect.location(cls)["line"]);   # the line Thing was declared on
```

## dir: the lightweight companion

`dir(value)` is a lighter-weight tool for listing the methods available on a
value. It returns a sorted list of names and works on instances, primitives, and
modules:

```gb
io.println(dir([1, 2, 3]).contains("push"));   # true
io.println(reflect.methods("hi").contains("upper"));   # true
```

`reflect.methods` and `dir` overlap for listing callable members; reach for
`reflect.methods` when you are already inside a reflection workflow, and `dir`
for quick interactive exploration. See [Syntax basics](02-syntax-basics.md) for
the introduction to `dir` and `typeof`.

## What is and isn't reflectable

- **User classes, functions, fields, and methods** are fully reflectable:
  structure, parameters, decorators, and locations are all available.
- **Decorators** are inspectable metadata, not erased.
- **Docblocks** are retained: `reflect.doc` / `reflect.docs` expose the comment
  written above a function, method, class, or interface, and each
  `reflect.fields` entry carries its field's `doc`.
- **Generics** are reified: `instanceof list<int>` and `reflect.typeBindings`
  resolve concrete type arguments at runtime.
- **Native module class exports** are reflectable via the `module.Class` form
  (`reflect.class("http.Request")`).
- **Native module functions** resolve to first-class callables
  (`reflect.function("math.sqrt")`, 1.16.0). They carry no source-level
  structure, so parameter and location introspection reports nothing for them.
- **Field and method listings cover a class's own declared members.** Inherited
  members live on the parent class, reachable with `reflect.parent`.
