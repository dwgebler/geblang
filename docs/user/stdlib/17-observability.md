# Observability

Geblang provides lightweight observability modules for scripts, services, tests,
and framework code:

- `log` for structured logging to stdout, stderr, files, syslog, or custom handlers (see the [Logging](17a-logging.md) chapter).
- `metrics` for in-process counters, gauges, and simple timings.
- `trace` for span-style request/workflow traces.
- `profile` for runtime memory and GC diagnostics.

These modules are intentionally small. They provide useful defaults and clear
extension points without requiring a collector, daemon, or external service.

## Logging

Logging has its own chapter: see [Logging](17a-logging.md) for destinations
(stdout, stderr, file, stream, syslog, custom), levels, the `log.LogInterface`
handler, and logging patterns.

## Metrics

Import `metrics` for process-local counters and gauges. Metric names are
free-form strings; use dot-separated names by convention:
`"http.requests"`, `"jobs.completed"`, `"db.pool.open"`.

### Recording Values

| Function | Returns | Description |
|----------|---------|-------------|
| `inc(name)` | `void` | Increment a counter by 1 |
| `inc(name, amount)` | `void` | Increment by an integer amount |
| `set(name, value)` | `void` | Set a metric to an absolute numeric value |
| `reset()` | `void` | Clear all metrics |

```gb
metrics.inc("jobs.completed");
metrics.inc("bytes.sent", 4096);
metrics.set("queue.depth", 12);
```

### Reading Values

| Function | Returns | Description |
|----------|---------|-------------|
| `get(name)` | number | Current value for one metric |
| `snapshot()` | `dict<string, number>` | Copy of all metrics |

```gb
let jobs = metrics.get("jobs.completed");
let all = metrics.snapshot();
```

### Timing

| Function | Returns | Description |
|----------|---------|-------------|
| `now()` | opaque timestamp | Monotonic timestamp token for `duration()` |
| `duration(start)` | `float` | Milliseconds elapsed since `start` (sub-millisecond precision) |

```gb
let start = metrics.now();
# work
metrics.set("job.durationMs", metrics.duration(start));
```

Metrics are in-process. Export snapshots to logs, HTTP endpoints, or external
collectors when you need persistence.

### Labelled metrics and Prometheus exposition

For Prometheus-compatible scraping, declare the metric kind explicitly. Labels
are declared up front and label values pick a per-combo slot:

| Function | Returns | Description |
|----------|---------|-------------|
| `counter(name, opts)` | `string` | Declare a counter. opts.help (string), opts.labels (list<string>) |
| `gauge(name, opts)` | `string` | Declare a gauge. Same opts shape as counter. |
| `histogram(name, opts)` | `string` | Declare a histogram. opts.buckets (ascending list<float>) overrides the default set. |
| `observe(name, value, labels?)` | `void` | Record a histogram sample. |
| `toPrometheus()` | `string` | Emit every registered metric in Prometheus v0.0.4 text format. |

```gb
metrics.counter("http_requests_total", {
    "help":   "Total HTTP requests",
    "labels": ["path", "status"]
});

metrics.inc("http_requests_total", 1, {"path": "/api", "status": "200"});
metrics.inc("http_requests_total", 2, {"path": "/api", "status": "200"});

metrics.histogram("request_seconds", {
    "labels":  ["path"],
    "buckets": [0.005, 0.01, 0.025, 0.05, 0.1, 0.5, 1]
});
metrics.observe("request_seconds", 0.012, {"path": "/api"});

io.println(metrics.toPrometheus());
```

Output:

```
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{path="/api",status="200"} 3
# TYPE request_seconds histogram
request_seconds_bucket{path="/api",le="0.005"} 0
request_seconds_bucket{path="/api",le="0.01"} 0
request_seconds_bucket{path="/api",le="0.025"} 1
... (cumulative buckets up to +Inf, plus _sum and _count)
```

Legacy label-less metrics (recorded via `metrics.inc(name)` without a prior
`metrics.counter` declaration) appear in the Prometheus output with TYPE
`untyped`. Mix the two styles freely - the declared metrics get full HELP +
TYPE headers; the legacy ones get the simple form.

Histogram defaults: when no `opts.buckets` is given the metric uses the
Prometheus client-library default set: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25,
0.5, 1, 2.5, 5, 10. Override with a list of ascending upper bounds; the
`+Inf` bucket is added automatically.

## Trace

