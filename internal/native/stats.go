package native

import (
	"fmt"
	"math"
	mrand "math/rand"
	"sort"
	"time"

	"geblang/internal/runtime"
)

// DistributionMethods is the canonical method list for dir/catalog guards.
var DistributionMethods = []string{"pdf", "cdf", "ppf", "mean", "variance", "std", "sample"}

func statsIsDiscrete(kind string) bool {
	return kind == "binomial" || kind == "poisson"
}

func statsFloatArg(args []runtime.Value, label string) (float64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s expects one numeric argument", label)
	}
	return FloatLike(args[0])
}

func statsTwoFloats(args []runtime.Value, label, shape string) (float64, float64, error) {
	if len(args) != 2 {
		return 0, 0, fmt.Errorf("%s expects %s", label, shape)
	}
	a, err := FloatLike(args[0])
	if err != nil {
		return 0, 0, err
	}
	b, err := FloatLike(args[1])
	if err != nil {
		return 0, 0, err
	}
	return a, b, nil
}

// Standard series + Lentz continued-fraction algorithm for the regularized incomplete gamma.
func statsRegGammaP(a, x float64) float64 {
	if x <= 0 || a <= 0 {
		if x == 0 {
			return 0
		}
		return math.NaN()
	}
	lg, _ := math.Lgamma(a)
	if x < a+1 {
		sum := 1.0 / a
		term := sum
		for n := 1; n < 2000; n++ {
			term *= x / (a + float64(n))
			sum += term
			if math.Abs(term) < math.Abs(sum)*1e-16 {
				break
			}
		}
		return sum * math.Exp(-x+a*math.Log(x)-lg)
	}
	const tiny = 1e-300
	b := x + 1 - a
	c := 1.0 / tiny
	dd := 1.0 / b
	h := dd
	for i := 1; i < 2000; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		dd = an*dd + b
		if math.Abs(dd) < tiny {
			dd = tiny
		}
		c = b + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		dd = 1 / dd
		del := dd * c
		h *= del
		if math.Abs(del-1) < 1e-16 {
			break
		}
	}
	q := math.Exp(-x+a*math.Log(x)-lg) * h
	return 1 - q
}

// statsRegBetaI returns the regularized incomplete beta I_x(a, b).
func statsRegBetaI(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(a*math.Log(x) + b*math.Log(1-x) + lab - la - lb)
	if x < (a+1)/(a+b+2) {
		return front * statsBetaCF(x, a, b) / a
	}
	return 1 - front*statsBetaCF(1-x, b, a)/b
}

// Standard Lentz continued-fraction algorithm for the regularized incomplete beta.
func statsBetaCF(x, a, b float64) float64 {
	const tiny = 1e-300
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	dd := 1 - qab*x/qap
	if math.Abs(dd) < tiny {
		dd = tiny
	}
	dd = 1 / dd
	h := dd
	for m := 1; m < 2000; m++ {
		mf := float64(m)
		m2 := 2 * mf
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		dd = 1 + aa*dd
		if math.Abs(dd) < tiny {
			dd = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		dd = 1 / dd
		h *= dd * c
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		dd = 1 + aa*dd
		if math.Abs(dd) < tiny {
			dd = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		dd = 1 / dd
		del := dd * c
		h *= del
		if math.Abs(del-1) < 1e-16 {
			break
		}
	}
	return h
}

// statsInvertCDF brackets and bisects cdf(x)=p for x in [lo, +inf).
func statsInvertCDF(cdf func(float64) float64, p, lo float64) float64 {
	hi := lo + 1
	for cdf(hi) < p && hi < 1e15 {
		hi = lo + (hi-lo)*2
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if cdf(mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// statsSampleGamma draws a Gamma(shape, scale) variate (Marsaglia-Tsang).
func statsSampleGamma(shape, scale float64, rng *mrand.Rand) float64 {
	if shape < 1 {
		u := rng.Float64()
		return statsSampleGamma(shape+1, scale, rng) * math.Pow(u, 1/shape)
	}
	dd := shape - 1.0/3.0
	cc := 1 / math.Sqrt(9*dd)
	for {
		x := rng.NormFloat64()
		v := 1 + cc*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x {
			return dd * v * scale
		}
		if math.Log(u) < 0.5*x*x+dd*(1-v+math.Log(v)) {
			return dd * v * scale
		}
	}
}

// statsPutFloat adds a float entry to a result dict.
func statsPutFloat(d *runtime.Dict, key string, v float64) {
	k := runtime.String{Value: key}
	d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: runtime.Float{Value: v}})
}

func statsSampleMean(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func statsSampleVariance(xs []float64, ddof int) float64 {
	m := statsSampleMean(xs)
	ss := 0.0
	for _, x := range xs {
		d := x - m
		ss += d * d
	}
	return ss / float64(len(xs)-ddof)
}

func statsAlternative(opts runtime.Value) (string, error) {
	if opts == nil {
		return "two-sided", nil
	}
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return "", fmt.Errorf("options must be a dict")
	}
	v, ok := ndDictValue(dict, "alternative")
	if !ok {
		return "two-sided", nil
	}
	s, ok := v.(runtime.String)
	if !ok {
		return "", fmt.Errorf("alternative must be a string")
	}
	switch s.Value {
	case "two-sided", "less", "greater":
		return s.Value, nil
	}
	return "", fmt.Errorf("alternative must be 'two-sided', 'less', or 'greater'")
}

func statsBoolOpt(opts runtime.Value, key string, def bool) (bool, error) {
	if opts == nil {
		return def, nil
	}
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return def, fmt.Errorf("options must be a dict")
	}
	v, ok := ndDictValue(dict, key)
	if !ok {
		return def, nil
	}
	b, ok := v.(runtime.Bool)
	if !ok {
		return def, fmt.Errorf("%s must be a bool", key)
	}
	return b.Value, nil
}

func statsIntervalResult(low, high float64) runtime.Value {
	d := runtime.NewDictHint(2)
	statsPutFloat(&d, "low", low)
	statsPutFloat(&d, "high", high)
	return d
}

// statsRanks returns 1-based average ranks plus the sizes of each tie group.
func statsRanks(xs []float64) ([]float64, []int) {
	n := len(xs)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return xs[idx[a]] < xs[idx[b]] })
	ranks := make([]float64, n)
	ties := []int{}
	i := 0
	for i < n {
		j := i
		for j+1 < n && xs[idx[j+1]] == xs[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[idx[k]] = avg
		}
		if j > i {
			ties = append(ties, j-i+1)
		}
		i = j + 1
	}
	return ranks, ties
}

