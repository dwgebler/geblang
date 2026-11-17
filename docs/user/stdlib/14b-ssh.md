# SSH

The `ssh` module (1.2.0) is a Geblang-native SSH client. It runs
commands, streams I/O, transfers files via SFTP, and forwards
ports - all on top of the stream protocol so SSH sessions
behave the same as files, pipes, and TCP sockets.

## Connect

```gb
import io;
import ssh;

let c = ssh.connect("alice@server.example.com", {
    "port": 22,
    "privateKeyFile": "~/.ssh/id_rsa",
    "knownHostsFile": "~/.ssh/known_hosts",
});
let r = c.exec("uname -a");
io.println(r.stdout);
c.close();
```

`ssh.connect(target, opts = {})`:

| `opts` key | Type | Description |
|------------|------|-------------|
| `port` | `int` | Default 22 |
| `timeoutMs` | `int` | Connect timeout |
| `password` | `string` | Password auth |
| `privateKey` | `string` | PEM private key as a string |
| `privateKeyFile` | `string` | Path to a PEM private key |
| `passphrase` | `string` | Decrypts a passphrased key |
| `agent` | `bool` | Use `$SSH_AUTH_SOCK` |
| `knownHostsFile` | `string` | Path to a `known_hosts` file |
| `insecureSkipHostKey` | `bool` | Skip host-key verification (dev only) |

`target` is `"user@host"` or just `"host"` (defaults to `$USER`).
At least one auth method is required. At least one host-key check
is required - explicit opt-in to insecure mode is safer than a
silent default.

## One-shot commands: `exec`

```gb
let r = c.exec("ls /srv");
io.println(r.stdout);    # captured output
io.println(r.stderr);    # captured error stream
io.println(r.exitCode);  # 0 on success
```

`exec` blocks until the command completes and returns an
`ExecResult`. Use it for short commands with bounded output.

## Streaming: `spawn`

```gb
let s = c.spawn("tail -f /var/log/app.log");
for (line in s.stdout) {
    io.println(line);
}
s.kill();
```

`spawn` returns an `SSHSession` with `stdin` / `stdout` / `stderr`
as `streams.IOStream` values - the same shape as
`proc.Process`. Use it for long-running commands or when
you need to stream input through stdin.

Because the remote host controls what arrives on `stdout` and `stderr`,
they have the same limits as sockets: a line read is capped at 1 MiB and
`readAll` / `readLines` at 64 MiB in total (pass `maxBytes` to change it,
`0` for no limit); going over raises `IOError`.

| Method | Returns | Description |
|--------|---------|-------------|
| `wait()` | `int` | Block until exit, return exit code |
| `kill()` | `void` | Send SIGKILL via the SSH channel |
| `signal(name)` | `void` | Send a named signal (`"TERM"`, `"HUP"`, ...) |

## SFTP

```gb
c.upload("./app.bin", "/srv/bin/app");
c.download("/srv/log/app.log", "./remote.log");
for (entry in c.sftpList("/srv")) {
    io.println(entry["name"] as string);
}
c.sftpMkdir("/srv/new", 0o755);
c.sftpRemove("/srv/old.tmp");

# Stream a remote file as a regular IOStream (read mode, default).
let remote = c.sftpOpen("/srv/log/app.log");
for (line in remote) {
    io.println(line);
}
remote.close();

# Write to a remote file.
let out = c.sftpOpen("/srv/data/out.txt", "w");
out.write("hello\n");
out.close();
```

`sftpOpen(remotePath, mode = "r")` returns a `streams.IOStream`. Pass `"w"` to
open for writing. You can pipe it through `streams.copy(remote, localFile)` or
iterate line-by-line.
The underlying SFTP client is lazy-initialised and cached on the
SSH connection - the first SFTP call pays the setup cost; later
calls reuse it.

## Port forwarding

```gb
# Local: localhost:8080 -> internal-db:5432 (via the SSH server).
let tunnel = c.forwardLocal(8080, "internal-db:5432");

# ... use 127.0.0.1:8080 from this process ...

tunnel.close();
```

```gb
# Remote: <server>:9090 -> localhost:3000 (server initiates connections back).
let rev = c.forwardRemote(9090, "localhost:3000");
rev.close();
```

Tunnels run an accept-loop goroutine on the appropriate side.
`close()` stops the loop and joins so the next call from the
parent goroutine happens-after the last forwarded byte.

## When to use ssh vs proc vs sockets

| Use case | Module |
|----------|--------|
| Run a local subprocess | `proc.spawn` |
| Run a remote command via SSH | `ssh.spawn` (or `ssh.exec`) |
| Transfer files between hosts | `ssh.upload` / `ssh.download` |
| Talk to a non-SSH TCP service | `sockets.dial` |
| Build a TCP server | `sockets.serve` |
| Expose a non-SSH service through SSH | `ssh.forwardLocal` |

The `Process`, `Socket`, and `SSHSession` classes all expose the
same `streams.IOStream`-shaped stdin / stdout / stderr surface, so
piping data between them is uniform.
