package bytecode

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

func primitiveReSplit(patternArg runtime.Value, text string) (runtime.Value, error) {
	pattern, ok := patternArg.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("regex pattern must be string")
	}
	re, err := native.CompileCachedRegex(pattern.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %v", err)
	}
	parts := re.Split(text, -1)
	out := make([]runtime.Value, len(parts))
	for i, p := range parts {
		out[i] = runtime.String{Value: p}
	}
	return &runtime.List{Elements: out}, nil
}

func primitiveReReplace(patternArg runtime.Value, text string, replArg runtime.Value) (runtime.Value, error) {
	pattern, ok := patternArg.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("regex pattern must be string")
	}
	repl, ok := replArg.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("regex replacement must be string")
	}
	re, err := native.CompileCachedRegex(pattern.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %v", err)
	}
	return runtime.String{Value: re.ReplaceAllString(text, repl.Value)}, nil
}

func primitiveReMatches(patternArg runtime.Value, text string) (runtime.Value, error) {
	pattern, ok := patternArg.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("regex pattern must be string")
	}
	re, err := native.CompileCachedRegex(pattern.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %v", err)
	}
	return runtime.Bool{Value: re.MatchString(text)}, nil
}

func primitiveMethod(receiver runtime.Value, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "toInt", "toDecimal", "toFloat", "toBool":
		if name == "toInt" {
			if text, ok := receiver.(runtime.String); ok && len(args) >= 1 {
				base, err := native.IntBaseArg(args, "string.toInt")
				if err != nil {
					return nil, err
				}
				return native.StringParseBase(text.Value, base, "string.toInt")
			}
		}
		target, _ := primitiveConversionTarget(name)
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
	case "length":
		if len(args) != 0 {
			return nil, fmt.Errorf("length expects no arguments")
		}
		return primitiveLength(receiver)
	case "isEmpty":
		if len(args) != 0 {
			return nil, fmt.Errorf("length expects no arguments")
		}
		length, err := primitiveLength(receiver)
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
	case "contains":
		if len(args) != 1 {
			return nil, fmt.Errorf("contains expects one argument")
		}
		switch value := receiver.(type) {
		case runtime.String:
			arg, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.contains expects string")
			}
			return runtime.Bool{Value: strings.Contains(value.Value, arg.Value)}, nil
		case *runtime.List:
			for _, element := range value.Elements {
				if valuesEqual(element, args[0]) {
					return runtime.Bool{Value: true}, nil
				}
			}
			return runtime.Bool{Value: false}, nil
		case runtime.Dict:
			_, ok := value.GetEntry(native.DictKey(args[0]))
			return runtime.Bool{Value: ok}, nil
		case runtime.Set:
			_, ok := value.Elements[native.DictKey(args[0])]
			return runtime.Bool{Value: ok}, nil
		case runtime.Bytes:
			if needle, ok := args[0].(runtime.Bytes); ok {
				return runtime.Bool{Value: bytes.Contains(value.Value, needle.Value)}, nil
			}
			byteVal, ok := native.AsInt64(args[0])
			if !ok {
				return nil, fmt.Errorf("bytes.contains expects bytes or int byte")
			}
			if byteVal < 0 || byteVal > 255 {
				return runtime.Bool{Value: false}, nil
			}
			return runtime.Bool{Value: bytes.Contains(value.Value, []byte{byte(byteVal)})}, nil
		case runtime.Range:
			nb, ok := native.IntValueToBigInt(args[0])
			if !ok {
				return runtime.Bool{Value: false}, nil
			}
			return runtime.Bool{Value: value.ContainsInt(nb)}, nil
		default:
			return nil, fmt.Errorf("%s has no method contains", receiver.TypeName())
		}
	case "startsWith":
		if len(args) != 1 {
			return nil, fmt.Errorf("startsWith expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method startsWith", receiver.TypeName())
		}
		arg, err := singleStringArg(args, "string.startsWith")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: strings.HasPrefix(value.Value, arg)}, nil
	case "endsWith":
		if len(args) != 1 {
			return nil, fmt.Errorf("endsWith expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method endsWith", receiver.TypeName())
		}
		arg, err := singleStringArg(args, "string.endsWith")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: strings.HasSuffix(value.Value, arg)}, nil
	case "trim":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.trim expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method trim", receiver.TypeName())
		}
		return runtime.String{Value: strings.TrimSpace(value.Value)}, nil
	case "trimStart":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.trimStart expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method trimStart", receiver.TypeName())
		}
		return runtime.String{Value: strings.TrimLeftFunc(value.Value, unicode.IsSpace)}, nil
	case "trimEnd":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.trimEnd expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method trimEnd", receiver.TypeName())
		}
		return runtime.String{Value: strings.TrimRightFunc(value.Value, unicode.IsSpace)}, nil
	case "repeat":
		if len(args) != 1 {
			return nil, fmt.Errorf("string.repeat expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method repeat", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method padStart", receiver.TypeName())
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
		for len(runes) < targetLen {
			padRunes := []rune(pad)
			needed := targetLen - len(runes)
			if needed < len(padRunes) {
				runes = append([]rune(pad[:needed]), runes...)
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method padEnd", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method chars", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method codePoints", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method graphemes", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method graphemeLength", receiver.TypeName())
		}
		return runtime.NewInt64(int64(native.GraphemeCount(value.Value))), nil
	case "truncateGraphemes":
		if len(args) != 1 {
			return nil, fmt.Errorf("string.truncateGraphemes expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method truncateGraphemes", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method codePointAt", receiver.TypeName())
		}
		i, err := indexInt(args[0])
		if err != nil {
			return nil, fmt.Errorf("string.codePointAt: %v", err)
		}
		runes := []rune(value.Value)
		if i < 0 {
			i = len(runes) + i
		}
		if i < 0 || i >= len(runes) {
			return runtime.Null{}, nil
		}
		return runtime.NewInt64(int64(runes[i])), nil
	case "format":
		switch value := receiver.(type) {
		case runtime.String:
			formatted, err := formatString(value.Value, args)
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: formatted}, nil
		case runtime.Decimal:
			if len(args) != 1 {
				return nil, fmt.Errorf("decimal.format expects scale")
			}
			scale, err := decimalFormatScale(args[0])
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: value.Value.FloatString(scale)}, nil
		default:
			return nil, fmt.Errorf("%s has no method format", receiver.TypeName())
		}
	case "lower":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.lower expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method lower", receiver.TypeName())
		}
		return runtime.String{Value: strings.ToLower(value.Value)}, nil
	case "upper":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.upper expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method upper", receiver.TypeName())
		}
		return runtime.String{Value: strings.ToUpper(value.Value)}, nil
	case "capitalize":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.capitalize expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method capitalize", receiver.TypeName())
		}
		return runtime.String{Value: native.StringCapitalize(value.Value)}, nil
	case "title":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.title expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method title", receiver.TypeName())
		}
		return runtime.String{Value: native.StringTitle(value.Value)}, nil
	case "isBlank":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.isBlank expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method isBlank", receiver.TypeName())
		}
		return runtime.Bool{Value: native.StringIsBlank(value.Value)}, nil
	case "isDecimal":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.isDecimal expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method isDecimal", receiver.TypeName())
		}
		return runtime.Bool{Value: native.StringIsDecimal(value.Value)}, nil
	case "isNumeric":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.isNumeric expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method isNumeric", receiver.TypeName())
		}
		return runtime.Bool{Value: native.StringIsInt(value.Value) || native.StringIsDecimal(value.Value)}, nil
	case "lines":
		if len(args) != 0 {
			return nil, fmt.Errorf("string.lines expects no arguments")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method lines", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, native.UnknownMethodError(receiver.TypeName(), name)
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method equalsIgnoreCase", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method containsIgnoreCase", receiver.TypeName())
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
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method replace", receiver.TypeName())
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
	case "splitRegex":
		if len(args) != 1 {
			return nil, fmt.Errorf("string.splitRegex expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method splitRegex", receiver.TypeName())
		}
		return primitiveReSplit(args[0], value.Value)
	case "replaceRegex":
		if len(args) != 2 {
			return nil, fmt.Errorf("string.replaceRegex expects (pattern, replacement)")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method replaceRegex", receiver.TypeName())
		}
		return primitiveReReplace(args[0], value.Value, args[1])
	case "matchesRegex":
		if len(args) != 1 {
			return nil, fmt.Errorf("string.matchesRegex expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method matchesRegex", receiver.TypeName())
		}
		return primitiveReMatches(args[0], value.Value)
	case "split":
		if len(args) != 1 {
			return nil, fmt.Errorf("string.split expects one argument")
		}
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method split", receiver.TypeName())
		}
		sep, err := singleStringArg(args, "string.split")
		if err != nil {
			return nil, err
		}
		parts := strings.Split(value.Value, sep)
		out := make([]runtime.Value, 0, len(parts))
		for _, part := range parts {
			out = append(out, runtime.String{Value: part})
		}
		return &runtime.List{Elements: out}, nil
	case "indexOf":
		if len(args) != 1 {
			return nil, fmt.Errorf("indexOf expects one argument")
		}
		switch value := receiver.(type) {
		case runtime.String:
			needle, err := singleStringArg(args, "string.indexOf")
			if err != nil {
				return nil, err
			}
			byteIndex := strings.Index(value.Value, needle)
			if byteIndex < 0 {
				return runtime.NewInt64(-1), nil
			}
			return runtime.SmallInt{Value: int64(len([]rune(value.Value[:byteIndex])))}, nil
		case *runtime.List:
			for i, element := range value.Elements {
				if valuesEqual(element, args[0]) {
					return runtime.NewInt64(int64(i)), nil
				}
			}
			return runtime.NewInt64(-1), nil
		default:
			return nil, fmt.Errorf("%s has no method indexOf", receiver.TypeName())
		}
	case "substring", "slice":
		switch value := receiver.(type) {
		case runtime.String:
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
		case *runtime.List:
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
		case runtime.Bytes:
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
		default:
			return nil, native.UnknownMethodError(receiver.TypeName(), name)
		}
	case "lastIndexOf":
		value, ok := receiver.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s has no method lastIndexOf", receiver.TypeName())
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("string.lastIndexOf expects one argument")
		}
		needle, ok2 := args[0].(runtime.String)
		if !ok2 {
			return nil, fmt.Errorf("string.lastIndexOf expects string")
		}
		byteIndex := strings.LastIndex(value.Value, needle.Value)
		if byteIndex < 0 {
			return runtime.NewInt64(-1), nil
		}
		return runtime.SmallInt{Value: int64(len([]rune(value.Value[:byteIndex])))}, nil
	case "reverse":
		switch value := receiver.(type) {
		case runtime.String:
			if len(args) != 0 {
				return nil, fmt.Errorf("string.reverse expects no arguments")
			}
			runes := []rune(value.Value)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return runtime.String{Value: string(runes)}, nil
		default:
			return nil, fmt.Errorf("%s has no method reverse", receiver.TypeName())
		}
	case "count":
		switch value := receiver.(type) {
		case runtime.String:
			if len(args) != 1 {
				return nil, fmt.Errorf("string.count expects one argument")
			}
			needle, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("string.count expects string")
			}
			return runtime.NewInt64(int64(strings.Count(value.Value, needle.Value))), nil
		default:
			return nil, fmt.Errorf("%s has no method count", receiver.TypeName())
		}
	case "get":
		if len(args) != 1 {
			return nil, fmt.Errorf("get expects one argument")
		}
		switch value := receiver.(type) {
		case *runtime.List:
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Elements) + i
			}
			if i < 0 || i >= len(value.Elements) {
				return runtime.Null{}, nil
			}
			return value.Elements[i], nil
		case runtime.Dict:
			entry, ok := value.GetEntry(dictKeyFor(args[0]))
			if !ok {
				return runtime.Null{}, nil
			}
			return entry.Value, nil
		case runtime.String:
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			runes := []rune(value.Value)
			if i < 0 {
				i = len(runes) + i
			}
			if i < 0 || i >= len(runes) {
				return runtime.Null{}, nil
			}
			return runtime.String{Value: string(runes[i])}, nil
		case runtime.Bytes:
			i, err := indexInt(args[0])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i = len(value.Value) + i
			}
			if i < 0 || i >= len(value.Value) {
				return runtime.Null{}, nil
			}
			return runtime.NewInt64(int64(value.Value[i])), nil
		default:
			return nil, fmt.Errorf("%s has no method get", receiver.TypeName())
		}
	case "copy":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.copy expects no arguments", receiver.TypeName())
		}
		switch value := receiver.(type) {
		case *runtime.List:
			elems := make([]runtime.Value, len(value.Elements))
			copy(elems, value.Elements)
			return &runtime.List{Elements: elems}, nil
		case runtime.Dict:
			d := runtime.NewDict()
			value.ForEachEntry(func(k string, e runtime.DictEntry) bool {
				d.PutEntry(k, e)
				return true
			})
			return d, nil
		case runtime.Set:
			elements := make(map[string]runtime.SetEntry, len(value.Elements))
			for k, v := range value.Elements {
				elements[k] = v
			}
			return runtime.Set{Elements: elements}, nil
		default:
			return nil, fmt.Errorf("%s has no method copy", receiver.TypeName())
		}
	case "deepCopy":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.deepCopy expects no arguments", receiver.TypeName())
		}
		switch receiver.(type) {
		case *runtime.List, runtime.Dict, runtime.Set:
			return runtime.CloneValue(receiver), nil
		default:
			return nil, fmt.Errorf("%s has no method deepCopy", receiver.TypeName())
		}
	case "set":
		if len(args) != 2 {
			return nil, fmt.Errorf("set expects two arguments")
		}
		switch value := receiver.(type) {
		case *runtime.List:
			if value.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
			}
			if len(value.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[1], value.ElementTypes[0]) {
				return nil, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot assign %s to list<%s>", args[1].TypeName(), value.ElementTypes[0])}
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
		case runtime.Dict:
			if value.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen dict"}
			}
			if err := vmCheckDictWriteTags(value, args[0], args[1]); err != nil {
				return nil, err
			}
			value.PutEntry(native.DictKey(args[0]), runtime.DictEntry{Key: args[0], Value: args[1]})
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("%s has no method set", receiver.TypeName())
		}
	case "delete":
		if len(args) != 1 {
			return nil, fmt.Errorf("dict.delete expects one argument")
		}
		value, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s has no method delete", receiver.TypeName())
		}
		if value.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen dict"}
		}
		value.DelEntry(native.DictKey(args[0]))
		return runtime.Null{}, nil
	case "insert":
		if dict, ok := receiver.(runtime.Dict); ok {
			if len(args) != 2 {
				return nil, fmt.Errorf("dict.insert expects two arguments")
			}
			if dict.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen dict"}
			}
			if err := vmCheckDictWriteTags(dict, args[0], args[1]); err != nil {
				return nil, err
			}
			dict.PutEntry(native.DictKey(args[0]), runtime.DictEntry{Key: args[0], Value: args[1]})
			return runtime.Null{}, nil
		}
		if list, ok := receiver.(*runtime.List); ok {
			if len(args) != 2 {
				return nil, fmt.Errorf("list.insert expects two arguments (index, value)")
			}
			i, err := indexInt(args[0])
			if err != nil {
				return nil, fmt.Errorf("list.insert: %v", err)
			}
			if i < 0 {
				i = len(list.Elements) + i
			}
			if i < 0 {
				i = 0
			}
			if i > len(list.Elements) {
				i = len(list.Elements)
			}
			if list.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
			}
			if len(list.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[1], list.ElementTypes[0]) {
				return nil, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot insert %s to list<%s>", args[1].TypeName(), list.ElementTypes[0])}
			}
			list.Elements = append(list.Elements, nil)
			copy(list.Elements[i+1:], list.Elements[i:])
			list.Elements[i] = args[1]
			return list, nil
		}
		return nil, fmt.Errorf("%s has no method insert", receiver.TypeName())
	case "add":
		if len(args) != 1 {
			return nil, fmt.Errorf("set.add expects one argument")
		}
		value, ok := receiver.(runtime.Set)
		if !ok {
			return nil, fmt.Errorf("%s has no method add", receiver.TypeName())
		}
		if value.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen set"}
		}
		if len(value.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[0], value.ElementTypes[0]) {
			return nil, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot add %s to set<%s>", args[0].TypeName(), value.ElementTypes[0])}
		}
		value.Elements[native.DictKey(args[0])] = runtime.SetEntry{Value: args[0]}
		return value, nil
	case "remove":
		if len(args) != 1 {
			return nil, fmt.Errorf("remove expects one argument")
		}
		switch v := receiver.(type) {
		case runtime.Set:
			if v.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen set"}
			}
			delete(v.Elements, native.DictKey(args[0]))
			return v, nil
		case runtime.Dict:
			if v.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen dict"}
			}
			v.DelEntry(native.DictKey(args[0]))
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("%s has no method remove", receiver.TypeName())
		}
	case "toList":
		if len(args) != 0 {
			return nil, fmt.Errorf("toList expects no arguments")
		}
		switch value := receiver.(type) {
		case *runtime.List:
			return value, nil
		case runtime.Set:
			return &runtime.List{Elements: orderedSetValues(value)}, nil
		case runtime.Range:
			var elements []runtime.Value
			current := new(big.Int).Set(value.Start)
			step := value.Step
			for rangeContains(current, value.End, step, value.Exclusive) {
				elements = append(elements, runtime.Int{Value: new(big.Int).Set(current)})
				current.Add(current, step)
			}
			return &runtime.List{Elements: elements}, nil
		case runtime.Bytes:
			elements := make([]runtime.Value, len(value.Value))
			for i, b := range value.Value {
				elements[i] = runtime.NewInt64(int64(b))
			}
			return &runtime.List{Elements: elements}, nil
		default:
			return nil, fmt.Errorf("%s has no method toList", receiver.TypeName())
		}
	case "union":
		if len(args) != 1 {
			return nil, fmt.Errorf("set.union expects one argument")
		}
		value, ok := receiver.(runtime.Set)
		if !ok {
			return nil, fmt.Errorf("%s has no method union", receiver.TypeName())
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
		value, ok := receiver.(runtime.Set)
		if !ok {
			return nil, fmt.Errorf("%s has no method intersection", receiver.TypeName())
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
		value, ok := receiver.(runtime.Set)
		if !ok {
			return nil, fmt.Errorf("%s has no method difference", receiver.TypeName())
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
	case "hasKey":
		if len(args) != 1 {
			return nil, fmt.Errorf("dict.hasKey expects one argument")
		}
		value, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s has no method hasKey", receiver.TypeName())
		}
		_, exists := value.GetEntry(native.DictKey(args[0]))
		return runtime.Bool{Value: exists}, nil
	case "keys":
		if len(args) != 0 {
			return nil, fmt.Errorf("dict.keys expects no arguments")
		}
		value, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s has no method keys", receiver.TypeName())
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
		value, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s has no method values", receiver.TypeName())
		}
		values := make([]runtime.Value, 0, value.Len())
		value.ForEachEntry(func(_ string, e runtime.DictEntry) bool {
			values = append(values, e.Value)
			return true
		})
		return &runtime.List{Elements: values}, nil
	case "items", "entries":
		if len(args) != 0 {
			return nil, fmt.Errorf("dict.%s expects no arguments", name)
		}
		value, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, native.UnknownMethodError(receiver.TypeName(), name)
		}
		items := make([]runtime.Value, 0, value.Len())
		value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			items = append(items, &runtime.List{Elements: []runtime.Value{entry.Key, entry.Value}})
			return true
		})
		return &runtime.List{Elements: items}, nil
	case "first":
		if len(args) != 0 {
			return nil, fmt.Errorf("first expects no arguments")
		}
		switch value := receiver.(type) {
		case *runtime.List:
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			return value.Elements[0], nil
		case runtime.Range:
			if value.Length().Sign() == 0 {
				return runtime.Null{}, nil
			}
			return runtime.Int{Value: new(big.Int).Set(value.Start)}, nil
		default:
			return nil, fmt.Errorf("%s has no method first", receiver.TypeName())
		}
	case "last":
		if len(args) != 0 {
			return nil, fmt.Errorf("last expects no arguments")
		}
		switch value := receiver.(type) {
		case *runtime.List:
			if len(value.Elements) == 0 {
				return runtime.Null{}, nil
			}
			return value.Elements[len(value.Elements)-1], nil
		case runtime.Range:
			n := value.Length()
			if n.Sign() == 0 {
				return runtime.Null{}, nil
			}
			last := new(big.Int).Mul(value.Step, new(big.Int).Sub(n, big.NewInt(1)))
			last.Add(last, value.Start)
			return runtime.Int{Value: last}, nil
		default:
			return nil, fmt.Errorf("%s has no method last", receiver.TypeName())
		}
	case "push":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.push expects one argument")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method push", receiver.TypeName())
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if len(list.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[0], list.ElementTypes[0]) {
			return nil, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot push %s to list<%s>", args[0].TypeName(), list.ElementTypes[0])}
		}
		list.Elements = append(list.Elements, args[0])
		return list, nil
	case "fill":
		if len(args) != 2 {
			return nil, fmt.Errorf("list.fill expects two arguments (value, count)")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method fill", receiver.TypeName())
		}
		count, err := indexInt(args[1])
		if err != nil {
			return nil, err
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if count < 0 {
			return nil, vmTypedError{class: "ValueError", message: "list.fill count must be >= 0"}
		}
		if count > 0 && len(list.ElementTypes) > 0 && !vmValueSatisfiesElementTag(args[0], list.ElementTypes[0]) {
			return nil, vmTypedError{class: "TypeError", message: fmt.Sprintf("cannot fill list<%s> with %s", list.ElementTypes[0], args[0].TypeName())}
		}
		for i := 0; i < count; i++ {
			list.Elements = append(list.Elements, args[0])
		}
		return list, nil
	case "append":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.append expects one argument")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method append", receiver.TypeName())
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if len(list.ElementTypes) > 0 {
			if !vmValueSatisfiesElementTag(args[0], list.ElementTypes[0]) {
				return nil, vmTypedError{
					class:   "TypeError",
					message: fmt.Sprintf("cannot append %s to list<%s>", args[0].TypeName(), list.ElementTypes[0]),
				}
			}
		}
		list.Elements = append(list.Elements, args[0])
		return runtime.Null{}, nil
	case "extend":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.extend expects one argument")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method extend", receiver.TypeName())
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("list.extend expects a list argument, got %s", args[0].TypeName())
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if len(list.ElementTypes) > 0 {
			for i, el := range other.Elements {
				if !vmValueSatisfiesElementTag(el, list.ElementTypes[0]) {
					return nil, vmTypedError{
						class:   "TypeError",
						message: fmt.Sprintf("cannot extend list<%s> with %s at index %d", list.ElementTypes[0], el.TypeName(), i),
					}
				}
			}
		}
		list.Elements = append(list.Elements, other.Elements...)
		return runtime.Null{}, nil
	case "clear":
		switch v := receiver.(type) {
		case *runtime.List:
			if v.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
			}
			v.Elements = v.Elements[:0]
			return runtime.Null{}, nil
		case runtime.Dict:
			if v.Frozen {
				return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen dict"}
			}
			v.Clear()
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("%s has no method clear", receiver.TypeName())
		}
	case "pop":
		if len(args) != 0 {
			return nil, fmt.Errorf("list.pop expects no arguments")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method pop", receiver.TypeName())
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		if len(list.Elements) > 0 {
			list.Elements = list.Elements[:len(list.Elements)-1]
		}
		return list, nil
	case "removeAt":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.removeAt expects one argument")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method removeAt", receiver.TypeName())
		}
		i, err := indexInt(args[0])
		if err != nil {
			return nil, fmt.Errorf("list.removeAt: %v", err)
		}
		if i < 0 {
			i = len(list.Elements) + i
		}
		if i < 0 || i >= len(list.Elements) {
			return nil, fmt.Errorf("list.removeAt: index out of range")
		}
		if list.Frozen {
			return nil, vmTypedError{class: "ImmutableError", message: "cannot modify frozen list"}
		}
		list.Elements = append(list.Elements[:i], list.Elements[i+1:]...)
		return list, nil
	case "concat":
		if len(args) != 1 {
			return nil, fmt.Errorf("list.concat expects one argument")
		}
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method concat", receiver.TypeName())
		}
		other, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("list.concat expects a list argument")
		}
		newElements := make([]runtime.Value, len(list.Elements)+len(other.Elements))
		copy(newElements, list.Elements)
		copy(newElements[len(list.Elements):], other.Elements)
		return &runtime.List{Elements: newElements}, nil
	case "join":
		list, ok := receiver.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s has no method join", receiver.TypeName())
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("list.join expects one argument (separator)")
		}
		sep, ok2 := args[0].(runtime.String)
		if !ok2 {
			return nil, fmt.Errorf("list.join separator must be a string")
		}
		parts := make([]string, len(list.Elements))
		for i, el := range list.Elements {
			parts[i] = el.Inspect()
		}
		return runtime.String{Value: strings.Join(parts, sep.Value)}, nil
	case "merge":
		dict, ok := receiver.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s has no method merge", receiver.TypeName())
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("dict.merge expects one argument")
		}
		other, ok2 := args[0].(runtime.Dict)
		if !ok2 {
			return nil, fmt.Errorf("dict.merge expects a dict argument")
		}
		merged := runtime.NewDictHint(dict.Len() + other.Len())
		dict.ForEachEntry(func(k string, e runtime.DictEntry) bool { merged.PutEntry(k, e); return true })
		other.ForEachEntry(func(k string, e runtime.DictEntry) bool { merged.PutEntry(k, e); return true })
		return merged, nil
	case "abs":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.abs expects no arguments", receiver.TypeName())
		}
		return native.NumericAbs(receiver)
	case "isZero":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.isZero expects no arguments", receiver.TypeName())
		}
		return numericSignCheck(receiver, func(sign int) bool { return sign == 0 })
	case "isPositive":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.isPositive expects no arguments", receiver.TypeName())
		}
		return numericSignCheck(receiver, func(sign int) bool { return sign > 0 })
	case "isNegative":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.isNegative expects no arguments", receiver.TypeName())
		}
		return numericSignCheck(receiver, func(sign int) bool { return sign < 0 })
	case "isNaN":
		if len(args) != 0 {
			return nil, fmt.Errorf("float.isNaN expects no arguments")
		}
		value, ok := receiver.(runtime.Float)
		if !ok {
			return nil, fmt.Errorf("%s has no method isNaN", receiver.TypeName())
		}
		return runtime.Bool{Value: math.IsNaN(value.Value)}, nil
	case "isInf":
		if len(args) != 0 {
			return nil, fmt.Errorf("float.isInf expects no arguments")
		}
		value, ok := receiver.(runtime.Float)
		if !ok {
			return nil, fmt.Errorf("%s has no method isInf", receiver.TypeName())
		}
		return runtime.Bool{Value: math.IsInf(value.Value, 0)}, nil
	case "isInt":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.isInt expects no arguments", receiver.TypeName())
		}
		switch v := receiver.(type) {
		case runtime.String:
			return runtime.Bool{Value: native.StringIsInt(v.Value)}, nil
		case runtime.Float:
			return runtime.Bool{Value: native.FloatIsInt(v.Value)}, nil
		case runtime.Decimal:
			return runtime.Bool{Value: v.Value.IsInt()}, nil
		}
		return nil, native.UnknownMethodError(receiver.TypeName(), name)
	case "round":
		return native.NumericRoundMethod(receiver, args, native.RoundHalfAwayZero, receiver.TypeName()+".round")
	case "floor":
		return native.NumericRoundMethod(receiver, args, native.RoundFloor, receiver.TypeName()+".floor")
	case "ceil":
		return native.NumericRoundMethod(receiver, args, native.RoundCeil, receiver.TypeName()+".ceil")
	case "truncate":
		return native.NumericRoundMethod(receiver, args, native.RoundTrunc, receiver.TypeName()+".truncate")
	case "sign":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.sign expects no arguments", receiver.TypeName())
		}
		return native.NumericSign(receiver)
	case "clamp":
		if len(args) != 2 {
			return nil, fmt.Errorf("%s.clamp expects two arguments", receiver.TypeName())
		}
		return native.NumericClamp(receiver, args[0], args[1])
	case "isEven", "isOdd":
		if len(args) != 0 {
			return nil, fmt.Errorf("int.%s expects no arguments", name)
		}
		bi, ok := native.IntValueToBigInt(receiver)
		if !ok {
			return nil, native.UnknownMethodError(receiver.TypeName(), name)
		}
		even := bi.Bit(0) == 0
		if name == "isOdd" {
			return runtime.Bool{Value: !even}, nil
		}
		return runtime.Bool{Value: even}, nil
	case "not":
		if len(args) != 0 {
			return nil, fmt.Errorf("bool.not expects no arguments")
		}
		value, ok := receiver.(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("%s has no method not", receiver.TypeName())
		}
		return runtime.Bool{Value: !value.Value}, nil
	case "toString":
		if value, ok := receiver.(runtime.Decimal); ok {
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
		}
		switch receiver.(type) {
		case runtime.SmallInt, runtime.Int:
			base, err := native.IntBaseArg(args, "int.toString")
			if err != nil {
				return nil, err
			}
			if base == 10 {
				return runtime.String{Value: receiver.Inspect()}, nil
			}
			s, err := native.IntFormatBase(receiver, base)
			if err != nil {
				return nil, err
			}
			return runtime.String{Value: s}, nil
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.toString expects no arguments", receiver.TypeName())
		}
		if value, ok := receiver.(runtime.Bytes); ok {
			return native.BytesToUTF8String(value.Value, "bytes.toString")
		}
		return runtime.String{Value: receiver.Inspect()}, nil
	case "toHex":
		if len(args) != 0 {
			return nil, fmt.Errorf("bytes.toHex expects no arguments")
		}
		value, ok := receiver.(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("%s has no method toHex", receiver.TypeName())
		}
		return runtime.String{Value: hex.EncodeToString(value.Value)}, nil
	case "toBase64":
		if len(args) != 0 {
			return nil, fmt.Errorf("bytes.toBase64 expects no arguments")
		}
		value, ok := receiver.(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("%s has no method toBase64", receiver.TypeName())
		}
		return runtime.String{Value: base64.StdEncoding.EncodeToString(value.Value)}, nil
	case "toBase64Url":
		if len(args) != 0 {
			return nil, fmt.Errorf("bytes.toBase64Url expects no arguments")
		}
		value, ok := receiver.(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("%s has no method toBase64Url", receiver.TypeName())
		}
		return runtime.String{Value: base64.RawURLEncoding.EncodeToString(value.Value)}, nil
	default:
		return nil, native.UnknownMethodError(receiver.TypeName(), name)
	}
}

