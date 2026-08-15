package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"geblang/internal/ast"
	"geblang/internal/embedfold"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/native"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/semantic"
	"geblang/internal/token"
)

type replConfig struct {
	mode      executionMode
	traceExec bool
	moduleDir string
}

func runREPL(in io.Reader, out io.Writer, errOut io.Writer, config replConfig) int {
	if config.mode == executionVMStrict {
		fmt.Fprintln(errOut, "repl: --vm-strict is not supported until REPL VM session execution is implemented")
		return 2
	}
	moduleDir := config.moduleDir
	if moduleDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "repl: %v\n", err)
			return 1
		}
		moduleDir = cwd
	}
	session, err := evaluator.NewSession(out, nil, []string{moduleDir})
	if err != nil {
		fmt.Fprintf(errOut, "repl: %v\n", err)
		return 1
	}
	defer session.Close()

	const banner = "   _____      _     _                   \n" +
		"  / ____|    | |   | |                  \n" +
		" | |  __  ___| |__ | | __ _ _ __   __ _ \n" +
		" | | |_ |/ _ \\ '_ \\| |/ _` | '_ \\ / _` |\n" +
		" | |__| |  __/ |_) | | (_| | | | | (_| |\n" +
		"  \\_____|\\___|_.__/|_|\\__,_|_| |_|\\__, |\n" +
		"                                   __/ |\n" +
		"                                  |___/ "

	fmt.Fprintf(out, "%s\n\n", banner)
	fmt.Fprintf(out, bannerString, version)
	fmt.Fprintln(out, "Type :help for commands, :quit to exit. Press Ctrl+C twice to quit.")
	if config.traceExec {
		fmt.Fprintln(errOut, "geblang: repl execution=evaluator reason=persistent session")
	}

	lines, err := newREPLLineReader(in, out, func(line string) replCompletion {
		return completeREPLLine(line, session)
	})
	if err != nil {
		fmt.Fprintf(errOut, "repl: %v\n", err)
		return 1
	}
	defer lines.Close()

	var buffer strings.Builder
	for {
		prompt := "...> "
		if buffer.Len() == 0 {
			prompt = "geb> "
		}
		line, err := lines.ReadLine(prompt)
		if err == io.EOF {
			if buffer.Len() > 0 {
				evalREPLSource(buffer.String(), session, out, errOut)
			}
			return 0
		}
		if err != nil {
			fmt.Fprintf(errOut, "repl: %v\n", err)
			return 1
		}
		if buffer.Len() == 0 && strings.HasPrefix(strings.TrimSpace(line), ":") {
			if done := handleREPLCommand(strings.TrimSpace(line), session, lines.History(), out, errOut, &buffer); done {
				return 0
			}
			continue
		}
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		source := buffer.String()
		if needsMoreInput(source) {
			continue
		}
		evalREPLSource(source, session, out, errOut)
		buffer.Reset()
	}
}

type replLineReader interface {
	ReadLine(prompt string) (string, error)
	History() []string
	Close() error
}

type scannerLineReader struct {
	scanner *bufio.Scanner
	out     io.Writer
	history []string
}

type replCompletion struct {
	Start       int
	Replacement string
	Candidates  []string
}

type replCompleter func(line string) replCompletion

func newREPLLineReader(in io.Reader, out io.Writer, completer replCompleter) (replLineReader, error) {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if inOK && outOK && isTerminal(inFile) && isTerminal(outFile) {
		return newTerminalLineReader(inFile, outFile, completer)
	}
	return &scannerLineReader{scanner: bufio.NewScanner(in), out: out}, nil
}

func (r *scannerLineReader) ReadLine(prompt string) (string, error) {
	fmt.Fprint(r.out, prompt)
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	line := r.scanner.Text()
	if shouldRecordREPLHistory(line) {
		r.history = append(r.history, line)
	}
	return line, nil
}

func (r *scannerLineReader) History() []string {
	return append([]string(nil), r.history...)
}

