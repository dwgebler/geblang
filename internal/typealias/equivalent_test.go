package typealias

import "testing"

func TestEquivalentNamesPreservesNestedReifiedShape(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		want        bool
	}{
		{"Item", "entities.Item", false},
		{"list<entities.Item>", "list<Item>", false},
		{"dict<string,list<entities.Item>>", "dict<string, list<entities.Item>>", true},
		{"?int[]", "?list<int>", true},
		{"list<first.Item>", "list<second.Item>", false},
		{"int|string", "string|int", true},
		{"int|int", "int", true},
		{"int&int", "int", true},
		{"first.Item&second.Other", "second.Other&first.Item", true},
		{"list<?int>", "?list<int>", false},
		{"Box<list<int>>", "Box<list<string>>", false},
		{"list<entities.Item>", "list<entities.Other>", false},
	} {
		t.Run(tc.left+"_"+tc.right, func(t *testing.T) {
			if got := EquivalentNames(tc.left, tc.right); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
