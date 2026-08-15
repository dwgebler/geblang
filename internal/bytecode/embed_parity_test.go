package bytecode_test

// EmbeddedLiteral is synthesized (never parsed by the fold), so these tests hand-build the node.

import (
	"bytes"
	"path/filepath"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/embedfold"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/token"
)

// runParityProgram mirrors runParity but takes a pre-built program, so synthesized nodes can be exercised on both backends.
func runParityProgram(t *testing.T, program *ast.Program, source string, want string) {
	t.Helper()
	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	if err := bytecode.NewVM(chunk, &vmOut).Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if evOut.String() != vmOut.String() {
		t.Errorf("output mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

func TestParityEmbeddedLiteralTextAndBinary(t *testing.T) {
	source := `import io;
let a = "placeholder";
io.println(a);
io.println("${typeof(a)}");
`
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	let := program.Statements[1].(*ast.DeclarationStatement)
	let.Value = &ast.EmbeddedLiteral{Token: token.Token{Line: 2, Column: 9}, Path: "f.txt", Content: []byte("hello-embed")}
	runParityProgram(t, program, source, "hello-embed\nstring\n")

	p2 := parser.New(lexer.New(source))
	program2 := p2.ParseProgram()
	let2 := program2.Statements[1].(*ast.DeclarationStatement)
	let2.Value = &ast.EmbeddedLiteral{Token: token.Token{Line: 2, Column: 9}, Path: "f.bin", Binary: true, Content: []byte{0x68, 0x69}}
	// bytes render as lowercase hex (runtime.Bytes.Inspect), not decoded text.
	runParityProgram(t, program2, source, "6869\nbytes\n")
}

// runParityEmbed writes files into a temp dir, parses+folds main.gb there, then checks both backends agree.
func runParityEmbed(t *testing.T, files map[string]string, source, want string) {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		writeFile(t, dir, rel, content)
	}
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	if _, diags := embedfold.Fold(program, filepath.Join(dir, "main.gb"), embedfold.Inline); len(diags) > 0 {
		t.Fatalf("fold: %+v", diags)
	}
	runParityProgram(t, program, source, want)
}

func TestParityEmbedTextInterpolationAndFunc(t *testing.T) {
	runParityEmbed(t, map[string]string{"d/greet.txt": "hello"}, `import io;
func banner(): string { return embed("d/greet.txt"); }
io.println("${embed("d/greet.txt")}!");
io.println(banner());
`, "hello!\nhello\n")
}

func TestParityEmbedBinaryLen(t *testing.T) {
	runParityEmbed(t, map[string]string{"d/a.bin": "\x00\x01\xff"}, `import io;
let b = embed("d/a.bin", {binary: true});
io.println("${b.length()}");
`, "3\n")
}

func TestParityEmbedCrossModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "emblib.gb", "module emblib;\nexport func banner(): string { return embed(\"data/msg.txt\"); }\n")
	writeFile(t, dir, "data/msg.txt", "from-module")
	runParityModulesDir(t, dir, `import emblib;
import io;
io.println(emblib.banner());
`, "from-module\n")
}
