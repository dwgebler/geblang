# Geblang for data scientists

## Who this is for

This guide is for data scientists, analysts, and engineers who work in Python
with NumPy, pandas, and SciPy (or similar stacks) and want to use Geblang for
data processing, statistical work, or building data pipelines. You will find
familiar concepts with different names and a few Geblang-specific rules to
keep in mind.

## Quick orientation

The following program builds an array, fits a distribution, loads a dataframe
from CSV text, and prints a summary - the kind of thing you might write to
explore a dataset:

```gb
import io;
import ndarray as nd;
import stats;
import dataframe as df;

/* Array and basic statistics */
let temps = nd.array([22.1, 24.5, 19.3, 27.8, 23.6, 21.0, 25.9]);
io.println("mean: ${temps.mean()}");
io.println("std:  ${temps.std()}");

/* Fit a normal distribution and find the 95th percentile */
let d = stats.normal(temps.mean(), temps.std());
io.println("95th pct: ${d.ppf(0.95)}");

/* Summarize a dataframe */
let frame = df.fromDict({
    "city":  ["Oslo", "Rome", "Cairo"],
    "temp":  [22.1, 24.5, 27.8],
    "humid": [65, 55, 40],
});
io.println("shape: ${frame.shape()}");
```

Output:

```
mean: 23.45714285714286
std:  2.918251270228538
95th pct: 28.25723904333401
shape: [3, 3]
```

## Coming from NumPy/pandas/SciPy: concept mapping

| You know | Geblang equivalent |
|----------|--------------------|
| `numpy` | `ndarray` module |
| `pandas.DataFrame` | `dataframe` module |
| `scipy.stats` distributions | `stats` distributions |
| `scipy.stats` hypothesis tests | `stats.tTestIndependent`, `stats.chiSquareTest`, etc. |
| `scipy.stats.linregress` | `stats.linregress` |
| `numpy.polyfit` / `polyval` | `stats.polyfit` / `stats.polyval` |
| `scipy.special.erf`, `gamma`, Bessel functions | `math.erf`, `math.gamma`, `math.j0`, etc. |
| `cmath.rect` / `abs` / `phase` | `complex.fromPolar`, `z.abs()`, `z.arg()` |
| Haversine / geodetic math | `geo` module |
| `physics` constants (`scipy.constants`) | `physics` module |
| `json.load` / `json.dump` | `json.parse` / `json.stringify` |
| `pandas.read_csv` | `dataframe.readCsv` |
| `df.to_json()` | `frame.toJson()` |

## Key features for you

### N-dimensional arrays (ndarray)

The `ndarray` module provides NumPy-style N-dimensional arrays over typed
contiguous storage. Two dtypes are supported: `float64` and `int64`. Mixed
operations promote `int64` to `float64`; `div` and `pow` always produce
`float64`.

```gb
import io;
import ndarray as nd;

let prices = nd.array([10.0, 20.0, 30.0, 40.0, 50.0]);

/* Scalar arithmetic broadcasts across the array */
let discounted = prices * 0.9;
io.println("discounted: ${discounted.toList()}");
/* [9, 18, 27, 36, 45] */

/* Comparison returns a 0/1 mask; where selects matching elements */
let above = prices.where(prices > prices.mean());
io.println("above avg: ${above.toList()}");
/* [40, 50] */

/* Matrix product */
let a = nd.array([[1.0, 2.0], [3.0, 4.0]]);
let b = nd.array([[5.0, 6.0], [7.0, 8.0]]);
io.println("a @ b: ${a.matmul(b).toList()}");
/* [[19, 22], [43, 50]] */
```

Constructors: `nd.zeros(shape)`, `nd.ones(shape)`, `nd.eye(n)`,
`nd.arange(start, stop)`, `nd.linspace(start, stop, count)`,
`nd.random(shape)`, `nd.randn(shape)`.

