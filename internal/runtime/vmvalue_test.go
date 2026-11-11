package runtime

import (
	"math"
	"math/big"
	"testing"
)

func TestVMValueRoundTripPrimitives(t *testing.T) {
	cases := []struct {
		name string
		v    Value
	}{
		{"null", Null{}},
		{"bool true", Bool{Value: true}},
		{"bool false", Bool{Value: false}},
		{"smallint zero", SmallInt{Value: 0}},
		{"smallint positive", SmallInt{Value: 42}},
		{"smallint negative", SmallInt{Value: -7}},
		{"smallint max int64", SmallInt{Value: math.MaxInt64}},
		{"float zero", Float{Value: 0}},
		{"float pi", Float{Value: 3.14159}},
		{"float negative", Float{Value: -2.71828}},
		{"float max", Float{Value: math.MaxFloat64}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roundtrip := VMValueFromValue(c.v).ToValue()
			if !ValuesEqual(roundtrip, c.v) {
				t.Fatalf("round-trip lost equality: got %#v, want %#v", roundtrip, c.v)
			}
		})
	}
}

func TestVMValueRoundTripBoxed(t *testing.T) {
	bigInt := Int{Value: new(big.Int).Lsh(big.NewInt(1), 65)}
	decimal := Decimal{Value: big.NewRat(355, 113)}
	str := String{Value: "hello"}
	bytes := Bytes{Value: []byte{1, 2, 3}}
	list := &List{Elements: []Value{SmallInt{Value: 1}, SmallInt{Value: 2}}}
	dict := DictFromEntries(map[string]DictEntry{
		"a": {Key: String{Value: "a"}, Value: SmallInt{Value: 10}},
	}, nil)

	cases := []struct {
		name string
		v    Value
	}{
		{"big int", bigInt},
		{"decimal", decimal},
		{"string", str},
		{"bytes", bytes},
		{"list", list},
		{"dict", dict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roundtrip := VMValueFromValue(c.v).ToValue()
			if !ValuesEqual(roundtrip, c.v) {
				t.Fatalf("round-trip lost equality: got %#v, want %#v", roundtrip, c.v)
			}
		})
	}
}

func TestVMValueZeroIsNull(t *testing.T) {
	var zero VMValue
	v := zero.ToValue()
	if _, ok := v.(Null); !ok {
		t.Fatalf("zero VMValue should produce Null, got %#v", v)
	}
	if !zero.IsNull() {
		t.Fatalf("zero VMValue should report IsNull")
	}
}

func TestVMValueAsSmallInt(t *testing.T) {
	if n, ok := VMValueSmallInt(42).AsSmallInt(); !ok || n != 42 {
		t.Fatalf("AsSmallInt on SmallInt: got %v ok=%v", n, ok)
	}
	if _, ok := VMValueBool(true).AsSmallInt(); ok {
		t.Fatalf("AsSmallInt on Bool should fail")
	}
	if _, ok := VMValueFromValue(String{Value: "x"}).AsSmallInt(); ok {
		t.Fatalf("AsSmallInt on String should fail")
	}
}

func TestVMValueAsIntAnyHandlesBoxedInt(t *testing.T) {
	boxed := VMValueFromValue(NewInt64(7))
	if n, ok := boxed.AsIntAny(); !ok || n != 7 {
		t.Fatalf("AsIntAny on boxed Int: got %v ok=%v", n, ok)
	}
	if n, ok := VMValueSmallInt(11).AsIntAny(); !ok || n != 11 {
		t.Fatalf("AsIntAny on SmallInt: got %v ok=%v", n, ok)
	}
	overflow := VMValueFromValue(Int{Value: new(big.Int).Lsh(big.NewInt(1), 65)})
	if _, ok := overflow.AsIntAny(); ok {
		t.Fatalf("AsIntAny on overflow big.Int should fail")
	}
}

func TestVMValueAsBigInt(t *testing.T) {
	if b, ok := VMValueSmallInt(99).AsBigInt(); !ok || b.Int64() != 99 {
		t.Fatalf("AsBigInt on SmallInt: got %v ok=%v", b, ok)
	}
	original := new(big.Int).Lsh(big.NewInt(1), 65)
	v := VMValueFromValue(Int{Value: original})
	if b, ok := v.AsBigInt(); !ok || b.Cmp(original) != 0 {
		t.Fatalf("AsBigInt on big.Int: got %v ok=%v", b, ok)
	}
}

func TestVMValueFloatBits(t *testing.T) {
	v := VMValueFloat(math.Pi)
	if got, ok := v.AsFloat(); !ok || got != math.Pi {
		t.Fatalf("AsFloat: got %v ok=%v", got, ok)
	}
	// NaN survives the bit-cast round-trip.
	v = VMValueFloat(math.NaN())
	if got, ok := v.AsFloat(); !ok || !math.IsNaN(got) {
		t.Fatalf("NaN lost: got %v ok=%v", got, ok)
	}
}

func TestVMValueTypeName(t *testing.T) {
	if VMValueNull.TypeName() != "null" {
		t.Fatalf("null type name")
	}
	if VMValueBool(true).TypeName() != "bool" {
		t.Fatalf("bool type name")
	}
	if VMValueSmallInt(5).TypeName() != "int" {
		t.Fatalf("smallint type name")
	}
	if VMValueFloat(1.5).TypeName() != "float" {
		t.Fatalf("float type name")
	}
	if VMValueFromValue(String{Value: "x"}).TypeName() != "string" {
		t.Fatalf("string type name")
	}
}
