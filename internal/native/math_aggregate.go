package native

import (
	"fmt"
	"math/big"
	"sort"

	"geblang/internal/runtime"
)

// Set elements come back in ascending order so float accumulation is deterministic.
func numericCollection(v runtime.Value, label string) ([]runtime.Value, string, error) {
	switch c := v.(type) {
	case *runtime.List:
		for i, elem := range c.Elements {
			if !isNumber(elem) {
				return nil, "", fmt.Errorf("%s: list element %d: expected numeric value, got %s", label, i, elem.TypeName())
			}
		}
		return c.Elements, "list", nil
	case runtime.Set:
		values := make([]runtime.Value, 0, len(c.Elements))
		for _, entry := range c.Elements {
			if !isNumber(entry.Value) {
				return nil, "", fmt.Errorf("%s: set element: expected numeric value, got %s", label, entry.Value.TypeName())
			}
			values = append(values, entry.Value)
		}
		sort.SliceStable(values, func(i, j int) bool {
			cmp, _ := NumericCompare(values[i], values[j])
			return cmp < 0
		})
		return values, "set", nil
	}
	return nil, "", fmt.Errorf("%s: argument must be a list or set", label)
}

func isNumber(v runtime.Value) bool {
	switch v.(type) {
	case runtime.SmallInt, runtime.Int, runtime.Decimal, runtime.Float:
		return true
	}
	return false
}

// numericFloats is numericCollection converted to float64, rejecting an empty input.
func numericFloats(v runtime.Value, label string) ([]float64, error) {
	values, kind, err := numericCollection(v, label)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s: %s must not be empty", label, kind)
	}
	out := make([]float64, len(values))
	for i, value := range values {
		out[i], _ = FloatLike(value)
	}
	return out, nil
}

// mathSum accumulates left to right with the same promotion rules as `+`.
func mathSum(values []runtime.Value) (runtime.Value, error) {
	intAcc := new(big.Int)
	var ratAcc *big.Rat
	var floatAcc float64
	isFloat := false
	for _, v := range values {
		switch x := v.(type) {
		case runtime.SmallInt, runtime.Int:
			n, _ := IntValueToBigInt(x)
			switch {
			case isFloat:
				f, _ := new(big.Rat).SetInt(n).Float64()
				floatAcc += f
			case ratAcc != nil:
				ratAcc.Add(ratAcc, new(big.Rat).SetInt(n))
			default:
				intAcc.Add(intAcc, n)
			}
		case runtime.Decimal:
			if isFloat {
				return nil, sumMixError("float", "decimal")
			}
			if ratAcc == nil {
				ratAcc = new(big.Rat).SetInt(intAcc)
			}
			ratAcc.Add(ratAcc, x.Value)
		case runtime.Float:
			if ratAcc != nil {
				return nil, sumMixError("decimal", "float")
			}
			if !isFloat {
				floatAcc, _ = new(big.Rat).SetInt(intAcc).Float64()
				isFloat = true
			}
			floatAcc += x.Value
		}
	}
	switch {
	case isFloat:
		return runtime.Float{Value: floatAcc}, nil
	case ratAcc != nil:
		return runtime.Decimal{Value: ratAcc}, nil
	case intAcc.IsInt64():
		return runtime.SmallInt{Value: intAcc.Int64()}, nil
	}
	return runtime.Int{Value: intAcc}, nil
}

func sumMixError(left, right string) error {
	return fmt.Errorf("math.sum: cannot mix decimal and float (got %s and %s): cast one side - 'as float' drops decimal exactness, 'as decimal' adopts the float's imprecision", left, right)
}

func registerMathAggregates(r *Registry) {
	r.Register("math", "sum", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.sum expects a single list or set argument")
		}
		values, _, err := numericCollection(args[0], "math.sum")
		if err != nil {
			return nil, err
		}
		return mathSum(values)
	})
	r.Register("math", "mean", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("math.mean expects a single list or set argument")
		}
		xs, err := numericFloats(args[0], "math.mean")
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: statsSampleMean(xs)}, nil
	})
}
