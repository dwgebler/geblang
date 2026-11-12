# Durable jobs and workers

`import jobs` provides a SQL queue, bounded workers and persistent UTC schedules.
SQLite, PostgreSQL (`postgres` or `pgx`) and MySQL 8 are supported. The queue uses
short transactions and renewable, token-fenced leases. Server-database control
transactions use READ COMMITTED to avoid InnoDB range-lock deadlocks between
competing consumers; caller-owned enqueueIn transactions keep their own isolation. Delivery is at least once:
make external effects idempotent, for example by recording `job.id` alongside a
business update in one transaction.

## A producer and worker

```gb
import db;
import jobs;
import io;

let conn = db.Connection("sqlite", "./jobs.db");
let queue = jobs.Queue(conn, {"namespace": "reports"});
queue.register("report", func(jobs.Job job): void {
    job.throwIfCancelled();
    io.println(job.payload["account"]);
});
queue.enqueue("report", {"account": "a-123"});
let result = jobs.Worker(queue, {"drainOnce": true}).run();
```

Producers need no handler registration. Consumers register handlers before
starting; registration then closes. Unknown job names fail permanently.
Payloads must be JSON dictionaries; closures and application instances are not
job payloads. The default UTF-8 encoded limit is 1 MiB; `maxPayloadBytes: 0`
explicitly disables it. Names and namespaces have 1 to 128 characters. Table
prefixes use lowercase ASCII letters, digits and underscores, at most 48.

`Queue(conn, opts)` requires `namespace`. Options include `tablePrefix`
(default `geblang_jobs`), `maxPayloadBytes`, `operationTimeoutMs` (5000),
`autoCreate` (true), `onEvent` (optional callable) and `logEvents` (false).
Fresh schemas initialize automatically. `autoCreate: false` verifies an existing
schema, suitable for runtime credentials without DDL privileges. Unsupported
schema versions and incomplete migrations fail startup.

## Publication and transactions

`enqueue(name, payload, opts)` returns an ID. Options are `queue` (default
`default`), `delayMs`, `runAtMs` (UTC epoch milliseconds), and `dedupeKey`.
Choose delay or absolute time, not both. A dedupe key is scoped by namespace
and job name. While equivalent work is active, publication returns its existing
ID without replacing its payload; terminal completion releases the reservation.

`enqueueIn(tx, name, payload, opts)` participates in a caller-owned transaction
created by the exact connection passed to Queue. Commit or rollback the business
change and job together. Another connection, even to the same database, is
rejected. Cross-database outbox relaying is not provided.

`register(name, handler, opts)` accepts `maxAttempts` (5), `backoffMs`
(`[1000, 5000, 30000, 120000, 600000]`), `timeoutMs` (0, disabled), and `jitter`
(false). Backoff may be a list of nonnegative milliseconds or a callable taking
the attempt number. Crash recovery uses the policy and next delay persisted at
claim time. Callable policies must be registered consistently on every worker.

## Worker ownership and shutdown

`Worker(queue, opts)` accepts:

| Option | Default | Meaning |
| --- | --- | --- |
| `queues` | `["high", "default", "low"]` | Strict priority order; other queues remain pending. |
| `names` | `[]` | Optional job-name filter. |
| `maxConcurrency` | 1 | Local per-worker execution limit. |
| `queueConcurrency` | `{}` | Additional local limits per selected queue. |
| `pollIntervalMs` | 1000 | Empty-queue polling interval. |
| `leaseMs` | 60000 | Renewable claim lifetime. |
| `heartbeatMs` | 10000 | Renewal and recovery interval. |
| `shutdownTimeoutMs` | 30000 | Grace period before requesting cancellation. |
| `maxStoreFailures` | 10 | Consecutive failed store cycles tolerated before the worker stops. |
| `maxJobs` | unlimited | Finite admitted-attempt limit. |
| `drainOnce` | false | Exit when no selected work is currently ready. |
| `workerId` | hostname and PID | Diagnostic owner identity. |

Heartbeat plus the database operation timeout must fit inside the lease. Size
the database pool for concurrent handlers and control queries. Limits are local,
not fleet-wide. Strict priorities may starve lower queues.

A failed claim or recovery query does not stop the worker. It emits a
`worker.store_error` event, keeps renewing in-flight claims and retries with a
growing pause (up to 30 seconds). After `maxStoreFailures` consecutive failed
cycles `run()` raises `JobError`. A job whose retry policy cannot be evaluated
(a `backoffMs` callable that throws or returns an invalid delay) fails
permanently with `invalid retry policy` in its last error; other jobs continue.

`run()` joins all owned tasks and returns `WorkerResult` with `drained`,
`started`, `completed`, `failed`, `retry` and `lost` counts. `start()` returns a
`WorkerHandle`; call `requestStop()` to stop admission, then `wait(timeoutMs)`.
`handle.status()` exposes concurrent-safe health and approximate lifecycle counters.
An incomplete wait reports `drained: false`; it does not detach or terminate the
handler. Keep ownership of the handle and wait again.