Reductions: `sum`, `mean`, `min`, `max`, `std`, `variance` (whole array or
along one axis with `{"axis": n}`). Linear algebra: `matmul`, `dot`,
`nd.solve(a, b)`, `nd.inv(a)`, `nd.det(a)`.

See [stdlib/30-ndarray.md](../stdlib/30-ndarray.md) for the full reference.

### Dataframes (dataframe)

The `dataframe` module provides column-oriented dataframes with four column
dtypes: `float64`, `int64`, `string`, and `bool`. Every verb returns a new
frame; nothing mutates in place.

```gb
import io;
import dataframe as df;

let sales = df.fromDict({
    "product": ["A", "B", "A", "C", "B", "C"],
    "region":  ["East", "West", "West", "East", "East", "West"],
    "revenue": [1200, 950, 1100, 800, 1400, 760],
});

/* Columnwise filter - fast, no per-row callback */
let east = sales.filter(df.col("region").eq("East"))
                .select(["product", "revenue"]);

/* Group and aggregate */
let byProduct = sales.groupBy("product").agg({
    "revenue": ["sum", "mean"],
});
io.println("${byProduct.toDicts()}");

/* Bridge to ndarray for compute */
let arr = sales.col("revenue").values();
io.println("std: ${arr.std()}");
```

Column expressions use `df.col("name")` and chain: `gt`, `lt`, `gte`, `lte`,
`eq`, `ne`, `and_`, `or_`, `isNull`. Arithmetic operators (`+`, `*`, etc.)
and comparison operators also work directly on column expressions.

When you need per-row Geblang code (rare), use `filterFn`:

```
/* fragment - filterFn is the escape hatch for complex per-row logic */
let odd = frame.filterFn(func(any row): bool {
    return (row["id"] as int) % 2 == 1;
});
```

Output methods: `toCsv()`, `toJson()`, `toDicts()`, `df.writeCsv(frame, path)`.
IO constructors: `df.readCsv(path)`, `df.fromCsv(text)`, `df.fromRecords(rows)`,
`df.fromJson(text)`.

See [stdlib/31-dataframe.md](../stdlib/31-dataframe.md) for the full reference.

### Statistics (stats)

The `stats` module provides probability distributions as objects, hypothesis
tests, confidence intervals, regression, and descriptive statistics.

```gb
import io;
import stats;

/* Distributions: each has pdf, cdf, ppf, mean, variance, std, sample */
let d = stats.normal(0.0, 1.0);
io.println("pdf(0):    ${d.pdf(0.0)}");      /* 0.3989... */
io.println("cdf(1.96): ${d.cdf(1.96)}");     /* 0.9750... */
io.println("ppf(0.975): ${d.ppf(0.975)}");   /* 1.9599... */

/* Sample: returns a 1-D ndarray */
let draws = d.sample(1000, {"seed": 42});
io.println("sample mean: ${draws.mean()}");

/* Hypothesis test */
let a = [2.1, 2.4, 2.6, 2.8, 3.0];
let b = [1.9, 2.0, 2.2, 2.3, 2.5];
let t = stats.tTestIndependent(a, b);
io.println("p-value: ${t["pvalue"]}");

/* Linear regression */
let xs = [1.0, 2.0, 3.0, 4.0, 5.0];
let ys = [2.1, 3.9, 6.0, 8.1, 9.8];
let fit = stats.linregress(xs, ys);
io.println("slope: ${fit["slope"]}, r2: ${fit["r2"]}");

/* Descriptive */
io.println("corrcoef: ${stats.corrcoef(xs, ys)}");
io.println("skewness: ${stats.skewness(a)}");
```

Available distributions: `normal`, `uniform`, `exponential`, `gamma`, `beta`,
`chiSquared`, `studentT`, `f`, `lognormal`, `weibull`, `binomial`, `poisson`.

Tests: `tTestOneSample`, `tTestIndependent`, `tTestPaired`, `chiSquareTest`,
`chiSquareIndependence`, `mannWhitneyU`, `ksTest`.

