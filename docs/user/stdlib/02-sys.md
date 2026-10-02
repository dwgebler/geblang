# System, Environment, And Processes

Import `sys` for process information, environment variables, script arguments,
sleeping, exit status, and subprocess execution.

`sys` is about the running process and the host operating system. It does not
own filesystem mutation; use `io` for files/directories and `path` for path
string manipulation.

```gb
import sys;
import io;

io.println(sys.platform());
io.println(sys.cwd());
```

## Module Boundaries

| Need | Module |
|------|--------|
| Read/write files, make directories, chmod, temp files | `io` |
| Join, clean, inspect, glob path strings | `path` / `pathlib` |
| Current user, environment variables, cwd, process info | `sys` |
| Run or manage subprocesses | `sys` or `process` |

## Process Information

| Function | Returns | Description |
|----------|---------|-------------|
| `platform()` | `string` | Host OS name, such as `linux`, `darwin`, or `windows` |
| `osVersion()` | `string` | OS kernel/release string (Linux: uname release) |
| `arch()` | `string` | CPU architecture, such as `amd64` or `arm64` |
| `hostname()` | `string` | Hostname |
| `pid()` | `int` | Current process ID |
| `goroutineId()` | `int` | Id of the current goroutine (see below) |
| `username()` | `string` | Current OS username |
| `homedir()` | `string` | Current user's home directory |
| `tmpdir()` | `string` | Host temporary directory |
| `cwd()` | `string` | Current working directory |

```gb
io.println(sys.hostname());
io.println(sys.pid() as string);
io.println(sys.homedir());
io.println(sys.tmpdir());
```

`platform()` names the OS; `osVersion()` complements it with the kernel/release
string (on Linux, the `uname` release such as `6.6.0-generic`). `osVersion()` is
implemented on Linux; on platforms where it is not yet available it throws a
catchable error naming the platform rather than returning an empty string.

Use `sys.tmpdir()` with `path.join` when you need a location, and `io.tempFile`
or `io.tempDir` when you want Geblang to create a unique path for you.

`sys.goroutineId()` returns the id of the goroutine that calls it. It is stable
for the life of that goroutine and unique among goroutines running at the same
time (an id may be reused only after the goroutine that held it has exited). It
is an advanced primitive for building goroutine-local or request-scoped state:
key a `store.Store` by the id, and clear that key when the goroutine's work
finishes so a later goroutine reusing the id starts clean. Most code never needs
it; prefer passing state explicitly or sharing through a `store.Store`.

## Bundled Resources

| Function | Returns | Description |
|----------|---------|-------------|
| `bundleDir()` | `string` | Extract directory of a built binary's embedded resources, or `""` when not running from a bundle |
| `packageRoot()` | `string` | Directory of the current package manifest, or `""` when none is available |

`geblang build` can embed non-code files (templates, static assets, data) listed
under `resources:` in `geblang.yaml`. A running program locates them through
the `resources` module. It uses the package manifest directory in development
and the extracted bundle directory in a built binary.

```gb
import resources;
let html = resources.readText("templates/page.html");
```

`resources.path(name)` returns an absolute readable path. `exists(name)` returns
false when a declared file is absent; other operations raise an error.
`readBytes(name)` preserves binary data, and `open(name)` returns a read-only
`file.File` that the caller closes. Paths must be project-relative, use forward
slashes, and stay inside the resource root after symlinks resolve. The module
only exposes files declared in `resources:` or added with `--resource`. For a
script without a manifest, use `resources.withRoot(root)` and call the same
methods on its `ResourceRoot` value.

See [Bundling And Standalone Executables](../13-bundling.md) for the `resources:`
manifest field and how embedding works.

## Environment Variables

| Function | Returns | Description |
|----------|---------|-------------|
| `getenv(name)` | `string|null` | Environment variable value, or `null` when unset |
| `setenv(name, value)` | `void` | Set an environment variable for the current process and child processes |
| `environ()` | `dict<string, string>` | Snapshot of the current environment |

