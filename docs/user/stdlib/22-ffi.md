# Foreign Function Interface

`import ffi;` gives Geblang scripts direct access to C-ABI shared
libraries: libtorch, libsqlite, libcurl, libopencv, libssl, or
anything else with a stable C interface. The dispatcher sits
in-process so calls have no IPC overhead - what `crypt.sha256`
costs, an FFI call costs.

FFI is the lower-latency counterpart to the subprocess-based
[`ext` protocol](env-ext.html). Pick FFI for tight numeric
loops and library bindings where every microsecond matters; pick
`ext` for long-running services or untrusted code that must run
isolated from the script.

## Capability gating

FFI is off by default. A project enables it in `geblang.yaml`:

```yaml
name: myapp
version: 0.1.0
source: src
permissions:
  ffi:
    enabled: true
    libraries:
      - path: /usr/lib/x86_64-linux-gnu/libm.so.6
      - glob: /opt/torch/lib/*.so
      - glob: libsqlite3*
```

A standalone script (no project manifest) opts in on the command
line:

```sh
geblang --allow-ffi 'libm.so.*' script.gb
geblang --allow-ffi /usr/lib/libcurl.so.4 --allow-ffi 'libssl.so.*' script.gb
```

Patterns containing `*`, `?`, or `[` are treated as globs; bare
paths are exact-match. Either form works in the manifest or on
the CLI; the CLI overlay additively extends what the manifest
declared.

When you `geblang build` a project, the resolved FFI policy is
baked into the binary (along with `--allow-ffi` build flags), so
the binary runs without re-declaring the allow-list. See
[Bundling](../13-bundling.md).

`geblang doctor` surfaces the active policy:

```text
ffi: enabled, 2 allow-list rule(s)
  path /usr/lib/x86_64-linux-gnu/libm.so.6
  glob /opt/torch/lib/*.so
```

A blocked load throws `PermissionError` with a message naming the
path and the policy entry that would need to change. The error is
catchable:

```gb
try {
    let lib = ffi.dlopen("/etc/something-forbidden.so");
} catch (PermissionError e) {
    io.println("blocked: " + e.message);
}
```

## A first example: libm

```gb
import ffi;
import io;

let lib = ffi.dlopen("libm.so.6");
let sin = lib.symbol("sin", [ffi.DOUBLE], ffi.DOUBLE);
let cos = lib.symbol("cos", [ffi.DOUBLE], ffi.DOUBLE);

io.println(sin(1.5707963267948966));   # 1.0
io.println(cos(0.0));                  # 1.0
lib.close();
```

`lib.symbol(name, argTypes, retType)` returns a Geblang callable
bound to the native function. Calling it dispatches straight
into C through a per-signature trampoline; the first lookup pays
the registration cost and subsequent calls reuse it.

## Type table

| Constant       | C type             | Geblang in    | Geblang out |
|----------------|--------------------|---------------|-------------|
| `ffi.VOID`     | `void`             | (return only) | `null`      |
| `ffi.INT8`     | `int8_t`           | `int`         | `int`       |
| `ffi.INT16`    | `int16_t`          | `int`         | `int`       |
| `ffi.INT32`    | `int32_t`          | `int`         | `int`       |
| `ffi.INT64`    | `int64_t`          | `int`         | `int`       |
| `ffi.UINT8`    | `uint8_t`          | `int`         | `int`       |
| `ffi.UINT16`   | `uint16_t`         | `int`         | `int`       |
| `ffi.UINT32`   | `uint32_t`         | `int`         | `int`       |
| `ffi.UINT64`   | `uint64_t`         | `int`         | `int`       |
| `ffi.FLOAT`    | `float`            | `decimal`     | `decimal`   |
| `ffi.DOUBLE`   | `double`           | `decimal`     | `decimal`   |
| `ffi.PTR`      | `void*`            | `int`         | `int`       |
| `ffi.CSTRING`  | `const char*`      | `string`      | `string`    |
| `ffi.BYTES`    | `uint8_t*` + len   | `bytes`       | n/a         |

`ffi.PTR` is the universal opaque-handle type. The Geblang side
reads it as `int` (the raw pointer value); the runtime treats
the value as opaque. A function returning a pointer to memory
the library owns is the caller's responsibility to either free
through the library's own release function or hand straight back
into another call.

