# Geblang for systems programmers

## Who this is for

This guide is for programmers who work close to the hardware: C and C++ developers,
kernel and embedded engineers, shell scripters, and anyone who thinks in terms of
processes, file descriptors, byte buffers, and system calls. Geblang gives you
typed scripting with first-class FFI for C libraries, process spawning and signal
handling, BSD-socket-style networking, SSH, and binary data manipulation. This guide
maps what you already know to the Geblang equivalents and shows you the points where
the language differs from C or shell convention.

## Quick orientation

The following program queries the host OS, spawns a subprocess, and processes its
output - a common systems scripting pattern:

```gb
import io;
import sys;
import proc;

io.println("platform: ${sys.platform()}");
io.println("arch:     ${sys.arch()}");
io.println("kernel:   ${sys.osVersion()}");
io.println("pid:      ${sys.pid()}");

/* Spawn a child and stream its output line by line */
let p = proc.spawn("find", ["/tmp", "-maxdepth", "1", "-name", "*.gb"]);
for (line in p.stdout) {
    io.println(line);
}
p.wait();
```

Run it without any special flags:

```sh
geblang run script.gb
```

No capability gate is required for `sys` or `proc.spawn`.

## Coming from C and the shell: concept mapping

| C / shell concept | Geblang equivalent |
|-------------------|--------------------|
| `dlopen` / `dlsym` | `ffi` module - `ffi.dlopen`, `lib.symbol` |
| C struct layout (`struct timeval`) | `ffi.StructOf([["field", ffi.TYPE], ...])` |
| `malloc` / `free` | `ffi.alloc(n)` / `ffi.free(ptr)` |
| Callbacks (`qsort` comparator) | `ffi.callback(func(...): T {...}, argTypes, retType)` |
| `fork` / `exec` + shell pipeline | `proc.spawn(cmd, args)` - streaming stdin/stdout |
| Synchronous subprocess (`system(3)`) | `sys.run(cmd, args)` or `process.run(cmd, args)` |
| `kill(2)` / `raise(3)` | `process.kill(pid)` / `process.signal(pid, name)` |
| `signal(2)` / `sigaction(2)` | `sys.onSignal(name, handler)` |
| `getenv` / `setenv` | `sys.getenv(name)` / `sys.setenv(name, value)` |
| `uname(2)` | `sys.platform()`, `sys.arch()`, `sys.osVersion()` |
| `getpid` / `getuid` / `getgid` | `process.pid()` / `process.uid()` / `process.gid()` |
| BSD `socket` / `connect` / `send` / `recv` | `sockets.dial(host, port)` |
| `bind` / `accept` loop | `sockets.serve(host, port, handler)` |
| SSH client (`libssh2`, `ssh(1)`) | `ssh` module - `ssh.connect`, `c.exec`, `c.spawn` |
| `uint8_t` buffer (`memcpy`, bit ops) | `bytes` type - `bytes.fromHex`, `b.toList()`, etc. |
| `struct` packing (`__attribute__((packed))`) | `binary.pack` / `binary.unpack` |
| `libz` gzip / `zstd` | `compress.gzip` / `clib.zstd` (FFI-backed) |
| `libmagic` / `file(1)` MIME detection | `clib.magic` (FFI-backed) |
| `sd_notify` / journald | `clib.systemd` (FFI-backed) |

## Key features for you

### Environment and host info (`sys`, `process`)

`sys` exposes the running process's environment, OS identity, and credentials.
No capability flag is required for read-only access:

```gb
import io;
import sys;
import process;

io.println("platform: ${sys.platform()}");
io.println("arch:     ${sys.arch()}");
io.println("hostname: ${sys.hostname()}");
io.println("pid:      ${sys.pid()}");
io.println("user:     ${sys.username()}");
io.println("cwd:      ${sys.cwd()}");

let home = sys.getenv("HOME");
if (home != null) {
    io.println("HOME=${home}");
}

io.println("uid=${process.uid()} gid=${process.gid()}");
```

