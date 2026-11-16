package formatter_test

import "testing"

// The first statement after the module/import header follows the same blank-line rule as any other: an author's blank is kept, none is added.
func TestFormatKeepsBlankLineAfterHeader(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"documented const":         {"module m;\n\n/** Doc. */\nexport const A = 1;\n", "module m;\n\n/** Doc. */\nexport const A = 1;\n"},
		"compact documented const": {"module m;\n/** Doc. */\nexport const A = 1;\n", "module m;\n/** Doc. */\nexport const A = 1;\n"},
		"plain const":              {"module m;\n\nconst A = 1;\n", "module m;\n\nconst A = 1;\n"},
		"comment before let":       {"module m;\n\n# note\nlet a = 1;\n", "module m;\n\n# note\nlet a = 1;\n"},
		"after imports":            {"module m;\nimport io;\n\nlet a = 1;\nio.println(a);\n", "module m;\nimport io;\n\nlet a = 1;\nio.println(a);\n"},
		"compact script":           {"import io;\nio.println(1);\n", "import io;\nio.println(1);\n"},
		"header stays compact":     {"module m;\n\nimport io;\n", "module m;\nimport io;\n"},
		"function always blank":    {"module m;\nfunc f(): int { return 1; }\n", "module m;\n\nfunc f(): int {\n    return 1;\n}\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := roundtrip(t, c.src)
			parseOK(t, got)
			if got != c.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}
