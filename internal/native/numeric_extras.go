package native

import (
	"fmt"
	"math"
	"math/big"

	"geblang/internal/runtime"
)

func intExtraMethod(receiver runtime.Value, name string, args []runtime.Value) (runtime.Value, bool, error) {
	label := "int." + name
	switch name {
	case "isBetween":
		result, err := NumericIsBetween(label, receiver, args)
		return result, true, err
	case "gcd", "lcm":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("%s expects one argument", label)
		}
		other, ok := IntValueToBigInt(args[0])
		if !ok {
			return nil, true, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s expects int, got %s", label, args[0].TypeName())}
		}
		self, _ := IntValueToBigInt(receiver)
		if name == "gcd" {
			return bigIntValue(intGcd(self, other)), true, nil
		}
		return bigIntValue(intLcm(self, other)), true, nil
	case "digits", "isPowerOfTwo", "bitCount":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("%s expects no arguments", label)
		}
		self, _ := IntValueToBigInt(receiver)
		switch name {
		case "digits":
			text := new(big.Int).Abs(self).String()
			out := make([]runtime.Value, len(text))
			for i := 0; i < len(text); i++ {
				out[i] = runtime.SmallInt{Value: int64(text[i] - '0')}
			}
			return &runtime.List{Elements: out}, true, nil
		case "isPowerOfTwo":
			return runtime.Bool{Value: self.Sign() > 0 && popCount(self) == 1}, true, nil
		}
		return runtime.SmallInt{Value: int64(popCount(new(big.Int).Abs(self)))}, true, nil
	}
	return nil, false, nil
}

func intGcd(a, b *big.Int) *big.Int {
	return new(big.Int).GCD(nil, nil, new(big.Int).Abs(a), new(big.Int).Abs(b))
}

func intLcm(a, b *big.Int) *big.Int {
	if a.Sign() == 0 || b.Sign() == 0 {
		return new(big.Int)
	}
	product := new(big.Int).Abs(new(big.Int).Mul(a, b))
	return product.Quo(product, intGcd(a, b))
}

func popCount(n *big.Int) int {
	count := 0
	for _, word := range n.Bits() {
		for w := uint64(word); w != 0; w &= w - 1 {
			count++
		}
	}
	return count
}

func realExtraMethod(receiver runtime.Value, name string, args []runtime.Value) (runtime.Value, bool, error) {
	label := receiver.TypeName() + "." + name
	switch name {
	case "isBetween":
		result, err := NumericIsBetween(label, receiver, args)
		return result, true, err
	case "isCloseTo":
		f, isFloat := receiver.(runtime.Float)
		if !isFloat {
			return nil, false, nil
		}
		result, err := floatIsCloseTo(f.Value, args)
		return result, true, err
	}
	return nil, false, nil
}

func isNaNValue(v runtime.Value) bool {
	f, ok := v.(runtime.Float)
	return ok && math.IsNaN(f.Value)
}

// NumericIsBetween reports lo <= v <= hi across numeric types; NaN is never between.
func NumericIsBetween(label string, v runtime.Value, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects two arguments (lo, hi)", label)
	}
	lo, hi := args[0], args[1]
	for _, bound := range args {
		if _, ok := runtime.NumericToFloat(bound); !ok {
			return nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s expects numeric bounds, got %s", label, bound.TypeName())}
		}
		if isNaNValue(bound) {
			return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s bounds must not be NaN", label)}
		}
	}
	order, err := NumericCompare(lo, hi)
	if err != nil {
		return nil, err
	}
	if order > 0 {
		return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s expects lo <= hi", label)}
	}
	if isNaNValue(v) {
		return runtime.Bool{Value: false}, nil
	}
	below, err := NumericCompare(v, lo)
	if err != nil {
		return nil, err
	}
	above, err := NumericCompare(v, hi)
	if err != nil {
		return nil, err
	}
	return runtime.Bool{Value: below >= 0 && above <= 0}, nil
}

func floatIsCloseTo(a float64, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("float.isCloseTo expects (other[, relTol[, absTol]])")
	}
	nums := []float64{0, 1e-9, 0}
	for i, arg := range args {
		f, ok := runtime.NumericToFloat(arg)
		if !ok {
			return nil, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("float.isCloseTo expects numeric arguments, got %s", arg.TypeName())}
		}
		nums[i] = f
	}
	b, relTol, absTol := nums[0], nums[1], nums[2]
	if relTol < 0 || absTol < 0 {
		return nil, runtime.ClassifiedError{Class: "ValueError", Message: "float.isCloseTo tolerances must not be negative"}
	}
	if a == b {
		return runtime.Bool{Value: true}, nil
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return runtime.Bool{Value: false}, nil
	}
	diff := math.Abs(a - b)
	bound := math.Max(relTol*math.Max(math.Abs(a), math.Abs(b)), absTol)
	return runtime.Bool{Value: diff <= bound}, nil
}
