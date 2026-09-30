package native

import (
	"fmt"
	"geblang/internal/runtime"
	"math"
	"math/big"
	"sort"
)

// maxCombinatoricN bounds factorial/perm/comb input (memory/CPU safety).
const maxCombinatoricN = 100000

func registerMath(r *Registry) {
	r.Register("math", "abs", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.abs expects exactly one argument")
		}
		return NumericAbs(args[0])
	})
	r.Register("math", "min", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("math.min expects at least one argument")
		}
		return NumericBest(args, func(cmp int) bool { return cmp < 0 })
	})
	r.Register("math", "max", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("math.max expects at least one argument")
		}
		return NumericBest(args, func(cmp int) bool { return cmp > 0 })
	})
	r.Register("math", "clamp", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("math.clamp expects value, min, max")
		}
		if cmp, err := NumericCompare(args[1], args[2]); err != nil {
			return nil, err
		} else if cmp > 0 {
			return nil, fmt.Errorf("math.clamp min must be <= max")
		}
		if cmp, err := NumericCompare(args[0], args[1]); err != nil {
			return nil, err
		} else if cmp < 0 {
			return args[1], nil
		}
		if cmp, err := NumericCompare(args[0], args[2]); err != nil {
			return nil, err
		} else if cmp > 0 {
			return args[2], nil
		}
		return args[0], nil
	})
	r.Register("math", "lerp", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("math.lerp expects (a, b, t)")
		}
		rats, floats, err := interpOperands(args, "math.lerp")
		if err != nil {
			return nil, err
		}
		if floats != nil {
			a, b, t := floats[0], floats[1], floats[2]
			return runtime.Float{Value: a + (b-a)*t}, nil
		}
		a, b, t := rats[0], rats[1], rats[2]
		scaled := new(big.Rat).Mul(new(big.Rat).Sub(b, a), t)
		return runtime.Decimal{Value: new(big.Rat).Add(a, scaled)}, nil
	})
	r.Register("math", "remap", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 5 {
			return nil, fmt.Errorf("math.remap expects (x, inLow, inHigh, outLow, outHigh)")
		}
		rats, floats, err := interpOperands(args, "math.remap")
		if err != nil {
			return nil, err
		}
		if floats != nil {
			x, il, ih, ol, oh := floats[0], floats[1], floats[2], floats[3], floats[4]
			if ih == il {
				return nil, fmt.Errorf("math.remap: input range has zero width (inLow == inHigh)")
			}
			return runtime.Float{Value: ol + (x-il)*(oh-ol)/(ih-il)}, nil
		}
		x, il, ih, ol, oh := rats[0], rats[1], rats[2], rats[3], rats[4]
		den := new(big.Rat).Sub(ih, il)
		if den.Sign() == 0 {
			return nil, fmt.Errorf("math.remap: input range has zero width (inLow == inHigh)")
		}
		num := new(big.Rat).Mul(new(big.Rat).Sub(x, il), new(big.Rat).Sub(oh, ol))
		return runtime.Decimal{Value: new(big.Rat).Add(ol, new(big.Rat).Quo(num, den))}, nil
	})
	r.Register("math", "floor", func(args []runtime.Value) (runtime.Value, error) {
		return IntUnaryMath(args, math.Floor, "math.floor")
	})
	r.Register("math", "ceil", func(args []runtime.Value) (runtime.Value, error) {
		return IntUnaryMath(args, math.Ceil, "math.ceil")
	})
	r.Register("math", "round", func(args []runtime.Value) (runtime.Value, error) {
		return IntUnaryMath(args, math.Round, "math.round")
	})
	r.Register("math", "sqrt", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Sqrt, "math.sqrt")
	})
	r.Register("math", "sin", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Sin, "math.sin")
	})
	r.Register("math", "cos", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Cos, "math.cos")
	})
	r.Register("math", "tan", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Tan, "math.tan")
	})
	r.Register("math", "asin", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Asin, "math.asin")
	})
	r.Register("math", "acos", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Acos, "math.acos")
	})
	r.Register("math", "atan", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Atan, "math.atan")
	})
	r.Register("math", "atan2", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.atan2 expects exactly two arguments")
		}
		y, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		x, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: math.Atan2(y, x)}, nil
	})
	r.Register("math", "log", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Log, "math.log")
	})
	r.Register("math", "log10", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Log10, "math.log10")
	})
	r.Register("math", "exp", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Exp, "math.exp")
	})
	r.Register("math", "pow", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.pow expects exactly two arguments")
		}
		base, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		exponent, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: math.Pow(base, exponent)}, nil
	})
	r.Register("math", "pi", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.pi expects no arguments")
		}
		return runtime.Float{Value: math.Pi}, nil
	})
	r.Register("math", "e", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.e expects no arguments")
		}
		return runtime.Float{Value: math.E}, nil
	})
	r.Register("math", "tau", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.tau expects no arguments")
		}
		return runtime.Float{Value: 2 * math.Pi}, nil
	})
	r.Register("math", "ln2", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.ln2 expects no arguments")
		}
		return runtime.Float{Value: math.Ln2}, nil
	})
	r.Register("math", "ln10", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.ln10 expects no arguments")
		}
		return runtime.Float{Value: math.Log(10)}, nil
	})
	r.Register("math", "sqrt2", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.sqrt2 expects no arguments")
		}
		return runtime.Float{Value: math.Sqrt2}, nil
	})
	r.Register("math", "phi", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.phi expects no arguments")
		}
		return runtime.Float{Value: math.Phi}, nil
	})
	r.Register("math", "maxInt", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.maxInt expects no arguments")
		}
		return runtime.SmallInt{Value: math.MaxInt64}, nil
	})
	r.Register("math", "minInt", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.minInt expects no arguments")
		}
		return runtime.SmallInt{Value: math.MinInt64}, nil
	})
	r.Register("math", "maxFloat", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.maxFloat expects no arguments")
		}
		return runtime.Float{Value: math.MaxFloat64}, nil
	})
	r.Register("math", "minFloat", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.minFloat expects no arguments")
		}
		return runtime.Float{Value: math.SmallestNonzeroFloat64}, nil
	})
	r.Register("math", "epsilon", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.epsilon expects no arguments")
		}
		return runtime.Float{Value: 2.220446049250313e-16}, nil
	})
	r.Register("math", "sqrt2Pi", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.sqrt2Pi expects no arguments")
		}
		return runtime.Float{Value: math.Sqrt(2 * math.Pi)}, nil
	})
	r.Register("math", "log2Pi", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.log2Pi expects no arguments")
		}
		return runtime.Float{Value: math.Log(2 * math.Pi)}, nil
	})
	r.Register("math", "log2", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Log2, "math.log2")
	})
	r.Register("math", "trunc", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Trunc, "math.trunc")
	})
	r.Register("math", "cbrt", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Cbrt, "math.cbrt")
	})
	r.Register("math", "sign", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.sign expects exactly one argument")
		}
		v, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		switch {
		case v < 0:
			return runtime.NewInt64(-1), nil
		case v > 0:
			return runtime.NewInt64(1), nil
		default:
			return runtime.NewInt64(0), nil
		}
	})
	r.Register("math", "hypot", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.hypot expects exactly two arguments")
		}
		a, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		b, err := FloatLike(args[1])
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: math.Hypot(a, b)}, nil
	})
	r.Register("math", "inf", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.inf expects no arguments")
		}
		return runtime.Float{Value: math.Inf(1)}, nil
	})
	r.Register("math", "nan", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("math.nan expects no arguments")
		}
		return runtime.Float{Value: math.NaN()}, nil
	})
	r.Register("math", "isNaN", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.isNaN expects exactly one argument")
		}
		v, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: math.IsNaN(v)}, nil
	})
	r.Register("math", "isInf", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.isInf expects exactly one argument")
		}
		v, err := FloatLike(args[0])
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: math.IsInf(v, 0)}, nil
	})
	r.Register("math", "isPrime", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.isPrime expects exactly one argument")
		}
		n, ok := IntValueToBigInt(args[0])
		if !ok {
			return nil, fmt.Errorf("math.isPrime: argument must be an integer")
		}
		return runtime.Bool{Value: n.ProbablyPrime(20)}, nil
	})
	r.Register("math", "median", func(args []runtime.Value) (runtime.Value, error) {
		nums, err := mathNumericList(args, "math.median")
		if err != nil {
			return nil, err
		}
		if len(nums) == 0 {
			return nil, fmt.Errorf("math.median: list must not be empty")
		}
		return runtime.Float{Value: mathQuantile(nums, 0.5)}, nil
	})
	r.Register("math", "percentile", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.percentile expects (list, p)")
		}
		nums, err := mathNumericListSingle(args[0], "math.percentile")
		if err != nil {
			return nil, err
		}
		if len(nums) == 0 {
			return nil, fmt.Errorf("math.percentile: list must not be empty")
		}
		p, err := FloatLike(args[1])
		if err != nil {
			return nil, fmt.Errorf("math.percentile: p must be numeric: %v", err)
		}
		if p < 0 || p > 100 {
			return nil, fmt.Errorf("math.percentile: p must be in [0, 100]")
		}
		return runtime.Float{Value: mathQuantile(nums, p/100)}, nil
	})
	r.Register("math", "quantile", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.quantile expects (list, q)")
		}
		nums, err := mathNumericListSingle(args[0], "math.quantile")
		if err != nil {
			return nil, err
		}
		if len(nums) == 0 {
			return nil, fmt.Errorf("math.quantile: list must not be empty")
		}
		q, err := FloatLike(args[1])
		if err != nil {
			return nil, fmt.Errorf("math.quantile: q must be numeric: %v", err)
		}
		if q < 0 || q > 1 {
			return nil, fmt.Errorf("math.quantile: q must be in [0, 1]")
		}
		return runtime.Float{Value: mathQuantile(nums, q)}, nil
	})
	r.Register("math", "mode", func(args []runtime.Value) (runtime.Value, error) {
		nums, err := mathNumericList(args, "math.mode")
		if err != nil {
			return nil, err
		}
		if len(nums) == 0 {
			return nil, fmt.Errorf("math.mode: list must not be empty")
		}
		// Count occurrences; ties broken by lowest value (deterministic).
		counts := map[float64]int{}
		for _, v := range nums {
			counts[v]++
		}
		best := nums[0]
		bestCount := 0
		for v, c := range counts {
			if c > bestCount || (c == bestCount && v < best) {
				best = v
				bestCount = c
			}
		}
		return runtime.Float{Value: best}, nil
	})
	r.Register("math", "gamma", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Gamma, "math.gamma")
	})
	r.Register("math", "lgamma", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, func(x float64) float64 { v, _ := math.Lgamma(x); return v }, "math.lgamma")
	})
	r.Register("math", "lbeta", func(args []runtime.Value) (runtime.Value, error) {
		return FloatBinaryMath(args, func(a, b float64) float64 {
			la, _ := math.Lgamma(a)
			lb, _ := math.Lgamma(b)
			lab, _ := math.Lgamma(a + b)
			return la + lb - lab
		}, "math.lbeta")
	})
	r.Register("math", "beta", func(args []runtime.Value) (runtime.Value, error) {
		return FloatBinaryMath(args, func(a, b float64) float64 {
			la, _ := math.Lgamma(a)
			lb, _ := math.Lgamma(b)
			lab, _ := math.Lgamma(a + b)
			return math.Exp(la + lb - lab)
		}, "math.beta")
	})
	r.Register("math", "erf", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Erf, "math.erf")
	})
	r.Register("math", "erfc", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Erfc, "math.erfc")
	})
	r.Register("math", "erfinv", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Erfinv, "math.erfinv")
	})
	r.Register("math", "j0", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.J0, "math.j0")
	})
	r.Register("math", "j1", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.J1, "math.j1")
	})
	r.Register("math", "y0", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Y0, "math.y0")
	})
	r.Register("math", "y1", func(args []runtime.Value) (runtime.Value, error) {
		return FloatUnaryMath(args, math.Y1, "math.y1")
	})
	r.Register("math", "jn", func(args []runtime.Value) (runtime.Value, error) {
		return besselN(args, math.Jn, "math.jn")
	})
	r.Register("math", "yn", func(args []runtime.Value) (runtime.Value, error) {
		return besselN(args, math.Yn, "math.yn")
	})
	r.Register("math", "factorial", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.factorial expects exactly one argument")
		}
		n, err := intArg(args[0], "math.factorial n")
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, fmt.Errorf("math.factorial: n must be non-negative")
		}
		if n > maxCombinatoricN {
			return nil, fmt.Errorf("math.factorial: n too large (max %d)", maxCombinatoricN)
		}
		result := big.NewInt(1)
		for i := int64(2); i <= n; i++ {
			result.Mul(result, big.NewInt(i))
		}
		return bigIntValue(result), nil
	})
	r.Register("math", "comb", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.comb expects exactly two arguments")
		}
		n, err := intArg(args[0], "math.comb n")
		if err != nil {
			return nil, err
		}
		k, err := intArg(args[1], "math.comb k")
		if err != nil {
			return nil, err
		}
		if n < 0 || k < 0 {
			return nil, fmt.Errorf("math.comb: n and k must be non-negative")
		}
		if n > maxCombinatoricN {
			return nil, fmt.Errorf("math.comb: n too large (max %d)", maxCombinatoricN)
		}
		if k > n {
			return runtime.SmallInt{Value: 0}, nil
		}
		return bigIntValue(new(big.Int).Binomial(n, k)), nil
	})
	r.Register("math", "perm", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.perm expects exactly two arguments")
		}
		n, err := intArg(args[0], "math.perm n")
		if err != nil {
			return nil, err
		}
		k, err := intArg(args[1], "math.perm k")
		if err != nil {
			return nil, err
		}
		if n < 0 || k < 0 {
			return nil, fmt.Errorf("math.perm: n and k must be non-negative")
		}
		if n > maxCombinatoricN {
			return nil, fmt.Errorf("math.perm: n too large (max %d)", maxCombinatoricN)
		}
		if k > n {
			return runtime.SmallInt{Value: 0}, nil
		}
		result := big.NewInt(1)
		for i := n - k + 1; i <= n; i++ {
			result.Mul(result, big.NewInt(i))
		}
		return bigIntValue(result), nil
	})
	r.Register("math", "gcd", func(args []runtime.Value) (runtime.Value, error) {
		a, b, err := bigIntPair(args, "math.gcd")
		if err != nil {
			return nil, err
		}
		return bigIntValue(new(big.Int).GCD(nil, nil, new(big.Int).Abs(a), new(big.Int).Abs(b))), nil
	})
	r.Register("math", "lcm", func(args []runtime.Value) (runtime.Value, error) {
		a, b, err := bigIntPair(args, "math.lcm")
		if err != nil {
			return nil, err
		}
		if a.Sign() == 0 || b.Sign() == 0 {
			return runtime.SmallInt{Value: 0}, nil
		}
		absA := new(big.Int).Abs(a)
		absB := new(big.Int).Abs(b)
		g := new(big.Int).GCD(nil, nil, absA, absB)
		return bigIntValue(absA.Mul(absA.Quo(absA, g), absB)), nil
	})
	r.Register("math", "lcomb", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("math.lcomb expects exactly two arguments")
		}
		n, err := intArg(args[0], "math.lcomb n")
		if err != nil {
			return nil, err
		}
		k, err := intArg(args[1], "math.lcomb k")
		if err != nil {
			return nil, err
		}
		if n < 0 || k < 0 {
			return nil, fmt.Errorf("math.lcomb: n and k must be non-negative")
		}
		ln1, _ := math.Lgamma(float64(n) + 1)
		lk1, _ := math.Lgamma(float64(k) + 1)
		lnk1, _ := math.Lgamma(float64(n-k) + 1)
		return runtime.Float{Value: ln1 - lk1 - lnk1}, nil
	})
}

func mathNumericList(args []runtime.Value, label string) ([]float64, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a single list argument", label)
	}
	return mathNumericListSingle(args[0], label)
}

func mathNumericListSingle(v runtime.Value, label string) ([]float64, error) {
	list, ok := v.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s: argument must be a list", label)
	}
	nums := make([]float64, len(list.Elements))
	for i, elem := range list.Elements {
		f, err := FloatLike(elem)
		if err != nil {
			return nil, fmt.Errorf("%s: list element %d: %v", label, i, err)
		}
		nums[i] = f
	}
	return nums, nil
}

// mathQuantile computes the q-quantile (q in [0, 1]) using R's type-7
// linear-interpolation algorithm - the most common default across
// numpy, pandas, R, Excel.
func mathQuantile(nums []float64, q float64) float64 {
	sorted := append([]float64(nil), nums...)
	sort.Float64s(sorted)
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := q * float64(len(sorted)-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