`ffi.BYTES` is argument-only: the underlying byte slice is
passed by pointer + length, zero-copy. The C function must not
retain the pointer past the call.

`ffi.CSTRING` allocates a scratch buffer on each call (in) or
copies the returned `char*` into a fresh Geblang string (out).
No long-lived heap allocation either way.

## Memory helpers

Working with C-side memory requires explicit lifetime management;
Geblang's GC doesn't track FFI-owned allocations.

```gb
let ptr = ffi.alloc(64);                 # libc malloc(64)
ffi.writeBytes(ptr, bytes.fromHex("deadbeef"));
let data = ffi.readBytes(ptr, 4);
ffi.free(ptr);

let cs = ffi.cString("hello geblang");   # null-terminated C string
let s = ffi.readCString(cs);
ffi.free(cs);

let code = ffi.errno();                  # thread-local C errno
```

`ffi.alloc(n)` returns a pointer; `ffi.free(ptr)` releases it
(NULL-safe). `ffi.readBytes(ptr, n)` copies; `ffi.writeBytes(ptr, data)`
copies in. `ffi.readCString(ptr)` walks to the null terminator
(capped at 1 MiB to bound a missing-terminator walk).
`ffi.cString(s)` allocates and copies; you Free it. `ffi.errno()`
reads the OS thread's errno after the most recent failing call.

## The recommended pattern: handle wrapper with `with`

C libraries usually expose long-lived handles (file handles,
database connections, tensors, contexts). Wrap them in a Geblang
class with `__enter` / `__exit` and use `with` blocks so the
release call fires automatically at scope exit:

```gb
import ffi;
import io;

let sqlite = ffi.dlopen("libsqlite3.so.0");
let open = sqlite.symbol("sqlite3_open", [ffi.CSTRING, ffi.PTR], ffi.INT32);
let close = sqlite.symbol("sqlite3_close", [ffi.PTR], ffi.INT32);
let exec = sqlite.symbol("sqlite3_exec",
    [ffi.PTR, ffi.CSTRING, ffi.PTR, ffi.PTR, ffi.PTR], ffi.INT32);

class Database {
    int handle;
    func Database(int handle) { this.handle = handle; }
    func __enter(): Database { return this; }
    func __exit(): void { close(this.handle); }
}

func openInMemory(): Database {
    let slot = ffi.alloc(8);
    let rc = open(":memory:", slot) as int;
    if (rc != 0) {
        ffi.free(slot);
        throw RuntimeError("sqlite3_open failed: " + rc.toString());
    }
    let raw = ffi.readBytes(slot, 8);
    ffi.free(slot);
    int handle = 0;
    for (i in 0..7) {
        handle = handle + ((raw[i] as int) << (i * 8));
    }
    return Database(handle);
}

with (db = openInMemory()) {
    exec(db.handle, "CREATE TABLE t (n INT); INSERT INTO t VALUES (42);", 0, 0, 0);
    /* db.handle stays valid here */
}
/* __exit has fired; the connection is closed. */
```

`with` calls `__enter` on entry and `__exit` on exit, even
when control leaves the block via an exception. This is the
right primitive for handles whose lifetime maps to a code
region. For program-end cleanup, see `func ~ClassName()`
destructors in the language guide.

## C structs

For C APIs that take a pointer to a struct (most syscalls, many
graphics / audio libraries), describe the layout with
`ffi.StructOf` and use the returned `Struct` to read and write
fields without computing offsets manually.

```gb
import ffi;
import io;

let libc = ffi.dlopen("libc.so.6");
let gettimeofday = libc.symbol("gettimeofday", [ffi.PTR, ffi.PTR], ffi.INT32);

let Timeval = ffi.StructOf([
    ["tv_sec",  ffi.INT64],
    ["tv_usec", ffi.INT64],
]);

let tv = Timeval.alloc();
gettimeofday(tv, 0);
io.println(Timeval.get(tv, "tv_sec"));
io.println(Timeval.get(tv, "tv_usec"));
ffi.free(tv);
libc.close();
```

Fields are declared in C declaration order as `[name, type]`
pairs. The layout follows standard C alignment rules: each field
is aligned to its size, the total size is padded to the largest
field's alignment. `Struct.size` reports the byte size;
`Struct.alloc()` returns a zeroed buffer of that size, and you
free it with `ffi.free`.