// statsKSProb is the Kolmogorov survival series Q_KS(lambda).
func statsKSProb(lambda float64) float64 {
	if lambda <= 0 {
		return 1
	}
	a := -2 * lambda * lambda
	sum := 0.0
	sign := 1.0
	for j := 1; j <= 100; j++ {
		term := sign * math.Exp(a*float64(j)*float64(j))
		sum += term
		if math.Abs(term) < 1e-12 {
			break
		}
		sign = -sign
	}
	p := 2 * sum
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	return p
}

func statsTTestResult(tStat, df float64, alternative string) runtime.Value {
	dist := &runtime.Distribution{Kind: "studentT", Params: []float64{df}}
	var p float64
	switch alternative {
	case "less":
		p = statsCdf(dist, tStat)
	case "greater":
		p = 1 - statsCdf(dist, tStat)
	default:
		p = 2 * (1 - statsCdf(dist, math.Abs(tStat)))
	}
	d := runtime.NewDictHint(3)
	statsPutFloat(&d, "statistic", tStat)
	statsPutFloat(&d, "pvalue", p)
	statsPutFloat(&d, "df", df)
	return d
}

func statsChiResult(stat, df float64) runtime.Value {
	dist := &runtime.Distribution{Kind: "chiSquared", Params: []float64{df}}
	d := runtime.NewDictHint(3)
	statsPutFloat(&d, "statistic", stat)
	statsPutFloat(&d, "pvalue", 1-statsCdf(dist, stat))
	statsPutFloat(&d, "df", df)
	return d
}

func statsFloatMatrix(v runtime.Value, label string) ([][]float64, error) {
	list, ok := v.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s: argument must be a list of lists", label)
	}
	rows := make([][]float64, len(list.Elements))
	for i, row := range list.Elements {
		r, err := mathNumericListSingle(row, fmt.Sprintf("%s row %d", label, i))
		if err != nil {
			return nil, err
		}
		rows[i] = r
	}
	return rows, nil
}

// statsSolveLinear solves a*x = b by Gauss-Jordan elimination with partial pivoting; ok=false if singular.
func statsSolveLinear(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	m := make([][]float64, n)
	for i := 0; i < n; i++ {
		m[i] = make([]float64, n+1)
		copy(m[i], a[i])
		m[i][n] = b[i]
	}
	for col := 0; col < n; col++ {
		piv := col
		for row := col + 1; row < n; row++ {
			if math.Abs(m[row][col]) > math.Abs(m[piv][col]) {
				piv = row
			}
		}
		if math.Abs(m[piv][col]) < 1e-12 {
			return nil, false
		}
		m[col], m[piv] = m[piv], m[col]
		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := m[row][col] / m[col][col]
			for c := col; c <= n; c++ {
				m[row][c] -= factor * m[col][c]
			}
		}
	}
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = m[i][n] / m[i][i]
	}
	return x, true
}

