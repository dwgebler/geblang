package bytecode

import (
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"math/big"
	"sort"
	"strings"
)

func (vm *VM) collectionsNativeCall(fn string, args []runtime.Value) (runtime.Value, error) {
	// The module-form HOF (collections.map/filter/...) reaches listHigherOrderMethod with a bare instruction; carry the native call site so a throwing callback's caller frame shows it.
	savedHofLine := vm.hofCallLine
	vm.hofCallLine = vm.nativeCallLine
	defer func() { vm.hofCallLine = savedHofLine }()
	switch fn {
	case "length":
		if len(args) != 1 {
			return nil, fmt.Errorf("collections.length expects one argument")
		}
		switch v := args[0].(type) {
		case *runtime.List:
			return runtime.SmallInt{Value: int64(len(v.Elements))}, nil
		case runtime.Dict:
			return runtime.SmallInt{Value: int64(v.Len())}, nil
		case runtime.Set:
			return runtime.SmallInt{Value: int64(len(v.Elements))}, nil
		case runtime.String:
			return runtime.SmallInt{Value: int64(len([]rune(v.Value)))}, nil
		default:
			return nil, fmt.Errorf("collections.length does not support %s", args[0].TypeName())
		}
	case "isEmpty":
		if len(args) != 1 {
			return nil, fmt.Errorf("collections.isEmpty expects one argument")
		}
		switch v := args[0].(type) {
		case *runtime.List:
			return runtime.Bool{Value: len(v.Elements) == 0}, nil
		case runtime.Dict:
			return runtime.Bool{Value: v.Len() == 0}, nil
		case runtime.Set:
			return runtime.Bool{Value: len(v.Elements) == 0}, nil
		case runtime.String:
			return runtime.Bool{Value: len(v.Value) == 0}, nil
		default:
			return nil, fmt.Errorf("collections.isEmpty does not support %s", args[0].TypeName())
		}
	case "contains":
		if len(args) != 2 {
			return nil, fmt.Errorf("collections.contains expects two arguments")
		}
		switch v := args[0].(type) {
		case *runtime.List:
			for _, el := range v.Elements {
				if valuesEqual(el, args[1]) {
					return runtime.Bool{Value: true}, nil
				}
			}
			return runtime.Bool{Value: false}, nil
		case runtime.Dict:
			_, ok := v.GetEntry(dictKeyFor(args[1]))
			return runtime.Bool{Value: ok}, nil
		case runtime.Set:
			_, ok := v.Elements[dictKeyFor(args[1])]
			return runtime.Bool{Value: ok}, nil
		case runtime.String:
			sub, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("collections.contains string needle must be string")
			}
			return runtime.Bool{Value: strings.Contains(v.Value, sub.Value)}, nil
		default:
			return nil, fmt.Errorf("collections.contains does not support %s", args[0].TypeName())
		}
	case "reverse":
		if len(args) != 1 {
			return nil, fmt.Errorf("collections.reverse expects one argument")
		}
		switch v := args[0].(type) {
		case *runtime.List:
			out := make([]runtime.Value, len(v.Elements))
			for i, el := range v.Elements {
				out[len(v.Elements)-1-i] = el
			}
			return &runtime.List{Elements: out}, nil
		case runtime.String:
			runes := []rune(v.Value)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return runtime.String{Value: string(runes)}, nil
		case runtime.Bytes:
			out := append([]byte(nil), v.Value...)
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return runtime.Bytes{Value: out}, nil
		default:
			return nil, fmt.Errorf("collections.reverse does not support %s", args[0].TypeName())
		}
	case "sort":
		if len(args) != 1 {
			return nil, fmt.Errorf("collections.sort expects one argument")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("collections.sort expects list")
		}
		out := make([]runtime.Value, len(list.Elements))
		copy(out, list.Elements)
		var sortErr error
		sort.SliceStable(out, func(i, j int) bool {
			if sortErr != nil {
				return false
			}
			cmp, err := valuesCompare(out[i], out[j])
			if err != nil {
				sortErr = err
				return false
			}
			return cmp < 0
		})
		if sortErr != nil {
			return nil, sortErr
		}
		return &runtime.List{Elements: out}, nil
	case "join":
		if len(args) != 2 {
			return nil, fmt.Errorf("collections.join expects list and separator")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("collections.join expects list")
		}
		sep, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("collections.join separator must be string")
		}
		parts := make([]string, 0, len(list.Elements))
		for _, el := range list.Elements {
			if s, ok := el.(runtime.String); ok {
				parts = append(parts, s.Value)
			} else {
				parts = append(parts, el.Inspect())
			}
		}
		return runtime.String{Value: strings.Join(parts, sep.Value)}, nil
	case "range":
		return collectionsNativeRange(args)
	case "take":
		return vm.collectionsNativeTake(args)
	case "lazyMap":
		return vm.collectionsNativeLazyMap(args)
	case "lazyFilter":
		return vm.collectionsNativeLazyFilter(args)
	case "bfs", "dfs", "topologicalSort", "shortestPath":
		if len(args) == 0 {
			return nil, fmt.Errorf("collections.%s expects a graph dict as first argument", fn)
		}
		graph, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("collections.%s expects dict as first argument (adjacency graph)", fn)
		}
		result, _, err := vm.dictCollectionsMethod(graph, fn, args[1:])
		return result, err
	case "map", "filter", "reduce", "find", "any", "all", "flatten", "unique", "zip", "sorted",
		"groupBy", "partition",
		"findLast", "containsBy", "indexBy", "binarySearch", "lowerBound", "upperBound",
		"minBy", "maxBy", "sortBy", "topBy", "sumBy", "averageBy",
		"topK", "bottomK", "frequencies", "mode",
		"difference", "intersection", "differenceBy", "intersectionBy", "zipWith",
		"flatMap", "uniqueBy", "takeWhile", "dropWhile", "windowed", "unzip", "scan", "enumerate":
		if len(args) == 0 {
			return nil, fmt.Errorf("collections.%s expects at least a collection argument", fn)
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("collections.%s expects list as first argument", fn)
		}
		result, _, err := vm.listHigherOrderMethod(Instruction{}, list, fn, args[1:])
		return result, err
	case "chunk":
		if len(args) != 2 {
			return nil, fmt.Errorf("collections.chunk expects list and size")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("collections.chunk expects list as first argument")
		}
		result, _, err := vm.listHigherOrderMethod(Instruction{}, list, "chunk", args[1:])
		return result, err
	default:
		return nil, fmt.Errorf("unknown collections function: %s", fn)
	}
}

