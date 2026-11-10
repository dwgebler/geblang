package runtime

import (
	"math"
	"math/big"
)

// NumericToRat returns the exact rational value of a number, or ok=false for a
// non-numeric value or a non-finite float (NaN/Inf). A finite float yields its
// exact dyadic value, so cross-type comparison stays lossless and truthful.
func NumericToRat(v Value) (*big.Rat, bool) {
	switch n := v.(type) {
	case SmallInt:
		return new(big.Rat).SetInt64(n.Value), true
	case Int:
		return new(big.Rat).SetInt(n.Value), true
	case Decimal:
		return new(big.Rat).Set(n.Value), true
	case Float:
		if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) {
			return nil, false
		}
		if r := new(big.Rat).SetFloat64(n.Value); r != nil {
			return r, true
		}
	}
	return nil, false
}

// NumericToFloat returns the float64 value of any number, ok=false otherwise.
func NumericToFloat(v Value) (float64, bool) {
	switch n := v.(type) {
	case SmallInt:
		return float64(n.Value), true
	case Int:
		f, _ := new(big.Float).SetInt(n.Value).Float64()
		return f, true
	case Decimal:
		f, _ := n.Value.Float64()
		return f, true
	case Float:
		return n.Value, true
	}
	return 0, false
}

// NumericValuesEqual reports equality between two numbers by exact value across
// int/decimal/float; bothNumeric is false when either operand is not a number.
// Non-finite floats fall back to float64 equality.
func NumericValuesEqual(left Value, right Value) (equal bool, bothNumeric bool) {
	if lr, ok := NumericToRat(left); ok {
		if rr, ok := NumericToRat(right); ok {
			return lr.Cmp(rr) == 0, true
		}
	}
	if lf, lok := NumericToFloat(left); lok {
		if rf, rok := NumericToFloat(right); rok {
			return lf == rf, true
		}
	}
	return false, false
}

// MulInt64 multiplies exactly, reporting ok=false when the product does not fit in int64.
func MulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	c := a * b
	// The sign test catches MinInt64 * -1, where the division check alone would itself overflow.
	if (c < 0) != ((a < 0) != (b < 0)) || c/b != a {
		return c, false
	}
	return c, true
}