Run with:

```sh
geblang run script.gb
```

`sys.run` is the synchronous subprocess call (equivalent to `system(3)` but with
captured output):

```gb
import io;
import sys;

let result = sys.run("uname", ["-sr"]);
if ((result["code"] as int) == 0) {
    io.println(result["stdout"] as string);
} else {
    io.stderrWrite(result["stderr"] as string);
}
```

For process identity queries (`ppid`, `uid`, `gid`, `euid`, `egid`), inspecting
other processes by pid (`process.info(pid)`, `process.list()`), and reading the
process environment (`sys.environ()`), no capability flag is needed. Sending signals
to arbitrary pids (`process.kill(pid)`, `process.signal(pid, name)`) and changing
credentials (`process.setuid`) are gated behind `--allow-process-control`.

See [stdlib/02-sys.md](../stdlib/02-sys.md) for the full `sys` reference.

### Subprocess streaming (`proc`)

For long-running processes or pipeline-style I/O, `proc.spawn` is the right
primitive. It returns immediately; the child runs concurrently. `stdin`, `stdout`,
and `stderr` are `IOStream` values:

```gb
import io;
import proc;

/* Pipe data through a child process */
let p = proc.spawn("grep", ["world"]);
p.stdin.writeln("hello there");
p.stdin.writeln("hello world");
p.stdin.writeln("goodbye");
p.stdin.close();
for (line in p.stdout) {
    io.println(line);
}
p.wait();
```

Run with:

```sh
geblang run script.gb
```

`p.kill()` sends SIGKILL; `p.signal(name)` sends a named signal. These act on a
child process the script launched and need no capability flag. Exit codes follow the
shell convention: signal termination is 128 + signal number.

See [stdlib/02a-proc.md](../stdlib/02a-proc.md) for the `Process` API and PTY mode.

### Byte buffers and binary protocols (`bytes`, `binary`)

The `bytes` type is an immutable byte slice. `binary.pack` / `binary.unpack`
give Python `struct`-style framing for protocol headers:

```gb
import io;
import bytes;
import binary;

/* Build and parse a custom binary packet header */
let header = binary.pack(">IHB", 0xDEADBEEF, 512, 3);
io.println("header hex: ${bytes.toHex(header)}");

let parts = binary.unpack(">IHB", header);
io.println("magic:   ${parts[0]}");
io.println("length:  ${parts[1]}");
io.println("version: ${parts[2]}");

/* Raw byte manipulation */
let payload = bytes.fromString("hello world");
io.println("bytes: ${bytes.toHex(payload)}");
io.println("first byte value: ${payload[0]}");
```

Run with:

```sh
geblang run script.gb
```

Format prefix: `>` big-endian, `<` little-endian, `!` network (= big), `=` host
native. Field codes: `b`/`B` (int8/uint8), `h`/`H` (int16/uint16),
`i`/`I` (int32/uint32), `q`/`Q` (int64/uint64), `f` (float32), `d` (float64),
`Ns` (N-byte string), `Nx` (N pad bytes).

Other byte operations: `bytes.fromHex`, `bytes.toBase64`, `bytes.fromList`,
`bytes.concat`. `ffi.readBytes(ptr, n)` copies C-side memory into a `bytes` value;
`ffi.writeBytes(ptr, data)` goes the other way.

See [stdlib/10-bytes.md](../stdlib/10-bytes.md) for the full `bytes`, `binary`, and
`compress` reference.

### C library interop (`ffi`)

`ffi` gives in-process access to any shared library with a stable C ABI. It is the
Geblang equivalent of `dlopen` + `dlsym`. FFI is off by default; you enable it
explicitly per library.

**Capability gate.** For a standalone script, pass `--allow-ffi` with a glob or
exact path on the command line. For a project, declare it in `geblang.yaml` under
`permissions.ffi.libraries`.

