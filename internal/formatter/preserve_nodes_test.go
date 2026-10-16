package formatter_test

import (
	"strings"
	"testing"

	"geblang/internal/formatter"
)

func TestFormatPreservesEveryDeclaredNode(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"interface properties and defaults",
			"interface Entity {\n    string id;\n\n    func sameAs(Entity other): bool {\n        return this.id == other.id;\n    }\n\n    func save(): void;\n}",
			"interface Entity {\n    string id;\n\n    func sameAs(Entity other): bool {\n        return this.id == other.id;\n    }\n\n    func save(): void;\n}"},
		{"field decorators",
			"class P {\n    @notBlank\n    string name;\n    @Groups(\"read\", \"admin\") string id;\n}",
			"class P {\n    @notBlank\n    string name;\n    @Groups(\"read\", \"admin\")\n    string id;\n}"},
		{"dataclass field decorators",
			"@dataclass(frozen: true)\nexport class Line {\n    string sku;\n\n    @positive\n    int quantity;\n}",
			"@dataclass(frozen: true)\nexport class Line {\n    string sku;\n\n    @positive\n    int quantity;\n}"},
		{"destructor in place",
			"class R {\n    int n = 0;\n\n    func ~R() {\n        io.println(\"bye\");\n    }\n\n    func use(): void {}\n}",
			"class R {\n    int n = 0;\n\n    func ~R() {\n        io.println(\"bye\");\n    }\n\n    func use(): void {}\n}"},
		{"enum backing type and values",
			"enum Status: string {\n    Active = \"active\",\n    Closed = \"closed\"\n}",
			"enum Status: string {\n    Active = \"active\",\n    Closed = \"closed\"\n}"},
		{"blank lines between enum methods",
			"enum Q {\n    A,\n    B;\n\n    func x(): int {\n        return 1;\n    }\n\n    func y(): int {\n        return 2;\n    }\n}",
			"enum Q {\n    A,\n    B;\n\n    func x(): int {\n        return 1;\n    }\n\n    func y(): int {\n        return 2;\n    }\n}"},
		{"chained catches",
			"try {\n    a();\n} catch (ValueError e) {\n    b();\n} catch (RuntimeError e) {\n    c();\n}\nd();",
			"try {\n    a();\n} catch (ValueError e) {\n    b();\n} catch (RuntimeError e) {\n    c();\n}\nd();"},
		{"decorators before export",
			"export @immutable class C {\n    int n;\n}\n\n@memoize\nexport func f(int n): int {\n    return n;\n}",
			"@immutable\nexport class C {\n    int n;\n}\n\n@memoize\nexport func f(int n): int {\n    return n;\n}"},
		{"string literal inside interpolation", `let a = "x ${"has}and{both"} y";`, `let a = "x ${"has}and{both"} y";`},
		{"raw literal inside interpolation", `let b = "${'${name}'}";`, `let b = "${'${name}'}";`},
		{"empty literal inside interpolation", `let c = "a\n${""}b";`, `let c = "a\n${""}b";`},
		{"inclusive index range", "let a = xs[1..3];", "let a = xs[1..3];"},
		{"open inclusive index range", "let b = xs[3..];", "let b = xs[3..];"},
		{"exclusive index range", "let c = xs[1..<3];", "let c = xs[1:3];"},
		{"async enum method",
			"enum Light {\n    Red,\n    Green;\n\n    async func next(): string {\n        return \"x\";\n    }\n}",
			"enum Light {\n    Red,\n    Green;\n\n    async func next(): string {\n        return \"x\";\n    }\n}"},
		{"static async method",
			"class Rates {\n    static async func fetch(): int {\n        return 1;\n    }\n}",
			"class Rates {\n    static async func fetch(): int {\n        return 1;\n    }\n}"},
		{"module-qualified enum pattern",
			"let d = match (q) {\n    case quotes.Quote.Offered(string c, int p) => c;\n    default => \"\";\n};",
			"let d = match (q) {\n    case quotes.Quote.Offered(string c, int p) => c;\n    default => \"\";\n};"},
	}
	for _, c := range cases {
		out, err := formatter.Format([]byte(c.in))
		if err != nil {
			t.Errorf("%s: Format errored: %v", c.name, err)
			continue
		}
		if got := strings.TrimRight(string(out), "\n"); got != c.want {
			t.Errorf("%s:\n  got:  %q\n  want: %q", c.name, got, c.want)
		}
		again, err := formatter.Format(out)
		if err != nil || string(again) != string(out) {
			t.Errorf("%s: not idempotent (err=%v)\n  pass1: %q\n  pass2: %q", c.name, err, out, again)
		}
	}
}
