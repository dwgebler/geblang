package evaluator

import (
	"bytes"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"math/big"
	"sort"
	"strings"
)

func collectionsLength(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	switch value := args[0].(type) {
	case *runtime.List:
		return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
	case runtime.Dict:
		return runtime.SmallInt{Value: int64(value.Len())}, nil
	case runtime.Set:
		return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
	case runtime.String:
		return runtime.SmallInt{Value: int64(len([]rune(value.Value)))}, nil
	case runtime.Bytes:
		return runtime.SmallInt{Value: int64(len(value.Value))}, nil
	default:
		return nil, fmt.Errorf("%s does not support %s", call.Callee.String(), args[0].TypeName())
	}
}

func collectionsIsEmpty(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	length, err := collectionsLength(call, args)
	if err != nil {
		return nil, err
	}
	switch n := length.(type) {
	case runtime.SmallInt:
		return runtime.Bool{Value: n.Value == 0}, nil
	case runtime.Int:
		return runtime.Bool{Value: n.Value.Sign() == 0}, nil
	}
	return runtime.Bool{Value: false}, nil
}

func collectionsContains(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects collection and value", call.Callee.String())
	}
	switch value := args[0].(type) {
	case *runtime.List:
		for _, element := range value.Elements {
			if valuesEqualSimple(element, args[1]) {
				return runtime.Bool{Value: true}, nil
			}
		}
		return runtime.Bool{Value: false}, nil
	case runtime.Dict:
		_, ok := value.GetEntry(dictKey(args[1]))
		return runtime.Bool{Value: ok}, nil
	case runtime.Set:
		_, ok := value.Elements[dictKey(args[1])]
		return runtime.Bool{Value: ok}, nil
	case runtime.String:
		needle, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s string needle must be string", call.Callee.String())
		}
		return runtime.Bool{Value: strings.Contains(value.Value, needle.Value)}, nil
	case runtime.Bytes:
		needle, ok := args[1].(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("%s bytes needle must be bytes", call.Callee.String())
		}
		return runtime.Bool{Value: bytes.Contains(value.Value, needle.Value)}, nil
	default:
		return nil, fmt.Errorf("%s does not support %s", call.Callee.String(), args[0].TypeName())
	}
}

func collectionsReverse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	switch value := args[0].(type) {
	case *runtime.List:
		out := make([]runtime.Value, len(value.Elements))
		for i := range value.Elements {
			out[len(value.Elements)-1-i] = value.Elements[i]
		}
		return &runtime.List{Elements: out}, nil
	case runtime.String:
		runes := []rune(value.Value)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return runtime.String{Value: string(runes)}, nil
	case runtime.Bytes:
		out := append([]byte(nil), value.Value...)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return runtime.Bytes{Value: out}, nil
	default:
		return nil, fmt.Errorf("%s does not support %s", call.Callee.String(), args[0].TypeName())
	}
}

func (e *Evaluator) collectionsSort(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects list", call.Callee.String())
	}
	out := append([]runtime.Value(nil), list.Elements...)
	var compareErr error
	sort.SliceStable(out, func(i, j int) bool {
		if compareErr != nil {
			return false
		}
		cmp, err := e.compareForOrdering(out[i], out[j])
		if err != nil {
			compareErr = err
			return false
		}
		return cmp < 0
	})
	if compareErr != nil {
		return nil, compareErr
	}
	return &runtime.List{Elements: out}, nil
}

func collectionsJoin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects list and separator", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects list", call.Callee.String())
	}
	sep, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s separator must be string", call.Callee.String())
	}
	parts := make([]string, 0, len(list.Elements))
	for _, element := range list.Elements {
		if text, ok := element.(runtime.String); ok {
			parts = append(parts, text.Value)
		} else {
			parts = append(parts, element.Inspect())
		}
	}
	return runtime.String{Value: strings.Join(parts, sep.Value)}, nil
}

type collectionsIterator struct {
	next  func() (runtime.Value, bool, error)
	close func()
}

func collectionsIntArg(value runtime.Value, label string) (*big.Int, error) {
	intValue, ok := native.IntValueToBigInt(value)
	if !ok {
		return nil, fmt.Errorf("%s must be int", label)
	}
	return new(big.Int).Set(intValue), nil
}

func collectionsRangeContains(current, end, step *big.Int, exclusive bool) bool {
	cmp := current.Cmp(end)
	if step.Sign() > 0 {
		if exclusive {
			return cmp < 0
		}
		return cmp <= 0
	}
	if exclusive {
		return cmp > 0
	}
	return cmp >= 0
}

func collectionsIteratorFor(value runtime.Value, label string) (collectionsIterator, error) {
	switch v := value.(type) {
	case *runtime.List:
		index := 0
		return collectionsIterator{next: func() (runtime.Value, bool, error) {
			if index >= len(v.Elements) {
				return nil, false, nil
			}
			next := v.Elements[index]
			index++
			return next, true, nil
		}}, nil
	case *runtime.Generator:
		return collectionsIterator{next: v.Next, close: v.Close}, nil
	case runtime.Range:
		current := new(big.Int).Set(v.Start)
		end := new(big.Int).Set(v.End)
		step := new(big.Int).Set(v.Step)
		return collectionsIterator{next: func() (runtime.Value, bool, error) {
			if !collectionsRangeContains(current, end, step, v.Exclusive) {
				return nil, false, nil
			}
			out := runtime.Int{Value: new(big.Int).Set(current)}
			current.Add(current, step)
			return out, true, nil
		}}, nil
	default:
		return collectionsIterator{}, fmt.Errorf("%s expects iterable", label)
	}
}

