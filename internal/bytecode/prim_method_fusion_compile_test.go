package bytecode_test

import (
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func fusedPrimCount(t *testing.T, src string) int {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(prog, []byte(src), "fusion")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	count := 0
	for _, instruction := range chunk.Instructions {
		if instruction.Op == bytecode.OpPrimMethodGlobal || instruction.Op == bytecode.OpPrimMethodLocal {
			count++
		}
	}
	return count
}

func TestPrimitiveMethodSlotFusion(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"globalNoArgs", `let s = "a"; let n = s.length();`, 1},
		{"globalConstArg", `let d = {"a": 1}; let v = d.get("a");`, 1},
		{"globalVarArg", `let d = {"a": 1}; let k = "a"; let v = d.contains(k);`, 1},
		{"local", `func f(): int { let xs = [1]; return xs.length(); }`, 1},
		{"optional", `let s = "a"; let n = s?.length();`, 0},
		{"callArg", `func k(): string { return "a"; } let d = {"a": 1}; let v = d.get(k());`, 0},
		{"computedReceiver", `let n = ("a" + "b").length();`, 0},
		{"otherMethod", `let s = "a"; let u = s.upper();`, 0},
	}
	for _, tc := range cases {
		if got := fusedPrimCount(t, tc.src); got != tc.want {
			t.Errorf("%s: %d fused instructions, want %d", tc.name, got, tc.want)
		}
	}
}
