package native

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"geblang/internal/runtime"
)

// ParseArgv parses a list of argument strings against a schema dict.
//
// Schema entry keys are flag names; each entry is a dict with:
//
//	"type":     "bool" | "string" | "int" | "float"  (default "string")
//	"short":    single-character alias, e.g. "v"
//	"default":  default value
//	"help":     description text
//	"required": bool — error if the flag is absent and has no default
//
// Result dict keys: flag names with their parsed values, "_" for positional
// args (list of strings), and "error" (null or an error message string).
func ParseArgv(argv *runtime.List, schema runtime.Dict) runtime.Dict {
	return ParseArgvValidated(argv, schema, ValidateCLIValue)
}

func ParseArgvValidated(argv *runtime.List, schema runtime.Dict, validate func(runtime.Value, runtime.Dict) (string, error)) runtime.Dict {
	longToName := map[string]string{}
	shortToName := map[string]string{}
	typeOf := map[string]string{}

	for _, dk := range schema.EntryKeys() {
		entry, _ := schema.GetEntry(dk)
		name, ok := entry.Key.(runtime.String)
		if !ok {
			continue
		}
		spec, ok := entry.Value.(runtime.Dict)
		if !ok {
			continue
		}
		longToName[name.Value] = name.Value
		if short := dictString(spec, "short"); len(short) == 1 {
			shortToName[short] = name.Value
		}
		if t := dictString(spec, "type"); t != "" {
			typeOf[name.Value] = t
		} else {
			typeOf[name.Value] = "string"
		}
	}

	result := map[string]runtime.Value{}
	for _, dk := range schema.EntryKeys() {
		entry, _ := schema.GetEntry(dk)
		name, ok := entry.Key.(runtime.String)
		if !ok {
			continue
		}
		spec, ok := entry.Value.(runtime.Dict)
		if !ok {
			continue
		}
		if defaultVal, ok := dictLookup(spec, "default"); ok {
			result[name.Value] = defaultVal
		} else {
			switch typeOf[name.Value] {
			case "bool":
				result[name.Value] = runtime.Bool{Value: false}
			case "int":
				result[name.Value] = runtime.NewInt64(0)
			case "float":
				result[name.Value] = runtime.Float{Value: 0}
			default:
				result[name.Value] = runtime.String{Value: ""}
			}
		}
	}

	positional := []runtime.Value{}
	var parseErr string
	provided := map[string]bool{}
	elems := argv.Elements
	i := 0
	for i < len(elems) {
		arg, ok := elems[i].(runtime.String)
		if !ok {
			i++
			continue
		}
		s := arg.Value

		if s == "--" {
			i++
			for i < len(elems) {
				positional = append(positional, elems[i])
				i++
			}
			break
		}

		var flagName string
		var found bool
		var inlineVal string
		hasInline := false

		if strings.HasPrefix(s, "--") {
			key := s[2:]
			if idx := strings.IndexByte(key, '='); idx >= 0 {
				inlineVal = key[idx+1:]
				hasInline = true
				key = key[:idx]
			}
			flagName, found = longToName[key]
		} else if strings.HasPrefix(s, "-") && len(s) == 2 {
			flagName, found = shortToName[s[1:]]
		} else {
			positional = append(positional, elems[i])
			i++
			continue
		}

		if !found {
			if parseErr == "" {
				parseErr = fmt.Sprintf("unknown option: %s", s)
			}
			i++
			continue
		}

		provided[flagName] = true

		if typeOf[flagName] == "bool" {
			if hasInline {
				result[flagName] = runtime.Bool{Value: inlineVal != "false" && inlineVal != "0"}
			} else {
				result[flagName] = runtime.Bool{Value: true}
			}
			i++
			continue
		}

		if hasInline {
			v, err := coerceArgValue(inlineVal, typeOf[flagName])
			if err != nil && parseErr == "" {
				parseErr = fmt.Sprintf("option --%s: %v", flagName, err)
			}
			result[flagName] = v
			i++
			continue
		}

		i++
		if i >= len(elems) {
			if parseErr == "" {
				parseErr = fmt.Sprintf("option %s requires a value", s)
			}
			continue
		}
		valArg, ok := elems[i].(runtime.String)
		if !ok {
			if parseErr == "" {
				parseErr = fmt.Sprintf("option %s value must be a string", s)
			}
			i++
			continue
		}
		v, err := coerceArgValue(valArg.Value, typeOf[flagName])
		if err != nil && parseErr == "" {
			parseErr = fmt.Sprintf("option %s: %v", s, err)
		}
		result[flagName] = v
		i++
	}

	if parseErr == "" {
		for _, dk := range schema.EntryKeys() {
			entry, _ := schema.GetEntry(dk)
			name, ok := entry.Key.(runtime.String)
			if !ok {
				continue
			}
			spec, ok := entry.Value.(runtime.Dict)
			if !ok {
				continue
			}
			req, _ := dictLookup(spec, "required")
			if b, ok := req.(runtime.Bool); ok && b.Value && !provided[name.Value] {
				if _, hasDefault := dictLookup(spec, "default"); !hasDefault {
					parseErr = fmt.Sprintf("required option --%s is missing", name.Value)
					break
				}
			}
		}
	}

	if parseErr == "" && validate != nil {
		for _, key := range schema.EntryKeys() {
			entry, _ := schema.GetEntry(key)
			name, ok := entry.Key.(runtime.String)
			if !ok {
				continue
			}
			spec, ok := entry.Value.(runtime.Dict)
			if !ok {
				continue
			}
			_, hasDefault := dictLookup(spec, "default")
			if !provided[name.Value] && !hasDefault {
				continue
			}
			message, err := validate(result[name.Value], spec)
			if err != nil {
				message = err.Error()
			}
			if message != "" {
				parseErr = fmt.Sprintf("option --%s: %s", name.Value, message)
				break
			}
		}
	}

	entries := map[string]runtime.DictEntry{}
	for k, v := range result {
		kv := runtime.String{Value: k}
		entries[DictKey(kv)] = runtime.DictEntry{Key: kv, Value: v}
	}
	posKey := runtime.String{Value: "_"}
	entries[DictKey(posKey)] = runtime.DictEntry{Key: posKey, Value: &runtime.List{Elements: positional}}
	errKey := runtime.String{Value: "error"}
	var errVal runtime.Value
	if parseErr != "" {
		errVal = runtime.String{Value: parseErr}
	} else {
		errVal = runtime.Null{}
	}
	entries[DictKey(errKey)] = runtime.DictEntry{Key: errKey, Value: errVal}
	return runtime.Dict{Entries: entries}
}

