package transpilert

import "math"

// ModInt is Geblang's floor modulo: the remainder takes the divisor's sign.
func ModInt(a, b int64) int64 {
	if b == 0 {
		panic(NewError("RuntimeError", "modulo by zero"))
	}
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

func ModFloat(a, b float64) float64 {
	if b == 0 {
		panic(NewError("RuntimeError", "float modulo by zero"))
	}
	return a - math.Floor(a/b)*b
}