func (r *scannerLineReader) Close() error {
	return nil
}

type terminalLineReader struct {
	in        *os.File
	out       *os.File
	restore   func()
	history   []string
	store     *replHistoryStore
	completer replCompleter
	// row offset of the cursor from the prompt's start row; needed
	// because VT100 `\r` only resets the column.
	cursorRow int
}

func newTerminalLineReader(in *os.File, out *os.File, completer replCompleter) (*terminalLineReader, error) {
	restore, err := enterReplRawMode(int(in.Fd()))
	if err != nil {
		return nil, err
	}
	store := newREPLHistoryStore()
	history, err := store.Load()
	if err != nil {
		restore()
		return nil, err
	}
	return &terminalLineReader{in: in, out: out, restore: restore, history: history, store: store, completer: completer}, nil
}

func (r *terminalLineReader) ReadLine(prompt string) (string, error) {
	buffer := []rune{}
	cursor := 0
	historyIndex := len(r.history)
	draft := []rune{}
	ctrlCArmed := false
	r.cursorRow = 0
	fmt.Fprint(r.out, prompt)
	tmp := make([]byte, 1)
	for {
		n, err := r.in.Read(tmp)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		switch b := tmp[0]; b {
		case '\r', '\n':
			ctrlCArmed = false
			r.endLine(prompt, buffer)
			line := string(buffer)
			if shouldRecordREPLHistory(line) {
				r.history = append(r.history, line)
			}
			return line, nil
		case 4:
			ctrlCArmed = false
			if len(buffer) == 0 {
				r.endLine(prompt, buffer)
				return "", io.EOF
			}
		case 3:
			if ctrlCArmed {
				r.endLine(prompt, buffer)
				return "", io.EOF
			}
			ctrlCArmed = true
			oldBuffer := append([]rune(nil), buffer...)
			buffer = buffer[:0]
			cursor = 0
			historyIndex = len(r.history)
			draft = draft[:0]
			r.endLine(prompt, oldBuffer)
			fmt.Fprint(r.out, "^C Press Ctrl+C again to quit.\r\n")
			fmt.Fprint(r.out, prompt)
		case 127, 8:
			ctrlCArmed = false
			if cursor > 0 {
				buffer = append(buffer[:cursor-1], buffer[cursor:]...)
				cursor--
				r.redrawLine(prompt, buffer, cursor)
			}
		case 9:
			ctrlCArmed = false
			buffer = r.completeLine(prompt, buffer)
			cursor = len(buffer)
			historyIndex = len(r.history)
			draft = append(draft[:0], buffer...)
		case 27:
			ctrlCArmed = false
			seq := make([]byte, 2)
			if _, err := io.ReadFull(r.in, seq); err != nil {
				return "", err
			}
			if seq[0] != '[' {
				continue
			}
			if seq[1] >= '0' && seq[1] <= '9' {
				final, params, err := r.readCSISequence(seq[1])
				if err != nil {
					return "", err
				}
				switch {
				case final == '~' && string(params) == "3" && cursor < len(buffer):
					// Delete
					buffer = append(buffer[:cursor], buffer[cursor+1:]...)
					r.redrawLine(prompt, buffer, cursor)
					if historyIndex == len(r.history) {
						draft = append(draft[:0], buffer...)
					}
				case final == '~' && string(params) == "1":
					// Home (VT sequence)
					cursor = 0
					r.redrawLine(prompt, buffer, cursor)
				case final == '~' && string(params) == "4":
					// End (VT sequence)
					cursor = len(buffer)
					r.redrawLine(prompt, buffer, cursor)
				}
				continue
			}
			switch seq[1] {
			case 'H':
				// Home (xterm/ANSI)
				cursor = 0
				r.redrawLine(prompt, buffer, cursor)
			case 'F':
				// End (xterm/ANSI)
				cursor = len(buffer)
				r.redrawLine(prompt, buffer, cursor)
			case 'A':
				if len(r.history) == 0 {
					continue
				}
				if historyIndex == len(r.history) {
					draft = append(draft[:0], buffer...)
				}
				if historyIndex > 0 {
					historyIndex--
				}
				buffer = []rune(r.history[historyIndex])
				cursor = len(buffer)
				r.redrawLine(prompt, buffer, cursor)
			case 'B':
				if len(r.history) == 0 {
					continue
				}
				if historyIndex < len(r.history)-1 {
					historyIndex++
					buffer = []rune(r.history[historyIndex])
				} else {
					historyIndex = len(r.history)
					buffer = append(buffer[:0], draft...)
				}
				cursor = len(buffer)
				r.redrawLine(prompt, buffer, cursor)
			case 'C':
				if cursor < len(buffer) {
					cursor++
					r.redrawLine(prompt, buffer, cursor)
				}
			case 'D':
				if cursor > 0 {
					cursor--
					r.redrawLine(prompt, buffer, cursor)
				}
			}
		default:
			ctrlCArmed = false
			if b >= 32 {
				ch := rune(b)
				if cursor == len(buffer) {
					buffer = append(buffer, ch)
					cursor++
				} else {
					buffer = append(buffer[:cursor], append([]rune{ch}, buffer[cursor:]...)...)
					cursor++
				}
				r.redrawLine(prompt, buffer, cursor)
				if historyIndex == len(r.history) {
					draft = append(draft[:0], buffer...)
				}
			}
		}
	}
}

