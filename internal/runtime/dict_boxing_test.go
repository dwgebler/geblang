package runtime

import "testing"

var dictionaryValueSink Value

func TestDictionaryValueConversionDoesNotAllocate(t *testing.T) {
	dict := NewDictHint(2)

	allocations := testing.AllocsPerRun(100, func() { dictionaryValueSink = dict })

	if allocations != 0 {
		t.Fatalf("converting dictionary to Value allocated %g objects", allocations)
	}
}

func BenchmarkDictionaryValue(b *testing.B) {
	entry := DictEntry{Key: String{Value: "n"}, Value: SmallInt{Value: 1000}}
	b.ReportAllocs()
	for b.Loop() {
		dict := NewDictHint(2)
		dict.PutEntry("sn", entry)
		dictionaryValueSink = dict
	}
}
