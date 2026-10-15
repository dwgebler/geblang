# Logging

Geblang's `log` module emits structured JSON log entries to stdout, stderr,
files, network syslog, or custom handlers. A logger is a runtime handle
returned by a destination constructor (`log.stdout`, `log.stderr`, `log.file`,
`log.toStream`, `log.syslog`, or `log.custom`).

```gb
import log;

let logger = log.stdout();
log.info(logger, "server started", {"port": 8080});
```

### Built-In Logger Destinations

| Function | Returns | Description |
|----------|---------|-------------|
| `stdout()` | logger handle | Write JSON log lines to stdout |
| `stderr()` | logger handle | Write JSON log lines to stderr |
| `file(path)` | logger handle | Append JSON log lines to a file |
| `toStream(stream)` | logger handle | Write JSON log lines to any `streams.IOStream` (memory buffer, TCP socket, pipe, ...) |
| `syslog(opts)` | logger handle | Send RFC 5424 syslog records over UDP/TCP or to the local daemon (see [Syslog](#syslog)) |
| `close(logger)` | `void` | Close and unregister a logger |

`toStream` leaves the underlying stream alone on `log.close`, so the same
stream can back multiple loggers or stay open for non-log traffic. Pair it
with `streams.memory()` for in-process capture in tests, or with a network
stream to ship logs to a TCP/TLS log collector.

```gb
import streams;
import io;
import log;
let buf = streams.memory();
let capture = log.toStream(buf);
log.info(capture, "captured", {"k": "v"});
log.close(capture);
io.println(buf.toString());     # the JSON line(s) just written
```

```gb
let out = log.stdout();
let err = log.stderr();
let file = log.file("/var/log/app.log");
defer log.close(file);

log.info(out, "ready");
log.warn(err, "using fallback config", {"path": "config/local.yaml"});
log.error(file, "request failed", {"status": 500, "path": "/api/users"});
```

Built-in loggers emit one JSON object per line. The exact field order is not
part of the API, but entries include at least:

| Field | Type | Description |
|-------|------|-------------|
| `level` | `string` | `info`, `warn`, `error`, or `debug` |
| `message` | `string` | Log message |
| `fields` | `dict` | Structured fields passed by the caller |
| `time` | `string` | Timestamp as an RFC 3339 nanosecond string |

### Levels

| Function | Description |
|----------|-------------|
| `info(logger, message)` | Informational event |
| `info(logger, message, fields)` | Informational event with fields |
| `warn(logger, message)` | Warning event |
| `warn(logger, message, fields)` | Warning event with fields |
| `error(logger, message)` | Error event |
| `error(logger, message, fields)` | Error event with fields |
| `debug(logger, message)` | Debug event |
| `debug(logger, message, fields)` | Debug event with fields |

`fields` must be a dictionary. Keep fields machine-readable: prefer ids,
status codes, durations, paths, and booleans over preformatted message text.

```gb
log.info(logger, "user login", {
    "userId": "42",
    "ip": request["remoteAddr"],
    "remember": true
});
```

### `log.LogInterface`

Custom handlers can implement the exported `log.LogInterface`. Its required
method has this shape:

<!-- doctest:skip (callback signature illustration, not a full program) -->
```gb
func handle(string level, string message, dict<string, any> fields): void;
```

Use it when a class should be accepted by logging-aware code or when you want
compile-time checking of the handler shape.

```gb
import io;
import json;
import log;

class JsonSink implements log.LogInterface {
    func handle(string level, string message, dict<string, any> fields): void {
        io.println(json.stringify({
            "level": level,
            "message": message,
            "fields": fields
        }));
    }
}

let logger = log.custom(JsonSink());
log.info(logger, "custom logger ready", {"handler": "json"});
```

`log.custom(handler)` still checks structurally for a compatible `handle`
method, but implementing `log.LogInterface` makes the contract explicit:

```gb
class MemoryLogger implements log.LogInterface {
    list<dict<string, any>> entries = [];

    func handle(string level, string message, dict<string, any> fields): void {
        this.entries = this.entries.push({
            "level": level,
            "message": message,
            "fields": fields
        });
    }
}

let sink = MemoryLogger();
let logger = log.custom(sink);

log.error(logger, "validation failed", {"field": "email"});
io.println(sink.entries[0]["message"]);
```

Custom handlers are useful for:

- writing to external services;
- forwarding logs to test assertions;
- adapting logs into framework-specific event systems;
- redacting or transforming fields before output.

### Logger Lifecycle

Call `log.close(logger)` when a logger is no longer needed. File loggers close
their underlying file handle. Stdout/stderr/custom loggers unregister the
runtime handle.

```gb
let file = log.file("app.log");
defer log.close(file);
```

Do not write to a logger after closing it; the runtime will report an unknown
logger handle.

### Logging Patterns

Create a request logger:

```gb
func logRequest(any logger, dict<string, any> req, int status, int durationMs): void {
    log.info(logger, "http request", {
        "method": req["method"],
        "path": req["path"],
        "status": status,
        "durationMs": durationMs
    });
}
```

Capture logs in tests:

```gb
class Capture implements log.LogInterface {
    list<dict<string, any>> entries = [];

    func handle(string level, string message, dict<string, any> fields): void {
        this.entries = this.entries.push({
            "level": level,
            "message": message,
            "fields": fields
        });
    }
}

let capture = Capture();
let logger = log.custom(capture);
log.warn(logger, "rate limit", {"limit": 100});

io.println(capture.entries.length());
```
## Syslog

`log.syslog(opts)` sends entries to a syslog server or the local syslog daemon,
framed as RFC 5424. It is a logger destination like the others, so the same
`log.info` / `log.warn` / `log.error` / `log.debug` calls apply.

```gb
import log;

let logger = log.syslog({
    "network":  "udp",
    "address":  "logs.example.com:514",
    "facility": "local0",
    "app":      "checkout"
});
log.info(logger, "order placed", {"orderId": "A123"});
log.close(logger);
```

### Options

| Key | Default | Description |
|-----|---------|-------------|
| `network` | `"udp"` | `"udp"`, `"tcp"`, or `"local"` (the platform's syslog daemon socket). |
| `address` | required for udp/tcp | `host:port` of the syslog server. Ignored for `"local"`. |
| `facility` | `"user"` | Syslog facility name: `kern`, `user`, `daemon`, `auth`, `syslog`, `cron`, `local0`-`local7`, and others. |
| `app` | executable name | RFC 5424 APP-NAME. |
| `hostname` | OS hostname | RFC 5424 HOSTNAME. |

### Cross-platform behaviour

`"udp"` and `"tcp"` work on Linux, macOS/BSD, and Windows. `"local"` uses the
local daemon socket (`/dev/log` on Linux, `/var/run/syslog` on macOS/BSD) and
is Unix-only; on Windows it raises an error, because Windows has no syslog
daemon - send to a collector with `"udp"` or `"tcp"` instead.

### Message format

Each record is an RFC 5424 line:

```
<PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID - - MSG
```

`PRI` is `facility * 8 + severity`, where the level maps to a severity: `debug`
-> 7, `info` -> 6, `warn` -> 4 (warning), `error` -> 3 (err). `MSG` is the same
JSON object the other destinations emit (`level`, `message`, `fields`, `time`),
so a syslog pipeline and a file pipeline carry identical structured data.

`log.syslog` connects when constructed, so an unreachable host or a malformed
address fails immediately. Once connected, a transient send failure is dropped
rather than raised, so logging never crashes a running program.

See also [Observability](17-observability.md) for metrics, tracing, and profiling.
