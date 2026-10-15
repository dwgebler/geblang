# complex

Complex number type and arithmetic (1.27.0). `complex.of` and
`complex.fromPolar` return a `Complex` value with a full method set and
operator overloads.

```gb
import complex;
import io;

let z = complex.of(3.0, 4.0);
io.println("${z}");   /* 3+4i */
z.abs();              /* 5.0 */
z.arg();              /* ~0.9272... radians */
```

## Constructors

```gb
complex.of(re, im)
```

Returns a `Complex` with the given real and imaginary parts. Both arguments
must be numeric (int, decimal, or float).

```gb
complex.fromPolar(r, theta)
```

Returns a `Complex` from polar form `r * e^(i*theta)`. `theta` is in radians.

```gb
let z = complex.fromPolar(5.0, math.pi() / (4.0 as float));
/* ~3.5355+3.5355i */
```

## Rendering

A `Complex` value renders as `a+bi` or `a-bi` using the most compact
decimal representation for each part. For example, `complex.of(1.0, -2.0)`
renders as `1-2i`.

## Methods

Unary methods return a new `Complex` or float; they take no arguments.
Binary arithmetic methods accept either a `Complex` or a plain number; the
number is promoted to `Complex` with zero imaginary part.

| Method | Returns | Description |
|--------|---------|-------------|
| `re()` | float | real part |
| `im()` | float | imaginary part |
| `abs()` | float | modulus (magnitude) |
| `arg()` | float | argument (phase angle) in radians |
| `conj()` | Complex | complex conjugate |
| `neg()` | Complex | negation (unary minus) |
| `exp()` | Complex | e raised to the power of z |
| `sqrt()` | Complex | principal square root |
| `add(other)` | Complex | addition |
| `sub(other)` | Complex | subtraction |
| `mul(other)` | Complex | multiplication |
| `div(other)` | Complex | division |
| `pow(other)` | Complex | exponentiation |
| `equals(other)` | bool | exact equality (promotes numbers) |

```gb
let a = complex.of(1.0, 2.0);
let b = complex.of(3.0, 4.0);

a.add(b);    /* 4+6i */
a.mul(b);    /* -5+10i */
a.conj();    /* 1-2i */
a.equals(complex.of(1.0, 2.0));  /* true */
```

## Operator overloads

The operators `+`, `-`, `*`, `/`, and `**` are overloaded for `Complex`
values and are interchangeable with the corresponding methods. Unary `-`
is also supported. The `==` operator tests exact equality.

```gb
let z = complex.of(1.0, 2.0);
let w = complex.of(3.0, 4.0);

z + w;        /* same as z.add(w): 4+6i */
z * w;        /* same as z.mul(w): -5+10i */
z ** 2.0;     /* same as z.pow(2.0) */
-z;           /* same as z.neg(): -1-2i */
z == w;       /* same as z.equals(w): false */
```

A plain number on either side of a binary operator is promoted to `Complex`
with zero imaginary part:

```gb
z + 2.0;     /* complex.of(3.0, 2.0) */
3 * z;       /* complex.of(3.0, 6.0) */
```

## Error handling

- `complex.of` and `complex.fromPolar` raise `RuntimeError` if either
  argument is not numeric or the wrong number of arguments is supplied.
- Binary methods (`add`, `sub`, ...) raise `RuntimeError` if the argument
  cannot be promoted (e.g. a string).

```gb
try {
    complex.of(1.0, "x");
} catch (RuntimeError e) {
    io.println(e.message);
}
```
