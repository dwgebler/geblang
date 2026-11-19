package transpilert

import (
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// GbStringer is the exported hook a transpiled class with a __string dunder
// implements (reflection cannot reach the unexported __string method), so Show
// renders such instances via the dunder at top level like the interpreter.
type GbStringer interface{ GbString() string }

// Show renders a value as the interpreter prints it at top level (io.print /
// io.println and string interpolation): bare strings, instance __string when
// present. Containers render their elements in the nested form (strings
// quoted). It matches the engine's displayString/Inspect byte for byte.
func Show(v any) string {
	return render(v, false)
}

// render formats v the Geblang way. nested controls string quoting: top-level
// strings print bare, strings inside a container print JSON-quoted, matching
// the interpreter's inspectInsideContainer.
func render(v any, nested bool) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		if nested {
			return quoteString(x)
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return fmt.Sprintf("%g", x)
	case Int:
		return x.show()
	case *big.Rat:
		// decimal renders to 10 fractional places, matching Decimal.Inspect.
		return DecimalToString(x)
	case *big.Int:
		return x.String()
	case []byte:
		return hexEncode(x)
	}
	return renderReflect(reflect.ValueOf(v), nested)
}

func (a Int) show() string {
	if a.Big != nil {
		return a.Big.String()
	}
	return strconv.FormatInt(a.I64, 10)
}

func renderReflect(rv reflect.Value, nested bool) string {
	// Enums lower to a named scalar with a generated String() (Name.Variant
	// form); the interpreter renders them identically at any nesting. Checked
	// here, after render's explicit type switch handles primitives and *big.Rat
	// (whose String() is the rational form, not the decimal one).
	if rv.Kind() != reflect.Pointer && rv.Kind() != reflect.Interface {
		if s, ok := stringerValue(rv); ok {
			return s
		}
	}
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return "null"
		}
		if s, ok := instanceString(rv, nested); ok {
			return s
		}
		return renderReflect(rv.Elem(), nested)
	case reflect.Interface:
		if rv.IsNil() {
			return "null"
		}
		return render(rv.Interface(), nested)
	case reflect.Slice, reflect.Array:
		parts := make([]string, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			parts[i] = render(rv.Index(i).Interface(), true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Map:
		// A set is map[T]struct{}; the interpreter renders it sorted as set{...}.
		if rv.Type().Elem().Kind() == reflect.Struct && rv.Type().Elem().NumField() == 0 {
			parts := make([]string, 0, rv.Len())
			for _, k := range rv.MapKeys() {
				parts = append(parts, render(k.Interface(), true))
			}
			sort.Strings(parts)
			return "set{" + strings.Join(parts, ", ") + "}"
		}
		parts := make([]string, 0, rv.Len())
		for _, k := range rv.MapKeys() {
			parts = append(parts, render(k.Interface(), true)+": "+render(rv.MapIndex(k).Interface(), true))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case reflect.Struct:
		if od, ok := orderedDictParts(rv); ok {
			return od
		}
		return "<" + rv.Type().Name() + ">"
	case reflect.String:
		if nested {
			return quoteString(rv.String())
		}
		return rv.String()
	case reflect.Bool:
		if rv.Bool() {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", rv.Float())
	}
	return fmt.Sprintf("%v", rv.Interface())
}

// instanceString renders a class instance: __string() at top level (matching
// the interpreter's dunder dispatch), else the bare <ClassName> form. Nested
// instances never invoke __string, matching inspectInsideContainer.
func instanceString(rv reflect.Value, nested bool) (string, bool) {
	elem := rv.Type().Elem()
	if elem.Kind() != reflect.Struct {
		return "", false
	}
	if orderedDictPointer(elem) {
		return "", false
	}
	if !nested {
		if s, ok := rv.Interface().(GbStringer); ok {
			return s.GbString(), true
		}
	}
	return "<" + elem.Name() + ">", true
}

func orderedDictPointer(t reflect.Type) bool {
	return strings.HasPrefix(t.Name(), "OrderedDict[")
}

// orderedDictParts renders an OrderedDict in insertion order via reflection over
// its Keys/Values methods so Show needs no type parameters.
func orderedDictParts(rv reflect.Value) (string, bool) {
	if !strings.HasPrefix(rv.Type().Name(), "OrderedDict[") {
		return "", false
	}
	addr := rv
	if rv.CanAddr() {
		addr = rv.Addr()
	} else {
		p := reflect.New(rv.Type())
		p.Elem().Set(rv)
		addr = p
	}
	keysM := addr.MethodByName("Keys")
	valsM := addr.MethodByName("Values")
	if !keysM.IsValid() || !valsM.IsValid() {
		return "", false
	}
	keys := keysM.Call(nil)[0]
	vals := valsM.Call(nil)[0]
	parts := make([]string, keys.Len())
	for i := 0; i < keys.Len(); i++ {
		parts[i] = render(keys.Index(i).Interface(), true) + ": " + render(vals.Index(i).Interface(), true)
	}
	return "{" + strings.Join(parts, ", ") + "}", true
}

func stringerValue(rv reflect.Value) (string, bool) {
	m := rv.MethodByName("String")
	if !m.IsValid() || m.Type().NumIn() != 0 || m.Type().NumOut() != 1 || m.Type().Out(0).Kind() != reflect.String {
		return "", false
	}
	return m.Call(nil)[0].String(), true
}

// quoteString matches the interpreter's jsonQuoteString: JSON-style escapes with
// control characters as \uXXXX.
func quoteString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

const hexDigits = "0123456789abcdef"

func hexEncode(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexDigits[c>>4]
		out[i*2+1] = hexDigits[c&0x0f]
	}
	return string(out)
}