func primitiveLength(receiver runtime.Value) (runtime.Value, error) {
	switch value := receiver.(type) {
	case runtime.String:
		return runtime.SmallInt{Value: int64(runtime.StringRuneInfo(value.Value).RuneCount(value.Value))}, nil
	case runtime.Bytes:
		return runtime.SmallInt{Value: int64(len(value.Value))}, nil
	case *runtime.List:
		return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
	case runtime.Dict:
		return runtime.SmallInt{Value: int64(value.Len())}, nil
	case runtime.Set:
		return runtime.SmallInt{Value: int64(len(value.Elements))}, nil
	case runtime.Range:
		return runtime.Int{Value: value.Length()}, nil
	default:
		return nil, fmt.Errorf("%s has no method length", receiver.TypeName())
	}
}

func singleStringArg(args []runtime.Value, label string) (string, error) {
	value, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s expects string", label)
	}
	return value.Value, nil
}

func formatArgs(args []runtime.Value) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = formatArg(arg)
	}
	return out
}

func formatString(format string, args []runtime.Value) (string, error) {
	formatted := fmt.Sprintf(format, formatArgs(args)...)
	if strings.Contains(formatted, "%!") {
		return "", fmt.Errorf("invalid string.format arguments for %q", format)
	}
	return formatted, nil
}

