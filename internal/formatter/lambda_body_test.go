package formatter_test

import "testing"

func TestFormatLambdaBodies(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"single return stays inline",
			"let f = func(int x): int { return x * 2; };\n",
			"let f = func(int x): int { return x * 2; };\n",
		},
		{
			"multi-line single return is joined",
			"let f = func(int x): int {\n    return x * 2;\n};\n",
			"let f = func(int x): int { return x * 2; };\n",
		},
		{
			"single non-return statement expands",
			"let f = func(int x): void { io.println(x); };\n",
			"let f = func(int x): void {\n    io.println(x);\n};\n",
		},
		{
			"assignment body expands",
			"xs.each(func(int x): void { total = total + x; });\n",
			"xs.each(func(int x): void {\n    total = total + x;\n});\n",
		},
		{
			"return too wide for one line expands",
			"let f = func(string aaaaaaaaaaaaaaaaaaaa, string bbbbbbbbbbbbbbbbbbbb): string { return aaaaaaaaaaaaaaaaaaaa + bbbbbbbbbbbbbbbbbbbb; };\n",
			"let f = func(string aaaaaaaaaaaaaaaaaaaa, string bbbbbbbbbbbbbbbbbbbb): string {\n    return aaaaaaaaaaaaaaaaaaaa + bbbbbbbbbbbbbbbbbbbb;\n};\n",
		},
		{
			"comment inside a return body keeps it expanded",
			"let f = func(int x): int {\n    # doubled for the wire format\n    return x * 2;\n};\n",
			"let f = func(int x): int {\n    # doubled for the wire format\n    return x * 2;\n};\n",
		},
		{
			"async single return stays inline",
			"let f = async func(int x): int { return x; };\n",
			"let f = async func(int x): int { return x; };\n",
		},
		{
			"empty body",
			"let f = func(): void {};\n",
			"let f = func(): void {};\n",
		},
		{
			"call after an expanded lambda is not split into a chain",
			"ok(this.failure(func(): void { math.sum(xs); }).startsWith(\"math.sum\"));\n",
			"ok(this.failure(func(): void {\n    math.sum(xs);\n}).startsWith(\"math.sum\"));\n",
		},
		{
			"list element after an expanded lambda stays on its line",
			"let cases = [[func(): void { run(); }, \"boom\"]];\n",
			"let cases = [[func(): void {\n    run();\n}, \"boom\"]];\n",
		},
		{
			"author-split chain still breaks",
			"let n = xs\n    .filter(func(int x): bool { return x > 1; })\n    .length();\n",
			"let n = xs\n    .filter(func(int x): bool { return x > 1; })\n    .length();\n",
		},
		{
			"author-split list still breaks",
			"let xs = [\n    1,\n    2,\n];\n",
			"let xs = [\n    1,\n    2,\n];\n",
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
