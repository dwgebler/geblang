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
connections. Size `maxOpenConns` to your expected concurrency - a shared
connection then serves parallel tasks and handlers at full speed:

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
let db = db.Connection({
    "driver": "sqlite",
    "dsn": "/var/lib/app/app.sqlite",
    "wal": true,
    "synchronous": "NORMAL",
    "foreignKeys": true,
});
db.optimize();
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
    "path": "/tmp/app.sqlite"
});

let postgres = db.Connection({
    "driver": "postgres",
    "host": "localhost",
    "port": 5432,
    "database": "app",
    "user": "app",
    "password": "secret",
    "sslmode": "disable"
});

let mysql = db.Connection({
    "driver": "mysql",
    "host": "127.0.0.1",
    "port": 3306,
    "database": "app",
    "user": "app",
    "password": "secret",
    "parseTime": true
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
| `begin()` | `db.Transaction` | Start a transaction |
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
    "path": "/tmp/app.sqlite"
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
| `isEmpty()` | `bool` | Read enough to determine whether the result set is empty |
| `first()` | `dict|null` | First row or `null`; reads and caches it if needed |
| `get(index)` | `dict|null` | Row by zero-based index; drains until that index is available |
| `toList()` | `list<dict>` | Alias for `all()` |

```gb
let rows = conn.query("select id, name from users order by id");
defer rows.close();

io.println(rows.columns());

while (rows.next()) {
    let row = rows.row();
    io.println(row["id"] as string + ": " + row["name"]);
}
```

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
