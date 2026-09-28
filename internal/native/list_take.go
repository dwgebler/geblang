package native

import (
	"fmt"

	"geblang/internal/runtime"
)

// classedError crosses the native boundary as the named Geblang class on both backends.
type classedError struct{ class, msg string }

func (e *classedError) Error() string      { return e.msg }
func (e *classedError) ErrorClass() string { return e.class }

func errFrozenList() error {
	return &classedError{class: "ImmutableError", msg: "cannot modify frozen list"}
}

// ListShift removes the first element in place; a no-op on an empty list.
func ListShift(list *runtime.List, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("list.shift expects no arguments")
	}
	if list.Frozen {
		return nil, errFrozenList()
	}
	if len(list.Elements) > 0 {
		listRemoveAt(list, 0)
	}
	return list, nil
}

// ListTake implements takeFirst, takeLast and takeAt: remove one element and return it.
func ListTake(list *runtime.List, method string, args []runtime.Value) (runtime.Value, error) {
	var i int
	switch method {
	case "takeFirst", "takeLast":
		if len(args) != 0 {
			return nil, fmt.Errorf("list.%s expects no arguments", method)
		}
		if list.Frozen {
			return nil, errFrozenList()
		}
		if len(list.Elements) == 0 {
			return nil, &classedError{class: "ValueError", msg: "list." + method + " on empty list"}
		}
		if method == "takeLast" {
			i = len(list.Elements) - 1
		}
	case "takeAt":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.takeAt expects one argument")
		}
		n, ok := IntValueToBigInt(args[0])
		if !ok {
			return nil, fmt.Errorf("list.takeAt: index must be int, got %s", args[0].TypeName())
		}
		if !n.IsInt64() {
			return nil, fmt.Errorf("list.takeAt: index out of range")
		}
		idx := n.Int64()
		if idx < 0 {
			idx += int64(len(list.Elements))
		}
		if idx < 0 || idx >= int64(len(list.Elements)) {
			return nil, fmt.Errorf("list.takeAt: index out of range")
		}
		i = int(idx)
	default:
		return nil, fmt.Errorf("unknown method list.%s", method)
	}
	if list.Frozen {
		return nil, errFrozenList()
	}
	return listRemoveAt(list, i), nil
}

func listRemoveAt(list *runtime.List, i int) runtime.Value {
	v := list.Elements[i]
	copy(list.Elements[i:], list.Elements[i+1:])
	last := len(list.Elements) - 1
	list.Elements[last] = nil
	list.Elements = list.Elements[:last]
	return v
}
