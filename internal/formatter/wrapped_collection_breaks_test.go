package formatter_test

import (
	"testing"

	"geblang/internal/formatter"
)

func TestFormatKeepsAuthorBrokenCollectionsInsideWrappingExpressions(t *testing.T) {
	cases := []struct{ name, src string }{
		{"cast", "let a = {\n    \"k\": 1,\n    \"j\": 2,\n} as dict<string, any>;\n"},
		{"cast as the only call argument", "xs.push({\n    \"k\": 1,\n    \"j\": 2,\n} as dict<string, any>);\n"},
		{"null-coalescing operand", "let d = m ?? {\n    \"k\": 1,\n    \"j\": 2,\n};\n"},
		{"ternary branch", "let e = c ? {\n    \"k\": 1,\n    \"j\": 2,\n} : {};\n"},
		{"method receiver", "let g = [\n    1,\n    2,\n].length();\n"},
		{"logical operand", "let h = !c && [\n    1,\n    2,\n].contains(1);\n"},
		{"comments survive", "xs.push({\n    # first\n    \"k\": 1, # trailing\n    \"j\": 2,\n} as dict<string, any>);\n"},
		{"nested inside a broken list", "let rows = [\n    {\n        \"k\": 1,\n        \"j\": 2,\n    } as dict<string, any>,\n    {\"k\": 3} as dict<string, any>,\n];\n"},
		{"inside a block", "func f(list<any> xs): void {\n    xs.push({\n        \"k\": 1,\n        \"j\": 2,\n    } as dict<string, any>);\n}\n"},
	}
	for _, tc := range cases {
		got := roundtrip(t, tc.src)
		parseOK(t, got)
		if got != tc.src {
			t.Fatalf("%s:\ngot:\n%s\nwant:\n%s", tc.name, got, tc.src)
		}
	}
}

func TestFormatJoinsUnbrokenWrappedCollections(t *testing.T) {
	if got := roundtrip(t, "let a = {\"k\": 1, \"j\": 2} as dict<string, any>;\n"); got != "let a = {\"k\": 1, \"j\": 2} as dict<string, any>;\n" {
		t.Fatalf("got %q", got)
	}
	out, err := formatter.FormatWithOptions([]byte("let a = {\n    \"k\": 1,\n} as dict<string, any>;\n"), formatter.Options{Clean: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "let a = {\"k\": 1} as dict<string, any>;\n" {
		t.Fatalf("clean mode got %q", out)
	}
}