```gb
let env = sys.getenv("APP_ENV");
if (env == null) {
    sys.setenv("APP_ENV", "development");
}

let all = sys.environ();
io.println(all["PATH"]);
```

`setenv` affects the current Geblang process and subprocesses launched after the
call. It does not change the parent shell's environment.

Use the `dotenv` module when loading `.env` files:

```gb
import dotenv;

if (io.exists(".env")) {
    dotenv.loadAndApply(".env");
}
```

## Script Arguments

| Function | Returns | Description |
|----------|---------|-------------|
| `args()` | `list<string>` | Arguments passed to the script |

```gb
let argv = sys.args();
if (argv.length() > 0) {
    io.println("first arg: " + argv[0]);
}
```

For user-facing command-line applications, prefer the `cli` module for option
parsing, help text, prompts, and terminal formatting.

## Sleep And Exit

| Function | Returns | Description |
|----------|---------|-------------|
| `sleep(ms)` | `void` | Block for the given milliseconds |
| `exit(code)` | never | Exit the script with the given process status |

```gb
sys.sleep(500);
sys.exit(0);
```

`sys.sleep` blocks the current execution thread. In async code, prefer
`async.sleep(ms)` so the scheduler can continue other work while waiting.

## Running Subprocesses

Use `sys.run` when you want to run a command, wait for it to finish, and inspect
captured stdout/stderr.

| Function | Returns | Description |
|----------|---------|-------------|
| `run(command, args)` | `dict` | Run a command with a list of arguments |
| `shell(command)` | `dict` | Run a shell command through `/bin/sh -c` |
| `runWithOptions(options)` | `dict` | Run with cwd/env/timeout options |

Result dictionary:

| Field | Type | Description |
|-------|------|-------------|
| `code` | `int` | Exit code, where `0` normally means success |
| `stdout` | `string` | Captured standard output |
| `stderr` | `string` | Captured standard error |
| `timedOut` | `bool` | Whether the process was killed by timeout |

```gb
let result = sys.run("git", ["log", "--oneline", "-5"]);
if ((result["code"] as int) == 0) {
    io.println(result["stdout"]);
} else {
    io.stderrWrite(result["stderr"]);
}
```

`sys.shell(command)` is convenient for shell features such as pipes and
redirection, but it executes through the host shell. Do not pass untrusted input
into a shell command string.

```gb
let result = sys.shell("ls -la | head -5");
```

## `runWithOptions`

`sys.runWithOptions(options)` accepts:

| Option | Type | Description |
|--------|------|-------------|
| `command` | `string` | Required executable |
| `args` | `list<string>` | Arguments, default `[]` |
| `cwd` | `string` | Working directory |
| `env` | `dict<string, string>` | Extra environment values merged into the current environment |
| `timeoutMs` | `int` | Kill the process after this many milliseconds |

```gb
let result = sys.runWithOptions({
    "command": "npm",
    "args": ["run", "build"],
    "cwd": "/app",
    "env": {"NODE_ENV": "production"},
    "timeoutMs": 30000
});

if (result["timedOut"] as bool) {
    io.stderrWrite("build timed out\n");
}
```

The `env` dictionary is merged with the current process environment. To modify
or pass through a large environment, start with `sys.environ()`:

```gb
let env = sys.environ();
env["PATH"] = "/usr/local/bin:" + env["PATH"];

let result = sys.runWithOptions({
    "command": "node",
    "args": ["server.js"],
    "env": env
});
```

## Streaming Subprocess Handles

Use `sys.start` when you need to interact with a process while it runs.