func registerStats(r *Registry) {
	r.Register("stats", "binomial", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.binomial expects (n, p)")
		}
		nv, ok := AsInt64(args[0])
		if !ok || nv < 0 {
			return nil, fmt.Errorf("stats.binomial: n must be a non-negative int")
		}
		p, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("stats.binomial: p must be in [0, 1]")
		}
		return &runtime.Distribution{Kind: "binomial", Params: []float64{float64(nv), p}}, nil
	})
	r.Register("stats", "poisson", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.poisson expects (lambda)")
		}
		lambda, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		if lambda <= 0 {
			return nil, fmt.Errorf("stats.poisson: lambda must be positive")
		}
		return &runtime.Distribution{Kind: "poisson", Params: []float64{lambda}}, nil
	})
	r.Register("stats", "normal", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.normal expects (mu, sigma)")
		}
		mu, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		sigma, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		if sigma <= 0 {
			return nil, fmt.Errorf("stats.normal: sigma must be positive")
		}
		return &runtime.Distribution{Kind: "normal", Params: []float64{mu, sigma}}, nil
	})
	r.Register("stats", "uniform", func(args []runtime.Value) (runtime.Value, error) {
		a, b, err := statsTwoFloats(args, "stats.uniform", "(a, b)")
		if err != nil {
			return nil, err
		}
		if a >= b {
			return nil, fmt.Errorf("stats.uniform: require a < b")
		}
		return &runtime.Distribution{Kind: "uniform", Params: []float64{a, b}}, nil
	})
	r.Register("stats", "exponential", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.exponential expects (rate)")
		}
		rate, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		if rate <= 0 {
			return nil, fmt.Errorf("stats.exponential: rate must be positive")
		}
		return &runtime.Distribution{Kind: "exponential", Params: []float64{rate}}, nil
	})
	r.Register("stats", "lognormal", func(args []runtime.Value) (runtime.Value, error) {
		mu, sigma, err := statsTwoFloats(args, "stats.lognormal", "(mu, sigma)")
		if err != nil {
			return nil, err
		}
		if sigma <= 0 {
			return nil, fmt.Errorf("stats.lognormal: sigma must be positive")
		}
		return &runtime.Distribution{Kind: "lognormal", Params: []float64{mu, sigma}}, nil
	})
	r.Register("stats", "weibull", func(args []runtime.Value) (runtime.Value, error) {
		shape, scale, err := statsTwoFloats(args, "stats.weibull", "(shape, scale)")
		if err != nil {
			return nil, err
		}
		if shape <= 0 || scale <= 0 {
			return nil, fmt.Errorf("stats.weibull: shape and scale must be positive")
		}
		return &runtime.Distribution{Kind: "weibull", Params: []float64{shape, scale}}, nil
	})
	r.Register("stats", "gamma", func(args []runtime.Value) (runtime.Value, error) {
		shape, scale, err := statsTwoFloats(args, "stats.gamma", "(shape, scale)")
		if err != nil {
			return nil, err
		}
		if shape <= 0 || scale <= 0 {
			return nil, fmt.Errorf("stats.gamma: shape and scale must be positive")
		}
		return &runtime.Distribution{Kind: "gamma", Params: []float64{shape, scale}}, nil
	})
	r.Register("stats", "chiSquared", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.chiSquared expects (df)")
		}
		df, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		if df <= 0 {
			return nil, fmt.Errorf("stats.chiSquared: df must be positive")
		}
		return &runtime.Distribution{Kind: "chiSquared", Params: []float64{df}}, nil
	})
	r.Register("stats", "beta", func(args []runtime.Value) (runtime.Value, error) {
		a, b, err := statsTwoFloats(args, "stats.beta", "(alpha, beta)")
		if err != nil {
			return nil, err
		}
		if a <= 0 || b <= 0 {
			return nil, fmt.Errorf("stats.beta: alpha and beta must be positive")
		}
		return &runtime.Distribution{Kind: "beta", Params: []float64{a, b}}, nil
	})
	r.Register("stats", "studentT", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.studentT expects (df)")
		}
		df, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		if df <= 0 {
			return nil, fmt.Errorf("stats.studentT: df must be positive")
		}
		return &runtime.Distribution{Kind: "studentT", Params: []float64{df}}, nil
	})
	r.Register("stats", "f", func(args []runtime.Value) (runtime.Value, error) {
		d1, d2, err := statsTwoFloats(args, "stats.f", "(d1, d2)")
		if err != nil {
			return nil, err
		}
		if d1 <= 0 || d2 <= 0 {
			return nil, fmt.Errorf("stats.f: d1 and d2 must be positive")
		}
		return &runtime.Distribution{Kind: "f", Params: []float64{d1, d2}}, nil
	})
	r.Register("stats", "tTestOneSample", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("stats.tTestOneSample expects (sample, mu, opts?)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.tTestOneSample sample")
		if err != nil {
			return nil, err
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.tTestOneSample: sample needs at least 2 values")
		}
		mu, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		var opts runtime.Value
		if len(args) == 3 {
			opts = args[2]
		}
		alt, err := statsAlternative(opts)
		if err != nil {
			return nil, err
		}
		n := float64(len(xs))
		m := statsSampleMean(xs)
		s := math.Sqrt(statsSampleVariance(xs, 1))
		tStat := (m - mu) / (s / math.Sqrt(n))
		return statsTTestResult(tStat, n-1, alt), nil
	})
	r.Register("stats", "tTestIndependent", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("stats.tTestIndependent expects (a, b, opts?)")
		}
		a, err := mathNumericListSingle(args[0], "stats.tTestIndependent a")
		if err != nil {
			return nil, err
		}
		b, err := mathNumericListSingle(args[1], "stats.tTestIndependent b")
		if err != nil {
			return nil, err
		}
		if len(a) < 2 || len(b) < 2 {
			return nil, fmt.Errorf("stats.tTestIndependent: each sample needs at least 2 values")
		}
		var opts runtime.Value
		if len(args) == 3 {
			opts = args[2]
		}
		alt, err := statsAlternative(opts)
		if err != nil {
			return nil, err
		}
		equalVar, err := statsBoolOpt(opts, "equalVar", true)
		if err != nil {
			return nil, err
		}
		n1, n2 := float64(len(a)), float64(len(b))
		m1, m2 := statsSampleMean(a), statsSampleMean(b)
		v1, v2 := statsSampleVariance(a, 1), statsSampleVariance(b, 1)
		var tStat, df float64
		if equalVar {
			sp2 := ((n1-1)*v1 + (n2-1)*v2) / (n1 + n2 - 2)
			tStat = (m1 - m2) / math.Sqrt(sp2*(1/n1+1/n2))
			df = n1 + n2 - 2
		} else {
			se2 := v1/n1 + v2/n2
			tStat = (m1 - m2) / math.Sqrt(se2)
			df = se2 * se2 / ((v1/n1)*(v1/n1)/(n1-1) + (v2/n2)*(v2/n2)/(n2-1))
		}
		return statsTTestResult(tStat, df, alt), nil
	})
	r.Register("stats", "tTestPaired", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("stats.tTestPaired expects (a, b, opts?)")
		}
		a, err := mathNumericListSingle(args[0], "stats.tTestPaired a")
		if err != nil {
			return nil, err
		}
		b, err := mathNumericListSingle(args[1], "stats.tTestPaired b")
		if err != nil {
			return nil, err
		}
		if len(a) != len(b) {
			return nil, fmt.Errorf("stats.tTestPaired: samples must have equal length")
		}
		if len(a) < 2 {
			return nil, fmt.Errorf("stats.tTestPaired: samples need at least 2 values")
		}
		var opts runtime.Value
		if len(args) == 3 {
			opts = args[2]
		}
		alt, err := statsAlternative(opts)
		if err != nil {
			return nil, err
		}
		diffs := make([]float64, len(a))
		for i := range a {
			diffs[i] = a[i] - b[i]
		}
		n := float64(len(diffs))
		m := statsSampleMean(diffs)
		s := math.Sqrt(statsSampleVariance(diffs, 1))
		tStat := m / (s / math.Sqrt(n))
		return statsTTestResult(tStat, n-1, alt), nil
	})
	r.Register("stats", "chiSquareTest", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 3 {
			return nil, fmt.Errorf("stats.chiSquareTest expects (observed, expected?, opts?)")
		}
		obs, err := mathNumericListSingle(args[0], "stats.chiSquareTest observed")
		if err != nil {
			return nil, err
		}
		if len(obs) < 2 {
			return nil, fmt.Errorf("stats.chiSquareTest: need at least 2 categories")
		}
		var exp []float64
		var opts runtime.Value
		for _, a := range args[1:] {
			if dict, ok := a.(runtime.Dict); ok {
				opts = dict
				continue
			}
			exp, err = mathNumericListSingle(a, "stats.chiSquareTest expected")
			if err != nil {
				return nil, err
			}
		}
		ddof := 0
		if opts != nil {
			if v, ok := ndDictValue(opts.(runtime.Dict), "ddof"); ok {
				n, ok := AsInt64(v)
				if !ok {
					return nil, fmt.Errorf("stats.chiSquareTest: ddof must be an int")
				}
				ddof = int(n)
			}
		}
		if len(exp) == 0 {
			total := 0.0
			for _, o := range obs {
				total += o
			}
			uniform := total / float64(len(obs))
			exp = make([]float64, len(obs))
			for i := range exp {
				exp[i] = uniform
			}
		} else if len(exp) != len(obs) {
			return nil, fmt.Errorf("stats.chiSquareTest: expected length must match observed")
		}
		stat := 0.0
		for i := range obs {
			if exp[i] <= 0 {
				return nil, fmt.Errorf("stats.chiSquareTest: expected counts must be positive")
			}
			d := obs[i] - exp[i]
			stat += d * d / exp[i]
		}
		return statsChiResult(stat, float64(len(obs)-1-ddof)), nil
	})
	r.Register("stats", "mannWhitneyU", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("stats.mannWhitneyU expects (a, b, opts?)")
		}
		a, err := mathNumericListSingle(args[0], "stats.mannWhitneyU a")
		if err != nil {
			return nil, err
		}
		b, err := mathNumericListSingle(args[1], "stats.mannWhitneyU b")
		if err != nil {
			return nil, err
		}
		if len(a) == 0 || len(b) == 0 {
			return nil, fmt.Errorf("stats.mannWhitneyU: samples must be non-empty")
		}
		var opts runtime.Value
		if len(args) == 3 {
			opts = args[2]
		}
		alt, err := statsAlternative(opts)
		if err != nil {
			return nil, err
		}
		n1, n2 := len(a), len(b)
		combined := make([]float64, 0, n1+n2)
		combined = append(combined, a...)
		combined = append(combined, b...)
		ranks, ties := statsRanks(combined)
		r1 := 0.0
		for i := 0; i < n1; i++ {
			r1 += ranks[i]
		}
		u1 := r1 - float64(n1)*float64(n1+1)/2
		nn := float64(n1 + n2)
		muU := float64(n1) * float64(n2) / 2
		tieSum := 0.0
		for _, t := range ties {
			tf := float64(t)
			tieSum += tf*tf*tf - tf
		}
		sigmaU := math.Sqrt(float64(n1) * float64(n2) / 12 * ((nn + 1) - tieSum/(nn*(nn-1))))
		z := 0.0
		if sigmaU > 0 {
			diff := u1 - muU
			if diff > 0 {
				diff -= 0.5
			} else if diff < 0 {
				diff += 0.5
			}
			z = diff / sigmaU
		}
		norm := &runtime.Distribution{Kind: "normal", Params: []float64{0, 1}}
		var p float64
		switch alt {
		case "less":
			p = statsCdf(norm, z)
		case "greater":
			p = 1 - statsCdf(norm, z)
		default:
			p = 2 * (1 - statsCdf(norm, math.Abs(z)))
		}
		if p > 1 {
			p = 1
		}
		d := runtime.NewDictHint(2)
		statsPutFloat(&d, "statistic", u1)
		statsPutFloat(&d, "pvalue", p)
		return d, nil
	})
	r.Register("stats", "ksTest", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.ksTest expects (a, b)")
		}
		a, err := mathNumericListSingle(args[0], "stats.ksTest a")
		if err != nil {
			return nil, err
		}
		b, err := mathNumericListSingle(args[1], "stats.ksTest b")
		if err != nil {
			return nil, err
		}
		if len(a) == 0 || len(b) == 0 {
			return nil, fmt.Errorf("stats.ksTest: samples must be non-empty")
		}
		sa := append([]float64(nil), a...)
		sb := append([]float64(nil), b...)
		sort.Float64s(sa)
		sort.Float64s(sb)
		n1, n2 := len(sa), len(sb)
		i, j := 0, 0
		dstat := 0.0
		for i < n1 && j < n2 {
			x := math.Min(sa[i], sb[j])
			for i < n1 && sa[i] <= x {
				i++
			}
			for j < n2 && sb[j] <= x {
				j++
			}
			gap := math.Abs(float64(i)/float64(n1) - float64(j)/float64(n2))
			if gap > dstat {
				dstat = gap
			}
		}
		en := math.Sqrt(float64(n1) * float64(n2) / float64(n1+n2))
		d := runtime.NewDictHint(2)
		statsPutFloat(&d, "statistic", dstat)
		statsPutFloat(&d, "pvalue", statsKSProb((en+0.12+0.11/en)*dstat))
		return d, nil
	})
	r.Register("stats", "confidenceIntervalMean", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("stats.confidenceIntervalMean expects (sample, level?)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.confidenceIntervalMean sample")
		if err != nil {
			return nil, err
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.confidenceIntervalMean: sample needs at least 2 values")
		}
		level := 0.95
		if len(args) == 2 {
			level, err = FloatLike(args[1])
			if err != nil {
				return nil, err
			}
		}
		if level <= 0 || level >= 1 {
			return nil, fmt.Errorf("stats.confidenceIntervalMean: level must be in (0, 1)")
		}
		n := float64(len(xs))
		m := statsSampleMean(xs)
		s := math.Sqrt(statsSampleVariance(xs, 1))
		tdist := &runtime.Distribution{Kind: "studentT", Params: []float64{n - 1}}
		tc := statsPpf(tdist, 1-(1-level)/2)
		margin := tc * s / math.Sqrt(n)
		return statsIntervalResult(m-margin, m+margin), nil
	})
	r.Register("stats", "confidenceIntervalProportion", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("stats.confidenceIntervalProportion expects (successes, n, level?)")
		}
		k, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("stats.confidenceIntervalProportion: successes must be an int")
		}
		nn, ok := AsInt64(args[1])
		if !ok || nn <= 0 {
			return nil, fmt.Errorf("stats.confidenceIntervalProportion: n must be a positive int")
		}
		if k < 0 || k > nn {
			return nil, fmt.Errorf("stats.confidenceIntervalProportion: successes must be in [0, n]")
		}
		level := 0.95
		var err error
		if len(args) == 3 {
			level, err = FloatLike(args[2])
			if err != nil {
				return nil, err
			}
		}
		if level <= 0 || level >= 1 {
			return nil, fmt.Errorf("stats.confidenceIntervalProportion: level must be in (0, 1)")
		}
		ph := float64(k) / float64(nn)
		se := math.Sqrt(ph * (1 - ph) / float64(nn))
		norm := &runtime.Distribution{Kind: "normal", Params: []float64{0, 1}}
		zc := statsPpf(norm, 1-(1-level)/2)
		margin := zc * se
		return statsIntervalResult(ph-margin, ph+margin), nil
	})
	r.Register("stats", "confidenceIntervalDiffMeans", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 4 {
			return nil, fmt.Errorf("stats.confidenceIntervalDiffMeans expects (a, b, level?, opts?)")
		}
		a, err := mathNumericListSingle(args[0], "stats.confidenceIntervalDiffMeans a")
		if err != nil {
			return nil, err
		}
		b, err := mathNumericListSingle(args[1], "stats.confidenceIntervalDiffMeans b")
		if err != nil {
			return nil, err
		}
		if len(a) < 2 || len(b) < 2 {
			return nil, fmt.Errorf("stats.confidenceIntervalDiffMeans: each sample needs at least 2 values")
		}
		level := 0.95
		var opts runtime.Value
		for _, arg := range args[2:] {
			if dict, ok := arg.(runtime.Dict); ok {
				opts = dict
				continue
			}
			level, err = FloatLike(arg)
			if err != nil {
				return nil, err
			}
		}
		if level <= 0 || level >= 1 {
			return nil, fmt.Errorf("stats.confidenceIntervalDiffMeans: level must be in (0, 1)")
		}
		equalVar, err := statsBoolOpt(opts, "equalVar", true)
		if err != nil {
			return nil, err
		}
		n1, n2 := float64(len(a)), float64(len(b))
		m1, m2 := statsSampleMean(a), statsSampleMean(b)
		v1, v2 := statsSampleVariance(a, 1), statsSampleVariance(b, 1)
		var se, df float64
		if equalVar {
			sp2 := ((n1-1)*v1 + (n2-1)*v2) / (n1 + n2 - 2)
			se = math.Sqrt(sp2 * (1/n1 + 1/n2))
			df = n1 + n2 - 2
		} else {
			se2 := v1/n1 + v2/n2
			se = math.Sqrt(se2)
			df = se2 * se2 / ((v1/n1)*(v1/n1)/(n1-1) + (v2/n2)*(v2/n2)/(n2-1))
		}
		tdist := &runtime.Distribution{Kind: "studentT", Params: []float64{df}}
		tc := statsPpf(tdist, 1-(1-level)/2)
		diff := m1 - m2
		margin := tc * se
		return statsIntervalResult(diff-margin, diff+margin), nil
	})
	r.Register("stats", "linregress", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.linregress expects (x, y)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.linregress x")
		if err != nil {
			return nil, err
		}
		ys, err := mathNumericListSingle(args[1], "stats.linregress y")
		if err != nil {
			return nil, err
		}
		if len(xs) != len(ys) {
			return nil, fmt.Errorf("stats.linregress: x and y must have equal length")
		}
		n := len(xs)
		if n < 3 {
			return nil, fmt.Errorf("stats.linregress: need at least 3 points")
		}
		mx := statsSampleMean(xs)
		my := statsSampleMean(ys)
		sxx, sxy, syy := 0.0, 0.0, 0.0
		for i := 0; i < n; i++ {
			dx := xs[i] - mx
			dy := ys[i] - my
			sxx += dx * dx
			sxy += dx * dy
			syy += dy * dy
		}
		if sxx == 0 {
			return nil, fmt.Errorf("stats.linregress: x has zero variance")
		}
		nf := float64(n)
		slope := sxy / sxx
		intercept := my - slope*mx
		rval := 0.0
		if syy > 0 {
			rval = sxy / math.Sqrt(sxx*syy)
		}
		r2 := rval * rval
		ssres := math.Max(0, syy-slope*sxy)
		stderr := math.Sqrt((ssres / (nf - 2)) / sxx)
		pvalue := 0.0
		if r2 < 1 {
			tstat := rval * math.Sqrt((nf-2)/(1-r2))
			dist := &runtime.Distribution{Kind: "studentT", Params: []float64{nf - 2}}
			pvalue = 2 * (1 - statsCdf(dist, math.Abs(tstat)))
		}
		d := runtime.NewDictHint(6)
		statsPutFloat(&d, "slope", slope)
		statsPutFloat(&d, "intercept", intercept)
		statsPutFloat(&d, "r", rval)
		statsPutFloat(&d, "r2", r2)
		statsPutFloat(&d, "pvalue", pvalue)
		statsPutFloat(&d, "stderr", stderr)
		return d, nil
	})
	r.Register("stats", "polyfit", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("stats.polyfit expects (x, y, degree)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.polyfit x")
		if err != nil {
			return nil, err
		}
		ys, err := mathNumericListSingle(args[1], "stats.polyfit y")
		if err != nil {
			return nil, err
		}
		if len(xs) != len(ys) {
			return nil, fmt.Errorf("stats.polyfit: x and y must have equal length")
		}
		deg, ok := AsInt64(args[2])
		if !ok || deg < 1 || deg > 10 {
			return nil, fmt.Errorf("stats.polyfit: degree must be an int in [1, 10]")
		}
		d := int(deg)
		if len(xs) < d+1 {
			return nil, fmt.Errorf("stats.polyfit: need at least degree+1 points")
		}
		dim := d + 1
		powSums := make([]float64, 2*d+1)
		for _, x := range xs {
			p := 1.0
			for k := 0; k <= 2*d; k++ {
				powSums[k] += p
				p *= x
			}
		}
		a := make([][]float64, dim)
		for j := 0; j < dim; j++ {
			a[j] = make([]float64, dim)
			for k := 0; k < dim; k++ {
				a[j][k] = powSums[j+k]
			}
		}
		c := make([]float64, dim)
		for i := range xs {
			p := 1.0
			for j := 0; j < dim; j++ {
				c[j] += ys[i] * p
				p *= xs[i]
			}
		}
		sol, ok2 := statsSolveLinear(a, c)
		if !ok2 {
			return nil, fmt.Errorf("stats.polyfit: singular system (cannot fit)")
		}
		out := make([]runtime.Value, dim)
		for i := 0; i < dim; i++ {
			out[i] = runtime.Float{Value: sol[dim-1-i]}
		}
		return &runtime.List{Elements: out}, nil
	})
	r.Register("stats", "polyval", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.polyval expects (coeffs, x)")
		}
		coeffs, err := mathNumericListSingle(args[0], "stats.polyval coeffs")
		if err != nil {
			return nil, err
		}
		if len(coeffs) == 0 {
			return nil, fmt.Errorf("stats.polyval: coeffs must be non-empty")
		}
		x, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		acc := 0.0
		for _, coef := range coeffs {
			acc = acc*x + coef
		}
		return runtime.Float{Value: acc}, nil
	})
	r.Register("stats", "skewness", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.skewness expects (xs)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.skewness xs")
		if err != nil {
			return nil, err
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.skewness: need at least 2 values")
		}
		mean := statsSampleMean(xs)
		m2, m3 := 0.0, 0.0
		for _, x := range xs {
			d := x - mean
			m2 += d * d
			m3 += d * d * d
		}
		n := float64(len(xs))
		m2 /= n
		m3 /= n
		if m2 == 0 {
			return nil, fmt.Errorf("stats.skewness: zero variance")
		}
		return runtime.Float{Value: m3 / math.Pow(m2, 1.5)}, nil
	})
	r.Register("stats", "kurtosis", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.kurtosis expects (xs)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.kurtosis xs")
		if err != nil {
			return nil, err
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.kurtosis: need at least 2 values")
		}
		mean := statsSampleMean(xs)
		m2, m4 := 0.0, 0.0
		for _, x := range xs {
			d := x - mean
			dd := d * d
			m2 += dd
			m4 += dd * dd
		}
		n := float64(len(xs))
		m2 /= n
		m4 /= n
		if m2 == 0 {
			return nil, fmt.Errorf("stats.kurtosis: zero variance")
		}
		return runtime.Float{Value: m4/(m2*m2) - 3}, nil
	})
	r.Register("stats", "covariance", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.covariance expects (xs, ys)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.covariance xs")
		if err != nil {
			return nil, err
		}
		ys, err := mathNumericListSingle(args[1], "stats.covariance ys")
		if err != nil {
			return nil, err
		}
		if len(xs) != len(ys) {
			return nil, fmt.Errorf("stats.covariance: xs and ys must have equal length")
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.covariance: need at least 2 values")
		}
		mx := statsSampleMean(xs)
		my := statsSampleMean(ys)
		sxy := 0.0
		for i := range xs {
			sxy += (xs[i] - mx) * (ys[i] - my)
		}
		return runtime.Float{Value: sxy / (float64(len(xs)) - 1)}, nil
	})
	r.Register("stats", "corrcoef", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.corrcoef expects (xs, ys)")
		}
		xs, err := mathNumericListSingle(args[0], "stats.corrcoef xs")
		if err != nil {
			return nil, err
		}
		ys, err := mathNumericListSingle(args[1], "stats.corrcoef ys")
		if err != nil {
			return nil, err
		}
		if len(xs) != len(ys) {
			return nil, fmt.Errorf("stats.corrcoef: xs and ys must have equal length")
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.corrcoef: need at least 2 values")
		}
		mx := statsSampleMean(xs)
		my := statsSampleMean(ys)
		sxx, sxy, syy := 0.0, 0.0, 0.0
		for i := range xs {
			dx := xs[i] - mx
			dy := ys[i] - my
			sxx += dx * dx
			sxy += dx * dy
			syy += dy * dy
		}
		if sxx == 0 || syy == 0 {
			return nil, fmt.Errorf("stats.corrcoef: zero variance in xs or ys")
		}
		return runtime.Float{Value: sxy / math.Sqrt(sxx*syy)}, nil
	})
	r.Register("stats", "chiSquareIndependence", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.chiSquareIndependence expects (table)")
		}
		table, err := statsFloatMatrix(args[0], "stats.chiSquareIndependence table")
		if err != nil {
			return nil, err
		}
		rows := len(table)
		if rows < 2 {
			return nil, fmt.Errorf("stats.chiSquareIndependence: need at least 2 rows")
		}
		cols := len(table[0])
		if cols < 2 {
			return nil, fmt.Errorf("stats.chiSquareIndependence: need at least 2 columns")
		}
		for _, row := range table {
			if len(row) != cols {
				return nil, fmt.Errorf("stats.chiSquareIndependence: table must be rectangular")
			}
		}
		rowSums := make([]float64, rows)
		colSums := make([]float64, cols)
		total := 0.0
		for i := 0; i < rows; i++ {
			for j := 0; j < cols; j++ {
				rowSums[i] += table[i][j]
				colSums[j] += table[i][j]
				total += table[i][j]
			}
		}
		if total <= 0 {
			return nil, fmt.Errorf("stats.chiSquareIndependence: table total must be positive")
		}
		stat := 0.0
		expData := make([]runtime.Value, rows)
		for i := 0; i < rows; i++ {
			rowVals := make([]runtime.Value, cols)
			for j := 0; j < cols; j++ {
				e := rowSums[i] * colSums[j] / total
				if e <= 0 {
					return nil, fmt.Errorf("stats.chiSquareIndependence: expected counts must be positive")
				}
				d := table[i][j] - e
				stat += d * d / e
				rowVals[j] = runtime.Float{Value: e}
			}
			expData[i] = &runtime.List{Elements: rowVals}
		}
		df := float64((rows - 1) * (cols - 1))
		res := statsChiResult(stat, df).(runtime.Dict)
		ek := runtime.String{Value: "expected"}
		res.PutEntry(DictKey(ek), runtime.DictEntry{Key: ek, Value: &runtime.List{Elements: expData}})
		return res, nil
	})
}

