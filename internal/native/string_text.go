package native

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"geblang/internal/runtime"
)

func stringTextMethod(s, name string, args []runtime.Value) (runtime.Value, bool, error) {
	switch name {
	case "words", "snakeCase", "camelCase", "kebabCase", "slugify":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("string.%s expects no arguments", name)
		}
		switch name {
		case "words":
			return stringList(StringWords(s)), true, nil
		case "snakeCase":
			return runtime.String{Value: StringSnakeCase(s)}, true, nil
		case "camelCase":
			return runtime.String{Value: StringCamelCase(s)}, true, nil
		case "kebabCase":
			return runtime.String{Value: StringKebabCase(s)}, true, nil
		}
		return runtime.String{Value: StringSlugify(s)}, true, nil
	case "center":
		if len(args) < 1 || len(args) > 2 {
			return nil, true, fmt.Errorf("string.center expects (width[, pad])")
		}
		width, err := widthArg(args[0])
		if err != nil {
			return nil, true, fmt.Errorf("string.center: %v", err)
		}
		pad := " "
		if len(args) == 2 {
			padStr, ok := args[1].(runtime.String)
			if !ok || padStr.Value == "" {
				return nil, true, fmt.Errorf("string.center: pad must be a non-empty string")
			}
			pad = padStr.Value
		}
		return runtime.String{Value: StringCenter(s, width, pad)}, true, nil
	case "wrap":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("string.wrap expects one argument (width)")
		}
		width, err := widthArg(args[0])
		if err != nil {
			return nil, true, fmt.Errorf("string.wrap: %v", err)
		}
		if width < 1 {
			return nil, true, runtime.ClassifiedError{Class: "ValueError", Message: "string.wrap: width must be at least 1"}
		}
		return stringList(StringWrap(s, width)), true, nil
	}
	return nil, false, nil
}

func stringList(items []string) *runtime.List {
	out := make([]runtime.Value, len(items))
	for i, item := range items {
		out[i] = runtime.String{Value: item}
	}
	return &runtime.List{Elements: out}
}

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

func StringSlugify(s string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			continue
		}
		pendingDash = true
	}
	return b.String()
}

func StringCenter(s string, width int, pad string) string {
	total := width - utf8.RuneCountInString(s)
	if total <= 0 {
		return s
	}
	left := total / 2
	return padFill(pad, left) + s + padFill(pad, total-left)
}

// StringWrap greedily wraps each input line to width characters; an over-long word keeps its own line.
func StringWrap(s string, width int) []string {
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