func decimalFormatScale(value runtime.Value) (int, error) {
	scale, err := indexInt(value)
	if err != nil {
		return 0, fmt.Errorf("decimal scale must be int")
	}
	if scale < 0 || scale > 10000 {
		return 0, fmt.Errorf("decimal scale must be between 0 and 10000")
	}
	return scale, nil
}

func formatArg(value runtime.Value) any {
	switch value := value.(type) {
	case runtime.Null:
		return nil
	case runtime.Bool:
		return value.Value
	case runtime.SmallInt:
		return value.Value
	case runtime.Int:
		if value.Value.IsInt64() {
			return value.Value.Int64()
		}
		return value.Value.String()
	case runtime.Decimal:
		f, _ := value.Value.Float64()
		return f
	case runtime.Float:
		return value.Value
	case runtime.String:
		return value.Value
	case runtime.Bytes:
		return value.Value
	default:
		return value.Inspect()
	}
}

func numericSignCheck(value runtime.Value, check func(int) bool) (runtime.Value, error) {
	switch value := value.(type) {
	case runtime.SmallInt:
		sign := 0
		if value.Value > 0 {
			sign = 1
		} else if value.Value < 0 {
			sign = -1
		}
		return runtime.Bool{Value: check(sign)}, nil
	case runtime.Int:
		return runtime.Bool{Value: check(value.Value.Sign())}, nil
	case runtime.Decimal:
		return runtime.Bool{Value: check(value.Value.Sign())}, nil
	case runtime.Float:
		sign := 0
		if value.Value > 0 {
			sign = 1
		} else if value.Value < 0 {
			sign = -1
		}
		return runtime.Bool{Value: check(sign)}, nil
	default:
		return nil, fmt.Errorf("%s has no numeric sign methods", value.TypeName())
	}
}

