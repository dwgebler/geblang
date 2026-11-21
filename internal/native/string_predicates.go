package native

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"geblang/internal/runtime"
)

var stringPredicates = map[string]func(string) bool{
	"isDigits":       StringIsDigits,
	"isAlpha":        StringIsAlpha,
	"isAlphanumeric": StringIsAlphanumeric,
	"isLower":        StringIsLower,
	"isUpper":        StringIsUpper,
	"isAscii":        StringIsAscii,
	"isHex":          StringIsHex,
	"isUuid":         StringIsUuid,
	"isEmail":        StringIsEmail,
	"isUrl":          StringIsUrl,
	"isUtf8":         utf8.ValidString,
}

func stringPredicate(value runtime.String, name string, args []runtime.Value) (runtime.Value, bool, error) {
	fn, ok := stringPredicates[name]
	if !ok {
		return nil, false, nil
	}
	if len(args) != 0 {
		return nil, true, fmt.Errorf("string.%s expects no arguments", name)
	}
	return runtime.Bool{Value: fn(value.Value)}, true, nil
}

func allBytes(s string, ok func(byte) bool) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isHexDigit(c byte) bool {
	return isASCIIDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func StringIsDigits(s string) bool { return allBytes(s, isASCIIDigit) }

func StringIsAlpha(s string) bool { return allBytes(s, isASCIILetter) }

func StringIsAlphanumeric(s string) bool {
	return allBytes(s, func(c byte) bool { return isASCIILetter(c) || isASCIIDigit(c) })
}

func StringIsAscii(s string) bool { return allBytes(s, func(c byte) bool { return c < utf8.RuneSelf }) }

func StringIsHex(s string) bool { return allBytes(s, isHexDigit) }

func StringIsLower(s string) bool { return asciiCaseOnly(s, 'a', 'A') }

func StringIsUpper(s string) bool { return asciiCaseOnly(s, 'A', 'a') }

func asciiCaseOnly(s string, want, reject byte) bool {
	seen := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= reject && c <= reject+25 {
			return false
		}
		if c >= want && c <= want+25 {
			seen = true
		}
	}
	return seen
}

func StringIsUuid(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
		} else if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func hasSpace(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
}

func StringIsEmail(s string) bool {
	if hasSpace(s) || strings.Count(s, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(s, "@")
	if local == "" {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
	}
	return true
}

func StringIsUrl(s string) bool {
	if hasSpace(s) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Hostname() != ""
}
