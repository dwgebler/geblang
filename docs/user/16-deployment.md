# Deployment And Performance Tuning

This chapter covers running Geblang programs in production: shipping the
binary, picking sensible runtime settings, observing live behaviour, and
diagnosing hot spots. None of it changes how you write code - it's about
operating what you've already written.

## Shipping A Single Binary

`geblang build --entry <module> --out <path>` bundles your program, its
stdlib, and any installed dependencies into one self-contained executable.
The output is a normal Linux/macOS/Windows binary you can `scp` to a server
or copy into a Docker image; the receiver does not need Geblang installed.

```sh
geblang build --entry app --out dist/myapp
./dist/myapp --version
```

For containerised deployments, build inside a minimal base image and copy
the artefact into a small runtime image:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN GOTOOLCHAIN=local go build -o /out/geblang ./cmd/geblang
RUN /out/geblang build --entry app --out /out/myapp

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/myapp /myapp
ENTRYPOINT ["/myapp"]
```

A default `geblang build` binary embeds the running runtime, which links
glibc dynamically, so it needs a base image that provides libc (the
distroless `base-debian12` above, or `debian:bookworm-slim`), not `scratch`.
A binary cross-built with `scripts/cross-build.sh` forces `CGO_ENABLED=0` and
is fully static, so `FROM scratch` works for those.

The resulting image is typically 15-25 MB and starts in tens of
milliseconds. There is no JVM warm-up, no interpreter fork, no source
parsing on the hot path.

## Runtime Settings

### GOMAXPROCS

Geblang inherits Go's scheduler, and current toolchains are
container-aware: under cgroup CPU limits (Kubernetes, ECS, systemd) the
default thread count follows the container's allocation, not the host's
core count. Override only when you want fewer threads than the
allocation, for example to leave headroom for a sidecar:

```sh
GOMAXPROCS=2 ./myapp
```

### Garbage Collection

Geblang programs allocate the same way any Go program does, so the same GC
knobs apply:

- `GOGC` (default 100) - target heap growth before each collection. Lower
  values trade more CPU for a smaller resident set; higher values trade
  memory for fewer collections. For latency-sensitive workloads,
  `GOGC=200` and `GOMEMLIMIT=2GiB` is a common starting point.
- `GOMEMLIMIT` - soft cap on total Go memory. Beyond this, GC runs more
  aggressively. Set it just below your container limit so the OOM killer
  is never the first signal that the heap grew.

```sh
GOGC=150 GOMEMLIMIT=1GiB ./myapp
```

For batch jobs that allocate heavily and then exit, `GOGC=off` and a
generous memory limit can shave noticeable seconds off total runtime.
Don't do this for long-running services.

### Bytecode Cache

Binaries produced by `geblang build` bundle precompiled bytecode, so
they need no cache and no warm-up - first start is already the fast
path.

When you deploy source and run it with `geblang` directly, compiled
bytecode is cached under `.geblang-cache/<toolchain-version>/` relative
to the working directory. First runs parse + compile + execute;
subsequent runs skip parse/compile when the source hash matches. Run
each entrypoint once during the image build if you want cold starts
deterministic, and keep the cache directory out of version control.
Clean entries with `geblang cache clean` if you suspect staleness (the
source-hash check should make this unnecessary).

## Observability

### Structured Logging

Use the `log` stdlib module rather than `io.println` for anything that
should reach a log aggregator. It emits JSON lines with `level`, `time`,
`message`, and any `fields` you attach:

```gb
import log;

log.info("user.login", {"user_id": id, "ip": req.remoteAddr});
```

Pipe stdout straight into your platform's log collector
(stdout/stderr to Cloud Logging / Loki / Datadog / Elastic).

### Tracing And Metrics

The `metrics` module exposes counters, gauges, and histograms over a
Prometheus-compatible scrape endpoint:

```gb
import metrics;
metrics.inc("http.requests");
metrics.observe("http.latency_ms", 12.4);
```

For distributed tracing, the `trace` module emits OpenTelemetry spans:

```gb
import trace;
let span = trace.start("loadUser");
try {
    return loadUser(id);
} finally {
    trace.end(span);
}
```

Both modules are pull-based: your service exposes the data and your
observability stack scrapes it. There is no opinionated reporter to fight
with.

### Profiling

Geblang ships with a built-in `profiler` module for capturing CPU, heap,
and goroutine snapshots from inside the running program:

```gb
import profiler;
import io;

