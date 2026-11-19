package formatter_test

import (
	"testing"

	"geblang/internal/formatter"
)

func TestFormatKeepsAlignedColumns(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"aligned = in let declarations",
			"let pi    = 3.14159;\nlet big   = 1;\nlet label = \"Ada\";\n",
			"let pi    = 3.14159;\nlet big   = 1;\nlet label = \"Ada\";\n",
		},
		{
			"aligned names and = in typed declarations",
			"int     count  = 10;\ndecimal money  = 12.50;\nfloat   ratio  = 0.25f;\n",
			"int     count = 10;\ndecimal money = 12.50;\nfloat   ratio = 0.25f;\n",
		},
		{
			"aligned assignments",
			"x    = 1;\nlong = 2;\n",
			"x    = 1;\nlong = 2;\n",
		},
		{
			"aligned compound assignments",
			"x    += 1;\nlong += 2;\n",
			"x    += 1;\nlong += 2;\n",
		},
		{
			"aligned class fields",
			"class C {\n    int    a  = 1;\n    string bb = \"x\";\n}\n",
			"class C {\n    int    a  = 1;\n    string bb = \"x\";\n}\n",
		},
		{
			"aligned code and comments together",
			"let wide  = 1e308f;   # float\nlet exact = 1.5e-3;   # decimal\n",
			"let wide  = 1e308f;   # float\nlet exact = 1.5e-3;   # decimal\n",
		},
		{
			"aligned match arms",
			"let m = match (d) {\n    case Direction.North => \"n\";\n    case Direction.East  => \"e\";\n    default              => \"x\";\n};\n",
			"let m = match (d) {\n    case Direction.North => \"n\";\n    case Direction.East  => \"e\";\n    default              => \"x\";\n};\n",
		},
		{
			"unaligned match arm spacing is normalised",
			"let n = match (d) {\n    case 1   => \"a\";\n    case 22 => \"b\";\n};\n",
			"let n = match (d) {\n    case 1 => \"a\";\n    case 22 => \"b\";\n};\n",
		},
		{
			"aligned dict stays aligned when an entry is wrapped",
			"let r = req({\n    \"method\":  \"POST\",\n    \"headers\": {\"Content-Type\": \"application/json\", \"Authorization\": \"Bearer \" + this.apiKey, \"X\": 1},\n    \"body\":    payload\n});\n",
			"let r = req({\n    \"method\":  \"POST\",\n    \"headers\": {\n        \"Content-Type\": \"application/json\",\n        \"Authorization\": \"Bearer \" + this.apiKey,\n        \"X\": 1,\n    },\n    \"body\":    payload,\n});\n",
		},
		{
			"aligned group is re-padded when an earlier part of a line changes",
			"list<int|string> a    = [];\nint              bbbb = 1;\n",
			"list<int | string> a    = [];\nint                bbbb = 1;\n",
		},
		{
			"aligned value wrapped in parentheses",
			"let m = {\n    \"body\":   m[\"value\"],\n    \"id\":     (m[\"offset\"] as string),\n    \"handle\": m[\"offset\"],\n};\n",
			"let m = {\n    \"body\":   m[\"value\"],\n    \"id\":     (m[\"offset\"] as string),\n    \"handle\": m[\"offset\"],\n};\n",
		},
		{
			"aligned named arguments in a broken call",
			"let c = make(\n    width:  10,\n    height: 20\n);\n",
			"let c = make(\n    width:  10,\n    height: 20\n);\n",
		},
		{"stray spacing on a lone line is normalised", "let   x   =   42;\n", "let x = 42;\n"},
		{"stray spacing before = is normalised", "x    = 1;\n", "x = 1;\n"},
		{
			"mixed assignment operators the author aligned stay aligned",
			"x    = 1;\nyyy  += 2;\n",
			"x   = 1;\nyyy += 2;\n",
		},
		{
			"assignments in different columns are not aligned",
			"x  = 1;\nyyy  += 2;\n",
			"x = 1;\nyyy += 2;\n",
		},
		{
			"a blank line breaks the alignment block",
			"let a    = 1;\n\nlet bbbb = 2;\n",
			"let a = 1;\n\nlet bbbb = 2;\n",
		},
	}
	for _, tc := range cases {
		got := roundtrip(t, tc.src)
		parseOK(t, got)
		if got != tc.want {
			t.Fatalf("%s:\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
		}
	}
}

func TestFormatCleanCollapsesAlignedColumns(t *testing.T) {
	out, err := formatter.FormatWithOptions([]byte("let pi    = 3.14159;\nlet label = \"Ada\";\n"), formatter.Options{Clean: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "let pi = 3.14159;\nlet label = \"Ada\";\n" {
		t.Fatalf("got %q", got)
	}
}