Valid field types are PTR, INT*, UINT*, FLOAT, and DOUBLE.
CSTRING and BYTES are not valid as direct fields - if your
struct holds a string or buffer, model it as PTR and manage the
pointed-at memory explicitly. Struct-by-value (passing or
returning a struct on the stack instead of by pointer) is not
supported.

## Typed arrays

For C APIs that take a pointer + length to a homogeneous array,
use `ffi.writeArray` / `ffi.readArray` to marshal a Geblang list.

```gb
let n = 5;
let buf = ffi.alloc(n * ffi.sizeOf(ffi.INT32));
ffi.writeArray(buf, ffi.INT32, [5, 2, 8, 1, 4]);

qsort(buf, n, ffi.sizeOf(ffi.INT32), cmp);

let sorted = ffi.readArray(buf, ffi.INT32, n);   /* [1, 2, 4, 5, 8] */
ffi.free(buf);
```

`ffi.sizeOf(type)` gives the per-element byte size. Element types
are the same primitive set as struct fields: INT*, UINT*, FLOAT,
DOUBLE, PTR. CSTRING and BYTES are not valid element types.

## Zero-copy bytes view

`ffi.readBytes(ptr, n)` always copies. For large buffers (image
frames, audio samples, tensors) the copy cost dominates; reach
for `ffi.bytesView(ptr, n)` instead. The returned `bytes` value
ALIASES the C memory - no copy is performed.

```gb
let view = ffi.bytesView(framePtr, width * height * 4);
/* read or write `view` like any bytes value; mutations land in the
   C-side buffer. */
```

Lifetime contract: once the C side frees the memory, the bytes
value is invalid. Every read or write through it after that point
is undefined. Reach for `bytesView` only when you control the
lifetime explicitly; default to `readBytes` for correctness.

## Callbacks (Geblang as a C function pointer)

Native libraries that drive their own loop (libcurl multi-handle,
qsort, audio callbacks, libavformat) take function pointers. Build
one with `ffi.callback`:

```gb
let cmp = ffi.callback(func(int pa, int pb): int {
    let a = ffi.readBytes(pa as int, 4);
    let b = ffi.readBytes(pb as int, 4);
    let ai = (a[0] as int) | ((a[1] as int) << 8) | ((a[2] as int) << 16) | ((a[3] as int) << 24);
    let bi = (b[0] as int) | ((b[1] as int) << 8) | ((b[2] as int) << 16) | ((b[3] as int) << 24);
    return ai - bi;
}, [ffi.PTR, ffi.PTR], ffi.INT32);

let qsort = libc.symbol("qsort",
    [ffi.PTR, ffi.UINT64, ffi.UINT64, ffi.PTR], ffi.VOID);
qsort(buf, 5, 4, cmp);
```

The returned int is the C function pointer; pass it to native
calls whose signature declares the slot as `ffi.PTR`.

### Constraints

- **Signature types**: `INT*`, `UINT*`, `PTR` only. Floats are not
  supported in callback signatures. CSTRING / BYTES are not
  supported either - declare PTR and decode inside the Geblang
  function with `ffi.readCString`, `ffi.readBytes`, or
  `Struct.get`.
- **Return types**: `VOID`, `INT*`, `UINT*`, `PTR`.
- **Lifetime**: callbacks live for the rest of the process.
  Backing is `purego.NewCallback`, which caps at 2000 callbacks
  per process. Don't create them inside hot loops; build once,
  reuse.
- **Threading**: the Geblang runtime must be reachable when C
  calls back. Libraries that invoke callbacks from arbitrary OS
  threads may misbehave; document the threading model per
  binding. The canonical use cases (qsort, single-threaded
  driver loops) are safe.
- **Errors**: if the Geblang function throws, the bridge returns
  the type's zero value to C. The library will see a "successful"
  callback that produced 0. Validate inputs and use try/catch
  inside the callback when the contract matters.

## Memory ownership rules

1. Memory you allocate via `ffi.alloc` you must `ffi.free`.
2. Memory the library allocates and returns to you is owned by
   the library; pass it to the library's release function.
3. Geblang `string` / `bytes` values passed to CSTRING / BYTES
   arguments are NOT pinned past the call. The C side must not
   retain the pointer.