Import `trace` for lightweight span-based tracing. Spans are handles while they
are active and dictionaries after `snapshot`.

### Span API

| Function | Returns | Description |
|----------|---------|-------------|
| `start(name)` | span handle | Start a span |
| `event(span, name, fields)` | `void` | Attach an event |
| `end(span)` | `void` | Finish a span |
| `snapshot()` | `list<dict>` | Completed spans |
| `reset()` | `void` | Clear completed spans |

```gb
let span = trace.start("load-users");
trace.event(span, "query", {"table": "users"});
trace.event(span, "marshal", {"format": "json"});
trace.end(span);

let spans = trace.snapshot();
```

Completed span dictionaries contain:

| Key | Type | Description |
|-----|------|-------------|
| `id` | `int` | Span id |
| `name` | `string` | Span name |
| `startUnixNano` | `int` | Start timestamp (nanoseconds) |
| `ended` | `bool` | Whether the span has ended |
| `endUnixNano` | `int` | End timestamp (nanoseconds) |
| `durationNanos` | `int` | Duration in nanoseconds |
| `attrs` | `dict` | Span attributes |
| `events` | `list<dict>` | Span events |

Event dictionaries contain `name`, `attrs`, and `unixNano`.

### Parent / child spans

A child span shares its parent's `traceId` and records the parent's
`spanId` in its `parentSpanId`. Pass the parent's handle via opts on
`trace.start`:

```gb
let root = trace.start("handle-request", {"http.method": "GET"});
let dbSpan = trace.start("db.query", {}, {"parent": root});
trace.event(dbSpan, "row-fetched", {"count": 42});
trace.end(dbSpan);
trace.end(root);
```

Root spans (no `opts.parent`) get a fresh 16-byte traceId; child spans
inherit it. Span IDs are 8 random bytes per span. IDs are exposed in
OTLP output only - the `snapshot()` dict surface stays the same shape
as before.

### OTLP export

| Function | Returns | Description |
|----------|---------|-------------|
| `toOtlpJson(opts?)` | `string` | Serializes recorded spans as an OTLP/HTTP JSON request body |
| `exportOtlp(endpoint, opts?)` | `dict` | POSTs the OTLP/HTTP body to a collector and returns `{status, ok}` |

`opts` for both functions accepts:

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `serviceName` | `string` | `"geblang"` | The `service.name` resource attribute |
| `scopeName` | `string` | `"geblang.trace"` | InstrumentationScope name |
| `scopeVersion` | `string` | toolchain version | InstrumentationScope version (defaults to the running Geblang version) |
| `resource` | `dict<string, string>` | `{}` | Extra resource attributes |
| `headers` | `dict<string, string>` | `{}` | Extra HTTP headers (exportOtlp only). Use to set `Authorization`, `X-Api-Key`, etc. |
| `timeoutMs` | `int` | `10000` | HTTP timeout (exportOtlp only) |

`endpoint` is the collector base URL (e.g. `http://localhost:4318`);
`/v1/traces` is appended automatically. The collector returning a
non-2xx status sets `result.ok = false` but does NOT throw, so the
caller can decide whether to retry / buffer.

```gb
import trace;

let span = trace.start("checkout");
trace.end(span);

let result = trace.exportOtlp("http://localhost:4318", {
    "serviceName": "checkout-service",
    "headers":     {"Authorization": "Bearer ..."}
});
if (!result["ok"]) {
    log.error(stderr, "OTLP export failed", {"status": result["status"]});
}
```

Compatible with any collector that accepts OTLP/HTTP JSON: the
OpenTelemetry Collector, Jaeger (with the OTLP receiver enabled),
Tempo, Datadog Agent, Honeycomb, etc.

## Profile

Import `profile` for runtime diagnostics. These helpers are useful for
debugging memory-heavy scripts and checking allocation pressure.

### Memory Stats

| Function | Returns | Description |
|----------|---------|-------------|
| `memStats()` | `dict` | Runtime memory and GC counters |
| `gc()` | `void` | Force a garbage collection cycle |

```gb
let mem = profile.memStats();
io.println("heap alloc bytes: " + mem["heapAlloc"] as string);
io.println("sys bytes: " + mem["sys"] as string);
io.println("gc cycles: " + mem["numGC"] as string);
```

Common `memStats` fields include `heapAlloc`, `heapSys`, `heapInuse`,
`heapObjects`, `sys`, `numGC`, and `pauseTotalNs`.