let snap = profiler.snapshot();
doWork();
let delta = profiler.delta(snap);
io.println("elapsed_ms = " + (delta["elapsed_ms"] as string));
io.println("heap_alloc = " + (delta["heap_alloc"] as string));
```

### Where a program spends its time

`geblang run --profile profile.txt app.gb` samples the program while it runs
and, when it exits, writes a report of where the time went:

```sh
geblang --profile profile.txt tools/simulate.gb 100000
```

About once a millisecond, each running goroutine records its Geblang call stack:
the function it is in and every function that called it, across modules
and inside `async` tasks. `profile.txt` lists the functions with the most
samples by own time (the function itself was running), by total time (it
or anything it called was running) and the hottest lines in loops.
`profile.folded` beside it holds the whole stacks, one per line, in the
format flame-graph tools read (`flamegraph.pl`, speedscope, Firefox
Profiler's importer).

Workers on the same goroutine contribute at most one sample per tick. If the
requested report already ends in `.folded`, its stack output is written to
`<report>.folded` so the text report is preserved.

Set `GEBLANG_PROFILE=profile.txt` to profile any run the same way, including
a built binary or a server; the report is written when the program exits
(for a server, when it shuts down). A sample is taken at the next function
call or loop iteration after each tick, and time in a native function
(`secureRandom`, `json`, a database query) counts towards the Geblang
function that called it. A call counts towards the function making it. The
evaluator (`--disable-vm`) samples at each statement and names a closure
after the module of the function that called it.

Sampling costs one check per function call and loop iteration while it is
off, and a few percent of run time while it is on.

For engine-level profiling, set `GEBLANG_PPROF=localhost:6060` when
starting any program (script or built binary) and a standard Go
`net/http/pprof` endpoint comes up on that address:

```sh
GEBLANG_PPROF=localhost:6060 ./myapp
go tool pprof "http://localhost:6060/debug/pprof/profile?seconds=10"
```

Bind it to localhost (or a private interface) only - the endpoint is
unauthenticated by design.

## Common Bottlenecks

When a program is slower than expected, the culprit is almost always one
of:

1. **Unbounded string accumulation.** Geblang strings are immutable.
   The engine recognises the common `acc = acc + piece` loop shape and
   appends in place, so simple accumulation loops are fast - but
   accumulation through function boundaries, conditionals the optimiser
   cannot see through, or interpolation rebuilds the string each time.
   For heavy assembly use the `strbuilder` module (amortised O(n)
   appends, one final `build`), or collect parts in a list and
   `parts.join("")` at the end.
2. **List indexing where a generator would do.** If you only consume the
   first N items of a large derived sequence, prefer a `generator<T>`
   function over building the full list.
3. **Per-request HTTP clients.** `http.get(...)` and friends use a
   shared default client, but if you wrap each call in a fresh
   `http.newClient(...)`, you also create a fresh connection pool. Build
   the client once at startup and reuse it.
4. **Synchronous I/O in async handlers.** A handler that calls
   `io.readText` while serving a request blocks the goroutine. Use
   `async.io.readText` (or `async.io.readBytes`) and await the result so
   other requests progress.
5. **Heavy work inside a class constructor.** Decorators and field
   defaults run for every instance. Cache shared state in `static`
   class members, or defer derived sequences with the lazy `streams`
   module so work happens on consumption, not construction.

Benchmark micro-suspects with the `benchmarks/` harness or the stdlib
`time.elapsed` helper:

```gb
import time;
import io;

let t = time.now();
doWork();
io.println("took " + (time.elapsed(t) as string) + "ms");
```

A 3-5x repeat is usually enough to filter noise; for statistically
robust numbers, `benchmarks/run.sh` reports a 5-run median (adjustable
with `--repeats`) across geblang/python/php/node for comparison.

## Production Checklist

- [ ] Binary built with `geblang build --out` (no source on the host).
- [ ] `GOMAXPROCS` matches the container's allocated CPU.
- [ ] `GOMEMLIMIT` is set just below the container memory limit.
- [ ] Source-run deployments only: bytecode cache warmed during the
      image build (built binaries bundle precompiled bytecode).
- [ ] Structured logs (`log.info / warn / error`) flow to stdout.
- [ ] `metrics` endpoint is wired up to your scrape target.
- [ ] Health-check route returns 200 only when downstream deps are
      reachable.
- [ ] Graceful shutdown: a SIGTERM handler (`sys.onSignal`) calls
      `http.shutdown(server, 5000)` so in-flight requests finish, while
      the main goroutine blocks in `http.wait(server)`.
- [ ] Crash reports include the toolchain version (`geblang --version`;
      built binaries answer `--version` too).
