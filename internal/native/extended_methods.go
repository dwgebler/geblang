package native

import "geblang/internal/runtime"

// CallFunc invokes a Geblang callable on the active backend.
type CallFunc func(fn runtime.Value, args []runtime.Value) (runtime.Value, error)

// ExtendedMethod dispatches primitive methods shared by both backends after their own tables miss.
func ExtendedMethod(receiver runtime.Value, name string, args []runtime.Value, call CallFunc) (runtime.Value, bool, error) {
	switch value := receiver.(type) {
	case runtime.String:
		if result, ok, err := stringPredicate(value, name, args); ok {
			return result, true, err
		}
		return stringTextMethod(value.Value, name, args)
	case runtime.SmallInt, runtime.Int:
		return intExtraMethod(receiver, name, args)
	case runtime.Float, runtime.Decimal:
		return realExtraMethod(receiver, name, args)
	case runtime.Bytes:
		return bytesExtraMethod(value.Value, name, args)
	case runtime.Set:
		return setExtraMethod(value, name, args, call)
	case runtime.Dict:
		return dictExtraMethod(value, name, args, call)
	}
	return nil, false, nil
}