```sh
geblang run --allow-ffi 'libm.so.*' script.gb
geblang run --allow-ffi 'libc.so.*' --allow-ffi 'libssl.so.*' script.gb
```

A blocked load throws a catchable `PermissionError`. A segfault inside native code
kills the whole process - there is no sandbox once the library is loaded. Use FFI
for libraries you trust.

**Calling a function:**

```gb
import io;
import ffi;

let lib = ffi.dlopen("libm.so.6");
let sin = lib.symbol("sin", [ffi.DOUBLE], ffi.DOUBLE);
let cos = lib.symbol("cos", [ffi.DOUBLE], ffi.DOUBLE);
let pow = lib.symbol("pow", [ffi.DOUBLE, ffi.DOUBLE], ffi.DOUBLE);

io.println("sin(pi/2) = ${sin(1.5707963267948966)}");
io.println("cos(0)    = ${cos(0.0)}");
io.println("2^10      = ${pow(2.0, 10.0)}");
lib.close();
```

Run with:

```sh
geblang run --allow-ffi 'libm.so.*' script.gb
```

`lib.symbol(name, argTypes, retType)` returns a typed callable. The first call pays
the trampoline registration cost; subsequent calls reuse it. `ffi.DOUBLE` maps to
`decimal` on the Geblang side (not `float`).

**C structs** - describe layout with `ffi.StructOf` to avoid manual offset
arithmetic:

```gb
import io;
import ffi;

let libc = ffi.dlopen("libc.so.6");
let gettimeofday = libc.symbol("gettimeofday", [ffi.PTR, ffi.PTR], ffi.INT32);

let Timeval = ffi.StructOf([
    ["tv_sec",  ffi.INT64],
    ["tv_usec", ffi.INT64],
]);

let tv = Timeval.alloc();
gettimeofday(tv, 0);
io.println("seconds since epoch: ${Timeval.get(tv, "tv_sec")}");
io.println("microseconds:        ${Timeval.get(tv, "tv_usec")}");
ffi.free(tv);
libc.close();
```

Run with:

```sh
geblang run --allow-ffi 'libc.so.*' script.gb
```

**Memory ownership** follows C rules: memory you allocate with `ffi.alloc` you must
`ffi.free`; memory the library allocates is owned by the library. Wrap long-lived
handles in a class with `__enter` / `__exit` and use `with` blocks for automatic
cleanup on scope exit, even when exceptions occur.

For libraries with more than a handful of functions, `geblang bind bindings/lib.yaml
--out src/lib.gb` generates a typed Geblang module from a YAML manifest so you write
`lib.funcName(args)` instead of managing `lib.symbol(...)` declarations manually.

See [stdlib/22-ffi.md](../stdlib/22-ffi.md) for the type table, memory helpers,
callbacks, typed arrays, the `bytesView` zero-copy path, and `geblang bind`.

### TCP sockets (`sockets`)

`sockets.dial` is the connect-side; `sockets.serve` is the accept-side. Sockets
are `IOStream`-shaped, so `readLine`, `writeln`, and `for (line in conn)` work
directly:

```gb
import io;
import sockets;
import sys;

let server = sockets.serve("127.0.0.1", 19877, func(sockets.Socket conn): void {
    let line = conn.readLine();
    conn.writeln("echo: " + line);
    conn.close();
});

sys.sleep(50);

let client = sockets.dial("127.0.0.1", 19877);
client.writeln("hello sockets");
let reply = client.readLine();
io.println(reply);
client.close();

server.close();
```

Run with:

```sh
geblang run script.gb
```

For TLS, pass `{"tls": true}` as the third argument to `sockets.dial`. The system
trust store is used; SNI is set from the host name.

For raw bytes, UDP, custom framing, or DNS helpers, use the lower-level `net`
module. See [stdlib/14a-sockets.md](../stdlib/14a-sockets.md).

### SSH (`ssh`)

