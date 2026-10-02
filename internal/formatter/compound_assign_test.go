package formatter_test

import "testing"

func TestFormatKeepsCompoundAssignment(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"plus", "x += 2;\n", "x += 2;\n"},
		{"explicit form is untouched", "x = x + 2;\n", "x = x + 2;\n"},
		{"minus on an index", "d[\"a\"] -= 1;\n", "d[\"a\"] -= 1;\n"},
		{"times on a list slot", "xs[0] *= 3;\n", "xs[0] *= 3;\n"},
		{"divide", "x /= 2;\n", "x /= 2;\n"},
		{"integer divide", "x //= 2;\n", "x //= 2;\n"},
		{"modulo", "x %= 2;\n", "x %= 2;\n"},
		{"power", "x **= 2;\n", "x **= 2;\n"},
		{"bit and", "x &= 2;\n", "x &= 2;\n"},
		{"bit or", "x |= 2;\n", "x |= 2;\n"},
		{"bit xor", "x ^= 2;\n", "x ^= 2;\n"},
		{"shift left", "x <<= 1;\n", "x <<= 1;\n"},
		{"shift right", "x >>= 1;\n", "x >>= 1;\n"},
		{"null coalesce", "n ??= 4;\n", "n ??= 4;\n"},
		{"ternary right side", "x += n > 1 ? 1 : 2;\n", "x += n > 1 ? 1 : 2;\n"},
		{"spacing is normalised", "x+=2;\n", "x += 2;\n"},
		{
			"field inside a method",
			"class C {\n    int v = 0;\n    func bump(): void {\n        this.v += 1;\n        this.v = this.v + 1;\n    }\n}\n",
			"class C {\n    int v = 0;\n    func bump(): void {\n        this.v += 1;\n        this.v = this.v + 1;\n    }\n}\n",
		},
		{
			"c-style loop update",
			"for (let int i = 0; i < 3; i += 1) {\n    total += i;\n}\n",
			"for (let int i = 0; i < 3; i += 1) {\n    total += i;\n}\n",
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