func valuesEqual(left runtime.Value, right runtime.Value) bool {
	if eq, both := runtime.NumericValuesEqual(left, right); both {
		return eq
	}
	switch leftValue := left.(type) {
	case *runtime.List:
		rightValue, ok := right.(*runtime.List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for i, element := range leftValue.Elements {
			if !valuesEqual(element, rightValue.Elements[i]) {
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
			if !ok || !valuesEqual(entry.Key, other.Key) || !valuesEqual(entry.Value, other.Value) {
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
			if !ok || !valuesEqual(entry.Value, other.Value) {
				return false
			}
		}
		return true
	case runtime.EnumVariant:
		rv, ok := right.(runtime.EnumVariant)
		if !ok || leftValue.Enum != rv.Enum || leftValue.Variant != rv.Variant || len(leftValue.Fields) != len(rv.Fields) {
			return false
		}
		for i, f := range leftValue.Fields {
			if !valuesEqual(f, rv.Fields[i]) {
				return false
			}
		}
		return true
	case *runtime.Instance:
		rightValue, ok := right.(*runtime.Instance)
		if !ok || !strings.EqualFold(leftValue.Class.Name, rightValue.Class.Name) || len(leftValue.Fields) != len(rightValue.Fields) {
			return false
		}
		for name, value := range leftValue.Fields {
			other, ok := rightValue.Fields[name]
			if !ok || !valuesEqual(value, other) {
				return false
			}
		}
		return true
	}
	return primitiveEqual(left, right)
}

func cloneSetEntries(elements map[string]runtime.SetEntry) map[string]runtime.SetEntry {
	out := make(map[string]runtime.SetEntry, len(elements))
	for key, entry := range elements {
		out[key] = entry
	}
	return out
}

func orderedSetValues(value runtime.Set) []runtime.Value {
	keys := make([]string, 0, len(value.Elements))
	for key := range value.Elements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]runtime.Value, 0, len(keys))
	for _, key := range keys {
		values = append(values, value.Elements[key].Value)
	}
	return values
}

func vmHTTPHeadersMethod(receiver runtime.HTTPHeaders, name string, args []runtime.Value) (runtime.Value, error) {
	headers := vmCopyHTTPHeaders(receiver)
	switch name {
	case "get":
		key, err := vmSingleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		values := headers.Values[key]
		if len(values) == 0 {
			return runtime.Null{}, nil
		}
		return runtime.String{Value: values[0]}, nil
	case "getAll":
		key, err := vmSingleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		values := headers.Values[key]
		elements := make([]runtime.Value, len(values))
		for i, value := range values {
			elements[i] = runtime.String{Value: value}
		}
		return &runtime.List{Elements: elements}, nil
	case "has":
		key, err := vmSingleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: len(headers.Values[key]) > 0}, nil
	case "set":
		key, value, err := vmHeaderNameValue(name, args)
		if err != nil {
			return nil, err
		}
		headers.Values[key] = []string{value}
		return headers, nil
	case "add":
		key, value, err := vmHeaderNameValue(name, args)
		if err != nil {
			return nil, err
		}
		headers.Values[key] = append(headers.Values[key], value)
		return headers, nil
	case "delete":
		key, err := vmSingleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		delete(headers.Values, key)
		return headers, nil
	case "keys":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Headers.keys expects no arguments")
		}
		keys := make([]string, 0, len(headers.Values))
		for key := range headers.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		elements := make([]runtime.Value, len(keys))
		for i, key := range keys {
			elements[i] = runtime.String{Value: key}
		}
		return &runtime.List{Elements: elements}, nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Headers.toDict expects no arguments")
		}
		return vmHTTPHeadersToDict(headers), nil
	default:
		return nil, fmt.Errorf("http.Headers has no method %s", name)
	}
}

