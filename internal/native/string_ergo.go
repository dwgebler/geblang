package native

import (
	"fmt"
	"strings"
	"unicode"

	"geblang/internal/runtime"
)

// String ergonomics helpers shared by both backends so the eval and VM
// dispatches stay in lockstep.

// StringCapitalize upper-cases the first rune and lower-cases the rest.
func StringCapitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// StringTitle title-cases each whitespace-separated word (first rune upper,
// remaining runes lower); whitespace is preserved verbatim.
func StringTitle(s string) string {
	var b strings.Builder
	inWord := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			inWord = false
			b.WriteRune(r)
		case !inWord:
			b.WriteRune(unicode.ToUpper(r))
			inWord = true
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// StringLines splits on line boundaries (\n and \r\n). A trailing newline
// does not yield a final empty element; the empty string yields no lines.
func StringLines(s string) []string {
	if s == "" {
		return []string{}
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func StringRemovePrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

func StringRemoveSuffix(s, suffix string) string { return strings.TrimSuffix(s, suffix) }

func StringIsBlank(s string) bool { return strings.TrimSpace(s) == "" }

func StringEqualsIgnoreCase(a, b string) bool { return strings.EqualFold(a, b) }

func StringContainsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// StringTrimMethod implements trim, trimStart and trimEnd: whitespace by default, else any rune in the optional cutset.
func StringTrimMethod(method, s string, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("string.%s expects at most one argument", method)
	}
	if len(args) == 0 {
		switch method {
		case "trimStart":
			return runtime.String{Value: strings.TrimLeftFunc(s, unicode.IsSpace)}, nil
		case "trimEnd":
			return runtime.String{Value: strings.TrimRightFunc(s, unicode.IsSpace)}, nil
		}
		return runtime.String{Value: strings.TrimSpace(s)}, nil
	}
	cutset, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("string.%s expects a string of characters to remove", method)
	}
	switch method {
	case "trimStart":
		return runtime.String{Value: strings.TrimLeft(s, cutset.Value)}, nil
	case "trimEnd":
		return runtime.String{Value: strings.TrimRight(s, cutset.Value)}, nil
	}
	return runtime.String{Value: strings.Trim(s, cutset.Value)}, nil
}

// StringCaseMethod implements upper and lower: the whole string, or the first n characters (the last -n when negative).
func StringCaseMethod(method, s string, args []runtime.Value) (runtime.Value, error) {
	convert := strings.ToUpper
	if method == "lower" {
		convert = strings.ToLower
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("string.%s expects at most one argument", method)
	}
	if len(args) == 0 {
		return runtime.String{Value: convert(s)}, nil
	}
	n, err := intArg(args[0], "string."+method)
	if err != nil {
		return nil, err
	}
	r := []rune(s)
	if n >= int64(len(r)) || -n >= int64(len(r)) {
		return runtime.String{Value: convert(s)}, nil
	}
	if n >= 0 {
		return runtime.String{Value: convert(string(r[:n])) + string(r[n:])}, nil
	}
	split := int64(len(r)) + n
	return runtime.String{Value: string(r[:split]) + convert(string(r[split:]))}, nil
}
