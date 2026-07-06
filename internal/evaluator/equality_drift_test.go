package evaluator

import (
	"io"
	"math/big"
	"testing"

	"geblang/internal/runtime"
)

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

// The evaluator's == (valuesEqual) compares a container-nested instance structurally, matching top level and the VM, not by identity (finding 9.6).
func TestEvaluatorNestedInstanceEqualityStructural(t *testing.T) {
	e := New(io.Discard)
	cls := &runtime.Class{Name: "Point", Methods: map[string][]runtime.Function{}}
	inst := func(x int64) *runtime.Instance {
		return &runtime.Instance{Class: cls, Fields: map[string]runtime.Value{"x": runtime.SmallInt{Value: x}}}
	}
	list := func(v runtime.Value) *runtime.List { return &runtime.List{Elements: []runtime.Value{v}} }
	one, oneAgain, two := inst(1), inst(1), inst(2)
	assertEqual := func(left, right runtime.Value, want bool) {
		t.Helper()
		got, err := e.valuesEqual(left, right)
		if err != nil {
			t.Fatalf("valuesEqual: %v", err)
		}
		if got != want {
			t.Fatalf("valuesEqual(%s, %s) = %v, want %v", left.Inspect(), right.Inspect(), got, want)
		}
	}
	assertEqual(list(one), list(oneAgain), true)
	assertEqual(list(one), list(two), false)
	assertEqual(list(list(one)), list(list(oneAgain)), true)
	assertEqual(list(list(one)), list(list(two)), false)
}

// TestEvaluatorEqualityMatchesCanonical fails if the evaluator's == drifts from the shared runtime equality (assertEquals path) for any leaf pair (finding 6.2).
func TestEvaluatorEqualityMatchesCanonical(t *testing.T) {
	values := leafEqualityMatrix()
	for _, a := range values {
		for _, b := range values {
			eval := primitiveEqual(a, b)
			canonical := runtime.ValuesEqual(a, b)
			if eval != canonical {
				t.Fatalf("evaluator ==(%s,%s)=%v but runtime.ValuesEqual=%v", a.Inspect(), b.Inspect(), eval, canonical)
			}
		}
	}
}