// DistributionMethod dispatches a method call on a Distribution; shared by both backends.
func DistributionMethod(d *runtime.Distribution, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "pdf":
		x, err := statsFloatArg(args, "pdf")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: statsPdf(d, x)}, nil
	case "cdf":
		x, err := statsFloatArg(args, "cdf")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: statsCdf(d, x)}, nil
	case "ppf":
		p, err := statsFloatArg(args, "ppf")
		if err != nil {
			return nil, err
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("ppf: p must be in [0, 1]")
		}
		return runtime.Float{Value: statsPpf(d, p)}, nil
	case "mean":
		if len(args) != 0 {
			return nil, fmt.Errorf("mean takes no arguments")
		}
		return runtime.Float{Value: statsMean(d)}, nil
	case "variance":
		if len(args) != 0 {
			return nil, fmt.Errorf("variance takes no arguments")
		}
		return runtime.Float{Value: statsVariance(d)}, nil
	case "std":
		if len(args) != 0 {
			return nil, fmt.Errorf("std takes no arguments")
		}
		return runtime.Float{Value: math.Sqrt(statsVariance(d))}, nil
	case "sample":
		return statsSample(d, args)
	default:
		return nil, fmt.Errorf("stats.Distribution has no method %s", name)
	}
}

func statsPdf(d *runtime.Distribution, x float64) float64 {
	switch d.Kind {
	case "binomial":
		n, p := d.Params[0], d.Params[1]
		if x < 0 || x > n || x != math.Trunc(x) {
			return 0
		}
		k := x
		if p == 0 {
			if k == 0 {
				return 1
			}
			return 0
		}
		if p == 1 {
			if k == n {
				return 1
			}
			return 0
		}
		lc1, _ := math.Lgamma(n + 1)
		lc2, _ := math.Lgamma(k + 1)
		lc3, _ := math.Lgamma(n - k + 1)
		return math.Exp(lc1 - lc2 - lc3 + k*math.Log(p) + (n-k)*math.Log(1-p))
	case "poisson":
		lambda := d.Params[0]
		if x < 0 || x != math.Trunc(x) {
			return 0
		}
		lg, _ := math.Lgamma(x + 1)
		return math.Exp(x*math.Log(lambda) - lambda - lg)
	case "normal":
		mu, sigma := d.Params[0], d.Params[1]
		z := (x - mu) / sigma
		return math.Exp(-0.5*z*z) / (sigma * math.Sqrt(2*math.Pi))
	case "uniform":
		a, b := d.Params[0], d.Params[1]
		if x < a || x > b {
			return 0
		}
		return 1 / (b - a)
	case "exponential":
		rate := d.Params[0]
		if x < 0 {
			return 0
		}
		return rate * math.Exp(-rate*x)
	case "lognormal":
		mu, sigma := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		z := (math.Log(x) - mu) / sigma
		return math.Exp(-0.5*z*z) / (x * sigma * math.Sqrt(2*math.Pi))
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		if x < 0 {
			return 0
		}
		return (k / lambda) * math.Pow(x/lambda, k-1) * math.Exp(-math.Pow(x/lambda, k))
	case "gamma":
		k, theta := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		lg, _ := math.Lgamma(k)
		return math.Exp((k-1)*math.Log(x) - x/theta - lg - k*math.Log(theta))
	case "chiSquared":
		df := d.Params[0]
		if x <= 0 {
			return 0
		}
		k, theta := df/2, 2.0
		lg, _ := math.Lgamma(k)
		return math.Exp((k-1)*math.Log(x) - x/theta - lg - k*math.Log(theta))
	case "beta":
		a, b := d.Params[0], d.Params[1]
		if x <= 0 || x >= 1 {
			return 0
		}
		la, _ := math.Lgamma(a)
		lb, _ := math.Lgamma(b)
		lab, _ := math.Lgamma(a + b)
		return math.Exp((a-1)*math.Log(x) + (b-1)*math.Log(1-x) + lab - la - lb)
	case "studentT":
		nu := d.Params[0]
		lg1, _ := math.Lgamma((nu + 1) / 2)
		lg2, _ := math.Lgamma(nu / 2)
		return math.Exp(lg1-lg2) / math.Sqrt(nu*math.Pi) * math.Pow(1+x*x/nu, -(nu+1)/2)
	case "f":
		d1, d2 := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		la, _ := math.Lgamma(d1 / 2)
		lb, _ := math.Lgamma(d2 / 2)
		lab, _ := math.Lgamma((d1 + d2) / 2)
		logp := lab - la - lb + (d1/2)*math.Log(d1/d2) + (d1/2-1)*math.Log(x) - ((d1+d2)/2)*math.Log(1+d1*x/d2)
		return math.Exp(logp)
	}
	return math.NaN()
}

