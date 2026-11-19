package transpilert

import (
	"math"
	"math/big"
	"testing"
)

func TestStringToInt(t *testing.T) {
	cases := map[string]int64{
		"42":    42,
		"-7":    -7,
		"0x1F":  31,
		"0b101": 5,
		"0o17":  15,
		"1_000": 1000,
	}
	for in, want := range cases {
		if got := StringToInt(in); got != want {
			t.Errorf("StringToInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestStringToIntBadPanics(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*Error)
		if !ok {
			t.Fatalf("expected *Error panic, got %T (%v)", r, r)
		}
		if e.Class != "RuntimeError" || e.Message != `invalid integer literal "abc"` {
			t.Fatalf("wrong error: class=%q msg=%q", e.Class, e.Message)
		}
	}()
	StringToInt("abc")
}

func TestStringToFloat(t *testing.T) {
	if got := StringToFloat("3.5"); got != 3.5 {
		t.Errorf("StringToFloat = %v", got)
	}
}

func TestStringToFloatBadPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		} else if _, ok := r.(*Error); !ok {
			t.Fatalf("expected *Error, got %T", r)
		}
	}()
	StringToFloat("xyz")
}

func TestStringToDecimal(t *testing.T) {
	d := StringToDecimal("12.5")
	if d.Cmp(big.NewRat(25, 2)) != 0 {
		t.Errorf("StringToDecimal = %v", d)
	}
}

func TestNumericCheckPredicates(t *testing.T) {
	intCases := map[string]bool{"42": true, "-7": true, "0xFF": true, "1_000": true, "3.5": false, "abc": false, "": false}
	for in, want := range intCases {
		if StringIsInt(in) != want {
			t.Errorf("StringIsInt(%q) = %v, want %v", in, StringIsInt(in), want)
		}
		// Invariant: isInt true iff StringToInt does not panic.
		if didNotPanic(func() { StringToInt(in) }) != StringIsInt(in) {
			t.Errorf("StringIsInt(%q) disagrees with StringToInt", in)
		}
	}
	decCases := map[string]bool{"3.5": true, "42": true, "-0.25": true, "abc": false, "": false}
	for in, want := range decCases {
		if StringIsDecimal(in) != want {
			t.Errorf("StringIsDecimal(%q) = %v, want %v", in, StringIsDecimal(in), want)
		}
		if didNotPanic(func() { StringToDecimal(in) }) != StringIsDecimal(in) {
			t.Errorf("StringIsDecimal(%q) disagrees with StringToDecimal", in)
		}
	}
	if !StringIsNumeric("42") || !StringIsNumeric("3.5") || StringIsNumeric("abc") {
		t.Error("StringIsNumeric")
	}
	if !FloatIsInt(3.0) || FloatIsInt(3.5) || FloatIsInt(math.NaN()) || FloatIsInt(math.Inf(1)) {
		t.Error("FloatIsInt")
	}
	if !DecimalIsInt(big.NewRat(7, 1)) || DecimalIsInt(big.NewRat(7, 2)) {
		t.Error("DecimalIsInt")
	}
}

func didNotPanic(f func()) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f()
	return true
}

func TestStringToBool(t *testing.T) {
	if !StringToBool("true") || StringToBool("false") {
		t.Fatal("bool conversion wrong")
	}
}

func TestStringToBoolBadPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	StringToBool("yes")
}

func TestToStringForms(t *testing.T) {
	if IntToString(100) != "100" {
		t.Error("IntToString")
	}
	if FloatToString(2.5) != "2.5" {
		t.Error("FloatToString")
	}
	if DecimalToString(big.NewRat(25, 2)) != "12.5" {
		t.Errorf("DecimalToString = %q", DecimalToString(big.NewRat(25, 2)))
	}
	if BoolToString(false) != "false" {
		t.Error("BoolToString")
	}
}

func TestNumericCrossConversions(t *testing.T) {
	if FloatToInt(3.9) != 3 || FloatToInt(-3.9) != -3 {
		t.Error("FloatToInt truncation")
	}
	if DecimalToInt(big.NewRat(7, 2)) != 3 {
		t.Error("DecimalToInt truncation")
	}
	if IntToFloat(5) != 5.0 {
		t.Error("IntToFloat")
	}
	if IntToDecimal(4).Cmp(big.NewRat(4, 1)) != 0 {
		t.Error("IntToDecimal")
	}
}
