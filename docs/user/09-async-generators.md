# Async And Generators

## Async Functions

Async functions use `async func`. Calling one starts it and returns a `Task`.
The call itself does not block; the program only waits when it reaches `await`
or `task.await()`.

```gb
async func double(int x): int {
    return x * 2;
}

let task = double(21);
io.println("calculation has started");
io.println(await task);
```

Tasks can also be inspected:

```gb
io.println(task.done());
io.println(task.await());
```

### Async methods

Methods can be async too. Calling one starts it and returns a `Task`, however
the method is reached: on an instance, through `this`, inherited, as a bound
method value, or on a class from another module. A static method is written
`static async func`:

```gb
class Rates {
    async func refresh(): int { ... }
    static async func fetch(string code): decimal { ... }
}

let pending = [Rates.fetch(code) for code in ["GBP", "EUR"]];
let rates = await async.all(pending);
```

Enum methods can be async as well (`async func` inside the enum's method
block); calling one on a variant returns a `Task`.

An async call made inside another async function or method also returns a
`Task`, so fan-out inside async code runs concurrently.

## Async Module

```gb
import async;
import io;

let worker = async.run(func(): int {
    return 42;
});

io.println(async.await(worker));
await async.sleep(100);
```

`async.done(task)` reports completion.

### Combining tasks

`async.all([tasks])` returns a new task that completes when every input task
has completed. The result is a list of values in the original order. If any
input fails, the returned task fails immediately with the first error and the
remaining tasks are cancelled.

```gb
import async;
import io;

let pageA = async.run(func(): string {
    await async.sleep(40);
    return "A";
});
let pageB = async.run(func(): string {
    await async.sleep(60);
    return "B";
});

let pages = await async.all([pageA, pageB]);
io.println(pages[0]);  # "A"
io.println(pages[1]);  # "B"
```

`async.race([tasks])` returns a task that completes with the first finisher's
value. The rest are cancelled.

```gb
let fast = async.run(func(): string {
    await async.sleep(20);
    return "fast";
});
let slow = async.run(func(): string {
    await async.sleep(200);
    return "slow";
});

io.println(await async.race([fast, slow]));  # "fast"
```

For fan-out that must complete as a unit (all-or-nothing, with the rest
cancelled on the first failure), see Structured Concurrency below.

### Timeouts

`async.timeout(task, ms)` wraps a task with a deadline. If the task does not
complete within the given milliseconds, the wrapping task fails with a
`TimeoutError` (an `IOError` subclass) and the inner task is cancelled.

```gb
let slow = async.run(func(): string {
    await async.sleep(500);
    return "never";
});

# Awaiting the timeout wrapper raises TimeoutError when the deadline expires:
let result = await async.timeout(slow, 50);   # TimeoutError after 50ms
```

### Cancellation

A task can be cancelled explicitly with `async.cancel(task)` or `task.cancel()`.
Cancelling marks the task complete with a sentinel error, so any pending or
later `await` returns with that error. Use `task.cancelled` to check the state
non-destructively.

```gb
let job = async.run(func(): int {
    await async.sleep(1000);
    return 1;
});

job.cancel();
io.println(job.cancelled);   # true
```

## Structured Concurrency (`async.scope`)

`async.run` starts a task and hands you a handle; awaiting it is then your job,
and the task keeps running on its own goroutine even if the function that
started it has already returned. That flexibility is useful, but when you fan
work out it leaves three questions open: who waits for all of it, what happens
if one task fails, and what stops a task from outliving the work it was meant
for?

`async.scope(body)` answers all three. It runs `body` with a task group and
guarantees that every task spawned inside is awaited before the scope returns
(nothing leaks past it), and that if the body or any child task throws, the
remaining children are cancelled and the first error is rethrown. The tasks
belong to the block:

```gb
import async;
import async.scope as ascope;

let users = ascope.scope(func(any group): list<any> {
    let a = group.spawn(func(): any { return fetchUser(1); });
    let b = group.spawn(func(): any { return fetchUser(2); });
    return [async.await(a), async.await(b)];
});
# Both fetches have finished here. If either threw, the other was cancelled
# and the error propagated out of scope() - no half-finished state, nothing
# still running in the background.
```

Because the first failure is rethrown out of `scope()`, you handle it with an
ordinary `try`/`catch` around the call - no per-task error plumbing:

