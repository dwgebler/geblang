package evaluator_test

import (
	"bytes"
	"testing"

	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestConsoleInput_MixedAPIsPreserveLinesAndRestoreScopes(t *testing.T) {
	source := `import io;
import cli;
let restore = io.redirectStdin(io.memory("outer first\nouter last"));
io.withStdin("alpha\nbeta\n", func(): void {
    io.println(io.stdinReadLine());
    io.println(cli.prompt("ignored"));
    io.println(io.stdinReadLine() == null);
});
io.println(io.stdinReadLine());
io.withStdin("one\ntwo\nthree\nfour", func(): void {
    io.println(cli.prompt("ignored"));
    io.println(io.readLine(io.stdin()));
    io.withStdin("inner", func(): void { io.println(cli.prompt("ignored")); });
    io.println(io.stdinReadLine());
    io.println(io.stdinReadAll());
});
io.println(io.stdinReadLine());
restore();
`
	programParser := parser.New(lexer.New(source))
	program := programParser.ParseProgram()
	if len(programParser.Errors()) != 0 {
		t.Fatal(programParser.Errors())
	}
	var output bytes.Buffer
	eval := evaluator.New(&output)
	defer eval.Cleanup()

	_, err := eval.Eval(program)

	if err != nil {
		t.Fatal(err)
	}
	want := "alpha\nbeta\ntrue\nouter first\none\ntwo\ninner\nthree\nfour\nouter last\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
