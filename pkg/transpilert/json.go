package transpilert

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// JSON encoding/decoding matched byte-for-byte to the interpreter's json
// module: object keys sorted alphabetically, no spaces, decimals rendered to
// 10 fractional places, floats via shortest 'g', bytes as base64, big ints
// outside int64 range quoted. Decode classifies fractional/exponent numbers as
// decimal (*big.Rat) and integers as int64/*big.Int so a parse-then-Show round
// trip prints identically to the interpreter.

// JSONStringify encodes v as the interpreter's json.stringify does.
func JSONStringify(v any) string {
	var b strings.Builder
	encodeJSON(&b, v)
	return b.String()
}

func encodeJSON(b *strings.Builder, v any) {
	if v == nil {
		b.WriteString("null")
		return
	}
	switch x := v.(type) {
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		encodeJSONString(b, x)
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case int:
		b.WriteString(strconv.Itoa(x))
	case *big.Int:
		if x.IsInt64() {
			b.WriteString(x.String())
		} else {
			b.WriteByte('"')
			b.WriteString(x.String())
			b.WriteByte('"')
		}
	case Int:
		if x.Big != nil {
			encodeJSON(b, x.Big)
		} else {
			b.WriteString(strconv.FormatInt(x.I64, 10))
		}
	case *big.Rat:
		b.WriteString(DecimalToString(x))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			panic(&Error{Class: "RuntimeError", Message: fmt.Sprintf("json: unsupported value: %g", x), Parents: []string{"Error"}})
		}
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case []byte:
		b.WriteByte('"')
		b.WriteString(base64.StdEncoding.EncodeToString(x))
		b.WriteByte('"')
	default:
		encodeJSONReflect(b, reflect.ValueOf(v))
	}
}

func encodeJSONReflect(b *strings.Builder, rv reflect.Value) {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			b.WriteString("null")
			return
		}
		if encodeOrderedDict(b, rv) {
			return
		}
		encodeJSON(b, rv.Elem().Interface())
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b.WriteByte('"')
			b.WriteString(base64.StdEncoding.EncodeToString(rv.Bytes()))
			b.WriteByte('"')
			return
		}
		b.WriteByte('[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			encodeJSON(b, rv.Index(i).Interface())
		}
		b.WriteByte(']')
	case reflect.String:
		encodeJSONString(b, rv.String())
	case reflect.Bool:
		encodeJSON(b, rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(rv.Int(), 10))
	case reflect.Float32, reflect.Float64:
		encodeJSON(b, rv.Float())
	default:
		panic(&Error{Class: "RuntimeError", Message: "json.stringify does not support this value", Parents: []string{"Error"}})
	}
}

// encodeOrderedDict emits an *OrderedDict's entries with string keys sorted
// alphabetically; reports false if rv is not an ordered dict.
func encodeOrderedDict(b *strings.Builder, rv reflect.Value) bool {
	if !strings.HasPrefix(rv.Type().Elem().Name(), "OrderedDict[") {
		return false
	}
	keysM := rv.MethodByName("Keys")
	valsM := rv.MethodByName("Values")
	if !keysM.IsValid() || !valsM.IsValid() {
		return false
	}
	keys := keysM.Call(nil)[0]
	vals := valsM.Call(nil)[0]
	type pair struct {
		k string
		v any
	}
	pairs := make([]pair, keys.Len())
	for i := 0; i < keys.Len(); i++ {
		ks, ok := keys.Index(i).Interface().(string)
		if !ok {
			panic(&Error{Class: "RuntimeError", Message: "json.stringify only supports dicts with string keys", Parents: []string{"Error"}})
		}
		pairs[i] = pair{ks, vals.Index(i).Interface()}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		encodeJSONString(b, p.k)
		b.WriteByte(':')
		encodeJSON(b, p.v)
	}
	b.WriteByte('}')
	return true
}

func encodeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		if start < i {
			b.WriteString(s[start:i])
		}
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			const hex = "0123456789abcdef"
			b.WriteString(`\u00`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
		start = i + 1
	}
	if start < len(s) {
		b.WriteString(s[start:])
	}
	b.WriteByte('"')
}

