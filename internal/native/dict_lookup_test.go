package native

import (
	"fmt"
	"testing"

	"geblang/internal/runtime"
)

func TestLookupDictEntry_StringKeysPreserveTypesAndStorage(t *testing.T) {
	for _, size := range []int{0, 8, 12} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/legacy=%t", size, legacy), func(t *testing.T) {
				dict := runtime.NewDictHint(size)
				if legacy {
					dict = runtime.Dict{Entries: map[string]runtime.DictEntry{}}
				}
				keys := []runtime.Value{runtime.String{Value: ""}, runtime.String{Value: "1"}, runtime.SmallInt{Value: 1}, runtime.String{Value: "s\x00\u65e5\u672c\u8a9e"}}
				for i := 0; i < size; i++ {
					keys = append(keys, runtime.String{Value: fmt.Sprintf("padding-%d", i)})
				}
				for i, key := range keys {
					dict.PutEntry(DictKey(key), runtime.DictEntry{Key: key, Value: runtime.SmallInt{Value: int64(i)}})
				}

				for i, key := range keys {
					entry, ok := LookupDictEntry(dict, key)
					if !ok || entry.Value != (runtime.SmallInt{Value: int64(i)}) {
						t.Fatalf("key %v: got %v, %t", key, entry.Value, ok)
					}
				}
				if _, ok := LookupDictEntry(dict, runtime.String{Value: "missing"}); ok {
					t.Fatal("missing key found")
				}
				dict.DelEntry(DictKey(keys[1]))
				if _, ok := LookupDictEntry(dict, keys[1]); ok {
					t.Fatal("deleted key found")
				}
				dict.PutEntry(DictKey(keys[1]), runtime.DictEntry{Key: keys[1], Value: runtime.SmallInt{Value: 99}})
				if entry, ok := LookupDictEntry(dict, keys[1]); !ok || entry.Value != (runtime.SmallInt{Value: 99}) {
					t.Fatal("reinserted value lost")
				}
			})
		}
	}
}

var dictLookupResult runtime.DictEntry

func BenchmarkStringDictLookup(b *testing.B) {
	for _, size := range []int{4, 16} {
		dict := runtime.NewDictHint(size)
		for i := 0; i < size; i++ {
			key := runtime.String{Value: fmt.Sprintf("key-%d", i)}
			dict.PutEntry(DictKey(key), runtime.DictEntry{Key: key, Value: runtime.SmallInt{Value: int64(i)}})
		}
		var key runtime.Value = runtime.String{Value: "key-3"}
		b.Run(fmt.Sprintf("size=%d/encoded", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dictLookupResult, _ = dict.GetEntry(DictKey(key))
			}
		})
		b.Run(fmt.Sprintf("size=%d/direct", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dictLookupResult, _ = LookupDictEntry(dict, key)
			}
		})
	}
}
