package transpilert

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Primitive conversion methods (toInt/toFloat/toDecimal/toBool/toString),
// matching the interpreter's castValue semantics byte for byte. Fallible
// conversions panic *Error{Class:"RuntimeError"} so the top-level gbUncaught
// recovery renders the identical uncaught text and exits 1.

// parseIntDigits mirrors ast.ParseIntLiteral: underscore separators, 0b/0o/0x
// base prefixes, base-10 otherwise, arbitrary precision.
func parseIntDigits(lit string) (*big.Int, bool) {
	digits := lit
	if strings.ContainsRune(digits, '_') {
		digits = strings.ReplaceAll(digits, "_", "")
	}
	base := 10
	if len(digits) > 2 && digits[0] == '0' {
		switch digits[1] {
		case 'b', 'B':
			base, digits = 2, digits[2:]
		case 'o', 'O':
			base, digits = 8, digits[2:]
		case 'x', 'X':
			base, digits = 16, digits[2:]
		}
	}
	if digits == "" {
		return nil, false
	}
	return new(big.Int).SetString(digits, base)
}

// StringToInt parses a string to int64 (fast int mode), matching the
// interpreter's `string as int`. Out-of-int64 magnitudes are unreachable for
// Tier-1 and follow the documented fast-mode int64 divergence.
func StringToInt(s string) int64 {
	v, ok := parseIntDigits(s)
	if !ok {
		panic(NewError("RuntimeError", fmt.Sprintf("invalid integer literal %q", s)))
	}
	return v.Int64()
}

func StringToFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(NewError("RuntimeError", err.Error()))
	}
	return f
}

func parseDecimalDigits(s string) (*big.Rat, bool) {
	digits := s
	if strings.ContainsRune(digits, '_') {
		digits = strings.ReplaceAll(digits, "_", "")
	}
	return new(big.Rat).SetString(digits)
}

func StringToDecimal(s string) *big.Rat {
	v, ok := parseDecimalDigits(s)
	if !ok {
		panic(NewError("RuntimeError", fmt.Sprintf("invalid decimal literal %q", s)))
	}
	return v
}

// StringIsInt/StringIsDecimal reuse the toInt/toDecimal parse so the predicate
// cannot drift from the cast. FloatIsInt/DecimalIsInt report whole numbers.
func StringIsInt(s string) bool {
	_, ok := parseIntDigits(s)
	return ok
}

func StringIsDecimal(s string) bool {
	_, ok := parseDecimalDigits(s)
	return ok
}

func StringIsNumeric(s string) bool {
	return StringIsInt(s) || StringIsDecimal(s)
}

func FloatIsInt(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f)
}

func DecimalIsInt(v *big.Rat) bool { return v.IsInt() }

// StringToBool accepts only "true"/"false", matching the interpreter; anything
// else is an uncaught cast error.
func StringToBool(s string) bool {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	panic(NewError("RuntimeError", "cannot cast string to bool"))
}

// toString helpers use the interpreter's Inspect representation per type.
func IntToString(v int64) string     { return strconv.FormatInt(v, 10) }
func FloatToString(v float64) string { return fmt.Sprintf("%g", v) }

// DecimalToString is the default decimal rendering: ten places with trailing zeros dropped, keeping one fractional digit.
func DecimalToString(v *big.Rat) string {
	text := strings.TrimRight(v.FloatString(10), "0")
	if strings.HasSuffix(text, ".") {
		text += "0"
	}
	if text == "-0.0" {
		return "0.0"
	}
	return text
}
func BoolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// Numeric cross-conversions matching castValue (truncate toward zero, etc.).
func FloatToInt(v float64) int64    { return int64(math.Trunc(v)) }
func DecimalToInt(v *big.Rat) int64 { return new(big.Int).Quo(v.Num(), v.Denom()).Int64() }
func IntToFloat(v int64) float64    { return float64(v) }
func DecimalToFloat(v *big.Rat) float64 {
	f, _ := v.Float64()
	return f
}
func IntToDecimal(v int64) *big.Rat { return new(big.Rat).SetInt64(v) }
func FloatToDecimal(v float64) *big.Rat {
	return StringToDecimal(strconv.FormatFloat(v, 'g', -1, 64))
}
