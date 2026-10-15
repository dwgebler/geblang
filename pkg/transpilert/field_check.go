package transpilert

import (
	"fmt"
	"math/big"
)

func fieldTypeError(v any, declared, field string) *Error {
	return &Error{Class: "TypeError", Message: fmt.Sprintf("cannot assign %s to %s field %s", gbTypeName(v), declared, field), Parents: []string{"Error"}}
}

// ExpectFieldIntFast unwraps an any value written to an int field in fast int mode.
func ExpectFieldIntFast(v any, field string) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case Int:
		if x.Big == nil {
			return x.I64
		}
	}
	panic(fieldTypeError(v, "int", field))
}

// ExpectFieldInt unwraps an any value written to an int field in big-int mode.
func ExpectFieldInt(v any, field string) Int {
	switch x := v.(type) {
	case int64:
		return FromInt64(x)
	case int:
		return FromInt64(int64(x))
	case *big.Int:
		return FromBig(x)
	case Int:
		return x
	}
	panic(fieldTypeError(v, "int", field))
}

// ExpectFieldFloat unwraps an any value written to a float field.
func ExpectFieldFloat(v any, field string) float64 {
	if x, ok := v.(float64); ok {
		return x
	}
	panic(fieldTypeError(v, "float", field))
}

// ExpectFieldDecimal unwraps an any value written to a decimal field.
func ExpectFieldDecimal(v any, field string) *big.Rat {
	if x, ok := v.(*big.Rat); ok {
		return x
	}
	panic(fieldTypeError(v, "decimal", field))
}

// ExpectFieldString unwraps an any value written to a string field.
func ExpectFieldString(v any, field string) string {
	if x, ok := v.(string); ok {
		return x
	}
	panic(fieldTypeError(v, "string", field))
}

// ExpectFieldBool unwraps an any value written to a bool field.
func ExpectFieldBool(v any, field string) bool {
	if x, ok := v.(bool); ok {
		return x
	}
	panic(fieldTypeError(v, "bool", field))
}
