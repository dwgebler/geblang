package runtime

import "testing"

func TestCloneFunctionDeepCopiesCapturedEnvironment(t *testing.T) {
	outer := NewEnvironment()
	sharedList := &List{Elements: []Value{NewInt64(1)}}
	if err := outer.Define("items", sharedList, false); err != nil {
		t.Fatal(err)
	}

	original := Function{Name: "handler", Env: outer}
	cloned := CloneFunction(original)

	clonedListValue, ok := cloned.Env.Get("items")
	if !ok {
		t.Fatal("cloned environment is missing captured list")
	}
	clonedList, ok := clonedListValue.(*List)
	if !ok {
		t.Fatalf("cloned captured value has type %T, want List", clonedListValue)
	}
	clonedList.Elements[0] = NewInt64(99)

	originalListValue, ok := original.Env.Get("items")
	if !ok {
		t.Fatal("original environment is missing captured list")
	}
	originalList := originalListValue.(*List)
	if got := originalList.Elements[0].(Int).Value.Int64(); got != 1 {
		t.Fatalf("original captured list was mutated: got %d, want 1", got)
	}
}

func TestCloneModulePreservesCanonical(t *testing.T) {
	module := &Module{Name: "native", Canonical: "async.sync", Exports: map[string]Value{}}
	cloned, ok := CloneValue(module).(*Module)
	if !ok {
		t.Fatalf("CloneValue returned %T, want *Module", cloned)
	}
	if cloned.Canonical != "async.sync" {
		t.Fatalf("cloned module Canonical = %q, want %q", cloned.Canonical, "async.sync")
	}

	env := NewEnvironment()
	if err := env.Define("native", module, false); err != nil {
		t.Fatal(err)
	}
	fn := CloneFunction(Function{Name: "handler", Env: env})
	got, ok := fn.Env.Get("native")
	if !ok {
		t.Fatal("cloned environment is missing the native alias binding")
	}
	gotModule, ok := got.(*Module)
	if !ok {
		t.Fatalf("cloned binding has type %T, want *Module", got)
	}
	if gotModule.Canonical != "async.sync" {
		t.Fatalf("cloned env module Canonical = %q, want %q", gotModule.Canonical, "async.sync")
	}
}

func TestCloneFunctionPreservesEnvironmentCycles(t *testing.T) {
	env := NewEnvironment()
	fn := Function{Name: "handler", Env: env}
	if err := env.Define("handler", fn, true); err != nil {
		t.Fatal(err)
	}

	cloned := CloneFunction(fn)
	captured, ok := cloned.Env.Get("handler")
	if !ok {
		t.Fatal("cloned environment is missing recursive function binding")
	}
	capturedFn, ok := captured.(Function)
	if !ok {
		t.Fatalf("captured handler has type %T, want Function", captured)
	}
	if capturedFn.Env != cloned.Env {
		t.Fatal("recursive function binding does not point at the cloned environment")
	}
}

func TestCloneCyclicDictTerminates(t *testing.T) {
	d := NewDict()
	d.PutEntry("self", DictEntry{Key: String{Value: "self"}, Value: d})

	cloned := CloneValue(d).(Dict)
	var self Value
	cloned.ForEachEntry(func(k string, e DictEntry) bool {
		if k == "self" {
			self = e.Value
		}
		return true
	})
	if self == nil {
		t.Fatal("cloned cyclic dict lost its self entry")
	}
	if dictIdentity(self.(Dict)) != dictIdentity(cloned) {
		t.Fatal("cyclic dict self-reference was split, not preserved")
	}
}

func TestFrozenDictionarySnapshotKeepsCycleViewFlags(t *testing.T) {
	source := NewDict()
	source.PutEntry("sself", DictEntry{Key: String{Value: "self"}, Value: source})
	frozen := ShallowFreeze(source)

	snapshot := CloneSnapshot(frozen).(Dict)
	self, ok := snapshot.GetStringEntry("self")

	if !ok || !ValuesIdentical(snapshot, self.Value) || !IsFrozen(snapshot) || IsFrozen(self.Value) || IsFrozen(source) {
		t.Fatal("snapshot changed cycle identity or per-view frozen flags")
	}
}

func TestCloneDictAliasClonedOnce(t *testing.T) {
	d := NewDict()
	d.PutEntry("k", DictEntry{Key: String{Value: "k"}, Value: NewInt64(1)})
	list := &List{Elements: []Value{d, d}}

	cloned := CloneValue(list).(*List)
	if dictIdentity(cloned.Elements[0].(Dict)) != dictIdentity(cloned.Elements[1].(Dict)) {
		t.Fatal("a dict aliased twice was split into two clones")
	}
}

func TestCloneCyclicBytecodeCellTerminates(t *testing.T) {
	c1 := &BytecodeCell{}
	c2 := &BytecodeCell{}
	c1.Value = c2
	c2.Value = c1

	cloned := CloneValue(c1).(*BytecodeCell)
	if cloned.Value.(*BytecodeCell).Value.(*BytecodeCell) != cloned {
		t.Fatal("cyclic bytecode cell was split, not preserved")
	}
}