func (r *terminalLineReader) readCSISequence(first byte) (byte, []byte, error) {
	params := []byte{first}
	tmp := make([]byte, 1)
	for len(params) < 16 {
		n, err := r.in.Read(tmp)
		if err != nil {
			return 0, nil, err
		}
		if n == 0 {
			continue
		}
		b := tmp[0]
		if b >= 0x40 && b <= 0x7e {
			return b, params, nil
		}
		params = append(params, b)
	}
	return 0, params, nil
}

func (r *terminalLineReader) completeLine(prompt string, buffer []rune) []rune {
	if r.completer == nil {
		return buffer
	}
	line := string(buffer)
	completion := r.completer(line)
	if completion.Replacement == "" && len(completion.Candidates) == 0 {
		return buffer
	}
	if completion.Replacement != "" && completion.Start >= 0 && completion.Start <= len(line) {
		buffer = []rune(line[:completion.Start] + completion.Replacement)
		r.redrawLine(prompt, buffer, len(buffer))
		return buffer
	}
	if len(completion.Candidates) > 0 {
		r.endLine(prompt, buffer)
		for _, candidate := range completion.Candidates {
			fmt.Fprintln(r.out, candidate)
		}
		r.redrawLine(prompt, buffer, len(buffer))
	}
	return buffer
}

// redrawLine repaints prompt+buffer and positions the cursor at
// `cursor`, walking up to the prompt row first so wrapped rows
// don't leak between redraws.
func (r *terminalLineReader) redrawLine(prompt string, buffer []rune, cursor int) {
	r.cursorRow = writeRedraw(r.out, r.terminalWidth(), prompt, buffer, cursor, r.cursorRow)
}

// endLine walks the cursor past any wrapped rows before emitting the
// trailing CRLF, so committed input doesn't get overprinted by the
// next thing the host writes.
func (r *terminalLineReader) endLine(prompt string, buffer []rune) {
	r.cursorRow = writeEnd(r.out, r.terminalWidth(), prompt, buffer, r.cursorRow)
}

func writeEnd(out io.Writer, width int, prompt string, buffer []rune, prevRow int) int {
	pw := displayWidth(prompt)
	totalCols := pw + len(buffer)
	lastRow := 0
	if totalCols > 0 {
		lastRow = (totalCols - 1) / width
	}
	if down := lastRow - prevRow; down > 0 {
		fmt.Fprintf(out, "\x1b[%dB", down)
	}
	fmt.Fprint(out, "\r\n")
	return 0
}