Confidence intervals: `confidenceIntervalMean`, `confidenceIntervalProportion`,
`confidenceIntervalDiffMeans`.

Regression: `linregress`, `polyfit`, `polyval`.

See [stdlib/32-stats.md](../stdlib/32-stats.md) for the full reference.

### Reading and writing CSV and JSON

```gb
import io;
import csv;
import json;
import dataframe as df;

let csvText = "city,temp,humid\nOslo,22.1,65\nRome,24.5,55\nCairo,27.8,40\n";

/* Parse CSV text into a dataframe; override inferred types per column */
let frame = df.fromCsv(csvText, {"types": {"humid": "int"}});
io.println("shape: ${frame.shape()}");
io.println("${frame.toJson()}");

/* Raw CSV parsing when you need individual cells */
let rows = csv.parseDict(csvText);
io.println("${rows[0]["city"]}");

/* JSON serialization */
io.println(json.stringify({"result": "ok", "count": 3}));
let data = json.parse('{"x": 1, "y": 2}');
io.println("x=${data["x"]}");
```

Output:

```
shape: [3, 3]
[{"city":"Oslo","humid":65,"temp":22.1},{"city":"Rome","humid":55,"temp":24.5},{"city":"Cairo","humid":40,"temp":27.8}]
Oslo
{"count":3,"result":"ok"}
x=1
```

`dataframe.readCsv(path)` reads a file directly. `df.writeCsv(frame, path)`
writes one. Column types are inferred (int64 -> float64 -> bool -> string)
unless you override them with `{"types": {...}}`.

See [stdlib/07-data-formats.md](../stdlib/07-data-formats.md) for the full
reference, including YAML, TOML, XML, MessagePack, and schema validation.

### Handing data off for plotting

Geblang has no built-in plotting library. The typical handoff pattern is to
write CSV or JSON from a frame and pass it to an external tool:

```gb
import io;
import dataframe as df;

/* fragment - write a frame to CSV for downstream plotting */
df.writeCsv(frame, "/tmp/results.csv");

/* Or write JSON records for tools that prefer that */
io.writeText("/tmp/results.json", frame.toJson());
```

From Python, load it with `pandas.read_csv("/tmp/results.csv")` and plot as
usual with Matplotlib or Seaborn. From JavaScript, `fetch` the JSON file or
read it with `fs.readFileSync`.

### Physics constants and unit conversion (physics)

```gb
import io;
import physics;

io.println("c = ${physics.c()} m/s");
io.println("100 mi = ${physics.convert(100.0, "mi", "km")} km");
io.println("100 C = ${physics.convert(100.0, "C", "F")} F");
io.println("G = ${physics.G()} m^3 kg^-1 s^-2");
```

Length, mass, time, and temperature conversions. Temperature uses affine
conversion (Celsius, Fahrenheit, Kelvin), not a simple scale.

See [stdlib/33-physics.md](../stdlib/33-physics.md) for the full constant and
unit list.

### Complex numbers (complex)

```gb
import io;
import complex;
import math;

let z = complex.of(3.0, 4.0);
io.println("abs: ${z.abs()}");          /* 5 */
io.println("arg: ${z.arg()}");          /* ~0.9272 radians */

let w = complex.of(1.0, 2.0);
io.println("product: ${z * w}");        /* -5+10i */

let polar = complex.fromPolar(5.0, math.pi() / 4.0f);
io.println("polar: ${polar}");
```

Operators `+`, `-`, `*`, `/`, `**`, and unary `-` are overloaded. Use the
method forms (`add`, `mul`, etc.) when you need to be explicit.

See [stdlib/34-complex.md](../stdlib/34-complex.md).

### Geodetic calculations (geo)