func vmCopyHTTPHeaders(headers runtime.HTTPHeaders) runtime.HTTPHeaders {
	out := runtime.HTTPHeaders{Values: map[string][]string{}}
	for key, values := range headers.Values {
		out.Values[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return out
}

func vmHTTPHeadersToDict(headers runtime.HTTPHeaders) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	for key, values := range headers.Values {
		keyValue := runtime.String{Value: http.CanonicalHeaderKey(key)}
		var value runtime.Value
		if len(values) == 1 {
			value = runtime.String{Value: values[0]}
		} else {
			elements := make([]runtime.Value, len(values))
			for i, item := range values {
				elements[i] = runtime.String{Value: item}
			}
			value = &runtime.List{Elements: elements}
		}
		entries[native.DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	return runtime.Dict{Entries: entries}
}

func vmSingleHeaderName(method string, args []runtime.Value) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("http.Headers.%s expects name", method)
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("http.Headers.%s name must be string", method)
	}
	return http.CanonicalHeaderKey(name.Value), nil
}

func vmHeaderNameValue(method string, args []runtime.Value) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("http.Headers.%s expects name and value", method)
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("http.Headers.%s name must be string", method)
	}
	value, ok := args[1].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("http.Headers.%s value must be string", method)
	}
	return http.CanonicalHeaderKey(name.Value), value.Value, nil
}