func writeRedraw(out io.Writer, width int, prompt string, buffer []rune, cursor int, prevRow int) int {
	pw := displayWidth(prompt)
	totalCols := pw + len(buffer)
	totalRows := rowsFor(totalCols, width)
	targetCol := pw + cursor
	cursorRow := targetCol / width
	cursorCol := targetCol % width

	if prevRow > 0 {
		fmt.Fprintf(out, "\x1b[%dA", prevRow)
	}
	fmt.Fprint(out, "\r\x1b[J")
	fmt.Fprint(out, prompt)
	fmt.Fprint(out, string(buffer))
	if len(buffer) > 0 && totalCols%width == 0 {
		// force wrap past terminals' sticky last column
		fmt.Fprint(out, " \r")
	}
	postRow := totalRows - 1
	if postRow < 0 {
		postRow = 0
	}
	if up := postRow - cursorRow; up > 0 {
		fmt.Fprintf(out, "\x1b[%dA", up)
	}
	fmt.Fprint(out, "\r")
	if cursorCol > 0 {
		fmt.Fprintf(out, "\x1b[%dC", cursorCol)
	}
	return cursorRow
}

func rowsFor(cells, width int) int {
	if width <= 0 {
		return 1
	}
	if cells == 0 {
		return 1
	}
	rows := cells / width
	if cells%width != 0 {
		rows++
	}
	return rows
}

// displayWidth strips ANSI CSI escapes so prompts with colour codes still measure correctly.
func displayWidth(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		n++
		i++
	}
	return n
}

func (r *terminalLineReader) terminalWidth() int {
	return terminalColumns(int(r.out.Fd()))
}

func (r *terminalLineReader) History() []string {
	return append([]string(nil), r.history...)
}

func (r *terminalLineReader) Close() error {
	var historyErr error
	if r.store != nil {
		historyErr = r.store.Save(r.history)
	}
	if r.restore == nil {
		return historyErr
	}
	r.restore()
	return historyErr
}

const replHistoryLimit = 1000

type replHistoryStore struct {
	path string
}

func newREPLHistoryStore() *replHistoryStore {
	if path := os.Getenv("GEBLANG_HISTORY"); path != "" {
		return &replHistoryStore{path: path}
	}
	configDir, err := os.UserConfigDir()
	if err == nil && configDir != "" {
		return &replHistoryStore{path: filepath.Join(configDir, "geblang", "history")}
	}
	homeDir, err := os.UserHomeDir()
	if err == nil && homeDir != "" {
		return &replHistoryStore{path: filepath.Join(homeDir, ".geblang_history")}
	}
	return &replHistoryStore{}
}

func (s *replHistoryStore) Load() ([]string, error) {
	if s.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	history := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			history = append(history, line)
		}
	}
	return trimHistory(history), nil
}

func (s *replHistoryStore) Save(history []string) error {
	if s.path == "" {
		return nil
	}
	history = trimHistory(history)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	var out strings.Builder
	for _, entry := range history {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		out.WriteString(entry)
		out.WriteByte('\n')
	}
	return os.WriteFile(s.path, []byte(out.String()), 0o600)
}

func trimHistory(history []string) []string {
	normalized := make([]string, 0, len(history))
	for _, entry := range history {
		if !shouldRecordREPLHistory(entry) {
			continue
		}
		if len(normalized) > 0 && normalized[len(normalized)-1] == entry {
			continue
		}
		normalized = append(normalized, entry)
	}
	if len(normalized) > replHistoryLimit {
		normalized = normalized[len(normalized)-replHistoryLimit:]
	}
	return normalized
}

func shouldRecordREPLHistory(line string) bool {
	switch strings.TrimSpace(line) {
	case "", ":quit", ":q", ":exit":
		return false
	default:
		return true
	}
}

var replCommands = []string{":exit", ":help", ":history", ":imports", ":load", ":members", ":mode", ":modules", ":q", ":quit", ":reset", ":stdlib", ":vars"}

