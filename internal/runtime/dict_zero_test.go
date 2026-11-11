package runtime

import "testing"

func TestDictWithoutEntriesAllocatesOnFirstWriteForAllAliases(t *testing.T) {
	original := DictFromEntries(nil, nil)
	alias := original
	alias.PutEntry("sk", DictEntry{Key: String{Value: "k"}, Value: NewInt64(1)})
	if original.Len() != 1 || alias.Len() != 1 {
		t.Fatalf("lengths = %d/%d, want 1/1", original.Len(), alias.Len())
	}
	if got := original.EntryValue("sk"); got == nil || got.Inspect() != "1" {
		t.Fatalf("original sees %v, want 1", got)
	}
}

func TestZeroDictWritePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("write to zero Dict did not panic")
		}
	}()
	Dict{}.PutEntry("sk", DictEntry{Key: String{Value: "k"}, Value: NewInt64(1)})
}
