package embedfold_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/embedfold"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func foldSource(t *testing.T, dir, source string, mode embedfold.Mode) (*ast.Program, []embedfold.Record, []embedfold.Diagnostic) {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	recs, diags := embedfold.Fold(program, filepath.Join(dir, "main.gb"), mode)
	return program, recs, diags
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Reflection walk, independent of the rewriter's own type switch, so a traversal hole cannot hide.
func unfoldedEmbeds(node any) []string {
	var found []string
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() || !v.CanInterface() {
				return
			}
			if v.Kind() == reflect.Pointer {
				if seen[v.Pointer()] {
					return
				}
				seen[v.Pointer()] = true
				if call, ok := v.Interface().(*ast.CallExpression); ok {
					if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "embed" {
						found = append(found, call.String())
					}
				}
			}
			walk(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if !v.Type().Field(i).IsExported() {
					continue
				}
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		}
	}
	walk(reflect.ValueOf(node))
	return found
}

func declValue(t *testing.T, program *ast.Program, index int) ast.Expression {
	t.Helper()
	decl, ok := program.Statements[index].(*ast.DeclarationStatement)
	if !ok {
		t.Fatalf("statement %d is %T, want *ast.DeclarationStatement", index, program.Statements[index])
	}
	return decl.Value
}

func embeddedLiteral(t *testing.T, expr ast.Expression) *ast.EmbeddedLiteral {
	t.Helper()
	lit, ok := expr.(*ast.EmbeddedLiteral)
	if !ok {
		t.Fatalf("expression is %T, want *ast.EmbeddedLiteral", expr)
	}
	return lit
}

func TestFoldInlineText(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "d/a.txt", "A")

	program, recs, diags := foldSource(t, dir, `let x = embed("d/a.txt");`, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	lit := embeddedLiteral(t, declValue(t, program, 0))
	if string(lit.Content) != "A" {
		t.Errorf("content = %q, want %q", lit.Content, "A")
	}
	if lit.Binary {
		t.Error("Binary = true, want false")
	}
	if lit.Path != "d/a.txt" {
		t.Errorf("path = %q, want %q", lit.Path, "d/a.txt")
	}
	if len(recs) != 1 {
		t.Fatalf("records = %+v, want 1", recs)
	}
	if recs[0].Path != "d/a.txt" {
		t.Errorf("record path = %q, want %q", recs[0].Path, "d/a.txt")
	}
	if want := filepath.Join(dir, "d", "a.txt"); recs[0].Abs != want {
		t.Errorf("record abs = %q, want %q", recs[0].Abs, want)
	}
	if recs[0].Hash != sha256.Sum256([]byte("A")) {
		t.Errorf("record hash = %x, want sha256 of content", recs[0].Hash)
	}
}

func TestFoldInlineBinaryOpts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.bin", "\x00\x01\xff")

	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{"bare key", `let b = embed("a.bin", {binary: true});`, true},
		{"string key", `let b = embed("a.bin", { "binary": true });`, true},
		{"explicit false", `let b = embed("a.bin", {binary: false});`, false},
		{"empty opts", `let b = embed("a.bin", {});`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program, recs, diags := foldSource(t, dir, tc.source, embedfold.Inline)
			if len(diags) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", diags)
			}
			lit := embeddedLiteral(t, declValue(t, program, 0))
			if lit.Binary != tc.want {
				t.Errorf("Binary = %v, want %v", lit.Binary, tc.want)
			}
			if string(lit.Content) != "\x00\x01\xff" {
				t.Errorf("content = %x, want 0001ff", lit.Content)
			}
			if len(recs) != 1 {
				t.Errorf("records = %+v, want 1", recs)
			}
		})
	}
}

