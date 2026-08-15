package bytecode_test

// Bytes-constant + embed-record Encode/Decode coverage; compiler-side collection is covered in embed_parity_test.go.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/embedfold"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/token"
)

func writeFile(t *testing.T, dir string, rel string, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestEncodeDecodeBytesConstantAndEmbeds(t *testing.T) {
	source := `let a = "x";` + "\n"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	let := program.Statements[0].(*ast.DeclarationStatement)
	let.Value = &ast.EmbeddedLiteral{Token: token.Token{Line: 1, Column: 9}, Path: "d/a.bin", Binary: true, Content: []byte{0, 1, 255}}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(chunk.Embeds) != 1 || chunk.Embeds[0].Path != "d/a.bin" {
		t.Fatalf("embed records not collected: %+v", chunk.Embeds)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Embeds) != 1 || decoded.Embeds[0] != chunk.Embeds[0] {
		t.Errorf("embed records did not round-trip: %+v", decoded.Embeds)
	}
	var gotBytes runtime.Bytes
	found := false
	for _, constant := range decoded.Constants {
		if b, ok := constant.(runtime.Bytes); ok {
			gotBytes, found = b, true
			break
		}
	}
	if !found || !bytes.Equal(gotBytes.Value, []byte{0, 1, 255}) {
		t.Errorf("decoded Bytes constant payload = %v, found=%v, want [0 1 255]", gotBytes.Value, found)
	}

	encodedAgain, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode again: %v", err)
	}
	if !bytes.Equal(encoded, encodedAgain) {
		t.Error("Encode of the same chunk must be deterministic")
	}
}

func TestEmbedsFresh(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.gb")
	writeFile(t, dir, "d/a.txt", "AAA")
	rec := bytecode.EmbedRecord{Path: "d/a.txt", Hash: bytecode.SourceHash([]byte("AAA"))}
	chunk := bytecode.Chunk{Embeds: []bytecode.EmbedRecord{rec}}
	if !bytecode.EmbedsFresh(chunk, srcPath) {
		t.Error("fresh embeds reported stale")
	}
	writeFile(t, dir, "d/a.txt", "BBB")
	if bytecode.EmbedsFresh(chunk, srcPath) {
		t.Error("stale embed reported fresh")
	}
	os.Remove(filepath.Join(dir, "d/a.txt"))
	if bytecode.EmbedsFresh(chunk, srcPath) {
		t.Error("missing embed reported fresh")
	}
	if !bytecode.EmbedsFresh(bytecode.Chunk{}, srcPath) {
		t.Error("no-embeds chunk must always be fresh")
	}
}

// foldAndCompile runs the real parse->fold->compile pipeline against a fixture file on disk.
func foldAndCompile(t *testing.T, source string) bytecode.Chunk {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "d.txt", "EMBED-DATA")
	srcPath := filepath.Join(dir, "main.gb")
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	if _, diags := embedfold.Fold(program, srcPath, embedfold.Inline); len(diags) != 0 {
		t.Fatalf("fold diagnostics: %+v", diags)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return chunk
}

// TestConstantPathEmbedRecording covers the constant-literal compile path, which historically bypassed recordEmbed.
func TestConstantPathEmbedRecording(t *testing.T) {
	wantHash := bytecode.SourceHash([]byte("EMBED-DATA"))

	cases := []struct {
		name   string
		source string
	}{
		{
			name: "function parameter default",
			source: `func f(string x = embed("d.txt")): string {
    return x;
}
`,
		},
		{
			name: "lambda parameter default",
			source: `let g = func(string x = embed("d.txt")): string {
    return x;
};
`,
		},
		{
			name: "class static const value",
			source: `class Build {
    static const VERSION = embed("d.txt");
}
`,
		},
		{
			name: "class instance field default",
			source: `class Holder {
    string field = embed("d.txt");
}
`,
		},
		{
			name: "decorator arguments, direct and nested in a list",
			source: `@note(embed("d.txt"))
@wrapped([embed("d.txt")])
class Decorated {
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk := foldAndCompile(t, tc.source)
			if len(chunk.Embeds) != 1 {
				t.Fatalf("Embeds = %+v, want exactly one record for d.txt", chunk.Embeds)
			}
			if got := chunk.Embeds[0]; got.Path != "d.txt" || got.Hash != wantHash {
				t.Errorf("Embeds[0] = %+v, want {Path: d.txt, Hash: %x}", got, wantHash)
			}
		})
	}
}

// TestEmbedsSortedDeterministically uses two paths out of alphabetical order; one path can't tell sort from a no-op.
func TestEmbedsSortedDeterministically(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "z.txt", "Z")
	writeFile(t, dir, "a.txt", "A")
	source := `let z = embed("z.txt");
let a = embed("a.txt");
`
	srcPath := filepath.Join(dir, "main.gb")
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	if _, diags := embedfold.Fold(program, srcPath, embedfold.Inline); len(diags) != 0 {
		t.Fatalf("fold diagnostics: %+v", diags)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(chunk.Embeds) != 2 || chunk.Embeds[0].Path != "a.txt" || chunk.Embeds[1].Path != "z.txt" {
		t.Fatalf("Embeds not sorted: %+v", chunk.Embeds)
	}
}

func TestDecodeRejectsVersionMismatch(t *testing.T) {
	data := append([]byte(bytecode.Magic), 0, 0)
	binary.BigEndian.PutUint16(data[len(bytecode.Magic):], 79)
	if _, err := bytecode.Decode(data); err == nil {
		t.Fatal("expected version-mismatch decode error")
	}
}