```gb
import io;
import geo;

let dist = geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522);
io.println("London to Paris: ${dist} km");

let bearing = geo.bearing(51.5074, -0.1278, 48.8566, 2.3522);
io.println("bearing: ${bearing} degrees");

let mid = geo.midpoint(51.5074, -0.1278, 48.8566, 2.3522);
io.println("midpoint lat: ${mid["lat"]}");
```

All coordinates are decimal degrees. Distance units: `"km"` (default), `"m"`,
`"mi"`, `"nmi"`.

See [stdlib/35-geo.md](../stdlib/35-geo.md).

### Math: special functions and statistics

`import math` gives you trigonometry, logarithms, roots, constants, and special
functions:

```gb
import io;
import math;

io.println("erf(1):   ${math.erf(1.0f)}");   /* 0.8427... */
io.println("gamma(5): ${math.gamma(5.0f)}"); /* 24.0 (= 4!) */
io.println("j0(0):    ${math.j0(0.0f)}");    /* 1.0 */
```

Special functions: `gamma`, `lgamma`, `beta`, `lbeta`, `erf`, `erfc`,
`erfinv`, `j0`, `j1`, `jn`, `y0`, `y1`, `yn`.

Descriptive stats over lists: `math.median(xs)`, `math.percentile(xs, p)`,
`math.quantile(xs, q)`, `math.mode(xs)`.

Combinatorics: `math.factorial(n)`, `math.comb(n, k)`, `math.perm(n, k)`,
`math.gcd(a, b)`, `math.lcm(a, b)`.

See [stdlib/11-math-datetime.md](../stdlib/11-math-datetime.md).

## Gotchas

**`decimal` is the default float type.** A bare literal `3.14` is a `decimal`
(arbitrary-precision rational), not a `float64`. For `ndarray` and `stats` - and
anywhere you need IEEE 754 floats - use the `f` suffix: `3.14f`. Passing a
`decimal` where a `float` is needed is a type error; use `value as float` to
cast.

```
/* fragment - float literal syntax */
let x = 3.14;    /* decimal */
let y = 3.14f;   /* float */
let z = x as float;
```

**You cannot mix `decimal` and `float` in arithmetic.** The type checker
rejects it. Pick one side: either suffix all your literals with `f`, or cast
with `as float`.

**`list.push(x)` mutates in place and returns the receiver.** There is no
immutable append that returns a new list; use `sorted()`, `reversed()`, or
`copy()` to get a copy variant.

**Type-first parameter syntax.** Function parameters are `float x`, not `x:
float`. Return type comes after the closing paren: `func add(float a, float b):
float { return a + b; }`.

**No `//` line comments.** Geblang uses `#` for line comments, `/* ... */` for
block comments, and `/** ... */` for doc blocks.

**`parent()`, not `super`.** In a subclass constructor or method, call the
parent with `parent(args)`.

**Interpolation uses double-quoted strings.** `"${value}"` embeds a value;
single-quoted strings `'like this'` do not interpolate.

## Where to go next

- [ndarray reference](../stdlib/30-ndarray.md) - full constructor and method list
- [dataframe reference](../stdlib/31-dataframe.md) - grouping, joins, pivot, filterFn
- [stats reference](../stdlib/32-stats.md) - all distributions and tests
- [physics reference](../stdlib/33-physics.md) - constants and unit conversion
- [complex reference](../stdlib/34-complex.md) - complex arithmetic
- [geo reference](../stdlib/35-geo.md) - geodetic functions
- [math and datetime](../stdlib/11-math-datetime.md) - special functions, rounding, combinatorics
- [data formats](../stdlib/07-data-formats.md) - CSV, JSON, YAML, TOML, schema validation
- [examples/ndarray.gb](https://github.com/dwgebler/geblang/blob/main/examples/ndarray.gb) - runnable array examples
- [examples/dataframe.gb](https://github.com/dwgebler/geblang/blob/main/examples/dataframe.gb) - runnable dataframe examples
- [examples/math.gb](https://github.com/dwgebler/geblang/blob/main/examples/math.gb) - math module examples
