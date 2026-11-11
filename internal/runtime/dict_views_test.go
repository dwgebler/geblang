package runtime

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDictionaryViewsShareEntriesAndKeepPrivateMetadata(t *testing.T) {
	for _, source := range []Dict{NewDictHint(2), DictFromEntries(map[string]DictEntry{}, &[]string{})} {
		typed := source.WithElementTypes([]string{"string", "int"})
		frozen := ShallowFreeze(typed).(Dict)
		widened := frozen.WithElementTypes([]string{"string", "any"})

		for i := 0; i < 20; i++ {
			key := fmt.Sprint(i)
			source.PutEntry("s"+key, DictEntry{Key: String{Value: key}, Value: SmallInt{Value: int64(i)}})
		}
		source.DelEntry("s0")

		if IsFrozen(source) || IsFrozen(typed) || !IsFrozen(frozen) || !IsFrozen(widened) {
			t.Fatal("view changed another view's frozen flag")
		}
		if len(source.ElementTypes) != 0 || !reflect.DeepEqual(typed.ElementTypes, []string{"string", "int"}) || !reflect.DeepEqual(frozen.ElementTypes, typed.ElementTypes) {
			t.Fatal("tagging changed another view's element types")
		}
		for _, view := range []Dict{typed, frozen, widened} {
			if !ValuesIdentical(source, view) || view.Len() != 19 || !reflect.DeepEqual(view.EntryKeys(), source.EntryKeys()) {
				t.Fatal("view lost shared entries or insertion order")
			}
		}
		source.Clear()
		if widened.Len() != 0 {
			t.Fatal("clear did not update another view")
		}
	}
}

func TestDictionaryFreezeAndSnapshotCopiesKeepOriginalIndependent(t *testing.T) {
	source := NewDictHint(1).WithElementTypes([]string{"string", "int"})
	source.PutEntry("sn", DictEntry{Key: String{Value: "n"}, Value: SmallInt{Value: 7}})

	frozen := FreezeShallowCopy(source).(Dict)
	deep := DeepFreeze(source).(Dict)
	snapshot := CloneSnapshot(frozen).(Dict)
	clone := CloneValue(frozen).(Dict)
	source.DelEntry("sn")

	for _, copy := range []Dict{frozen, deep, snapshot, clone} {
		if copy.Len() != 1 || ValuesIdentical(source, copy) || !reflect.DeepEqual(copy.ElementTypes, []string{"string", "int"}) {
			t.Fatal("copy lost entries, tags or independence")
		}
	}
	if IsFrozen(source) || IsFrozen(clone) || !IsFrozen(frozen) || !IsFrozen(snapshot) || !IsDeepFrozen(deep) {
		t.Fatal("copy lost frozen state")
	}
}

func TestZeroDictionaryAccessorsAndFreezeRemainEmpty(t *testing.T) {
	var source Dict

	for _, value := range []Value{source, FreezeShallowCopy(source), DeepFreeze(source), ShallowFreeze(source), CloneValue(source)} {
		dict := value.(Dict)
		if dict.Len() != 0 || dict.Inspect() != "{}" || len(dict.EntryKeys()) != 0 || !dict.HasOnlyStringKeys() {
			t.Fatalf("empty dictionary changed: %s", dict.Inspect())
		}
	}
	if IsFrozen(source) || !ValuesIdentical(source, source.WithElementTypes([]string{"string", "int"})) {
		t.Fatal("empty dictionary metadata changed its original state or identity")
	}
	left := NativeObject{Kind: "marker", Payload: source}
	right := NativeObject{Kind: "marker", Payload: source.WithElementTypes(nil)}
	if equal, handled := LeafValuesEqual(left, right); !handled || !equal {
		t.Fatal("empty dictionary header changed native payload equality")
	}
}

func TestNativeDictionaryPayloadEqualityKeepsTaggedCopySemantics(t *testing.T) {
	source := NewDictHint(1)
	source.PutEntry("sn", DictEntry{Key: String{Value: "n"}, Value: SmallInt{Value: 7}})
	typed := source.WithElementTypes([]string{"string", "int"})

	left := NativeObject{Kind: "serveFile", Payload: typed}
	right := NativeObject{Kind: "serveFile", Payload: CloneValue(typed)}

	if equal, handled := LeafValuesEqual(left, right); !handled || !equal {
		t.Fatal("cloning a tagged dictionary changed native payload equality")
	}
}