func completeREPLLine(line string, session *evaluator.Session) replCompletion {
	start := completionTokenStart(line)
	prefix := line[start:]
	if strings.HasPrefix(strings.TrimLeft(line, " \t"), ":") {
		return completeFromCandidates(start, prefix, replCommands)
	}
	if dot := strings.LastIndex(prefix, "."); dot >= 0 {
		moduleName := prefix[:dot]
		memberPrefix := prefix[dot+1:]
		if moduleName == "" {
			return replCompletion{}
		}
		members := session.MemberNames(moduleName)
		for i, member := range members {
			members[i] = moduleName + "." + member
		}
		return completeFromCandidates(start, moduleName+"."+memberPrefix, members)
	}
	names := append([]string{}, session.Names()...)
	for _, imported := range session.Imports() {
		if alias, _, ok := strings.Cut(imported, "="); ok {
			names = append(names, alias)
		} else {
			names = append(names, imported)
		}
	}
	names = append(names, native.BareBuiltins...)
	names = append(names, "embed")
	return completeFromCandidates(start, prefix, uniqueSorted(names))
}

func completionTokenStart(line string) int {
	for i := len(line); i > 0; i-- {
		c := line[i-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == ':' {
			continue
		}
		return i
	}
	return 0
}

func completeFromCandidates(start int, prefix string, candidates []string) replCompletion {
	matches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix)) {
			matches = append(matches, candidate)
		}
	}
	matches = uniqueSorted(matches)
	switch len(matches) {
	case 0:
		return replCompletion{}
	case 1:
		if matches[0] == prefix {
			return replCompletion{Candidates: matches}
		}
		return replCompletion{Start: start, Replacement: matches[0]}
	default:
		common := commonPrefix(matches)
		if len(common) > len(prefix) {
			return replCompletion{Start: start, Replacement: common, Candidates: matches}
		}
		return replCompletion{Candidates: matches}
	}
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func handleREPLCommand(command string, session *evaluator.Session, history []string, out io.Writer, errOut io.Writer, buffer *strings.Builder) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case ":quit", ":q", ":exit":
		return true
	case ":help":
		fmt.Fprintln(out, "Commands: :help, :quit, :reset, :load <file>, :vars, :imports, :stdlib, :modules, :members <module>, :mode, :history")
	case ":reset":
		if err := session.Reset(); err != nil {
			fmt.Fprintf(errOut, "%v\n", err)
			return false
		}
		buffer.Reset()
		fmt.Fprintln(out, "session reset")
	case ":load":
		if len(parts) != 2 {
			fmt.Fprintln(errOut, ":load expects a file path")
			return false
		}
		data, err := os.ReadFile(parts[1])
		if err != nil {
			fmt.Fprintf(errOut, "load %s: %v\n", parts[1], err)
			return false
		}
		evalREPLSource(string(data), session, out, errOut)
	case ":vars":
		for _, name := range session.Names() {
			fmt.Fprintln(out, name)
		}
	case ":imports":
		for _, name := range session.Imports() {
			fmt.Fprintln(out, name)
		}
	case ":stdlib":
		for _, name := range session.StdlibModules() {
			fmt.Fprintln(out, name)
		}
	case ":modules":
		imports := session.Imports()
		if len(imports) == 0 {
			fmt.Fprintln(out, "imports: none")
		} else {
			fmt.Fprintln(out, "imports:")
			for _, name := range imports {
				fmt.Fprintf(out, "  %s\n", name)
			}
		}
		extensions := session.LoadedExtensions()
		if len(extensions) == 0 {
			fmt.Fprintln(out, "extensions: none")
		} else {
			fmt.Fprintln(out, "extensions:")
			for _, ext := range extensions {
				name := ext.Name
				if name == "" {
					name = "extension"
				}
				kind := "connected"
				if ext.Managed {
					kind = "managed"
				}
				fmt.Fprintf(out, "  #%d %s (%s): %s\n", ext.ID, name, kind, strings.Join(ext.Functions, ", "))
			}
		}
	case ":members":
		if len(parts) != 2 {
			fmt.Fprintln(errOut, ":members expects a module name")
			return false
		}
		members := session.MemberNames(parts[1])
		if len(members) == 0 {
			members = session.StdlibMemberNames(parts[1])
		}
		if len(members) == 0 {
			fmt.Fprintf(errOut, "unknown module or value %s\n", parts[1])
			return false
		}
		for _, name := range members {
			fmt.Fprintln(out, name)
		}
	case ":mode":
		fmt.Fprintln(out, "evaluator")
	case ":history":
		for i, entry := range history {
			fmt.Fprintf(out, "%d  %s\n", i+1, entry)
		}
	default:
		fmt.Fprintf(errOut, "unknown REPL command %s\n", parts[0])
	}
	return false
}

