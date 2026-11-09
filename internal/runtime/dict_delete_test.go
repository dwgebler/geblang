package runtime

import (
	"fmt"
	"strings"
	"testing"
)

func dictKeysString(d Dict) string {
	var parts []string
	d.ForEachEntry(func(_ string, entry DictEntry) bool {
		parts = append(parts, entry.Key.(String).Value)
		return true
	})
	return strings.Join(parts, ",")
}

func putString(d Dict, key string, value int64) {
	d.PutEntry(DictStringKeyPrefix+key, DictEntry{Key: String{Value: key}, Value: SmallInt{Value: value}})
}

func TestDictDeletePreservesOrderAndReinsertAppends(t *testing.T) {
	d := NewDictHint(0)
	for i := 0; i < 20; i++ {
		putString(d, fmt.Sprintf("k%d", i), int64(i))
	}
	for i := 0; i < 20; i += 2 {
		d.DelEntry(DictStringKeyPrefix + fmt.Sprintf("k%d", i))
	}
	putString(d, "k4", 99)
	want := "k1,k3,k5,k7,k9,k11,k13,k15,k17,k19,k4"
	if got := dictKeysString(d); got != want || d.Len() != 11 {
		t.Fatalf("order after deletes = %q (len %d), want %q", got, d.Len(), want)
	}
	if entry, ok := d.GetStringEntry("k4"); !ok || entry.Value.(SmallInt).Value != 99 {
		t.Fatalf("reinserted key lookup = %v %v", entry, ok)
	}
	if strings.Join(d.OrderedKeys(), ",") != "sk1,sk3,sk5,sk7,sk9,sk11,sk13,sk15,sk17,sk19,sk4" {
		t.Fatalf("OrderedKeys = %v", d.OrderedKeys())
	}
}

func TestDictDeleteCompactionKeepsOrder(t *testing.T) {
	d := NewDictHint(0)
	for i := 0; i < 1000; i++ {
		putString(d, fmt.Sprintf("k%d", i), int64(i))
	}
	for i := 0; i < 1000; i++ {
		if i%10 != 0 {
			d.DelEntry(DictStringKeyPrefix + fmt.Sprintf("k%d", i))
		}
	}
	var want []string
	for i := 0; i < 1000; i += 10 {
		want = append(want, fmt.Sprintf("k%d", i))
	}
	if got := dictKeysString(d); got != strings.Join(want, ",") || d.Len() != 100 {
		t.Fatalf("order after compaction diverged (len %d)", d.Len())
	}
	for _, k := range want {
		if _, ok := d.GetStringEntry(k); !ok {
			t.Fatalf("lost %s after compaction", k)
		}
	}
	d.DelEntry(DictStringKeyPrefix + "missing")
	if d.Len() != 100 {
		t.Fatalf("deleting a missing key changed length")
	}
}
