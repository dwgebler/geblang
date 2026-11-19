package formatter_test

import (
	"testing"

	"geblang/internal/formatter"
)

func TestFormatKeepsGroupingParens(t *testing.T) {
	cases := []string{
		"let a = (2.5).round();\n",
		"let a = (3.14159f).round(2);\n",
		"let d = (\"abc\").upper();\n",
		"let c = (y).z;\n",
		"let e = (items[0]).name;\n",
		"let f = (load()).size;\n",
		"let g = (await job).value;\n",
		"let r = (1..3);\n",
		"let t = x ? (a) : (b);\n",
		"foo((x));\n",
		"let l = ([1, 2]).length();\n",
		"let b = (x + 1) * 2;\n",
		"if ((x > 1) && y) {\n    io.println(2);\n}\n",
		"func q(): int {\n    return (x);\n}\n",
		"let s = \"${(func(int x): int { return x; })(3)} ${(a + b) * 2} ${(n)}\";\n",
	}
	for _, src := range cases {
		got := roundtrip(t, src)
		parseOK(t, got)
		if got != src {
			t.Fatalf("got:\n%s\nwant:\n%s", got, src)
		}
	}
}

func TestFormatSyntaxParensAreNotDoubled(t *testing.T) {
	cases := []string{
		"if (x > 1) {\n    io.println(1);\n}\n",
		"while (x < 3) {\n    x = x + 1;\n}\n",
		"let m = match (x) {\n    case 1 => \"one\";\n    default => \"other\";\n};\n",
	}
	for _, src := range cases {
		if got := roundtrip(t, src); got != src {
			t.Fatalf("got:\n%s\nwant:\n%s", got, src)
		}
	}
}

func TestFormatCleanDropsGroupingParens(t *testing.T) {
	out, err := formatter.FormatWithOptions([]byte("let a = (2.5).round();\n"), formatter.Options{Clean: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "let a = 2.5.round();\n" {
		t.Fatalf("got %q", got)
	}
}