func TestFoldEveryExpressionPosition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	source := `import io;

@note(embed("a.txt"))
func decorated(string p = embed("a.txt")): string {
    return embed("a.txt");
}

class Holder {
    @note(embed("a.txt"))
    string field = embed("a.txt");

    func Holder(string v = embed("a.txt")) {
        this.field = embed("a.txt");
    }

    func method(): string {
        return embed("a.txt");
    }
}

let inList = [embed("a.txt")];
let inDict = {"k": embed("a.txt")};
let inSet = {embed("a.txt")};
let inInterp = "${embed("a.txt")}";
let inTernary = true ? embed("a.txt") : embed("a.txt");
let inMatch = match (1) {
    case 1 => embed("a.txt");
    default => embed("a.txt");
};
let inPipe = embed("a.txt") |> io.println;
let inComprehension = [embed("a.txt") for i in [1, 2]];
let inLambda = func(): string { return embed("a.txt"); };
let inIndex = [embed("a.txt")][0];
let inCast = embed("a.txt") as string;
let inInfix = embed("a.txt") + embed("a.txt");
io.println(embed("a.txt"));

if (embed("a.txt") == "A") {
    io.println(embed("a.txt"));
}

for (int i = 0; i < 1; i++) {
    io.println(embed("a.txt"));
}

for (string s in [embed("a.txt")]) {
    io.println(s);
}

while (embed("a.txt") == "") {
    io.println(embed("a.txt"));
}

try {
    throw embed("a.txt");
} catch (Exception e) {
    io.println(embed("a.txt"));
} finally {
    io.println(embed("a.txt"));
}

match (embed("a.txt")) {
    case "A" if (embed("a.txt") == "A"): { io.println(embed("a.txt")); }
    default: { io.println("no"); }
}
`
	program, recs, diags := foldSource(t, dir, source, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if left := unfoldedEmbeds(program); len(left) != 0 {
		t.Errorf("unfolded embed calls remain: %v", left)
	}
	if len(recs) != 1 {
		t.Errorf("records = %+v, want 1 (deduped)", recs)
	}
}

func TestFoldDiagnostics(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")
	writeFile(t, dir, "d/x.txt", "X")

	cases := []struct {
		name     string
		source   string
		want     string
		contains bool
	}{
		{name: "no args", source: `let x = embed();`, want: "embed expects (path) or (path, {binary: true})"},
		{name: "too many args", source: `let x = embed("a.txt", {binary: true}, 3);`, want: "embed expects (path) or (path, {binary: true})"},
		{name: "non literal path", source: `let x = embed(p);`, want: "embed path must be a string literal"},
		{name: "interpolated path", source: `let x = embed("${p}.txt");`, want: "embed path must be a string literal"},
		{name: "non literal opts", source: `let x = embed("a.txt", opts);`, want: "embed options must be a dict literal"},
		{name: "unknown opt", source: `let x = embed("a.txt", {mode: true});`, want: `unknown embed option "mode"`},
		{name: "non bool opt", source: `let x = embed("a.txt", {binary: 1});`, want: "embed option binary must be true or false"},
		{name: "named arg", source: `let x = embed("a.txt", x: true);`, want: "embed does not accept named arguments"},
		{name: "empty path", source: `let x = embed("");`, want: "embed path must not be empty"},
		{name: "absolute path", source: `let x = embed("/etc/hosts");`, want: "embed path must be relative to the source file"},
		{name: "parent escape", source: `let x = embed("../x.txt");`, want: `embed path must not contain ".."`},
		{name: "parent escape after clean", source: `let x = embed("d/../../x.txt");`, want: `embed path must not contain ".."`},
		{name: "backslash", source: `let x = embed("d\\a.txt");`, want: "embed path must use forward slashes"},
		{name: "missing file", source: `let x = embed("missing.txt");`, want: `embed cannot read "missing.txt"`, contains: true},
		{name: "directory", source: `let x = embed("d");`, want: `embed path "d" is a directory, not a file`},
		{name: "value position", source: `let f = embed;`, want: "embed is a compile-time construct and cannot be used as a value"},
		// Finding 2: pinned design behaviors.
		{name: "pipe to embed", source: `let x = 1 |> embed;`, want: "embed is a compile-time construct and cannot be used as a value"},
		{name: "spread argument", source: `let x = embed(...xs);`, want: "embed expects (path) or (path, {binary: true})"},
		// embed(_) parses as a PartialExpression; its callee hits the same value-position check as `let f = embed;`.
		{name: "hole argument", source: `let x = embed(_);`, want: "embed is a compile-time construct and cannot be used as a value"},
		{name: "type arguments", source: `let x = embed<int>("a.txt");`, want: "embed does not take type arguments"},
		// literalDictKey failure: entry is a spread, not a key:value pair.
		{name: "opts entry is spread", source: `let x = embed("a.txt", {...{}});`, want: "embed options must be a dict literal"},
		// literalDictKey failure: key is neither an identifier nor a string literal.
		{name: "opts entry has computed key", source: `let x = embed("a.txt", {1: true});`, want: "embed options must be a dict literal"},
		// non-*ast.Literal.Value type assertion to bool fails (distinct from "non bool opt", which fails the *ast.Literal assertion itself).
		{name: "opts binary null literal", source: `let x = embed("a.txt", {binary: null});`, want: "embed option binary must be true or false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, diags := foldSource(t, dir, tc.source, embedfold.Inline)
			if len(diags) != 1 {
				t.Fatalf("diagnostics = %+v, want exactly 1", diags)
			}
			got := diags[0].Message
			if tc.contains {
				if !strings.Contains(got, tc.want) {
					t.Errorf("message = %q, want it to contain %q", got, tc.want)
				}
			} else if got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
			if diags[0].Line <= 0 {
				t.Errorf("line = %d, want > 0", diags[0].Line)
			}
			if diags[0].Column <= 0 {
				t.Errorf("column = %d, want > 0", diags[0].Column)
			}
		})
	}

	// ReadFile fails after Stat succeeds; root ignores 0400-family permissions, so skip there.
	t.Run("unreadable file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		locked := filepath.Join(dir, "locked.txt")
		if err := os.WriteFile(locked, []byte("A"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o644) })

		_, _, diags := foldSource(t, dir, `let x = embed("locked.txt");`, embedfold.Inline)
		if len(diags) != 1 {
			t.Fatalf("diagnostics = %+v, want exactly 1", diags)
		}
		if !strings.Contains(diags[0].Message, `embed cannot read "locked.txt"`) {
			t.Errorf("message = %q", diags[0].Message)
		}
	})
}

func TestFoldValidateMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.bin", "\x00\x01\xff")

	t.Run("missing file diagnosed", func(t *testing.T) {
		_, _, diags := foldSource(t, dir, `let x = embed("missing.txt");`, embedfold.Validate)
		if len(diags) != 1 {
			t.Fatalf("diagnostics = %+v, want exactly 1", diags)
		}
		if !strings.Contains(diags[0].Message, `embed cannot read "missing.txt"`) {
			t.Errorf("message = %q", diags[0].Message)
		}
	})

	t.Run("placeholder literal", func(t *testing.T) {
		program, recs, diags := foldSource(t, dir, `let b = embed("a.bin", {binary: true});`, embedfold.Validate)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		lit := embeddedLiteral(t, declValue(t, program, 0))
		if lit.Content != nil {
			t.Errorf("content = %x, want nil placeholder", lit.Content)
		}
		if !lit.Binary {
			t.Error("Binary = false, want true")
		}
		if len(recs) != 1 {
			t.Fatalf("records = %+v, want 1", recs)
		}
		if recs[0].Hash != [32]byte{} {
			t.Errorf("hash = %x, want the zero value in Validate mode", recs[0].Hash)
		}
		if recs[0].Path != "a.bin" {
			t.Errorf("record path = %q, want %q", recs[0].Path, "a.bin")
		}
	})
}

func TestFoldDedupesRecords(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "d/a.txt", "A")

	source := `let a = embed("d/a.txt");
let b = embed("./d/a.txt");
`
	program, recs, diags := foldSource(t, dir, source, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %+v, want 1", recs)
	}
	first := embeddedLiteral(t, declValue(t, program, 0))
	second := embeddedLiteral(t, declValue(t, program, 1))
	if first.Path != second.Path {
		t.Errorf("paths differ: %q vs %q", first.Path, second.Path)
	}
	if &first.Content[0] != &second.Content[0] {
		t.Error("content was read twice; the two literals should share one buffer")
	}
}

func TestFoldIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	program, recs, diags := foldSource(t, dir, `let x = embed("a.txt");`, embedfold.Inline)
	if len(diags) != 0 || len(recs) != 1 {
		t.Fatalf("first fold: recs=%+v diags=%+v", recs, diags)
	}
	before := program.String()

	recs2, diags2 := embedfold.Fold(program, filepath.Join(dir, "main.gb"), embedfold.Inline)
	if len(recs2) != 0 {
		t.Errorf("second fold records = %+v, want none", recs2)
	}
	if len(diags2) != 0 {
		t.Errorf("second fold diagnostics = %+v, want none", diags2)
	}
	if after := program.String(); after != before {
		t.Errorf("program changed: %q -> %q", before, after)
	}
}

