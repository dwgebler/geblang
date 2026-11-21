package transpilert

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// String case/wrap and bytes search helpers mirror internal/native so --native matches the interpreter.

// StringWords splits at non-alphanumerics and case changes, keeping acronyms whole and digits on the preceding word.
func StringWords(s string) []string {
	words := []string{}
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 && unicode.IsUpper(r) {
			prev := cur[len(cur)-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

func StringSnakeCase(s string) string {
	return strings.ToLower(strings.Join(StringWords(s), "_"))
}

func StringKebabCase(s string) string {
	return strings.ToLower(strings.Join(StringWords(s), "-"))
}

func StringCamelCase(s string) string {
	var b strings.Builder
	for i, word := range StringWords(s) {
		if i == 0 {
			b.WriteString(strings.ToLower(word))
			continue
		}
		b.WriteString(StringCapitalize(word))
	}
	return b.String()
}

func StringCenter(s string, width int64, pad string) string {
	if pad == "" {
		panic(NewError("RuntimeError", "string.center: pad must be a non-empty string"))
	}
	total := int(width) - utf8.RuneCountInString(s)
	if total <= 0 {
		return s
	}
	left := total / 2
	return padFill(pad, left) + s + padFill(pad, total-left)
}

// StringWrap greedily wraps each input line to width characters; an over-long word keeps its own line.
func StringWrap(s string, w int64) []string {
	if w < 1 {
		panic(NewError("ValueError", "string.wrap: width must be at least 1"))
	}
	width := int(w)
	out := []string{}
	for _, line := range StringLines(s) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			out = append(out, "")
			continue
		}
		cur := fields[0]
		curLen := utf8.RuneCountInString(cur)
		for _, word := range fields[1:] {
			wordLen := utf8.RuneCountInString(word)
			if curLen+1+wordLen > width {
				out = append(out, cur)
				cur, curLen = word, wordLen
				continue
			}
			cur += " " + word
			curLen += 1 + wordLen
		}
		out = append(out, cur)
	}
	return out
}

// byteNeedle accepts bytes or an int byte value; an int outside 0-255 can never match.
func byteNeedle(name string, needle any) ([]byte, bool) {
	var n int64
	switch v := needle.(type) {
	case []byte:
		return v, true
	case int64:
		n = v
	case int:
		n = int64(v)
	default:
		panic(NewError("RuntimeError", "bytes."+name+" expects bytes or int byte"))
	}
	if n < 0 || n > 255 {
		return nil, false
	}
	return []byte{byte(n)}, true
}

func BytesStartsWith(b []byte, prefix any) bool {
	needle, ok := byteNeedle("startsWith", prefix)
	return ok && bytes.HasPrefix(b, needle)
}

func BytesEndsWith(b []byte, suffix any) bool {
	needle, ok := byteNeedle("endsWith", suffix)
	return ok && bytes.HasSuffix(b, needle)
}

func BytesIndexOf(b []byte, needle any) int64 {
	n, ok := byteNeedle("indexOf", needle)
	if !ok {
		return -1
	}
	return int64(bytes.Index(b, n))
}
