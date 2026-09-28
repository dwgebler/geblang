package native

import (
	"fmt"
	"math"

	"geblang/internal/runtime"
)

func statsVarianceFor(args []runtime.Value, label string) (float64, error) {
	if len(args) < 1 || len(args) > 2 {
		return 0, fmt.Errorf("%s expects (xs, opts?)", label)
	}
	xs, err := numericFloats(args[0], label)
	if err != nil {
		return 0, err
	}
	var opts runtime.Value
	if len(args) == 2 {
		opts = args[1]
	}
	population, err := statsBoolOpt(opts, "population", false)
	if err != nil {
		return 0, fmt.Errorf("%s: %v", label, err)
	}
	if population {
		return statsSampleVariance(xs, 0), nil
	}
	if len(xs) < 2 {
		return 0, fmt.Errorf("%s: sample variance needs at least 2 values", label)
	}
	return statsSampleVariance(xs, 1), nil
}

func statsListFloats(v runtime.Value, label string) ([]float64, error) {
	if _, ok := v.(*runtime.List); !ok {
		return nil, fmt.Errorf("%s: argument must be a list", label)
	}
	return numericFloats(v, label)
}

func statsPositive(xs []float64, label string) error {
	for _, x := range xs {
		if !(x > 0) {
			return fmt.Errorf("%s: values must be positive", label)
		}
	}
	return nil
}

func statsOneArg(args []runtime.Value, label string) ([]float64, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (xs)", label)
	}
	return numericFloats(args[0], label)
}

func statsMinMax(xs []float64) (float64, float64) {
	lo, hi := xs[0], xs[0]
	for _, x := range xs[1:] {
		lo = math.Min(lo, x)
		hi = math.Max(hi, x)
	}
	return lo, hi
}

func registerStatsDescriptive(r *Registry) {
	r.Register("stats", "variance", func(args []runtime.Value) (runtime.Value, error) {
		v, err := statsVarianceFor(args, "stats.variance")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: v}, nil
	})
	r.Register("stats", "stdev", func(args []runtime.Value) (runtime.Value, error) {
		v, err := statsVarianceFor(args, "stats.stdev")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: math.Sqrt(v)}, nil
	})
	r.Register("stats", "geometricMean", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.geometricMean")
		if err != nil {
			return nil, err
		}
		if err := statsPositive(xs, "stats.geometricMean"); err != nil {
			return nil, err
		}
		logSum := 0.0
		for _, x := range xs {
			logSum += math.Log(x)
		}
		return runtime.Float{Value: math.Exp(logSum / float64(len(xs)))}, nil
	})
	r.Register("stats", "harmonicMean", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.harmonicMean")
		if err != nil {
			return nil, err
		}
		if err := statsPositive(xs, "stats.harmonicMean"); err != nil {
			return nil, err
		}
		inv := 0.0
		for _, x := range xs {
			inv += 1 / x
		}
		return runtime.Float{Value: float64(len(xs)) / inv}, nil
	})
	r.Register("stats", "weightedMean", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("stats.weightedMean expects (xs, weights)")
		}
		xs, err := statsListFloats(args[0], "stats.weightedMean")
		if err != nil {
			return nil, err
		}
		ws, err := statsListFloats(args[1], "stats.weightedMean")
		if err != nil {
			return nil, err
		}
		if len(xs) != len(ws) {
			return nil, fmt.Errorf("stats.weightedMean: xs and weights must have equal length")
		}
		total, acc := 0.0, 0.0
		for i, w := range ws {
			if w < 0 {
				return nil, fmt.Errorf("stats.weightedMean: weights must not be negative")
			}
			total += w
			acc += w * xs[i]
		}
		if total == 0 {
			return nil, fmt.Errorf("stats.weightedMean: weights must not sum to zero")
		}
		return runtime.Float{Value: acc / total}, nil
	})
	r.Register("stats", "range", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.range")
		if err != nil {
			return nil, err
		}
		lo, hi := statsMinMax(xs)
		return runtime.Float{Value: hi - lo}, nil
	})
	r.Register("stats", "iqr", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.iqr")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: mathQuantile(xs, 0.75) - mathQuantile(xs, 0.25)}, nil
	})
	r.Register("stats", "mad", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.mad")
		if err != nil {
			return nil, err
		}
		med := mathQuantile(xs, 0.5)
		devs := make([]float64, len(xs))
		for i, x := range xs {
			devs[i] = math.Abs(x - med)
		}
		return runtime.Float{Value: mathQuantile(devs, 0.5)}, nil
	})
	r.Register("stats", "zscores", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("stats.zscores expects (xs)")
		}
		xs, err := statsListFloats(args[0], "stats.zscores")
		if err != nil {
			return nil, err
		}
		if len(xs) < 2 {
			return nil, fmt.Errorf("stats.zscores: sample variance needs at least 2 values")
		}
		sd := math.Sqrt(statsSampleVariance(xs, 1))
		if sd == 0 {
			return nil, fmt.Errorf("stats.zscores: zero variance")
		}
		mean := statsSampleMean(xs)
		out := make([]runtime.Value, len(xs))
		for i, x := range xs {
			out[i] = runtime.Float{Value: (x - mean) / sd}
		}
		return &runtime.List{Elements: out}, nil
	})
	r.Register("stats", "describe", func(args []runtime.Value) (runtime.Value, error) {
		xs, err := statsOneArg(args, "stats.describe")
		if err != nil {
			return nil, err
		}
		sd := math.NaN()
		if len(xs) > 1 {
			sd = math.Sqrt(statsSampleVariance(xs, 1))
		}
		lo, hi := statsMinMax(xs)
		d := runtime.NewDictHint(8)
		countKey := runtime.String{Value: "count"}
		d.PutEntry(DictKey(countKey), runtime.DictEntry{Key: countKey, Value: runtime.SmallInt{Value: int64(len(xs))}})
		statsPutFloat(&d, "mean", statsSampleMean(xs))
		statsPutFloat(&d, "stdev", sd)
		statsPutFloat(&d, "min", lo)
		statsPutFloat(&d, "q1", mathQuantile(xs, 0.25))
		statsPutFloat(&d, "median", mathQuantile(xs, 0.5))
		statsPutFloat(&d, "q3", mathQuantile(xs, 0.75))
		statsPutFloat(&d, "max", hi)
		return d, nil
	})
}
