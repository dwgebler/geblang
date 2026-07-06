package bytecode

import (
	"math/big"
	"testing"

	"geblang/internal/runtime"
)

// leafEqualityMatrix is the value set both the VM == and assertEquals must agree on.
func leafEqualityMatrix() []runtime.Value {
	return []runtime.Value{
		runtime.Null{},
		runtime.Bool{Value: true},
		runtime.Bool{Value: false},
		runtime.SmallInt{Value: 1},
		runtime.SmallInt{Value: 2},
		runtime.Int{Value: big.NewInt(1)},
		runtime.Int{Value: big.NewInt(2)},
		runtime.Decimal{Value: big.NewRat(1, 1)},
		runtime.Float{Value: 1.0},
		runtime.Float{Value: 1.5},
		runtime.String{Value: "a"},
		runtime.String{Value: "Widget"},
		runtime.Bytes{Value: []byte{1, 2}},
		runtime.Type{Name: "Widget"},
		runtime.Type{Name: "Gadget"},
		runtime.BytecodeClass{Module: "m", Name: "Widget", Index: 0},
		runtime.BytecodeClass{Module: "n", Name: "Widget", Index: 0},
		&runtime.Complex{C: complex(1, 2)},
		&runtime.Complex{C: complex(1, 3)},
		markerObject("/a"),
		markerObject("/b"),
	}
}

// markerObject mimics the serveFile marker: a NativeObject with an uncomparable Dict payload.
func markerObject(path string) runtime.NativeObject {
	return runtime.NativeObject{Kind: "serveFile", Payload: runtime.Dict{Entries: map[string]runtime.DictEntry{
		"path": {Key: runtime.String{Value: "path"}, Value: runtime.String{Value: path}},
	}}}
}

// The VM's structural helper agrees with the canonical runtime equality for instances nested in containers (finding 9.6).
func TestVMNestedInstanceEqualityMatchesCanonical(t *testing.T) {
	cls := &runtime.Class{Name: "Point"}
	inst := func(x int64) *runtime.Instance {
		return &runtime.Instance{Class: cls, Fields: map[string]runtime.Value{"x": runtime.SmallInt{Value: x}}}
	}
	list := func(v runtime.Value) *runtime.List { return &runtime.List{Elements: []runtime.Value{v}} }
	one, oneAgain, two := inst(1), inst(1), inst(2)
	cases := []struct {
		left, right runtime.Value
	}{
		{one, oneAgain},
		{one, two},
		{list(one), list(oneAgain)},
		{list(one), list(two)},
		{list(list(one)), list(list(oneAgain))},
		{list(list(one)), list(list(two))},
	}
	for _, c := range cases {
		if vm, canonical := valuesEqual(c.left, c.right), runtime.ValuesEqual(c.left, c.right); vm != canonical {
			t.Fatalf("VM valuesEqual=%v but runtime.ValuesEqual=%v for %s vs %s", vm, canonical, c.left.Inspect(), c.right.Inspect())
		}
	}
}

// TestVMEqualityMatchesCanonical fails if the VM's == drifts from the shared runtime equality (assertEquals path) for any leaf pair (finding 6.2).
func TestVMEqualityMatchesCanonical(t *testing.T) {
	values := leafEqualityMatrix()
	for _, a := range values {
		for _, b := range values {
			vm := valuesEqual(a, b)
			canonical := runtime.ValuesEqual(a, b)
			if vm != canonical {
				t.Fatalf("VM ==(%s,%s)=%v but runtime.ValuesEqual=%v", a.Inspect(), b.Inspect(), vm, canonical)
			}
		}
	}
}
