package runtime

import "math/big"

// IterableElementsToCheck returns the values an iterable<T> check validates: every element of an eager collection, one representative int for a range, and none for lazy sources, whose values are only known as they are produced.
func IterableElementsToCheck(value Value) []Value {
	switch v := value.(type) {
	case *List:
		return v.Elements
	case Set:
		out := make([]Value, 0, len(v.Elements))
		for _, entry := range v.Elements {
			out = append(out, entry.Value)
		}
		return out
	case Dict:
		return DictPairs(v)
	case String:
		return StringChars(v)
	case Range:
		if v.Length().Sign() == 0 {
			return nil
		}
		return []Value{Int{Value: new(big.Int).Set(v.Start)}}
	}
	return nil
}
