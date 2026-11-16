package bytecode_test

import (
	"bytes"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestParityLocalRedeclarationRejected(t *testing.T) {
	for name, body := range map[string]string{
		"variable":               "let x = 1; let x = 2;",
		"constant":               "const x = 1; let x = 2;",
		"typed":                  "int x = 1; int x = 2;",
		"unpack":                 "let x = 1; let x, y = [2, 3];",
		"duplicate_unpack":       "let x, x = [1, 2];",
		"function_then_variable": "func x(): int { return 1; } let x = 2;",
		"variable_then_function": "let x = 1; func x(): int { return 2; }",
		"twice_in_catch_body":    "try { throw ValueError(\"e\"); } catch (Error a) { let a = 1; let a = 2; }",
	} {
		t.Run(name, func(t *testing.T) {
			source := "func probe(): void { " + body + " } probe();"
			p := parser.New(lexer.New(source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			_, compileErr := bytecode.Compile(program, []byte(source), "local-redeclaration")
			_, evalErr := evaluator.New(&bytes.Buffer{}).Eval(program)

			for backend, err := range map[string]error{"compiler": compileErr, "evaluator": evalErr} {
				if err == nil || !strings.Contains(err.Error(), "already declared") {
					t.Fatalf("%s error = %v, want redeclaration rejection", backend, err)
				}
			}
			assertImportedModuleRejected(t, "func probe(): void { "+body+" } init { probe(); }")
		})
	}
}

func TestParityLocalShadowingAndRebinding(t *testing.T) {
	source := `import io;
func probe(int x): void {
    let x = 1;
    if (true) { let x = 2; io.println(x); }
    io.println(x);
    del x;
    let x = 3;
    io.println(x);
    for (i in 0..<2) { let value = i; io.println(value); }
}
probe(9);`

	ev, vm := parityOutputs(t, source)

	if ev != "2\n1\n3\n0\n1\n" || vm != ev {
		t.Fatalf("evaluator=%q VM=%q", ev, vm)
	}
}

func TestParityLocalRebindingRequiresExecutedDeletion(t *testing.T) {
	source := `import io;
func probe(bool retire): int {
    let value = 1;
    if (retire) { del value; }
    let value = 2;
    return value;
}
for (retire in [false, true]) {
    try { io.println(probe(retire)); }
    catch (Error e) { io.println(e.message.contains("already declared")); }
}
func captured(): int {
    let value = 1;
    let read = func(): int { return value; };
    del value;
    let value = 2;
    return read();
}
io.println(captured());`

	ev, vm := parityOutputs(t, source)

	if ev != "true\n2\n2\n" || vm != ev {
		t.Fatalf("evaluator=%q VM=%q", ev, vm)
	}
}