type collectionsNativeIterator struct {
	next  func() (runtime.Value, bool, error)
	close func()
}

func collectionsNativeIntArg(value runtime.Value, label string) (*big.Int, error) {
	nb, ok := native.IntValueToBigInt(value)
	if !ok {
		return nil, fmt.Errorf("%s must be int", label)
	}
	return new(big.Int).Set(nb), nil
}

func collectionsNativeRangeContains(current, end, step *big.Int, exclusive bool) bool {
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

func collectionsNativeIteratorFor(value runtime.Value, label string) (collectionsNativeIterator, error) {
	switch v := value.(type) {
	case *runtime.List:
		index := 0
		return collectionsNativeIterator{next: func() (runtime.Value, bool, error) {
			if index >= len(v.Elements) {
				return nil, false, nil
			}
			next := v.Elements[index]
			index++
			return next, true, nil
		}}, nil
	case *runtime.Generator:
		return collectionsNativeIterator{next: v.Next, close: v.Close}, nil
	case runtime.Range:
		current := new(big.Int).Set(v.Start)
		end := new(big.Int).Set(v.End)
		step := new(big.Int).Set(v.Step)
		return collectionsNativeIterator{next: func() (runtime.Value, bool, error) {
			if !collectionsNativeRangeContains(current, end, step, v.Exclusive) {
				return nil, false, nil
			}
			out := runtime.Int{Value: new(big.Int).Set(current)}
			current.Add(current, step)
			return out, true, nil
		}}, nil
	default:
		return collectionsNativeIterator{}, fmt.Errorf("%s expects iterable", label)
	}
}

func collectionsNativeRange(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("collections.range expects end, optional start/end, or start/end/step")
	}
	start := big.NewInt(0)
	end, err := collectionsNativeIntArg(args[0], "collections.range end")
	if err != nil {
		return nil, err
	}
	if len(args) >= 2 {
		start, err = collectionsNativeIntArg(args[0], "collections.range start")
		if err != nil {
			return nil, err
		}
		end, err = collectionsNativeIntArg(args[1], "collections.range end")
		if err != nil {
			return nil, err
		}
	}
	step := big.NewInt(1)
	if len(args) == 3 {
		step, err = collectionsNativeIntArg(args[2], "collections.range step")
		if err != nil {
			return nil, err
		}
		if step.Sign() == 0 {
			return nil, fmt.Errorf("collections.range step cannot be zero")
		}
	}
	current := new(big.Int).Set(start)
	return runtime.NewGenerator(func() (runtime.Value, bool, error) {
		if !collectionsNativeRangeContains(current, end, step, true) {
			return nil, false, nil
		}
		out := runtime.Int{Value: new(big.Int).Set(current)}
		current.Add(current, step)
		return out, true, nil
	}), nil
}

func (vm *VM) collectionsNativeTake(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("collections.take expects iterable and count")
	}
	source, err := collectionsNativeIteratorFor(args[0], "collections.take")
	if err != nil {
		return nil, err
	}
	count, err := collectionsNativeIntArg(args[1], "collections.take count")
	if err != nil {
		return nil, err
	}
	if count.Sign() < 0 {
		return nil, fmt.Errorf("collections.take count cannot be negative")
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

func (vm *VM) collectionsNativeLazyMap(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("collections.lazyMap expects iterable and function")
	}
	source, err := collectionsNativeIteratorFor(args[0], "collections.lazyMap")
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
		mapped, err := vm.callCallable(fn, []runtime.Value{next})
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

func (vm *VM) collectionsNativeLazyFilter(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("collections.lazyFilter expects iterable and function")
	}
	source, err := collectionsNativeIteratorFor(args[0], "collections.lazyFilter")
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
			keep, err := vm.callCallable(fn, []runtime.Value{next})
			if err != nil {
				if source.close != nil {
					source.close()
				}
				return nil, false, err
			}
			if b, ok := keep.(runtime.Bool); ok && b.Value {
				return next, true, nil
			}
		}
	}, func() {
		if source.close != nil {
			source.close()
		}
	}), nil
}

func valuesCompare(left, right runtime.Value) (int, error) {
	if ls, ok := left.(runtime.String); ok {
		if rs, ok := right.(runtime.String); ok {
			if ls.Value < rs.Value {
				return -1, nil
			}
			if ls.Value > rs.Value {
				return 1, nil
			}
			return 0, nil
		}
	}
	return native.NumericCompare(left, right)
}

// sortElements stably sorts elements in place, via the optional
// less/comparator callback in args.
func (vm *VM) sortElements(elements []runtime.Value, args []runtime.Value) error {
	var sortErr error
	sort.SliceStable(elements, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		if len(args) == 1 {
			result, err := vm.callCallable(args[0], []runtime.Value{elements[i], elements[j]})
			if err != nil {
				sortErr = err
				return false
			}
			less, err := native.SortLess(result)
			if err != nil {
				sortErr = err
				return false
			}
			return less
		}
		cmp, err := valuesCompare(elements[i], elements[j])
		if err != nil {
			sortErr = err
			return false
		}
		return cmp < 0
	})
	return sortErr
}

