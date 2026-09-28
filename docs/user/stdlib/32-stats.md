# stats

Probability distributions as objects (1.27.0). Each constructor returns
a `Distribution` with a uniform method set: `pdf`, `cdf`, `ppf`, `mean`,
`variance`, `std`, and `sample`.

```gb
import stats;

let d = stats.normal(0.0, 1.0);
d.pdf(0.0);               /* 0.3989... */
d.cdf(1.96);              /* 0.9750... */
d.ppf(0.975);             /* 1.9599... */
d.mean(); d.variance(); d.std();

d.sample(1000);                   /* 1-D ndarray of 1000 draws */
d.sample(1000, {"seed": 42});     /* reproducible draw */
d.sample();                       /* single scalar */
```

## Distributions

| Constructor | Parameters | Support |
|-------------|-----------|---------|
| `normal(mu, sigma)` | sigma > 0 (std dev) | all reals |
| `uniform(a, b)` | a < b | [a, b] |
| `exponential(rate)` | rate > 0 | x >= 0 |
| `gamma(shape, scale)` | shape > 0, scale > 0 | x > 0 |
| `beta(alpha, beta)` | alpha > 0, beta > 0 | [0, 1] |
| `chiSquared(df)` | df > 0 | x >= 0 |
| `studentT(df)` | df > 0 | all reals |
| `f(d1, d2)` | d1 > 0, d2 > 0 | x >= 0 |
| `lognormal(mu, sigma)` | sigma > 0 | x > 0 |
| `weibull(shape, scale)` | shape > 0, scale > 0 | x >= 0 |
| `binomial(n, p)` | n >= 0 (int), 0 <= p <= 1 | {0, ..., n} |
| `poisson(lambda)` | lambda > 0 | {0, 1, 2, ...} |

## Moments

| Distribution | mean | variance |
|--------------|------|----------|
| normal | mu | sigma^2 |
| uniform | (a+b)/2 | (b-a)^2/12 |
| exponential | 1/rate | 1/rate^2 |
| gamma | shape*scale | shape*scale^2 |
| beta | a/(a+b) | ab/((a+b)^2(a+b+1)) |
| chiSquared | df | 2*df |
| studentT | 0 (df > 1), NaN otherwise | df/(df-2) (df > 2), NaN otherwise |
| f | d2/(d2-2) (d2 > 2), NaN otherwise | standard closed form |
| lognormal | exp(mu + sigma^2/2) | standard closed form |
| weibull | scale*gamma(1+1/shape) | standard closed form |
| binomial | n*p | n*p*(1-p) |
| poisson | lambda | lambda |

`mean()` and `variance()` return NaN where the moment is undefined for
the given parameters (for example, `studentT` mean when df <= 1). This
follows the `math` module's NaN convention rather than throwing.

## Methods

- `pdf(x)` - probability density (continuous) or probability mass (discrete).
  For discrete distributions this is the mass function; `pdf` at a
  non-integer argument returns 0.0.
- `cdf(x)` - cumulative distribution function.
- `ppf(p)` - inverse CDF (quantile function); p must be in [0, 1].
- `mean()`, `variance()`, `std()` - closed-form moments. `std()` is
  `sqrt(variance())`.
- `sample(n)` - draw n variates, returned as a 1-D ndarray (`float64` for
  continuous distributions, `int64` for discrete).
- `sample(n, {"seed": k})` - reproducible draw; the same seed produces the
  same sequence on both the evaluator and the VM.
- `sample()` - single scalar draw (float for continuous, int for discrete).

## Reproducibility

The seed is local to the call: passing `{"seed": k}` does not affect the
module's shared RNG used by unseeded calls. The same seed produces
byte-identical results on both backends and across process restarts.

```gb
let a = stats.normal(0.0, 1.0).sample(5, {"seed": 1});
let b = stats.normal(0.0, 1.0).sample(5, {"seed": 1});
/* a and b are identical */
```

## Error handling

- Constructors throw `RuntimeError` if parameters are out of range (e.g.
  `sigma <= 0`, `a >= b`, `df <= 0`, `p < 0 or p > 1`, `n < 0`).
- `ppf(p)` throws `RuntimeError` if p is outside [0, 1].
- `sample(n)` throws `RuntimeError` if n < 0.

