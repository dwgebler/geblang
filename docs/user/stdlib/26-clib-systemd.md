# systemd Integration (`clib.systemd`)

> **Linux only.** This module dlopens libsystemd and will throw at
> import time on systems where the library is absent.

`import clib.systemd as systemd;` provides two things: the sd_notify
readiness protocol (READY, WATCHDOG, STATUS) and structured journald
logging. Together these are the standard integration points for a
long-running process managed by systemd.

The module uses the FFI layer so the `ffi` capability must be enabled.

## Capability

Add to `geblang.yaml`:

```yaml
permissions:
  ffi:
    enabled: true
    libraries:
      - glob: libsystemd*
```

For a standalone script:

```sh
geblang --allow-ffi 'libsystemd*' script.gb
```

## Functions

### `notify(string state): bool`

Raw `sd_notify` call. `state` is a newline-separated set of
assignments (`"READY=1"`, `"STATUS=serving 12 requests"`). Returns
`true` if the notification was delivered to the service manager,
`false` if the process is not running under systemd. Calling this
outside a systemd service is safe and returns `false`.

### `ready(): bool`

Sends `READY=1`. Call this once the process has finished
initialisation and is ready to accept work. Equivalent to
`notify("READY=1")`.

### `watchdog(): bool`

Sends `WATCHDOG=1`. Call periodically (at most half the watchdog
interval) to prevent the service manager from restarting the process.

### `status(string text): bool`

Sets the free-form status line visible in `systemctl status`. The
text becomes `STATUS=<text>`. Useful for progress updates during
long operations.

### `journal(string message, dict<string,string> fields = {}): void`

Sends a structured entry to the systemd journal. `message` becomes
the `MESSAGE` field. Each key/value pair in `fields` is sent as an
additional `KEY=value` field. Standard journald field names include
`PRIORITY` (syslog numerics: `"3"` = error, `"6"` = info),
`SYSLOG_IDENTIFIER`, and `UNIT`. Custom field names are allowed and
are searchable with `journalctl`.

## Service readiness sequence

```gb
import clib.systemd as systemd;
import io;

/* ... initialise database connections, load config ... */

let delivered = systemd.ready();
io.println("ready notification delivered: ${delivered}");
systemd.status("serving");
```

Updating the status line during long operations:

```gb
systemd.status("running migration 3 of 7");
/* ... */
systemd.status("migration complete");
```

## Structured journal logging

```gb
import clib.systemd as systemd;

systemd.journal("request handled", {
    "PRIORITY":          "6",
    "SYSLOG_IDENTIFIER": "myapp",
    "REQUEST_ID":        requestId,
    "HTTP_STATUS":       "200",
});
```

Query the journal with matching fields:

```sh
journalctl SYSLOG_IDENTIFIER=myapp HTTP_STATUS=200
```

## Watchdog example

```gb
import clib.systemd as systemd;
import async;

/* Ping the watchdog every 10 seconds from a background task. */
async.run(func(): void {
    while (true) {
        systemd.watchdog();
        async.sleep(10000);
    }
});
```

Set `WatchdogSec=30` in the `[Service]` section of the unit file and
call `watchdog()` at most every 15 seconds.

## Thread safety

`sd_notify` and `sd_journal_sendv` are thread-safe in libsystemd.
All five functions in this module are safe to call concurrently from
multiple async tasks.