func collectionsRange(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects end, optional start/end, or start/end/step", call.Callee.String())
	}
	start := big.NewInt(0)
	end, err := collectionsIntArg(args[0], call.Callee.String()+" end")
	if err != nil {
		return nil, err
	}
	if len(args) >= 2 {
		start, err = collectionsIntArg(args[0], call.Callee.String()+" start")
		if err != nil {
			return nil, err
		}
		end, err = collectionsIntArg(args[1], call.Callee.String()+" end")
		if err != nil {
			return nil, err
		}
	}
	step := big.NewInt(1)
	if len(args) == 3 {
		step, err = collectionsIntArg(args[2], call.Callee.String()+" step")
		if err != nil {
			return nil, err
		}
		if step.Sign() == 0 {
			return nil, fmt.Errorf("%s step cannot be zero", call.Callee.String())
		}
	}
	current := new(big.Int).Set(start)
	return runtime.NewGenerator(func() (runtime.Value, bool, error) {
		if !collectionsRangeContains(current, end, step, true) {
			return nil, false, nil
		}
		out := runtime.Int{Value: new(big.Int).Set(current)}
		current.Add(current, step)
		return out, true, nil
	}), nil
}

func collectionsTake(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects iterable and count", call.Callee.String())
	}
	source, err := collectionsIteratorFor(args[0], call.Callee.String())
	if err != nil {
		return nil, err
	}
	count, err := collectionsIntArg(args[1], call.Callee.String()+" count")
	if err != nil {
		return nil, err
	}
	if count.Sign() < 0 {
		return nil, fmt.Errorf("%s count cannot be negative", call.Callee.String())
	}
	remaining := new(big.Int).Set(count)
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		if remaining.Sign() <= 0 {
			if source.close != nil {
				source.close()
			}
			return nil, false, nil
		}
		next, ok, err := source.next()
		if err != nil || !ok {
			if source.close != nil {
				source.close()
			}
			return next, ok, err
		}
		remaining.Sub(remaining, big.NewInt(1))
		return next, true, nil
	}, func() {
		if source.close != nil {
			source.close()
		}
	}), nil
}

func (e *Evaluator) collectionsLazyMap(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects iterable and function", call.Callee.String())
	}
	source, err := collectionsIteratorFor(args[0], call.Callee.String())
	if err != nil {
		return nil, err
	}
	fn := args[1]
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		next, ok, err := source.next()
		if err != nil || !ok {
			if source.close != nil {
				source.close()
			}
			return next, ok, err
		}
		mapped, err := e.callValue(fn, []runtime.Value{next})
		if err != nil {
			if source.close != nil {
				source.close()
			}
			return nil, false, err
		}
		return mapped, true, nil
	}, func() {
		if source.close != nil {
			source.close()
		}
	}), nil
}

func (e *Evaluator) collectionsLazyFilter(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects iterable and function", call.Callee.String())
	}
	source, err := collectionsIteratorFor(args[0], call.Callee.String())
	if err != nil {
		return nil, err
	}
	fn := args[1]
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		for {
			next, ok, err := source.next()
			if err != nil || !ok {
				if source.close != nil {
					source.close()
				}
				return next, ok, err
			}
			keep, err := e.callValue(fn, []runtime.Value{next})
			if err != nil {
				if source.close != nil {
					source.close()
				}
				return nil, false, err
			}
			if isTruthy(keep) {
				return next, true, nil
			}
		}
	}, func() {
		if source.close != nil {
			source.close()
		}
	}), nil
}

func (e *Evaluator) collectionsMethod(name string, arity int) builtinFunc {
	return func(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
		if arity >= 0 && len(args) != arity {
			return nil, fmt.Errorf("%s expects %d argument(s)", call.Callee.String(), arity)
		}
		if name == "sorted" && len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("%s expects list and optional comparison function", call.Callee.String())
		}
		if name == "sortBy" && len(args) != 2 && len(args) != 3 {
			return nil, fmt.Errorf("%s expects a collection, selector, and optional descending flag", call.Callee.String())
		}
		if len(args) == 0 {
			return nil, fmt.Errorf("%s expects a collection", call.Callee.String())
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s expects list", call.Callee.String())
		}
		return e.evalMethodCall(list, name, args[1:])
	}
}

func (e *Evaluator) collectionsGraphMethod(name string, extraArgs int) builtinFunc {
	return func(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1+extraArgs {
			return nil, fmt.Errorf("%s expects %d argument(s)", call.Callee.String(), 1+extraArgs)
		}
		graph, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s expects dict as first argument (adjacency graph)", call.Callee.String())
		}
		return e.evalMethodCall(graph, name, args[1:])
	}
}

func valuesEqualSimple(left runtime.Value, right runtime.Value) bool {
	switch leftValue := left.(type) {
	case *runtime.List:
		rightValue, ok := right.(*runtime.List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for i, element := range leftValue.Elements {
			if !valuesEqualSimple(element, rightValue.Elements[i]) {
				return false
			}
		}
		return true
	case runtime.Dict:
		rightValue, ok := right.(runtime.Dict)
		if !ok || leftValue.Len() != rightValue.Len() {
			return false
		}
		equal := true
		leftValue.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			other, ok := rightValue.GetEntry(key)
			if !ok || !valuesEqualSimple(entry.Key, other.Key) || !valuesEqualSimple(entry.Value, other.Value) {
				equal = false
				return false
			}
			return true
		})
		return equal
	case runtime.Set:
		rightValue, ok := right.(runtime.Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok || !valuesEqualSimple(entry.Value, other.Value) {
				return false
			}
		}
		return true
	default:
		return primitiveEqual(left, right)
	}
}
