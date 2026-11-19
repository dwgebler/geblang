package formatter_test

import "testing"

func TestFormatEmptyBodiesAndComments(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"empty class", "class A {}\n", "class A {}\n"},
		{"empty class written multi-line", "class A {\n}\n", "class A {}\n"},
		{"empty subclass", "class B extends A {\n}\n", "class B extends A {}\n"},
		{"empty interface", "interface I {}\n", "interface I {}\n"},
		{"empty function", "func f(): void {}\n", "func f(): void {}\n"},
		{
			"comment-only class keeps its comment inside",
			"class C {\n    # later\n}\n",
			"class C {\n    # later\n}\n",
		},
		{
			"comment-only interface",
			"interface I {\n    # later\n}\n",
			"interface I {\n    # later\n}\n",
		},
		{
			"comment-only function",
			"func f(): void {\n    # todo\n}\n",
			"func f(): void {\n    # todo\n}\n",
		},
		{
			"comment-only func literal",
			"let f = func(): void {\n    # todo\n};\n",
			"let f = func(): void {\n    # todo\n};\n",
		},
		{
			"comment after the last class member",
			"class E {\n    int a;\n\n    # end note\n}\n",
			"class E {\n    int a;\n\n    # end note\n}\n",
		},
		{
			"comment after the last interface member",
			"interface I {\n    func f(): void;\n    # tail\n}\n",
			"interface I {\n    func f(): void;\n    # tail\n}\n",
		},
		{
			"comment after the last enum variant",
			"enum Color {\n    Red,\n    Green\n    # tail\n}\n",
			"enum Color {\n    Red,\n    Green\n    # tail\n}\n",
		},
		{
			"comment after the last enum method",
			"enum Color {\n    Red;\n    func label(): string {\n        return \"r\";\n    }\n    # tail\n}\n",
			"enum Color {\n    Red;\n    func label(): string {\n        return \"r\";\n    }\n    # tail\n}\n",
		},
		{
			"comment after the last statement of a function",
			"func g(): void {\n    let x = 1;\n    # tail\n}\n",
			"func g(): void {\n    let x = 1;\n    # tail\n}\n",
		},
		{
			"comment after the last statement of an if",
			"if (true) {\n    let x = 1;\n    # tail\n}\n",
			"if (true) {\n    let x = 1;\n    # tail\n}\n",
		},
		{
			"blank line between two comments is kept",
			"# one\n\n# two\nlet x = 1;\n",
			"# one\n\n# two\nlet x = 1;\n",
		},
		{
			"blank line between a comment and the next statement is kept",
			"# header\n\nlet x = 1;\n",
			"# header\n\nlet x = 1;\n",
		},
		{
			"blank lines inside a block are kept",
			"func g(): void {\n    # a\n\n    # b\n    let x = 1;\n}\n",
			"func g(): void {\n    # a\n\n    # b\n    let x = 1;\n}\n",
		},
		{
			"no blank line is added after an opening brace",
			"let a = 1;\n\nclass X {\n\n    # first\n    int v;\n}\n",
			"let a = 1;\n\nclass X {\n    # first\n    int v;\n}\n",
		},
		{
			"blank lines at end of file are kept between comments",
			"let x = 1;\n# tail\n\n# last\n",
			"let x = 1;\n# tail\n\n# last\n",
		},
		{
			"doc comment stays attached to a decorated export",
			"## Lists users.\n@route(\"GET\", \"/users\")\nexport func listUsers(): void {}\n",
			"## Lists users.\n@route(\"GET\", \"/users\")\nexport func listUsers(): void {}\n",
		},
		{
			"blank line before a decorated export is kept",
			"# a\n\n@route(\"GET\", \"/x\")\nexport func f(): void {}\n",
			"# a\n\n@route(\"GET\", \"/x\")\nexport func f(): void {}\n",
		},
		{
			"aligned trailing comments keep their spacing",
			"let x = 1;    # aligned\nlet yy = 22;  # aligned\n",
			"let x = 1;    # aligned\nlet yy = 22;  # aligned\n",
		},
		{"trailing comment gets at least one space", "let z = 3;# tight\n", "let z = 3; # tight\n"},
		{"tab before a trailing comment becomes spaces", "let z = 3;\t# tab\n", "let z = 3;    # tab\n"},
		{"trailing block comment keeps its spacing", "let w = 4;   /* blk */\n", "let w = 4;   /* blk */\n"},
	}
	for _, tc := range cases {
		got := roundtrip(t, tc.src)
		parseOK(t, got)
		if got != tc.want {
			t.Fatalf("%s:\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
		}
		if again := roundtrip(t, got); again != got {
			t.Fatalf("%s: not idempotent:\nfirst:\n%s\nsecond:\n%s", tc.name, got, again)
		}
	}
}
