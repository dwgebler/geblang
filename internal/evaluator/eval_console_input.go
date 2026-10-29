package evaluator

import (
	"bufio"
	"io"
	"strings"
	"sync"
)

type consoleInputState struct {
	source   io.Reader
	reader   *bufio.Reader
	quiet    bool
	keys     bool
	terminal *bool
}

type consoleInput struct {
	mu sync.Mutex
	consoleInputState
}

func newConsoleInput(source io.Reader) *consoleInput {
	return &consoleInput{consoleInputState: consoleInputState{source: source, reader: bufio.NewReader(source)}}
}

func (in *consoleInput) Read(p []byte) (int, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.reader.Read(p)
}

func (in *consoleInput) readLine() (string, bool, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	line, err := in.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false, err
	}
	eof := err == io.EOF && line == ""
	line = strings.TrimRight(line, "\r\n")
	return line, eof, nil
}

func (in *consoleInput) readKey() (tuiKey, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return readTUIKey(in.reader)
}

func (in *consoleInput) state() consoleInputState {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.consoleInputState
}

func (in *consoleInput) redirect(source io.Reader, quiet, keys bool) func() {
	if source == in {
		return func() {}
	}
	in.mu.Lock()
	previous := in.consoleInputState
	in.consoleInputState = consoleInputState{source: source, reader: bufio.NewReader(source), quiet: quiet, keys: keys, terminal: previous.terminal}
	in.mu.Unlock()
	return func() {
		in.mu.Lock()
		defer in.mu.Unlock()
		in.consoleInputState = previous
	}
}

func (e *Evaluator) readConsoleLine() (string, error) {
	line, _, err := e.input.readLine()
	return line, err
}