```gb
try {
    let users = ascope.scope(func(any group): list<any> {
        let a = group.spawn(func(): any { return fetchUser(1); });
        let b = group.spawn(func(): any { return fetchUser(2); });
        return [async.await(a), async.await(b)];
    });
    render(users);
} catch (RuntimeError e) {
    # Reached as soon as either fetch fails; the other task has already been
    # cancelled. e carries the first error that occurred.
    io.println("fetch failed: " + e.message);
}
```

`group.spawn(fn)` starts a task (the same `Task` you get from `async.run`) and
`group.cancel()` signals every child. Cancellation is cooperative: a task that
never checks its cancel signal runs to completion.

### Which one do I use?

- Reach for `async.scope` when a set of parallel tasks must finish together as a
  unit and a failure in one should stop the rest: parallel fetches in a request
  handler, concurrent file reads, assembling a batch of results where any single
  failure should abort the others. The scope makes sure they all complete (or all
  stop) before you continue, and that none keep running afterward.
- Reach for plain `async.run` (with `async.await` / `async.all`) for tasks you
  manage individually, or for genuine fire-and-forget background work meant to
  outlive the current call (warming a cache, emitting a metric).

To make the request-handler case concrete: say a page needs the current user,
their recent orders, and some recommendations. In PHP you would fetch the three
one after another, each call blocking, so the handler takes as long as the three
added together. Fetching them in parallel makes the handler as slow as the
single slowest call instead - but you still cannot render the page until all
three have arrived, and if any one fails there is no page to build, so the other
two are wasted work. "Execute as a unit" is exactly that: `async.scope` starts
the three together, hands you every result once they are all in, and cancels the
rest the moment one fails.

Coming from PHP, this is worth pinning down. A handler passed directly to
`http.serve`, `http.listen`, or `net.serve` gets an isolated copy of its
captured state for each request. Router and framework applications instead
share their application graph so services and connection pools persist; shared
mutable values must use `store.Store` or another concurrency-safe handle. PHP
discards the whole process state after every request, but a Geblang server is
long-lived, so a task you spawn keeps running on its goroutine until it
finishes. `async.scope` gives a block of parallel work the tidy "everything is
done when this returns" guarantee: wait for the tasks, stop them all if one
fails, and never let them outlive the scope.

## High-Level Task Helpers (`async.tasks`)

`async.tasks` is a thin layer over the async core for callers who would rather
hand off a function and some data than wire up tasks, channels, and semaphores
by hand. Import it with an alias:

```gb
import async.tasks as task;
```

- `task.map(items, fn, opts?)` runs `fn` over each item concurrently and returns
  the results in input order. `opts.concurrency` (an int) caps how many run at
  once; omit it for unbounded parallelism. It fails fast: the first error is
  rethrown and the remaining tasks are cancelled.
- `task.forEach(items, fn, opts?)` is `map` for side effects; it returns null.
- `task.retry(fn, opts?)` calls `fn`, retrying on error with exponential
  backoff. `opts`: `attempts` (default 3), `delayMs` (0), `factor` (2.0),
  `maxDelayMs` (0 = uncapped), `jitter` (false), and `retryIf(error): bool` to
  decide whether a given error is retryable.
- `task.settle(tasks)` awaits every task without failing fast, returning one
  dict per task: `{"ok": true, "value": ...}` or `{"ok": false, "error": ...}`.
- `task.any(tasks)` returns the result of the first task, in list order, that
  succeeds, and throws if every task fails.
- `task.parallel(fns)` runs a list or dict of zero-argument callables at once; a
  list returns results in order, a dict returns a result dict with the same keys.

```gb
import async.tasks as task;

let bodies = task.map(urls, func(string u): string { return fetch(u); }, {"concurrency": 8});

let user = task.retry(func(): any { return loadUser(id); }, {"attempts": 5, "delayMs": 100});

let data = task.parallel({"user": func(): any { return loadUser(1); },
                          "orders": func(): any { return loadOrders(1); }});
```

### Capturing the loop variable

A closure that captures a `for` loop variable directly shares one binding, so
spawning tasks in a loop and reading the loop variable inside them is a race:
the tasks may see the wrong value - the last one, or a racy mix - rather than
each its own:

```gb
for (i in 1..3) {
    async.run(func(): int { return i; });   # tasks may see the wrong value
}
```

Bind a fresh variable per iteration instead:

```gb
for (i in 1..3) {
    let n = i;
    async.run(func(): int { return n; });   # each task sees its own value
}
```

The `async.tasks` helpers handle this for you; the note matters only when you
spawn tasks in a loop by hand.

