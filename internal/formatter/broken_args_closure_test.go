package formatter_test

import "testing"

func TestFormatKeepsAuthorBrokenArgumentsAroundClosures(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"dict argument with a multi-line closure stays broken",
			"configure({\n    \"name\": \"a\",\n    \"f\": func(int x): int {\n        let y = x;\n        return y;\n    },\n});\n",
			"configure({\n    \"name\": \"a\",\n    \"f\": func(int x): int {\n        let y = x;\n        return y;\n    },\n});\n",
		},
		{
			"list argument with a multi-line closure stays broken",
			"register([\n    \"a\",\n    func(): void {\n        let a = 1;\n        io.println(a);\n    },\n]);\n",
			"register([\n    \"a\",\n    func(): void {\n        let a = 1;\n        io.println(a);\n    },\n]);\n",
		},
		{
			"named arguments with a multi-line closure stay broken",
			"run(\n    \"job\",\n    handler: func(int x): void {\n        let y = x;\n        io.println(y);\n    }\n);\n",
			"run(\n    \"job\",\n    handler: func(int x): void {\n        let y = x;\n        io.println(y);\n    }\n);\n",
		},
		{
			"nested call keeps the inner broken dict",
			"outer(inner({\n    \"k\": 1,\n    \"f\": func(): void {\n        let a = 1;\n        io.println(a);\n    },\n}));\n",
			"outer(\n    inner({\n        \"k\": 1,\n        \"f\": func(): void {\n            let a = 1;\n            io.println(a);\n        },\n    })\n);\n",
		},
		{
			"broken chain segment indents its broken arguments",
			"let q = repo.query(\"x\")\n    .where({\n        \"a\": 1,\n        \"f\": func(): int {\n            let z = 1;\n            return z;\n        },\n    });\n",
			"let q = repo.query(\"x\")\n    .where({\n        \"a\": 1,\n        \"f\": func(): int {\n            let z = 1;\n            return z;\n        },\n    });\n",
		},
		{
			"closure in a broken chain segment indents its body",
			"let ys = xs\n    .filter((int x) => x > 0)\n    .map(func(int x): int {\n        let y = x * 2;\n        return y;\n    });\n",
			"let ys = xs\n    .filter((int x) => x > 0)\n    .map(func(int x): int {\n        let y = x * 2;\n        return y;\n    });\n",
		},
		{
			"an over-wide call with no arguments keeps its empty parens together",
			"func f(any request): void {\n    if (true) {\n        if (true) {\n            let contentType = http.requestObject(request).header(\"Content-Type\").lower().split(\";\")[0].trim();\n        }\n    }\n}\n",
			"func f(any request): void {\n    if (true) {\n        if (true) {\n            let contentType = http.requestObject(request).header(\"Content-Type\").lower().split(\";\")[0].trim();\n        }\n    }\n}\n",
		},
		{
			"an author-broken chain ending in a no-argument call stays broken",
			"let contentType = http.requestObject(request)\n    .header(\"Content-Type\")\n    .lower()\n    .trim();\n",
			"let contentType = http.requestObject(request)\n    .header(\"Content-Type\")\n    .lower()\n    .trim();\n",
		},
		{
			"an author-broken dict assigned to an index stays broken",
			"func f(dict<string, any> out): void {\n    out[\"a\"] = {\n        \"type\": \"object\",\n        \"required\": [\"x\"],\n    };\n}\n",
			"func f(dict<string, any> out): void {\n    out[\"a\"] = {\n        \"type\": \"object\",\n        \"required\": [\"x\"],\n    };\n}\n",
		},
		{
			"an author-broken list in a plain and a compound assignment stays broken",
			"func f(): void {\n    let y = [];\n    y = [\n        1,\n        2,\n    ];\n    y += [\n        3,\n        4,\n    ];\n}\n",
			"func f(): void {\n    let y = [];\n    y = [\n        1,\n        2,\n    ];\n    y += [\n        3,\n        4,\n    ];\n}\n",
		},
		{
			"an over-wide assigned call breaks its arguments",
			"this.value = configureSomethingVeryLong(firstArgumentName, secondArgumentName, thirdArgumentNameLonger);\n",
			"this.value = configureSomethingVeryLong(\n    firstArgumentName,\n    secondArgumentName,\n    thirdArgumentNameLonger\n);\n",
		},
		{
			"a short assignment stays on one line",
			"x = {\"a\": 1};\n",
			"x = {\"a\": 1};\n",
		},
		{
			"single closure argument still hugs the call",
			"spawn(func(): void {\n    let a = 1;\n    io.println(a);\n});\n",
			"spawn(func(): void {\n    let a = 1;\n    io.println(a);\n});\n",
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