| Function | Returns | Description |
|----------|---------|-------------|
| `start(command, args)` | process handle | Start a process with stdin/stdout/stderr pipes |
| `startWithOptions(options)` | process handle | Start with cwd/env options |
| `processWrite(proc, text)` | `int` | Write text to stdin |
| `processCloseStdin(proc)` | `void` | Close stdin |
| `processReadStdout(proc)` | `string` | Read all remaining stdout |
| `processReadStderr(proc)` | `string` | Read all remaining stderr |
| `processReadStdoutN(proc, n)` | `string` | Read up to `n` bytes from stdout |
| `processReadStderrN(proc, n)` | `string` | Read up to `n` bytes from stderr |
| `processWait(proc)` | `int` | Wait for exit and return the code |
| `processKill(proc)` | `void` | Send SIGKILL |
| `processSignal(proc, signal)` | `void` | Send `KILL`, `TERM`, `INT`, or `HUP` |
| `processPid(proc)` | `int` | Return the OS process ID |

```gb
let proc = sys.start("cat", []);

sys.processWrite(proc, "hello\n");
sys.processCloseStdin(proc);

io.println(sys.processReadStdout(proc));
let code = sys.processWait(proc);
io.println(code);
```

`startWithOptions` accepts `command`, `args`, `cwd`, and `env`. It does not
accept `timeoutMs`; manage long-running process lifecycle yourself with
`processKill`, `processSignal`, or the object-oriented `process` module.

## Async Patterns

`sys.run` and `sys.runWithOptions` are synchronous. Use `async.run` to start
blocking work concurrently:

```gb
import async;
import sys;

let lintTask = async.run(func(): dict {
    return sys.run("eslint", ["src/"]);
});

let testTask = async.run(func(): dict {
    return sys.run("pytest", ["tests/"]);
});

let lintResult = await lintTask;
let testResult = await testTask;
```

Inside async functions, prefer `async.sleep` over `sys.sleep`.

## `process` Module

Import `process` for a class-based alternative to `sys.run` and `sys.start`.
The process module wraps the same underlying functionality in `Result` and
`Process` objects.

```gb
import process;

let result = process.run("git", ["status", "--short"]);
if (result.isOk()) {
    io.println(result.stdout());
}
```

### Module Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `process.run(cmd, args...)` | `Result` | Run command and wait |
| `process.runWithOptions(opts)` | `Result` | Run with options dict |
| `process.shell(cmd)` | `Result` | Run through the host shell |
| `process.start(cmd, args...)` | `Process` | Start a live process |
| `process.startWithOptions(opts)` | `Process` | Start with options dict |

Both `run` and `start` accept either variadic string arguments or a command
followed by a list.

### `process.Result`

| Method | Returns | Description |
|--------|---------|-------------|
| `isOk()` | `bool` | Exit code is 0 |
| `code()` | `int` | Exit code |
| `stdout()` | `string` | Captured stdout |
| `stderr()` | `string` | Captured stderr |
| `timedOut()` | `bool` | Process was killed by timeout |

### `process.Process`

| Method | Returns | Description |
|--------|---------|-------------|
| `write(text)` | `int` | Write text to stdin |
| `closeStdin()` | `void` | Close stdin |
| `readStdout()` | `string` | Read all remaining stdout |
| `readStderr()` | `string` | Read all remaining stderr |
| `readStdoutN(n)` | `string` | Read up to `n` bytes from stdout |
| `readStderrN(n)` | `string` | Read up to `n` bytes from stderr |
| `wait()` | `int` | Wait for exit |
| `kill()` | `void` | Send SIGKILL |
| `signal(name)` | `void` | Send `KILL`, `TERM`, `INT`, or `HUP` |
| `pid()` | `int` | OS process ID |

```gb
let proc = process.start("cat", []);
proc.write("hello\n");
proc.closeStdin();
io.println(proc.readStdout());
io.println(proc.wait());
```

### Current-process identity and credentials

The running script is itself a process, so its identity and credentials are
`process` functions. These are read-only and need no launch capability.