func statsCdf(d *runtime.Distribution, x float64) float64 {
	switch d.Kind {
	case "binomial":
		n, p := d.Params[0], d.Params[1]
		if x < 0 {
			return 0
		}
		k := math.Floor(x)
		if k >= n {
			return 1
		}
		// P(X <= k) = I_{1-p}(n-k, k+1)
		return statsRegBetaI(1-p, n-k, k+1)
	case "poisson":
		lambda := d.Params[0]
		if x < 0 {
			return 0
		}
		k := math.Floor(x)
		// P(X <= k) = 1 - regularized gamma P(k+1, lambda)
		return 1 - statsRegGammaP(k+1, lambda)
	case "normal":
		mu, sigma := d.Params[0], d.Params[1]
		return 0.5 * math.Erfc(-(x-mu)/(sigma*math.Sqrt2))
	case "uniform":
		a, b := d.Params[0], d.Params[1]
		if x < a {
			return 0
		}
		if x > b {
			return 1
		}
		return (x - a) / (b - a)
	case "exponential":
		if x < 0 {
			return 0
		}
		return 1 - math.Exp(-d.Params[0]*x)
	case "lognormal":
		mu, sigma := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		return 0.5 * math.Erfc(-(math.Log(x)-mu)/(sigma*math.Sqrt2))
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		if x < 0 {
			return 0
		}
		return 1 - math.Exp(-math.Pow(x/lambda, k))
	case "gamma":
		k, theta := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		return statsRegGammaP(k, x/theta)
	case "chiSquared":
		df := d.Params[0]
		if x <= 0 {
			return 0
		}
		return statsRegGammaP(df/2, x/2)
	case "beta":
		return statsRegBetaI(x, d.Params[0], d.Params[1])
	case "studentT":
		nu := d.Params[0]
		ib := 0.5 * statsRegBetaI(nu/(nu+x*x), nu/2, 0.5)
		if x > 0 {
			return 1 - ib
		}
		return ib
	case "f":
		d1, d2 := d.Params[0], d.Params[1]
		if x <= 0 {
			return 0
		}
		return statsRegBetaI(d1*x/(d1*x+d2), d1/2, d2/2)
	}
	return math.NaN()
}

