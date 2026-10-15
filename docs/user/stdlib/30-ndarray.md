# ndarray

N-dimensional numeric arrays over contiguous typed storage (1.19.0):
elementwise arithmetic with broadcasting, zero-copy views, reductions,
linear algebra, and seeded random generation. The compute substrate for
numeric work in the NumPy mould.

```gb
import ndarray as nd;
import io;

let a = nd.array([[1.0, 2.0], [3.0, 4.0]]);
io.println(a.shape());        # [2, 2]
io.println(a.sum());          # 10
io.println(a.matmul(nd.eye(2)).toList());
```

Arrays come in two dtypes: `float64` and `int64`. `nd.array` infers the
dtype from its input - any float element gives `float64`, an all-int
input gives `int64`; `astype` converts explicitly. Mixed-dtype binary
operations promote `int64` to `float64`, and `div` and `pow` always
produce `float64`.

## Constructors

| Function | Result |
|----------|--------|
| `array(values)` | Array from a (possibly nested) numeric list |
| `zeros(shape)` / `ones(shape)` | float64 array of zeros / ones |
| `full(shape, value)` | Filled array; dtype follows the fill value |
| `eye(n)` | n x n float64 identity |
| `arange(start, stop, step = 1)` | int64 range `[start, stop)` |
| `linspace(start, stop, count)` | `count` evenly spaced float64 values, inclusive |
| `random(shape, opts = {})` | Uniform `[0, 1)` float64 samples |
| `randn(shape, opts = {})` | Standard normal float64 samples |

`random` and `randn` accept `{"seed": n}` for reproducible sequences
(the same generator family as the `random` module; not cryptographic - use `secrets` for keys and tokens).

## Shape, access, and views

`shape()`, `dtype()`, `size()`, `get(index)`, `set(index, value)`
(index is one position per dimension: `a.get([1, 0])`).

`slice([[start, stop], ...])`, `t()` (transpose), and `reshape(shape)`
on a contiguous array are zero-copy views: writing through a view
mutates the underlying array. `copy()` materialises an independent
array; `toList()` converts to nested lists.

```gb
let row = a.slice([[1, 2]]);   # second row, as a view
row.set([0, 0], 99.0);         # a.get([1, 0]) is now 99.0
```

## Elementwise operations and broadcasting

`add`, `sub`, `mul`, `div`, `pow` accept an array or a scalar and
broadcast trailing dimensions NumPy-style (a `[2, 1]` column and a
`[1, 3]` row combine to `[2, 3]`). `addScalar` / `subScalar` /
`mulScalar` / `divScalar` are scalar conveniences. Unary: `neg`, `abs`,
`sqrt`, `exp`, `log`, `clip(lo, hi)`.

Comparisons `gt`, `lt`, `gte`, `lte`, `eq`, `ne` produce an int64 0/1
mask; `where(mask)` selects the masked elements as a 1-D array:

```gb
let big = a.where(a.gt(2.0));
```

### Operators

The arithmetic operators map to the elementwise methods, with scalars
on either side, and unary minus negates:

```gb
let c = a + b;        # a.add(b), broadcasting included
let d = 2 * a - 1;    # scalars mix on either side
let e = a ** 2;       # a.pow(2)
let f = -a;           # a.neg()
```

The ordering operators return masks, so a NumPy-style selection reads
naturally:

```gb
let big = a.where(a > 2.0);
```

`==` and `!=` keep their language-wide meaning; use `eq()` / `ne()` for
elementwise equality masks.

## Reductions

`sum`, `mean`, `min`, `max` reduce the whole array, or along one axis
with `{"axis": n}` (`a.sum({"axis": 0})` is column sums). `std` and
`variance` are sample statistics over the whole array. `argmin` /
`argmax` return the flat index of the extreme; `cumsum` returns the
running sum flattened to 1-D. Integer arrays keep `int64` for `sum` /
`min` / `max`; `mean`, `std`, and axis reductions produce `float64`.

## Linear algebra

| Call | Result |
|------|--------|
| `a.matmul(b)` | Matrix product (blocked float64 kernel) |
| `a.dot(v)` | Dot product of two 1-D arrays (use matmul for matrix / matrix-vector products) |
| `solve(a, b)` | Solves `a x = b` (Gaussian elimination, partial pivoting) |
| `inv(a)` | Matrix inverse |
| `det(a)` | Determinant (`0.0` for a singular matrix) |

`solve` and `inv` throw a catchable `RuntimeError` on a singular
matrix. The kernels are pure Go; for BLAS-class performance on large
matrices, measure first - a 1000 x 1000 multiply is in the hundreds of
milliseconds.
