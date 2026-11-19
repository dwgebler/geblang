package formatter_test

import "testing"

func TestFormatPlacesCommentsAndDecorators(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"typed static field keeps its spelling",
			"class C {\n    static int errors = 0;\n    static let int b = 1;\n}\n",
			"class C {\n    static int errors = 0;\n    static let int b = 1;\n}\n",
		},
		{
			"comment after a block's opening brace stays on that line",
			"for (i in 0..3) {    # note\n    io.print(i);\n}\n",
			"for (i in 0..3) {    # note\n    io.print(i);\n}\n",
		},
		{
			"comment after else brace",
			"if (x) { # one\n    y();\n} else {   # two\n    z();\n}\n",
			"if (x) { # one\n    y();\n} else {   # two\n    z();\n}\n",
		},
		{
			"comment after a class opening brace",
			"class K {  # cls\n    int a;\n}\n",
			"class K {  # cls\n    int a;\n}\n",
		},
		{
			"comment after a func literal opening brace",
			"let f = func(): void {   # lam\n    a();\n    b();\n};\n",
			"let f = func(): void {   # lam\n    a();\n    b();\n};\n",
		},
		{
			"comments inside a broken method chain",
			"let r = req\n    .withJson(x)       # sets body\n    # about send\n    .send();           # go\n",
			"let r = req\n    .withJson(x)       # sets body\n    # about send\n    .send();           # go\n",
		},
		{
			"comments inside a multi-line dict",
			"let d = {\n    \"a\": 1,   # one\n    # group\n    \"b\": 2,   # two\n    # tail\n};\n",
			"let d = {\n    \"a\": 1,   # one\n    # group\n    \"b\": 2,   # two\n    # tail\n};\n",
		},
		{
			"comments inside a multi-line list",
			"let xs = [\n    1,   # one\n    2,\n];\n",
			"let xs = [\n    1,   # one\n    2,\n];\n",
		},
		{
			"comments inside multi-line call arguments",
			"let c = call(\n    a,   # first\n    b\n);\n",
			"let c = call(\n    a,   # first\n    b\n);\n",
		},
		{
			"comments inside a multi-line operator chain",
			"let total = a\n    + b   # bee\n    + c;\n",
			"let total = a\n    + b   # bee\n    + c;\n",
		},
		{
			"comment inside a func literal inside a broken list is not lost",
			"let xs = [\n    func(): void {\n        # inside\n        a();\n        b();\n    },\n    2,\n];\n",
			"let xs = [\n    func(): void {\n        # inside\n        a();\n        b();\n    },\n    2,\n];\n",
		},
		{
			"enum variant comments",
			"enum P: int {\n    # levels\n    Low    = 1,   # cheap\n    Medium = 2,\n    Urgent = 9    # page\n    # end\n}\n",
			"enum P: int {\n    # levels\n    Low    = 1,   # cheap\n    Medium = 2,\n    Urgent = 9    # page\n    # end\n}\n",
		},
		{
			"comments on match expression arms",
			"let first = match (xs) {   # top\n    # lead\n    case [int kept, _] => kept;     # discard\n    default            => 0;\n    # tail\n};\n",
			"let first = match (xs) {   # top\n    # lead\n    case [int kept, _] => kept;     # discard\n    default            => 0;\n    # tail\n};\n",
		},
		{
			"comments on match statement arms",
			"match (v) {\n    case 1 => a();   # one\n    # tail\n}\n",
			"match (v) {\n    case 1 => a();   # one\n    # tail\n}\n",
		},
		{
			"comment after statements sharing a line follows the last one",
			"log(\"a\"); log(\"b\");   # both\nfunc f(): void {\n    a(); b();   # pair\n}\n",
			"log(\"a\");\nlog(\"b\");   # both\n\nfunc f(): void {\n    a();\n    b();   # pair\n}\n",
		},
		{
			"field decorators stay where they were written",
			"class U {\n    @immutable string id;\n    @A\n    @B string mixed;\n    @C\n    string own;\n}\n",
			"class U {\n    @immutable string id;\n    @A\n    @B string mixed;\n    @C\n    string own;\n}\n",
		},
		{
			"aligned values in a multi-line dict",
			"let d = {\n    \"memory\":      131072,\n    \"parallelism\": 2,\n};\n",
			"let d = {\n    \"memory\":      131072,\n    \"parallelism\": 2,\n};\n",
		},
		{
			"arguments in separate single-line calls are not aligned",
			"let o = c({\"p\": \"openai\",    \"k\": 1});\nlet a = c({\"p\": \"anthropic\", \"k\": 2});\n",
			"let o = c({\"p\": \"openai\", \"k\": 1});\nlet a = c({\"p\": \"anthropic\", \"k\": 2});\n",
		},
		{
			"trailing multi-line collection argument hugs the call line",
			"let h = crypt.hash(\"pw\", {\n    \"memory\": 1,   # KiB\n    \"time\":   4,\n});\n",
			"let h = crypt.hash(\"pw\", {\n    \"memory\": 1,   # KiB\n    \"time\":   4,\n});\n",
		},
		{
			"named trailing collection argument hugs",
			"let h = g(a, opts: {\n    \"k\": 1,\n    \"j\": 2,\n});\n",
			"let h = g(a, opts: {\n    \"k\": 1,\n    \"j\": 2,\n});\n",
		},
		{
			"author one-argument-per-line call stays exploded",
			"let q = f(\n    a,\n    {\"x\": 1}\n);\n",
			"let q = f(\n    a,\n    {\"x\": 1}\n);\n",
		},
		{
			"block comment text is kept as written",
			"let a = 1;     /* level 1  - fastest    */\nlet b = 2; /*tight*/\n",
			"let a = 1;     /* level 1  - fastest    */\nlet b = 2; /*tight*/\n",
		},
		{
			"broken chain with index steps keeps its breaks",
			"let id = store.save({})[\"headers\"][\"Set-Cookie\"]\n    .split(\";\")[0]\n    .substring(8);   /* extract */\n",
			"let id = store.save({})[\"headers\"][\"Set-Cookie\"]\n    .split(\";\")[0]\n    .substring(8);   /* extract */\n",
		},
		{
			"index steps on one line stay flat",
			"let a = xs[0][1];\nlet b = rows.first()[\"name\"].upper();\nlet s = text.split(\",\")[1:3];\n",
			"let a = xs[0][1];\nlet b = rows.first()[\"name\"].upper();\nlet s = text.split(\",\")[1:3];\n",
		},
		{
			"property step after an index on a new line",
			"let c = data\n    .items[2]\n    .name;\n",
			"let c = data\n    .items[2]\n    .name;\n",
		},
		{
			"stray spacing inside a lone dict is normalised",
			"let z = {\"a\":    1};\nfoo(a,    b);\n",
			"let z = {\"a\": 1};\nfoo(a, b);\n",
		},
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