## Doing Work While A Task Runs

A common mistake is to start a task and immediately await it. That is valid, but
it gives the program no opportunity to do useful work concurrently.

This version blocks straight away:

```gb
let result = await fetchReport();
io.println(result);
```

This version starts the work, continues with local work, then waits only when
the result is actually needed:

```gb
async func fetchReport(): string {
    await async.sleep(100);
    return "report ready";
}

let reportTask = fetchReport();

io.println("started report");
io.println("loading local config");
io.println("rendering progress indicator");

let report = await reportTask;
io.println(report);
```

For polling-style workflows, use `async.done`:

```gb
import async;
import io;

async func background(): string {
    await async.sleep(150);
    return "background result";
}

let task = background();

while (!async.done(task)) {
    io.println("tick");
    await async.sleep(25);
}

io.println(async.await(task));
```

The important rule is that a task value is just a value. Store it in a variable,
put it in a list, pass it to another function, or await it later.

```gb
func awaitAll(list<Task<string>> tasks): list<string> {
    list<string> results = [];
    for (task in tasks) {
        results.push(await task);
    }
    return results;
}
```

## Concurrent I/O Shape

Async is most useful around I/O, timers, HTTP calls, sockets, and work that can
run independently. Prefer the async stdlib wrappers for I/O instead of writing
your own `async func` around synchronous calls:

```gb
import async.io as aio;
import io;

let configTask = aio.readText("config/app.json");
let templateTask = aio.readText("templates/page.html");

io.println("both reads have started");

let config = await configTask;
let template = await templateTask;
```

If the second operation depends on the first result, await between them:

```gb
let config = await readConfig();
let template = await readTemplateFor(config);
```

That sequential form is clearer and avoids pretending dependent work is
parallel.

Async modules currently include:

- `async.io`: file and handle reads/writes.
- `async.http`: HTTP client calls.
- `async.net`: TCP/UDP socket operations.
- `async.stream`: JSON, YAML, XML, and CSV streaming parser work.

These APIs return `Task` values. Their current implementation runs host I/O work
outside the caller's flow; the roadmap target is a shared event-loop scheduler
that can power higher-throughput networking engines without changing these
public module APIs.

Example socket workflow:

```gb
import async;
import async.net as anet;
import bytes;
import net;
import io;

let listener = await anet.listenTcp("127.0.0.1:0");
let address = net.localAddr(listener);

let server = async.run(func(): string {
    let conn = await anet.accept(listener);
    let message = await anet.read(conn, 4);
    await anet.write(conn, "pong");
    await anet.close(conn);
    return bytes.toString(message);
});

let client = await anet.connectTcp(address);
await anet.write(client, "ping");
io.println(bytes.toString(await anet.read(client, 4)));
io.println(await server);
```

## Error Handling In Tasks

Errors thrown inside an async task are re-raised when the task is awaited:

```gb
async func loadRequired(string path): string {
    if (!io.exists(path)) {
        throw IOError("missing file: " + path);
    }
    return io.readText(path);
}

let task = loadRequired("settings.json");

try {
    io.println(await task);
} catch (IOError e) {
    io.println("could not load settings: " + e.message);
}
```

Use this pattern to start work early, then handle the failure at the point where
the result becomes necessary.

## Generators

Generator functions use `yield` and return lazy `generator` values:

```gb
func numbers(): generator<int> {
    yield 1;
    yield 2;
    yield 3;
}

for (n in numbers()) {
    io.println(n);
}
```

Function literals can be generators:

```gb
let upTo = func(int max): generator<int> {
    for (let int i = 1; i <= max; i++) {
        yield i;
    }
};
```

### Manual stepping

Generators can be stepped by hand instead of (or mixed with) `for-in`
(1.19.0). `next()` advances and returns the next value, or `null` once
the generator is exhausted; `done()` reports exhaustion without
consuming a value (it peeks ahead, and the peeked value is returned by
the following `next()`); `close()` ends iteration early and releases
the producer:

```gb
let g = numbers();
while (!g.done()) {
    io.println(g.next());
}

let h = upTo(10);
let first = h.next();     # take one by hand...
for (n in h) {            # ...then hand the rest to for-in
    io.println(n);
}
h.close();
```

For generators of nullable elements, prefer `done()` over checking
`next()` for `null`.

## Iterable Parameters

Use `iterable<T>` when an API accepts generator-like inputs:

```gb
func sum(iterable<int> values): int {
    int total = 0;
    for (n in values) {
        total += n;
    }
    return total;
}
```