```gb
try {
    stats.normal(0.0, -1.0);
} catch (RuntimeError e) {
    io.println(e.message); /* sigma must be > 0 */
}
```

## Example: large-sample mean convergence

```gb
import stats;

let d = stats.poisson(4.0);
let s = d.sample(10000, {"seed": 42});
io.println(s.mean()); /* close to 4.0 */
```

## Hypothesis tests and confidence intervals

Tests return a dict with `statistic`, `pvalue`, and (where applicable) `df`.
Confidence intervals return `{low, high}`.

```gb
import stats;

let a = [2.1, 2.4, 2.6, 2.8, 3.0];
let b = [1.9, 2.0, 2.2, 2.3, 2.5];

stats.tTestOneSample(a, 2.5);                            /* one-sample t vs mu=2.5 */
stats.tTestIndependent(a, b);                            /* pooled two-sample t */
stats.tTestIndependent(a, b, {"equalVar": false});       /* Welch variant */
stats.tTestPaired(a, b);                                 /* paired t */
stats.chiSquareTest([10, 20, 30], [20, 20, 20]);         /* goodness-of-fit */
stats.chiSquareIndependence([[10, 20], [30, 40]]);       /* independence */
stats.mannWhitneyU(a, b);                                /* Mann-Whitney U */
stats.ksTest(a, b);                                      /* Kolmogorov-Smirnov */
stats.confidenceIntervalMean(a, 0.95);                   /* CI for mean */
stats.confidenceIntervalProportion(40, 100, 0.95);       /* CI for proportion */
stats.confidenceIntervalDiffMeans(a, b, 0.95);           /* CI for difference of means */
```

| Function | Result keys |
|----------|-------------|
| `tTestOneSample(sample, mu, opts?)` | `{statistic, pvalue, df}` |
| `tTestIndependent(a, b, opts?)` | `{statistic, pvalue, df}` |
| `tTestPaired(a, b, opts?)` | `{statistic, pvalue, df}` |
| `chiSquareTest(observed, expected?, opts?)` | `{statistic, pvalue, df}` |
| `chiSquareIndependence(table)` | `{statistic, pvalue, df, expected}` |
| `mannWhitneyU(a, b, opts?)` | `{statistic, pvalue}` |
| `ksTest(a, b)` | `{statistic, pvalue}` |
| `confidenceIntervalMean(sample, level?)` | `{low, high}` |
| `confidenceIntervalProportion(successes, n, level?)` | `{low, high}` |
| `confidenceIntervalDiffMeans(a, b, level?, opts?)` | `{low, high}` |

**`opts` keys:**

- `alternative` - `"two-sided"` (default), `"less"`, or `"greater"`.
- `equalVar` - `true` (default, pooled) or `false` (Welch) for `tTestIndependent` and `confidenceIntervalDiffMeans`.
- `ddof` - integer delta degrees of freedom for `chiSquareTest` (default 0).

`chiSquareIndependence` returns the matrix of expected cell counts in `expected`
as a list of lists in addition to `statistic`, `pvalue`, and `df`.

The confidence `level` parameter defaults to `0.95` if omitted. All sample
arguments must be lists of numbers; mismatched lengths, empty lists, or
out-of-range `level` values raise `RuntimeError`.

## Regression

```gb
import stats;

let xs = [1.0, 2.0, 3.0, 4.0, 5.0];
let ys = [2.1, 3.9, 6.0, 8.1, 9.8];

let fit = stats.linregress(xs, ys);   /* {slope, intercept, r, r2, pvalue, stderr} */
let c = stats.polyfit(xs, ys, 2);     /* coefficients, highest degree first */
stats.polyval(c, 3.0);                /* evaluate the polynomial at x=3.0 */
```

| Function | Result |
|----------|--------|
| `linregress(x, y)` | `{slope, intercept, r, r2, pvalue, stderr}` (requires n >= 3) |
| `polyfit(x, y, degree)` | `list<float>` of `degree+1` coefficients, highest degree first (degree in [1, 10]) |
| `polyval(coeffs, x)` | float (coefficients highest degree first) |

