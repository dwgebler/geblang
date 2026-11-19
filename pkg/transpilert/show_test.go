package transpilert

import (
	"math/big"
	"testing"
)

func TestShowScalars(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int64(42), "42"},
		{int64(-7), "-7"},
		{true, "true"},
		{false, "false"},
		{"hello", "hello"},
		{nil, "null"},
		{3.14, "3.14"},
		{1.0, "1"},
		{100.0, "100"},
		{0.5, "0.5"},
		{1000000.0, "1e+06"},
		{0.0001, "0.0001"},
		{1e20, "1e+20"},
		{1e-20, "1e-20"},
		{[]byte("hi"), "6869"},
		{FromInt64(99), "99"},
	}
	for _, c := range cases {
		if got := Show(c.in); got != c.want {
			t.Errorf("Show(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShowDecimal(t *testing.T) {
	r, _ := new(big.Rat).SetString("3.14")
	if got := Show(r); got != "3.14" {
		t.Errorf("Show(decimal 3.14) = %q, want %q", got, "3.14")
	}
	one, _ := new(big.Rat).SetString("1")
	if got := Show(one); got != "1.0" {
		t.Errorf("Show(decimal 1) = %q, want %q", got, "1.0")
	}
}

func TestShowBigInt(t *testing.T) {
	big := new(big.Int)
	big.SetString("100000000000000000000000", 10)
	if got := Show(FromBig(big)); got != "100000000000000000000000" {
		t.Errorf("Show(big int) = %q", got)
	}
}

func TestShowList(t *testing.T) {
	if got := Show([]int64{1, 2, 3}); got != "[1, 2, 3]" {
		t.Errorf("got %q", got)
	}
	if got := Show([][]int64{{1, 2}, {3, 4}}); got != "[[1, 2], [3, 4]]" {
		t.Errorf("got %q", got)
	}
}

func TestShowNestedStringsQuoted(t *testing.T) {
	if got := Show([]string{"a", "b"}); got != `["a", "b"]` {
		t.Errorf("got %q", got)
	}
	// A bare string at top level is unquoted; quoted only when nested.
	if got := Show("a"); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := Show([]string{"with \"q\""}); got != `["with \"q\""]` {
		t.Errorf("got %q", got)
	}
}

func TestShowNestedFloats(t *testing.T) {
	if got := Show([]float64{3.14, 2.0}); got != "[3.14, 2]" {
		t.Errorf("got %q", got)
	}
}

func TestShowDict(t *testing.T) {
	d := NewOrderedDict[string, int64]()
	d.Set("k", 1)
	d.Set("k2", 2)
	if got := Show(d); got != `{"k": 1, "k2": 2}` {
		t.Errorf("got %q", got)
	}
}

func TestShowDictNested(t *testing.T) {
	d := NewOrderedDict[string, []string]()
	d.Set("tags", []string{"x", "y"})
	if got := Show(d); got != `{"tags": ["x", "y"]}` {
		t.Errorf("got %q", got)
	}
}

func TestShowDictInList(t *testing.T) {
	d := NewOrderedDict[string, int64]()
	d.Set("a", 1)
	if got := Show([]any{d}); got != `[{"a": 1}]` {
		t.Errorf("got %q", got)
	}
}

func TestShowSet(t *testing.T) {
	s := map[int64]struct{}{3: {}, 1: {}, 2: {}}
	if got := Show(s); got != "set{1, 2, 3}" {
		t.Errorf("got %q", got)
	}
	ss := map[string]struct{}{"b": {}, "a": {}, "c": {}}
	if got := Show(ss); got != `set{"a", "b", "c"}` {
		t.Errorf("got %q", got)
	}
}

type showStringer struct{ n string }

func (s *showStringer) GbString() string { return "Named(" + s.n + ")" }

type showPlain struct{ x int64 }

func TestShowInstanceTopLevelUsesDunder(t *testing.T) {
	if got := Show(&showStringer{n: "bob"}); got != "Named(bob)" {
		t.Errorf("got %q", got)
	}
}

func TestShowInstanceNestedNoDunder(t *testing.T) {
	if got := Show([]any{&showStringer{n: "bob"}}); got != "[<showStringer>]" {
		t.Errorf("got %q", got)
	}
}

func TestShowInstancePlain(t *testing.T) {
	if got := Show(&showPlain{x: 1}); got != "<showPlain>" {
		t.Errorf("got %q", got)
	}
}

func TestShowTypedNil(t *testing.T) {
	var p *int64
	if got := Show(p); got != "null" {
		t.Errorf("got %q", got)
	}
	var sp *showPlain
	if got := Show(sp); got != "null" {
		t.Errorf("got %q", got)
	}
}

type showEnum int

func (e showEnum) String() string { return "E.V" }

func TestShowEnumUsesStringer(t *testing.T) {
	if got := Show(showEnum(0)); got != "E.V" {
		t.Errorf("got %q", got)
	}
	// An enum nested in a container also renders via its Stringer.
	if got := Show([]any{showEnum(0)}); got != "[E.V]" {
		t.Errorf("got %q", got)
	}
}

func TestShowNullablePointer(t *testing.T) {
	v := int64(5)
	if got := Show(&v); got != "5" {
		t.Errorf("got %q", got)
	}
}
