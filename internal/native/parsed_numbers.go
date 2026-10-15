package native

import (
	"math/big"
	"strconv"
	"strings"

	"geblang/internal/runtime"
)

// ConvertParsedNumbers converts parsed numbers, including numeric collection elements, to a declared field type.
func ConvertParsedNumbers(value runtime.Value, declared string) runtime.Value {
	declared = strings.TrimPrefix(strings.TrimSpace(declared), "?")
	switch declared {
	case "int", "float", "decimal":
		return convertParsedNumber(value, declared)
	}
	if strings.HasSuffix(declared, "[]") {
		return convertParsedList(value, strings.TrimSuffix(declared, "[]"))
	}
	name, args, ok := splitGenericType(declared)
	if !ok {
		return value
	}
	switch {
	case name == "list" && len(args) == 1:
		return convertParsedList(value, args[0])
	case name == "dict" && len(args) == 2:
		dict, isDict := value.(runtime.Dict)
		if !isDict {
			return value
		}
		out := runtime.NewDictHint(dict.Len())
		dict.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			entry.Value = ConvertParsedNumbers(entry.Value, args[1])
			out.PutEntry(key, entry)
			return true
		})
		return out
	}
	return value
}

func convertParsedList(value runtime.Value, element string) runtime.Value {
	list, ok := value.(*runtime.List)
	if !ok {
		return value
	}
	elements := make([]runtime.Value, len(list.Elements))
	for i, item := range list.Elements {
		elements[i] = ConvertParsedNumbers(item, element)
	}
	return &runtime.List{Elements: elements}
}

func convertParsedNumber(value runtime.Value, target string) runtime.Value {
	rat, ok := parsedNumberRat(value)
	if !ok {
		return value
	}
	switch target {
	case "float":
		if _, isFloat := value.(runtime.Float); isFloat {
			return value
		}
		f, _ := rat.Float64()
		return runtime.Float{Value: f}
	case "decimal":
		if _, isDecimal := value.(runtime.Decimal); isDecimal {
			return value
		}
		return runtime.Decimal{Value: rat}
	case "int":
		switch value.(type) {
		case runtime.Int, runtime.SmallInt:
			return value
		}
		if !rat.IsInt() {
			return value
		}
		n := new(big.Int).Set(rat.Num())
		if n.IsInt64() {
			return runtime.SmallInt{Value: n.Int64()}
		}
		return runtime.Int{Value: n}
	}
	return value
}

func parsedNumberRat(value runtime.Value) (*big.Rat, bool) {
	switch v := value.(type) {
	case runtime.SmallInt:
		return new(big.Rat).SetInt64(v.Value), true
	case runtime.Int:
		return new(big.Rat).SetInt(v.Value), true
	case runtime.Decimal:
		return new(big.Rat).Set(v.Value), true
	case runtime.Float:
		// The shortest decimal form keeps 0.1 exact, matching `as decimal`.
		rat, ok := new(big.Rat).SetString(strconv.FormatFloat(v.Value, 'g', -1, 64))
		return rat, ok
	}
	return nil, false
}

func splitGenericType(typ string) (string, []string, bool) {
	open := strings.IndexByte(typ, '<')
	if open <= 0 || !strings.HasSuffix(typ, ">") {
		return "", nil, false
	}
	inner := typ[open+1 : len(typ)-1]
	var args []string
	depth, start := 0, 0
	for i, ch := range inner {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(inner[start:]))
	return strings.ToLower(strings.TrimSpace(typ[:open])), args, true
}
