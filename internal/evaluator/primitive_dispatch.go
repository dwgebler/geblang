package evaluator

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode"

	"geblang/internal/native"
	"geblang/internal/runtime"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// evalMethodCall dispatches the primitive methods (list/dict/set/string/
// bytes/range and friends) for the evaluator backend.
func (e *Evaluator) evalMethodCall(receiver runtime.Value, name string, args []runtime.Value) (runtime.Value, error) {
	// currentLine here is the method call site; a primitive HOF invokes its callback via callValue, which stamps this line so the caller frame's trace line stays the HOF call site rather than a previous callback's residue.
	savedHofLine := e.hofCallLine
	e.hofCallLine = e.currentLine
	defer func() { e.hofCallLine = savedHofLine }()
	if class, ok := receiver.(*runtime.Class); ok {
		methods := lookupStaticMethodOverloads(class, name)
		if len(methods) > 0 {
			method, err := selectOverload(class.Name+"."+name, methods, args)
			if err != nil {
				return nil, err
			}
			return e.applyFunction(method, args)
		}
		if method, ok := lookupStaticMethod(class, "__callStatic"); ok {
			return e.applyFunction(method, []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}})
		}
		return nil, fmt.Errorf("unknown static method %s.%s", class.Name, name)
	}
	if instance, ok := receiver.(*runtime.Instance); ok {
		methods := lookupMethodOverloads(instance.Class, name)
		if len(methods) > 0 {
			method, err := selectOverload(instance.Class.Name+"."+name, methods, args)
			if err != nil {
				return nil, err
			}
			return e.applyFunctionWithThis(method, args, instance)
		}
		if method, ok := lookupMethod(instance.Class, "__call"); ok {
			return e.applyFunctionWithThis(method, []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}}, instance)
		}
		return nil, native.UnknownMethodError(instance.Class.Name, name)
	}
	if variant, ok := receiver.(runtime.EnumVariant); ok {
		if method, ok := e.lookupEnumMethod(variant.Enum, name); ok {
			return e.applyEnumMethod(method, args, variant)
		}
		return nil, native.UnknownMethodError(variant.Enum.Name, name)
	}
	if target, ok := primitiveConversionTarget(name); ok {
		if target == "int" {
			if text, ok := receiver.(runtime.String); ok && len(args) >= 1 {
				base, err := native.IntBaseArg(args, "string.toInt")
				if err != nil {
					return nil, err
				}
				return native.StringParseBase(text.Value, base, "string.toInt")
			}
		}
		if target == "decimal" && len(args) >= 1 {
			places, err := native.RoundPlacesArg(args, "toDecimal")
			if err != nil {
				return nil, err
			}
			d, err := castValue(receiver, "decimal")
			if err != nil {
				return nil, err
			}
			return native.DecimalQuantize(d.(runtime.Decimal), places, native.RoundHalfAwayZero), nil
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.%s expects no arguments", receiver.TypeName(), name)
		}
		return castValue(receiver, target)
	}
	switch value := receiver.(type) {
	case runtime.Dict:
		switch name {
		case "copy":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.copy expects no arguments")
			}
			copied := runtime.NewDict()
			value.ForEachEntry(func(k string, e runtime.DictEntry) bool {
				copied.PutEntry(k, e)
				return true
			})
			return copied, nil
		case "deepCopy":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.deepCopy expects no arguments")
			}
			return runtime.CloneValue(value), nil
		case "keys":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.keys expects no arguments")
			}
			keys := make([]runtime.Value, 0, value.Len())
			value.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
				keys = append(keys, e.Key)
				return true
			})
			return &runtime.List{Elements: keys}, nil
		case "values":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.values expects no arguments")
			}
			values := make([]runtime.Value, 0, value.Len())
			value.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
				values = append(values, e.Value)
				return true
			})
			return &runtime.List{Elements: values}, nil
		case "search":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.search expects one argument")
			}
			matches := []runtime.Value{}
			var cbErr error
			if runtime.IsCallableValue(args[0]) {
				value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
					keep, err := e.callValue(args[0], []runtime.Value{entry.Value})
					if err != nil {
						cbErr = err
						return false
					}
					if isTruthy(keep) {
						matches = append(matches, entry.Key)
					}
					return true
				})
			} else {
				value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
					eq, err := e.valuesEqual(entry.Value, args[0])
					if err != nil {
						cbErr = err
						return false
					}
					if eq {
						matches = append(matches, entry.Key)
					}
					return true
				})
			}
			if cbErr != nil {
				return nil, cbErr
			}
			return &runtime.List{Elements: matches}, nil
		case "searchPattern":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.searchPattern expects one argument (regex)")
			}
			pat, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("dict.searchPattern expects a string pattern")
			}
			re, err := native.CompileSearchRegex(pat.Value)
			if err != nil {
				return nil, thrownError{value: runtime.Error{Class: "ValueError", Message: fmt.Sprintf("invalid regex: %v", err)}}
			}
			matches := []runtime.Value{}
			value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				if s, ok := entry.Value.(runtime.String); ok && re.MatchString(s.Value) {
					matches = append(matches, entry.Key)
				}
				return true
			})
			return &runtime.List{Elements: matches}, nil
		case "items", "entries":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.%s expects no arguments", name)
			}
			items := make([]runtime.Value, 0, value.Len())
			value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				items = append(items, &runtime.List{Elements: []runtime.Value{entry.Key, entry.Value}})
				return true
			})
			return &runtime.List{Elements: items}, nil
		case "get":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.get expects one argument")
			}
			entry, ok := value.GetEntry(dictKey(args[0]))
			if !ok {
				return runtime.Null{}, nil
			}
			return entry.Value, nil
		case "set", "insert":
			if len(args) != 2 {
				return nil, fmt.Errorf("dict.%s expects two arguments", name)
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen dict"}}
			}
			if err := checkDictWriteTags(value, args[0], args[1]); err != nil {
				return nil, err
			}
			value.PutEntry(dictKey(args[0]), runtime.DictEntry{Key: args[0], Value: args[1]})
			return runtime.Null{}, nil
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.length expects no arguments")
			}
			return runtime.SmallInt{Value: int64(value.Len())}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: value.Len() == 0}, nil
		case "hasKey":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.hasKey expects one argument")
			}
			_, ok := value.GetEntry(dictKey(args[0]))
			return runtime.Bool{Value: ok}, nil
		case "delete", "remove":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.%s expects one argument", name)
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen dict"}}
			}
			value.DelEntry(dictKey(args[0]))
			return runtime.Null{}, nil
		case "clear":
			if len(args) != 0 {
				return nil, fmt.Errorf("dict.clear expects no arguments")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen dict"}}
			}
			value.Clear()
			return runtime.Null{}, nil
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.contains expects one argument")
			}
			_, ok := value.GetEntry(dictKey(args[0]))
			return runtime.Bool{Value: ok}, nil
		case "bfs":
			if len(args) != 1 {
				return nil, fmt.Errorf("collections.bfs expects (graph, start)")
			}
			start := args[0]
			seen := map[string]bool{dictKey(start): true}
			queue := []runtime.Value{start}
			visited := []runtime.Value{}
			for len(queue) > 0 {
				node := queue[0]
				queue = queue[1:]
				visited = append(visited, node)
				if entry, ok := value.GetEntry(dictKey(node)); ok {
					if neighbors, ok := entry.Value.(*runtime.List); ok {
						for _, nb := range neighbors.Elements {
							k := dictKey(nb)
							if !seen[k] {
								seen[k] = true
								queue = append(queue, nb)
							}
						}
					}
				}
			}
			return &runtime.List{Elements: visited}, nil
		case "dfs":
			if len(args) != 1 {
				return nil, fmt.Errorf("collections.dfs expects (graph, start)")
			}
			start := args[0]
			seen := map[string]bool{}
			stack := []runtime.Value{start}
			visited := []runtime.Value{}
			for len(stack) > 0 {
				node := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				k := dictKey(node)
				if seen[k] {
					continue
				}
				seen[k] = true
				visited = append(visited, node)
				if entry, ok := value.GetEntry(dictKey(node)); ok {
					if neighbors, ok := entry.Value.(*runtime.List); ok {
						for i := len(neighbors.Elements) - 1; i >= 0; i-- {
							nb := neighbors.Elements[i]
							if !seen[dictKey(nb)] {
								stack = append(stack, nb)
							}
						}
					}
				}
			}
			return &runtime.List{Elements: visited}, nil
		case "topologicalSort":
			if len(args) != 0 {
				return nil, fmt.Errorf("collections.topologicalSort expects (graph)")
			}
			allNodes := map[string]runtime.Value{}
			inDegree := map[string]int{}
			value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
				k := dictKey(entry.Key)
				allNodes[k] = entry.Key
				if _, ok := inDegree[k]; !ok {
					inDegree[k] = 0
				}
				if neighbors, ok := entry.Value.(*runtime.List); ok {
					for _, nb := range neighbors.Elements {
						nbKey := dictKey(nb)
						if _, exists := allNodes[nbKey]; !exists {
							allNodes[nbKey] = nb
						}
						inDegree[nbKey]++
					}
				}
				return true
			})
			// Build sorted initial queue for deterministic output.
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
				if entry, ok := value.GetEntry(dictKey(node)); ok {
					if neighbors, ok := entry.Value.(*runtime.List); ok {
						for _, nb := range neighbors.Elements {
							nbKey := dictKey(nb)
							inDegree[nbKey]--
							if inDegree[nbKey] == 0 {
								queue = append(queue, nb)
							}
						}
					}
				}
			}
			if len(result) != len(allNodes) {
				return nil, fmt.Errorf("collections.topologicalSort: cycle detected")
			}
			return &runtime.List{Elements: result}, nil
		case "shortestPath":
			if len(args) != 2 {
				return nil, fmt.Errorf("collections.shortestPath expects (graph, start, end)")
			}
			start, end := args[0], args[1]
			endKey := dictKey(end)
			parent := map[string]runtime.Value{}
			seen := map[string]bool{dictKey(start): true}
			queue := []runtime.Value{start}
			found := false
			for len(queue) > 0 && !found {
				node := queue[0]
				queue = queue[1:]
				if dictKey(node) == endKey {
					found = true
					break
				}
				if entry, ok := value.GetEntry(dictKey(node)); ok {
					if neighbors, ok := entry.Value.(*runtime.List); ok {
						for _, nb := range neighbors.Elements {
							k := dictKey(nb)
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
				return runtime.Null{}, nil
			}
			path := []runtime.Value{end}
			cur := end
			for dictKey(cur) != dictKey(start) {
				p, ok := parent[dictKey(cur)]
				if !ok {
					return runtime.Null{}, nil
				}
				path = append([]runtime.Value{p}, path...)
				cur = p
			}
			return &runtime.List{Elements: path}, nil
		case "merge":
			if len(args) != 1 {
				return nil, fmt.Errorf("dict.merge expects one argument")
			}
			other, ok := args[0].(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("dict.merge expects a dict argument")
			}
			merged := runtime.NewDictHint(value.Len() + other.Len())
			value.ForEachEntry(func(k string, e runtime.DictEntry) bool { merged.PutEntry(k, e); return true })
			other.ForEachEntry(func(k string, e runtime.DictEntry) bool { merged.PutEntry(k, e); return true })
			return merged, nil
		}
	case runtime.Set:
		switch name {
		case "copy":
			if len(args) != 0 {
				return nil, fmt.Errorf("set.copy expects no arguments")
			}
			return runtime.Set{Elements: cloneSetEntries(value.Elements)}, nil
		case "deepCopy":
			if len(args) != 0 {
				return nil, fmt.Errorf("set.deepCopy expects no arguments")
			}
			return runtime.CloneValue(value), nil
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("set.length expects no arguments")
			}
			return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("set.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: len(value.Elements) == 0}, nil
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.contains expects one argument")
			}
			_, ok := value.Elements[dictKey(args[0])]
			return runtime.Bool{Value: ok}, nil
		case "add":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.add expects one argument")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen set"}}
			}
			if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot add %s to set<%s>", args[0].TypeName(), value.ElementTypes[0])}}
			}
			value.Elements[dictKey(args[0])] = runtime.SetEntry{Value: args[0]}
			return value, nil
		case "remove":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.remove expects one argument")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen set"}}
			}
			delete(value.Elements, dictKey(args[0]))
			return value, nil
		case "toList":
			if len(args) != 0 {
				return nil, fmt.Errorf("set.toList expects no arguments")
			}
			return &runtime.List{Elements: orderedSetValues(value)}, nil
		case "union":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.union expects one argument")
			}
			other, ok := args[0].(runtime.Set)
			if !ok {
				return nil, fmt.Errorf("set.union expects set")
			}
			elements := cloneSetEntries(value.Elements)
			for key, entry := range other.Elements {
				elements[key] = entry
			}
			return runtime.Set{Elements: elements}, nil
		case "intersection":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.intersection expects one argument")
			}
			other, ok := args[0].(runtime.Set)
			if !ok {
				return nil, fmt.Errorf("set.intersection expects set")
			}
			elements := map[string]runtime.SetEntry{}
			for key, entry := range value.Elements {
				if _, exists := other.Elements[key]; exists {
					elements[key] = entry
				}
			}
			return runtime.Set{Elements: elements}, nil
		case "difference":
			if len(args) != 1 {
				return nil, fmt.Errorf("set.difference expects one argument")
			}
			other, ok := args[0].(runtime.Set)
			if !ok {
				return nil, fmt.Errorf("set.difference expects set")
			}
			elements := map[string]runtime.SetEntry{}
			for key, entry := range value.Elements {
				if _, exists := other.Elements[key]; !exists {
					elements[key] = entry
				}
			}
			return runtime.Set{Elements: elements}, nil
		}
	case *runtime.List:
		switch name {
		case "copy":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.copy expects no arguments")
			}
			elems := make([]runtime.Value, len(value.Elements))
			copy(elems, value.Elements)
			return &runtime.List{Elements: elems}, nil
		case "deepCopy":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.deepCopy expects no arguments")
			}
			return runtime.CloneValue(value), nil
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.length expects no arguments")
			}
			return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: len(value.Elements) == 0}, nil
		case "get":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.get expects one argument")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			return listElement(value, i)
		case "set":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.set expects two arguments")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[1], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot assign %s to list<%s>", args[1].TypeName(), value.ElementTypes[0])}}
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Elements) + i
			}
			if i < 0 || i >= len(value.Elements) {
				return nil, fmt.Errorf("list index out of range")
			}
			value.Elements[i] = args[1]
			return runtime.Null{}, nil
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.contains expects one argument")
			}
			for _, el := range value.Elements {
				eq, err := e.valuesEqual(el, args[0])
				if err != nil {
					return nil, err
				}
				if eq {
					return runtime.Bool{Value: true}, nil
				}
			}
			return runtime.Bool{Value: false}, nil
		case "indexOf":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.indexOf expects one argument")
			}
			for i, el := range value.Elements {
				eq, err := e.valuesEqual(el, args[0])
				if err != nil {
					return nil, err
				}
				if eq {
					return runtime.NewInt64(int64(i)), nil
				}
			}
			return runtime.NewInt64(-1), nil
		case "search":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.search expects one argument")
			}
			matches := []runtime.Value{}
			if runtime.IsCallableValue(args[0]) {
				for i, el := range value.Elements {
					keep, err := e.callValue(args[0], []runtime.Value{el})
					if err != nil {
						return nil, err
					}
					if isTruthy(keep) {
						matches = append(matches, runtime.NewInt64(int64(i)))
					}
				}
			} else {
				for i, el := range value.Elements {
					eq, err := e.valuesEqual(el, args[0])
					if err != nil {
						return nil, err
					}
					if eq {
						matches = append(matches, runtime.NewInt64(int64(i)))
					}
				}
			}
			return &runtime.List{Elements: matches}, nil
		case "searchPattern":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.searchPattern expects one argument (regex)")
			}
			pat, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("list.searchPattern expects a string pattern")
			}
			re, err := native.CompileSearchRegex(pat.Value)
			if err != nil {
				return nil, thrownError{value: runtime.Error{Class: "ValueError", Message: fmt.Sprintf("invalid regex: %v", err)}}
			}
			matches := []runtime.Value{}
			for i, el := range value.Elements {
				if s, ok := el.(runtime.String); ok && re.MatchString(s.Value) {
					matches = append(matches, runtime.NewInt64(int64(i)))
				}
			}
			return &runtime.List{Elements: matches}, nil
		case "slice":
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("list.slice expects (start[, end])")
			}
			n := len(value.Elements)
			start, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("list.slice: %v", err)
			}
			if start < 0 {
				start = n + start
			}
			if start < 0 {
				start = 0
			}
			if start > n {
				start = n
			}
			end := n
			if len(args) == 2 {
				end, err = indexInt(args[1])
				if err != nil {
					return nil, fmt.Errorf("list.slice: %v", err)
				}
				if end < 0 {
					end = n + end
				}
				if end < 0 {
					end = 0
				}
				if end > n {
					end = n
				}
			}
			if start >= end {
				return &runtime.List{Elements: nil}, nil
			}
			return &runtime.List{Elements: value.Elements[start:end]}, nil
		case "join":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.join expects one argument (separator)")
			}
			sep, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("list.join separator must be a string")
			}
			parts := make([]string, len(value.Elements))
			for i, el := range value.Elements {
				parts[i] = el.Inspect()
			}
			return runtime.String{Value: strings.Join(parts, sep.Value)}, nil
		case "first":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.first expects no arguments")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			return value.Elements[0], nil
		case "last":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.last expects no arguments")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			return value.Elements[len(value.Elements)-1], nil
		case "push":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.push expects one argument")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot push %s to list<%s>", args[0].TypeName(), value.ElementTypes[0])}}
			}
			value.Elements = append(value.Elements, args[0])
			return value, nil
		case "fill":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.fill expects two arguments (value, count)")
			}
			count, err := indexInt(args[1])
			if err != nil {
				return nil, err
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if count < 0 {
				return nil, thrownError{value: runtime.Error{Class: "ValueError", Message: "list.fill count must be >= 0"}}
			}
			if count > 0 && len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot fill list<%s> with %s", value.ElementTypes[0], args[0].TypeName())}}
			}
			for i := 0; i < count; i++ {
				value.Elements = append(value.Elements, args[0])
			}
			return value, nil
		case "append":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.append expects one argument")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 {
				if !valueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
					return nil, thrownError{value: runtime.Error{
						Class:   "TypeError",
						Message: fmt.Sprintf("cannot append %s to list<%s>", args[0].TypeName(), value.ElementTypes[0]),
					}}
				}
			}
			value.Elements = append(value.Elements, args[0])
			return runtime.Null{}, nil
		case "extend":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.extend expects one argument")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.extend expects a list argument, got %s", args[0].TypeName())
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 {
				for i, el := range other.Elements {
					if !valueSatisfiesElementTag(el, value.ElementTypes[0]) {
						return nil, thrownError{value: runtime.Error{
							Class:   "TypeError",
							Message: fmt.Sprintf("cannot extend list<%s> with %s at index %d", value.ElementTypes[0], el.TypeName(), i),
						}}
					}
				}
			}
			value.Elements = append(value.Elements, other.Elements...)
			return runtime.Null{}, nil
		case "clear":
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			value.Elements = value.Elements[:0]
			return runtime.Null{}, nil
		case "toList":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.toList expects no arguments")
			}
			return value, nil
		case "pop":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.pop expects no arguments")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.Elements) > 0 {
				value.Elements = value.Elements[:len(value.Elements)-1]
			}
			return value, nil
		case "insert":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.insert expects two arguments (index, item)")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Elements) + i
			}
			if i < 0 {
				i = 0
			}
			if i > len(value.Elements) {
				i = len(value.Elements)
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[1], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot insert %s to list<%s>", args[1].TypeName(), value.ElementTypes[0])}}
			}
			value.Elements = append(value.Elements, nil)
			copy(value.Elements[i+1:], value.Elements[i:])
			value.Elements[i] = args[1]
			return value, nil
		case "removeAt":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.removeAt expects one argument")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Elements) + i
			}
			if i < 0 || i >= len(value.Elements) {
				return nil, fmt.Errorf("list index out of range")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			value.Elements = append(value.Elements[:i], value.Elements[i+1:]...)
			return value, nil
		case "concat":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.concat expects one argument")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.concat expects list argument")
			}
			newElements := make([]runtime.Value, len(value.Elements)+len(other.Elements))
			copy(newElements, value.Elements)
			copy(newElements[len(value.Elements):], other.Elements)
			return &runtime.List{Elements: newElements}, nil
		case "map":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.map expects one argument (function)")
			}
			result := make([]runtime.Value, len(value.Elements))
			for i, el := range value.Elements {
				mapped, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				result[i] = mapped
			}
			return &runtime.List{Elements: result}, nil
		case "filter":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.filter expects one argument (function)")
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				keep, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(keep) {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "reduce":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.reduce expects two arguments (function, initial)")
			}
			acc := args[1]
			for _, el := range value.Elements {
				next, err := e.callValue(args[0], []runtime.Value{acc, el})
				if err != nil {
					return nil, err
				}
				acc = next
			}
			return acc, nil
		case "find":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.find expects one argument (function)")
			}
			for _, el := range value.Elements {
				match, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(match) {
					return el, nil
				}
			}
			return runtime.Null{}, nil
		case "any":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.any expects one argument (function)")
			}
			for _, el := range value.Elements {
				result, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(result) {
					return runtime.Bool{Value: true}, nil
				}
			}
			return runtime.Bool{Value: false}, nil
		case "all":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.all expects one argument (function)")
			}
			for _, el := range value.Elements {
				result, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if !isTruthy(result) {
					return runtime.Bool{Value: false}, nil
				}
			}
			return runtime.Bool{Value: true}, nil
		case "count":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.count expects one argument (function)")
			}
			n := 0
			for _, el := range value.Elements {
				result, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(result) {
					n++
				}
			}
			return runtime.NewInt64(int64(n)), nil
		case "sorted", "sort":
			if len(args) > 1 {
				return nil, fmt.Errorf("list.%s expects zero or one argument", name)
			}
			if name == "sort" {
				if value.Frozen {
					return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
				}
				if sortErr := e.sortElements(value.Elements, args); sortErr != nil {
					return nil, sortErr
				}
				return value, nil
			}
			newElements := make([]runtime.Value, len(value.Elements))
			copy(newElements, value.Elements)
			if sortErr := e.sortElements(newElements, args); sortErr != nil {
				return nil, sortErr
			}
			return &runtime.List{Elements: newElements}, nil
		case "reverse", "reversed":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.%s expects no arguments", name)
			}
			if name == "reverse" {
				if value.Frozen {
					return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
				}
				for i, j := 0, len(value.Elements)-1; i < j; i, j = i+1, j-1 {
					value.Elements[i], value.Elements[j] = value.Elements[j], value.Elements[i]
				}
				return value, nil
			}
			newElements := make([]runtime.Value, len(value.Elements))
			for i, el := range value.Elements {
				newElements[len(value.Elements)-1-i] = el
			}
			return &runtime.List{Elements: newElements, ElementTypes: value.ElementTypes}, nil
		case "prepend", "unshift":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.%s expects one argument", name)
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
				return nil, thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot %s %s to list<%s>", name, args[0].TypeName(), value.ElementTypes[0])}}
			}
			value.Elements = append(value.Elements, nil)
			copy(value.Elements[1:], value.Elements)
			value.Elements[0] = args[0]
			return value, nil
		case "remove":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.remove expects one argument")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			for i, el := range value.Elements {
				eq, err := e.valuesEqual(el, args[0])
				if err != nil {
					return nil, err
				}
				if eq {
					value.Elements = append(value.Elements[:i], value.Elements[i+1:]...)
					break
				}
			}
			return value, nil
		case "flatten":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.flatten expects no arguments")
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				if nested, ok := el.(*runtime.List); ok {
					result = append(result, nested.Elements...)
				} else {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "unique":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.unique expects no arguments")
			}
			seen := make([]runtime.Value, 0, len(value.Elements))
			var result []runtime.Value
			for _, el := range value.Elements {
				found := false
				for _, s := range seen {
					eq, err := e.valuesEqual(el, s)
					if err != nil {
						return nil, err
					}
					if eq {
						found = true
						break
					}
				}
				if !found {
					seen = append(seen, el)
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "zip":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.zip expects one argument (list)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.zip expects list argument")
			}
			n := len(value.Elements)
			if len(other.Elements) < n {
				n = len(other.Elements)
			}
			result := make([]runtime.Value, n)
			for i := 0; i < n; i++ {
				result[i] = &runtime.List{Elements: []runtime.Value{value.Elements[i], other.Elements[i]}}
			}
			return &runtime.List{Elements: result}, nil
		case "groupBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.groupBy expects one argument (function)")
			}
			entries := map[string]runtime.DictEntry{}
			for _, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				dk := native.DictKey(key)
				existing, ok := entries[dk]
				if !ok {
					existing = runtime.DictEntry{Key: key, Value: &runtime.List{}}
				}
				existing.Value = &runtime.List{Elements: append(existing.Value.(*runtime.List).Elements, el)}
				entries[dk] = existing
			}
			return runtime.Dict{Entries: entries}, nil
		case "chunk":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.chunk expects one argument (size)")
			}
			n, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if n <= 0 {
				return nil, fmt.Errorf("list.chunk size must be positive")
			}
			var chunks []runtime.Value
			for i := 0; i < len(value.Elements); i += n {
				end := i + n
				if end > len(value.Elements) {
					end = len(value.Elements)
				}
				chunks = append(chunks, &runtime.List{Elements: append([]runtime.Value(nil), value.Elements[i:end]...)})
			}
			return &runtime.List{Elements: chunks}, nil
		case "partition":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.partition expects one argument (function)")
			}
			var yes, no []runtime.Value
			for _, el := range value.Elements {
				result, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(result) {
					yes = append(yes, el)
				} else {
					no = append(no, el)
				}
			}
			return &runtime.List{Elements: []runtime.Value{
				&runtime.List{Elements: yes},
				&runtime.List{Elements: no},
			}}, nil
		case "enumerate":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.enumerate expects no arguments")
			}
			result := make([]runtime.Value, len(value.Elements))
			for i, el := range value.Elements {
				result[i] = &runtime.List{Elements: []runtime.Value{runtime.NewInt64(int64(i)), el}}
			}
			return &runtime.List{Elements: result}, nil
		case "flatMap":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.flatMap expects one argument (function)")
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				mapped, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				nested, ok := mapped.(*runtime.List)
				if !ok {
					return nil, fmt.Errorf("list.flatMap function must return a list")
				}
				result = append(result, nested.Elements...)
			}
			return &runtime.List{Elements: result}, nil
		case "uniqueBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.uniqueBy expects one argument (function)")
			}
			seenKeys := make([]runtime.Value, 0, len(value.Elements))
			var result []runtime.Value
			for _, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				found := false
				for _, s := range seenKeys {
					eq, err := e.valuesEqual(key, s)
					if err != nil {
						return nil, err
					}
					if eq {
						found = true
						break
					}
				}
				if !found {
					seenKeys = append(seenKeys, key)
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "takeWhile":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.takeWhile expects one argument (function)")
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				keep, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if !isTruthy(keep) {
					break
				}
				result = append(result, el)
			}
			return &runtime.List{Elements: result}, nil
		case "dropWhile":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.dropWhile expects one argument (function)")
			}
			dropping := true
			var result []runtime.Value
			for _, el := range value.Elements {
				if dropping {
					keep, err := e.callValue(args[0], []runtime.Value{el})
					if err != nil {
						return nil, err
					}
					if isTruthy(keep) {
						continue
					}
					dropping = false
				}
				result = append(result, el)
			}
			return &runtime.List{Elements: result}, nil
		case "windowed":
			if len(args) != 1 && len(args) != 2 {
				return nil, fmt.Errorf("list.windowed expects size and optional step")
			}
			size, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			step := 1
			if len(args) == 2 {
				step, err = indexInt(args[1])
				if err != nil {
					return nil, err
				}
			}
			if size <= 0 || step <= 0 {
				return nil, fmt.Errorf("list.windowed size and step must be positive")
			}
			var windows []runtime.Value
			for i := 0; i+size <= len(value.Elements); i += step {
				windows = append(windows, &runtime.List{Elements: append([]runtime.Value(nil), value.Elements[i:i+size]...)})
			}
			return &runtime.List{Elements: windows}, nil
		case "unzip":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.unzip expects no arguments")
			}
			firsts := make([]runtime.Value, 0, len(value.Elements))
			seconds := make([]runtime.Value, 0, len(value.Elements))
			for _, el := range value.Elements {
				pair, ok := el.(*runtime.List)
				if !ok || len(pair.Elements) != 2 {
					return nil, fmt.Errorf("list.unzip expects a list of 2-element lists")
				}
				firsts = append(firsts, pair.Elements[0])
				seconds = append(seconds, pair.Elements[1])
			}
			return &runtime.List{Elements: []runtime.Value{
				&runtime.List{Elements: firsts},
				&runtime.List{Elements: seconds},
			}}, nil
		case "scan":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.scan expects two arguments (initial, function)")
			}
			acc := args[0]
			result := []runtime.Value{acc}
			for _, el := range value.Elements {
				next, err := e.callValue(args[1], []runtime.Value{acc, el})
				if err != nil {
					return nil, err
				}
				acc = next
				result = append(result, acc)
			}
			return &runtime.List{Elements: result}, nil
		case "findLast":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.findLast expects one argument (function)")
			}
			for i := len(value.Elements) - 1; i >= 0; i-- {
				result, err := e.callValue(args[0], []runtime.Value{value.Elements[i]})
				if err != nil {
					return nil, err
				}
				if isTruthy(result) {
					return value.Elements[i], nil
				}
			}
			return runtime.Null{}, nil
		case "containsBy":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.containsBy expects two arguments (value, function)")
			}
			target, fn := args[0], args[1]
			for _, el := range value.Elements {
				key, err := e.callValue(fn, []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				eq, err := e.valuesEqual(key, target)
				if err != nil {
					return nil, err
				}
				if eq {
					return runtime.Bool{Value: true}, nil
				}
			}
			return runtime.Bool{Value: false}, nil
		case "indexBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.indexBy expects one argument (function)")
			}
			for i, el := range value.Elements {
				result, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if isTruthy(result) {
					return runtime.NewInt64(int64(i)), nil
				}
			}
			return runtime.NewInt64(-1), nil
		case "binarySearch":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.binarySearch expects one argument (value)")
			}
			target := args[0]
			lo, hi := 0, len(value.Elements)
			for lo < hi {
				mid := (lo + hi) / 2
				cmp, err := compareValues(value.Elements[mid], target)
				if err != nil {
					return nil, err
				}
				if cmp == 0 {
					return runtime.NewInt64(int64(mid)), nil
				} else if cmp < 0 {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			return runtime.NewInt64(-1), nil
		case "binarySearchBy":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.binarySearchBy expects a selector and a target key")
			}
			target := args[1]
			lo, hi := 0, len(value.Elements)
			for lo < hi {
				mid := (lo + hi) / 2
				key, err := e.callValue(args[0], []runtime.Value{value.Elements[mid]})
				if err != nil {
					return nil, err
				}
				cmp, err := compareValues(key, target)
				if err != nil {
					return nil, err
				}
				if cmp == 0 {
					return runtime.NewInt64(int64(mid)), nil
				} else if cmp < 0 {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			return runtime.NewInt64(-1), nil
		case "lowerBound":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.lowerBound expects one argument (value)")
			}
			target := args[0]
			lo, hi := 0, len(value.Elements)
			for lo < hi {
				mid := (lo + hi) / 2
				cmp, err := compareValues(value.Elements[mid], target)
				if err != nil {
					return nil, err
				}
				if cmp < 0 {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			return runtime.NewInt64(int64(lo)), nil
		case "upperBound":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.upperBound expects one argument (value)")
			}
			target := args[0]
			lo, hi := 0, len(value.Elements)
			for lo < hi {
				mid := (lo + hi) / 2
				cmp, err := compareValues(value.Elements[mid], target)
				if err != nil {
					return nil, err
				}
				if cmp <= 0 {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			return runtime.NewInt64(int64(lo)), nil
		case "minBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.minBy expects one argument (function)")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			best := value.Elements[0]
			bestKey, err := e.callValue(args[0], []runtime.Value{best})
			if err != nil {
				return nil, err
			}
			for _, el := range value.Elements[1:] {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				cmp, err := compareValues(key, bestKey)
				if err != nil {
					return nil, err
				}
				if cmp < 0 {
					best, bestKey = el, key
				}
			}
			return best, nil
		case "maxBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.maxBy expects one argument (function)")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			best := value.Elements[0]
			bestKey, err := e.callValue(args[0], []runtime.Value{best})
			if err != nil {
				return nil, err
			}
			for _, el := range value.Elements[1:] {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				cmp, err := compareValues(key, bestKey)
				if err != nil {
					return nil, err
				}
				if cmp > 0 {
					best, bestKey = el, key
				}
			}
			return best, nil
		case "sortBy":
			if len(args) != 1 && len(args) != 2 {
				return nil, fmt.Errorf("list.sortBy expects a selector and an optional descending flag")
			}
			if value.Frozen {
				return nil, thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
			}
			descending := false
			if len(args) == 2 {
				b, ok := args[1].(runtime.Bool)
				if !ok {
					return nil, fmt.Errorf("list.sortBy descending flag must be a bool")
				}
				descending = b.Value
			}
			type keyedEl struct {
				key runtime.Value
				el  runtime.Value
			}
			pairs := make([]keyedEl, len(value.Elements))
			for i, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				pairs[i] = keyedEl{key, el}
			}
			var sortErr error
			sort.SliceStable(pairs, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				cmp, err := compareValues(pairs[i].key, pairs[j].key)
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
				return nil, sortErr
			}
			for i, p := range pairs {
				value.Elements[i] = p.el
			}
			return value, nil
		case "topBy":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.topBy expects two arguments (function, count)")
			}
			nInt64, ok := toInt64(args[1])
			if !ok {
				return nil, fmt.Errorf("list.topBy: count must be an integer")
			}
			n := int(nInt64)
			type keyedEl struct {
				key runtime.Value
				el  runtime.Value
			}
			pairs := make([]keyedEl, len(value.Elements))
			for i, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				pairs[i] = keyedEl{key, el}
			}
			var sortErr error
			sort.SliceStable(pairs, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				cmp, err := compareValues(pairs[i].key, pairs[j].key)
				if err != nil {
					sortErr = err
					return false
				}
				return cmp > 0
			})
			if sortErr != nil {
				return nil, sortErr
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
			return &runtime.List{Elements: result}, nil
		case "sumBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.sumBy expects one argument (function)")
			}
			sum := new(big.Rat)
			hasFloat := false
			var floatSum float64
			for _, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
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
					return nil, fmt.Errorf("list.sumBy: selector must return a number, got %s", key.TypeName())
				}
			}
			if hasFloat {
				return runtime.Float{Value: floatSum}, nil
			}
			if sum.IsInt() {
				return runtime.Int{Value: new(big.Int).Set(sum.Num())}, nil
			}
			return runtime.Decimal{Value: sum}, nil
		case "averageBy":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.averageBy expects one argument (function)")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			sum := new(big.Rat)
			hasFloat := false
			var floatSum float64
			for _, el := range value.Elements {
				key, err := e.callValue(args[0], []runtime.Value{el})
				if err != nil {
					return nil, err
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
					return nil, fmt.Errorf("list.averageBy: selector must return a number, got %s", key.TypeName())
				}
			}
			count := int64(len(value.Elements))
			if hasFloat {
				return runtime.Float{Value: floatSum / float64(count)}, nil
			}
			avg := new(big.Rat).Quo(sum, new(big.Rat).SetInt64(count))
			if avg.IsInt() {
				return runtime.Int{Value: new(big.Int).Set(avg.Num())}, nil
			}
			return runtime.Decimal{Value: avg}, nil
		case "topK":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.topK expects one argument (count)")
			}
			nInt64, ok := toInt64(args[0])
			if !ok {
				return nil, fmt.Errorf("list.topK: count must be an integer")
			}
			n := int(nInt64)
			newElements := make([]runtime.Value, len(value.Elements))
			copy(newElements, value.Elements)
			var sortErr error
			sort.SliceStable(newElements, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				cmp, err := compareValues(newElements[i], newElements[j])
				if err != nil {
					sortErr = err
					return false
				}
				return cmp > 0
			})
			if sortErr != nil {
				return nil, sortErr
			}
			if n < 0 {
				n = 0
			}
			if n > len(newElements) {
				n = len(newElements)
			}
			return &runtime.List{Elements: newElements[:n]}, nil
		case "bottomK":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.bottomK expects one argument (count)")
			}
			nInt64, ok := toInt64(args[0])
			if !ok {
				return nil, fmt.Errorf("list.bottomK: count must be an integer")
			}
			n := int(nInt64)
			newElements := make([]runtime.Value, len(value.Elements))
			copy(newElements, value.Elements)
			var sortErr error
			sort.SliceStable(newElements, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				cmp, err := compareValues(newElements[i], newElements[j])
				if err != nil {
					sortErr = err
					return false
				}
				return cmp < 0
			})
			if sortErr != nil {
				return nil, sortErr
			}
			if n < 0 {
				n = 0
			}
			if n > len(newElements) {
				n = len(newElements)
			}
			return &runtime.List{Elements: newElements[:n]}, nil
		case "frequencies":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.frequencies expects no arguments")
			}
			type countEntry struct {
				value runtime.Value
				count int
			}
			seen := map[string]int{}
			var counts []countEntry
			for _, el := range value.Elements {
				k := el.Inspect()
				if idx, ok := seen[k]; ok {
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
			return runtime.Dict{Entries: entries}, nil
		case "mode":
			if len(args) != 0 {
				return nil, fmt.Errorf("list.mode expects no arguments")
			}
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			type countEntry struct {
				value runtime.Value
				count int
			}
			seen := map[string]int{}
			var counts []countEntry
			for _, el := range value.Elements {
				k := el.Inspect()
				if idx, ok := seen[k]; ok {
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
			return best.value, nil
		case "difference":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.difference expects one argument (list)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.difference: second argument must be a list")
			}
			exclude := map[string]bool{}
			for _, el := range other.Elements {
				exclude[el.Inspect()] = true
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				if !exclude[el.Inspect()] {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "intersection":
			if len(args) != 1 {
				return nil, fmt.Errorf("list.intersection expects one argument (list)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.intersection: second argument must be a list")
			}
			include := map[string]bool{}
			for _, el := range other.Elements {
				include[el.Inspect()] = true
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				if include[el.Inspect()] {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "differenceBy":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.differenceBy expects two arguments (list, function)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.differenceBy: second argument must be a list")
			}
			fn := args[1]
			exclude := map[string]bool{}
			for _, el := range other.Elements {
				key, err := e.callValue(fn, []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				exclude[key.Inspect()] = true
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				key, err := e.callValue(fn, []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if !exclude[key.Inspect()] {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "intersectionBy":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.intersectionBy expects two arguments (list, function)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.intersectionBy: second argument must be a list")
			}
			fn := args[1]
			include := map[string]bool{}
			for _, el := range other.Elements {
				key, err := e.callValue(fn, []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				include[key.Inspect()] = true
			}
			var result []runtime.Value
			for _, el := range value.Elements {
				key, err := e.callValue(fn, []runtime.Value{el})
				if err != nil {
					return nil, err
				}
				if include[key.Inspect()] {
					result = append(result, el)
				}
			}
			return &runtime.List{Elements: result}, nil
		case "zipWith":
			if len(args) != 2 {
				return nil, fmt.Errorf("list.zipWith expects two arguments (list, function)")
			}
			other, ok := args[0].(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("list.zipWith: second argument must be a list")
			}
			fn := args[1]
			n := len(value.Elements)
			if len(other.Elements) < n {
				n = len(other.Elements)
			}
			result := make([]runtime.Value, n)
			for i := 0; i < n; i++ {
				combined, err := e.callValue(fn, []runtime.Value{value.Elements[i], other.Elements[i]})
				if err != nil {
					return nil, err
				}
				result[i] = combined
			}
			return &runtime.List{Elements: result}, nil
		}
	case runtime.String:
		switch name {
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.length expects no arguments")
			}
			return runtime.SmallInt{Value: int64(runtime.StringRuneInfo(value.Value).RuneCount(value.Value))}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: value.Value == ""}, nil
		case "get":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.get expects one argument")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			return stringElement(value, i)
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.contains expects one argument")
			}
			needle, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.contains expects string")
			}
			return runtime.Bool{Value: strings.Contains(value.Value, needle.Value)}, nil
		case "search":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.search expects one argument")
			}
			matches := []runtime.Value{}
			if runtime.IsCallableValue(args[0]) {
				for i, r := range []rune(value.Value) {
					keep, err := e.callValue(args[0], []runtime.Value{runtime.String{Value: string(r)}})
					if err != nil {
						return nil, err
					}
					if isTruthy(keep) {
						matches = append(matches, runtime.NewInt64(int64(i)))
					}
				}
				return &runtime.List{Elements: matches}, nil
			}
			sub, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.search expects a string or callable")
			}
			for _, pos := range native.StringMatchRunePositions(value.Value, sub.Value) {
				matches = append(matches, runtime.NewInt64(int64(pos)))
			}
			return &runtime.List{Elements: matches}, nil
		case "searchPattern":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.searchPattern expects one argument (regex)")
			}
			pat, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.searchPattern expects a string pattern")
			}
			re, err := native.CompileSearchRegex(pat.Value)
			if err != nil {
				return nil, thrownError{value: runtime.Error{Class: "ValueError", Message: fmt.Sprintf("invalid regex: %v", err)}}
			}
			matches := []runtime.Value{}
			for _, pos := range native.RegexMatchRunePositions(re, value.Value) {
				matches = append(matches, runtime.NewInt64(int64(pos)))
			}
			return &runtime.List{Elements: matches}, nil
		case "startsWith":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.startsWith expects one argument")
			}
			prefix, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.startsWith expects string")
			}
			return runtime.Bool{Value: strings.HasPrefix(value.Value, prefix.Value)}, nil
		case "endsWith":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.endsWith expects one argument")
			}
			suffix, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.endsWith expects string")
			}
			return runtime.Bool{Value: strings.HasSuffix(value.Value, suffix.Value)}, nil
		case "trim":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.trim expects no arguments")
			}
			return runtime.String{Value: strings.TrimSpace(value.Value)}, nil
		case "trimStart":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.trimStart expects no arguments")
			}
			return runtime.String{Value: strings.TrimLeftFunc(value.Value, unicode.IsSpace)}, nil
		case "trimEnd":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.trimEnd expects no arguments")
			}
			return runtime.String{Value: strings.TrimRightFunc(value.Value, unicode.IsSpace)}, nil
		case "repeat":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.repeat expects one argument")
			}
			n, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.repeat: %v", err)
			}
			if n < 0 {
				n = 0
			}
			return runtime.String{Value: strings.Repeat(value.Value, n)}, nil
		case "padStart":
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("string.padStart expects (length[, pad])")
			}
			targetLen, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.padStart: %v", err)
			}
			pad := " "
			if len(args) == 2 {
				padStr, ok := args[1].(runtime.String)
				if !ok || len(padStr.Value) == 0 {
					return nil, fmt.Errorf("string.padStart: pad must be a non-empty string")
				}
				pad = padStr.Value
			}
			runes := []rune(value.Value)
			padRunes := []rune(pad)
			for len(runes) < targetLen {
				needed := targetLen - len(runes)
				if needed < len(padRunes) {
					runes = append(padRunes[:needed], runes...)
				} else {
					runes = append(padRunes, runes...)
				}
			}
			if len(runes) > targetLen {
				runes = runes[len(runes)-targetLen:]
			}
			return runtime.String{Value: string(runes)}, nil
		case "padEnd":
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("string.padEnd expects (length[, pad])")
			}
			targetLen, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.padEnd: %v", err)
			}
			pad := " "
			if len(args) == 2 {
				padStr, ok := args[1].(runtime.String)
				if !ok || len(padStr.Value) == 0 {
					return nil, fmt.Errorf("string.padEnd: pad must be a non-empty string")
				}
				pad = padStr.Value
			}
			runes := []rune(value.Value)
			padRunes := []rune(pad)
			for len(runes) < targetLen {
				needed := targetLen - len(runes)
				if needed < len(padRunes) {
					runes = append(runes, padRunes[:needed]...)
				} else {
					runes = append(runes, padRunes...)
				}
			}
			if len(runes) > targetLen {
				runes = runes[:targetLen]
			}
			return runtime.String{Value: string(runes)}, nil
		case "chars":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.chars expects no arguments")
			}
			runes := []rune(value.Value)
			elements := make([]runtime.Value, len(runes))
			for i, r := range runes {
				elements[i] = runtime.String{Value: string(r)}
			}
			return &runtime.List{Elements: elements}, nil
		case "codePoints":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.codePoints expects no arguments")
			}
			runes := []rune(value.Value)
			elements := make([]runtime.Value, len(runes))
			for i, r := range runes {
				elements[i] = runtime.NewInt64(int64(r))
			}
			return &runtime.List{Elements: elements}, nil
		case "graphemes":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.graphemes expects no arguments")
			}
			clusters := native.Graphemes(value.Value)
			elements := make([]runtime.Value, len(clusters))
			for i, c := range clusters {
				elements[i] = runtime.String{Value: c}
			}
			return &runtime.List{Elements: elements}, nil
		case "graphemeLength":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.graphemeLength expects no arguments")
			}
			return runtime.NewInt64(int64(native.GraphemeCount(value.Value))), nil
		case "truncateGraphemes":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.truncateGraphemes expects one argument")
			}
			n, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.truncateGraphemes: %v", err)
			}
			return runtime.String{Value: native.TruncateGraphemes(value.Value, n)}, nil
		case "codePointAt":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.codePointAt expects one argument")
			}
			idx, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.codePointAt: %v", err)
			}
			runes := []rune(value.Value)
			if idx < 0 {
				idx = len(runes) + idx
			}
			if idx < 0 || idx >= len(runes) {
				return runtime.Null{}, nil
			}
			return runtime.NewInt64(int64(runes[idx])), nil
		case "format":
			formatted, err := formatString(value.Value, args)
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: formatted}, nil
		case "lower":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.lower expects no arguments")
			}
			return runtime.String{Value: strings.ToLower(value.Value)}, nil
		case "upper":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.upper expects no arguments")
			}
			return runtime.String{Value: strings.ToUpper(value.Value)}, nil
		case "capitalize":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.capitalize expects no arguments")
			}
			return runtime.String{Value: native.StringCapitalize(value.Value)}, nil
		case "title":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.title expects no arguments")
			}
			return runtime.String{Value: native.StringTitle(value.Value)}, nil
		case "isBlank":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.isBlank expects no arguments")
			}
			return runtime.Bool{Value: native.StringIsBlank(value.Value)}, nil
		case "isInt":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.isInt expects no arguments")
			}
			return runtime.Bool{Value: native.StringIsInt(value.Value)}, nil
		case "isDecimal":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.isDecimal expects no arguments")
			}
			return runtime.Bool{Value: native.StringIsDecimal(value.Value)}, nil
		case "isNumeric":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.isNumeric expects no arguments")
			}
			return runtime.Bool{Value: native.StringIsInt(value.Value) || native.StringIsDecimal(value.Value)}, nil
		case "lines":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.lines expects no arguments")
			}
			parts := native.StringLines(value.Value)
			out := make([]runtime.Value, 0, len(parts))
			for _, part := range parts {
				out = append(out, runtime.String{Value: part})
			}
			return &runtime.List{Elements: out}, nil
		case "removePrefix", "removeSuffix":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.%s expects one argument (string)", name)
			}
			affix, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.%s expects string", name)
			}
			if name == "removePrefix" {
				return runtime.String{Value: native.StringRemovePrefix(value.Value, affix.Value)}, nil
			}
			return runtime.String{Value: native.StringRemoveSuffix(value.Value, affix.Value)}, nil
		case "equalsIgnoreCase":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.equalsIgnoreCase expects one argument (string)")
			}
			other, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.equalsIgnoreCase expects string")
			}
			return runtime.Bool{Value: native.StringEqualsIgnoreCase(value.Value, other.Value)}, nil
		case "containsIgnoreCase":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.containsIgnoreCase expects one argument (string)")
			}
			sub, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.containsIgnoreCase expects string")
			}
			return runtime.Bool{Value: native.StringContainsIgnoreCase(value.Value, sub.Value)}, nil
		case "replace":
			if len(args) != 2 && len(args) != 3 {
				return nil, fmt.Errorf("string.replace expects old, new, and optional count")
			}
			oldValue, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.replace old value must be string")
			}
			newValue, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.replace new value must be string")
			}
			count := -1
			if len(args) == 3 {
				var err error
				count, err = indexInt(args[2])
				if err != nil {
					return nil, err
				}
			}
			return runtime.String{Value: strings.Replace(value.Value, oldValue.Value, newValue.Value, count)}, nil
		case "split":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.split expects one argument")
			}
			sep, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.split expects string")
			}
			parts := strings.Split(value.Value, sep.Value)
			out := make([]runtime.Value, 0, len(parts))
			for _, part := range parts {
				out = append(out, runtime.String{Value: part})
			}
			return &runtime.List{Elements: out}, nil
		case "splitRegex":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.splitRegex expects one argument")
			}
			return e.natives.Call("re", "split", []runtime.Value{args[0], value})
		case "replaceRegex":
			if len(args) != 2 {
				return nil, fmt.Errorf("string.replaceRegex expects (pattern, replacement)")
			}
			return e.natives.Call("re", "replace", []runtime.Value{args[0], args[1], value})
		case "matchesRegex":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.matchesRegex expects one argument")
			}
			return e.natives.Call("re", "test", []runtime.Value{args[0], value})
		case "indexOf":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.indexOf expects one argument")
			}
			needle, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.indexOf expects string")
			}
			byteIndex := strings.Index(value.Value, needle.Value)
			if byteIndex < 0 {
				return runtime.NewInt64(-1), nil
			}
			return runtime.SmallInt{Value: int64(len([]rune(value.Value[:byteIndex])))}, nil
		case "substring", "slice":
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("string.%s expects (start[, end])", name)
			}
			ri := runtime.StringRuneInfo(value.Value)
			n := ri.RuneCount(value.Value)
			start, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("string.%s: %v", name, err)
			}
			if start < 0 {
				start = n + start
			}
			if start < 0 {
				start = 0
			}
			if start > n {
				start = n
			}
			end := n
			if len(args) == 2 {
				end, err = indexInt(args[1])
				if err != nil {
					return nil, fmt.Errorf("string.%s: %v", name, err)
				}
				if end < 0 {
					end = n + end
				}
				if end < 0 {
					end = 0
				}
				if end > n {
					end = n
				}
			}
			if start >= end {
				return runtime.String{Value: ""}, nil
			}
			return runtime.String{Value: ri.Substring(value.Value, start, end)}, nil
		case "toString":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.toString expects no arguments")
			}
			return value, nil
		case "lastIndexOf":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.lastIndexOf expects one argument")
			}
			needle, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.lastIndexOf expects string")
			}
			byteIndex := strings.LastIndex(value.Value, needle.Value)
			if byteIndex < 0 {
				return runtime.NewInt64(-1), nil
			}
			return runtime.SmallInt{Value: int64(len([]rune(value.Value[:byteIndex])))}, nil
		case "reverse":
			if len(args) != 0 {
				return nil, fmt.Errorf("string.reverse expects no arguments")
			}
			runes := []rune(value.Value)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return runtime.String{Value: string(runes)}, nil
		case "count":
			if len(args) != 1 {
				return nil, fmt.Errorf("string.count expects one argument")
			}
			needle, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.count expects string")
			}
			return runtime.NewInt64(int64(strings.Count(value.Value, needle.Value))), nil
		}
	case runtime.Bytes:
		switch name {
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.length expects no arguments")
			}
			return runtime.SmallInt{Value: int64(len(value.Value))}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: len(value.Value) == 0}, nil
		case "get":
			if len(args) != 1 {
				return nil, fmt.Errorf("bytes.get expects one argument")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Value) + i
			}
			if i < 0 || i >= len(value.Value) {
				return nil, fmt.Errorf("bytes index out of range")
			}
			return runtime.NewInt64(int64(value.Value[i])), nil
		case "toString":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.toString expects no arguments")
			}
			return native.BytesToUTF8String(value.Value, "bytes.toString")
		case "toHex":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.toHex expects no arguments")
			}
			return runtime.String{Value: hex.EncodeToString(value.Value)}, nil
		case "toBase64":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.toBase64 expects no arguments")
			}
			return runtime.String{Value: base64.StdEncoding.EncodeToString(value.Value)}, nil
		case "toBase64Url":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.toBase64Url expects no arguments")
			}
			return runtime.String{Value: base64.RawURLEncoding.EncodeToString(value.Value)}, nil
		case "toList":
			if len(args) != 0 {
				return nil, fmt.Errorf("bytes.toList expects no arguments")
			}
			elements := make([]runtime.Value, len(value.Value))
			for i, b := range value.Value {
				elements[i] = runtime.NewInt64(int64(b))
			}
			return &runtime.List{Elements: elements}, nil
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("bytes.contains expects one argument")
			}
			if needle, ok := args[0].(runtime.Bytes); ok {
				return runtime.Bool{Value: bytes.Contains(value.Value, needle.Value)}, nil
			}
			needleVal, ok := toInt64(args[0])
			if !ok {
				return nil, fmt.Errorf("bytes.contains expects bytes or int byte")
			}
			b := needleVal
			if b < 0 || b > 255 {
				return runtime.Bool{Value: false}, nil
			}
			return runtime.Bool{Value: bytes.Contains(value.Value, []byte{byte(b)})}, nil
		case "slice":
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("bytes.slice expects (start[, end])")
			}
			n := len(value.Value)
			start, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("bytes.slice: %v", err)
			}
			if start < 0 {
				start = n + start
			}
			if start < 0 {
				start = 0
			}
			if start > n {
				start = n
			}
			end := n
			if len(args) == 2 {
				end, err = indexInt(args[1])
				if err != nil {
					return nil, fmt.Errorf("bytes.slice: %v", err)
				}
				if end < 0 {
					end = n + end
				}
				if end < start {
					end = start
				}
				if end > n {
					end = n
				}
			}
			out := make([]byte, end-start)
			copy(out, value.Value[start:end])
			return runtime.Bytes{Value: out}, nil
		}
	case runtime.Bool:
		switch name {
		case "toString":
			if len(args) != 0 {
				return nil, fmt.Errorf("bool.toString expects no arguments")
			}
			return runtime.String{Value: value.Inspect()}, nil
		case "not":
			if len(args) != 0 {
				return nil, fmt.Errorf("bool.not expects no arguments")
			}
			return runtime.Bool{Value: !value.Value}, nil
		}
	case runtime.SmallInt:
		// Promote and re-dispatch through the Int branch so every int
		// method works on both runtime representations.
		return e.evalMethodCall(runtime.Int{Value: big.NewInt(value.Value)}, name, args)
	case runtime.Int:
		switch name {
		case "abs":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.abs expects no arguments")
			}
			return native.NumericAbs(value)
		case "isZero":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.isZero expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() == 0}, nil
		case "isPositive":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.isPositive expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() > 0}, nil
		case "isNegative":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.isNegative expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() < 0}, nil
		case "toString":
			base, err := native.IntBaseArg(args, "int.toString")
			if err != nil {
				return nil, err
			}
			if base == 10 {
				return runtime.String{Value: value.Inspect()}, nil
			}
			s, err := native.IntFormatBase(value, base)
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: s}, nil
		case "sign":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.sign expects no arguments")
			}
			return native.NumericSign(value)
		case "clamp":
			if len(args) != 2 {
				return nil, fmt.Errorf("int.clamp expects two arguments")
			}
			return native.NumericClamp(value, args[0], args[1])
		case "isEven":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.isEven expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Bit(0) == 0}, nil
		case "isOdd":
			if len(args) != 0 {
				return nil, fmt.Errorf("int.isOdd expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Bit(0) == 1}, nil
		}
	case runtime.Decimal:
		switch name {
		case "abs":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.abs expects no arguments")
			}
			return native.NumericAbs(value)
		case "isZero":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.isZero expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() == 0}, nil
		case "isPositive":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.isPositive expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() > 0}, nil
		case "isNegative":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.isNegative expects no arguments")
			}
			return runtime.Bool{Value: value.Value.Sign() < 0}, nil
		case "isInt":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.isInt expects no arguments")
			}
			return runtime.Bool{Value: value.Value.IsInt()}, nil
		case "toString":
			if len(args) > 1 {
				return nil, fmt.Errorf("decimal.toString expects optional scale")
			}
			scale := 10
			if len(args) == 1 {
				var err error
				scale, err = decimalFormatScale(args[0])
				if err != nil {
					return nil, err
				}
			}
			return runtime.String{Value: value.Value.FloatString(scale)}, nil
		case "format":
			if len(args) != 1 {
				return nil, fmt.Errorf("decimal.format expects scale")
			}
			scale, err := decimalFormatScale(args[0])
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: value.Value.FloatString(scale)}, nil
		case "round":
			return native.NumericRoundMethod(value, args, native.RoundHalfAwayZero, "decimal.round")
		case "floor":
			return native.NumericRoundMethod(value, args, native.RoundFloor, "decimal.floor")
		case "ceil":
			return native.NumericRoundMethod(value, args, native.RoundCeil, "decimal.ceil")
		case "truncate":
			return native.NumericRoundMethod(value, args, native.RoundTrunc, "decimal.truncate")
		case "sign":
			if len(args) != 0 {
				return nil, fmt.Errorf("decimal.sign expects no arguments")
			}
			return native.NumericSign(value)
		case "clamp":
			if len(args) != 2 {
				return nil, fmt.Errorf("decimal.clamp expects two arguments")
			}
			return native.NumericClamp(value, args[0], args[1])
		}
	case runtime.Range:
		switch name {
		case "length":
			if len(args) != 0 {
				return nil, fmt.Errorf("range.length expects no arguments")
			}
			return runtime.Int{Value: value.Length()}, nil
		case "isEmpty":
			if len(args) != 0 {
				return nil, fmt.Errorf("range.isEmpty expects no arguments")
			}
			return runtime.Bool{Value: value.Length().Sign() == 0}, nil
		case "contains":
			if len(args) != 1 {
				return nil, fmt.Errorf("range.contains expects one argument")
			}
			n, ok := args[0].(runtime.Int)
			if !ok {
				return runtime.Bool{Value: false}, nil
			}
			return runtime.Bool{Value: value.ContainsInt(n.Value)}, nil
		case "first":
			if len(args) != 0 {
				return nil, fmt.Errorf("range.first expects no arguments")
			}
			if value.Length().Sign() == 0 {
				return runtime.Null{}, nil
			}
			return runtime.Int{Value: new(big.Int).Set(value.Start)}, nil
		case "last":
			if len(args) != 0 {
				return nil, fmt.Errorf("range.last expects no arguments")
			}
			n := value.Length()
			if n.Sign() == 0 {
				return runtime.Null{}, nil
			}
			last := new(big.Int).Mul(value.Step, new(big.Int).Sub(n, big.NewInt(1)))
			last.Add(last, value.Start)
			return runtime.Int{Value: last}, nil
		case "toList":
			if len(args) != 0 {
				return nil, fmt.Errorf("range.toList expects no arguments")
			}
			var elements []runtime.Value
			current := new(big.Int).Set(value.Start)
			step := value.Step
			for {
				cmp := current.Cmp(value.End)
				if step.Sign() > 0 {
					if value.Exclusive && cmp >= 0 {
						break
					}
					if !value.Exclusive && cmp > 0 {
						break
					}
				} else {
					if value.Exclusive && cmp <= 0 {
						break
					}
					if !value.Exclusive && cmp < 0 {
						break
					}
				}
				elements = append(elements, runtime.Int{Value: new(big.Int).Set(current)})
				current.Add(current, step)
			}
			return &runtime.List{Elements: elements}, nil
		}
	case runtime.Float:
		switch name {
		case "abs":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.abs expects no arguments")
			}
			return native.NumericAbs(value)
		case "isZero":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isZero expects no arguments")
			}
			return runtime.Bool{Value: value.Value == 0}, nil
		case "isPositive":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isPositive expects no arguments")
			}
			return runtime.Bool{Value: value.Value > 0}, nil
		case "isNegative":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isNegative expects no arguments")
			}
			return runtime.Bool{Value: value.Value < 0}, nil
		case "isNaN":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isNaN expects no arguments")
			}
			return runtime.Bool{Value: math.IsNaN(value.Value)}, nil
		case "isInf":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isInf expects no arguments")
			}
			return runtime.Bool{Value: math.IsInf(value.Value, 0)}, nil
		case "isInt":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.isInt expects no arguments")
			}
			return runtime.Bool{Value: native.FloatIsInt(value.Value)}, nil
		case "toString":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.toString expects no arguments")
			}
			return runtime.String{Value: value.Inspect()}, nil
		case "round":
			return native.NumericRoundMethod(value, args, native.RoundHalfAwayZero, "float.round")
		case "floor":
			return native.NumericRoundMethod(value, args, native.RoundFloor, "float.floor")
		case "ceil":
			return native.NumericRoundMethod(value, args, native.RoundCeil, "float.ceil")
		case "truncate":
			return native.NumericRoundMethod(value, args, native.RoundTrunc, "float.truncate")
		case "sign":
			if len(args) != 0 {
				return nil, fmt.Errorf("float.sign expects no arguments")
			}
			return native.NumericSign(value)
		case "clamp":
			if len(args) != 2 {
				return nil, fmt.Errorf("float.clamp expects two arguments")
			}
			return native.NumericClamp(value, args[0], args[1])
		}
	}
	return nil, native.UnknownMethodError(receiver.TypeName(), name)
}