func statsPpf(d *runtime.Distribution, p float64) float64 {
	switch d.Kind {
	case "binomial", "poisson":
		if p == 0 {
			return 0
		}
		if p == 1 {
			if d.Kind == "binomial" {
				return d.Params[0]
			}
			return math.Inf(1)
		}
		k := 0.0
		for statsCdf(d, k) < p {
			k++
			if k > 1e9 {
				break
			}
		}
		return k
	case "normal":
		mu, sigma := d.Params[0], d.Params[1]
		if p == 0 {
			return math.Inf(-1)
		}
		if p == 1 {
			return math.Inf(1)
		}
		return mu + sigma*math.Sqrt2*math.Erfinv(2*p-1)
	case "uniform":
		a, b := d.Params[0], d.Params[1]
		return a + p*(b-a)
	case "exponential":
		if p == 1 {
			return math.Inf(1)
		}
		return -math.Log(1-p) / d.Params[0]
	case "lognormal":
		mu, sigma := d.Params[0], d.Params[1]
		if p == 0 {
			return 0
		}
		if p == 1 {
			return math.Inf(1)
		}
		return math.Exp(mu + sigma*math.Sqrt2*math.Erfinv(2*p-1))
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		if p == 1 {
			return math.Inf(1)
		}
		return lambda * math.Pow(-math.Log(1-p), 1/k)
	case "gamma", "chiSquared":
		if p == 0 {
			return 0
		}
		if p == 1 {
			return math.Inf(1)
		}
		return statsInvertCDF(func(x float64) float64 { return statsCdf(d, x) }, p, 0)
	case "beta":
		if p == 0 {
			return 0
		}
		if p == 1 {
			return 1
		}
		lo, hi := 0.0, 1.0
		for i := 0; i < 200; i++ {
			mid := (lo + hi) / 2
			if statsCdf(d, mid) < p {
				lo = mid
			} else {
				hi = mid
			}
		}
		return (lo + hi) / 2
	case "studentT":
		if p == 0 {
			return math.Inf(-1)
		}
		if p == 1 {
			return math.Inf(1)
		}
		if p == 0.5 {
			return 0
		}
		if p < 0.5 {
			return -statsPpf(d, 1-p)
		}
		return statsInvertCDF(func(x float64) float64 { return statsCdf(d, x) }, p, 0)
	case "f":
		if p == 0 {
			return 0
		}
		if p == 1 {
			return math.Inf(1)
		}
		return statsInvertCDF(func(x float64) float64 { return statsCdf(d, x) }, p, 0)
	}
	return math.NaN()
}

