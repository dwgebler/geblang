package formatter_test

import (
	"testing"

	"geblang/internal/formatter"
)

func TestFormatGeneratorComprehension(t *testing.T) {
	cases := map[string]string{
		"let g = (  x*2   for x in xs );\n":                "let g = (x * 2 for x in xs);\n",
		"let g = (x for int x in xs if x>0);\n":            "let g = (x for int x in xs if x > 0);\n",
		"let g = (a+b for a in xs for b in ys if a!=b);\n": "let g = (a + b for a in xs for b in ys if a != b);\n",
		"let g = (\"${k}=${v}\" for k, v in d);\n":         "let g = (\"${k}=${v}\" for k, v in d);\n",
		"let t = total((x for x in xs));\n":                "let t = total((x for x in xs));\n",
		"let n = (y for y in (x for x in xs));\n":          "let n = (y for y in (x for x in xs));\n",
	}
	for src, want := range cases {
		out, err := formatter.Format([]byte(src))
		if err != nil {
			t.Fatalf("Format(%q) error: %v", src, err)
		}
		if string(out) != want {
			t.Fatalf("Format(%q) = %q, want %q", src, string(out), want)
		}
	}
}