func evalREPLSource(source string, session *evaluator.Session, out io.Writer, errOut io.Writer) {
	program, ok := parseAnalyzeREPLSource(source, errOut, session)
	if !ok {
		return
	}
	result, err := session.Eval(program)
	if err != nil {
		var uncaught *runtime.UncaughtError
		if errors.As(err, &uncaught) {
			fmt.Fprintln(errOut, uncaught.Render())
		} else {
			fmt.Fprintf(errOut, "Error: %v\n", err)
		}
		return
	}
	if result.Exited {
		fmt.Fprintf(out, "exit(%d)\n", result.ExitCode)
		return
	}
	if result.HasValue && !result.IsVoid {
		fmt.Fprintln(out, formatREPLValue(result.Value))
	}
}

const replLineWidth = 80

func formatREPLValue(value runtime.Value) string {
	return formatREPLValueAt(value, 0)
}

func formatREPLValueAt(value runtime.Value, depth int) string {
	if depth > 4 {
		return value.Inspect()
	}
	switch v := value.(type) {
	case *runtime.List:
		return replFormatList(v.Elements, depth)
	case runtime.Dict:
		return replFormatDict(v, depth)
	case runtime.Set:
		return replFormatSet(v, depth)
	case *runtime.Instance:
		return replFormatInstance(v, depth)
	case runtime.EnumVariant:
		return replFormatEnumVariant(v, depth)
	case runtime.Error:
		return replFormatError(v)
	case runtime.DateTimeInstant:
		return "Instant(" + time.Unix(v.Unix, 0).UTC().Format("2006-01-02T15:04:05Z") + ")"
	case runtime.DateTimeDuration:
		return "Duration(" + (time.Duration(v.Seconds) * time.Second).String() + ")"
	case runtime.Type:
		return "Type<" + v.Name + ">"
	case runtime.BytecodeClass:
		if v.Module != "" {
			return "<class " + v.Module + "." + v.Name + ">"
		}
		return "<class " + v.Name + ">"
	case *runtime.Class:
		return "<class " + v.Name + ">"
	case runtime.Bytes:
		return replFormatBytes(v)
	default:
		return value.Inspect()
	}
}

// replNested formats a value in a collection or field context: strings are quoted.
func replNested(value runtime.Value, depth int) string {
	if s, ok := value.(runtime.String); ok {
		return strconv.Quote(s.Value)
	}
	return formatREPLValueAt(value, depth)
}

func replFormatList(elements []runtime.Value, depth int) string {
	if len(elements) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(elements))
	for _, el := range elements {
		parts = append(parts, replNested(el, depth+1))
	}
	return replBracket("[", "]", parts, depth)
}

func replFormatDict(v runtime.Dict, depth int) string {
	if v.Len() == 0 {
		return "{}"
	}
	parts := make([]string, 0, v.Len())
	v.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		parts = append(parts, replNested(entry.Key, depth+1)+": "+replNested(entry.Value, depth+1))
		return true
	})
	sort.Strings(parts)
	return replBracket("{", "}", parts, depth)
}

