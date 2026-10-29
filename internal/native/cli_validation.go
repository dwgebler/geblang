package native

import (
	"fmt"
	"math"

	"geblang/internal/runtime"
)

func ValidateCLIRules(spec runtime.Dict) error {
	minimum, hasMin := dictLookup(spec, "min")
	maximum, hasMax := dictLookup(spec, "max")
	for _, bound := range []struct {
		value runtime.Value
		set   bool
	}{{minimum, hasMin}, {maximum, hasMax}} {
		if bound.set && !finiteCLINumber(bound.value) {
			return runtime.ClassifiedError{Class: "ValueError", Message: "validation bounds must be finite numbers"}
		}
	}
	if hasMin && hasMax {
		comparison, err := NumericCompare(minimum, maximum)
		if err != nil {
			return err
		}
		if comparison > 0 {
			return runtime.ClassifiedError{Class: "ValueError", Message: "validation min must not exceed max"}
		}
	}
	if choices, present := dictLookup(spec, "choices"); present {
		if _, ok := choices.(*runtime.List); !ok {
			return runtime.ClassifiedError{Class: "ValueError", Message: "validation choices must be a list"}
		}
	}
	return nil
}

func ValidateCLIValue(value runtime.Value, spec runtime.Dict) (string, error) {
	if err := ValidateCLIRules(spec); err != nil {
		return "", err
	}
	minimum, hasMin := dictLookup(spec, "min")
	maximum, hasMax := dictLookup(spec, "max")
	if hasMin || hasMax {
		if !finiteCLINumber(value) {
			return "expected a finite number", nil
		}
		if hasMin {
			comparison, err := NumericCompare(value, minimum)
			if err != nil {
				return "", err
			}
			if comparison < 0 {
				return fmt.Sprintf("must be at least %s", minimum.Inspect()), nil
			}
		}
		if hasMax {
			comparison, err := NumericCompare(value, maximum)
			if err != nil {
				return "", err
			}
			if comparison > 0 {
				return fmt.Sprintf("must be at most %s", maximum.Inspect()), nil
			}
		}
	}
	if choices, ok := dictLookup(spec, "choices"); ok {
		items, ok := choices.(*runtime.List)
		if !ok {
			return "", runtime.ClassifiedError{Class: "ValueError", Message: "validation choices must be a list"}
		}
		for _, item := range items.Elements {
			if runtime.ValuesEqual(value, item) {
				return "", nil
			}
		}
		return "must be one of the permitted choices", nil
	}
	return "", nil
}

func finiteCLINumber(value runtime.Value) bool {
	switch number := value.(type) {
	case runtime.SmallInt, runtime.Int, runtime.Decimal:
		return true
	case runtime.Float:
		return !math.IsNaN(number.Value) && !math.IsInf(number.Value, 0)
	default:
		return false
	}
}
