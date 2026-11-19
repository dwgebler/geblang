package native

import (
	"fmt"

	"geblang/internal/runtime"
)

// PredicateResult reads a predicate callback's result; anything but a bool is a TypeError on both backends.
func PredicateResult(method string, v runtime.Value) (bool, error) {
	if b, ok := v.(runtime.Bool); ok {
		return b.Value, nil
	}
	return false, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s predicate must return bool, got %s", method, v.TypeName())}
}

// MatchKey identifies a value for set-style list operations; the type name keeps "1" and 1 distinct.
func MatchKey(v runtime.Value) string {
	return v.TypeName() + "\x00" + v.Inspect()
}
