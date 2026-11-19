# Watch

The `watch` module exposes two ways to observe filesystem changes:
event-driven watching via `watch.start` (1.1.0, backed by
fsnotify) and a simple polling pair (`snapshot` / `wait`) for
scripts that prefer not to keep a callback alive.

## Event-driven watching (`watch.start`)

```gb
import io;
import watch;

let handle = watch.start("config", func(dict<string, any> event): void {
    io.println((event["type"] as string) + " " + (event["path"] as string));
}, {"recursive": true});

# ... do other work; the callback fires for each event ...

watch.stop(handle);
```

Functions:

- `watch.start(path, callback)`: register a watcher on a file or
  directory; returns a handle. The callback receives an event
  dict for each filesystem event.
- `watch.start(path, callback, {recursive: true})`: walk
  subdirectories at start time and register each one (new
  directories created after registration are not auto-added).
- `watch.stop(handle)`: stop the watcher and wait for the
  in-flight callback to finish before returning.

Event dict shape:

| Key | Type | Description |
|-----|------|-------------|
| `path` | `string` | Full path of the file the event refers to |
| `type` | `string` | `"create"`, `"write"`, `"remove"`, `"rename"`, or `"chmod"` |

Callbacks fire on a worker goroutine. Mutations to module-level
state in the callback are visible to the parent after
`watch.stop` returns. Mutations to local closures are not
propagated back across goroutines, so collect into module-level
state when you need to observe events from outside the callback.

## Polling (`snapshot` / `wait`)

Useful for tools and tests that just want to know whether
something has changed since a known baseline.

```gb
import io;
import watch;

let changed = watch.wait("config/app.yaml", 5000);

if (changed["changed"] as bool) {
    io.println("config changed");
}
```

Functions:

- `snapshot(path)`: capture the current state of a file or
  directory (returned as a dict).
- `wait(path, timeoutMs = 30000, intervalMs = 250)`: take a fresh
  snapshot, then poll until the path changes or the timeout expires.
  Returns `{"changed": bool, "before": dict, "after": dict}`.
  `intervalMs` controls the poll interval (default 250 ms).

Polling is fine for low-volume workloads. For high-volume
watching, prefer `watch.start`.