Handlers receive `Job` with `id`, `name`, `queue`, `payload`, `attempt`,
`createdAtMs`, `scheduledAtMs`, optional `deadlineAtMs`, `scheduleId` and
`occurrenceAtMs`. Check `isCancellationRequested()` or `throwIfCancelled()` in
long work. Timeout is cooperative: the worker retains the slot and renews the
claim until the handler stops, then records a timeout and applies the retry
policy even if the handler returned normally. Cancellation requested only by
shutdown is different: a handler that returns normally is recorded as
completed, and one that throws is retried. For hard deadlines run a supervised
process.
Expired owners cannot renew or finalize a newer attempt, but fencing cannot
undo an external side effect already performed by an old owner.

## Persistent schedules

```gb
let scheduler = jobs.Scheduler(queue);
scheduler.add("daily-report", "0 9 * * *", "report", {"account": "a-123"});
let publisher = scheduler.start();
```

Run publishers independently from workers, including when workers are saturated.
`tick()` atomically records occurrences, publishes jobs, and advances each cursor.
Multiple publishers can share the same schedule identity. Registration is
idempotent for identical definitions; conflicting definitions raise
`ConflictError`. `revision(id)` returns the stored revision or null. `update`
requires `expectedRevision` in its options and re-enables the schedule;
`disable(id)` stops future publication. `inspect(opts)` lists revisions, enabled
state, next due time and `lagMs`, paginated by `afterId` and `limit` (1 to 1000).

Schedules use UTC only. `dayMatch` is `or` by default, or `and` when both day
fields must match. Options include `queue`, `dedupeKey`, `missed` (`skip`,
`latest`, `all`), `overlap` (`allow`, `skip`), `maxAgeMs` (86400000), and
`batchSize` (100). Skip drops older minutes, latest coalesces missed times,
and all catches up within the age and batch bounds. Overlap skip records a
skipped occurrence while an earlier scheduled job remains active. `occurrences(id, {afterMs, limit})` lists queued, coalesced and skipped results;
`skipped_until_ms` marks the exclusive end of a skipped range. Handler
execution stays separate from publication. Keep occurrence history; automatic
occurrence pruning and cursor rewinding are not provided.

## Application entry points

Export `main(list<string> args): int` from an application module and return
`jobs.cli(worker, scheduler, args)` (pass null for no scheduler). Run:

```sh
geblang worker --entry app.worker
geblang worker --entry app.worker --concurrency 4 --queue high --queue default
geblang worker --entry app.worker --once
geblang worker --entry app.worker --max-jobs 10
geblang worker --entry app.worker --scheduler-only
geblang worker --entry app.worker stats --format json
geblang worker --entry app.worker dlq list --limit 100
geblang worker --entry app.worker dlq retry JOB_ID
geblang worker --entry app.worker dlq purge --all
```

The bootstrap supplies the real connection, namespace and registrations; the
command does not infer a database from environment variables. `--queue`
replaces the configured queue list; `queueConcurrency` limits for queues left
unselected are ignored. It uses normal
module resolution and the same entry point can be built as a standalone binary.
SIGINT/SIGTERM stop admission and share one drain deadline across publisher and
worker. Exit codes: 0 clean, 2 invalid configuration/arguments, 1 infrastructure
failure, incomplete drain, or terminal failures in a finite run.

## Inspection, history and migration

`get(id)` returns a nullable `JobSnapshot`. `stats()` returns state counts,
ready/delayed counts and oldest ready age. `failed({afterId, limit})` paginates
failed jobs. `attempts(id, {afterSequence, limit})` returns ordered execution
history. `retry(id)` preserves history and starts a new retry cycle; an active
equivalent dedupe reservation raises `ConflictError`. `purge(id)` removes a
terminal job and its attempts. `prune({beforeMs, limit})` removes bounded batches
of terminal jobs and attempts. History is retained until an operator removes it.

Opt-in JSON lifecycle logging and `onEvent(event)` include namespace, worker,
job identity, attempt sequence, timing and bounded errors, without payloads or
connection URLs. Hooks run after state commits, must be fast and safe for
concurrent invocation, and cannot roll back a job. Hook exceptions are reported
to stderr. Error messages originate in handlers; avoid putting secrets in them.

For a legacy SQL queue, stop producers, workers and schedulers, back up the
database, then call `migrateLegacy(conn, {sourceTable, namespace})` for a read-only
dry run. Inspect `MigrationReport.total`, `invalid` and `errors`. Apply with
`dryRun: false, stopped: true`. The copy preserves IDs, payloads, attempts and
source tables; it can resume interrupted batches. Running jobs become pending
or terminal according to their retry ceiling. Do not mix old and new workers.
`migrationCli(conn, opts, args)` offers JSON reports with `--dry-run` or
`--apply --stopped`, before constructing the application Queue.

See `examples/jobs.gb` and the module bootstrap in `examples/jobs_app/`.
