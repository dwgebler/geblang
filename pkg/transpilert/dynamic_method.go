package transpilert

import (
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Dynamic method dispatch over an any-typed receiver (e.g. a navigated
// json/xml result). CallMethod type-switches on recv's dynamic Go type and
// routes name to the same per-type helpers the concrete-typed lowering uses, so
// a `.length()` on a known list and on an any-list give identical results.
// Return values and unknown-method errors match the interpreter's evalMethodCall
// byte for byte. Higher-order methods (map/filter/reduce/...) take an any-typed
// Geblang closure lowered to a `func(any) any`/`func(any) bool`, asserted and
// applied here; concrete-typed callbacks and *By variants diagnose at lower time.

// CallMethod dispatches name on an any-typed receiver. args are boxed to any.
func CallMethod(recv any, name string, args []any) any {
	switch v := recv.(type) {
	case string:
		return stringMethod(v, name, args)
	case []byte:
		return bytesMethod(v, name, args)
	case bool:
		return boolMethod(v, name, args)
	case float64:
		return floatMethod(v, name, args)
	case *big.Rat:
		return decimalMethod(v, name, args)
	case int64:
		return intMethod(FromInt64(v), name, args)
	case int:
		return intMethod(FromInt64(int64(v)), name, args)
	case *big.Int:
		return intMethod(FromBig(v), name, args)
	case Int:
		return intMethod(v, name, args)
	case []any:
		return listMethod(v, name, args)
	}
	rv := reflect.ValueOf(recv)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() && hasPrefix(rv.Type().Elem().Name(), "OrderedDict[") {
		return dictMethod(rv, name, args)
	}
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		return listMethod(reflectToList(rv), name, args)
	}
	panic(unknownMethod(recv, name))
}

func unknownMethod(recv any, name string) *Error {
	return NewError("RuntimeError", "unknown method "+gbTypeName(recv)+"."+name)
}

func reflectToList(rv reflect.Value) []any {
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func wantArgs(typ, name string, got, n int) {
	if got != n {
		word := "arguments"
		if n == 1 {
			word = "one argument"
		}
		if n == 0 {
			panic(NewError("RuntimeError", typ+"."+name+" expects no "+word))
		}
		panic(NewError("RuntimeError", typ+"."+name+" expects "+word))
	}
}

func stringMethod(s string, name string, args []any) any {
	switch name {
	case "length":
		wantArgs("string", name, len(args), 0)
		return StringLength(s)
	case "isEmpty":
		wantArgs("string", name, len(args), 0)
		return s == ""
	case "upper":
		wantArgs("string", name, len(args), 0)
		return strings.ToUpper(s)
	case "lower":
		wantArgs("string", name, len(args), 0)
		return strings.ToLower(s)
	case "trim":
		wantArgs("string", name, len(args), 0)
		return StringTrim(s)
	case "trimStart":
		wantArgs("string", name, len(args), 0)
		return StringTrimStart(s)
	case "trimEnd":
		wantArgs("string", name, len(args), 0)
		return StringTrimEnd(s)
	case "reverse":
		wantArgs("string", name, len(args), 0)
		return StringReverse(s)
	case "capitalize":
		wantArgs("string", name, len(args), 0)
		return StringCapitalize(s)
	case "title":
		wantArgs("string", name, len(args), 0)
		return StringTitle(s)
	case "isBlank":
		wantArgs("string", name, len(args), 0)
		return StringIsBlank(s)
	case "isInt", "isDecimal", "isNumeric", "isDigits", "isAlpha", "isAlphanumeric", "isLower", "isUpper", "isAscii", "isHex", "isUuid", "isEmail", "isUrl", "isUtf8":
		wantArgs("string", name, len(args), 0)
		return stringPredicates[name](s)
	case "contains":
		wantArgs("string", name, len(args), 1)
		return strings.Contains(s, argString(args[0], "string.contains expects string"))
	case "startsWith":
		wantArgs("string", name, len(args), 1)
		return strings.HasPrefix(s, argString(args[0], "string.startsWith expects string"))
	case "endsWith":
		wantArgs("string", name, len(args), 1)
		return strings.HasSuffix(s, argString(args[0], "string.endsWith expects string"))
	case "containsIgnoreCase":
		wantArgs("string", name, len(args), 1)
		return StringContainsIgnoreCase(s, argString(args[0], "string.containsIgnoreCase expects string"))
	case "equalsIgnoreCase":
		wantArgs("string", name, len(args), 1)
		return strings.EqualFold(s, argString(args[0], "string.equalsIgnoreCase expects string"))
	case "removePrefix":
		wantArgs("string", name, len(args), 1)
		return strings.TrimPrefix(s, argString(args[0], "string.removePrefix expects string"))
	case "removeSuffix":
		wantArgs("string", name, len(args), 1)
		return strings.TrimSuffix(s, argString(args[0], "string.removeSuffix expects string"))
	case "indexOf":
		wantArgs("string", name, len(args), 1)
		return StringIndexOf(s, argString(args[0], "string.indexOf expects string"))
	case "lastIndexOf":
		wantArgs("string", name, len(args), 1)
		return StringLastIndexOf(s, argString(args[0], "string.lastIndexOf expects string"))
	case "count":
		wantArgs("string", name, len(args), 1)
		return StringCount(s, argString(args[0], "string.count expects string"))
	case "get":
		wantArgs("string", name, len(args), 1)
		return indexString(s, args[0])
	case "chars":
		wantArgs("string", name, len(args), 0)
		return strSliceToAny(StringChars(s))
	case "lines":
		wantArgs("string", name, len(args), 0)
		return strSliceToAny(StringLines(s))
	case "split":
		wantArgs("string", name, len(args), 1)
		return strSliceToAny(strings.Split(s, argString(args[0], "string.split expects string")))
	case "splitRegex":
		wantArgs("string", name, len(args), 1)
		return strSliceToAny(StringSplitRegex(s, argString(args[0], "string.splitRegex expects string")))
	case "codePoints":
		wantArgs("string", name, len(args), 0)
		return int64SliceToAny(StringCodePoints(s))
	case "repeat":
		wantArgs("string", name, len(args), 1)
		return StringRepeat(s, indexInt(args[0]))
	case "replace":
		if len(args) != 2 && len(args) != 3 {
			panic(NewError("RuntimeError", "string.replace expects old, new, and optional count"))
		}
		count := -1
		if len(args) == 3 {
			count = indexInt(args[2])
		}
		return strings.Replace(s, argString(args[0], "string.replace old value must be string"), argString(args[1], "string.replace new value must be string"), count)
	case "replaceRegex":
		wantArgs("string", name, len(args), 2)
		return StringReplaceRegex(s, argString(args[0], "string.replaceRegex expects string"), argString(args[1], "string.replaceRegex expects string"))
	case "matchesRegex":
		wantArgs("string", name, len(args), 1)
		return StringMatchesRegex(s, argString(args[0], "string.matchesRegex expects string"))
	case "padStart":
		return strPad(s, args, true)
	case "padEnd":
		return strPad(s, args, false)
	case "substring", "slice":
		return strSlice(s, args)
	case "words":
		wantArgs("string", name, len(args), 0)
		return strSliceToAny(StringWords(s))
	case "snakeCase":
		wantArgs("string", name, len(args), 0)
		return StringSnakeCase(s)
	case "camelCase":
		wantArgs("string", name, len(args), 0)
		return StringCamelCase(s)
	case "kebabCase":
		wantArgs("string", name, len(args), 0)
		return StringKebabCase(s)
	case "wrap":
		wantArgs("string", name, len(args), 1)
		return strSliceToAny(StringWrap(s, int64(indexInt(args[0]))))
	case "center":
		if len(args) < 1 || len(args) > 2 {
			panic(NewError("RuntimeError", "string.center expects (width[, pad])"))
		}
		pad := " "
		if len(args) == 2 {
			pad = argString(args[1], "string.center: pad must be a non-empty string")
		}
		return StringCenter(s, int64(indexInt(args[0])), pad)
	case "slugify":
		panic(NewError("RuntimeError", "string.slugify is not supported in native builds"))
	case "toString":
		wantArgs("string", name, len(args), 0)
		return s
	case "toInt":
		return StringToInt(s)
	case "toFloat":
		return StringToFloat(s)
	case "toDecimal":
		return StringToDecimal(s)
	case "toBool":
		return StringToBool(s)
	}
	panic(NewError("RuntimeError", "unknown method string."+name))
}

func strPad(s string, args []any, atStart bool) string {
	if len(args) < 1 || len(args) > 2 {
		if atStart {
			panic(NewError("RuntimeError", "string.padStart expects (length[, pad])"))
		}
		panic(NewError("RuntimeError", "string.padEnd expects (length[, pad])"))
	}
	targetLen := indexInt(args[0])
	pad := " "
	if len(args) == 2 {
		p, ok := args[1].(string)
		if !ok || p == "" {
			label := "string.padStart"
			if !atStart {
				label = "string.padEnd"
			}
			panic(NewError("RuntimeError", label+": pad must be a non-empty string"))
		}
		pad = p
	}
	if atStart {
		return StringPadStart(s, targetLen, pad)
	}
	return StringPadEnd(s, targetLen, pad)
}

func strSlice(s string, args []any) string {
	if len(args) < 1 || len(args) > 2 {
		panic(NewError("RuntimeError", "string.slice expects (start[, end])"))
	}
	if len(args) == 2 {
		return StringSlice(s, indexInt(args[0]), indexInt(args[1]))
	}
	return StringSliceFrom(s, indexInt(args[0]))
}

func bytesMethod(b []byte, name string, args []any) any {
	switch name {
	case "length":
		wantArgs("bytes", name, len(args), 0)
		return int64(len(b))
	case "isEmpty":
		wantArgs("bytes", name, len(args), 0)
		return len(b) == 0
	case "get":
		wantArgs("bytes", name, len(args), 1)
		return BytesGet(b, indexInt(args[0]))
	case "toHex":
		wantArgs("bytes", name, len(args), 0)
		return BytesToHex(b)
	case "isUtf8":
		wantArgs("bytes", name, len(args), 0)
		return BytesIsUtf8(b)
	case "startsWith":
		wantArgs("bytes", name, len(args), 1)
		return BytesStartsWith(b, args[0])
	case "endsWith":
		wantArgs("bytes", name, len(args), 1)
		return BytesEndsWith(b, args[0])
	case "indexOf":
		wantArgs("bytes", name, len(args), 1)
		return BytesIndexOf(b, args[0])
	case "toBase64":
		wantArgs("bytes", name, len(args), 0)
		return BytesToBase64(b)
	case "toBase64Url":
		wantArgs("bytes", name, len(args), 0)
		return BytesToBase64Url(b)
	case "toList":
		wantArgs("bytes", name, len(args), 0)
		return int64SliceToAny(BytesToList(b))
	case "toString":
		switch len(args) {
		case 0:
			return string(b)
		case 1:
			return BytesToStringEncoding(b, argString(args[0], "bytes.toString encoding must be string"))
		}
		panic(NewError("RuntimeError", "bytes.toString expects optional encoding"))
	case "contains":
		wantArgs("bytes", name, len(args), 1)
		return BytesContains(b, args[0])
	case "slice":
		if len(args) < 1 || len(args) > 2 {
			panic(NewError("RuntimeError", "bytes.slice expects (start[, end])"))
		}
		if len(args) == 2 {
			return BytesSlice(b, true, indexInt(args[0]), indexInt(args[1]))
		}
		return BytesSlice(b, false, indexInt(args[0]), 0)
	}
	panic(NewError("RuntimeError", "unknown method bytes."+name))
}

func boolMethod(v bool, name string, args []any) any {
	switch name {
	case "not":
		wantArgs("bool", name, len(args), 0)
		return !v
	case "toString":
		wantArgs("bool", name, len(args), 0)
		return BoolToString(v)
	}
	panic(NewError("RuntimeError", "unknown method bool."+name))
}

func floatMethod(v float64, name string, args []any) any {
	switch name {
	case "abs":
		wantArgs("float", name, len(args), 0)
		return math.Abs(v)
	case "isZero":
		wantArgs("float", name, len(args), 0)
		return v == 0
	case "isPositive":
		wantArgs("float", name, len(args), 0)
		return v > 0
	case "isNegative":
		wantArgs("float", name, len(args), 0)
		return v < 0
	case "isNaN":
		wantArgs("float", name, len(args), 0)
		return math.IsNaN(v)
	case "isInf":
		wantArgs("float", name, len(args), 0)
		return math.IsInf(v, 0)
	case "sign":
		wantArgs("float", name, len(args), 0)
		return floatSign(v)
	case "toString":
		wantArgs("float", name, len(args), 0)
		return FloatToString(v)
	case "toInt":
		return FloatToInt(v)
	case "toFloat":
		return v
	case "toDecimal":
		return FloatToDecimal(v)
	case "toBool":
		return v != 0
	}
	panic(NewError("RuntimeError", "unknown method float."+name))
}

func floatSign(v float64) int64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func decimalMethod(v *big.Rat, name string, args []any) any {
	switch name {
	case "abs":
		wantArgs("decimal", name, len(args), 0)
		return new(big.Rat).Abs(v)
	case "isZero":
		wantArgs("decimal", name, len(args), 0)
		return v.Sign() == 0
	case "isPositive":
		wantArgs("decimal", name, len(args), 0)
		return v.Sign() > 0
	case "isNegative":
		wantArgs("decimal", name, len(args), 0)
		return v.Sign() < 0
	case "sign":
		wantArgs("decimal", name, len(args), 0)
		return int64(v.Sign())
	case "toString":
		if len(args) == 0 {
			return DecimalToString(v)
		}
		if len(args) == 1 {
			return v.FloatString(indexInt(args[0]))
		}
		panic(NewError("RuntimeError", "decimal.toString expects optional scale"))
	case "toInt":
		return DecimalToInt(v)
	case "toFloat":
		return DecimalToFloat(v)
	case "toDecimal":
		return v
	case "toBool":
		return v.Sign() != 0
	}
	panic(NewError("RuntimeError", "unknown method decimal."+name))
}

func intMethod(v Int, name string, args []any) any {
	switch name {
	case "abs":
		wantArgs("int", name, len(args), 0)
		return FromBig(new(big.Int).Abs(v.big()))
	case "isZero":
		wantArgs("int", name, len(args), 0)
		return v.big().Sign() == 0
	case "isPositive":
		wantArgs("int", name, len(args), 0)
		return v.big().Sign() > 0
	case "isNegative":
		wantArgs("int", name, len(args), 0)
		return v.big().Sign() < 0
	case "sign":
		wantArgs("int", name, len(args), 0)
		return int64(v.big().Sign())
	case "isEven":
		wantArgs("int", name, len(args), 0)
		return v.big().Bit(0) == 0
	case "isOdd":
		wantArgs("int", name, len(args), 0)
		return v.big().Bit(0) == 1
	case "toString":
		if len(args) == 0 {
			return v.show()
		}
		panic(NewError("RuntimeError", "int.toString with a base is not supported in --native"))
	case "toInt":
		return v
	case "toFloat":
		f, _ := new(big.Rat).SetInt(v.big()).Float64()
		return f
	case "toDecimal":
		return new(big.Rat).SetInt(v.big())
	case "toBool":
		return v.big().Sign() != 0
	}
	panic(NewError("RuntimeError", "unknown method int."+name))
}

func listMethod(xs []any, name string, args []any) any {
	switch name {
	case "length":
		wantArgs("list", name, len(args), 0)
		return int64(len(xs))
	case "isEmpty":
		wantArgs("list", name, len(args), 0)
		return len(xs) == 0
	case "first":
		wantArgs("list", name, len(args), 0)
		if len(xs) == 0 {
			return nil
		}
		return xs[0]
	case "last":
		wantArgs("list", name, len(args), 0)
		if len(xs) == 0 {
			return nil
		}
		return xs[len(xs)-1]
	case "get":
		wantArgs("list", name, len(args), 1)
		return indexList(xs, args[0])
	case "contains":
		wantArgs("list", name, len(args), 1)
		for _, el := range xs {
			if dynEqual(el, args[0]) {
				return true
			}
		}
		return false
	case "indexOf":
		wantArgs("list", name, len(args), 1)
		for i, el := range xs {
			if dynEqual(el, args[0]) {
				return int64(i)
			}
		}
		return int64(-1)
	case "join":
		wantArgs("list", name, len(args), 1)
		sep := argString(args[0], "list.join separator must be a string")
		parts := make([]string, len(xs))
		for i, el := range xs {
			parts[i] = Show(el)
		}
		return strings.Join(parts, sep)
	case "slice":
		return listSlice(xs, args)
	case "reversed":
		wantArgs("list", name, len(args), 0)
		out := make([]any, len(xs))
		for i, el := range xs {
			out[len(xs)-1-i] = el
		}
		return out
	case "copy":
		wantArgs("list", name, len(args), 0)
		out := make([]any, len(xs))
		copy(out, xs)
		return out
	case "toList":
		wantArgs("list", name, len(args), 0)
		return xs
	case "sorted":
		wantArgs("list", name, len(args), 0)
		return listSorted(xs)
	case "map":
		return Map(xs, hofMapFn(name, args))
	case "filter":
		return Filter(xs, hofPredFn(name, args))
	case "find":
		return Find(xs, hofPredFn(name, args))
	case "findLast":
		return FindLast(xs, hofPredFn(name, args))
	case "any":
		return AnyMatch(xs, hofPredFn(name, args))
	case "all":
		return AllMatch(xs, hofPredFn(name, args))
	case "count":
		return Count(xs, hofPredFn(name, args))
	case "reduce":
		if len(args) != 2 {
			panic(NewError("RuntimeError", "list.reduce expects two arguments (function, initial)"))
		}
		fn := hofReduceFn(args[0])
		acc := args[1]
		for _, el := range xs {
			acc = fn(acc, el)
		}
		return acc
	case "flatMap":
		fn := hofMapFn(name, args)
		var out []any
		for _, el := range xs {
			nested, ok := fn(el).([]any)
			if !ok {
				panic(NewError("RuntimeError", "list.flatMap function must return a list"))
			}
			out = append(out, nested...)
		}
		return out
	case "sortBy":
		return listSortBy(xs, name, args)
	case "groupBy":
		return listGroupBy(xs, hofKeyFn(name, args))
	case "partition":
		return listPartition(xs, hofPredFn(name, args))
	case "maxBy":
		return listMinMaxBy(xs, hofKeyFn(name, args), true)
	case "minBy":
		return listMinMaxBy(xs, hofKeyFn(name, args), false)
	case "sumBy":
		sum, hasFloat, floatSum := sumByAccumulate(xs, hofKeyFn(name, args), name)
		return numberFromAccumulator(sum, hasFloat, floatSum)
	case "averageBy":
		fn := hofKeyFn(name, args)
		if len(xs) == 0 {
			return nil
		}
		sum, hasFloat, floatSum := sumByAccumulate(xs, fn, name)
		count := int64(len(xs))
		if hasFloat {
			return floatSum / float64(count)
		}
		avg := new(big.Rat).Quo(sum, new(big.Rat).SetInt64(count))
		if avg.IsInt() {
			return FromBig(new(big.Int).Set(avg.Num()))
		}
		return avg
	case "uniqueBy":
		return listUniqueBy(xs, hofKeyFn(name, args))
	case "takeWhile", "dropWhile", "indexBy", "scan",
		"zipWith", "containsBy", "differenceBy", "intersectionBy", "topBy",
		"binarySearchBy":
		panic(NewError("RuntimeError", "list."+name+" on an any value needs a typed list (cast to list<...> first) in --native"))
	}
	panic(NewError("RuntimeError", "unknown method list."+name))
}

// hofMapFn asserts a 1-arg map-style callback lowered from `func(any): any`.
func hofMapFn(method string, args []any) func(any) any {
	if len(args) != 1 {
		panic(NewError("RuntimeError", "list."+method+" expects one argument (function)"))
	}
	fn, ok := args[0].(func(any) any)
	if !ok {
		panic(hofCallbackError(method, args[0]))
	}
	return fn
}

// hofPredFn asserts a 1-arg predicate callback lowered from `func(any): bool`.
func hofPredFn(method string, args []any) func(any) bool {
	if len(args) != 1 {
		panic(NewError("RuntimeError", "list."+method+" expects one argument (function)"))
	}
	fn, ok := args[0].(func(any) bool)
	if !ok {
		panic(hofCallbackError(method, args[0]))
	}
	return fn
}

// hofReduceFn asserts the 2-arg fold callback lowered from `func(any, any): any`.
func hofReduceFn(arg any) func(any, any) any {
	fn, ok := arg.(func(any, any) any)
	if !ok {
		panic(hofCallbackError("reduce", arg))
	}
	return fn
}

func hofCallbackError(method string, got any) *Error {
	return NewError("RuntimeError", "list."+method+" callback on an any value must be an any-typed function; got "+gbTypeName(got))
}

// hofKeyFn asserts a 1-arg key selector lowered from `func(any): any`, shared by
// the *By family.
func hofKeyFn(method string, args []any) func(any) any {
	if len(args) != 1 {
		panic(NewError("RuntimeError", "list."+method+" expects one argument (function)"))
	}
	fn, ok := args[0].(func(any) any)
	if !ok {
		panic(hofCallbackError(method, args[0]))
	}
	return fn
}

// listSortBy stably sorts by the selector key (ascending unless the optional
// second arg is true), returning the receiver mutated like the interpreter.
func listSortBy(xs []any, name string, args []any) []any {
	if len(args) != 1 && len(args) != 2 {
		panic(NewError("RuntimeError", "list.sortBy expects a selector and an optional descending flag"))
	}
	fn, ok := args[0].(func(any) any)
	if !ok {
		panic(hofCallbackError(name, args[0]))
	}
	descending := false
	if len(args) == 2 {
		b, ok := args[1].(bool)
		if !ok {
			panic(NewError("RuntimeError", "list.sortBy descending flag must be a bool"))
		}
		descending = b
	}
	keys := make([]any, len(xs))
	for i, el := range xs {
		keys[i] = fn(el)
	}
	type keyed struct {
		key, el any
	}
	pairs := make([]keyed, len(xs))
	for i := range xs {
		pairs[i] = keyed{keys[i], xs[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		cmp := dynCompare(pairs[i].key, pairs[j].key)
		if descending {
			return cmp > 0
		}
		return cmp < 0
	})
	for i, p := range pairs {
		xs[i] = p.el
	}
	return xs
}

// listGroupBy returns an OrderedDict keyed by the selector result (first-seen
// key value, deduped via dynDictKey), each value the list of matching elements.
func listGroupBy(xs []any, fn func(any) any) *OrderedDict[any, any] {
	d := NewOrderedDict[any, any]()
	index := map[string]any{}
	for _, el := range xs {
		key := groupKey(fn(el))
		dk := dynDictKey(key)
		stored, ok := index[dk]
		if !ok {
			index[dk] = key
			stored = key
			d.Set(key, []any{el})
			continue
		}
		cur, _ := d.Get(stored)
		d.Set(stored, append(cur.([]any), el))
	}
	return d
}

// groupKey rejects a non-comparable selector key (a list) the native dict map
// cannot hold; a typed receiver keeps the interpreter's list-key support.
func groupKey(key any) any {
	if _, isList := key.([]any); isList {
		panic(NewError("RuntimeError", "list.groupBy on an any value needs a typed list (cast to list<...> first) for a non-scalar key in --native"))
	}
	return key
}

// listPartition returns [matching, non-matching] mirroring the interpreter's
// two-list shape.
func listPartition(xs []any, pred func(any) bool) []any {
	yes := []any{}
	no := []any{}
	for _, el := range xs {
		if pred(el) {
			yes = append(yes, el)
		} else {
			no = append(no, el)
		}
	}
	return []any{yes, no}
}

// listMinMaxBy returns the element with the max (or min) selector key; the first
// extreme wins ties, an empty list yields null.
func listMinMaxBy(xs []any, fn func(any) any, max bool) any {
	if len(xs) == 0 {
		return nil
	}
	best := xs[0]
	bestKey := fn(best)
	for _, el := range xs[1:] {
		key := fn(el)
		cmp := dynCompare(key, bestKey)
		if (max && cmp > 0) || (!max && cmp < 0) {
			best, bestKey = el, key
		}
	}
	return best
}

// sumByAccumulate folds the numeric selector results, promoting to float64 once
// any float key appears, matching the interpreter's int/decimal/float handling.
func sumByAccumulate(xs []any, fn func(any) any, name string) (*big.Rat, bool, float64) {
	sum := new(big.Rat)
	hasFloat := false
	var floatSum float64
	for _, el := range xs {
		key := fn(el)
		switch k := key.(type) {
		case float64:
			if !hasFloat {
				floatSum, _ = sum.Float64()
				hasFloat = true
			}
			floatSum += k
		case *big.Rat:
			if hasFloat {
				f, _ := k.Float64()
				floatSum += f
			} else {
				sum.Add(sum, k)
			}
		default:
			r, ok := numericRatNoFloat(key)
			if !ok {
				panic(NewError("RuntimeError", "list."+name+": selector must return a number, got "+gbTypeName(key)))
			}
			if hasFloat {
				f, _ := r.Float64()
				floatSum += f
			} else {
				sum.Add(sum, r)
			}
		}
	}
	return sum, hasFloat, floatSum
}

// numericRatNoFloat converts an int-shaped boxed value to a rational; floats and
// decimals are handled by the caller's explicit cases.
func numericRatNoFloat(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(x), true
	case int:
		return new(big.Rat).SetInt64(int64(x)), true
	case *big.Int:
		return new(big.Rat).SetInt(x), true
	case Int:
		return new(big.Rat).SetInt(x.big()), true
	}
	return nil, false
}

// numberFromAccumulator renders a sumBy total: float when any float key was
// seen, else an int when exact, else a decimal.
func numberFromAccumulator(sum *big.Rat, hasFloat bool, floatSum float64) any {
	if hasFloat {
		return floatSum
	}
	if sum.IsInt() {
		return FromBig(new(big.Int).Set(sum.Num()))
	}
	return sum
}

// listUniqueBy keeps the first element for each selector key, comparing keys by
// the interpreter's value equality.
func listUniqueBy(xs []any, fn func(any) any) []any {
	var seen []any
	var out []any
	for _, el := range xs {
		key := fn(el)
		found := false
		for _, s := range seen {
			if dynEqual(key, s) {
				found = true
				break
			}
		}
		if !found {
			seen = append(seen, key)
			out = append(out, el)
		}
	}
	return out
}

func listSlice(xs []any, args []any) []any {
	if len(args) < 1 || len(args) > 2 {
		panic(NewError("RuntimeError", "list.slice expects (start[, end])"))
	}
	n := len(xs)
	start := indexInt(args[0])
	if start < 0 {
		start += n
	}
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	end := n
	if len(args) == 2 {
		end = indexInt(args[1])
		if end < 0 {
			end += n
		}
		if end < 0 {
			end = 0
		}
		if end > n {
			end = n
		}
	}
	if start >= end {
		return []any{}
	}
	out := make([]any, end-start)
	copy(out, xs[start:end])
	return out
}

func listSorted(xs []any) []any {
	out := make([]any, len(xs))
	copy(out, xs)
	sort.SliceStable(out, func(i, j int) bool { return dynLess(out[i], out[j]) })
	return out
}

func dictMethod(rv reflect.Value, name string, args []any) any {
	switch name {
	case "length":
		wantArgs("dict", name, len(args), 0)
		return int64(dictLen(rv))
	case "isEmpty":
		wantArgs("dict", name, len(args), 0)
		return dictLen(rv) == 0
	case "keys":
		wantArgs("dict", name, len(args), 0)
		return dictKeys(rv)
	case "values":
		wantArgs("dict", name, len(args), 0)
		return dictValues(rv)
	case "items", "entries":
		wantArgs("dict", name, len(args), 0)
		keys := dictKeys(rv)
		vals := dictValues(rv)
		out := make([]any, len(keys))
		for i := range keys {
			out[i] = []any{keys[i], vals[i]}
		}
		return out
	case "get":
		wantArgs("dict", name, len(args), 1)
		val, _ := orderedDictGet(rv, args[0])
		return val
	case "hasKey", "contains":
		wantArgs("dict", name, len(args), 1)
		_, present := orderedDictHas(rv, args[0])
		return present
	case "set", "insert", "delete", "remove", "clear", "merge", "copy", "deepCopy":
		panic(NewError("RuntimeError", "dict."+name+" on an any value needs a typed dict (cast to dict<...> first) in --native"))
	}
	panic(NewError("RuntimeError", "unknown method dict."+name))
}

// dictLen / dictKeys / dictValues reflect over an *OrderedDict[K,V] so dispatch
// needs no type parameters; the dict's insertion order is preserved.
func dictLen(rv reflect.Value) int {
	m := rv.MethodByName("Len")
	return int(m.Call(nil)[0].Int())
}

func dictKeys(rv reflect.Value) []any {
	ks := rv.MethodByName("Keys").Call(nil)[0]
	out := make([]any, ks.Len())
	for i := 0; i < ks.Len(); i++ {
		out[i] = ks.Index(i).Interface()
	}
	return out
}

func dictValues(rv reflect.Value) []any {
	vs := rv.MethodByName("Values").Call(nil)[0]
	out := make([]any, vs.Len())
	for i := 0; i < vs.Len(); i++ {
		out[i] = vs.Index(i).Interface()
	}
	return out
}

// orderedDictHas reports presence, treating a wrong-key-type as a guaranteed
// miss, matching the interpreter's hasKey/contains on a dict.
func orderedDictHas(rv reflect.Value, key any) (any, bool) {
	getM := rv.MethodByName("Get")
	keyType := getM.Type().In(0)
	kv := reflect.ValueOf(unwrapKey(key))
	if !kv.IsValid() || !kv.Type().AssignableTo(keyType) {
		return nil, false
	}
	res := getM.Call([]reflect.Value{kv})
	return res[0].Interface(), res[1].Bool()
}

// dynEqual mirrors the interpreter's value equality for the shapes any values
// hold: numbers compare by exact rational across int/decimal, strings/bools by
// value, null by identity, lists element-wise.
func dynEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ra, oka := toRat(a); oka {
		if rb, okb := toRat(b); okb {
			return ra.Cmp(rb) == 0
		}
		return false
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !dynEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// dynLess gives the natural ordering for list.sorted on homogeneous numeric or
// string lists; mismatched types raise the interpreter's compare error.
func dynLess(a, b any) bool {
	if ra, oka := toRat(a); oka {
		if rb, okb := toRat(b); okb {
			return ra.Cmp(rb) < 0
		}
	}
	sa, oka := a.(string)
	sb, okb := b.(string)
	if oka && okb {
		return sa < sb
	}
	panic(NewError("RuntimeError", "cannot compare "+gbTypeName(a)+" and "+gbTypeName(b)))
}

// dynCompare is the 3-way ordering mirroring native.NumericCompare; argument
// order matters because the incomparable error names left then right.
func dynCompare(a, b any) int {
	if ra, oka := toRat(a); oka {
		if rb, okb := toRat(b); okb {
			return ra.Cmp(rb)
		}
	}
	if fa, oka := toFiniteOrFloat(a); oka {
		if fb, okb := toFiniteOrFloat(b); okb {
			switch {
			case fa < fb:
				return -1
			case fa > fb:
				return 1
			}
			return 0
		}
	}
	if sa, oka := a.(string); oka {
		if sb, okb := b.(string); okb {
			return strings.Compare(sa, sb)
		}
	}
	panic(NewError("RuntimeError", "cannot compare "+gbTypeName(a)+" and "+gbTypeName(b)))
}

// toFiniteOrFloat coerces a boxed numeric to float64 (the NaN/Inf fallback path
// in NumericCompare after the exact-rational attempt).
func toFiniteOrFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case *big.Int:
		f, _ := new(big.Float).SetInt(x).Float64()
		return f, true
	case Int:
		f, _ := new(big.Float).SetInt(x.big()).Float64()
		return f, true
	case *big.Rat:
		f, _ := x.Float64()
		return f, true
	case float64:
		return x, true
	}
	return 0, false
}

// dynDictKey mirrors native.DictKey for the shapes a groupBy selector returns,
// so equal keys dedup and distinct types never collide.
func dynDictKey(v any) string {
	switch x := v.(type) {
	case nil:
		return "n"
	case bool:
		if x {
			return "b1"
		}
		return "b0"
	case string:
		return "s" + x
	case []byte:
		return "y" + BytesToHex(x)
	case []any:
		parts := make([]string, len(x))
		for i, el := range x {
			parts[i] = dynDictKey(el)
		}
		return "L[" + strings.Join(parts, ",") + "]"
	case float64:
		if x == 0 {
			x = 0
		}
		return "f" + strconv.FormatFloat(x, 'g', -1, 64)
	}
	if r, ok := toRat(v); ok {
		if _, isDec := v.(*big.Rat); isDec {
			return "d" + r.RatString()
		}
		return "i" + r.RatString()
	}
	panic(NewError("RuntimeError", "unhashable dict key "+gbTypeName(v)))
}

// toRat converts a boxed int/decimal value to an exact rational; floats use
// their exact rational unless non-finite.
func toRat(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(x), true
	case int:
		return new(big.Rat).SetInt64(int64(x)), true
	case *big.Int:
		return new(big.Rat).SetInt(x), true
	case Int:
		return new(big.Rat).SetInt(x.big()), true
	case *big.Rat:
		return x, true
	case float64:
		if r := new(big.Rat).SetFloat64(x); r != nil {
			return r, true
		}
	}
	return nil, false
}

func argString(v any, msg string) string {
	s, ok := v.(string)
	if !ok {
		panic(NewError("RuntimeError", msg))
	}
	return s
}

func strSliceToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func int64SliceToAny(ns []int64) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}
