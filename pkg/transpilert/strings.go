package transpilert

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Typed adapters for Geblang's string methods on the transpiler's unboxed
// string representation. Length / indexing / slicing use code points (runes),
// matching the documented semantics. Self-contained over the Go stdlib (pure
// stdlib, no internal/native, no uniseg) so transpilert vendors trivially;
// grapheme segmentation is deferred in --native and hard-fails to the VM.

// StringLength returns the number of Unicode code points.
func StringLength(s string) int64 { return int64(len([]rune(s))) }

func StringUpper(s string) string { return strings.ToUpper(s) }
func StringLower(s string) string { return strings.ToLower(s) }
func StringTrim(s string) string  { return strings.TrimSpace(s) }

func StringTrimStart(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }
func StringTrimEnd(s string) string   { return strings.TrimRightFunc(s, unicode.IsSpace) }

func StringTrimSet(s, cutset string) string      { return strings.Trim(s, cutset) }
func StringTrimStartSet(s, cutset string) string { return strings.TrimLeft(s, cutset) }
func StringTrimEndSet(s, cutset string) string   { return strings.TrimRight(s, cutset) }

func StringUpperN(s string, n int64) string { return caseN(s, n, strings.ToUpper) }
func StringLowerN(s string, n int64) string { return caseN(s, n, strings.ToLower) }

// caseN converts the first n code points, or the last -n when n is negative.
func caseN(s string, n int64, convert func(string) string) string {
	r := []rune(s)
	size := int64(len(r))
	if n >= size || -n >= size {
		return convert(s)
	}
	if n >= 0 {
		return convert(string(r[:n])) + string(r[n:])
	}
	return string(r[:size+n]) + convert(string(r[size+n:]))
}

func StringContains(s, sub string) bool { return strings.Contains(s, sub) }
func StringStartsWith(s, p string) bool { return strings.HasPrefix(s, p) }
func StringEndsWith(s, p string) bool   { return strings.HasSuffix(s, p) }
func StringIsEmpty(s string) bool       { return len(s) == 0 }
func StringCount(s, sub string) int64   { return int64(strings.Count(s, sub)) }

// StringIndexOf returns the code-point index of sub, or -1.
func StringIndexOf(s, sub string) int64 {
	byteIndex := strings.Index(s, sub)
	if byteIndex < 0 {
		return -1
	}
	return int64(len([]rune(s[:byteIndex])))
}

// StringLastIndexOf returns the code-point index of the last sub, or -1.
func StringLastIndexOf(s, sub string) int64 {
	byteIndex := strings.LastIndex(s, sub)
	if byteIndex < 0 {
		return -1
	}
	return int64(len([]rune(s[:byteIndex])))
}

// StringReplace replaces up to count occurrences (count < 0 means all).
func StringReplace(s, old, new string, count int) string {
	return strings.Replace(s, old, new, count)
}

func StringSplit(s, sep string) []string { return strings.Split(s, sep) }

// StringRepeat repeats s n times (negative n yields empty).
func StringRepeat(s string, n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat(s, n)
}

// StringReverse reverses by code point.
func StringReverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// StringChars returns each code point as a single-rune string.
func StringChars(s string) []string {
	r := []rune(s)
	out := make([]string, len(r))
	for i, c := range r {
		out[i] = string(c)
	}
	return out
}

// StringCodePoints returns the Unicode code point of each rune.
func StringCodePoints(s string) []int64 {
	r := []rune(s)
	out := make([]int64, len(r))
	for i, c := range r {
		out[i] = int64(c)
	}
	return out
}

// StringSlice extracts code points from start up to (not including) end, with
// the interpreter's negative-index and clamping rules.
func StringSlice(s string, start, end int) string {
	r := []rune(s)
	n := len(r)
	start = clampIndex(start, n)
	end = clampIndex(end, n)
	if start >= end {
		return ""
	}
	return string(r[start:end])
}

// StringSliceFrom extracts code points from start to the end of the string.
func StringSliceFrom(s string, start int) string {
	r := []rune(s)
	return StringSlice(s, start, len(r))
}

func clampIndex(i, n int) int {
	if i < 0 {
		i = n + i
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}

// StringPadStart left-pads s with pad to targetLen code points; an empty pad
// raises a RuntimeError, matching the interpreter.
func StringPadStart(s string, targetLen int, pad string) string {
	return padString(s, targetLen, pad, true, "string.padStart")
}

// StringPadEnd right-pads s with pad to targetLen code points; an empty pad
// raises a RuntimeError, matching the interpreter.
func StringPadEnd(s string, targetLen int, pad string) string {
	return padString(s, targetLen, pad, false, "string.padEnd")
}

func padString(s string, targetLen int, pad string, atStart bool, label string) string {
	if pad == "" {
		panic(NewError("RuntimeError", label+": pad must be a non-empty string"))
	}
	fill := padFill(pad, targetLen-utf8.RuneCountInString(s))
	if atStart {
		return fill + s
	}
	return s + fill
}

// padFill repeats pad from its first character to exactly n characters.
func padFill(pad string, n int) string {
	if n <= 0 {
		return ""
	}
	padRunes := []rune(pad)
	out := make([]rune, n)
	for i := range out {
		out[i] = padRunes[i%len(padRunes)]
	}
	return string(out)
}

// StringCapitalize upper-cases the first rune and lower-cases the rest.
func StringCapitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// StringTitle title-cases each whitespace-separated word; whitespace verbatim.
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

func StringIsBlank(s string) bool { return strings.TrimSpace(s) == "" }

// StringLines splits on \n and \r\n; a trailing newline yields no final empty
// element, and the empty string yields no lines.
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
func StringEqualsIgnoreCase(a, b string) bool    { return strings.EqualFold(a, b) }
func StringContainsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
