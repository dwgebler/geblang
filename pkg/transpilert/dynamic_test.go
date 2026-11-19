package transpilert

import (
	"math/big"
	"testing"
)

func TestIndexDictMissYieldsNull(t *testing.T) {
	d := NewOrderedDict[string, any]()
	d.Set("name", "widget")
	if got := Index(d, "name"); got != "widget" {
		t.Errorf("Index hit = %v, want widget", got)
	}
	if got := Index(d, "absent"); got != nil {
		t.Errorf("Index miss = %v, want nil", got)
	}
	// A non-string key against a string-keyed dict is a guaranteed miss.
	if got := Index(d, int64(0)); got != nil {
		t.Errorf("Index wrong-key-type = %v, want nil", got)
	}
}

func TestIndexListIntegerAndNegative(t *testing.T) {
	xs := []any{"a", "b", "c"}
	if got := Index(xs, int64(0)); got != "a" {
		t.Errorf("Index[0] = %v", got)
	}
	if got := Index(xs, int64(-1)); got != "c" {
		t.Errorf("Index[-1] = %v", got)
	}
}

func TestIndexListOutOfRangePanics(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*Error)
		if !ok || e.Message != "list index out of range" {
			t.Fatalf("panic = %v, want list index out of range", r)
		}
	}()
	Index([]any{1}, int64(5))
}

func TestIndexStringCodepoint(t *testing.T) {
	if got := Index("héllo", int64(1)); got != "é" {
		t.Errorf("Index string[1] = %v", got)
	}
}

func TestIndexNonIntKeyOnList(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*Error)
		if !ok || e.Message != "index must be int, got string" {
			t.Fatalf("panic = %v, want index must be int", r)
		}
	}()
	Index([]any{1}, "x")
}

func TestIndexNotIndexable(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*Error)
		if !ok || e.Message != "int is not indexable" {
			t.Fatalf("panic = %v, want int is not indexable", r)
		}
	}()
	Index(int64(3), int64(0))
}

func TestAsIntFromBoxedShapes(t *testing.T) {
	cases := []struct {
		v    any
		want int64
	}{
		{int64(12), 12},
		{big.NewRat(7, 2), 3}, // decimal truncates toward zero
		{float64(-2.9), -2},
		{true, 1},
		{false, 0},
		{"42", 42},
	}
	for _, c := range cases {
		if got := AsIntFast(c.v); got != c.want {
			t.Errorf("AsIntFast(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestAsFloatFromBoxedShapes(t *testing.T) {
	if got := AsFloat(big.NewRat(7, 2)); got != 3.5 {
		t.Errorf("AsFloat(7/2) = %v", got)
	}
	if got := AsFloat(int64(4)); got != 4.0 {
		t.Errorf("AsFloat(4) = %v", got)
	}
	if got := AsFloat("1.5"); got != 1.5 {
		t.Errorf("AsFloat(\"1.5\") = %v", got)
	}
}

func TestAsStringFromBoxedShapes(t *testing.T) {
	if got := AsString("hi"); got != "hi" {
		t.Errorf("AsString string = %q", got)
	}
	if got := AsString(int64(7)); got != "7" {
		t.Errorf("AsString int = %q", got)
	}
	if got := AsString(big.NewRat(7, 2)); got != "3.5" {
		t.Errorf("AsString decimal = %q", got)
	}
}

func TestAsBoolAndList(t *testing.T) {
	if !AsBool(true) || AsBool(int64(0)) || !AsBool(int64(1)) {
		t.Error("AsBool mismatch")
	}
	xs := AsList([]any{1, 2, 3})
	if len(xs) != 3 {
		t.Errorf("AsList len = %d", len(xs))
	}
}

func TestAsDictPassThrough(t *testing.T) {
	d := NewOrderedDict[string, any]()
	d.Set("k", int64(1))
	if AsDict(d) != d {
		t.Error("AsDict should pass through a string-keyed ordered dict")
	}
}

func TestCastErrorMessage(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*Error)
		if !ok || e.Message != "cannot cast list to dict" {
			t.Fatalf("panic = %v, want cannot cast list to dict", r)
		}
	}()
	AsDict([]any{1})
}