The `ssh` module is an SSH client with exec, streaming spawn, SFTP, and port
forwarding. No external `ssh(1)` binary is required:

```
/* fragment - requires a real SSH server and credentials */
import io;
import ssh;

let c = ssh.connect("alice@server.example.com", {
    "privateKeyFile": "~/.ssh/id_rsa",
    "knownHostsFile": "~/.ssh/known_hosts",
});
let r = c.exec("uname -a");
io.println(r.stdout);
c.close();
```

Run with `geblang run script.gb` (the `ssh` module needs no capability flag).

`c.spawn(cmd)` returns an `SSHSession` with `stdin`/`stdout`/`stderr` streams - the
same shape as `proc.Process` - for long-running remote commands. SFTP operations:
`c.upload(local, remote)`, `c.download(remote, local)`, `c.sftpOpen(path, mode)`.
Port forwarding: `c.forwardLocal(localPort, "host:remotePort")`.

See [stdlib/14b-ssh.md](../stdlib/14b-ssh.md).

### FFI-backed system libraries (`clib.*`)

The `clib.*` family wraps common C libraries through the FFI layer. Each requires
the `--allow-ffi` flag (or the manifest equivalent):

**`clib.zstd`** - Zstandard compression (faster than gzip, comparable ratios):

```
/* fragment - requires libzstd installed */
import clib.zstd as zstd;
import bytes;

let original = bytes.fromString("some data to compress...");
let compressed = zstd.compress(original);
let recovered  = zstd.decompress(compressed);
```

Run with `geblang run --allow-ffi 'libzstd*' script.gb`.

**`clib.magic`** - MIME and file-type detection (the same database as `file(1)`):

```
/* fragment - requires libmagic installed */
import clib.magic as magic;
import io;

io.println(magic.mime("/path/to/file"));
```

Run with `geblang run --allow-ffi 'libmagic*' script.gb`.

**`clib.systemd`** - `sd_notify` readiness signalling and structured journald logging
for processes managed by systemd.

**`clib.curses`** - Full-screen TUI via libncurses.

See [stdlib/25-clib-zstd.md](../stdlib/25-clib-zstd.md),
[stdlib/28-clib-magic.md](../stdlib/28-clib-magic.md),
[stdlib/26-clib-systemd.md](../stdlib/26-clib-systemd.md),
[stdlib/29-clib-curses.md](../stdlib/29-clib-curses.md).

### Compression and archives (native, no FFI)

The built-in `compress` and `archive` modules handle gzip and zip/tar without the
FFI capability gate:

```gb
import io;
import bytes;
import compress;
import archive;

let data = bytes.fromString("repeated repeated repeated data for compression");
let zipped = compress.gzip(data);
let back = compress.gunzip(zipped);
io.println("original:   ${data.length()} bytes");
io.println("compressed: ${zipped.length()} bytes");
io.println("roundtrip:  ${bytes.toString(back)}");

let tgz = archive.tarGzWrite([
    {"name": "hello.txt", "data": "hello"},
    {"name": "world.txt", "data": "world"},
]);
let entries = archive.tarGzRead(tgz);
for (e in entries) {
    io.println("  ${e["name"]}: ${bytes.toString(e["data"] as bytes)}");
}
```

Run with:

```sh
geblang run script.gb
```

For Zstandard, reach for `clib.zstd` instead (see above).

## Gotchas

**FFI and process-control are default-deny.** `ffi.dlopen` throws `PermissionError`
unless you pass `--allow-ffi 'glob'` on the command line (or declare the library
under `permissions.ffi.libraries` in `geblang.yaml`). Similarly,
`process.kill(pid)` and `process.signal(pid, name)` on arbitrary pids throw
`PermissionError` unless you pass `--allow-process-control`. These are catchable, so
you can probe capability and degrade gracefully. The `Process` handle methods
(`p.kill()`, `p.signal(name)`) for a child the script itself spawned are always
available without the flag.

