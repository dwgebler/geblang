package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/modules"
)

const lintShapesModule = `module shapes;

export interface Scored {
    func score(): int;
}

export class Tag {
}
`

func lintDiagnostics(t *testing.T, source string) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"), []byte(lintShapesModule), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	opts := Options{
		Lint:          true,
		Resolver:      modules.NewResolver([]string{dir}),
		CrossModule:   true,
		NativeSymbols: map[string]map[string]struct{}{"json": {"parse": {}, "stringify": {}}},
	}
	_, diags := Source(file, source, opts)
	return diags
}

func TestUnusedImportSeesEveryUseSite(t *testing.T) {
	cases := map[string]string{
		"with value and body": "import file;\nimport io;\n" +
			"with (f = file.open(\"x.txt\", \"r\")) { io.println(f.readLine()); }\n",
		"list comprehension":       "import math;\nlet a = [math.abs(x) for x in [1, -2]];\n",
		"set comprehension filter": "import math;\nlet a = {x for x in [1, 2] if x > math.abs(-1)};\n",
		"dict comprehension":       "import math;\nlet a = {k: math.abs(v) for k, v in {\"a\": -1}};\n",
		"comprehension iterable":   "import math;\nlet a = [x for x in [math.abs(-1)]];\n",
		"formatted interpolation":  "import math;\nlet s = \"${math.pi:.2f}\";\n",
		"typed for variable":       "import shapes;\nfor (shapes.Tag t in []) { }\n",
		"select case body": "import io;\n" +
			"func f(any c): void {\n    select {\n        case let v = c.recv(): { io.println(v); }\n        default: { }\n    }\n}\n",
		"select default body": "import io;\n" +
			"func f(any c): void {\n    select {\n        case let v = c.recv(): { }\n        default: { io.println(1); }\n    }\n}\n",
		"function generic constraint":  "import shapes;\nfunc top<T implements shapes.Scored>(list<T> items): T { return items[0]; }\n",
		"class generic constraint":     "import shapes;\nclass Box<T implements shapes.Scored> { }\n",
		"parameter decorator":          "import shapes;\nfunc f(@shapes.Tag string s): void { }\n",
		"class decorator":              "import shapes;\n@shapes.Tag\nclass X { }\n",
		"function decorator":           "import shapes;\n@shapes.Tag\nfunc g(): void { }\n",
		"field decorator":              "import shapes;\nclass C {\n    @shapes.Tag\n    string s = \"\";\n}\n",
		"destructor body":              "import io;\nclass R {\n    func ~R() { io.println(\"bye\"); }\n}\n",
		"enum implements":              "import shapes;\nenum Level implements shapes.Scored {\n    Low, High;\n    func score(): int { return 1; }\n}\n",
		"enum method body":             "import math;\nenum Level {\n    Low, High;\n    func weight(): float { return math.pi; }\n}\n",
		"interface default body":       "import math;\ninterface Sized {\n    func size(): float {\n        return math.pi;\n    }\n}\n",
		"interface generic constraint": "import shapes;\ninterface Holder<T implements shapes.Scored> {\n    func get(): T;\n}\n",
		"match case type": "import shapes;\n" +
			"func f(any v): int {\n    return match (v) {\n        case shapes.Tag t => 1;\n        default => 0;\n    };\n}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			for _, d := range lintDiagnostics(t, source) {
				if d.Rule == "unused-import" {
					t.Fatalf("import reported unused: %+v\nsource:\n%s", d, source)
				}
			}
		})
	}
}

func TestUnusedImportStillFlagsUnusedInsideNewSites(t *testing.T) {
	source := "import io;\nimport math;\nwith (f = math.abs(-1)) { }\n"
	diags := lintDiagnostics(t, source)
	if !hasDiag(diags, "unused-import", "import io is not used") {
		t.Fatalf("expected io unused, got %+v", diags)
	}
	if hasDiag(diags, "unused-import", "import math is not used") {
		t.Fatalf("math is used in the with header, got %+v", diags)
	}
}

