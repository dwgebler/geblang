package runtime

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDictionaryAliasesPreserveOrderAndKeyTypesThroughGrowth(t *testing.T) {
	value := NewDictHint(2)
	alias := value
	for i := 0; i < 20; i++ {
		key := fmt.Sprint(i)
		value.PutEntry("s"+key, DictEntry{Key: String{Value: key}, Value: NewInt64(int64(i))})
	}
	if !alias.HasOnlyStringKeys() {
		t.Fatal("string keys rejected after growth")
	}
	alias.PutEntry("i1", DictEntry{Key: NewInt64(1), Value: Null{}})
	if value.HasOnlyStringKeys() {
		t.Fatal("non-string insertion through alias ignored")
	}
	value.DelEntry("i1")
	if !alias.HasOnlyStringKeys() {
		t.Fatal("deleted non-string key still counted")
	}
	value.DelEntry("s0")
	value.PutEntry("s0", DictEntry{Key: String{Value: "0"}, Value: NewInt64(99)})
	keys := alias.EntryKeys()
	if len(keys) != 20 || keys[0] != "s1" || keys[19] != "s0" {
		t.Fatalf("order: %v", keys)
	}
	entry, ok := alias.GetStringEntry("0")
	if !ok || entry.Value.Inspect() != "99" {
		t.Fatalf("lookup: %v, %v", entry, ok)
	}
	alias.Clear()
	if value.Len() != 0 || !value.HasOnlyStringKeys() {
		t.Fatal("clear did not update shared store")
	}
	value.PutEntry("sx", DictEntry{Key: String{Value: "x"}, Value: Null{}})
	if !reflect.DeepEqual(alias.EntryKeys(), []string{"sx"}) {
		t.Fatal(alias.EntryKeys())
	}
}

func TestDictionaryKeyTypeTrackingHandlesReplacementAndLegacyStorage(t *testing.T) {
	for _, value := range []Dict{NewDictHint(1), {Entries: map[string]DictEntry{}}} {
		value.PutEntry("key", DictEntry{Key: NewInt64(1), Value: Null{}})
		if value.HasOnlyStringKeys() {
			t.Fatal("non-string key accepted")
		}
		value.PutEntry("key", DictEntry{Key: String{Value: "key"}, Value: Null{}})
		if !value.HasOnlyStringKeys() {
			t.Fatal("replacement key type ignored")
		}
	}
}

func BenchmarkDictionaryShapes(b *testing.B) {
	for _, size := range []int{0, 1, 2, 6, 8, 16, 100} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			keys := make([]string, size)
			for i := range keys {
				keys[i] = fmt.Sprint(i)
			}
			b.ReportAllocs()
			for b.Loop() {
				value := NewDictHint(size)
				for _, key := range keys {
					value.PutEntry("s"+key, DictEntry{Key: String{Value: key}, Value: Null{}})
				}
				for _, key := range keys {
					if _, ok := value.GetStringEntry(key); !ok {
						b.Fatal(key)
					}
				}
			}
		})
	}
}