4. Pointers returned to Geblang are opaque ints. Doing arithmetic
   on them is supported (pointers are 64-bit ints on supported
   hosts), but pointer arithmetic has no type information; treat
   it the way you would in C.

## When NOT to use FFI

FFI is unsafe in the C sense: a null pointer dereference inside
the library crashes the whole Geblang process. The capability
gate prevents accidental loads, but it does not sandbox the
library once loaded. Use FFI for libraries you trust to behave
correctly with the arguments you pass them.

For untrusted code, use the `ext` protocol instead: it runs the
extension in a subprocess that the kernel isolates from your
script. Trade-off: per-call IPC overhead.

For Go libraries, prefer the in-tree native module path
(`internal/native/`) - FFI through dlopen is the path for
non-Go targets.

## Supported platforms

Dispatch backs onto `purego`, which covers Linux/macOS/Windows
on x86_64 and arm64 (SystemV + Windows x64 calling conventions).
Other architectures (RISC-V, PPC64) are not guaranteed.

Variadic functions (`printf`-family) are not supported. Most
variadic C APIs have a `va_list` sibling or a fixed-shape struct;
prefer those.

64-bit `long double` is not supported (Geblang has no
80-bit-precision native type).

## Error behaviour

| Failure mode                                              | Surface                                |
|-----------------------------------------------------------|----------------------------------------|
| `dlopen` failure (missing file, ELF mismatch)             | `RuntimeError`                         |
| Symbol not found                                          | `RuntimeError`                         |
| Permission denied by policy                               | `PermissionError` (catchable)          |
| Wrong arg count or wrong arg type at call site            | `RuntimeError` (catchable)             |
| Native code segfaults                                     | process dies; not catchable from Geblang |

The last is unavoidable - once control hands off to native
code, the language runtime no longer owns the process.

## Generating bindings: `geblang bind`

For a library with more than a handful of functions, writing
`lib.symbol(...)` declarations by hand gets repetitive. The
`geblang bind` CLI consumes a YAML manifest describing the
library and emits a typed Geblang module that wraps each call.

```yaml
# bindings/sqlite.yaml
module: sqlite
library: libsqlite3.so.0
doc: |
  Hand-curated subset of the SQLite C API.

constants:
  - { name: SQLITE_OK,   value: 0 }
  - { name: SQLITE_ROW,  value: 100 }
  - { name: SQLITE_DONE, value: 101 }

functions:
  - name: sqlite3_open
    args: [CSTRING, PTR]
    returns: INT32
  - name: sqlite3_close
    args: [PTR]
    returns: INT32
  - name: sqlite3_exec
    args: [PTR, CSTRING, PTR, PTR, PTR]
    returns: INT32
```

Run the generator:

```sh
geblang bind bindings/sqlite.yaml --out src/sqlite.gb
```

The output is a normal Geblang module: `import sqlite;` from
your code and call `sqlite.sqlite3_open(...)` like any other
function. The generator handles signature plumbing
(`lib.symbol` registration, primitive type marshalling, casts);
your code stays domain-focused.

Supported manifest sections:

- `module` / `library`: required header information.
- `doc`: optional module-level doc comment.
- `constants`: a list of `{name, value, doc}`. Emitted as
  `export const int NAME = VALUE`.
- `structs`: a dict of struct name to `{fields: [{name, type}]}`.
  Emitted as `export let Name = ffi.StructOf([...])`.
- `functions`: a list of `{name, args, returns, doc}`. Each
  becomes a typed `export func` wrapping a cached
  `lib.symbol(...)` callable.

The same primitive type set as the rest of the FFI applies
(`INT8`-`INT64`, `UINT8`-`UINT64`, `FLOAT`, `DOUBLE`, `PTR`,
`CSTRING`, `BYTES`). Struct fields are restricted to the
primitive numeric and pointer types, matching `ffi.StructOf`.

For libraries whose surface drifts, treat the generated file as
build output: regenerate on demand, don't hand-edit. For small
ad-hoc bindings the raw `lib.symbol(...)` form from earlier in
this chapter stays available - the generator is sugar, not the
only path.

## Not currently supported

- Struct passing by value (use a pointer to an `ffi.StructOf`
  buffer instead).
- Variadic function calls (`printf`-family).
- Long double / 80-bit float types.
- Automatic C-header parsing for `geblang bind` (the manifest is
  hand-written).
