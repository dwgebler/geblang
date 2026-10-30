package native

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"geblang/internal/runtime"
)

// smallIntInterfaceCache holds pre-boxed runtime.Value wrappers for
// small ints. Each SmallInt -> runtime.Value conversion is a heap
// allocation; returning a cached interface skips the alloc.
const (
	smallIntCacheMin = -128
	smallIntCacheMax = 1024
)

var smallIntInterfaceCache [smallIntCacheMax - smallIntCacheMin]runtime.Value

func init() {
	for i := range smallIntInterfaceCache {
		smallIntInterfaceCache[i] = runtime.SmallInt{Value: int64(i + smallIntCacheMin)}
	}
}

// parseJSONDirect walks the input bytes once and emits runtime.Value
// without the json.Decoder + any -> runtime.Value double pass that
// ParseJSONText previously paid for. Numeric semantics mirror the
// prior path: integers fitting int64 become runtime.SmallInt, larger
// integers become runtime.Int, anything with `.` / `e` / `E` becomes
// runtime.Decimal.
func parseJSONDirect(text string) (runtime.Value, *ParseError) {
	p := jsonParser{src: text, pos: 0}
	p.skipWhitespace()
	value, err := p.parseValue()
	if err != nil {
		pe := NewParseError(err.Error(), text, int64(p.pos+1))
		return nil, &pe
	}
	p.skipWhitespace()
	if p.pos != len(text) {
		pe := NewParseError("unexpected trailing content", text, int64(p.pos+1))
		return nil, &pe
	}
	return value, nil
}

// JSON syntax validation runs first, so escaped bytes can be skipped here.
func jsonDepthWithinLimit(text string) bool {
	if len(text) <= maxJSONDepth*2 {
		return true
	}
	depth := 0
	quoted := false
	for i := 0; i < len(text); i++ {
		if quoted {
			if text[i] == '\\' {
				i++
			} else if text[i] == '"' {
				quoted = false
			}
			continue
		}
		switch text[i] {
		case '"':
			quoted = true
		case '[', '{':
			depth++
			if depth > maxJSONDepth {
				return false
			}
		case ']', '}':
			depth--
		}
	}
	return true
}

type jsonParser struct {
	src   string
	pos   int
	depth int
	// keyCache memoises the dict-key form of object keys so repeated
	// keys (typical JSON: every record shares "id", "name", etc.)
	// reuse a single concatenated string instead of paying for the
	// "s"+key allocation on every entry. Bounded so pathological
	// inputs with thousands of unique keys don't grow the cache
	// unboundedly.
	keyCache map[string]jsonKeyEntry
}

// jsonKeyEntry interns both the dict map-key and the boxed runtime.Value
// key for a JSON object key, so repeated keys skip the interface-boxing
// alloc (not just the "s"+key alloc) on every entry.
type jsonKeyEntry struct {
	mapKey string
	keyVal runtime.Value
}

// jsonKeyCacheLimit caps the per-parse key intern table. The cache
// is per-jsonParser so each json.parse call gets a fresh table; the
// limit just prevents bad inputs from filling memory.
const jsonKeyCacheLimit = 256

func (p *jsonParser) parseValue() (runtime.Value, error) {
	if p.pos < len(p.src) && (p.src[p.pos] == '{' || p.src[p.pos] == '[') {
		if p.depth >= maxJSONDepth {
			return nil, fmt.Errorf("json: maximum nesting depth exceeded")
		}
		p.depth++
		defer func() { p.depth-- }()
	}
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: s}, nil
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, fmt.Errorf("unexpected character %q", c)
	}
}

func (p *jsonParser) parseObject() (runtime.Value, error) {
	p.pos++ // skip {
	var pending [8]struct {
		key   string
		entry runtime.DictEntry
	}
	count := 0
	d := runtime.Dict{}
	finish := func() runtime.Dict {
		if count <= len(pending) {
			result := runtime.NewDictHint(count)
			for _, item := range pending[:count] {
				result.PutEntry(item.key, item.entry)
			}
			return result
		}
		return d
	}
	p.skipWhitespace()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return finish(), nil
	}
	for {
		p.skipWhitespace()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return nil, fmt.Errorf("expected string key in object")
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return nil, fmt.Errorf("expected ':' after object key")
		}
		p.pos++
		p.skipWhitespace()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		entry, cached := p.keyCache[key]
		if !cached {
			entry = jsonKeyEntry{mapKey: dictKeyPrefixString + key, keyVal: runtime.String{Value: key}}
			if p.keyCache == nil {
				p.keyCache = make(map[string]jsonKeyEntry, 16)
			}
			if len(p.keyCache) < jsonKeyCacheLimit {
				p.keyCache[key] = entry
			}
		}
		item := runtime.DictEntry{Key: entry.keyVal, Value: value}
		if count < len(pending) {
			pending[count].key = entry.mapKey
			pending[count].entry = item
		} else {
			if count == len(pending) {
				d = runtime.NewDictHint(16)
				for _, prev := range pending {
					d.PutEntry(prev.key, prev.entry)
				}
			}
			d.PutEntry(entry.mapKey, item)
		}
		count++
		p.skipWhitespace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unexpected end of object")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case '}':
			p.pos++
			return finish(), nil
		default:
			return nil, fmt.Errorf("expected ',' or '}' in object")
		}
	}
}