func statsMean(d *runtime.Distribution) float64 {
	switch d.Kind {
	case "binomial":
		return d.Params[0] * d.Params[1]
	case "poisson":
		return d.Params[0]
	case "normal":
		return d.Params[0]
	case "uniform":
		return (d.Params[0] + d.Params[1]) / 2
	case "exponential":
		return 1 / d.Params[0]
	case "lognormal":
		mu, sigma := d.Params[0], d.Params[1]
		return math.Exp(mu + sigma*sigma/2)
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		return lambda * math.Gamma(1+1/k)
	case "gamma":
		return d.Params[0] * d.Params[1]
	case "chiSquared":
		return d.Params[0]
	case "beta":
		a, b := d.Params[0], d.Params[1]
		return a / (a + b)
	case "studentT":
		nu := d.Params[0]
		if nu <= 1 {
			return math.NaN()
		}
		return 0
	case "f":
		d2 := d.Params[1]
		if d2 <= 2 {
			return math.NaN()
		}
		return d2 / (d2 - 2)
	}
	return math.NaN()
}

func statsVariance(d *runtime.Distribution) float64 {
	switch d.Kind {
	case "binomial":
		n, p := d.Params[0], d.Params[1]
		return n * p * (1 - p)
	case "poisson":
		return d.Params[0]
	case "normal":
		s := d.Params[1]
		return s * s
	case "uniform":
		w := d.Params[1] - d.Params[0]
		return w * w / 12
	case "exponential":
		return 1 / (d.Params[0] * d.Params[0])
	case "lognormal":
		mu, sigma := d.Params[0], d.Params[1]
		return (math.Exp(sigma*sigma) - 1) * math.Exp(2*mu+sigma*sigma)
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		g1 := math.Gamma(1 + 1/k)
		g2 := math.Gamma(1 + 2/k)
		return lambda * lambda * (g2 - g1*g1)
	case "gamma":
		return d.Params[0] * d.Params[1] * d.Params[1]
	case "chiSquared":
		return 2 * d.Params[0]
	case "beta":
		a, b := d.Params[0], d.Params[1]
		return a * b / ((a + b) * (a + b) * (a + b + 1))
	case "studentT":
		nu := d.Params[0]
		if nu <= 2 {
			return math.NaN()
		}
		return nu / (nu - 2)
	case "f":
		d1, d2 := d.Params[0], d.Params[1]
		if d2 <= 4 {
			return math.NaN()
		}
		return 2 * d2 * d2 * (d1 + d2 - 2) / (d1 * (d2 - 2) * (d2 - 2) * (d2 - 4))
	}
	return math.NaN()
}

