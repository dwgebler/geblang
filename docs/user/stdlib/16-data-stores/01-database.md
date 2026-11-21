# Database

Import `db` for SQL connections, queries, transactions, prepared statements,
pool configuration, stats, and migrations.

Supported database drivers:

| Driver | Geblang name | Notes |
|--------|--------------|-------|
| SQLite | `"sqlite"` | File-backed or in-memory SQLite databases |
| PostgreSQL | `"postgres"` | PostgreSQL server databases |
| MySQL / MariaDB | `"mysql"` | MySQL-compatible server databases |

Use the class API (`db.Connection`) for application code. It is the portable DB
wrapper: use `?` positional placeholders or `:name` named placeholders across
SQLite, PostgreSQL, MySQL, and MariaDB. Geblang rewrites the query for the
selected driver and binds parameters in the correct order.

The low-level functional API (`db.open`, `db.query`, `db.exec`, and friends)
remains available for adapter code that wants raw handles. It is intentionally
closer to the underlying driver and should not be the default for application
code.

## Connecting

`db.Connection(driver, connectionString)` opens a connection pool using a driver
name and connection string:

```gb
let sqlite = db.Connection("sqlite", "/tmp/app.sqlite");
let memory = db.Connection("sqlite", ":memory:");

let pgUrl = db.Connection(
    "postgres",
    "postgres://app:secret@localhost:5432/app?sslmode=disable"
);

let pgKv = db.Connection(
    "postgres",
    "host=localhost port=5432 user=app password=secret dbname=app sslmode=disable"
);

let mysql = db.Connection(
    "mysql",
    "app:secret@tcp(127.0.0.1:3306)/app?parseTime=true"
);

let mysqlSocket = db.Connection(
    "mysql",
    "app:secret@unix(/var/run/mysqld/mysqld.sock)/app?parseTime=true"
);
```

