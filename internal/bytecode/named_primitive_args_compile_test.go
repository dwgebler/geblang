package bytecode_test

import (
	"bytes"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func compileNamedArgsProgram(t *testing.T, source string) bytecode.Chunk {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "named")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return chunk
}

func countOp(chunk bytecode.Chunk, op bytecode.Op) int {
	n := 0
	for _, instruction := range chunk.Instructions {
		if instruction.Op == op {
			n++
		}
	}
	return n
}

const typedNamedArgsProgram = `import io;
string s = "x";
list<int> xs = [3, 1, 2];
io.println(s.padStart(pad: "ab", width: 4));
io.println((1.0f).isCloseTo(1.1f, absTol: 0.2));
io.println(xs.reduce(initial: 1, combine: (int a, int b) => a + b));
`

const typedNamedArgsWant = "abax\ntrue\n7\n"

func TestNamedArgsOnTypedPrimitivesCompilePositional(t *testing.T) {
	chunk := compileNamedArgsProgram(t, typedNamedArgsProgram)
	if n := countOp(chunk, bytecode.OpMethodCallNamed); n != 0 {
		t.Fatalf("typed primitive receivers emitted %d named method calls, want 0", n)
	}
}

func TestNamedArgsNeedingRuntimeBindingStayNamed(t *testing.T) {
	chunk := compileNamedArgsProgram(t, `import io;
any s = "x";
string t = "y";
func w(): int { return 3; }
io.println(s.padStart(pad: "-", width: 3));
io.println(t.padStart(pad: "${w()}", width: w()));
`)
	if n := countOp(chunk, bytecode.OpMethodCallNamed); n != 2 {
		t.Fatalf("got %d named method calls, want 2 (untyped receiver, reordered side effects)", n)
	}
}

func TestNamedArgsRewriteSurvivesChunkRoundTrip(t *testing.T) {
	encoded, err := bytecode.Encode(compileNamedArgsProgram(t, typedNamedArgsProgram))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if out.String() != typedNamedArgsWant {
		t.Fatalf("got %q, want %q", out.String(), typedNamedArgsWant)
	}
}