func TestFoldUserEmbedFunctionIsHijacked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	source := `func embed(string p): string { return p; }
let x = embed("a.txt");
`
	program, recs, diags := foldSource(t, dir, source, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	lit := embeddedLiteral(t, declValue(t, program, 1))
	if string(lit.Content) != "A" {
		t.Errorf("content = %q, want %q", lit.Content, "A")
	}
	if len(recs) != 1 {
		t.Errorf("records = %+v, want 1", recs)
	}
}

// Arguments fold first, so the outer call sees a folded literal rather than a call and rejects it as a non-literal path.
func TestFoldNestedEmbedArgument(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	program, recs, diags := foldSource(t, dir, `let x = embed(embed("a.txt"));`, embedfold.Inline)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly 1", diags)
	}
	if want := "embed path must be a string literal"; diags[0].Message != want {
		t.Errorf("message = %q, want %q", diags[0].Message, want)
	}
	if len(recs) != 1 {
		t.Errorf("records = %+v, want 1 for the inner embed", recs)
	}
	call, ok := declValue(t, program, 0).(*ast.CallExpression)
	if !ok {
		t.Fatalf("outer call is %T, want it left unfolded", declValue(t, program, 0))
	}
	if _, ok := call.Arguments[0].Value.(*ast.EmbeddedLiteral); !ok {
		t.Errorf("inner argument is %T, want *ast.EmbeddedLiteral", call.Arguments[0].Value)
	}
}

// Select-case send value: no expression-statement path reaches it and the formatter renders it outside exprBare.
func TestFoldExoticPosition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	source := `import io;

class Worker {
    func run(): void {
        with (acquire()) {
            try {
                select {
                    case ch.send(embed("a.txt")): { io.println("sent"); }
                    default: { io.println("idle"); }
                }
            } finally {
                io.println("done");
            }
        }
    }
}
`
	program, recs, diags := foldSource(t, dir, source, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if left := unfoldedEmbeds(program); len(left) != 0 {
		t.Errorf("unfolded embed calls remain: %v", left)
	}
	if len(recs) != 1 {
		t.Errorf("records = %+v, want 1", recs)
	}
}

// Covers the rewrite.go traversal branches the other position tests don't reach.
func TestFoldRemainingTraversalPositions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	source := `import io;

export let exported = embed("a.txt");

init {
    io.println(embed("a.txt"));
}

let [destructA, destructB] = [embed("a.txt"), "x"];

func gen(): generator<string> {
    yield embed("a.txt");
}

async func loader(): string {
    return await embed("a.txt");
}

func combine(string a, string b): string {
    return a + b;
}

if (false) {
    io.println("no");
} else if (embed("a.txt") == "A") {
    io.println("yes");
}

interface Greeter {
    func hello(string name): string;

    func greet(): string {
        return embed("a.txt");
    }

    string label;
}

enum Kind: string {
    A = embed("a.txt");

    func describe(): string {
        return embed("a.txt");
    }
}

let matchAlternates = match ("A") {
    case "A" | "B" => embed("a.txt");
    default => "none";
};

let matchListLiteral = match ([1]) {
    case [(embed("a.txt"))] => "one";
    default => "none";
};

let comprehensionFilter = [x for x in [1, 2] if embed("a.txt") == "A"];
let formattedInterp = "${embed("a.txt"):>5}";
let setComp = {embed("a.txt") for i in [1, 2]};
let dictComp = {embed("a.txt"): i for i in [1, 2]};
let notEmbed = !embed("a.txt");
let negEmbed = -embed("a.txt");
let spreadEmbed = [...embed("a.txt")];
let rangeEmbed = embed("a.txt")..embed("a.txt");
let partialEmbed = combine(embed("a.txt"), _);
`
	program, recs, diags := foldSource(t, dir, source, embedfold.Inline)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if left := unfoldedEmbeds(program); len(left) != 0 {
		t.Errorf("unfolded embed calls remain: %v", left)
	}
	if len(recs) != 1 {
		t.Errorf("records = %+v, want 1 (deduped)", recs)
	}
}

func TestFoldNilProgram(t *testing.T) {
	recs, diags := embedfold.Fold(nil, "main.gb", embedfold.Inline)
	if recs != nil || diags != nil {
		t.Errorf("Fold(nil, ...) = %+v, %+v, want nil, nil", recs, diags)
	}
}
