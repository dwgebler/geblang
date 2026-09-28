package native

// PrimitiveMethods is the authoritative list of methods callable on each
// built-in type, used by `dir`, the LSP catalog, and the static
// unknown-method check. It is kept honest by a runtime guard test
// (TestPrimitiveMethodsAreCallable) that calls every name on a live
// value and fails on any that the engine does not recognise, so a stale
// or phantom entry cannot survive. Names are case-sensitive as written.
//
// Conversion helpers (toInt/toDecimal/toFloat/toBool) dispatch ahead of
// the per-type table and are recognised on every primitive; they are
// listed per type only where they are idiomatic (e.g. on string), and
// are otherwise handled via PrimitiveConversionMethods.
var PrimitiveMethods = map[string][]string{
	"string": {
		"chars", "codePointAt", "codePoints", "contains", "count", "endsWith", "format", "get",
		"graphemes", "graphemeLength", "truncateGraphemes",
		"indexOf", "isEmpty", "lastIndexOf", "length", "lower", "matchesRegex",
		"padEnd", "padStart", "repeat", "replace", "replaceRegex", "reverse",
		"search", "searchPattern", "slice", "split", "splitRegex", "startsWith", "substring", "toBool",
		"toDecimal", "toFloat", "toInt", "toString", "trim", "trimEnd",
		"trimStart", "upper",
		"capitalize", "title", "isBlank", "lines", "removePrefix", "removeSuffix",
		"equalsIgnoreCase", "containsIgnoreCase", "isInt", "isDecimal", "isNumeric",
	},
	"list": {
		"all", "any", "append", "averageBy", "binarySearch", "binarySearchBy", "bottomK", "chunk",
		"clear", "concat", "contains", "containsBy", "copy", "deepCopy", "count", "difference",
		"differenceBy", "extend", "fill", "filter", "find", "findLast", "first", "flatten",
		"frequencies", "get", "groupBy", "indexBy", "indexOf", "insert",
		"intersection", "intersectionBy", "isEmpty", "join", "last", "length",
		"lowerBound", "map", "maxBy", "minBy", "mode", "partition", "pop",
		"prepend", "push", "reduce", "remove", "removeAt", "reverse", "reversed",
		"search", "searchPattern",
		"set", "shift", "slice", "sort", "sortBy", "sorted", "sumBy", "takeAt", "takeFirst", "takeLast", "toList", "topBy",
		"topK", "unique", "unshift", "upperBound", "zip", "zipWith",
		"flatMap", "uniqueBy", "takeWhile", "dropWhile", "windowed", "unzip", "scan",
		"enumerate",
	},
	"dict": {
		"bfs", "clear", "contains", "copy", "delete", "dfs", "entries", "get",
		"deepCopy", "hasKey", "insert", "isEmpty", "items", "keys", "length", "merge",
		"remove", "search", "searchPattern", "set", "shortestPath", "topologicalSort", "values",
	},
	"set": {
		"add", "contains", "copy", "deepCopy", "difference", "intersection", "isEmpty",
		"length", "remove", "toList", "union",
	},
	"bytes": {
		"contains", "get", "isEmpty", "length", "slice", "toBase64",
		"toBase64Url", "toHex", "toList", "toString",
	},
	"range": {"contains", "first", "isEmpty", "last", "length", "toList"},
	"int": {
		"abs", "clamp", "isEven", "isNegative", "isOdd", "isPositive", "isZero",
		"sign", "toString",
	},
	"decimal": {
		"abs", "ceil", "clamp", "floor", "format", "isInt", "isNegative", "isPositive",
		"isZero", "round", "sign", "toString", "truncate",
	},
	"float": {
		"abs", "ceil", "clamp", "floor", "isInf", "isInt", "isNaN", "isNegative",
		"isPositive", "isZero", "round", "sign", "toString", "truncate",
	},
	"bool": {"not", "toString"},
}

// PrimitiveConversionMethods are recognised on every primitive (the
// conversion helpers dispatch before the per-type table). Some fail at
// runtime for incompatible source types, but the name is always
// recognised, so it is never a typo.
var PrimitiveConversionMethods = []string{"toBool", "toDecimal", "toFloat", "toInt", "toString"}

// IsPrimitiveType reports whether name is a built-in type with a method
// table.
func IsPrimitiveType(name string) bool {
	_, ok := PrimitiveMethods[name]
	return ok
}