func valuesIdentical(left runtime.Value, right runtime.Value) bool {
	switch leftValue := left.(type) {
	case *runtime.Instance:
		rightValue, ok := right.(*runtime.Instance)
		return ok && leftValue == rightValue
	case *runtime.Class:
		rightValue, ok := right.(*runtime.Class)
		return ok && leftValue == rightValue
	case runtime.Null:
		_, ok := right.(runtime.Null)
		return ok
	default:
		return primitiveEqual(left, right)
	}
}

func primitiveEqual(left runtime.Value, right runtime.Value) bool {
	switch leftValue := left.(type) {
	case runtime.Set:
		rightValue, ok := right.(runtime.Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok || !primitiveEqual(entry.Value, other.Value) {
				return false
			}
		}
		return true
	case *runtime.Instance:
		rightValue, ok := right.(*runtime.Instance)
		return ok && leftValue == rightValue
	}
	eq, _ := runtime.LeafValuesEqual(left, right)
	return eq
}

func (vm *VM) errorsIs(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("errors.is expects two arguments")
	}
	err, ok := args[0].(runtime.Error)
	if !ok {
		return nil, fmt.Errorf("errors.is: first argument must be an error, got %s", args[0].TypeName())
	}
	target, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("errors.is: second argument must be a string class name")
	}
	return runtime.Bool{Value: vm.errorValueMatches(err, target.Value)}, nil
}

