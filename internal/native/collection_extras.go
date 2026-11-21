package native

import (
	"fmt"
	"sort"

	"geblang/internal/runtime"
)

func sortedSetKeys(s runtime.Set) []string {
	keys := make([]string, 0, len(s.Elements))
	for key := range s.Elements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func setArg(label string, args []runtime.Value) (runtime.Set, error) {
	if len(args) != 1 {
		return runtime.Set{}, fmt.Errorf("%s expects one argument", label)
	}
	other, ok := args[0].(runtime.Set)
	if !ok {
		return runtime.Set{}, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s expects set, got %s", label, args[0].TypeName())}
	}
	return other, nil
}

func callbackArg(label string, args []runtime.Value, call CallFunc) error {
	if len(args) != 1 {
		return fmt.Errorf("%s expects one argument (function)", label)
	}
	if call == nil {
		return fmt.Errorf("%s needs a callback-capable dispatch", label)
	}
	return nil
}

func containsAll(set, sub runtime.Set) bool {
	for key := range sub.Elements {
		if _, ok := set.Elements[key]; !ok {
			return false
		}
	}
	return true
}

func setExtraMethod(s runtime.Set, name string, args []runtime.Value, call CallFunc) (runtime.Value, bool, error) {
	label := "set." + name
	switch name {
	case "isSubsetOf", "isSupersetOf", "isDisjointFrom", "symmetricDifference":
		other, err := setArg(label, args)
		if err != nil {
			return nil, true, err
		}
		switch name {
		case "isSubsetOf":
			return runtime.Bool{Value: containsAll(other, s)}, true, nil
		case "isSupersetOf":
			return runtime.Bool{Value: containsAll(s, other)}, true, nil
		case "isDisjointFrom":
			for key := range s.Elements {
				if _, ok := other.Elements[key]; ok {
					return runtime.Bool{Value: false}, true, nil
				}
			}
			return runtime.Bool{Value: true}, true, nil
		}
		elements := map[string]runtime.SetEntry{}
		for key, entry := range s.Elements {
			if _, ok := other.Elements[key]; !ok {
				elements[key] = entry
			}
		}
		for key, entry := range other.Elements {
			if _, ok := s.Elements[key]; !ok {
				elements[key] = entry
			}
		}
		return runtime.Set{Elements: elements}, true, nil
	case "filter", "map", "any", "all":
		if err := callbackArg(label, args, call); err != nil {
			return nil, true, err
		}
		elements := map[string]runtime.SetEntry{}
		for _, key := range sortedSetKeys(s) {
			entry := s.Elements[key]
			result, err := call(args[0], []runtime.Value{entry.Value})
			if err != nil {
				return nil, true, err
			}
			if name == "map" {
				elements[DictKey(result)] = runtime.SetEntry{Value: result}
				continue
			}
			keep, err := PredicateResult(label, result)
			if err != nil {
				return nil, true, err
			}
			switch {
			case name == "any" && keep:
				return runtime.Bool{Value: true}, true, nil
			case name == "all" && !keep:
				return runtime.Bool{Value: false}, true, nil
			case name == "filter" && keep:
				elements[key] = entry
			}
		}
		switch name {
		case "any":
			return runtime.Bool{Value: false}, true, nil
		case "all":
			return runtime.Bool{Value: true}, true, nil
		}
		return runtime.Set{Elements: elements}, true, nil
	}
	return nil, false, nil
}

func dictExtraMethod(d runtime.Dict, name string, args []runtime.Value, call CallFunc) (runtime.Value, bool, error) {
	label := "dict." + name
	switch name {
	case "pick", "omit":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("%s expects one argument (list of keys)", label)
		}
		keys, ok := args[0].(*runtime.List)
		if !ok {
			return nil, true, runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("%s expects a list of keys, got %s", label, args[0].TypeName())}
		}
		named := make(map[string]bool, len(keys.Elements))
		for _, key := range keys.Elements {
			named[DictKey(key)] = true
		}
		out := runtime.NewDict()
		d.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			if named[key] == (name == "pick") {
				out.PutEntry(key, entry)
			}
			return true
		})
		return out, true, nil
	case "invert":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("dict.invert expects no arguments")
		}
		out := runtime.NewDictHint(d.Len())
		var dupErr error
		d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			key := DictKey(entry.Value)
			if _, exists := out.GetEntry(key); exists {
				dupErr = runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("dict.invert: duplicate value %s", entry.Value.Inspect())}
				return false
			}
			out.PutEntry(key, runtime.DictEntry{Key: entry.Value, Value: entry.Key})
			return true
		})
		if dupErr != nil {
			return nil, true, dupErr
		}
		return out, true, nil
	case "filter", "mapValues", "any", "all":
		if err := callbackArg(label, args, call); err != nil {
			return nil, true, err
		}
		out := runtime.NewDict()
		var result runtime.Value
		var cbErr error
		d.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			if name == "mapValues" {
				mapped, err := call(args[0], []runtime.Value{entry.Value})
				if err != nil {
					cbErr = err
					return false
				}
				out.PutEntry(key, runtime.DictEntry{Key: entry.Key, Value: mapped})
				return true
			}
			raw, err := call(args[0], []runtime.Value{entry.Key, entry.Value})
			if err != nil {
				cbErr = err
				return false
			}
			keep, err := PredicateResult(label, raw)
			if err != nil {
				cbErr = err
				return false
			}
			switch {
			case name == "any" && keep:
				result = runtime.Bool{Value: true}
				return false
			case name == "all" && !keep:
				result = runtime.Bool{Value: false}
				return false
			case name == "filter" && keep:
				out.PutEntry(key, entry)
			}
			return true
		})
		if cbErr != nil {
			return nil, true, cbErr
		}
		if result != nil {
			return result, true, nil
		}
		switch name {
		case "any":
			return runtime.Bool{Value: false}, true, nil
		case "all":
			return runtime.Bool{Value: true}, true, nil
		}
		return out, true, nil
	}
	return nil, false, nil
}
