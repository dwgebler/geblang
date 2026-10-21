package runtime

// FreezeShallowCopy returns a frozen shallow copy, leaving the original
// mutable. Backs `const` parameters; primitives are returned unchanged.
func FreezeShallowCopy(v Value) Value {
	switch val := v.(type) {
	case *List:
		elems := make([]Value, len(val.Elements))
		copy(elems, val.Elements)
		return &List{Elements: elems, Frozen: true, ElementTypes: append([]string(nil), val.ElementTypes...)}
	case Dict:
		frozen := NewDictHint(val.Len())
		val.ForEachEntry(func(k string, e DictEntry) bool {
			frozen.PutEntry(k, e)
			return true
		})
		frozen.Frozen = true
		frozen.ElementTypes = append([]string(nil), val.ElementTypes...)
		return frozen
	case Set:
		elements := make(map[string]SetEntry, len(val.Elements))
		for k, e := range val.Elements {
			elements[k] = e
		}
		return Set{Elements: elements, Frozen: true, ElementTypes: append([]string(nil), val.ElementTypes...)}
	case *Instance:
		if val == nil {
			return v
		}
		fields := make(map[string]Value, len(val.Fields))
		for k, fv := range val.Fields {
			fields[k] = fv
		}
		return &Instance{Class: val.Class, Fields: fields, TypeBindings: val.TypeBindings, Frozen: true, ExtraTypeNames: val.ExtraTypeNames}
	default:
		return v
	}
}

// ShallowFreeze returns v with Frozen set to true for mutable collection and
// instance types. Primitive types are already immutable; they are returned
// unchanged. The returned value shares internal data (slice / map) with the
// original, so mutations attempted through the original are blocked once it is
// also frozen.
func ShallowFreeze(v Value) Value {
	switch val := v.(type) {
	case *List:
		val.Frozen = true
		return val
	case Dict:
		val.Frozen = true
		return val
	case Set:
		val.Frozen = true
		return val
	case *Instance:
		if val != nil {
			val.Frozen = true
		}
		return val
	default:
		return v
	}
}

// DeepFreeze recursively freezes v and all mutable values nested within it.
// Collections are copied (keeping insertion order and element-type tags);
// instances are frozen in place.
func DeepFreeze(v Value) Value {
	switch val := v.(type) {
	case *List:
		elems := make([]Value, len(val.Elements))
		for i, e := range val.Elements {
			elems[i] = DeepFreeze(e)
		}
		return &List{Elements: elems, Frozen: true, ElementTypes: append([]string(nil), val.ElementTypes...)}
	case Dict:
		frozen := NewDictHint(val.Len())
		val.ForEachEntry(func(k string, entry DictEntry) bool {
			frozen.PutEntry(k, DictEntry{Key: entry.Key, Value: DeepFreeze(entry.Value)})
			return true
		})
		frozen.Frozen = true
		frozen.ElementTypes = append([]string(nil), val.ElementTypes...)
		return frozen
	case Set:
		elements := make(map[string]SetEntry, len(val.Elements))
		for k, entry := range val.Elements {
			elements[k] = SetEntry{Value: DeepFreeze(entry.Value)}
		}
		return Set{Elements: elements, Frozen: true, ElementTypes: append([]string(nil), val.ElementTypes...)}
	case *Instance:
		if val != nil {
			for k, fv := range val.Fields {
				val.Fields[k] = DeepFreeze(fv)
			}
			val.Frozen = true
		}
		return val
	default:
		return v
	}
}

// IsDeepFrozen reports whether v can never change: a primitive, or a frozen
// collection or instance whose every nested value is itself deeply frozen.
// Such a value is safe to share between concurrent tasks without copying.
// Callables, cells, modules and native handles are conservatively treated as
// mutable because they can reach state outside the value.
func IsDeepFrozen(v Value) bool {
	return isDeepFrozen(v, map[any]bool{})
}

func isDeepFrozen(v Value, seen map[any]bool) bool {
	switch val := v.(type) {
	case nil, Null, Bool, Int, SmallInt, Float, Decimal, String, Bytes:
		return true
	case *List:
		if val == nil || !val.Frozen {
			return false
		}
		if seen[val] {
			return true
		}
		seen[val] = true
		for _, e := range val.Elements {
			if !isDeepFrozen(e, seen) {
				return false
			}
		}
		return true
	case Dict:
		if !val.Frozen {
			return false
		}
		if id := dictIdentity(val); id != 0 {
			if seen[id] {
				return true
			}
			seen[id] = true
		}
		ok := true
		val.ForEachEntry(func(_ string, entry DictEntry) bool {
			ok = isDeepFrozen(entry.Key, seen) && isDeepFrozen(entry.Value, seen)
			return ok
		})
		return ok
	case Set:
		if !val.Frozen {
			return false
		}
		for _, entry := range val.Elements {
			if !isDeepFrozen(entry.Value, seen) {
				return false
			}
		}
		return true
	case *Instance:
		if val == nil || !val.Frozen {
			return false
		}
		if seen[val] {
			return true
		}
		seen[val] = true
		for _, fv := range val.Fields {
			if !isDeepFrozen(fv, seen) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// IsFrozen returns true if v is frozen or is a primitive (always immutable).
func IsFrozen(v Value) bool {
	switch val := v.(type) {
	case *List:
		return val.Frozen
	case Dict:
		return val.Frozen
	case Set:
		return val.Frozen
	case *Instance:
		return val != nil && val.Frozen
	case Null, Bool, Int, SmallInt, Float, Decimal, String, Bytes:
		return true
	default:
		return true
	}
}
