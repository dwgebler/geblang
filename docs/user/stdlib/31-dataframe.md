# dataframe

Columnar data frames in the Pandas mould (1.19.0): named, typed columns
with per-column null masks, expression-based filtering and derivation,
grouping with aggregation, joins, and IO over CSV, JSON records, and
SQL. Numeric columns bridge to `ndarray` for compute.

```gb
import dataframe as df;
import io;

let users = df.readCsv("users.csv", {"types": {"age": "int"}});
let adults = users
    .filter(df.col("age").gt(30).and_(df.col("active").eq(true)))
    .sort("age", {"desc": true});
io.println(adults.head(5).toDicts());
```

Columns come in four dtypes - `float64`, `int64`, `string`, `bool` -
each with a null mask, so SQL `NULL`s and blank CSV cells survive round
trips. Every verb is immutable: it returns a new frame and never
mutates the receiver (untouched columns are shared, not copied).

## Construction and IO

| Function | Source |
|----------|--------|
| `fromDict(cols)` | `{"name": [...], "age": [...]}` column lists |
| `fromRecords(rows)` | List of row dicts; columns are the key union |
| `fromCsv(text, opts = {})` | CSV text with a header row |
| `fromJson(text)` | JSON array of records |
| `readCsv(path, opts = {})` | CSV file |
| `fromQuery(conn, sql, params...)` | Rows from a `db` connection |

CSV column types are inferred (`int64` -> `float64` -> `bool` ->
`string`); override per column with `{"types": {"age": "int"}}`.

Output: `frame.toCsv()` (text), `frame.toJson()`, `frame.toDicts()`,
`df.writeCsv(frame, path)`, and `df.toTable(frame, conn, table)` which
bulk-inserts (creating the table when missing) and returns the row
count. Table and column names must be plain identifiers.

## Inspection

`shape()` (`[rows, cols]`), `rows()`, `columns()`, `dtypes()`,
`head(n = 5)`, `tail(n = 5)`, and `describe()` (count/mean/std/min/max
per numeric column).

## Selection and filtering

Filters are expression objects built from `df.col(name)` and combined
columnwise - no per-row callback, which is what keeps filtering fast:

```gb
users.filter(df.col("age").gt(30).and_(df.col("active").eq(true)));
users.select(["name", "age"]);
users.sort("age", {"desc": true});
users.unique("country");
```

Expression methods: comparisons `gt lt gte lte eq ne` (against a value
or another expression), logic `and_ or_ not`, arithmetic
`add sub mul div` (string `add` concatenates), and `isNull()`.

The comparison and arithmetic operators build the same expressions, so
filters and derivations read like Polars:

```gb
users.filter(df.col("age") > 30);
users.withColumn("total", df.col("price") * df.col("qty"));
```

When a predicate cannot be expressed columnwise, `filterFn` runs a
function once per row. Each row is passed as a `dict` of column name to
value (nulls included), and the function returns a `bool`:

```gb
let cutoff = 40;
let adults = users.filterFn(func(any row): bool {
    return row["age"] != null && (row["age"] as int) > cutoff;
});
```

`filterFn` is the escape hatch, not the default: the columnwise
expression filter is much faster, so reach for `filterFn` only when the
condition genuinely needs per-row Geblang code. A `throw` inside the
predicate propagates out of `filterFn` and is catchable as usual.

`==` and `!=` keep their language-wide meaning; use `eq()` / `ne()` in
expressions.

## Derivation and nulls

```gb
users.withColumn("ageMonths", df.col("age").mul(12));
users.rename({"age": "years"});
users.drop(["tmp"]);
users.dropNulls(["age"]);     # no argument drops rows null in ANY column
users.fillNull("age", 0);
users.col("age").isNull();    # bool Series
```

Null propagation: arithmetic over a null yields null; comparisons over
a null yield false (so filters drop null rows unless you ask for them
with `isNull`).

## Grouping and joins

```gb
users.groupBy("country").agg({
    "age": ["mean", "max"],
    "id": "count",
});
orders.join(users, {"on": "userId", "how": "left"});
df.concat([a, b]);
```

Aggregations: `count`, `sum`, `mean`, `min`, `max`, `std`, `first`,
`last`, `collect`. Aggregated columns are named `<col>_<agg>`. `groupBy`
accepts one name or a list for composite keys. Joins are hash joins on
one key column; `how` is `inner`, `left`, `right`, or `outer`, and
clashing non-key column names get `_left` / `_right` suffixes.

`pivot` spreads one column's values into new columns, one row per
distinct index value, aggregating the values column per cell:

```gb
sales.pivot({"index": "region", "columns": "quarter", "values": "amount", "agg": "sum"});
```

`agg` accepts the aggregators above except `collect` and defaults to
`sum`. Rows whose index or columns cell is null are skipped; empty
cells are null. New columns appear in first-seen order.

## Series and the ndarray bridge

`frame.col(name)` returns a Series view: `name()`, `dtype()`,
`length()`, `toList()`, `isNull()`, and `sum`/`mean`/`min`/`max`
(null-aware). On numeric columns, `values()` hands you the data as a
1-D `ndarray` for the compute layer:

```gb
let prices = sales.col("price").values();   # ndarray
io.println(prices.std());
```