| Function | Returns | Description |
|----------|---------|-------------|
| `process.pid()` | `int` | Current process id (same value as `sys.pid()`) |
| `process.ppid()` | `int` | Parent process id |
| `process.uid()` | `int` | Real user id (unix) |
| `process.gid()` | `int` | Real group id (unix) |
| `process.euid()` | `int` | Effective user id (unix) |
| `process.egid()` | `int` | Effective group id (unix) |
| `process.groups()` | `list<int>` | Supplementary group ids (unix) |

The credential functions (`uid`, `gid`, `euid`, `egid`, `groups`) are unix
concepts. On platforms where they do not apply they throw a catchable error
naming the platform rather than returning a misleading zero.

### Inspecting other processes

Query processes by pid. These are read-only and need no launch capability.

| Function | Returns | Description |
|----------|---------|-------------|
| `process.list()` | `list<dict<string, any>>` | Running processes, each `{pid, ppid, name, cmdline, state}` |
| `process.info(pid)` | `dict<string, any>` | One process, or `null` when the pid is absent |
| `process.exists(pid)` | `bool` | Whether a process with that pid is running |

```gb
let self = process.info(process.pid());
io.println(self["name"]);
for (entry in process.list()) {
    io.println("${entry["pid"]} ${entry["name"]}");
}
```

`list` and `info` are implemented on Linux (read from `/proc`). On platforms
where they are not yet available they throw a catchable error naming the
platform. `exists` is available on all platforms.

### Privileged operations

Changing credentials or signalling an arbitrary process is dangerous, so these
operations are gated behind the `--allow-process-control` launch flag. Without
it, each throws a catchable `PermissionError` whose message names the flag.

| Function | Returns | Description |
|----------|---------|-------------|
| `process.setuid(uid)` | `void` | Set the real user id (unix, root-only) |
| `process.setgid(gid)` | `void` | Set the real group id (unix, root-only) |
| `process.kill(pid)` | `void` | Send SIGKILL to an arbitrary pid |
| `process.signal(pid, name)` | `void` | Send a named signal (`TERM`, `KILL`, `INT`, `HUP`, `QUIT`, `USR1`, `USR2`, `STOP`, `CONT`) to an arbitrary pid |

```gb
try {
    process.signal(targetPid, "TERM");
} catch (PermissionError e) {
    io.println(e.message);
}
```

Enable the privileged subset by launching with the flag:

```sh
geblang --allow-process-control app.gb
```

The `Process` handle methods `kill()` and `signal()` (above) act on a child the
script launched and stay ungated; only `process.kill(pid)` and
`process.signal(pid, name)` by arbitrary pid are gated. On Windows the signal set
is limited to `KILL`; other names throw a catchable error.

## Intercepting Signals

`sys.onSignal(name, handler)` traps a named signal for the current
process. The handler receives the canonical signal name and runs in an
isolated execution context (the same model as HTTP handlers), so share
state through `store` and terminate explicitly with `sys.exit`:

```gb
import io;
import sys;

sys.onSignal("SIGINT", func(string name): void {
    io.println("shutting down (${name})");
    sys.exit(0);
});
```

Supported names: `SIGINT`, `SIGTERM`, `SIGHUP`, `SIGQUIT`, `SIGUSR1`,
`SIGUSR2` (the `SIG` prefix is optional; `SIGUSR1`/`SIGUSR2` are not
available on Windows). `SIGKILL` cannot be trapped and is rejected.
Registering a handler replaces any previous handler for that signal;
`sys.clearSignal(name)` restores default delivery.

`sys.raise(name)` sends a signal to the current process - useful in
tests and for re-raising after cleanup:

```gb
import sys;
import store;

let state = store.Store();
state.set("reloads", 0);

sys.onSignal("SIGHUP", func(string name): void {
    state.update("reloads", func(any old): any { return (old as int) + 1; });
});
```

A handler that returns normally resumes the program; one that calls
`sys.exit(code)` runs the runtime's cleanup (open files, database
connections, destructors) and terminates with that code.