func replFormatSet(v runtime.Set, depth int) string {
	if len(v.Elements) == 0 {
		return "set{}"
	}
	parts := make([]string, 0, len(v.Elements))
	for _, entry := range v.Elements {
		parts = append(parts, replNested(entry.Value, depth+1))
	}
	sort.Strings(parts)
	return replBracket("set{", "}", parts, depth)
}

func replFormatInstance(v *runtime.Instance, depth int) string {
	if v == nil {
		return "null"
	}
	if len(v.Fields) == 0 {
		return "<" + v.Class.Name + " {}>"
	}
	parts := make([]string, 0, len(v.Fields))
	for name, field := range v.Fields {
		parts = append(parts, name+": "+replNested(field, depth+1))
	}
	sort.Strings(parts)
	return replBracket("<"+v.Class.Name+" {", "}>", parts, depth)
}

func replFormatEnumVariant(v runtime.EnumVariant, depth int) string {
	base := v.Enum.Name + "." + v.Variant
	if len(v.Fields) == 0 {
		return base
	}
	parts := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		parts = append(parts, replNested(f, depth+1))
	}
	return base + "(" + strings.Join(parts, ", ") + ")"
}

func replFormatError(v runtime.Error) string {
	var sb strings.Builder
	sb.WriteString(v.Class)
	if v.Message != "" {
		sb.WriteString(": ")
		sb.WriteString(v.Message)
	}
	if len(v.Fields) > 0 {
		fieldParts := make([]string, 0, len(v.Fields))
		for name, val := range v.Fields {
			fieldParts = append(fieldParts, name+": "+val.Inspect())
		}
		sort.Strings(fieldParts)
		sb.WriteString(" {")
		sb.WriteString(strings.Join(fieldParts, ", "))
		sb.WriteString("}")
	}
	if v.StackTrace != "" {
		lines := strings.Split(strings.TrimSpace(v.StackTrace), "\n")
		const maxFrames = 3
		shown := lines
		if len(lines) > maxFrames {
			shown = lines[:maxFrames]
		}
		for _, line := range shown {
			sb.WriteString("\n  ")
			sb.WriteString(line)
		}
		if len(lines) > maxFrames {
			sb.WriteString(fmt.Sprintf("\n  ... (%d more frames)", len(lines)-maxFrames))
		}
	}
	return sb.String()
}

func replFormatBytes(v runtime.Bytes) string {
	const previewBytes = 8
	hexStr := v.Inspect()
	if len(v.Value) <= previewBytes {
		return "bytes(" + strconv.Quote(hexStr) + ")"
	}
	return fmt.Sprintf("bytes(%d, 0x%s...)", len(v.Value), hexStr[:previewBytes*2])
}