// JSONValidate reports whether text is well-formed JSON.
func JSONValidate(text string) bool {
	p := &jsonDecoder{src: text}
	p.skipWS()
	if _, err := p.value(); err != nil {
		return false
	}
	p.skipWS()
	return p.pos == len(p.src)
}

// JSONParse decodes text into native Go values (matching the interpreter's
// runtime types): object -> *OrderedDict[string, any], array -> []any, integer
// -> int64 / *big.Int, fractional/exponent number -> *big.Rat (decimal),
// string -> string, bool -> bool, null -> nil. Panics a RuntimeError on
// malformed input.
func JSONParse(text string) any {
	p := &jsonDecoder{src: text}
	p.skipWS()
	v, err := p.value()
	if err != nil {
		panic(jsonParseError(err))
	}
	p.skipWS()
	if p.pos != len(p.src) {
		panic(jsonParseError(fmt.Errorf("unexpected trailing characters")))
	}
	return v
}

func jsonParseError(err error) *Error {
	return &Error{Class: "RuntimeError", Message: err.Error(), Parents: []string{"Error"}}
}

type jsonDecoder struct {
	src string
	pos int
}

func (p *jsonDecoder) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonDecoder) value() (any, error) {
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == 't' || c == 'f':
		return p.boolean()
	case c == 'n':
		return p.null()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return nil, fmt.Errorf("unexpected character %q", c)
	}
}

func (p *jsonDecoder) object() (any, error) {
	p.pos++ // {
	d := NewOrderedDict[string, any]()
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return d, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return nil, fmt.Errorf("expected string key in object")
		}
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return nil, fmt.Errorf("expected ':' after object key")
		}
		p.pos++
		p.skipWS()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		d.Set(key.(string), v)
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated object")
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return d, nil
		}
		return nil, fmt.Errorf("expected ',' or '}' in object")
	}
}

func (p *jsonDecoder) array() (any, error) {
	p.pos++ // [
	out := []any{}
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return out, nil
	}
	for {
		p.skipWS()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated array")
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == ']' {
			p.pos++
			return out, nil
		}
		return nil, fmt.Errorf("expected ',' or ']' in array")
	}
}

func (p *jsonDecoder) str() (any, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.src) {
				return nil, fmt.Errorf("unterminated escape")
			}
			esc := p.src[p.pos]
			p.pos++
			switch esc {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '/':
				b.WriteByte('/')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, err := p.unicodeEscape()
				if err != nil {
					return nil, err
				}
				b.WriteRune(r)
			default:
				return nil, fmt.Errorf("invalid escape \\%c", esc)
			}
			continue
		}
		if c < 0x20 {
			return nil, fmt.Errorf("invalid control character in string")
		}
		b.WriteByte(c)
		p.pos++
	}
	return nil, fmt.Errorf("unterminated string")
}

func (p *jsonDecoder) unicodeEscape() (rune, error) {
	if p.pos+4 > len(p.src) {
		return 0, fmt.Errorf("incomplete \\u escape")
	}
	hex := p.src[p.pos : p.pos+4]
	p.pos += 4
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid \\u escape %q", hex)
	}
	return rune(n), nil
}

func (p *jsonDecoder) boolean() (any, error) {
	if strings.HasPrefix(p.src[p.pos:], "true") {
		p.pos += 4
		return true, nil
	}
	if strings.HasPrefix(p.src[p.pos:], "false") {
		p.pos += 5
		return false, nil
	}
	return nil, fmt.Errorf("expected boolean literal")
}

func (p *jsonDecoder) null() (any, error) {
	if strings.HasPrefix(p.src[p.pos:], "null") {
		p.pos += 4
		return nil, nil
	}
	return nil, fmt.Errorf("expected null literal")
}

func (p *jsonDecoder) number() (any, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	isFloat := false
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		isFloat = true
		p.pos++
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		isFloat = true
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	}
	text := p.src[start:p.pos]
	if isFloat {
		r, ok := new(big.Rat).SetString(text)
		if !ok {
			return nil, fmt.Errorf("invalid number literal %q", text)
		}
		return r, nil
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		return v, nil
	}
	bi, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer literal %q", text)
	}
	return bi, nil
}
