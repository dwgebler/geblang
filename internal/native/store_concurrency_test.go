package native_test

import (
	"fmt"
	"sync"
	"testing"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

// TestStoreConcurrentIncrIsRaceFree is the proof that the Store is the safe
// shared-state mechanism: concurrent atomic increments lose no updates and do
// not race (run with -race). A plain Geblang dict mutated the same way fatally
// crashes the process (concurrent map read/write).
func TestStoreConcurrentIncrIsRaceFree(t *testing.T) {
	r := native.NewBuiltinRegistry()
	h, err := r.Call("store", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 64
	const perG = 250
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				if _, err := r.Call("store", "incr", []runtime.Value{h, runtime.String{Value: "k"}, runtime.SmallInt{Value: 1}}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, err := r.Call("store", "get", []runtime.Value{h, runtime.String{Value: "k"}})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(goroutines * perG)
	si, ok := got.(runtime.SmallInt)
	if !ok || si.Value != want {
		t.Fatalf("incr lost updates: got %v, want %d", got, want)
	}
}

// TestStoreConcurrentMixedAccessIsRaceFree hammers set/get/delete on many keys
// from many goroutines: the read+write mix that fatally crashes a plain map.
func TestStoreConcurrentMixedAccessIsRaceFree(t *testing.T) {
	r := native.NewBuiltinRegistry()
	h, err := r.Call("store", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 32
	const perG = 300
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				key := runtime.String{Value: fmt.Sprintf("k%d", (base+j)%17)}
				_, _ = r.Call("store", "set", []runtime.Value{h, key, runtime.SmallInt{Value: int64(j)}})
				_, _ = r.Call("store", "get", []runtime.Value{h, key})
				_, _ = r.Call("store", "keys", []runtime.Value{h})
				if j%3 == 0 {
					_, _ = r.Call("store", "delete", []runtime.Value{h, key})
				}
			}
		}(i * 7)
	}
	wg.Wait()
}

// TestStoreSharesDeepFrozenValues checks that a deeply frozen value is stored
// and returned by reference, while a shallow-frozen one is copied but stays
// frozen.
func TestStoreSharesDeepFrozenValues(t *testing.T) {
	r := native.NewBuiltinRegistry()
	h, err := r.Call("store", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	deep := runtime.DeepFreeze(&runtime.List{Elements: []runtime.Value{inner}}).(*runtime.List)
	if !runtime.IsDeepFrozen(deep) {
		t.Fatal("DeepFreeze result is not deeply frozen")
	}
	if _, err := r.Call("store", "set", []runtime.Value{h, runtime.String{Value: "deep"}, deep}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Call("store", "get", []runtime.Value{h, runtime.String{Value: "deep"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.(*runtime.List) != deep {
		t.Fatal("deeply frozen value was copied instead of shared")
	}

	shallow := runtime.ShallowFreeze(&runtime.List{Elements: []runtime.Value{inner}}).(*runtime.List)
	if runtime.IsDeepFrozen(shallow) {
		t.Fatal("shallow-frozen list with a mutable element reported deeply frozen")
	}
	if _, err := r.Call("store", "set", []runtime.Value{h, runtime.String{Value: "shallow"}, shallow}); err != nil {
		t.Fatal(err)
	}
	got, err = r.Call("store", "get", []runtime.Value{h, runtime.String{Value: "shallow"}})
	if err != nil {
		t.Fatal(err)
	}
	list := got.(*runtime.List)
	if list == shallow || !list.Frozen {
		t.Fatalf("shallow-frozen value should be a frozen copy (same=%v frozen=%v)", list == shallow, list.Frozen)
	}
	if list.Elements[0].(*runtime.List) == inner {
		t.Fatal("mutable nested value was shared")
	}
}

// TestStoreConcurrentDeepFrozenReadsAreRaceFree reads one shared deeply frozen
// value from many goroutines (run with -race).
func TestStoreConcurrentDeepFrozenReadsAreRaceFree(t *testing.T) {
	r := native.NewBuiltinRegistry()
	h, err := r.Call("store", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	d := runtime.NewDict()
	for i := 0; i < 50; i++ {
		d.PutEntry(fmt.Sprintf("k%d", i), runtime.DictEntry{Key: runtime.String{Value: fmt.Sprintf("k%d", i)}, Value: &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: int64(i)}}}})
	}
	frozen := runtime.DeepFreeze(d)
	if _, err := r.Call("store", "set", []runtime.Value{h, runtime.String{Value: "m"}, frozen}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				v, err := r.Call("store", "get", []runtime.Value{h, runtime.String{Value: "m"}})
				if err != nil {
					t.Error(err)
					return
				}
				sum := int64(0)
				v.(runtime.Dict).ForEachEntry(func(_ string, e runtime.DictEntry) bool {
					sum += e.Value.(*runtime.List).Elements[0].(runtime.SmallInt).Value
					return true
				})
				if sum != 1225 {
					t.Errorf("sum = %d", sum)
					return
				}
			}
		}()
	}
	wg.Wait()
}