Generators are lazy. Breaking out of a `for-in` loop closes the producer so
unbounded streams do not continue running.

```gb
func lines(list<string> paths): generator<string> {
    for (path in paths) {
        for (line in io.readText(path).split("\n")) {
            yield line;
        }
    }
}

for (line in lines(["a.log", "b.log"])) {
    if (line.contains("ERROR")) {
        io.println(line);
        break;
    }
}
```

Generators and async solve different problems. Generators avoid building a full
collection before iteration. Async tasks allow other work to continue while a
result is pending. For a large remote dataset, an API should usually combine
both ideas: fetch or read chunks asynchronously, then yield records lazily.

## Channels (1.7.0)

Typed message-passing between tasks. `import async.channel`
exposes the `Channel<T>` class.

```gb
import async;
import async.channel as ch;
import io;

let c = ch.Channel<int>(0);   # 0 = unbuffered handoff
async.run(func(): void {
    for (let int i = 0; i < 5; i++) { c.send(i); }
    c.close();
});
for (v in c) {
    io.println(v);
}
```

Constructor: `Channel<T>(buffer = 0)`. With `buffer = 0` send blocks
until a receiver is ready (synchronous handoff); a positive buffer
lets up to that many values sit pending before sends block.

| Method | Returns | Description |
|--------|---------|-------------|
| `send(value)` | `void` | Sends value; blocks if the buffer is full. Throws on send-after-close. |
| `recv()` | `T?` | Receives the next value; blocks until one is available. Returns `null` once the channel is closed and drained. |
| `trySend(value)` | `bool` | Non-blocking send. Returns `true` if completed, `false` if it would have blocked. Throws on send-after-close. |
| `tryRecv()` | `T?` | Non-blocking receive. Returns the next value or `null` if none is immediately available. Pair with `isClosed()` to distinguish "no value pending" from "closed and drained". |
| `close()` | `void` | Halts further sends and lets recvs drain to the end. Double-close throws. |
| `isClosed()` | `bool` | Reports closed state. Cheap atomic load. |

Channels implement the iterator protocol, so `for (x in channel)`
blocks on each recv and exits when the channel is closed and
drained. This is the idiomatic consumer pattern:

```gb
for (msg in incoming) {
    handle(msg);
}
```

### Typical patterns

**Producer / consumer**: producer task closes the channel when
done; consumer's `for-in` exits naturally.

**Fan-in to one consumer**: many producers share one channel;
close it from a coordinator once a WaitGroup observes the
producers are done.

```gb
import async;
import async.channel as ch;
import async.sync as sync;

let c = ch.Channel<int>(16);
let wg = sync.WaitGroup();
for (let int p = 0; p < 5; p++) {
    wg.add(1);
    async.run(func(): void {
        produceInto(c);
        wg.done();
    });
}
async.run(func(): void { wg.wait(); c.close(); });

for (v in c) { consume(v); }
```

**Non-blocking poll**: use `tryRecv()` when you want to check for
work without blocking. Pair with `isClosed()` to decide whether to
keep polling.

### Sentinel caveat

`recv()` returns `null` on a closed-drained channel. If you send
real `null` values, consumers cannot distinguish them from the
end-of-channel signal. Either avoid sending null or wrap your
values in a small dict / class.

### `select` statement (1.7.0)

`select` waits on multiple channel operations at once and runs
the case whose op becomes ready first. Cases are recv-with-
binding, recv-discard, send, plus an optional `default`.

```gb
select {
    case let v = c1.recv(): {
        handleC1(v);
    }
    case let s = c2.recv(): {
        handleC2(s);
    }
    case c3.send(payload): {
        sentToC3();
    }
    default: {
        nothingReady();
    }
}
```

The case head must be `c.recv()` (with or without a `let` binding)
or `c.send(value)`. The binding is scoped to the case body and
holds the received value (or `null` if the channel was
closed-drained at the moment of dispatch).

`default` makes the select opportunistic: if no other case can
fire immediately, the default body runs. Without `default`,
select blocks until at least one case is ready.

