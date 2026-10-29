package evaluator

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"

	"github.com/mattn/go-isatty"
)

func (e *Evaluator) cliWithContext(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("cli.withContext expects options and body")
	}
	opts, ok := args[0].(runtime.Dict)
	if !ok || !runtime.IsCallableValue(args[1]) {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cli.withContext expects a dict and a callable"}
	}
	state := e.input.state()
	if value, present := dictField(opts, "stdin"); present {
		var source io.Reader
		var err error
		if text, ok := value.(runtime.String); ok {
			source = strings.NewReader(text.Value)
		} else {
			source, err = e.ioReader(value)
		}
		if err != nil {
			return nil, err
		}
		if source != e.input {
			state.source, state.reader, state.keys = source, bufio.NewReader(source), false
		}
	}
	stdout, stderr := e.stdout, e.stderr
	for _, stream := range []struct {
		name   string
		target *io.Writer
	}{{"stdout", &stdout}, {"stderr", &stderr}} {
		if value, present := dictField(opts, stream.name); present {
			writer, err := e.ioWriter(value)
			if err != nil {
				return nil, err
			}
			*stream.target = writer
		}
	}
	for _, name := range []string{"quiet", "terminal"} {
		if value, present := dictField(opts, name); present {
			flag, ok := value.(runtime.Bool)
			if !ok {
				return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cli.withContext " + name + " must be bool"}
			}
			if name == "quiet" {
				state.quiet = flag.Value
			} else {
				terminal := flag.Value
				state.terminal = &terminal
			}
		}
	}
	e.input.mu.Lock()
	previous := e.input.consoleInputState
	e.input.consoleInputState = state
	e.input.mu.Unlock()
	previousOut, previousErr := e.stdout, e.stderr
	e.stdout, e.stderr = stdout, stderr
	defer func() {
		e.stdout, e.stderr = previousOut, previousErr
		e.input.mu.Lock()
		e.input.consoleInputState = previous
		e.input.mu.Unlock()
	}()
	return e.callValue(args[1], nil)
}

func (e *Evaluator) cliIsTerminal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("cli.isTerminal expects no arguments")
	}
	if terminal := e.input.state().terminal; terminal != nil {
		return runtime.Bool{Value: *terminal}, nil
	}
	file, ok := e.stderr.(*os.File)
	return runtime.Bool{Value: ok && isatty.IsTerminal(file.Fd())}, nil
}
