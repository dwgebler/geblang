# Subprocess streaming

The `proc` module (1.1.0) spawns a child process and returns
immediately, exposing `stdout`, `stderr`, and `stdin` as
stream-shaped values (see `streams.IOStream`). Use it when you
need to start a long-running process, pipe data in, or read
output incrementally. For one-shot synchronous runs that return
when the child exits, use `process.run` from the `process`
module instead.

```gb
import io;
import proc;

let p = proc.spawn("echo", ["hello"]);
io.println(p.stdout.readAll());
let code = p.wait();
io.println("exit " + (code as string));
```

## API

`proc.spawn(command, args = [], opts = {}): Process`

Starts the process and returns a `Process` value. The
constructor is non-blocking; the child runs concurrently.

| `opts` key | Type | Description |
|------------|------|-------------|
| `pty` | `bool` | When true, attach a pseudo-terminal. `stdin` and `stdout` share the master fd; `stderr` is `null`. |
| `cwd` | `string` | Set the child's working directory. |
| `env` | `dict<string, string>` | Replace the child's environment. |

The returned `Process` has:

| Member | Type | Description |
|--------|------|-------------|
| `pid` | `int` | The child's process id |
| `handle` | `int` | Opaque native handle |
| `stdin` | `streams.IOStream` | Write-only stream to the child's stdin |
| `stdout` | `streams.IOStream` | Read-only stream from the child's stdout |
| `stderr` | `streams.IOStream` or `null` | Read-only stream from stderr (null in PTY mode) |
| `wait()` | `int` | Block until exit; return the exit code |
| `kill()` | `void` | Send SIGKILL |
| `signal(name)` | `void` | Send the named signal (`"SIGTERM"`, `"SIGHUP"`, etc.) |

## Streaming stdin and stdout

`Process.stdin` is an `IOStream`. Use `write` / `writeln` and
call `close()` to signal EOF to the child. `stdout` exposes the
same surface; iterate with `for (line in p.stdout)` to consume
line-by-line.

```gb
import io;
import proc;

let p = proc.spawn("grep", ["error"]);
p.stdin.writeln("log: ok");
p.stdin.writeln("log: error here");
p.stdin.close();
for (line in p.stdout) {
    io.println(line);
}
p.wait();
```

## PTY mode

Some interactive programs detect a terminal and behave
differently. `{pty: true}` attaches a pseudo-terminal so the
child sees a TTY:

```gb
import io;
import proc;

let p = proc.spawn("python3", ["-i"], {"pty": true});
p.stdin.writeln("print(1 + 1)");
p.stdin.writeln("exit()");
io.println(p.stdout.readAll());
p.wait();
```

In PTY mode `stderr` merges into `stdout`, so `p.stderr` is
`null`.

## Signals and exit

These are instance methods on the spawned `Process` value, not module
functions. `p.kill()` sends SIGKILL; `p.signal(name)` sends a named
signal:

```gb
import sys;
import proc;

let p = proc.spawn("sleep", ["60"]);
sys.sleep(100);
p.signal("SIGTERM");
let code = p.wait();
```

The exit code from `wait()`:
- Normal exit: the program's exit code.
- Killed by signal: `-1`.

## When to prefer `process.run`

The `process` module's `process.run(cmd, args, opts)` is the
right choice when:

- The child writes a bounded amount of output (no streaming).
- You want to block until completion and inspect the result.
- You don't need to pipe stdin.

`proc.spawn` is the right choice when:

- The child runs concurrently with the parent.
- You want to stream stdout / stderr incrementally.
- You need to write to stdin and close it.
- You need PTY mode for interactive programs.
