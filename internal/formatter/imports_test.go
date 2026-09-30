package formatter_test

import "testing"

func TestFormatFromImports(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"single name", "from math import sqrt;\n", "from math import sqrt;\n"},
		{"several names", "from  dice  import Dice,simulate;\n", "from dice import Dice, simulate;\n"},
		{"name alias", "from bytes import toHex as hex;\n", "from bytes import toHex as hex;\n"},
		{"mixed aliases", "from math import sqrt, floor as fl, abs;\n", "from math import sqrt, floor as fl, abs;\n"},
		{"dotted path", "from a.b.c import D;\n", "from a.b.c import D;\n"},
		{"reserved namespace", "from geblang.bytes import toHex;\n", "from geblang.bytes import toHex;\n"},
		{"import reserved namespace", "import geblang.json;\n", "import geblang.json;\n"},
		{"import reserved namespace alias", "import geblang.json as j;\n", "import geblang.json as j;\n"},
		{
			"header group keeps order and comments",
			"module demo;\nfrom math import sqrt; # roots\nimport io;\n# bytes\nfrom bytes import toHex;\nlet x = 1;\n",
			"module demo;\nfrom math import sqrt; # roots\nimport io;\n# bytes\nfrom bytes import toHex;\nlet x = 1;\n",
		},
		{
			"inside a function body",
			"func f(): void {\nfrom math import abs;\nimport io;\nio.println(abs(-1));\n}\n",
			"func f(): void {\n    from math import abs;\n    import io;\n    io.println(abs(-1));\n}\n",
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
