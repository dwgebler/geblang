package transpilert

import (
	"reflect"
	"testing"
)

func TestStringLengthCodePoints(t *testing.T) {
	// "résumé" is 6 code points (two are two-byte runes).
	if StringLength("résumé") != 6 {
		t.Fatalf("length = %d, want 6", StringLength("résumé"))
	}
}

func TestStringCaseAndTrim(t *testing.T) {
	if StringUpper("héllo") != "HÉLLO" {
		t.Fatalf("upper = %q", StringUpper("héllo"))
	}
	if StringLower("HÉLLO") != "héllo" {
		t.Fatalf("lower = %q", StringLower("HÉLLO"))
	}
	if StringTrim("  hi  ") != "hi" {
		t.Fatal("trim mismatch")
	}
	if StringTrimStart("  hi  ") != "hi  " || StringTrimEnd("  hi  ") != "  hi" {
		t.Fatal("trim start/end mismatch")
	}
}

func TestStringSliceNegativeAndClamp(t *testing.T) {
	s := "hello world"
	if StringSlice(s, 0, 5) != "hello" {
		t.Fatalf("slice(0,5) = %q", StringSlice(s, 0, 5))
	}
	if StringSliceFrom(s, 6) != "world" {
		t.Fatalf("sliceFrom(6) = %q", StringSliceFrom(s, 6))
	}
	if StringSliceFrom(s, -5) != "world" {
		t.Fatalf("sliceFrom(-5) = %q", StringSliceFrom(s, -5))
	}
	if StringSlice(s, 0, -6) != "hello" {
		t.Fatalf("slice(0,-6) = %q", StringSlice(s, 0, -6))
	}
	if StringSlice(s, 5, 2) != "" {
		t.Fatal("start>=end should be empty")
	}
	if StringSlice(s, 0, 999) != s {
		t.Fatal("end overshoot should clamp")
	}
}

func TestStringSliceRuneAware(t *testing.T) {
	if StringSlice("héllo", 0, 2) != "hé" {
		t.Fatalf("rune slice = %q, want hé", StringSlice("héllo", 0, 2))
	}
}

func TestStringIndexOfRuneAware(t *testing.T) {
	// "héllo": 'l' is at code-point index 2 (after h and é).
	if StringIndexOf("héllo", "l") != 2 {
		t.Fatalf("indexOf = %d, want 2", StringIndexOf("héllo", "l"))
	}
	if StringIndexOf("abc", "z") != -1 {
		t.Fatal("missing needle should be -1")
	}
	if StringLastIndexOf("héllo", "l") != 3 {
		t.Fatalf("lastIndexOf = %d, want 3", StringLastIndexOf("héllo", "l"))
	}
}

func TestStringSplitChars(t *testing.T) {
	if got := StringSplit("a,b,c", ","); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("split = %v", got)
	}
	if got := StringChars("héllo"); !reflect.DeepEqual(got, []string{"h", "é", "l", "l", "o"}) {
		t.Fatalf("chars = %v", got)
	}
}

func TestStringReplaceRepeatReverse(t *testing.T) {
	if StringReplace("aaa", "a", "b", 2) != "bba" {
		t.Fatal("replace count mismatch")
	}
	if StringReplace("aaa", "a", "b", -1) != "bbb" {
		t.Fatal("replace all mismatch")
	}
	if StringRepeat("ab", 3) != "ababab" {
		t.Fatal("repeat mismatch")
	}
	if StringRepeat("ab", -1) != "" {
		t.Fatal("repeat negative mismatch")
	}
	if StringReverse("héllo") != "olléh" {
		t.Fatalf("reverse = %q", StringReverse("héllo"))
	}
}

func TestStringPad(t *testing.T) {
	if StringPadStart("7", 3, "0") != "007" {
		t.Fatalf("padStart = %q", StringPadStart("7", 3, "0"))
	}
	if StringPadEnd("7", 3, "0") != "700" {
		t.Fatalf("padEnd = %q", StringPadEnd("7", 3, "0"))
	}
	if StringPadStart("abcd", 2, "0") != "abcd" {
		t.Fatalf("padStart over-length = %q, want abcd", StringPadStart("abcd", 2, "0"))
	}
	if StringPadEnd("abcd", 2, "0") != "abcd" {
		t.Fatalf("padEnd over-length = %q, want abcd", StringPadEnd("abcd", 2, "0"))
	}
	if StringPadStart("x", 4, "ab") != "abax" {
		t.Fatalf("padStart multi-char = %q, want abax", StringPadStart("x", 4, "ab"))
	}
	if StringPadStart("x", 4, "\u00e9-") != "\u00e9-\u00e9x" {
		t.Fatalf("padStart multi-byte = %q", StringPadStart("x", 4, "\u00e9-"))
	}
}

func TestStringPredicates(t *testing.T) {
	if !StringContains("hello", "ell") || StringContains("hello", "z") {
		t.Fatal("contains mismatch")
	}
	if !StringStartsWith("hello", "he") || !StringEndsWith("hello", "lo") {
		t.Fatal("starts/ends mismatch")
	}
	if !StringIsEmpty("") || StringIsEmpty("x") {
		t.Fatal("isEmpty mismatch")
	}
	if StringCount("banana", "a") != 3 {
		t.Fatal("count mismatch")
	}
}

func TestStringErgonomicDelegation(t *testing.T) {
	if StringCapitalize("hELLO") != "Hello" {
		t.Fatalf("capitalize = %q", StringCapitalize("hELLO"))
	}
	if !StringIsBlank("   ") || StringIsBlank("x") {
		t.Fatal("isBlank mismatch")
	}
	if StringRemovePrefix("foobar", "foo") != "bar" {
		t.Fatal("removePrefix mismatch")
	}
	if StringRemoveSuffix("foobar", "bar") != "foo" {
		t.Fatal("removeSuffix mismatch")
	}
	if !StringEqualsIgnoreCase("ABC", "abc") {
		t.Fatal("equalsIgnoreCase mismatch")
	}
	if !StringContainsIgnoreCase("Hello", "ELL") {
		t.Fatal("containsIgnoreCase mismatch")
	}
}

func TestStringCodePoints(t *testing.T) {
	if got := StringCodePoints("Aé"); !reflect.DeepEqual(got, []int64{65, 233}) {
		t.Fatalf("codePoints = %v", got)
	}
}

func TestStringPadEmptyPadPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty pad")
		}
	}()
	StringPadStart("x", 5, "")
}
