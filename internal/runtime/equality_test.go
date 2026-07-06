package runtime

import (
	"math/big"
	"testing"
)

// leafMatrix covers the pairs the canonical helper owns (finding 6.2 drift surface).
func leafMatrix() []Value {
	return []Value{
		Null{},
		Bool{Value: true},
		Bool{Value: false},
		SmallInt{Value: 1},
		SmallInt{Value: 2},
		Int{Value: big.NewInt(1)},
		Int{Value: big.NewInt(2)},
		Decimal{Value: big.NewRat(1, 1)},
		Float{Value: 1.0},
		Float{Value: 1.5},
		String{Value: "a"},
		String{Value: "Widget"},
		Bytes{Value: []byte{1, 2}},
		Type{Name: "Widget"},
		Type{Name: "Gadget"},
		BytecodeClass{Module: "m", Name: "Widget", Index: 0},
		BytecodeClass{Module: "n", Name: "Widget", Index: 0},
	}
}

func TestLeafValuesEqualCases(t *testing.T) {
	cases := []struct {
		name  string
		left  Value
		right Value
		want  bool
	}{
		{"null-null", Null{}, Null{}, true},
		{"null-bool", Null{}, Bool{Value: false}, false},
		{"smallint-int-same", SmallInt{Value: 5}, Int{Value: big.NewInt(5)}, true},
		{"string-type-name", String{Value: "Widget"}, Type{Name: "Widget"}, true},
		{"string-type-mismatch", String{Value: "x"}, Type{Name: "Widget"}, false},
		// Type-vs-Class is name-only on both backends (finding 2.5 pinned here).
		{"type-bytecodeclass-name", Type{Name: "Widget"}, BytecodeClass{Module: "m", Name: "Widget", Index: 3}, true},
		{"bytecodeclass-type-name", BytecodeClass{Module: "m", Name: "Widget", Index: 3}, Type{Name: "Widget"}, true},
		{"bytecodeclass-type-mismatch", BytecodeClass{Module: "m", Name: "Widget"}, Type{Name: "Gadget"}, false},
		// Same-name different-module classes are still equal as class values only by full identity.
		{"bytecodeclass-module-aware", BytecodeClass{Module: "m", Name: "Widget"}, BytecodeClass{Module: "n", Name: "Widget"}, false},
		{"complex-equal", &Complex{C: complex(1, 2)}, &Complex{C: complex(1, 2)}, true},
		{"complex-diff", &Complex{C: complex(1, 2)}, &Complex{C: complex(1, 3)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, handled := LeafValuesEqual(c.left, c.right)
			if !handled {
				t.Fatalf("LeafValuesEqual(%s) returned handled=false", c.name)
			}
			if got != c.want {
				t.Fatalf("LeafValuesEqual(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// The leaf helper must be symmetric: equality does not depend on operand order.
func TestLeafValuesEqualSymmetric(t *testing.T) {
	values := leafMatrix()
	for _, a := range values {
		for _, b := range values {
			ab, hab := LeafValuesEqual(a, b)
			ba, hba := LeafValuesEqual(b, a)
			if hab && hba && ab != ba {
				t.Fatalf("asymmetric: LeafValuesEqual(%s,%s)=%v but (%s,%s)=%v", a.Inspect(), b.Inspect(), ab, b.Inspect(), a.Inspect(), ba)
			}
		}
	}
}

// Containers/instances/enums are the caller's job, not the leaf helper's.
func TestLeafValuesEqualDelegatesContainers(t *testing.T) {
	for _, v := range []Value{
		&List{Elements: []Value{SmallInt{Value: 1}}},
		Dict{Entries: map[string]DictEntry{}},
		Set{Elements: map[string]SetEntry{}},
	} {
		if _, handled := LeafValuesEqual(v, v); handled {
			t.Fatalf("LeafValuesEqual should delegate %s to the caller", v.TypeName())
		}
	}
}

// runtime.ValuesEqual (the assertEquals path) compares instances structurally at every nesting depth, not by identity (finding 9.6 canonical behavior).
func TestValuesEqualNestedInstances(t *testing.T) {
	cls := &Class{Name: "Point"}
	inst := func(x int64) *Instance {
		return &Instance{Class: cls, Fields: map[string]Value{"x": SmallInt{Value: x}}}
	}
	one, oneAgain, two := inst(1), inst(1), inst(2)
	if !ValuesEqual(one, oneAgain) {
		t.Fatal("distinct structurally-equal instances should compare equal at top level")
	}
	list := func(v Value) *List { return &List{Elements: []Value{v}} }
	if !ValuesEqual(list(one), list(oneAgain)) {
		t.Fatal("list of structurally-equal instances should compare equal")
	}
	if ValuesEqual(list(one), list(two)) {
		t.Fatal("list of unequal instances should not compare equal")
	}
	if !ValuesEqual(list(list(one)), list(list(oneAgain))) {
		t.Fatal("nested list of structurally-equal instances should compare equal")
	}
	if ValuesEqual(list(list(one)), list(list(two))) {
		t.Fatal("nested list of unequal instances should not compare equal")
	}
}

// A NativeObject with an uncomparable payload (serveFile marker) must compare without panicking (finding 2.6).
func TestNativeObjectsEqualUncomparablePayload(t *testing.T) {
	marker := func(path string) NativeObject {
		return NativeObject{Kind: "serveFile", Payload: Dict{Entries: map[string]DictEntry{
			"path": {Key: String{Value: "path"}, Value: String{Value: path}},
		}}}
	}
	if eq, _ := LeafValuesEqual(marker("/a"), marker("/a")); !eq {
		t.Fatal("equal serveFile markers should compare equal")
	}
	if eq, _ := LeafValuesEqual(marker("/a"), marker("/b")); eq {
		t.Fatal("distinct serveFile markers should not compare equal")
	}
}

// The comparable-payload path preserves the historical Go == identity behavior.
func TestNativeObjectsEqualComparablePayload(t *testing.T) {
	a := NativeObject{Kind: "handle", ID: 7, Payload: "x"}
	if !nativeObjectsEqual(a, NativeObject{Kind: "handle", ID: 7, Payload: "x"}) {
		t.Fatal("identical comparable native objects should be equal")
	}
	if nativeObjectsEqual(a, NativeObject{Kind: "handle", ID: 8, Payload: "x"}) {
		t.Fatal("different ID should not be equal")
	}
	if nativeObjectsEqual(a, NativeObject{Kind: "handle", ID: 7, Payload: "y"}) {
		t.Fatal("different comparable payload should not be equal")
	}
	if !nativeObjectsEqual(NativeObject{Kind: "h", ID: 1}, NativeObject{Kind: "h", ID: 1}) {
		t.Fatal("nil payloads should be equal")
	}
}