### Timing

| Function | Returns | Description |
|----------|---------|-------------|
| `now()` | opaque timestamp | Monotonic timestamp token for `elapsed()` |
| `elapsed(start)` | `float` | Milliseconds elapsed since `start` (sub-millisecond precision) |

```gb
let start = profile.now();
# work
let ms = profile.elapsed(start);
io.println("elapsed ms: " + ms as string);
```

## Profiler

Import `profiler` for precise CPU time and heap measurements from a native
Go runtime interface. Use `profiler.snapshot()` and `profiler.delta()` together
to bracket a section of code, or call `profiler.memory()` and `profiler.cpu()`
for one-off readings.

This module is always available as a native module and does not require
importing from `stdlib/`.

### Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `snapshot()` | `dict` | Captures wall clock (`wall_ns`), heap allocation (`heap_alloc`, `peak_alloc`, `total_alloc`), GC count (`num_gc`), and CPU nanoseconds (`cpu_user_ns`, `cpu_sys_ns`) |
| `delta(snapshot)` | `dict` | Returns measurements since the snapshot: `elapsed_ms`, `cpu_ms`, `heap_alloc`, `allocs`, `gc_count` |
| `memory()` | `dict` | Returns `heap_alloc`, `peak_alloc`, `heap_sys`, `stack_sys`, `total_alloc`, `gc_count` |
| `cpu()` | `dict` | Returns `user_ms` and `sys_ms` CPU time used by this process |
| `peak()` | `dict` | Returns `peak_alloc`: the highest heap allocation observed since the profiler was first called |

`peak_alloc` tracks the maximum live heap bytes seen across all profiler calls in the
process lifetime - equivalent to PHP's `memory_get_peak_usage()`. It is updated on every
call to `snapshot()`, `memory()`, and `peak()`.

### Example

```gb
import profiler;
import io;

let snap = profiler.snapshot();

# do some work
let sum = 0;
for (i in range(0, 1000000)) {
    sum = sum + i;
}

let d = profiler.delta(snap);
io.println("elapsed: " + d["elapsed_ms"] as string + " ms");
io.println("cpu: " + d["cpu_ms"] as string + " ms");
io.println("heap delta: " + d["heap_alloc"] as string + " bytes");
io.println("allocations: " + d["allocs"] as string + " bytes total");
io.println("gc cycles: " + d["gc_count"] as string);
```

For a one-off memory check including peak:

```gb
import profiler;
import io;

let mem = profiler.memory();
io.println("heap in use: " + mem["heap_alloc"] as string + " bytes");
io.println("peak heap:   " + mem["peak_alloc"] as string + " bytes");
io.println("heap from OS: " + mem["heap_sys"] as string + " bytes");
```

To check peak memory at the end of a script (similar to `memory_get_peak_usage()` in PHP):

```gb
import profiler;
import io;

# ... script work ...

let p = profiler.peak();
io.println("peak memory: " + p["peak_alloc"] as string + " bytes");
```

### Context Managers (1.14.0)

`profiler.timer()` and `profiler.profile()` return context managers for use with `with`. Declare the handle before the block and read its accessors after the block exits.

```gb
import profiler;
import io;

let t = profiler.timer();
with (t) {
    /* work to measure */
}
io.println("elapsed: " + (t.elapsedMs() as string) + " ms");

let p = profiler.profile();
with (p) {
    /* work to measure */
}
io.println("cpu: " + (p.cpuMs() as string) + " ms, heap: " + (p.heapBytes() as string) + " bytes");
```

**Timer methods**

| Method | Returns | Description |
|--------|---------|-------------|
| `elapsedNs()` | `int` | Elapsed nanoseconds for the block (0 before the block exits) |
| `elapsedMs()` | `float` | Elapsed milliseconds, microsecond precision |

**Profile methods**

| Method | Returns | Description |
|--------|---------|-------------|
| `elapsedMs()` | `float` | Wall-clock milliseconds for the block |
| `cpuMs()` | `float` | CPU milliseconds (user + sys) for the block |
| `heapBytes()` | `int` | Net heap bytes allocated during the block |
| `allocs()` | `int` | Total bytes allocated during the block |
| `gcCount()` | `int` | GC cycles during the block |
| `report()` | `dict` | Full measurement dict: `elapsed_ms`, `cpu_ms`, `heap_alloc`, `allocs`, `gc_count` |