func (p *jsonParser) parseArray() (runtime.Value, error) {
	p.pos++ // skip [
	// Hint a typical JSON-array size so the first few appends skip
	// the slice-grow path.
	elements := make([]runtime.Value, 0, 8)
	p.skipWhitespace()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return &runtime.List{Elements: elements}, nil
	}
	for {
		p.skipWhitespace()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		elements = append(elements, value)
		p.skipWhitespace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unexpected end of array")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case ']':
			p.pos++
			return &runtime.List{Elements: elements}, nil
		default:
			return nil, fmt.Errorf("expected ',' or ']' in array")
		}
	}
}

func (p *jsonParser) parseString() (string, error) {
	if p.pos >= len(p.src) || p.src[p.pos] != '"' {
		return "", fmt.Errorf("expected string")
	}
	p.pos++ // skip opening quote
	start := p.pos
	// Fast path: scan for terminator or escape.
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			s := p.src[start:p.pos]
			// Small extracted values should not pin a multi-megabyte document.
			if len(p.src) > 1<<20 && len(s) < 4096 {
				s = strings.Clone(s)
			}
			p.pos++
			return s, nil
		}
		if c == '\\' {
			// Slow path with escapes.
			return p.parseStringWithEscapes(start)
		}
		if c < 0x20 {
			return "", fmt.Errorf("invalid control character in string")
		}
		p.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *jsonParser) parseStringWithEscapes(start int) (string, error) {
	buf := make([]byte, 0, p.pos-start+16)
	buf = append(buf, p.src[start:p.pos]...)
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			p.pos++
			return string(buf), nil
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.src) {
				return "", fmt.Errorf("unterminated escape sequence")
			}
			esc := p.src[p.pos]
			p.pos++
			switch esc {
			case '"', '\\', '/':
				buf = append(buf, esc)
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'u':
				r, err := p.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				if utf16.IsSurrogate(r) {
					if p.pos+1 < len(p.src) && p.src[p.pos] == '\\' && p.src[p.pos+1] == 'u' {
						escapeStart := p.pos
						p.pos += 2
						r2, err := p.parseUnicodeEscape()
						if err != nil {
							return "", err
						}
						combined := utf16.DecodeRune(r, r2)
						if combined != utf8.RuneError {
							var tmp [4]byte
							n := utf8.EncodeRune(tmp[:], combined)
							buf = append(buf, tmp[:n]...)
							continue
						}
						p.pos = escapeStart
					}
					var tmp [4]byte
					n := utf8.EncodeRune(tmp[:], utf8.RuneError)
					buf = append(buf, tmp[:n]...)
					continue
				}
				var tmp [4]byte
				n := utf8.EncodeRune(tmp[:], r)
				buf = append(buf, tmp[:n]...)
			default:
				return "", fmt.Errorf("invalid escape \\%c", esc)
			}
			continue
		}
		if c < 0x20 {
			return "", fmt.Errorf("invalid control character in string")
		}
		buf = append(buf, c)
		p.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *jsonParser) parseUnicodeEscape() (rune, error) {
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

func (p *jsonParser) parseBool() (runtime.Value, error) {
	if p.pos+4 <= len(p.src) && p.src[p.pos:p.pos+4] == "true" {
		p.pos += 4
		return runtime.Bool{Value: true}, nil
	}
	if p.pos+5 <= len(p.src) && p.src[p.pos:p.pos+5] == "false" {
		p.pos += 5
		return runtime.Bool{Value: false}, nil
	}
	return nil, fmt.Errorf("expected boolean literal")
}

func (p *jsonParser) parseNull() (runtime.Value, error) {
	if p.pos+4 <= len(p.src) && p.src[p.pos:p.pos+4] == "null" {
		p.pos += 4
		return runtime.Null{}, nil
	}
	return nil, fmt.Errorf("expected null literal")
}

func (p *jsonParser) parseNumber() (runtime.Value, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c < '0' || c > '9' {
			break
		}
		p.pos++
	}
	isFloat := false
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		isFloat = true
		p.pos++
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c < '0' || c > '9' {
				break
			}
			p.pos++
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		isFloat = true
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c < '0' || c > '9' {
				break
			}
			p.pos++
		}
	}
	text := p.src[start:p.pos]
	if isFloat {
		return runtime.NewDecimalLiteral(text)
	}
	// Fast path: integers that fit int64 (the JSON common case for
	// ids, counts, scores) skip the big.Int parse via SetString and
	// build a `runtime.SmallInt` - the runtime's interface-inline
	// int representation. The VM has handled SmallInt natively since
	// 1.0.5; the evaluator's numeric-infix dispatcher promotes
	// SmallInt to Int on the fly and primitiveEqual now cross-
	// compares SmallInt with Int so parsed values compare equal to
	// literal int values.
	if n := p.pos - start; n <= 18 {
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			if v >= smallIntCacheMin && v < smallIntCacheMax {
				return smallIntInterfaceCache[v-smallIntCacheMin], nil
			}
			return runtime.SmallInt{Value: v}, nil
		}
	}
	value, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer literal %q", text)
	}
	return runtime.Int{Value: value}, nil
}

func (p *jsonParser) skipWhitespace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}
