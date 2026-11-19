package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every expression and statement position must reach the analyzer's method checks; BAD is a call check rejects.
func TestCheckReachesEveryExpressionPosition(t *testing.T) {
	const bad = `own.has(1)`
	cases := map[string]string{
		"expression statement":    `BAD;`,
		"let initializer":         `let v = BAD;`,
		"ternary condition":       `let v = BAD ? 1 : 2;`,
		"ternary then":            `let v = true ? BAD : false;`,
		"ternary else":            `let v = false ? true : BAD;`,
		"interpolation":           `let v = "${BAD}";`,
		"formatted interpolation": `let v = "${BAD:>8}";`,
		"cast":                    `let v = BAD as string;`,
		"range end":               `let v = 0..(BAD as int);`,
		"range step":              `let v = 0..10 by (BAD as int);`,
		"spread argument":         `let v = [...(BAD as list<int>)];`,
		"pipe left":               `let v = BAD |> io.println;`,
		"list comprehension body": `let v = [BAD for x in [1]];`,
		"list comprehension iter": `let v = [x for x in (BAD as list<int>)];`,
		"list comprehension if":   `let v = [x for x in [1] if BAD];`,
		"set comprehension":       `let v = {BAD for x in [1]};`,
		"dict comprehension key":  `let v = {"${BAD}": x for x in [1]};`,
		"dict comprehension val":  `let v = {"k": BAD for x in [1]};`,
		"match expression subject": `let v = match (BAD) {
    default => 1;
};`,
		"match expression arm": `let v = match (1) {
    case 1 => BAD;
    default => false;
};`,
		"match expression guard": `let v = match (1) {
    case int n if (BAD) => 1;
    default => 2;
};`,
		"await":                     "async func f(): bool {\n    return await async.run(() => BAD);\n}",
		"if condition":              "if (BAD) {\n    io.println(1);\n}",
		"else if":                   "if (false) {\n    io.println(1);\n} else if (BAD) {\n    io.println(2);\n}",
		"while":                     "while (BAD) {\n    break;\n}",
		"for-in iterable":           "for (x in (BAD as list<int>)) {\n    io.println(x);\n}",
		"c-style for condition":     "for (int i = 0; BAD; i++) {\n    break;\n}",
		"return":                    "func f(): bool {\n    return BAD;\n}",
		"throw":                     "func f(): void {\n    throw ValueError(\"${BAD}\");\n}",
		"match statement subject":   "match (BAD) {\n    default => io.println(1);\n}",
		"match statement arm":       "match (1) {\n    case 1 => io.println(BAD);\n    default => io.println(2);\n}",
		"default parameter":         "func f(bool b = BAD): bool {\n    return b;\n}",
		"field default":             "class C {\n    bool b = BAD;\n}",
		"method body":               "class C {\n    func m(): bool {\n        return BAD;\n    }\n}",
		"with value":                "with (r = BAD) {\n    io.println(r);\n}",
		"with body":                 "with (r = io.open(\"x\")) {\n    io.println(BAD);\n}",
		"select send value":         "let ch = async.channel(1);\nselect {\n    case ch.send(BAD): {\n        io.println(1);\n    }\n}",
		"select body":               "let ch = async.channel(1);\nselect {\n    case let v = ch.recv(): {\n        io.println(BAD);\n    }\n}",
		"select default":            "let ch = async.channel(1);\nselect {\n    case let v = ch.recv(): {\n        io.println(v);\n    }\n    default: {\n        io.println(BAD);\n    }\n}",
		"try body":                  "try {\n    io.println(BAD);\n} catch (Error e) {\n    io.println(1);\n}",
		"catch body":                "try {\n    io.println(1);\n} catch (Error e) {\n    io.println(BAD);\n}",
		"finally body":              "try {\n    io.println(1);\n} finally {\n    io.println(BAD);\n}",
		"yield":                     "func g(): generator<bool> {\n    yield BAD;\n}",
		"destructuring":             "let a, b = [BAD, 1];",
		"enum method body":          "enum E {\n    A;\n\n    func m(): bool {\n        return BAD;\n    }\n}",
		"interface default body":    "interface I {\n    func m(): bool {\n        return BAD;\n    }\n}",
		"lambda in call argument":   "let v = [1].map((int x) => BAD);",
		"nested closure in ternary": "func f(): callable {\n    return (string p) => {\n        return BAD ? \"y\" : \"n\";\n    };\n}",
	}
	for name, body := range cases {
		dir := t.TempDir()
		file := filepath.Join(dir, "main.gb")
		src := "import io;\nimport async;\n\nlet own = {};\n" + strings.ReplaceAll(body, "BAD", bad) + "\n"
		_, diags := Source(file, src, classCheckOpts(dir))
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "dict has no method has") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: check did not report the bad call.\nsource:\n%s\ndiagnostics: %v", name, src, diags)
		}
	}
}