// replBracket formats a bracketed collection inline when it fits within replLineWidth,
// otherwise wraps with one item per line and indentation.
func replBracket(open, close string, parts []string, depth int) string {
	inline := open + strings.Join(parts, ", ") + close
	if depth >= 3 || len(inline) <= replLineWidth {
		return inline
	}
	pad := strings.Repeat("  ", depth+1)
	closePad := strings.Repeat("  ", depth)
	var sb strings.Builder
	sb.WriteString(open)
	sb.WriteByte('\n')
	for i, part := range parts {
		sb.WriteString(pad)
		sb.WriteString(part)
		if i < len(parts)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(closePad)
	sb.WriteString(close)
	return sb.String()
}

// replInsertSemicolons inserts semicolons before newlines that follow
// statement-ending tokens, mirroring Go's ASI rule. This lets REPL users omit
// trailing semicolons without changing the script-mode requirement.
//
// Semicolons are NOT inserted when the line-ending token is still nested
// inside an outer `(` or `[`. Those always introduce a single expression
// (call args, list literal, indexing), so injecting `;` mid-expression is
// always wrong. Braces (`{`) are deliberately NOT tracked here because
// they straddle two cases - block-`{` (function/if/for bodies, where we
// DO want internal semicolons) and dict-`{` (where we don't). Inner
// closing `}` for a dict literal isn't a problem in practice because
// dicts don't contain statement-shaped lines that end on stmt-ender
// tokens at the dict-brace depth.
func replInsertSemicolons(source string) string {
	l := lexer.New(source)
	type lineInfo struct {
		last  token.Type
		depth int
	}
	lastOnLine := map[int]lineInfo{}
	depth := 0
	for {
		tok := l.NextToken()
		if tok.Type == token.EOF {
			break
		}
		switch tok.Type {
		case token.LParen, token.LBracket:
			depth++
		case token.RParen, token.RBracket:
			if depth > 0 {
				depth--
			}
		}
		lastOnLine[tok.Line] = lineInfo{last: tok.Type, depth: depth}
	}
	isStmtEnd := func(t token.Type) bool {
		switch t {
		case token.Ident,
			token.Int, token.Decimal, token.Float, token.String,
			token.RParen, token.RBrace, token.RBracket,
			token.Inc, token.Dec,
			token.Return, token.Break, token.Continue,
			token.Null, token.True, token.False, token.This,
			// `bool` is the only type-name that lexes as a keyword
			// (string/int/float/decimal/bytes are Idents). Treat it
			// as a stmt-ender so `x as bool` works at the REPL.
			token.Bool,
			// `>` and `>>` close generic-type-argument lists in 1.0.1+
			// expression forms like `Box<int>()` and 1.0.2's
			// `xs instanceof list<int>`. Treating them as statement-
			// enders lets the user type the line without a trailing
			// semicolon. The edge case `a > b` typed across two REPL
			// lines stops working, but that's not idiomatic REPL use.
			token.GT, token.RShift:
			return true
		}
		return false
	}
	var out strings.Builder
	curLine := 1
	for _, r := range source {
		if r == '\n' {
			info := lastOnLine[curLine]
			if isStmtEnd(info.last) && info.depth == 0 {
				out.WriteByte(';')
			}
		}
		if r == '\n' {
			curLine++
		}
		out.WriteRune(r)
	}
	return out.String()
}

func parseAnalyzeREPLSource(source string, errOut io.Writer, session *evaluator.Session) (*ast.Program, bool) {
	source = replInsertSemicolons(source)
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, msg := range p.Errors() {
			fmt.Fprintln(errOut, msg)
		}
		return nil, false
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return nil, false
	}
	sourcePath := filepath.Join(cwd, "__geblang_repl__.gb")
	if _, diags := embedfold.Fold(program, sourcePath, embedfold.Inline); len(diags) > 0 {
		d := diags[0]
		fmt.Fprintf(errOut, "%s:%d:%d: %s\n", sourcePath, d.Line, d.Column, d.Message)
		return nil, false
	}
	analyzer := semantic.New()
	if session != nil {
		for name, typeName := range session.TypeBindings() {
			analyzer.Declare(name, typeName)
		}
	}
	if diagnostics := analyzer.Analyze(program); len(diagnostics) > 0 {
		blocked := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == semantic.SeverityWarning {
				fmt.Fprintln(errOut, "warning: "+diagnostic.Message)
				continue
			}
			fmt.Fprintln(errOut, diagnostic.Message)
			blocked = true
		}
		if blocked {
			return nil, false
		}
	}
	return program, true
}

func needsMoreInput(source string) bool {
	var braces, parens, brackets int
	var quote rune
	triple := false
	escaped := false
	runes := []rune(source)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if triple {
				if r == quote && i+2 < len(runes) && runes[i+1] == quote && runes[i+2] == quote {
					quote = 0
					triple = false
					i += 2
				}
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			if i+2 < len(runes) && runes[i+1] == r && runes[i+2] == r {
				triple = true
				i += 2
			}
		case '{':
			braces++
		case '}':
			braces--
		case '(':
			parens++
		case ')':
			parens--
		case '[':
			brackets++
		case ']':
			brackets--
		}
	}
	trimmed := strings.TrimSpace(source)
	return quote != 0 || braces > 0 || parens > 0 || brackets > 0 || strings.HasSuffix(trimmed, "\\")
}