`linregress` fits a line y = slope*x + intercept using ordinary least squares.
`r` is the Pearson correlation coefficient, `r2` its square, `pvalue` the
two-tailed p-value against slope=0 via the Student-t distribution, and
`stderr` the standard error of the slope. Both `x` and `y` must have at
least 3 elements.

`polyfit(x, y, degree)` fits a polynomial of the given degree (1 to 10) using
normal equations. Coefficients are returned highest degree first, matching the
convention used by `polyval`. A singular design matrix raises `RuntimeError`.

`polyval(coeffs, x)` evaluates a polynomial at a single point using Horner's
method. `coeffs` is a list of coefficients highest degree first (matching
`polyfit` output). An empty `coeffs` list raises a runtime error.

```gb
/* fit a quadratic and evaluate at new points */
let c = stats.polyfit([0.0, 1.0, 2.0, 3.0], [0.0, 1.1, 3.9, 9.1], 2);
io.println(stats.polyval(c, 4.0));   /* ~16.0 */
```

## Descriptive statistics (1.35.0)

```gb
import stats;

let xs = [2, 4, 4, 4, 5, 5, 7, 9];

stats.variance(xs);                         /* 4.571428571428571 (sample, n-1) */
stats.stdev(xs, {"population": true});      /* 2.0 */
stats.weightedMean([1, 2], [3, 1]);         /* 1.25 */
stats.iqr([1, 2, 3, 4, 5, 6, 7, 8]);        /* 3.5 */
stats.zscores([1, 2, 3]);                   /* [-1.0, 0.0, 1.0] */
stats.describe([1, 2, 3, 4]);
/* {"count": 4, "mean": 2.5, "stdev": 1.29..., "min": 1.0,
    "q1": 1.75, "median": 2.5, "q3": 3.25, "max": 4.0} */
```

| Function | Result |
|----------|--------|
| `variance(xs, opts?)` | sample variance (n-1); `{"population": true}` for the population variance |
| `stdev(xs, opts?)` | sample standard deviation; `{"population": true}` for the population form |
| `geometricMean(xs)` | geometric mean; every value must be positive |
| `harmonicMean(xs)` | harmonic mean; every value must be positive |
| `weightedMean(xs, weights)` | mean weighted by non-negative weights (both lists, equal length) |
| `range(xs)` | max minus min |
| `iqr(xs)` | interquartile range Q3 - Q1, using the same type-7 quantile as `math.quantile` |
| `mad(xs)` | median absolute deviation (unscaled) |
| `zscores(xs)` | `list<float>` of standard scores using the sample standard deviation |
| `describe(xs)` | dict of `count`, `mean`, `stdev`, `min`, `q1`, `median`, `q3`, `max` |

Results are floats unless stated. `xs` may be a list or a set, except
`weightedMean` and `zscores`, which need a list because order matters.
Every function raises `RuntimeError` on an empty input or a non-numeric
element. The sample variance and standard deviation (and `zscores`) need at
least 2 values; the population forms need at least 1. `zscores` also raises
when every value is equal. `describe` of a single value reports a `NaN`
`stdev` rather than raising. The arithmetic mean, sum, median, mode and
percentiles are in the [`math`](11-math-datetime.md) module.

## Descriptive extensions

```gb
import stats;

let xs = [2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0];
let ys = [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0];

stats.skewness(xs);           /* population skewness */
stats.kurtosis(xs);           /* population excess kurtosis (normal is 0) */
stats.covariance(xs, ys);     /* sample covariance (n-1 denominator) */
stats.corrcoef(xs, ys);       /* Pearson correlation coefficient */
```

| Function | Result |
|----------|--------|
| `skewness(xs)` | population skewness (float) |
| `kurtosis(xs)` | population excess kurtosis, normal is 0 (float) |
| `covariance(xs, ys)` | sample covariance, n-1 denominator (float) |
| `corrcoef(xs, ys)` | Pearson correlation coefficient (float) |

`skewness` and `kurtosis` require at least 2 values and non-zero variance.
`covariance` and `corrcoef` require equal-length samples of at least 2
elements; `corrcoef` additionally requires that neither input is constant
(non-zero variance in both `xs` and `ys`). All functions raise `RuntimeError`
when their preconditions are not met.
