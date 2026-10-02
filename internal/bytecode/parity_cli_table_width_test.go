package bytecode_test

import (
	"bytes"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestParityCliTableDisplayWidth(t *testing.T) {
	source := `import io;
import cli;

let styled = cli.style("ok", {"fg": "green", "bold": true});
let rows = [
    {"name": "café", "mark": "██", "note": "a"},
    {"name": "plain", "mark": "#", "note": "b"},
    {"name": "日本", "mark": styled, "note": "c"},
    {"name": "é", "mark": "", "note": "d"}
];
let out = cli.table(rows, {"columns": ["name", "mark", "note"], "headers": ["Name", "Mark", "Note"]});
for (line in cli.stripAnsi(out).split("\n")) {
    io.println("[" + line + "]");
}
io.println(out.contains(styled));
io.println(cli.table([["█", "x"], ["ab", "y"]], ["L", "R"]));
io.println(cli.table([{"a": "é", "b": "x"}, {"a": "ee", "b": "y"}], {"columns": ["a", "b"], "separator": " | "}));
`
	program := parser.New(lexer.New(source)).ParseProgram()
	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	machine := bytecode.NewVM(chunk, &vmOut)
	machine.SetStatefulNativeCaller(evaluator.New(&vmOut))
	if err := machine.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	ev, vm := evOut.String(), vmOut.String()
	want := "[Name   Mark  Note]\n[-----  ----  ----]\n[café   ██    a   ]\n[plain  #     b   ]\n[日本   ok    c   ]\n[é            d   ]\n" +
		"true\nL   R\n--  -\n█   x\nab  y\na  | b\n-- | -\né  | x\nee | y\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
