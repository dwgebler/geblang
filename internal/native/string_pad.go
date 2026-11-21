package native

import (
	"fmt"

	"geblang/internal/runtime"
)

// StringPadMethod implements padStart and padEnd for both backends.
func StringPadMethod(method, s string, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("string.%s expects (length[, pad])", method)
	}
	width, err := widthArg(args[0])
	if err != nil {
		return nil, fmt.Errorf("string.%s: %v", method, err)
	}
	pad := " "
	if len(args) == 2 {
		padStr, ok := args[1].(runtime.String)
		if !ok || padStr.Value == "" {
			return nil, fmt.Errorf("string.%s: pad must be a non-empty string", method)
		}
		pad = padStr.Value
	}
	fill := padFill(pad, width-len([]rune(s)))
	if method == "padStart" {
		return runtime.String{Value: fill + s}, nil
	}
	return runtime.String{Value: s + fill}, nil
}

func widthArg(v runtime.Value) (int, error) {
	switch n := v.(type) {
	case runtime.SmallInt:
		return int(n.Value), nil
	case runtime.Int:
		if !n.Value.IsInt64() {
			return 0, runtime.ClassifiedError{Class: "ValueError", Message: "index is out of range"}
		}
		return int(n.Value.Int64()), nil
	}
	return 0, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("index must be int, got %s", v.TypeName())}
}

// padFill repeats pad from its first character to exactly n characters.
func padFill(pad string, n int) string {
	if n <= 0 {
		return ""
	}
	padRunes := []rune(pad)
	out := make([]rune, n)
	for i := range out {
		out[i] = padRunes[i%len(padRunes)]
	}
	return string(out)
}
