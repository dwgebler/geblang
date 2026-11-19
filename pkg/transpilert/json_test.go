package transpilert

import (
	"math/big"
	"testing"
)

func TestJSONStringifySortsKeysNoSpaces(t *testing.T) {
	d := NewOrderedDict[string, any]()
	d.Set("b", int64(2))
	d.Set("a", int64(1))
	if got := JSONStringify(d); got != `{"a":1,"b":2}` {
		t.Errorf("JSONStringify = %q", got)
	}
}

func TestJSONStringifyScalars(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{int64(42), "42"},
		{true, "true"},
		{nil, "null"},
		{"a\"b\n", `"a\"b\n"`},
		{[]int64{1, 2}, "[1,2]"},
		{big.NewRat(7, 2), "3.5"},
		{[]byte("abc"), `"YWJj"`},
	}
	for _, c := range cases {
		if got := JSONStringify(c.v); got != c.want {
			t.Errorf("JSONStringify(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestJSONParseClassifiesNumbers(t *testing.T) {
	if v := JSONParse("42"); v != int64(42) {
		t.Errorf("parse 42 = %T %v", v, v)
	}
	if v := JSONParse("3.5"); JSONStringify(v) != "3.5" {
		t.Errorf("parse 3.5 round trip = %q", JSONStringify(v))
	}
}

func TestJSONParseObjectPreservesInsertionOrder(t *testing.T) {
	v := JSONParse(`{"z": 1, "a": 2}`)
	d, ok := v.(*OrderedDict[string, any])
	if !ok {
		t.Fatalf("parse object type = %T", v)
	}
	if keys := d.Keys(); keys[0] != "z" || keys[1] != "a" {
		t.Errorf("keys = %v, want insertion order [z a]", keys)
	}
}

func TestJSONValidate(t *testing.T) {
	if !JSONValidate(`{"a":1}`) {
		t.Error("valid JSON reported invalid")
	}
	if JSONValidate("{bad") {
		t.Error("invalid JSON reported valid")
	}
}
