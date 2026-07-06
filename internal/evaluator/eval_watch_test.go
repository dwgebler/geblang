package evaluator

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// 9.18: pins the mutation to a poll iteration via watchWaitPollHook instead of racing a spawned subprocess's sleep against watch.wait's timeout (the prior flake under parallel package load).
func TestEvaluatorRunsWatchModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watched.txt")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatalf("write watched file: %v", err)
	}

	prevHook := watchWaitPollHook
	defer func() { watchWaitPollHook = prevHook }()
	watchWaitPollHook = func(iteration int) {
		if iteration == 1 {
			if err := os.WriteFile(path, []byte("ab"), 0o644); err != nil {
				t.Errorf("write mutation: %v", err)
			}
		}
	}

	input := `import io;
import watch;

let before = watch.snapshot(` + strconv.Quote(path) + `);
io.println(before["exists"]);
io.println(before["size"]);

let result = watch.wait(` + strconv.Quote(path) + `, 2000, 10);
io.println(result["changed"]);
io.println(result["before"]["size"]);
io.println(result["after"]["size"]);

let timeout = watch.wait(` + strconv.Quote(path) + `, 0, 50);
io.println(timeout["changed"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\n1\ntrue\n1\n2\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}