**`//` is integer floor division, not a comment.** In C you comment with `//`; in
Geblang `//` is the integer division operator (`7 // 2` is `3`). Use `#` for line
comments or `/* ... */` for block comments.

**`decimal` is the default float type.** A bare literal `1.5` is a `decimal`
(arbitrary-precision rational), not an IEEE 754 float. `ffi.FLOAT` and `ffi.DOUBLE`
return `decimal` on the Geblang side. Where you need IEEE 754 semantics, use the
`f` suffix (`1.5f`) or cast with `as float`. Mixing `decimal` and `float` in
arithmetic is a type error.

**`/` returns `decimal`, not int.** `7 / 2` is `3.5`. Use `7 // 2` to get `3`.
Assigning a division result to `int` is a compile-time error.

**No pointer arithmetic with types.** FFI pointers are `int` values on the Geblang
side. You can add offsets as integer arithmetic, but there is no type information:
treat pointer math the way you would treat raw `uintptr_t` in C.

**No variadic FFI calls.** `printf`-family functions are not supported through the
FFI layer. Most variadic C APIs have a `va_list` sibling or a fixed-shape struct
variant; use those.

**`parent()`, not `super`.** In subclass constructors and method overrides, call the
parent with `parent(args)`, not `super`.

**Type-first parameter syntax.** Function parameters are `int n`, not `n: int`.
Return type follows the closing paren: `func f(int n): void`.

**`list.push(x)` mutates in place.** It returns the receiver, not a new list. Use
`.sorted()`, `.reversed()`, or `.copy()` for copy variants.

**Conditions must be explicit booleans.** Zero, null, and empty collections are not
falsy. Write `if (n != 0)`, `if (ptr != null)`, `if (items.length() > 0)`.

## Where to go next

- [FFI reference](../stdlib/22-ffi.md) - type table, structs, callbacks, memory
  helpers, zero-copy `bytesView`, and `geblang bind`
- [Subprocess streaming](../stdlib/02a-proc.md) - `proc.spawn`, PTY mode, streaming
  stdin/stdout
- [System and environment](../stdlib/02-sys.md) - `sys.run`, environment variables,
  `runWithOptions`, signal trapping, the `process` module
- [Sockets](../stdlib/14a-sockets.md) - TCP client/server, TLS, the `net` module
- [SSH](../stdlib/14b-ssh.md) - exec, streaming spawn, SFTP, port forwarding
- [Bytes and binary](../stdlib/10-bytes.md) - `bytes`, `binary`, `compress`,
  `archive`, `encoding`
- [clib.zstd](../stdlib/25-clib-zstd.md) - Zstandard compression
- [clib.magic](../stdlib/28-clib-magic.md) - MIME and file-type detection
- [clib.systemd](../stdlib/26-clib-systemd.md) - sd_notify and journald
- [clib.curses](../stdlib/29-clib-curses.md) - full-screen TUI via libncurses
- [examples/ffi_libm.gb](https://github.com/dwgebler/geblang/blob/main/examples/ffi_libm.gb) - FFI with libm
- [examples/ffi_sqlite.gb](https://github.com/dwgebler/geblang/blob/main/examples/ffi_sqlite.gb) - FFI SQLite binding
- [examples/proc_streaming.gb](https://github.com/dwgebler/geblang/blob/main/examples/proc_streaming.gb) - subprocess streaming
- [examples/proc_pty.gb](https://github.com/dwgebler/geblang/blob/main/examples/proc_pty.gb) - PTY mode
- [examples/sockets_echo.gb](https://github.com/dwgebler/geblang/blob/main/examples/sockets_echo.gb) - echo server
- [examples/binary_pack.gb](https://github.com/dwgebler/geblang/blob/main/examples/binary_pack.gb) - binary packing
- [examples/bytes.gb](https://github.com/dwgebler/geblang/blob/main/examples/bytes.gb) - byte buffer operations