// HelpText generates a usage string for a program given its name and schema.
func HelpText(name string, schema runtime.Dict) string {
	if name == "" {
		name = "program"
	}
	var sb strings.Builder
	sb.WriteString("Usage: ")
	sb.WriteString(name)
	sb.WriteString(" [options] [args...]\n\nOptions:\n")

	for _, dk := range schema.EntryKeys() {
		entry, _ := schema.GetEntry(dk)
		flagName, ok := entry.Key.(runtime.String)
		if !ok {
			continue
		}
		spec, ok := entry.Value.(runtime.Dict)
		if !ok {
			continue
		}
		typ := dictString(spec, "type")
		if typ == "" {
			typ = "string"
		}
		short := dictString(spec, "short")
		help := dictString(spec, "help")

		var left string
		if short != "" {
			left = fmt.Sprintf("  -%s, --%s", short, flagName.Value)
		} else {
			left = fmt.Sprintf("      --%s", flagName.Value)
		}
		if typ != "bool" {
			left += " <" + typ + ">"
		}

		defVal := ""
		if dv, ok := dictLookup(spec, "default"); ok {
			defVal = " (default: " + argValueString(dv) + ")"
		}

		sb.WriteString(fmt.Sprintf("%-28s  %s%s\n", left, help, defVal))
	}
	return sb.String()
}

func coerceArgValue(text, typ string) (runtime.Value, error) {
	switch typ {
	case "int":
		n := new(big.Int)
		if _, ok := n.SetString(strings.TrimSpace(text), 10); !ok {
			return runtime.NewInt64(0), fmt.Errorf("expected integer, got %q", text)
		}
		return runtime.Int{Value: n}, nil
	case "float":
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return runtime.Float{Value: 0}, fmt.Errorf("expected float, got %q", text)
		}
		return runtime.Float{Value: f}, nil
	default:
		return runtime.String{Value: text}, nil
	}
}

func argValueString(v runtime.Value) string {
	switch v := v.(type) {
	case runtime.String:
		return v.Value
	case runtime.Bool:
		if v.Value {
			return "true"
		}
		return "false"
	case runtime.SmallInt:
		return strconv.FormatInt(v.Value, 10)
	case runtime.Int:
		return v.Value.String()
	case runtime.Float:
		return strconv.FormatFloat(v.Value, 'g', -1, 64)
	default:
		return v.Inspect()
	}
}