func TestUnreachableSeesEveryBody(t *testing.T) {
	cases := map[string]struct {
		source string
		line   int
	}{
		"with body":                           {"func f(): void {\n    with (x = 1) {\n        return;\n        let y = 2;\n    }\n}\n", 4},
		"select case body":                    {"func f(any c): void {\n    select {\n        case let v = c.recv(): {\n            return;\n            let y = 2;\n        }\n    }\n}\n", 5},
		"select default body":                 {"func f(any c): void {\n    select {\n        default: {\n            return;\n            let y = 2;\n        }\n    }\n}\n", 5},
		"closure body":                        {"let g = func(): int {\n    return 1;\n    let y = 2;\n};\n", 3},
		"enum method":                         {"enum Level {\n    Low;\n    func w(): int {\n        return 1;\n        let y = 2;\n    }\n}\n", 5},
		"destructor":                          {"class R {\n    func ~R() {\n        return;\n        let y = 2;\n    }\n}\n", 4},
		"interface default":                   {"interface Sized {\n    func size(): int {\n        return 1;\n        let y = 2;\n    }\n}\n", 4},
		"unreachable with statement position": {"func f(): void {\n    return;\n    with (x = 1) { }\n}\n", 3},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			diags := lintDiagnostics(t, tc.source)
			for _, d := range diags {
				if d.Rule == "unreachable" && d.Line == tc.line {
					return
				}
			}
			t.Fatalf("expected unreachable warning on line %d, got %+v", tc.line, diags)
		})
	}
}

// Every AST statement and expression node must be wired into the shared check walker.
func TestASTWalkerCoversEveryNode(t *testing.T) {
	src, err := os.ReadFile("astwalk.go")
	if err != nil {
		t.Fatal(err)
	}
	walker := string(src)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Join("..", "ast"), func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				if fn.Name.Name != "statementNode" && fn.Name.Name != "expressionNode" {
					continue
				}
				star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				ident, ok := star.X.(*ast.Ident)
				if !ok {
					continue
				}
				seen++
				if !strings.Contains(walker, "*ast."+ident.Name+":") && !strings.Contains(walker, "*ast."+ident.Name+",") {
					t.Errorf("astwalk.go has no case for ast.%s", ident.Name)
				}
			}
		}
	}
	if seen < 40 {
		t.Fatalf("found only %d AST node types; the scan is broken", seen)
	}
}

func TestErrorChecksReachEveryUseSite(t *testing.T) {
	cases := map[string]struct {
		source  string
		message string
	}{
		"member in with header":        {"import json;\nwith (f = json.decode(\"1\")) { }\n", "json has no exported member decode"},
		"member in with body":          {"import json;\nwith (f = 1) { json.decode(\"1\"); }\n", "json has no exported member decode"},
		"member in comprehension":      {"import json;\nlet a = [x for x in json.decode(\"[1]\")];\n", "json has no exported member decode"},
		"member in comprehension body": {"import json;\nlet a = [json.decode(x) for x in [\"1\"]];\n", "json has no exported member decode"},
		"member in formatted interp":   {"import json;\nlet s = \"${json.decode(\"1\"):s}\";\n", "json has no exported member decode"},
		"member in select body": {"import json;\n" +
			"func f(any c): void {\n    select {\n        case let v = c.recv(): { json.decode(v); }\n    }\n}\n", "json has no exported member decode"},
		"member in enum method":          {"import json;\nenum L {\n    A;\n    func f(): any { return json.decode(\"1\"); }\n}\n", "json has no exported member decode"},
		"member in parameter default":    {"import json;\nfunc f(any v = json.decode(\"1\")): void { }\n", "json has no exported member decode"},
		"member in pipe":                 {"import json;\nlet a = \"1\" |> json.decode;\n", "json has no exported member decode"},
		"type in with body":              {"import shapes;\nwith (f = 1) { shapes.Nope y = null; }\n", "shapes has no exported type Nope"},
		"type in comprehension var":      {"import shapes;\nlet a = [x for shapes.Nope x in []];\n", "shapes has no exported type Nope"},
		"instanceof in comprehension":    {"let a = [x for x in [1] if x instanceof Nope];\n", "unknown type \"Nope\" in instanceof"},
		"instanceof in formatted interp": {"let s = \"${1 instanceof Nope:s}\";\n", "unknown type \"Nope\" in instanceof"},
		"instanceof in enum method":      {"enum L {\n    A;\n    func f(any v): bool { return v instanceof Nope; }\n}\n", "unknown type \"Nope\" in instanceof"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			diags := lintDiagnostics(t, tc.source)
			for _, d := range diags {
				if d.Severity == SeverityError && strings.Contains(d.Message, tc.message) {
					return
				}
			}
			t.Fatalf("expected error %q, got %+v", tc.message, diags)
		})
	}
}

func TestNewBindingSitesShadowModuleAliases(t *testing.T) {
	cases := map[string]string{
		"comprehension variable": "import json;\nlet a = [json.size() for json in [[1]]];\n",
		"with binding":           "import json;\nwith (json = [1]) { json.size(); }\n",
		"match case binding":     "import json;\nfunc f(any v): int {\n    return match (v) {\n        case list json => json.size();\n        default => 0;\n    };\n}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			for _, d := range lintDiagnostics(t, source) {
				if d.Rule == "import" {
					t.Fatalf("shadowed alias flagged: %+v", d)
				}
			}
		})
	}
}