The options-dict form accepts pool tuning alongside the driver and DSN, applied
at connect time: `maxOpenConns`, `maxIdleConns`, `connMaxLifetimeMs`,
`connMaxIdleTimeMs`. Setting `maxOpenConns` without `maxIdleConns` keeps the
idle pool the same size; with no pool options the idle pool defaults to 8
connections. Size `maxOpenConns` to what the database can serve, not to the
number of concurrent callers; see
[Pool sizing and concurrency](#pool-sizing-and-concurrency):

```gb
let pool = db.Connection({
    "driver": "postgres",
    "dsn": pgUrl,
    "maxOpenConns": 16,
});
```

Parameterized queries use native prepared statements on all three drivers:
PostgreSQL binds parameters over the extended protocol (pgx caches the
server-side statements), MySQL uses the binary protocol's prepare/execute
pair, and SQLite binds through its prepared-statement C API. Parameter
values are never interpolated into SQL text.

A file-backed SQLite database gets server-friendly defaults on every pooled
connection: WAL journal mode, `synchronous(NORMAL)`, and a five-second
`busy_timeout` (so concurrent access waits instead of failing with
`database is locked`). `:memory:` databases, and any DSN that already carries a
`_pragma` or `busy_timeout` parameter, keep their own settings.

For a file-backed SQLite database, the options-dict form accepts tuning keys
that map to per-connection pragmas applied at connect time:

| Option | Pragma | Purpose |
|--------|--------|---------|
| `wal: true` | `journal_mode(WAL)` | Write-ahead logging: concurrent readers with one writer. |
| `synchronous: "NORMAL"` | `synchronous(NORMAL)` | Sync level (`OFF` / `NORMAL` / `FULL` / `EXTRA`); `NORMAL` is the safe, fast pairing with WAL. |
| `foreignKeys: true` | `foreign_keys(ON)` | Enforce foreign-key constraints. |
| `busyTimeoutMs: N` | `busy_timeout(N)` | Override the default five-second busy timeout. |
| `cacheSizeKb: N` | `cache_size(-N)` | Page-cache size in KiB. |
| `mmapSizeMb: N` | `mmap_size(N MiB)` | Memory-mapped I/O size. |
| `tempStoreMemory: true` | `temp_store(MEMORY)` | Keep temp tables and indices in memory. |
| `txLock: "immediate"` | `BEGIN IMMEDIATE` | How `begin()` starts a transaction: `"deferred"` (SQLite's default), `"immediate"` or `"exclusive"`. |

`txLock: "immediate"` makes each transaction take the write lock when it
begins, waiting up to the busy timeout for another writer to finish. With the
default deferred mode a transaction that reads and then writes takes the lock
only at its first write; under WAL, if another connection committed in
between, that write fails at once with `database is locked` and the busy
timeout does not help. Use `"immediate"` for read-then-write transactions when
more than one connection or process writes to the database.

A file database already uses WAL and `synchronous(NORMAL)` by default; these
options tune or override that (for example `wal: false`, or
`synchronous: "FULL"`). They are ignored for `:memory:` databases (the pragmas
only matter for a file). Run `connection.optimize()`
(`PRAGMA optimize`) periodically, or before closing a long-lived connection, to
let SQLite refresh its query-planner statistics.

```gb
let conn = db.Connection({
    "driver": "sqlite",
    "dsn": "/var/lib/app/app.sqlite",
    "wal": true,
    "synchronous": "NORMAL",
    "foreignKeys": true,
});
conn.optimize();
```

A private in-memory database (`:memory:`) is distinct per connection, so its
pool is pinned to a single connection; concurrent access shares one database
rather than each connection seeing a separate empty one. Use a shared cache
(`file::memory:?cache=shared`) or a file path if you need a larger pool.

The repository includes runnable examples:

- `examples/sqlite.gb` uses a local SQLite file and runs without external
  services.
- `examples/database_objects.gb` demonstrates the class API, option-dictionary
  connections, named placeholders, positional-list binding, transactions,
  prepared statements, streaming rows, and pool stats with SQLite.
- `examples/postgres_db.gb` reads `GEBLANG_POSTGRES_DSN` and skips cleanly when
  it is not set. Example DSN:
  `postgres://app:secret@localhost:5432/app?sslmode=disable`.
- `examples/mysql_db.gb` reads `GEBLANG_MYSQL_DSN` and skips cleanly when it is
  not set. Example DSN:
  `app:secret@tcp(127.0.0.1:3306)/app?parseTime=true`.

You can also pass an options dictionary and let Geblang build the connection
string:

```gb
let sqlite = db.Connection({
    "driver": "sqlite",
    "path": "/tmp/app.sqlite",
});

let postgres = db.Connection({
    "driver": "postgres",
    "host": "localhost",
    "port": 5432,
    "database": "app",
    "user": "app",
    "password": "secret",
    "sslmode": "disable",
});

let mysql = db.Connection({
    "driver": "mysql",
    "host": "127.0.0.1",
    "port": 3306,
    "database": "app",
    "user": "app",
    "password": "secret",
    "parseTime": true,
});
```

SQLite options support `path`, `file`, `database`, `dbname`, or `"memory": true`.
PostgreSQL options support `host`, `port`, `database`/`dbname`, `user`,
`password`, and `sslmode`. MySQL/MariaDB options support `host`, `port`,
`database`/`dbname`, `user`, `password`, `socket`, `parseTime`, `charset`, and
`loc`.

## Query Parameters

The class API accepts varargs, a positional list, or a named dictionary:

```gb
conn.exec("insert into users (name, email) values (?, ?)", "Ada", "ada@example.com");

conn.exec(
    "insert into users (name, email) values (?, ?)",
    ["Grace", "grace@example.com"]
);

conn.exec(
    "insert into users (name, email) values (:name, :email)",
    {"name": "Linus", "email": "linus@example.com"}
);

let rows = conn.query(
    "select id, email from users where name = :name",
    {"name": "Ada"}
);
```

Prepared statements use the same binding rules:

```gb
let stmt = conn.prepare("select id from users where email = :email");
let rows = stmt.query({"email": "ada@example.com"});
defer rows.close();
stmt.close();
```

Named placeholders are written as `:name`, where `name` starts with a letter or
underscore and then contains letters, digits, or underscores. Placeholders inside
quoted strings or SQL comments are ignored by the binder.

## `db.Connection`

`db.Connection(driver, connectionString)` or `db.Connection(options)` opens a
SQL connection pool and returns a connection object.

| Method | Returns | Description |
|--------|---------|-------------|
| `exec(sql, args...)` | `dict` | Execute a statement; returns `rowsAffected` and `lastInsertId` |
| `query(sql, args...)` | `db.Rows` | Run a query and return a streaming cursor |
| `queryOne(sql, args...)` | `dict|null` | Run a query and return its first row, or `null`; the cursor is closed before it returns (1.44.0) |
| `driver()` | `string` | Normalized driver name: `sqlite`, `postgres` or `mysql`; no credentials or connection URL |
| `begin(timeoutMs?, opts?)` | `db.Transaction` | Start a transaction with an optional lifetime deadline; a `null` timeout means none, so `begin(opts: {...})` sets only the isolation level |
| `execWithTimeout(timeoutMs, sql, args...)` | `dict` | Execute with a deadline, including pool wait |
| `queryWithTimeout(timeoutMs, sql, args...)` | `db.Rows` | Query with a deadline, including pool wait and streaming |
| `prepare(sql)` | `db.Statement` | Prepare a reusable statement |
| `configure(options)` | `void` | Set pool options such as `maxOpenConns`, `maxIdleConns`, `connMaxLifetimeMs`, `connMaxIdleTimeMs` |
| `stats()` | `dict` | Return pool statistics |
| `optimize()` | `void` | Run `PRAGMA optimize` (SQLite maintenance) |
| `migrate(migrations)` | `dict` | Apply idempotent migrations |
| `close()` | `void` | Close the connection pool |

```gb
import io;
import db;

let conn = db.Connection({
    "driver": "sqlite",
    "path": "/tmp/app.sqlite",
});
defer conn.close();

conn.exec("create table if not exists users (id text, name text)");
conn.exec(
    "insert into users (id, name) values (:id, :name)",
    {"id": "1", "name": "Ada"}
);

let rows = conn.query("select name from users where id = :id", {"id": "1"});
defer rows.close();

while (rows.next()) {
    let row = rows.row();
    io.println(row["name"]);
}
```

### Deadlines and pool contention

Timeouts are positive integer milliseconds. Existing `exec`, `query`, and
`begin()` calls retain their existing behavior. Timed operations use the SQL
driver's context cancellation and do not automatically inherit HTTP request
cancellation. Pool acquisition waits stop at the deadline; an executing driver
may take longer to return. In particular, SQLite lock waits can outlast the
context deadline until `busyTimeoutMs` expires or the lock is released. Set an
appropriate SQLite `busyTimeoutMs` as well as an operation timeout.

`begin(250)` applies one deadline to acquiring a pooled connection and the
entire transaction. Expiry automatically rolls back the transaction and later
`commit()` fails. It is not an acquisition-only timeout. Choose a budget that
covers all transaction work and always commit or roll back promptly.

`queryWithTimeout(250, sql, args...)` keeps its deadline through cursor reading.
Close the returned `Rows` explicitly or exhaust it with `all()`/iteration to
release resources promptly. Partial reads can fail when the deadline expires.

`stats()` exposes `waitCount` and `waitDurationMs`: cumulative pool acquisition
waits and their total duration since pool creation. Compare deltas between
samples to observe contention. They include cancelled waits and do not measure
SQL execution time or database lock waits. `inUse`, `idle`, and
`maxOpenConnections` describe the pool at the sample time.

## Pool sizing and concurrency

A `db.Connection` is a pool, and it is safe to share between tasks, threads and
server handlers. Open one per database per process at startup and pass it to
the code that needs it. Opening a connection per request or per task pays the
connect and authentication cost every time and defeats the pool.

### What holds a connection

Each operation borrows a connection from the pool and returns it when it is
done. The pool runs out when connections are held longer than the work needs,
so it helps to know when each one is returned:

| Operation | Connection returned |
|-----------|---------------------|
| `exec`, `execWithTimeout` | When the statement finishes |
| `queryOne` | Before it returns |
| `query(...).all()`, or `first()` / `get(i)` / `isEmpty()` on an unstepped cursor | Before it returns |
| `query(...)` stepped with `next()` or `for-in` | When the cursor is exhausted or closed |
| `begin()` | When the transaction commits or rolls back (or its deadline expires) |
| `prepare()` | Per execution; the statement itself holds no connection between calls |

Two patterns hold more than one connection at once. A loop over a streaming
cursor that runs another query per row holds the cursor's connection and
borrows a second for each inner query. A query made through the pool while
the same task has a transaction open also uses a second connection, and it does
not see the transaction's uncommitted writes. With `maxOpenConns` set to 1 an
untimed inner query in either pattern waits forever, because the only
connection is held by the outer operation. Read the outer result with `all()`
first, or run the inner queries on the transaction.

### Defaults

| Setting | Default |
|---------|---------|
| `maxOpenConns` | Unlimited |
| `maxIdleConns` | 8, or equal to `maxOpenConns` when only that is set |
| `connMaxLifetimeMs` | None: connections are reused until they fail |
| `connMaxIdleTimeMs` | None |

An unlimited pool opens a new connection whenever every existing one is busy.
Under a traffic spike that can mean hundreds of connections, which exhausts the
server's connection limit or slows every query down. Production code should
always set `maxOpenConns`.

### Choosing `maxOpenConns`

The pool limit protects the database, so size it from the database side:

1. Start from the server's connection limit (PostgreSQL's `max_connections`
   defaults to 100; MySQL's `max_connections` to 151) minus connections reserved
   for administration, migrations and other clients.
2. Divide by the number of processes that each open their own pool: every app
   instance, worker process and scheduled job runner counts. Two app replicas
   and two worker processes with `maxOpenConns: 20` can open 80 connections.
3. Prefer a smaller pool. A database executes only as many queries in parallel
   as it has cores and disk bandwidth for; connections beyond that queue inside
   the database instead of in the pool, with more memory per connection. A few
   connections per database core is a common starting point.
4. Measure under load and adjust (see [Reading contention](#reading-contention)).

Leave `maxIdleConns` unset so it matches `maxOpenConns`. A smaller idle pool
closes connections after every burst and reopens them on the next. Set it lower
only when idle connections cost something, such as a per-connection licence or
a shared server with a tight connection limit.

Set `connMaxLifetimeMs` (for example 30 minutes) when connections pass through
a load balancer, proxy or failover endpoint that drops long-lived connections,
or when credentials rotate. `connMaxIdleTimeMs` closes connections that stay
unused for that long, which shrinks the pool after a spike.

```gb
let conn = db.Connection({
    "driver": "postgres",
    "dsn": dsn,
    "maxOpenConns": 16,
    "connMaxLifetimeMs": 1800000,
    "connMaxIdleTimeMs": 300000,
});
```

### When callers outnumber connections

A caller that finds every connection busy waits in the pool until one is
returned. An untimed operation waits as long as it takes. A timed operation
(`execWithTimeout`, `queryWithTimeout`, `begin(timeoutMs)`) counts the wait
against its deadline and throws `TimeoutError` when the deadline passes:

```gb
try {
    let rows = conn.queryWithTimeout(250, "select * from orders where id = ?", id).all();
} catch (TimeoutError e) {
    # pool or database too busy: fail this request instead of piling up
}
```

A server that accepts more concurrent requests than the pool has connections is
normal: requests that do not touch the database, and requests between queries,
need no connection. Bound the server's concurrency as well, so that a spike
cannot queue an unbounded number of requests behind the pool. The HTTP server's
`maxConcurrent`, `queueSize` and `onOverload` options do this
([Concurrency limits](../14-http-net.md#concurrency-limits)). A practical
arrangement is:

- `maxOpenConns` sized for the database, as above.
- Server `maxConcurrent` sized for memory, usually several times
  `maxOpenConns`.
- Deadlines on database calls shorter than the client's own timeout, so a
  request that cannot get a connection fails quickly with a clear error.

Keep connection hold times short: use `queryOne` for single rows, close
cursors on early exits, keep transactions to the statements that must be
atomic, and make network calls (HTTP, mail, message brokers) before the
transaction starts or after it ends.

### Reading contention

Sample `conn.stats()` periodically and compare `waitCount` and
`waitDurationMs` between samples:

| Observation | Meaning |
|-------------|---------|
| Waits rise, `inUse` equals `maxOpenConnections`, database has spare CPU | Pool too small, or connections held too long |
| Waits rise and the database is saturated | Pool too large for the database; lower it and bound concurrency upstream |
| `openConnections` swings up and down with `maxIdleClosed` rising | Idle pool smaller than the working set |
| No waits | The pool is not the bottleneck |

### SQLite

SQLite runs in-process, so there is no server connection limit, but it allows
only one writer at a time. A larger pool lets more reads run in parallel; it
does not speed up writes. Writers queue on the database lock for up to
`busyTimeoutMs`, not in the pool, so set `busyTimeoutMs` together with your
operation deadlines and use `txLock: "immediate"` for transactions that read
and then write. A private `:memory:` database always uses a single connection.

## Transactions And Statements

`db.Transaction` has `exec`, `query`, `commit`, and `rollback`. Transaction
queries use the same portable placeholder rules as `Connection`.

```gb
let tx = conn.begin();
try {
    tx.exec(
        "insert into users (id, name) values (:id, :name)",
        {"id": "2", "name": "Grace"}
    );
    tx.commit();
} catch (Error e) {
    tx.rollback();
    throw e;
}
```

`db.Statement` has `exec`, `query`, and `close`. A statement remembers the named
placeholder order from `prepare`, so later calls can pass a dictionary.

```gb
let stmt = conn.prepare("select name from users where id = :id");
let result = stmt.query({"id": "2"});
defer result.close();
stmt.close();
```

## Rows

`Connection.query`, `Transaction.query`, and `Statement.query` return `db.Rows`.
Rows are streaming cursors: a `next()`/`row()` loop (or a `for-in` loop) holds
one row at a time regardless of result-set size, so scanning millions of rows
stays at constant memory on every driver (SQLite, PostgreSQL, MySQL).
Exhausting a cursor closes the native SQL rows automatically, but
`defer rows.close()` is still the normal pattern when a function may return
early.

Cursors are iterable directly (1.19.0):

```gb
for (row in conn.query("select id, name from users")) {
    io.println(row["name"]);
}
```

The random-access methods (`all()`, `first()`, `get(i)`, `length()`,
`isEmpty()`, `toList()`) start caching rows from the moment they are first
called. Mixing styles is allowed and has remaining-rows semantics: rows
already consumed by `next()` before the first random-access call are not
replayed - `rows.next(); rows.all()` returns everything after the first row.

On a cursor that has not been stepped with `next()` or `for-in`, `first()`,
`get(i)`, and `isEmpty()` read the whole result into the cache, which closes
the cursor and returns its connection to the pool (1.44.0). A single-row lookup
such as `conn.query(sql, id).first()` therefore never holds a connection open.
Use `conn.queryOne(sql, args...)` when only one row is wanted, or add a `LIMIT`,
so a large result is not buffered. Once a cursor is being stepped, these methods
read only as far as they need and the cursor stays open until it is exhausted
or closed.

The functional API (`db.query`, `db.txQuery`, and `db.stmtQuery`) keeps returning
an eager `list<dict>` for code that expects materialized result sets. As of
1.19.0 the functional helpers also accept `Connection`/`Transaction`/`Statement`
objects (not just raw handles) and normalize `?` placeholders per driver,
exactly like the class API.

| Method | Returns | Description |
|--------|---------|-------------|
| `next()` | `bool` | Advance to the next row |
| `row()` | `dict|null` | Current row after `next()`, or `null` before/after iteration |
| `columns()` | `list<string>` | Column names for the result set |
| `close()` | `void` | Close the cursor |
| `all()` | `list<dict>` | Drain the cursor and return the cached and remaining rows |
| `length()` | `int` | Drain the cursor and return the number of rows |
| `isEmpty()` | `bool` | Whether the result set is empty; drains an unstepped cursor |
| `first()` | `dict|null` | First row or `null`; drains an unstepped cursor |
| `get(index)` | `dict|null` | Row by zero-based index; drains an unstepped cursor, otherwise reads until that index |
| `toList()` | `list<dict>` | Alias for `all()` |

```gb
let rows = conn.query("select id, name from users order by id");
defer rows.close();

io.println(rows.columns());

while (rows.next()) {
    let row = rows.row();
    io.println((row["id"] as string) + ": " + row["name"]);
}
```

### Migrations

`conn.migrate(migrations)` takes a list of `{"id": string, "sql": string}`
dicts. It creates a `geblang_migrations` table if needed, runs the SQL of each
migration whose `id` is not yet recorded, and records it. All migrations run in
one transaction, so a failing statement applies none of them. The result lists
the ids it ran and the ids it skipped:

```gb
import io;
import db;

let conn = db.Connection("sqlite", ":memory:");
defer conn.close();

let migrations = [
    {"id": "001-users", "sql": "create table users (id text, name text)"},
    {"id": "002-email", "sql": "alter table users add column email text"},
];
io.println(conn.migrate(migrations));
io.println(conn.migrate(migrations));
```

The first call prints `{"applied": ["001-users", "002-email"], "skipped": []}`;
the second applies nothing and lists both ids as skipped. Keep each `sql` to one
statement, and never change a migration once it has been applied: add a new one.

## Low-Level Functions

Use these when writing adapters or working with raw handles:

| Function | Description |
|----------|-------------|
| `open(driver, connectionString)` | Open a raw database handle |
| `exec(handle, sql, args...)` | Execute SQL using driver-native placeholders |
| `query(handle, sql, args...)` | Return an eager `list<dict>` |
| `begin(handle)` | Start a raw transaction handle |
| `txExec(tx, sql, args...)` | Execute inside a transaction |
| `txQuery(tx, sql, args...)` | Eager transaction query |
| `commit(tx)` / `rollback(tx)` | Finish a transaction |
| `prepare(handle, sql)` | Prepare a raw statement handle |
| `stmtExec(stmt, args...)` | Execute a prepared statement |
| `stmtQuery(stmt, args...)` | Eager prepared-statement query |
| `stmtClose(stmt)` | Close a prepared statement |
| `configure(handle, options)` | Configure pool limits |
| `stats(handle)` | Return pool stats |
| `migrate(handle, migrations)` | Apply idempotent migrations |
| `close(handle)` | Close the database handle |

### Transaction isolation

`conn.begin(5000, {"isolation": "readCommitted"})` sets isolation for that
transaction without changing connection-pool or session defaults. Options accept
`default`, `readCommitted`, `repeatableRead` and `serializable`; the SQL driver
may reject unsupported levels. Existing `begin()` and `begin(timeoutMs)` retain
their behavior. Supply a positive lifetime timeout when passing options.