When several cases are simultaneously ready, the chosen one is
pseudo-random (Go's `reflect.Select` semantics) so producers and
consumers cannot starve each other through ordering.

## Synchronisation Primitives (1.6.0)

Tasks spawned via `async.run` execute as real goroutines, so when
multiple tasks coordinate over shared state the standard
synchronisation primitives apply. They live in two sub-modules
that wrap Go's `sync` and `sync/atomic` packages.

### `async.sync`: locks, semaphores, wait groups

```gb
import async.sync as sync;

let m = sync.Mutex();
m.lock();
try {
    # protected section
} finally {
    m.unlock();
}
```

| Class | Methods | Description |
|-------|---------|-------------|
| `Mutex` | `lock`, `unlock`, `tryLock` | Mutual-exclusion lock. `tryLock()` returns `false` instead of blocking when the lock is held. |
| `RWMutex` | `lock`, `unlock`, `tryLock`, `rLock`, `rUnlock`, `tryRLock` | Reader-writer lock. Many readers may coexist; writers are exclusive. |
| `Semaphore` | `acquire`, `release`, `tryAcquire` | Counted concurrency guard. Constructor: `Semaphore(permits)`; permits must be at least 1. |
| `WaitGroup` | `add(delta)`, `done`, `wait` | Counter for "this many tasks are running"; `wait()` blocks until the counter hits zero. |

### `async.atomic`: lock-free cells

```gb
import async.atomic as atomic;

let counter = atomic.AtomicInt(0);
counter.add(1);
if (counter.compareAndSwap(1, 42)) {
    /* succeeded */
}
```

| Class | Methods | Description |
|-------|---------|-------------|
| `AtomicInt` | `load`, `store(v)`, `add(delta)`, `compareAndSwap(old, new)` | Sequentially consistent int64. `add` returns the new value; `delta` may be negative. |
| `AtomicBool` | `load`, `store(v)`, `compareAndSwap(old, new)` | Sequentially consistent bool. Common for one-shot flags. |

### Typical patterns

**Counting completed tasks**:

```gb
import async;
import async.sync as sync;

let wg = sync.WaitGroup();
for (let int i = 0; i < n; i++) {
    wg.add(1);
    async.run(func(): void {
        try { doWork(i); } finally { wg.done(); }
    });
}
wg.wait();
```

**Bounded concurrency**:

```gb
import async;
import async.sync as sync;

let throttle = sync.Semaphore(10);   # at most 10 in flight
for (url in urls) {
    async.run(func(): void {
        throttle.acquire();
        try { fetch(url); } finally { throttle.release(); }
    });
}
```

**Lock-free counter**:

```gb
import async.atomic as atomic;
import io;
let hits = atomic.AtomicInt(0);
# in many tasks:
hits.add(1);
# later:
io.println(hits.load());
```

### Sharing mutable state safely

Because tasks run as real goroutines, two tasks (or a task and the main
flow) that touch the **same mutable value** at the same time are a data
race - exactly as in any threaded language. A class instance, list, dict,
or set has no built-in locking, so concurrent writes (or a write racing a
read) have undefined behaviour and can abort the program.

```gb
# WRONG: many tasks mutating one shared object with no synchronisation.
class Counter { int n; func bump(): void { this.n = this.n + 1; } }
let c = Counter();
for (let int i = 0; i < 100; i++) {
    async.run(func(): void { c.bump(); });   # data race on c.n
}
```

Make shared access explicit with the tools above:

- **A lock** around the critical section:

  ```gb
  import async.sync as sync;
  import async;
  let m = sync.Mutex();
  async.run(func(): void {
      m.lock();
      try { c.bump(); } finally { m.unlock(); }
  });
  ```

- **An atomic** cell for a single counter or flag (`atomic.AtomicInt`).
- **A channel** to hand values to one owning task instead of sharing the
  object (`async.channel`).
- **A `store.Store`** for shared mutable key-value state. It is a thread-safe
  map that serialises every access and deep-copies values in and out, with
  atomic `incr`, `getOrSet`, `compareAndSet`, and `update(key, fn)`. Reach for
  it instead of guarding a plain dict by hand:

  ```gb
  import store;
  import async;
  let s = store.Store();
  async.run(func(): void { s.incr("done"); });   # atomic, no lost updates
  ```

The concurrency primitives themselves - `async.channel`, `async.sync`,
and `async.atomic` - are safe to share across tasks; that is their
purpose. The rule applies to ordinary objects you create.

If a task only needs its own copy, pass the data in as an argument rather
than closing over a shared variable, so each task works on independent
state.

The same rule covers whole-object operations: serialising
(`json.stringify(obj)`), reflecting over, or otherwise reading all of an
object's fields while another task mutates it is a read-while-write race.
Take the lock (or hand the object to a single owning task) around those
reads too, not just around individual field writes.