func (vm *VM) errorTypeMatches(class string, target string) bool {
	// Strip an optional module prefix - `catch (errors.HttpException e)`
	// or `instanceof errors.HttpException` matches the bare class name
	// the parent chain records.
	target = stripModulePrefix(target)
	if target == "" || target == "Error" {
		// FatalError is its own tier, not an Error.
		return class != "FatalError"
	}
	seen := map[string]bool{}
	for current := class; current != ""; current = vm.errorParent(current) {
		key := strings.ToLower(current)
		if seen[key] {
			return false
		}
		seen[key] = true
		if strings.EqualFold(stripModulePrefix(current), target) {
			return true
		}
	}
	return false
}

// errorValueMatches is the value-aware variant: when the runtime.Error
// carries a Parents chain (populated at construction for error-derived
// classes) the chain takes precedence over vm.errorParent's chunk-local
// walk, so cross-module catch and `instanceof Parent` resolve correctly.
func (vm *VM) errorValueMatches(err runtime.Error, target string) bool {
	target = stripModulePrefix(target)
	if target == "" || target == "Error" {
		// FatalError is its own tier, not an Error.
		return !err.IsFatal()
	}
	if strings.EqualFold(stripModulePrefix(err.Class), target) {
		return true
	}
	for _, ancestor := range err.Parents {
		if strings.EqualFold(stripModulePrefix(ancestor), target) {
			return true
		}
	}
	// Fall back to the chunk-local walk for built-in error classes
	// whose parent chain isn't recorded in Parents.
	return vm.errorTypeMatches(err.Class, target)
}

func (vm *VM) errorParent(class string) string {
	for _, info := range vm.chunk.Classes {
		if !strings.EqualFold(info.Name, class) {
			continue
		}
		if info.ParentIndex >= 0 && int(info.ParentIndex) < len(vm.chunk.Classes) {
			return vm.chunk.Classes[info.ParentIndex].Name
		}
		return info.ParentName
	}
	switch class {
	case "TimeoutError", "TlsError":
		return "IOError"
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError":
		return "Error"
	default:
		return ""
	}
}

func (vm *VM) classExtendsBuiltinError(classInfo ClassInfo) bool {
	for {
		if isBuiltinErrorClass(classInfo.ParentName) {
			return true
		}
		if classInfo.ParentIndex < 0 || int(classInfo.ParentIndex) >= len(vm.chunk.Classes) {
			return false
		}
		classInfo = vm.chunk.Classes[classInfo.ParentIndex]
	}
}