func (vm *VM) listHigherOrderMethod(instruction Instruction, list *runtime.List, name string, args []runtime.Value) (runtime.Value, bool, error) {
	switch name {
	case "map":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.map expects one argument (function)")
		}
		result := make([]runtime.Value, len(list.Elements))
		for i, el := range list.Elements {
			mapped, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			result[i] = mapped
		}
		return &runtime.List{Elements: result}, true, nil
	case "filter":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.filter expects one argument (function)")
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			keep, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := keep.(runtime.Bool); ok && b.Value {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "search":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.search expects one argument")
		}
		matches := []runtime.Value{}
		if runtime.IsCallableValue(args[0]) {
			for i, el := range list.Elements {
				keep, err := vm.callCallable(args[0], []runtime.Value{el})
				if err != nil {
					return nil, true, err
				}
				if b, ok := keep.(runtime.Bool); ok && b.Value {
					matches = append(matches, runtime.NewInt64(int64(i)))
				}
			}
		} else {
			for i, el := range list.Elements {
				if valuesEqual(el, args[0]) {
					matches = append(matches, runtime.NewInt64(int64(i)))
				}
			}
		}
		return &runtime.List{Elements: matches}, true, nil
	case "searchPattern":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.searchPattern expects one argument (regex)")
		}
		pat, ok := args[0].(runtime.String)
		if !ok {
			return nil, true, fmt.Errorf("list.searchPattern expects a string pattern")
		}
		re, err := native.CompileSearchRegex(pat.Value)
		if err != nil {
			return nil, true, vmTypedError{class: "ValueError", message: fmt.Sprintf("invalid regex: %v", err)}
		}
		matches := []runtime.Value{}
		for i, el := range list.Elements {
			if s, ok := el.(runtime.String); ok && re.MatchString(s.Value) {
				matches = append(matches, runtime.NewInt64(int64(i)))
			}
		}
		return &runtime.List{Elements: matches}, true, nil
	case "reduce":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.reduce expects two arguments (function, initial)")
		}
		acc := args[1]
		for _, el := range list.Elements {
			next, err := vm.callCallable(args[0], []runtime.Value{acc, el})
			if err != nil {
				return nil, true, err
			}
			acc = next
		}
		return acc, true, nil
	case "find":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.find expects one argument (function)")
		}
		for _, el := range list.Elements {
			match, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := match.(runtime.Bool); ok && b.Value {
				return el, true, nil
			}
		}
		return runtime.Null{}, true, nil
	case "any":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.any expects one argument (function)")
		}
		for _, el := range list.Elements {
			result, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				return runtime.Bool{Value: true}, true, nil
			}
		}
		return runtime.Bool{Value: false}, true, nil
	case "all":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.all expects one argument (function)")
		}
		for _, el := range list.Elements {
			result, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && !b.Value {
				return runtime.Bool{Value: false}, true, nil
			}
		}
		return runtime.Bool{Value: true}, true, nil
	case "count":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.count expects one argument (function)")
		}
		n := 0
		for _, el := range list.Elements {
			result, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				n++
			}
		}
		return runtime.NewInt64(int64(n)), true, nil
	case "sorted":
		if len(args) > 1 {
			return nil, true, fmt.Errorf("list.%s expects zero or one argument", name)
		}
		newElements := make([]runtime.Value, len(list.Elements))
		copy(newElements, list.Elements)
		if sortErr := vm.sortElements(newElements, args); sortErr != nil {
			return nil, true, sortErr
		}
		return &runtime.List{Elements: newElements}, true, nil
	case "sort":
		if len(args) > 1 {
			return nil, true, fmt.Errorf("list.%s expects zero or one argument", name)
		}
		if list.Frozen {
			return nil, true, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if sortErr := vm.sortElements(list.Elements, args); sortErr != nil {
			return nil, true, sortErr
		}
		return list, true, nil
	case "reversed":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.%s expects no arguments", name)
		}
		newElements := make([]runtime.Value, len(list.Elements))
		for i, el := range list.Elements {
			newElements[len(list.Elements)-1-i] = el
		}
		return &runtime.List{Elements: newElements, ElementTypes: list.ElementTypes}, true, nil
	case "reverse":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.%s expects no arguments", name)
		}
		if list.Frozen {
			return nil, true, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		for i, j := 0, len(list.Elements)-1; i < j; i, j = i+1, j-1 {
			list.Elements[i], list.Elements[j] = list.Elements[j], list.Elements[i]
		}
		return list, true, nil
	case "prepend", "unshift":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.%s expects one argument", name)
		}
		if list.Frozen {
			return nil, true, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if len(list.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[0], list.ElementTypes[0]) {
			return nil, true, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot %s %s to list<%s>", name, args[0].TypeName(), list.ElementTypes[0])}
		}
		list.Elements = append(list.Elements, nil)
		copy(list.Elements[1:], list.Elements)
		list.Elements[0] = args[0]
		return list, true, nil
	case "remove":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.remove expects one argument")
		}
		if list.Frozen {
			return nil, true, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		for i, el := range list.Elements {
			if valuesEqual(el, args[0]) {
				list.Elements = append(list.Elements[:i], list.Elements[i+1:]...)
				break
			}
		}
		return list, true, nil
	case "flatten":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.flatten expects no arguments")
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			if nested, ok := el.(*runtime.List); ok {
				result = append(result, nested.Elements...)
			} else {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "unique":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.unique expects no arguments")
		}
		seen := make([]runtime.Value, 0, len(list.Elements))
		var result []runtime.Value
		for _, el := range list.Elements {
			found := false
			for _, s := range seen {
				if valuesEqual(el, s) {
					found = true
					break
				}
			}
			if !found {
				seen = append(seen, el)
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "zip":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.zip expects one argument (list)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.zip expects list argument")
		}
		n := len(list.Elements)
		if len(other.Elements) < n {
			n = len(other.Elements)
		}
		result := make([]runtime.Value, n)
		for i := 0; i < n; i++ {
			result[i] = &runtime.List{Elements: []runtime.Value{list.Elements[i], other.Elements[i]}}
		}
		return &runtime.List{Elements: result}, true, nil
	case "groupBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.groupBy expects one argument (function)")
		}
		entries := map[string]runtime.DictEntry{}
		for _, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			dk := native.DictKey(key)
			existing, ok := entries[dk]
			if !ok {
				existing = runtime.DictEntry{Key: key, Value: &runtime.List{}}
			}
			existing.Value = &runtime.List{Elements: append(existing.Value.(*runtime.List).Elements, el)}
			entries[dk] = existing
		}
		return runtime.Dict{Entries: entries}, true, nil
	case "chunk":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.chunk expects one argument (size)")
		}
		nInt, ok := native.AsInt64(args[0])
		if !ok {
			return nil, true, fmt.Errorf("list.chunk size must be int")
		}
		n := int(nInt)
		if n <= 0 {
			return nil, true, fmt.Errorf("list.chunk size must be positive")
		}
		var chunks []runtime.Value
		for i := 0; i < len(list.Elements); i += n {
			end := i + n
			if end > len(list.Elements) {
				end = len(list.Elements)
			}
			chunks = append(chunks, &runtime.List{Elements: append([]runtime.Value(nil), list.Elements[i:end]...)})
		}
		return &runtime.List{Elements: chunks}, true, nil
	case "partition":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.partition expects one argument (function)")
		}
		var yes, no []runtime.Value
		for _, el := range list.Elements {
			result, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				yes = append(yes, el)
			} else {
				no = append(no, el)
			}
		}
		return &runtime.List{Elements: []runtime.Value{
			&runtime.List{Elements: yes},
			&runtime.List{Elements: no},
		}}, true, nil
	case "enumerate":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.enumerate expects no arguments")
		}
		result := make([]runtime.Value, len(list.Elements))
		for i, el := range list.Elements {
			result[i] = &runtime.List{Elements: []runtime.Value{runtime.NewInt64(int64(i)), el}}
		}
		return &runtime.List{Elements: result}, true, nil
	case "flatMap":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.flatMap expects one argument (function)")
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			mapped, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			nested, ok := mapped.(*runtime.List)
			if !ok {
				return nil, true, fmt.Errorf("list.flatMap function must return a list")
			}
			result = append(result, nested.Elements...)
		}
		return &runtime.List{Elements: result}, true, nil
	case "uniqueBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.uniqueBy expects one argument (function)")
		}
		seenKeys := make([]runtime.Value, 0, len(list.Elements))
		var result []runtime.Value
		for _, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			found := false
			for _, s := range seenKeys {
				if valuesEqual(key, s) {
					found = true
					break
				}
			}
			if !found {
				seenKeys = append(seenKeys, key)
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "takeWhile":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.takeWhile expects one argument (function)")
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			keep, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := keep.(runtime.Bool); !ok || !b.Value {
				break
			}
			result = append(result, el)
		}
		return &runtime.List{Elements: result}, true, nil
	case "dropWhile":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.dropWhile expects one argument (function)")
		}
		dropping := true
		var result []runtime.Value
		for _, el := range list.Elements {
			if dropping {
				keep, err := vm.callCallable(args[0], []runtime.Value{el})
				if err != nil {
					return nil, true, err
				}
				if b, ok := keep.(runtime.Bool); ok && b.Value {
					continue
				}
				dropping = false
			}
			result = append(result, el)
		}
		return &runtime.List{Elements: result}, true, nil
	case "windowed":
		if len(args) != 1 && len(args) != 2 {
			return nil, true, fmt.Errorf("list.windowed expects size and optional step")
		}
		sizeInt, ok := native.AsInt64(args[0])
		if !ok {
			return nil, true, fmt.Errorf("list.windowed size must be int")
		}
		step := int64(1)
		if len(args) == 2 {
			step, ok = native.AsInt64(args[1])
			if !ok {
				return nil, true, fmt.Errorf("list.windowed step must be int")
			}
		}
		size := int(sizeInt)
		if size <= 0 || step <= 0 {
			return nil, true, fmt.Errorf("list.windowed size and step must be positive")
		}
		var windows []runtime.Value
		for i := 0; i+size <= len(list.Elements); i += int(step) {
			windows = append(windows, &runtime.List{Elements: append([]runtime.Value(nil), list.Elements[i:i+size]...)})
		}
		return &runtime.List{Elements: windows}, true, nil
	case "unzip":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.unzip expects no arguments")
		}
		firsts := make([]runtime.Value, 0, len(list.Elements))
		seconds := make([]runtime.Value, 0, len(list.Elements))
		for _, el := range list.Elements {
			pair, ok := el.(*runtime.List)
			if !ok || len(pair.Elements) != 2 {
				return nil, true, fmt.Errorf("list.unzip expects a list of 2-element lists")
			}
			firsts = append(firsts, pair.Elements[0])
			seconds = append(seconds, pair.Elements[1])
		}
		return &runtime.List{Elements: []runtime.Value{
			&runtime.List{Elements: firsts},
			&runtime.List{Elements: seconds},
		}}, true, nil
	case "scan":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.scan expects two arguments (initial, function)")
		}
		acc := args[0]
		result := []runtime.Value{acc}
		for _, el := range list.Elements {
			next, err := vm.callCallable(args[1], []runtime.Value{acc, el})
			if err != nil {
				return nil, true, err
			}
			acc = next
			result = append(result, acc)
		}
		return &runtime.List{Elements: result}, true, nil
	case "findLast":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.findLast expects one argument (function)")
		}
		for i := len(list.Elements) - 1; i >= 0; i-- {
			result, err := vm.callCallable(args[0], []runtime.Value{list.Elements[i]})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				return list.Elements[i], true, nil
			}
		}
		return runtime.Null{}, true, nil
	case "containsBy":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.containsBy expects two arguments (value, function)")
		}
		target, fn := args[0], args[1]
		for _, el := range list.Elements {
			key, err := vm.callCallable(fn, []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if valuesEqual(key, target) {
				return runtime.Bool{Value: true}, true, nil
			}
		}
		return runtime.Bool{Value: false}, true, nil
	case "indexBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.indexBy expects one argument (function)")
		}
		for i, el := range list.Elements {
			result, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if b, ok := result.(runtime.Bool); ok && b.Value {
				return runtime.NewInt64(int64(i)), true, nil
			}
		}
		return runtime.NewInt64(-1), true, nil
	case "binarySearch":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.binarySearch expects one argument (value)")
		}
		target := args[0]
		lo, hi := 0, len(list.Elements)
		for lo < hi {
			mid := (lo + hi) / 2
			cmp, err := valuesCompare(list.Elements[mid], target)
			if err != nil {
				return nil, true, err
			}
			if cmp == 0 {
				return runtime.NewInt64(int64(mid)), true, nil
			} else if cmp < 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return runtime.NewInt64(-1), true, nil
	case "binarySearchBy":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.binarySearchBy expects a selector and a target key")
		}
		target := args[1]
		lo, hi := 0, len(list.Elements)
		for lo < hi {
			mid := (lo + hi) / 2
			key, err := vm.callCallable(args[0], []runtime.Value{list.Elements[mid]})
			if err != nil {
				return nil, true, err
			}
			cmp, err := valuesCompare(key, target)
			if err != nil {
				return nil, true, err
			}
			if cmp == 0 {
				return runtime.NewInt64(int64(mid)), true, nil
			} else if cmp < 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return runtime.NewInt64(-1), true, nil
	case "lowerBound":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.lowerBound expects one argument (value)")
		}
		target := args[0]
		lo, hi := 0, len(list.Elements)
		for lo < hi {
			mid := (lo + hi) / 2
			cmp, err := valuesCompare(list.Elements[mid], target)
			if err != nil {
				return nil, true, err
			}
			if cmp < 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return runtime.NewInt64(int64(lo)), true, nil
	case "upperBound":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.upperBound expects one argument (value)")
		}
		target := args[0]
		lo, hi := 0, len(list.Elements)
		for lo < hi {
			mid := (lo + hi) / 2
			cmp, err := valuesCompare(list.Elements[mid], target)
			if err != nil {
				return nil, true, err
			}
			if cmp <= 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return runtime.NewInt64(int64(lo)), true, nil
	case "minBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.minBy expects one argument (function)")
		}
		if len(list.Elements) == 0 {
			return runtime.Null{}, true, nil
		}
		best := list.Elements[0]
		bestKey, err := vm.callCallable(args[0], []runtime.Value{best})
		if err != nil {
			return nil, true, err
		}
		for _, el := range list.Elements[1:] {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			cmp, err := valuesCompare(key, bestKey)
			if err != nil {
				return nil, true, err
			}
			if cmp < 0 {
				best, bestKey = el, key
			}
		}
		return best, true, nil
	case "maxBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.maxBy expects one argument (function)")
		}
		if len(list.Elements) == 0 {
			return runtime.Null{}, true, nil
		}
		best := list.Elements[0]
		bestKey, err := vm.callCallable(args[0], []runtime.Value{best})
		if err != nil {
			return nil, true, err
		}
		for _, el := range list.Elements[1:] {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			cmp, err := valuesCompare(key, bestKey)
			if err != nil {
				return nil, true, err
			}
			if cmp > 0 {
				best, bestKey = el, key
			}
		}
		return best, true, nil
	case "sortBy":
		if len(args) != 1 && len(args) != 2 {
			return nil, true, fmt.Errorf("list.sortBy expects a selector and an optional descending flag")
		}
		if list.Frozen {
			return nil, true, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		descending := false
		if len(args) == 2 {
			b, ok := args[1].(runtime.Bool)
			if !ok {
				return nil, true, fmt.Errorf("list.sortBy descending flag must be a bool")
			}
			descending = b.Value
		}
		type keyedEl struct {
			key runtime.Value
			el  runtime.Value
		}
		pairs := make([]keyedEl, len(list.Elements))
		for i, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			pairs[i] = keyedEl{key, el}
		}
		var sortErr error
		sort.SliceStable(pairs, func(i, j int) bool {
			if sortErr != nil {
				return false
			}
			cmp, err := valuesCompare(pairs[i].key, pairs[j].key)
			if err != nil {
				sortErr = err
				return false
			}
			if descending {
				return cmp > 0
			}
			return cmp < 0
		})
		if sortErr != nil {
			return nil, true, sortErr
		}
		for i, p := range pairs {
			list.Elements[i] = p.el
		}
		return list, true, nil
	case "topBy":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.topBy expects two arguments (function, count)")
		}
		nVal, nOk := native.AsInt64(args[1])
		if !nOk {
			return nil, true, fmt.Errorf("list.topBy: count must be an integer")
		}
		n := int(nVal)
		type keyedEl struct {
			key runtime.Value
			el  runtime.Value
		}
		pairs := make([]keyedEl, len(list.Elements))
		for i, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			pairs[i] = keyedEl{key, el}
		}
		var sortErr error
		sort.SliceStable(pairs, func(i, j int) bool {
			if sortErr != nil {
				return false
			}
			cmp, err := valuesCompare(pairs[i].key, pairs[j].key)
			if err != nil {
				sortErr = err
				return false
			}
			return cmp > 0
		})
		if sortErr != nil {
			return nil, true, sortErr
		}
		if n < 0 {
			n = 0
		}
		if n > len(pairs) {
			n = len(pairs)
		}
		result := make([]runtime.Value, n)
		for i := 0; i < n; i++ {
			result[i] = pairs[i].el
		}
		return &runtime.List{Elements: result}, true, nil
	case "sumBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.sumBy expects one argument (function)")
		}
		sum := new(big.Rat)
		hasFloat := false
		var floatSum float64
		for _, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			switch k := key.(type) {
			case runtime.SmallInt:
				if hasFloat {
					floatSum += float64(k.Value)
				} else {
					sum.Add(sum, new(big.Rat).SetInt64(k.Value))
				}
			case runtime.Int:
				if hasFloat {
					f, _ := new(big.Float).SetInt(k.Value).Float64()
					floatSum += f
				} else {
					sum.Add(sum, new(big.Rat).SetInt(k.Value))
				}
			case runtime.Decimal:
				if hasFloat {
					f, _ := k.Value.Float64()
					floatSum += f
				} else {
					sum.Add(sum, k.Value)
				}
			case runtime.Float:
				if !hasFloat {
					floatSum, _ = sum.Float64()
					hasFloat = true
				}
				floatSum += k.Value
			default:
				return nil, true, fmt.Errorf("list.sumBy: selector must return a number, got %s", key.TypeName())
			}
		}
		if hasFloat {
			return runtime.Float{Value: floatSum}, true, nil
		}
		if sum.IsInt() {
			n := new(big.Int).Set(sum.Num())
			if n.IsInt64() {
				return runtime.SmallInt{Value: n.Int64()}, true, nil
			}
			return runtime.Int{Value: n}, true, nil
		}
		return runtime.Decimal{Value: sum}, true, nil
	case "averageBy":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.averageBy expects one argument (function)")
		}
		if len(list.Elements) == 0 {
			return runtime.Null{}, true, nil
		}
		sum := new(big.Rat)
		hasFloat := false
		var floatSum float64
		for _, el := range list.Elements {
			key, err := vm.callCallable(args[0], []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			switch k := key.(type) {
			case runtime.SmallInt:
				if hasFloat {
					floatSum += float64(k.Value)
				} else {
					sum.Add(sum, new(big.Rat).SetInt64(k.Value))
				}
			case runtime.Int:
				if hasFloat {
					f, _ := new(big.Float).SetInt(k.Value).Float64()
					floatSum += f
				} else {
					sum.Add(sum, new(big.Rat).SetInt(k.Value))
				}
			case runtime.Decimal:
				if hasFloat {
					f, _ := k.Value.Float64()
					floatSum += f
				} else {
					sum.Add(sum, k.Value)
				}
			case runtime.Float:
				if !hasFloat {
					floatSum, _ = sum.Float64()
					hasFloat = true
				}
				floatSum += k.Value
			default:
				return nil, true, fmt.Errorf("list.averageBy: selector must return a number, got %s", key.TypeName())
			}
		}
		count := int64(len(list.Elements))
		if hasFloat {
			return runtime.Float{Value: floatSum / float64(count)}, true, nil
		}
		avg := new(big.Rat).Quo(sum, new(big.Rat).SetInt64(count))
		if avg.IsInt() {
			return runtime.Int{Value: new(big.Int).Set(avg.Num())}, true, nil
		}
		return runtime.Decimal{Value: avg}, true, nil
	case "topK":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.topK expects one argument (count)")
		}
		nVal, nOk := native.AsInt64(args[0])
		if !nOk {
			return nil, true, fmt.Errorf("list.topK: count must be an integer")
		}
		n := int(nVal)
		newElements := make([]runtime.Value, len(list.Elements))
		copy(newElements, list.Elements)
		var sortErr error
		sort.SliceStable(newElements, func(i, j int) bool {
			if sortErr != nil {
				return false
			}
			cmp, err := valuesCompare(newElements[i], newElements[j])
			if err != nil {
				sortErr = err
				return false
			}
			return cmp > 0
		})
		if sortErr != nil {
			return nil, true, sortErr
		}
		if n < 0 {
			n = 0
		}
		if n > len(newElements) {
			n = len(newElements)
		}
		return &runtime.List{Elements: newElements[:n]}, true, nil
	case "bottomK":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.bottomK expects one argument (count)")
		}
		nVal, nOk := native.AsInt64(args[0])
		if !nOk {
			return nil, true, fmt.Errorf("list.bottomK: count must be an integer")
		}
		n := int(nVal)
		newElements := make([]runtime.Value, len(list.Elements))
		copy(newElements, list.Elements)
		var sortErr error
		sort.SliceStable(newElements, func(i, j int) bool {
			if sortErr != nil {
				return false
			}
			cmp, err := valuesCompare(newElements[i], newElements[j])
			if err != nil {
				sortErr = err
				return false
			}
			return cmp < 0
		})
		if sortErr != nil {
			return nil, true, sortErr
		}
		if n < 0 {
			n = 0
		}
		if n > len(newElements) {
			n = len(newElements)
		}
		return &runtime.List{Elements: newElements[:n]}, true, nil
	case "frequencies":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.frequencies expects no arguments")
		}
		type countEntry struct {
			value runtime.Value
			count int
		}
		seen := map[string]int{}
		var counts []countEntry
		for _, el := range list.Elements {
			k := el.Inspect()
			if idx, ok2 := seen[k]; ok2 {
				counts[idx].count++
			} else {
				seen[k] = len(counts)
				counts = append(counts, countEntry{el, 1})
			}
		}
		entries := map[string]runtime.DictEntry{}
		for _, c := range counts {
			entries[native.DictKey(c.value)] = runtime.DictEntry{Key: c.value, Value: runtime.NewInt64(int64(c.count))}
		}
		return runtime.Dict{Entries: entries}, true, nil
	case "mode":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("list.mode expects no arguments")
		}
		if len(list.Elements) == 0 {
			return runtime.Null{}, true, nil
		}
		type countEntry struct {
			value runtime.Value
			count int
		}
		seen := map[string]int{}
		var counts []countEntry
		for _, el := range list.Elements {
			k := el.Inspect()
			if idx, ok2 := seen[k]; ok2 {
				counts[idx].count++
			} else {
				seen[k] = len(counts)
				counts = append(counts, countEntry{el, 1})
			}
		}
		best := counts[0]
		for _, c := range counts[1:] {
			if c.count > best.count {
				best = c
			}
		}
		return best.value, true, nil
	case "difference":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.difference expects one argument (list)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.difference: second argument must be a list")
		}
		exclude := map[string]bool{}
		for _, el := range other.Elements {
			exclude[el.Inspect()] = true
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			if !exclude[el.Inspect()] {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "intersection":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("list.intersection expects one argument (list)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.intersection: second argument must be a list")
		}
		include := map[string]bool{}
		for _, el := range other.Elements {
			include[el.Inspect()] = true
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			if include[el.Inspect()] {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "differenceBy":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.differenceBy expects two arguments (list, function)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.differenceBy: second argument must be a list")
		}
		fn := args[1]
		exclude := map[string]bool{}
		for _, el := range other.Elements {
			key, err := vm.callCallable(fn, []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			exclude[key.Inspect()] = true
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			key, err := vm.callCallable(fn, []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if !exclude[key.Inspect()] {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "intersectionBy":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.intersectionBy expects two arguments (list, function)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.intersectionBy: second argument must be a list")
		}
		fn := args[1]
		include := map[string]bool{}
		for _, el := range other.Elements {
			key, err := vm.callCallable(fn, []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			include[key.Inspect()] = true
		}
		var result []runtime.Value
		for _, el := range list.Elements {
			key, err := vm.callCallable(fn, []runtime.Value{el})
			if err != nil {
				return nil, true, err
			}
			if include[key.Inspect()] {
				result = append(result, el)
			}
		}
		return &runtime.List{Elements: result}, true, nil
	case "zipWith":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("list.zipWith expects two arguments (list, function)")
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, fmt.Errorf("list.zipWith: second argument must be a list")
		}
		fn := args[1]
		n := len(list.Elements)
		if len(other.Elements) < n {
			n = len(other.Elements)
		}
		result := make([]runtime.Value, n)
		for i := 0; i < n; i++ {
			combined, err := vm.callCallable(fn, []runtime.Value{list.Elements[i], other.Elements[i]})
			if err != nil {
				return nil, true, err
			}
			result[i] = combined
		}
		return &runtime.List{Elements: result}, true, nil
	}
	return nil, false, nil
}

func (vm *VM) stringSearchMethod(s runtime.String, name string, args []runtime.Value) (bool, runtime.Value, error) {
	switch name {
	case "search":
		if len(args) != 1 {
			return true, nil, fmt.Errorf("string.search expects one argument")
		}
		matches := []runtime.Value{}
		if runtime.IsCallableValue(args[0]) {
			for i, r := range []rune(s.Value) {
				keep, err := vm.callCallable(args[0], []runtime.Value{runtime.String{Value: string(r)}})
				if err != nil {
					return true, nil, err
				}
				if b, ok := keep.(runtime.Bool); ok && b.Value {
					matches = append(matches, runtime.NewInt64(int64(i)))
				}
			}
			return true, &runtime.List{Elements: matches}, nil
		}
		sub, ok := args[0].(runtime.String)
		if !ok {
			return true, nil, fmt.Errorf("string.search expects a string or callable")
		}
		for _, pos := range native.StringMatchRunePositions(s.Value, sub.Value) {
			matches = append(matches, runtime.NewInt64(int64(pos)))
		}
		return true, &runtime.List{Elements: matches}, nil
	case "searchPattern":
		if len(args) != 1 {
			return true, nil, fmt.Errorf("string.searchPattern expects one argument (regex)")
		}
		pat, ok := args[0].(runtime.String)
		if !ok {
			return true, nil, fmt.Errorf("string.searchPattern expects a string pattern")
		}
		re, err := native.CompileSearchRegex(pat.Value)
		if err != nil {
			return true, nil, vmTypedError{class: "ValueError", message: fmt.Sprintf("invalid regex: %v", err)}
		}
		matches := []runtime.Value{}
		for _, pos := range native.RegexMatchRunePositions(re, s.Value) {
			matches = append(matches, runtime.NewInt64(int64(pos)))
		}
		return true, &runtime.List{Elements: matches}, nil
	}
	return false, nil, nil
}

func (vm *VM) dictCollectionsMethod(graph runtime.Dict, name string, args []runtime.Value) (runtime.Value, bool, error) {
	switch name {
	case "search":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("dict.search expects one argument")
		}
		matches := []runtime.Value{}
		var cbErr error
		if runtime.IsCallableValue(args[0]) {
			graph.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				keep, err := vm.callCallable(args[0], []runtime.Value{entry.Value})
				if err != nil {
					cbErr = err
					return false
				}
				if b, ok := keep.(runtime.Bool); ok && b.Value {
					matches = append(matches, entry.Key)
				}
				return true
			})
		} else {
			graph.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				if valuesEqual(entry.Value, args[0]) {
					matches = append(matches, entry.Key)
				}
				return true
			})
		}
		if cbErr != nil {
			return nil, true, cbErr
		}
		return &runtime.List{Elements: matches}, true, nil
	case "searchPattern":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("dict.searchPattern expects one argument (regex)")
		}
		pat, ok := args[0].(runtime.String)
		if !ok {
			return nil, true, fmt.Errorf("dict.searchPattern expects a string pattern")
		}
		re, err := native.CompileSearchRegex(pat.Value)
		if err != nil {
			return nil, true, vmTypedError{class: "ValueError", message: fmt.Sprintf("invalid regex: %v", err)}
		}
		matches := []runtime.Value{}
		graph.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			if s, ok := entry.Value.(runtime.String); ok && re.MatchString(s.Value) {
				matches = append(matches, entry.Key)
			}
			return true
		})
		return &runtime.List{Elements: matches}, true, nil
	case "bfs":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("collections.bfs expects (graph, start)")
		}
		start := args[0]
		seen := map[string]bool{native.DictKey(start): true}
		queue := []runtime.Value{start}
		visited := []runtime.Value{}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			visited = append(visited, node)
			if entry, ok := graph.GetEntry(native.DictKey(node)); ok {
				if neighbors, ok := entry.Value.(*runtime.List); ok {
					for _, nb := range neighbors.Elements {
						k := native.DictKey(nb)
						if !seen[k] {
							seen[k] = true
							queue = append(queue, nb)
						}
					}
				}
			}
		}
		return &runtime.List{Elements: visited}, true, nil
	case "dfs":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("collections.dfs expects (graph, start)")
		}
		start := args[0]
		seen := map[string]bool{}
		stack := []runtime.Value{start}
		visited := []runtime.Value{}
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			k := native.DictKey(node)
			if seen[k] {
				continue
			}
			seen[k] = true
			visited = append(visited, node)
			if entry, ok := graph.GetEntry(native.DictKey(node)); ok {
				if neighbors, ok := entry.Value.(*runtime.List); ok {
					for i := len(neighbors.Elements) - 1; i >= 0; i-- {
						nb := neighbors.Elements[i]
						if !seen[native.DictKey(nb)] {
							stack = append(stack, nb)
						}
					}
				}
			}
		}
		return &runtime.List{Elements: visited}, true, nil
	case "topologicalSort":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("collections.topologicalSort expects (graph)")
		}
		allNodes := map[string]runtime.Value{}
		inDegree := map[string]int{}
		graph.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			k := native.DictKey(entry.Key)
			allNodes[k] = entry.Key
			if _, ok := inDegree[k]; !ok {
				inDegree[k] = 0
			}
			if neighbors, ok := entry.Value.(*runtime.List); ok {
				for _, nb := range neighbors.Elements {
					nbKey := native.DictKey(nb)
					if _, exists := allNodes[nbKey]; !exists {
						allNodes[nbKey] = nb
					}
					inDegree[nbKey]++
				}
			}
			return true
		})
		zeroKeys := make([]string, 0)
		for k, deg := range inDegree {
			if deg == 0 {
				zeroKeys = append(zeroKeys, k)
			}
		}
		sort.Strings(zeroKeys)
		queue := make([]runtime.Value, 0, len(zeroKeys))
		for _, k := range zeroKeys {
			queue = append(queue, allNodes[k])
		}
		result := []runtime.Value{}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			result = append(result, node)
			if entry, ok := graph.GetEntry(native.DictKey(node)); ok {
				if neighbors, ok := entry.Value.(*runtime.List); ok {
					for _, nb := range neighbors.Elements {
						nbKey := native.DictKey(nb)
						inDegree[nbKey]--
						if inDegree[nbKey] == 0 {
							queue = append(queue, nb)
						}
					}
				}
			}
		}
		if len(result) != len(allNodes) {
			return nil, true, fmt.Errorf("collections.topologicalSort: cycle detected")
		}
		return &runtime.List{Elements: result}, true, nil
	case "shortestPath":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("collections.shortestPath expects (graph, start, end)")
		}
		start, end := args[0], args[1]
		endKey := native.DictKey(end)
		parent := map[string]runtime.Value{}
		seen := map[string]bool{native.DictKey(start): true}
		queue := []runtime.Value{start}
		found := false
		for len(queue) > 0 && !found {
			node := queue[0]
			queue = queue[1:]
			if native.DictKey(node) == endKey {
				found = true
				break
			}
			if entry, ok := graph.GetEntry(native.DictKey(node)); ok {
				if neighbors, ok := entry.Value.(*runtime.List); ok {
					for _, nb := range neighbors.Elements {
						k := native.DictKey(nb)
						if !seen[k] {
							seen[k] = true
							parent[k] = node
							queue = append(queue, nb)
						}
					}
				}
			}
		}
		if !found {
			return runtime.Null{}, true, nil
		}
		path := []runtime.Value{end}
		cur := end
		for native.DictKey(cur) != native.DictKey(start) {
			p, ok := parent[native.DictKey(cur)]
			if !ok {
				return runtime.Null{}, true, nil
			}
			path = append([]runtime.Value{p}, path...)
			cur = p
		}
		return &runtime.List{Elements: path}, true, nil
	}
	return nil, false, nil
}