// statsRNG returns a seeded RNG from an optional opts dict {"seed": k}.
func statsRNG(opts runtime.Value) (*mrand.Rand, error) {
	seed := time.Now().UnixNano()
	if opts != nil {
		dict, ok := opts.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("sample options must be a dict")
		}
		if v, ok := ndDictValue(dict, "seed"); ok {
			n, ok := AsInt64(v)
			if !ok {
				return nil, fmt.Errorf("sample seed must be an int")
			}
			seed = n
		}
	}
	return mrand.New(mrand.NewSource(seed)), nil //nolint:gosec
}

func statsDrawOne(d *runtime.Distribution, rng *mrand.Rand) float64 {
	switch d.Kind {
	case "binomial":
		n, p := int(d.Params[0]), d.Params[1]
		count := 0
		for i := 0; i < n; i++ {
			if rng.Float64() < p {
				count++
			}
		}
		return float64(count)
	case "poisson":
		lambda := d.Params[0]
		if lambda < 30 {
			l := math.Exp(-lambda)
			k := 0
			prod := 1.0
			for {
				k++
				prod *= rng.Float64()
				if prod <= l {
					return float64(k - 1)
				}
			}
		}
		// large lambda: inverse-CDF from a normal-approx starting point
		u := rng.Float64()
		k := math.Max(0, math.Floor(lambda+math.Sqrt(lambda)*rng.NormFloat64()))
		for statsCdf(d, k) < u {
			k++
		}
		for k > 0 && statsCdf(d, k-1) >= u {
			k--
		}
		return k
	case "normal":
		return rng.NormFloat64()*d.Params[1] + d.Params[0]
	case "uniform":
		a, b := d.Params[0], d.Params[1]
		return a + rng.Float64()*(b-a)
	case "exponential":
		return rng.ExpFloat64() / d.Params[0]
	case "lognormal":
		return math.Exp(rng.NormFloat64()*d.Params[1] + d.Params[0])
	case "weibull":
		k, lambda := d.Params[0], d.Params[1]
		return lambda * math.Pow(-math.Log(1-rng.Float64()), 1/k)
	case "gamma":
		return statsSampleGamma(d.Params[0], d.Params[1], rng)
	case "chiSquared":
		return statsSampleGamma(d.Params[0]/2, 2, rng)
	case "beta":
		a, b := d.Params[0], d.Params[1]
		ga := statsSampleGamma(a, 1, rng)
		gb := statsSampleGamma(b, 1, rng)
		return ga / (ga + gb)
	case "studentT":
		nu := d.Params[0]
		z := rng.NormFloat64()
		v := statsSampleGamma(nu/2, 2, rng)
		return z / math.Sqrt(v/nu)
	case "f":
		d1, d2 := d.Params[0], d.Params[1]
		v1 := statsSampleGamma(d1/2, 2, rng)
		v2 := statsSampleGamma(d2/2, 2, rng)
		return (v1 / d1) / (v2 / d2)
	}
	return math.NaN()
}

func statsSample(d *runtime.Distribution, args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		rng, _ := statsRNG(nil)
		v := statsDrawOne(d, rng)
		if statsIsDiscrete(d.Kind) {
			return runtime.SmallInt{Value: int64(v)}, nil
		}
		return runtime.Float{Value: v}, nil
	}
	n, ok := AsInt64(args[0])
	if !ok || n < 0 {
		return nil, fmt.Errorf("sample(n): n must be a non-negative int")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("sample expects (n) or (n, opts)")
	}
	var opts runtime.Value
	if len(args) == 2 {
		opts = args[1]
	}
	rng, err := statsRNG(opts)
	if err != nil {
		return nil, err
	}
	count := int(n)
	if statsIsDiscrete(d.Kind) {
		out := make([]int64, count)
		for i := range out {
			out[i] = int64(statsDrawOne(d, rng))
		}
		return &runtime.NDArray{F64: nil, I64: out, Dtype: runtime.NDInt64,
			Shape: []int{count}, Strides: runtime.RowMajorStrides([]int{count})}, nil
	}
	out := make([]float64, count)
	for i := range out {
		out[i] = statsDrawOne(d, rng)
	}
	return &runtime.NDArray{F64: out, Dtype: runtime.NDFloat64,
		Shape: []int{count}, Strides: runtime.RowMajorStrides([]int{count})}, nil
}
