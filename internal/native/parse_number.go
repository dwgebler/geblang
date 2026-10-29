package native

import (
	"fmt"
	"strconv"

	"geblang/internal/runtime"
)

func StringNumberOrNull(text, method string, args []runtime.Value) (runtime.Value, error) {
	var value runtime.Value
	var err error
	switch method {
	case "toIntOrNull":
		if len(args) != 0 {
			base, baseErr := IntBaseArg(args, "string.toIntOrNull")
			if baseErr != nil {
				return nil, runtime.ClassifiedError{Class: "ValueError", Message: baseErr.Error()}
			}
			value, err = StringParseBase(text, base, "string.toIntOrNull")
		} else {
			value, err = runtime.NewIntLiteral(text)
		}
	case "toDecimalOrNull":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.toDecimalOrNull expects no arguments")
		}
		value, err = runtime.NewDecimalLiteral(text)
	case "toFloatOrNull":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.toFloatOrNull expects no arguments")
		}
		var number float64
		number, err = strconv.ParseFloat(text, 64)
		value = runtime.Float{Value: number}
	default:
		return nil, fmt.Errorf("unknown numeric parsing method %s", method)
	}
	if err != nil {
		return runtime.Null{}, nil
	}
	return value, nil
}
