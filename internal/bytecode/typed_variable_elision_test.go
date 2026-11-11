package bytecode_test

import (
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func typeAssertCount(t *testing.T, src string) int {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(prog, []byte(src), "elision")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	count := 0
	for _, instruction := range chunk.Instructions {
		if instruction.Op == bytecode.OpTypeAssert {
			count++
		}
	}
	return count
}

func TestTypedVariableChecksElidedWhenProven(t *testing.T) {
	proven := map[string]string{
		"literal":        `int n = 1;`,
		"intLiteralTo":   `float f = 5; decimal d = -2;`,
		"arithmetic":     `int a = 1; int b = a * 2 + a // 3 - a % 2;`,
		"compound":       `int i = 0; i += 1; i = i - 1;`,
		"mixedNumeric":   `float f = 1.0f; f = f + 1;`,
		"comparison":     `int a = 1; bool b = a < 2 && a != 0;`,
		"string":         `string s = "a"; s = s + "b";`,
		"typedCall":      `func f(): int { return 1; } int n = f();`,
		"constructor":    `class C { func C() {} } C c = C();`,
		"intRange":       `int t = 0; for (int i in 0..10) { t += i; }`,
		"typedParam":     `func g(int x): int { int y = x; x = y + 1; return x; }`,
		"primitiveQuery": `let s = "a"; let xs = [1]; int n = s.length() + xs.length(); bool b = xs.contains(1) && !s.isEmpty();`,
		"queryLoop":      `let s = "hello"; let xs = [1, 2]; let d = {"a": 1}; int total = 0; for (int i = 0; i < 10; i++) { total = total + s.length() + xs.length() + (d.get("a") as int); }`,
		"typeParameter":  `func h<T>(any x): int { T y = raw(x); return 0; } func raw(any v): any { return v; }`,
	}
	for name, src := range proven {
		if got := typeAssertCount(t, src); got != 0 {
			t.Errorf("%s: %d OpTypeAssert emitted for statically proven code", name, got)
		}
	}
	unproven := map[string]string{
		"anyCall":     `func raw(any v): any { return v; } string s = raw(1);`,
		"reassign":    `func raw(any v): any { return v; } string s = "a"; s = raw(1);`,
		"loop":        `func raw(any v): any { return v; } for (string s in raw([1])) {}`,
		"asyncCall":   `async func f(): int { return 1; } func g(): void { int n = f(); }`,
		"classLength": `class C { func C() {} func length(): any { return "x"; } } let c = C(); int n = c.length();`,
		"genericCall": `func id<T>(T v): T { return v; } int n = id(1);`,
	}
	for name, src := range unproven {
		if got := typeAssertCount(t, src); got == 0 {
			t.Errorf("%s: no OpTypeAssert emitted for an unproven value", name)
		}
	}
}
